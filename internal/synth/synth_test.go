package synth_test

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/takiren/scenaremo/internal/audio"
	"github.com/takiren/scenaremo/internal/cache"
	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/script"
	"github.com/takiren/scenaremo/internal/synth"
	"github.com/takiren/scenaremo/internal/tts"
)

// テスト用 wav の書式。VOICEVOX の既定に合わせてある。
const (
	testSampleRate = 24000
	testBitDepth   = 16
	testNumChans   = 1
)

// テストで使うセリフの長さ。
//
// 2 進小数で正確に表せる値だけを使う。MeasureBytes は data チャンクのバイト数から
// 長さを割り戻すため、1.234 秒のような値だと最下位の 1ns がずれて、
// 実測値の検証を「誤差つき」で書く羽目になる。
const (
	dur1 = 1500 * time.Millisecond
	dur2 = 500 * time.Millisecond
	dur3 = 2250 * time.Millisecond
)

// wavBytes は d の長さの wav をメモリ上に組み立てる。
//
// ディスクを経由しないのは、合成の段取りを確かめるテストがファイルシステムに依存しないようにするため。
// エンコーダ (go-audio/wav) は io.WriteSeeker を要求するので、ここでは 44 バイトの
// 標準ヘッダを直に書く。長さの計測が見ているのは fmt と data だけなので、これで足りる。
func wavBytes(d time.Duration) []byte {
	byteRate := testSampleRate * testNumChans * testBitDepth / 8
	pcmLen := int(int64(d) * int64(byteRate) / int64(time.Second))
	pcm := make([]byte, pcmLen)
	for i := range pcm {
		pcm[i] = byte(i % 251) // 無音でも長さは変わらないが、念のため値を入れておく
	}

	b := make([]byte, 0, 44+len(pcm))
	b = append(b, "RIFF"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(36+len(pcm)))
	b = append(b, "WAVE"...)
	b = append(b, "fmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1) // PCM
	b = binary.LittleEndian.AppendUint16(b, testNumChans)
	b = binary.LittleEndian.AppendUint32(b, testSampleRate)
	b = binary.LittleEndian.AppendUint32(b, uint32(byteRate))
	b = binary.LittleEndian.AppendUint16(b, testNumChans*testBitDepth/8)
	b = binary.LittleEndian.AppendUint16(b, testBitDepth)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(pcm)))
	b = append(b, pcm...)
	return b
}

// fakeStore はメモリ上の保管庫。実物 (internal/cache.Store) と同じく
// 見つからないときは os.ErrNotExist を包んだエラーを返す。
//
// Run は Workers の数だけ goroutine を立てて同時に Get / Put を呼ぶので、
// 実物と同じくここも同時に呼ばれて安全でなければならない。
// 守っていないと map への同時書き込みでテストごと落ちる（実際に CI で落ちた）。
// 記録 (gets / puts / data) を読むのは Run が戻ったあとなので、そちらは素で触ってよい。
type fakeStore struct {
	mu   sync.Mutex
	data map[string][]byte
	gets []string
	puts []string
	// putErr が非 nil なら Put は必ず失敗する。書き込めない状況の再現に使う。
	putErr error
}

func newStore() *fakeStore {
	return &fakeStore{data: map[string][]byte{}}
}

func (s *fakeStore) Get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.gets = append(s.gets, key)
	data, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("キャッシュが見つかりません: %w", os.ErrNotExist)
	}
	return data, nil
}

func (s *fakeStore) Put(key string, wav []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.puts = append(s.puts, key)
	if s.putErr != nil {
		return s.putErr
	}
	s.data[key] = append([]byte(nil), wav...)
	return nil
}

// fakeEngine は合成の代わりに、指定された長さの wav を組み立てて返すエンジン。
type fakeEngine struct {
	kind tts.EngineKind
	// durations はテキストごとに返す wav の長さ。
	durations map[string]time.Duration
	// err が非 nil なら合成は必ず失敗する。
	err error
	// result が非 nil なら durations の代わりにこれを返す。壊れた wav を返させるのに使う。
	result *tts.SynthesizeResult
	// nilResult が true なら (nil, nil) を返す。行儀の悪い実装を差し込まれた状況の再現に使う。
	nilResult bool

	mu   sync.Mutex
	reqs []tts.SynthesizeRequest

	onSynthesize func()
}

func (e *fakeEngine) Kind() tts.EngineKind { return e.kind }

func (e *fakeEngine) Synthesize(_ context.Context, req tts.SynthesizeRequest) (*tts.SynthesizeResult, error) {
	e.mu.Lock()
	e.reqs = append(e.reqs, req)
	e.mu.Unlock()

	if e.onSynthesize != nil {
		e.onSynthesize()
	}

	if e.err != nil {
		return nil, e.err
	}
	if e.nilResult {
		return nil, nil
	}
	if e.result != nil {
		return e.result, nil
	}
	d, ok := e.durations[req.Text]
	if !ok {
		return nil, fmt.Errorf("テストの取りこぼし: %q の長さが決まっていない", req.Text)
	}
	return &tts.SynthesizeResult{WAV: wavBytes(d)}, nil
}

func (e *fakeEngine) calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.reqs)
}

// baseScript は 2 シーン・3 セリフの台本を返す。個々のテストは必要な部分だけ書き換えて使う。
func baseScript() *script.Script {
	return &script.Script{
		Meta: script.Meta{Title: "テスト動画", Aspect: script.Aspect16x9, FPS: 30},
		Speakers: map[string]script.Speaker{
			"zundamon": {Engine: script.EngineVoicevox, StyleID: 3, SpeedScale: new(1.1)},
			"metan":    {Engine: script.EngineVoicevox, StyleID: 2},
		},
		Defaults: &script.Defaults{Speaker: "zundamon"},
		Scenes: []script.Scene{
			{
				Image: "assets/01.png",
				Lines: []script.Line{
					{Text: "1つめ"},
					{Speaker: "metan", Text: "2つめ"},
				},
			},
			{
				Image: "assets/02.png",
				Lines: []script.Line{
					{Text: "3つめ"},
				},
			},
		},
	}
}

// baseEngine は baseScript の 3 セリフに長さを割り当てたエンジンを返す。
func baseEngine() *fakeEngine {
	return &fakeEngine{
		kind: tts.EngineVoicevox,
		durations: map[string]time.Duration{
			"1つめ": dur1,
			"2つめ": dur2,
			"3つめ": dur3,
		},
	}
}

func baseInput(s *script.Script, e *fakeEngine, store *fakeStore) synth.Input {
	return synth.Input{
		Script:  s,
		Engines: synth.Engines{tts.EngineVoicevox: e},
		Store:   store,
	}
}

// wantKey は台本の話者定義から、そのセリフが置かれるべきキャッシュキーを組み立てる。
//
// 実装と同じ組み立てをテスト側でも書いているのは、キーの規則が
// examples/minimal/props.json（internal/props/example_test.go が同じ規則で組む）と
// 食い違っていないことを、ここで押さえておきたいためである。
func wantKey(t *testing.T, s *script.Script, alias, text string) string {
	t.Helper()
	speaker, ok := s.Speakers[alias]
	if !ok {
		t.Fatalf("テストの取りこぼし: 話者 %q が台本にない", alias)
	}
	return cache.Key(tts.EngineKind(speaker.Engine), tts.SynthesizeRequest{
		Text:    text,
		StyleID: speaker.StyleID,
		Params: tts.Params{
			SpeedScale:      speaker.SpeedScale,
			PitchScale:      speaker.PitchScale,
			IntonationScale: speaker.IntonationScale,
			VolumeScale:     speaker.VolumeScale,
		},
	})
}

func run(t *testing.T, in synth.Input) *synth.Result {
	t.Helper()
	got, err := synth.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run が失敗した: %v", err)
	}
	return got
}

func TestRun_全セリフを合成してキャッシュへ保存する(t *testing.T) {
	s := baseScript()
	engine := baseEngine()
	store := newStore()

	got := run(t, baseInput(s, engine, store))

	if engine.calls() != 3 {
		t.Errorf("エンジンの呼び出し回数 = %d, 期待値 3", engine.calls())
	}
	if got.Synthesized != 3 || got.Cached != 0 {
		t.Errorf("Synthesized = %d, Cached = %d, 期待値 3 / 0", got.Synthesized, got.Cached)
	}
	if len(store.puts) != 3 {
		t.Errorf("Put の回数 = %d, 期待値 3", len(store.puts))
	}

	// Audio は台本と同じ形（シーンの数、各シーンのセリフの数）で返る。
	if len(got.Audio) != 2 {
		t.Fatalf("Audio のシーン数 = %d, 期待値 2", len(got.Audio))
	}
	if len(got.Audio[0]) != 2 || len(got.Audio[1]) != 1 {
		t.Fatalf("Audio のセリフ数 = %d / %d, 期待値 2 / 1", len(got.Audio[0]), len(got.Audio[1]))
	}

	wants := []struct {
		scene, line int
		alias, text string
		duration    time.Duration
	}{
		{0, 0, "zundamon", "1つめ", dur1},
		{0, 1, "metan", "2つめ", dur2},
		{1, 0, "zundamon", "3つめ", dur3},
	}
	for _, w := range wants {
		a := got.Audio[w.scene][w.line]
		wantPath := ".scenaremo/audio/" + wantKey(t, s, w.alias, w.text) + ".wav"
		if a.Path != wantPath {
			t.Errorf("Audio[%d][%d].Path = %q, 期待値 %q", w.scene, w.line, a.Path, wantPath)
		}
		// 長さは wav の実測値であること（エンジンが返したバイト列から測れていること）。
		if a.Duration != w.duration {
			t.Errorf("Audio[%d][%d].Duration = %v, 期待値 %v", w.scene, w.line, a.Duration, w.duration)
		}
		if _, ok := store.data[wantKey(t, s, w.alias, w.text)]; !ok {
			t.Errorf("Audio[%d][%d] の wav がキャッシュへ保存されていない", w.scene, w.line)
		}
	}
}

func TestRun_キャッシュがあればエンジンを呼ばない(t *testing.T) {
	s := baseScript()
	engine := baseEngine()
	store := newStore()

	// 1 回目でキャッシュを埋め、同じ入力でもう一度回す。
	first := run(t, baseInput(s, engine, store))
	engine.reqs = nil

	got := run(t, baseInput(s, engine, store))

	if engine.calls() != 0 {
		t.Errorf("エンジンの呼び出し回数 = %d, 期待値 0（キャッシュで済むはず）", engine.calls())
	}
	if got.Synthesized != 0 || got.Cached != 3 {
		t.Errorf("Synthesized = %d, Cached = %d, 期待値 0 / 3", got.Synthesized, got.Cached)
	}
	// パスも長さも 1 回目と同じであること。キャッシュを通ると尺が変わる、では困る。
	for i := range got.Audio {
		for j := range got.Audio[i] {
			if got.Audio[i][j] != first.Audio[i][j] {
				t.Errorf("Audio[%d][%d] = %+v, 1 回目 = %+v (一致すべき)", i, j, got.Audio[i][j], first.Audio[i][j])
			}
		}
	}
}

func TestRun_NoCacheならキャッシュを読まずに合成し直す(t *testing.T) {
	s := baseScript()
	engine := baseEngine()
	store := newStore()

	run(t, baseInput(s, engine, store))
	engine.reqs = nil
	store.gets = nil
	store.puts = nil

	in := baseInput(s, engine, store)
	in.NoCache = true
	got := run(t, in)

	if engine.calls() != 3 {
		t.Errorf("エンジンの呼び出し回数 = %d, 期待値 3", engine.calls())
	}
	if got.Synthesized != 3 || got.Cached != 0 {
		t.Errorf("Synthesized = %d, Cached = %d, 期待値 3 / 0", got.Synthesized, got.Cached)
	}
	if len(store.gets) != 0 {
		t.Errorf("キャッシュを %d 回読んだ。NoCache のときは読まないこと: %v", len(store.gets), store.gets)
	}
	// 読まないだけで保存は続ける。ここを止めると props.json が指す wav が無くなる。
	if len(store.puts) != 3 {
		t.Errorf("Put の回数 = %d, 期待値 3（NoCache でも保存はする）", len(store.puts))
	}
}

func TestRun_壊れたキャッシュは合成し直す(t *testing.T) {
	s := baseScript()
	engine := baseEngine()
	store := newStore()

	// 1 セリフ目だけ、長さを測れない中身にしておく（書き込みが途中で終わった wav のつもり）。
	broken := wantKey(t, s, "zundamon", "1つめ")
	store.data[broken] = []byte("これは wav ではない")
	store.data[wantKey(t, s, "metan", "2つめ")] = wavBytes(dur2)
	store.data[wantKey(t, s, "zundamon", "3つめ")] = wavBytes(dur3)

	got := run(t, baseInput(s, engine, store))

	if engine.calls() != 1 {
		t.Fatalf("エンジンの呼び出し回数 = %d, 期待値 1（壊れていた 1 件だけ）", engine.calls())
	}
	if engine.reqs[0].Text != "1つめ" {
		t.Errorf("合成し直されたセリフ = %q, 期待値 \"1つめ\"", engine.reqs[0].Text)
	}
	if got.Synthesized != 1 || got.Cached != 2 {
		t.Errorf("Synthesized = %d, Cached = %d, 期待値 1 / 2", got.Synthesized, got.Cached)
	}
	if got.Audio[0][0].Duration != dur1 {
		t.Errorf("Audio[0][0].Duration = %v, 期待値 %v（合成し直した wav の実測値）", got.Audio[0][0].Duration, dur1)
	}
	// 壊れた wav を持ち回らないよう、キャッシュも差し替えられていること。
	if _, err := audio.MeasureBytes(store.data[broken]); err != nil {
		t.Errorf("壊れたキャッシュが差し替えられていない: %v", err)
	}
}

func TestRun_合成結果が壊れていればエラーになる(t *testing.T) {
	engine := baseEngine()
	engine.result = &tts.SynthesizeResult{WAV: []byte("wav ではない")}
	store := newStore()

	_, err := synth.Run(context.Background(), baseInput(baseScript(), engine, store))
	if err == nil {
		t.Fatal("エラーになるべきだが nil が返った")
	}
	// 合成結果そのものが壊れている場合は作り直しようがないので、そこで止める。
	assertContains(t, err, "scenes[0].lines[0]", "1つめ", "壊れた wav")
	if len(store.puts) != 0 {
		t.Errorf("壊れた wav を保存してはいけない: %v", store.puts)
	}
}

// Engine も差し替えられる口なので、(nil, nil) を返す実装に当たっても nil 参照で落ちないこと。
func TestRun_エンジンが結果を返さなくても落ちない(t *testing.T) {
	engine := baseEngine()
	engine.nilResult = true

	_, err := synth.Run(context.Background(), baseInput(baseScript(), engine, newStore()))
	if err == nil {
		t.Fatal("エラーになるべきだが nil が返った")
	}
	assertContains(t, err, "scenes[0].lines[0]", "エンジンが結果を返しませんでした")
}

func TestRun_パスの接頭辞(t *testing.T) {
	tests := []struct {
		name       string
		relDir     string
		wantPrefix string
	}{
		{
			name:       "空なら既定の場所",
			relDir:     "",
			wantPrefix: ".scenaremo/audio/",
		},
		{
			name:       "指定した場所",
			relDir:     "out/audio",
			wantPrefix: "out/audio/",
		},
		{
			name:       "末尾の区切りは重ねない",
			relDir:     "out/audio/",
			wantPrefix: "out/audio/",
		},
		{
			// filepath.Join で組み立てた値を渡されても props.json は / 区切りであること。
			// 変換は filepath.ToSlash に任せる（Linux / macOS では \ はファイル名に使える文字なので、
			// OS を問わず一律に置き換えるのは誤り）。internal/props の assetPath も同じ扱い。
			name:       "OS の区切りは / に直す",
			relDir:     filepath.Join("out", "audio"),
			wantPrefix: "out/audio/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := baseScript()
			in := baseInput(s, baseEngine(), newStore())
			in.RelAudioDir = tt.relDir

			got := run(t, in)

			want := tt.wantPrefix + wantKey(t, s, "zundamon", "1つめ") + ".wav"
			if got.Audio[0][0].Path != want {
				t.Errorf("Audio[0][0].Path = %q, 期待値 %q", got.Audio[0][0].Path, want)
			}
		})
	}
}

func TestRun_話者ごとのパラメータが合成要求に載る(t *testing.T) {
	s := baseScript()
	s.Speakers["zundamon"] = script.Speaker{
		Engine:          script.EngineVoicevox,
		StyleID:         3,
		SpeedScale:      new(1.2),
		PitchScale:      new(0.05),
		IntonationScale: new(1.3),
		VolumeScale:     new(0.9),
	}
	engine := baseEngine()

	run(t, baseInput(s, engine, newStore()))

	req := engine.reqs[0]
	if req.Text != "1つめ" || req.StyleID != 3 {
		t.Errorf("SynthesizeRequest = {Text:%q StyleID:%d}, 期待値 {Text:\"1つめ\" StyleID:3}", req.Text, req.StyleID)
	}
	wants := []struct {
		name string
		got  *float64
		want float64
	}{
		{"SpeedScale", req.Params.SpeedScale, 1.2},
		{"PitchScale", req.Params.PitchScale, 0.05},
		{"IntonationScale", req.Params.IntonationScale, 1.3},
		{"VolumeScale", req.Params.VolumeScale, 0.9},
	}
	for _, w := range wants {
		if w.got == nil {
			t.Errorf("Params.%s が nil。台本の指定が落ちている", w.name)
			continue
		}
		if *w.got != w.want {
			t.Errorf("Params.%s = %v, 期待値 %v", w.name, *w.got, w.want)
		}
	}

	// 指定の無い話者は nil のまま渡ること。0 で埋めると
	// 「エンジンの既定値を使う」が「0 を指定した」に化けてしまう。
	metan := engine.reqs[1]
	if metan.Params.SpeedScale != nil || metan.Params.PitchScale != nil ||
		metan.Params.IntonationScale != nil || metan.Params.VolumeScale != nil {
		t.Errorf("指定の無いパラメータが埋められている: %+v", metan.Params)
	}
}

func TestRun_同じセリフの2回目はキャッシュヒットになる(t *testing.T) {
	s := baseScript()
	// 2 つめのシーンに 1 セリフ目とまったく同じセリフを置く。
	s.Scenes[1].Lines = []script.Line{{Text: "1つめ"}}
	engine := baseEngine()

	got := run(t, baseInput(s, engine, newStore()))

	if got.Synthesized != 2 || got.Cached != 1 {
		t.Errorf("Synthesized = %d, Cached = %d, 期待値 2 / 1", got.Synthesized, got.Cached)
	}
	// 同じ入力からは同じ wav ができるので、同じファイルを指してよい。
	if got.Audio[0][0].Path != got.Audio[1][0].Path {
		t.Errorf("同じセリフのパスが違う: %q と %q", got.Audio[0][0].Path, got.Audio[1][0].Path)
	}
	if got.Audio[0][0].Duration != got.Audio[1][0].Duration {
		t.Errorf("同じセリフの長さが違う: %v と %v", got.Audio[0][0].Duration, got.Audio[1][0].Duration)
	}
}

func TestRun_話者を引けないセリフはエラーになる(t *testing.T) {
	tests := []struct {
		name  string
		fix   func(s *script.Script)
		wants []string
	}{
		{
			name: "speakers に無いエイリアス",
			fix: func(s *script.Script) {
				s.Scenes[1].Lines[0].Speaker = "居ない人"
			},
			wants: []string{"scenes[1].lines[0]", "3つめ", `"居ない人"`, "speakers に定義されていません"},
		},
		{
			name: "話者が決まらない",
			fix: func(s *script.Script) {
				// defaults.speaker が無く、セリフにも speaker が無い状態。
				s.Defaults.Speaker = ""
			},
			wants: []string{"scenes[0].lines[0]", "1つめ", "defaults.speaker"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := baseScript()
			tt.fix(s)
			engine := baseEngine()

			_, err := synth.Run(context.Background(), baseInput(s, engine, newStore()))
			if err == nil {
				t.Fatal("エラーになるべきだが nil が返った")
			}
			assertContains(t, err, tt.wants...)
		})
	}
}

// 長いセリフや改行を含むセリフをそのままエラーに載せると、肝心の理由が台本の抜き書きに埋もれる。
func TestRun_エラーに載るセリフは丸められる(t *testing.T) {
	long := "とても長いセリフです。\n改行も含んでいますし、エラーメッセージに全文を載せると理由が読めなくなります。"
	s := baseScript()
	s.Scenes[0].Lines[0] = script.Line{Speaker: "居ない人", Text: long}

	_, err := synth.Run(context.Background(), baseInput(s, baseEngine(), newStore()))
	if err == nil {
		t.Fatal("エラーになるべきだが nil が返った")
	}

	msg := err.Error()
	if strings.Contains(msg, long) {
		t.Errorf("セリフが丸められていない: %v", err)
	}
	if strings.Contains(msg, "\n") {
		t.Errorf("エラーメッセージに改行が混ざっている: %q", msg)
	}
	// どのセリフの話かは分かること。頭が残っていれば台本の中から目で見つけられる。
	assertContains(t, err, "scenes[0].lines[0]", "とても長いセリフです。", "…")
}

func TestRun_エンジンを用意できないセリフはエラーになる(t *testing.T) {
	s := baseScript()
	s.Speakers["metan"] = script.Speaker{Engine: "aivisspeech", StyleID: 2}
	engine := baseEngine()

	_, err := synth.Run(context.Background(), baseInput(s, engine, newStore()))
	if err == nil {
		t.Fatal("エラーになるべきだが nil が返った")
	}
	// どのセリフで詰まったのかと、代わりに何が使えるのかが分かること。
	assertContains(t, err, "scenes[0].lines[1]", "2つめ", `"metan"`, `"aivisspeech"`, "voicevox")
	// 1 セリフ目は合成済みなので、そこまでの成果はキャッシュに残っている。
	if engine.calls() != 1 {
		t.Errorf("エンジンの呼び出し回数 = %d, 期待値 1（失敗したセリフの手前まで）", engine.calls())
	}
}

// errFailingResolver は EngineResolver が返した原因を、呼び出し側が errors.Is で拾えることの目印。
var errFailingResolver = errors.New("エンジンの設定が読めない")

// failingResolver は何を引かれても失敗する EngineResolver。
type failingResolver struct{}

func (failingResolver) Engine(tts.EngineKind) (tts.Engine, error) { return nil, errFailingResolver }

// nilResolver は誤って (nil, nil) を返す EngineResolver。
type nilResolver struct{}

func (nilResolver) Engine(tts.EngineKind) (tts.Engine, error) { return nil, nil }

func TestRun_エンジンの解決に失敗した原因は残る(t *testing.T) {
	in := baseInput(baseScript(), baseEngine(), newStore())
	in.Engines = failingResolver{}

	_, err := synth.Run(context.Background(), in)
	if err == nil {
		t.Fatal("エラーになるべきだが nil が返った")
	}
	if !errors.Is(err, errFailingResolver) {
		t.Errorf("errors.Is で原因を拾えない: %v", err)
	}
	assertContains(t, err, "scenes[0].lines[0]")
}

// EngineResolver は差し替えられる口なので、(nil, nil) を返す実装に当たっても nil 参照で落ちないこと。
func TestRun_エンジンがnilで返っても落ちない(t *testing.T) {
	in := baseInput(baseScript(), baseEngine(), newStore())
	in.Engines = nilResolver{}

	_, err := synth.Run(context.Background(), in)
	if err == nil {
		t.Fatal("エラーになるべきだが nil が返った")
	}
	assertContains(t, err, "scenes[0].lines[0]", "nil")
}

func TestRun_合成の失敗は位置つきで返り原因も残る(t *testing.T) {
	engine := baseEngine()
	// エンジン未起動。build で最も多い失敗であり、この文面がそのまま利用者に届く。
	engine.err = &tts.EngineUnavailableError{
		Kind:    tts.EngineVoicevox,
		BaseURL: "http://127.0.0.1:50021",
		Err:     errors.New("connection refused"),
	}
	reporter := &recorder{}
	in := baseInput(baseScript(), engine, newStore())
	in.Reporter = reporter

	_, err := synth.Run(context.Background(), in)
	if err == nil {
		t.Fatal("エラーになるべきだが nil が返った")
	}
	assertContains(t, err, "scenes[0].lines[0]", "1つめ", "音声の合成に失敗しました")

	// 原因の型が残っていること。CLI はこれを見て案内を出し分けられる。
	var unavailable *tts.EngineUnavailableError
	if !errors.As(err, &unavailable) {
		t.Errorf("errors.As で *tts.EngineUnavailableError を拾えない: %v", err)
	}
	if slices.Contains(reporter.events, "Done(0, 0)") {
		t.Errorf("失敗した回に Done が呼ばれている: %v", reporter.events)
	}
}

func TestRun_キャッシュへ保存できなければエラーになる(t *testing.T) {
	store := newStore()
	// props.json が指す wav はここに置かれるので、保存できないまま進むと音の出ない動画になる。
	store.putErr = errors.New("ディスクがいっぱいです")

	_, err := synth.Run(context.Background(), baseInput(baseScript(), baseEngine(), store))
	if err == nil {
		t.Fatal("エラーになるべきだが nil が返った")
	}
	assertContains(t, err, "scenes[0].lines[0]", "音声を保存できませんでした", "ディスクがいっぱいです")
}

func TestRun_ctxのキャンセルで途中で止まる(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	engine := baseEngine()
	reporter := &recorder{onLineDone: func(index int) {
		if index == 0 {
			cancel() // 1 セリフ目を終えた直後に Ctrl-C が押された状況
		}
	}}
	in := baseInput(baseScript(), engine, newStore())
	in.Reporter = reporter

	_, err := synth.Run(ctx, in)
	if err == nil {
		t.Fatal("エラーになるべきだが nil が返った")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) が false: %v", err)
	}
	assertContains(t, err, "scenes[0].lines[1]", "2つめ", "再実行すれば続きから")
	if engine.calls() != 1 {
		t.Errorf("エンジンの呼び出し回数 = %d, 期待値 1（打ち切り後は合成しない）", engine.calls())
	}
	for _, e := range reporter.events {
		if strings.HasPrefix(e, "Done(") {
			t.Errorf("打ち切った回に Done が呼ばれている: %v", reporter.events)
		}
	}
}

// キャッシュヒットばかりだと Synthesize を通らないため、ctx を見る機会が別に要る。
func TestRun_キャッシュヒットでもctxのキャンセルで止まる(t *testing.T) {
	s := baseScript()
	engine := baseEngine()
	store := newStore()
	run(t, baseInput(s, engine, store)) // 全件をキャッシュへ入れておく
	engine.reqs = nil
	store.gets = nil

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := synth.Run(ctx, baseInput(s, engine, store))
	if err == nil {
		t.Fatal("エラーになるべきだが nil が返った")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) が false: %v", err)
	}
	if len(store.gets) != 0 || engine.calls() != 0 {
		t.Errorf("打ち切ったのに処理が進んでいる: Get %d 回, 合成 %d 回", len(store.gets), engine.calls())
	}
}

func TestRun_入力の検証(t *testing.T) {
	tests := []struct {
		name string
		in   func() synth.Input
		want string
	}{
		{
			name: "台本が無い",
			in: func() synth.Input {
				in := baseInput(nil, baseEngine(), newStore())
				return in
			},
			want: "台本がありません",
		},
		{
			name: "シーンが無い",
			in: func() synth.Input {
				s := baseScript()
				s.Scenes = nil
				return baseInput(s, baseEngine(), newStore())
			},
			want: "シーンが1つもありません",
		},
		{
			name: "エンジンが渡されていない",
			in: func() synth.Input {
				in := baseInput(baseScript(), baseEngine(), newStore())
				in.Engines = nil
				return in
			},
			want: "音声合成エンジンが渡されていません",
		},
		{
			name: "保管庫が渡されていない",
			in: func() synth.Input {
				in := baseInput(baseScript(), baseEngine(), newStore())
				in.Store = nil
				return in
			},
			want: "音声の保管庫が渡されていません",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// nil 参照で落ちるのではなく、何が足りないのかが分かるエラーになること。
			_, err := synth.Run(context.Background(), tt.in())
			if err == nil {
				t.Fatal("エラーになるべきだが nil が返った")
			}
			assertContains(t, err, tt.want)
		})
	}
}

func TestRun_Reporterの呼び出し順と引数(t *testing.T) {
	s := baseScript()
	engine := baseEngine()
	store := newStore()
	reporter := &recorder{}
	in := baseInput(s, engine, store)
	in.Reporter = reporter

	run(t, in)

	want := []string{
		"Start(3)",
		"LineStart(0, zundamon, 1つめ)",
		"LineDone(0, cached=false, 1.5s)",
		"LineStart(1, metan, 2つめ)",
		"LineDone(1, cached=false, 500ms)",
		"LineStart(2, zundamon, 3つめ)",
		"LineDone(2, cached=false, 2.25s)",
		"Done(3, 0)",
	}
	if !slices.Equal(reporter.events, want) {
		t.Errorf("進捗の通知が期待と違う:\n got: %v\nwant: %v", reporter.events, want)
	}

	// 2 周目はすべてキャッシュヒットとして通知されること。
	reporter.events = nil
	run(t, in)

	want = []string{
		"Start(3)",
		"LineStart(0, zundamon, 1つめ)",
		"LineDone(0, cached=true, 1.5s)",
		"LineStart(1, metan, 2つめ)",
		"LineDone(1, cached=true, 500ms)",
		"LineStart(2, zundamon, 3つめ)",
		"LineDone(2, cached=true, 2.25s)",
		"Done(0, 3)",
	}
	if !slices.Equal(reporter.events, want) {
		t.Errorf("2 周目の進捗の通知が期待と違う:\n got: %v\nwant: %v", reporter.events, want)
	}
}

func TestRun_並列に合成される(t *testing.T) {
	s := baseScript()
	store := newStore()

	engine := baseEngine()
	// 3 件のセリフが並列に呼ばれることを確認する。
	// 各合成リクエストで 50ms 待つ。順次なら 150ms かかるが、並列なら 50ms で終わるはず。
	engine.onSynthesize = func() {
		time.Sleep(50 * time.Millisecond)
	}

	in := baseInput(s, engine, store)
	in.Workers = 3 // 3 並列

	start := time.Now()
	got := run(t, in)
	elapsed := time.Since(start)

	if got.Synthesized != 3 {
		t.Errorf("Synthesized = %d, 期待値 3", got.Synthesized)
	}
	
	if elapsed >= 100*time.Millisecond {
		t.Errorf("合成に時間がかかりすぎている（並列化されていない）: %v", elapsed)
	}
}

// 進捗表示は CLI の都合であって合成の前提ではないので、渡されなくても動くこと。
func TestRun_Reporterがnilでも動く(t *testing.T) {
	in := baseInput(baseScript(), baseEngine(), newStore())
	in.Reporter = nil

	got := run(t, in)

	if got.Synthesized != 3 {
		t.Errorf("Synthesized = %d, 期待値 3", got.Synthesized)
	}
}

// Result.Audio がそのまま props.Build に通ること。
// ここが噛み合っていないと、合成が成功しても props.json を組み立てられない。
func TestRun_結果をpropsBuildへ渡せる(t *testing.T) {
	s := baseScript()
	got := run(t, baseInput(s, baseEngine(), newStore()))

	p, err := props.Build(props.Input{
		Script: s,
		Audio:  got.Audio,
		Credits: map[string]props.SpeakerCredit{
			"zundamon": {Name: "ずんだもん", UUID: "uuid-zundamon"},
			"metan":    {Name: "四国めたん", UUID: "uuid-metan"},
		},
		GeneratedBy: "scenaremo v0.0.0-test",
	})
	if err != nil {
		t.Fatalf("props.Build が失敗した: %v", err)
	}

	if p.Scenes[0].Lines[0].Audio != got.Audio[0][0].Path {
		t.Errorf("props の audio = %q, 合成結果のパス = %q (一致すべき)", p.Scenes[0].Lines[0].Audio, got.Audio[0][0].Path)
	}
	// 1.5 秒 / 30fps = 45 フレーム。実測長がそのまま尺になっていること。
	if p.Scenes[0].Lines[0].DurationInFrames != 45 {
		t.Errorf("props の durationInFrames = %d, 期待値 45", p.Scenes[0].Lines[0].DurationInFrames)
	}
}

func TestEngines_未登録のエンジンは使えるものを添えて断る(t *testing.T) {
	engines := synth.Engines{
		tts.EngineVoicevox:    &fakeEngine{kind: tts.EngineVoicevox},
		tts.EngineAivisSpeech: &fakeEngine{kind: tts.EngineAivisSpeech},
	}

	got, err := engines.Engine(tts.EngineVoicevox)
	if err != nil {
		t.Fatalf("登録済みのエンジンを引けない: %v", err)
	}
	if got.Kind() != tts.EngineVoicevox {
		t.Errorf("引けたエンジンの種別 = %q, 期待値 %q", got.Kind(), tts.EngineVoicevox)
	}

	if _, err := engines.Engine(tts.EngineCoeiroink); err == nil {
		t.Error("未登録のエンジンがエラーにならなかった")
	} else {
		assertContains(t, err, `"coeiroink"`, "aivisspeech, voicevox")
	}

	// 1 つも登録が無い場合も、nil を返して呼び出し側を落とさないこと。
	empty := synth.Engines{}
	if _, err := empty.Engine(tts.EngineVoicevox); err == nil {
		t.Error("空の対応表がエラーにならなかった")
	}
}

// recorder は Reporter の呼ばれ方を記録する。
type recorder struct {
	events []string
	// onLineDone は 1 セリフを終えた時点に割り込むための穴。ctx の打ち切りを再現するのに使う。
	onLineDone func(index int)
}

func (r *recorder) Start(total int) {
	r.events = append(r.events, fmt.Sprintf("Start(%d)", total))
}

func (r *recorder) LineStart(index int, speaker, text string) {
	r.events = append(r.events, fmt.Sprintf("LineStart(%d, %s, %s)", index, speaker, text))
}

func (r *recorder) LineDone(index int, cached bool, d time.Duration) {
	r.events = append(r.events, fmt.Sprintf("LineDone(%d, cached=%t, %v)", index, cached, d))
	if r.onLineDone != nil {
		r.onLineDone(index)
	}
}

func (r *recorder) Done(synthesized, cached int) {
	r.events = append(r.events, fmt.Sprintf("Done(%d, %d)", synthesized, cached))
}

// assertContains はエラーメッセージに必要な手掛かりが揃っているかを確かめる。
func assertContains(t *testing.T, err error, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("エラーメッセージに %q が含まれない: %v", want, err)
		}
	}
}
