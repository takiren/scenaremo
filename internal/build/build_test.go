package build_test

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/takiren/scenaremo/internal/build"
	"github.com/takiren/scenaremo/internal/script"
	"github.com/takiren/scenaremo/internal/tts"
)

// テストで使うセリフの長さ。
//
// 2 進小数で正確に表せる値だけを使う。長さは wav の data チャンクのバイト数から割り戻されるため、
// 1.234 秒のような値だと最下位の 1ns がずれ、実測値の検証を「誤差つき」で書く羽目になる。
const (
	dur1 = 1500 * time.Millisecond
	dur2 = 500 * time.Millisecond
	dur3 = 2250 * time.Millisecond
)

// テストの wav の書式。VOICEVOX の既定に合わせてある。
const (
	testSampleRate = 24000
	testNumChans   = 1
	testBitDepth   = 16
)

// scriptYAML はテストで使う台本。2 シーン・3 セリフ・話者 2 人。
const scriptYAML = `
meta:
  title: "テスト動画"
  fps: 30

speakers:
  zundamon:
    styleId: 3
  metan:
    styleId: 2
    speedScale: 1.05

defaults:
  speaker: zundamon

scenes:
  - image: assets/01.png
    lines:
      - text: 一つ目のセリフ
      - speaker: metan
        text: 二つ目のセリフ
  - image: assets/02.png
    lines:
      - text: 三つ目のセリフ
`

// lineDurations はセリフごとに偽エンジンが返す wav の長さ。
var lineDurations = map[string]time.Duration{
	"一つ目のセリフ": dur1,
	"二つ目のセリフ": dur2,
	"三つ目のセリフ": dur3,
}

// videoDir はテスト用の動画ディレクトリを作る。台本と、台本が参照する画像を置く。
//
// ここで実物のファイルを置くのは、build が確かめたいことが「部品を正しい順で呼ぶ」ではなく
// 「台本を読んで props.json と wav が実際に出来上がる」ことだからである。
func videoDir(t *testing.T, yaml string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("assets を作れない: %v", err)
	}
	for _, name := range []string{"01.png", "02.png"} {
		if err := os.WriteFile(filepath.Join(dir, "assets", name), []byte("png"), 0o644); err != nil {
			t.Fatalf("画像を置けない: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "script.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("台本を置けない: %v", err)
	}
	return dir
}

// fakeEngine は合成の代わりに指定された長さの wav を返し、話者一覧も答えるエンジン。
//
// build の差し替え口はこれ 1 つだけである。エンジンさえ偽物にすれば、台本の読み込みから
// キャッシュへの書き出し、props.json の出力までを実物のまま通せる。
type fakeEngine struct {
	kind tts.EngineKind

	// synthesized は合成を頼まれた要求。件数と中身の両方を見る。
	synthesized []tts.SynthesizeRequest
	// listed は話者一覧を聞かれた回数。
	listed int

	// speakers は /speakers の答え。nil なら既定の 2 人を返す。
	speakers []tts.Speaker
	// synthErr が非 nil なら合成は必ず失敗する。
	synthErr error
	// listErr が非 nil なら話者一覧の取得は必ず失敗する。
	listErr error
	// onSynthesize は合成のたびに呼ばれる。ctx の打ち切りを途中で起こすのに使う。
	onSynthesize func(int)
}

func (e *fakeEngine) Kind() tts.EngineKind { return e.kind }

func (e *fakeEngine) Synthesize(_ context.Context, req tts.SynthesizeRequest) (*tts.SynthesizeResult, error) {
	e.synthesized = append(e.synthesized, req)
	if e.onSynthesize != nil {
		e.onSynthesize(len(e.synthesized))
	}
	if e.synthErr != nil {
		return nil, e.synthErr
	}
	d, ok := lineDurations[req.Text]
	if !ok {
		return nil, errors.New("テストの取りこぼし: " + req.Text + " の長さが決まっていない")
	}
	return &tts.SynthesizeResult{WAV: wavBytes(d)}, nil
}

func (e *fakeEngine) Speakers(context.Context) ([]tts.Speaker, error) {
	e.listed++
	if e.listErr != nil {
		return nil, e.listErr
	}
	if e.speakers != nil {
		return e.speakers, nil
	}
	return defaultSpeakers(), nil
}

// defaultSpeakers は台本の styleId (3 と 2) を解決できる話者一覧。
func defaultSpeakers() []tts.Speaker {
	return []tts.Speaker{
		{
			Name:        "ずんだもん",
			SpeakerUUID: "388f246b-8c41-4ac1-8e2d-5d79f3ff56d9",
			Styles:      []tts.Style{{Name: "ノーマル", ID: 3}},
		},
		{
			Name:        "四国めたん",
			SpeakerUUID: "7ffcb7ce-00ec-4bdc-82cd-45a8889e43ff",
			Styles:      []tts.Style{{Name: "ノーマル", ID: 2}},
		},
	}
}

// opened は EngineFactory が呼ばれたときの引数 1 回分。
type opened struct {
	kind    tts.EngineKind
	baseURL string
}

// factory は fakeEngine を返す EngineFactory と、開かれたエンジンの記録を返す。
func factory(e *fakeEngine) (build.EngineFactory, *[]opened) {
	var log []opened
	return func(kind tts.EngineKind, baseURL string) (build.Engine, error) {
		log = append(log, opened{kind: kind, baseURL: baseURL})
		return e, nil
	}, &log
}

// wavBytes は d の長さの wav をメモリ上に組み立てる。
// 長さの計測が見ているのは fmt と data だけなので、44 バイトの標準ヘッダを直に書けば足りる。
func wavBytes(d time.Duration) []byte {
	byteRate := testSampleRate * testNumChans * testBitDepth / 8
	pcmLen := int(int64(d) * int64(byteRate) / int64(time.Second))
	pcm := make([]byte, pcmLen)

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

func TestRun_台本からpropsjsonとwavまで一気通貫で作る(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, _ := factory(engine)

	res, err := build.Run(context.Background(), build.Options{
		Dir:         dir,
		GeneratedBy: "scenaremo (test)",
		NewEngine:   newEngine,
	})
	if err != nil {
		t.Fatalf("build に失敗した: %v", err)
	}

	if res.Synthesized != 3 || res.Cached != 0 {
		t.Errorf("件数が違う: 合成 %d 件・キャッシュ %d 件", res.Synthesized, res.Cached)
	}
	if len(engine.synthesized) != 3 {
		t.Errorf("合成した回数が違う: %d", len(engine.synthesized))
	}

	// props.json が実際に書き出されていること。
	data, err := os.ReadFile(filepath.Join(dir, ".scenaremo", "props.json"))
	if err != nil {
		t.Fatalf("props.json が書かれていない: %v", err)
	}
	validateAgainstSchema(t, data)

	if res.Props.Meta.Title != "テスト動画" {
		t.Errorf("タイトルが違う: %q", res.Props.Meta.Title)
	}
	// 尺は音声の実測長で決まる。
	// シーン 1 は 1.5 秒 + 余白 0.3 秒 + 0.5 秒 + シーン末尾 0.1 秒 = 45+9+15+3 フレーム、
	// シーン 2 は 2.25 秒 + シーン末尾 0.1 秒 = 68+3 フレーム（30fps・切り上げ）。
	// これに既定で入るクレジットシーンの尺が乗る（→ issue #17）。長さの決め方は props の側の話なので、
	// 申告された値をそのまま足して、ここでは喋りの尺の積み上げだけを見張る。
	if got, want := res.Props.Meta.DurationInFrames,
		(45+9+15+3)+(68+3)+res.Props.Credits.DurationInFrames; got != want {
		t.Errorf("総フレーム数が違う: %d (want %d)", got, want)
	}

	// クレジットが台本の話者から集計されていること。
	if len(res.Props.Credits.Entries) != 2 {
		t.Fatalf("クレジットの件数が違う: %d", len(res.Props.Credits.Entries))
	}
	if got, want := res.Props.Credits.Entries[0].Text, "VOICEVOX:ずんだもん"; got != want {
		t.Errorf("クレジット表記が違う: %q (want %q)", got, want)
	}

	// props.json が指す wav が実在すること。ここがずれるとレンダリング時に音が鳴らない。
	for i, scene := range res.Props.Scenes {
		for j, line := range scene.Lines {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(line.Audio))); err != nil {
				t.Errorf("scenes[%d].lines[%d] の音声が実在しない (%s): %v", i, j, line.Audio, err)
			}
		}
	}
}

func TestRun_2回目はエンジンを呼ばない(t *testing.T) {
	dir := videoDir(t, scriptYAML)

	first := &fakeEngine{kind: tts.EngineVoicevox}
	newFirst, _ := factory(first)
	if _, err := build.Run(context.Background(), build.Options{Dir: dir, NewEngine: newFirst}); err != nil {
		t.Fatalf("1 回目の build に失敗した: %v", err)
	}

	// 2 回目はまっさらなエンジンで走らせる。合成を頼まれた回数が 0 なら、キャッシュだけで済んでいる。
	second := &fakeEngine{kind: tts.EngineVoicevox}
	newSecond, _ := factory(second)
	res, err := build.Run(context.Background(), build.Options{Dir: dir, NewEngine: newSecond})
	if err != nil {
		t.Fatalf("2 回目の build に失敗した: %v", err)
	}

	if len(second.synthesized) != 0 {
		t.Errorf("2 回目なのに合成している: %d 件", len(second.synthesized))
	}
	if res.Cached != 3 || res.Synthesized != 0 {
		t.Errorf("2 回目の件数が違う: 合成 %d 件・キャッシュ %d 件", res.Synthesized, res.Cached)
	}
}

func TestRun_NoCacheなら合成し直す(t *testing.T) {
	dir := videoDir(t, scriptYAML)

	first := &fakeEngine{kind: tts.EngineVoicevox}
	newFirst, _ := factory(first)
	if _, err := build.Run(context.Background(), build.Options{Dir: dir, NewEngine: newFirst}); err != nil {
		t.Fatalf("1 回目の build に失敗した: %v", err)
	}

	second := &fakeEngine{kind: tts.EngineVoicevox}
	newSecond, _ := factory(second)
	res, err := build.Run(context.Background(), build.Options{Dir: dir, NoCache: true, NewEngine: newSecond})
	if err != nil {
		t.Fatalf("2 回目の build に失敗した: %v", err)
	}

	if len(second.synthesized) != 3 {
		t.Errorf("--no-cache なのに合成し直していない: %d 件", len(second.synthesized))
	}
	if res.Synthesized != 3 {
		t.Errorf("合成の件数が違う: %d", res.Synthesized)
	}
}

func TestRun_台本ファイルを直に指定できる(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, _ := factory(engine)

	res, err := build.Run(context.Background(), build.Options{
		Dir:       filepath.Join(dir, "script.yaml"),
		NewEngine: newEngine,
	})
	if err != nil {
		t.Fatalf("build に失敗した: %v", err)
	}
	if res.Layout.Dir != dir {
		t.Errorf("動画ディレクトリが台本の親になっていない: %q", res.Layout.Dir)
	}
	if _, err := os.Stat(res.Layout.PropsPath); err != nil {
		t.Errorf("props.json が書かれていない: %v", err)
	}
}

// TestRun_クレジットの解決は合成より先 は、失敗するなら合成を始める前に落ちることを確かめる。
//
// 合成は数分かかる。styleId の間違いやエンジンの不調で結局失敗するのなら、
// 全セリフを喋らせ終えたあとではなく、始まってすぐに落ちなければ意味がない。
func TestRun_クレジットの解決は合成より先(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	engine := &fakeEngine{
		kind: tts.EngineVoicevox,
		// 台本が使う styleId 2 を知らないエンジン。
		speakers: []tts.Speaker{{Name: "ずんだもん", Styles: []tts.Style{{ID: 3}}}},
	}
	newEngine, _ := factory(engine)

	_, err := build.Run(context.Background(), build.Options{Dir: dir, NewEngine: newEngine})
	if err == nil {
		t.Fatal("解決できない話者があるのに成功した")
	}
	if len(engine.synthesized) != 0 {
		t.Errorf("クレジットを解決できないのに合成を始めている: %d 件", len(engine.synthesized))
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".scenaremo", "props.json")); statErr == nil {
		t.Error("失敗したのに props.json を書いている")
	}
}

func TestRun_話者一覧の取得に失敗したら理由をたどれる(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	engine := &fakeEngine{
		kind:    tts.EngineVoicevox,
		listErr: &tts.EngineUnavailableError{Kind: tts.EngineVoicevox, BaseURL: "http://127.0.0.1:50021"},
	}
	newEngine, _ := factory(engine)

	_, err := build.Run(context.Background(), build.Options{Dir: dir, NewEngine: newEngine})
	if err == nil {
		t.Fatal("エンジンに繋がらないのに成功した")
	}
	var unavailable *tts.EngineUnavailableError
	if !errors.As(err, &unavailable) {
		t.Errorf("エンジン未起動の案内が失われている: %v", err)
	}
}

func TestRun_使う話者のエンジンは種別ごとに1つだけ開く(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, log := factory(engine)

	if _, err := build.Run(context.Background(), build.Options{Dir: dir, NewEngine: newEngine}); err != nil {
		t.Fatalf("build に失敗した: %v", err)
	}

	// 話者は 2 人・セリフは 3 つあるが、エンジンは種別ごとに 1 つでよい。
	if len(*log) != 1 {
		t.Errorf("エンジンを開いた回数が違う: %v", *log)
	}
	// 合成とクレジットで同じクライアントを使い回すので、話者一覧の問い合わせも 1 回で済む。
	if engine.listed != 1 {
		t.Errorf("話者一覧の問い合わせ回数が違う: %d", engine.listed)
	}
}

func TestRun_接続先がエンジンへ渡る(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, log := factory(engine)

	if _, err := build.Run(context.Background(), build.Options{
		Dir:         dir,
		VoicevoxURL: "http://192.168.0.2:50021",
		NewEngine:   newEngine,
	}); err != nil {
		t.Fatalf("build に失敗した: %v", err)
	}

	if len(*log) != 1 || (*log)[0].baseURL != "http://192.168.0.2:50021" {
		t.Errorf("指定した接続先が渡っていない: %v", *log)
	}
}

func TestRun_接続先が壊れていればエンジンを作る前に落ちる(t *testing.T) {
	dir := videoDir(t, scriptYAML)

	// NewEngine を差し替えず、実物のクライアントを作らせる。
	// URL として解釈できない値ならネットワークへ出る前に弾かれるので、エンジンを起動せずに確かめられる。
	_, err := build.Run(context.Background(), build.Options{
		Dir:         dir,
		VoicevoxURL: "127.0.0.1:50021",
	})
	if err == nil {
		t.Fatal("接続先が壊れているのに成功した")
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".scenaremo", "props.json")); statErr == nil {
		t.Error("失敗したのに props.json を書いている")
	}
}

func TestRun_エンジンがnilで返っても落ちない(t *testing.T) {
	dir := videoDir(t, scriptYAML)

	_, err := build.Run(context.Background(), build.Options{
		Dir: dir,
		NewEngine: func(tts.EngineKind, string) (build.Engine, error) {
			return nil, nil
		},
	})
	if err == nil {
		t.Fatal("エンジンが nil なのに成功した")
	}
}

func TestRun_台本が見つからない(t *testing.T) {
	dir := t.TempDir()
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, log := factory(engine)

	_, err := build.Run(context.Background(), build.Options{Dir: dir, NewEngine: newEngine})
	if err == nil {
		t.Fatal("台本が無いのに成功した")
	}
	if len(*log) != 0 {
		t.Error("台本が無いのにエンジンへ繋ぎに行っている")
	}
}

func TestRun_台本が壊れていれば検証の報告が返る(t *testing.T) {
	// fps が数値でなく、必須の title も無い台本。
	dir := videoDir(t, `
meta:
  fps: "さんじゅう"
speakers:
  zundamon:
    styleId: 3
scenes:
  - image: assets/01.png
    lines:
      - speaker: zundamon
        text: セリフ
`)
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, _ := factory(engine)

	_, err := build.Run(context.Background(), build.Options{Dir: dir, NewEngine: newEngine})
	if err == nil {
		t.Fatal("台本が壊れているのに成功した")
	}
	// CLI が「検証の報告はそのまま出す」と判断できるよう、型が残っていること。
	var scriptErr *script.Error
	if !errors.As(err, &scriptErr) {
		t.Fatalf("台本の検証エラーとして返っていない: %v", err)
	}
	if len(scriptErr.Issues) == 0 {
		t.Error("問題の中身が入っていない")
	}
}

func TestRun_エンジンを作れなければ合成を始めない(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	wantErr := errors.New("baseUrl が不正です")

	_, err := build.Run(context.Background(), build.Options{
		Dir: dir,
		NewEngine: func(tts.EngineKind, string) (build.Engine, error) {
			return nil, wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Errorf("エンジンを作れなかった理由が失われている: %v", err)
	}
}

func TestRun_合成に失敗したらpropsjsonを書かない(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	engine := &fakeEngine{kind: tts.EngineVoicevox, synthErr: errors.New("エンジンが 500 を返しました")}
	newEngine, _ := factory(engine)

	_, err := build.Run(context.Background(), build.Options{Dir: dir, NewEngine: newEngine})
	if err == nil {
		t.Fatal("合成に失敗したのに成功した")
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".scenaremo", "props.json")); statErr == nil {
		t.Error("合成に失敗したのに props.json を書いている")
	}
}

func TestRun_進捗が通知される(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, _ := factory(engine)
	rep := &recordingReporter{}

	if _, err := build.Run(context.Background(), build.Options{
		Dir:       dir,
		NewEngine: newEngine,
		Reporter:  rep,
	}); err != nil {
		t.Fatalf("build に失敗した: %v", err)
	}

	if rep.total != 3 {
		t.Errorf("総数が伝わっていない: %d", rep.total)
	}
	if len(rep.started) != 3 || rep.started[0] != "zundamon: 一つ目のセリフ" {
		t.Errorf("合成中のセリフが伝わっていない: %v", rep.started)
	}
	if rep.doneSynthesized != 3 || rep.doneCached != 0 {
		t.Errorf("集計が伝わっていない: 合成 %d 件・キャッシュ %d 件", rep.doneSynthesized, rep.doneCached)
	}
}

func TestRun_打ち切られたら途中で止まる(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	engine := &fakeEngine{kind: tts.EngineVoicevox, onSynthesize: func(n int) {
		if n == 1 {
			cancel()
		}
	}}
	newEngine, _ := factory(engine)

	_, err := build.Run(ctx, build.Options{Dir: dir, NewEngine: newEngine})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("打ち切りとして返っていない: %v", err)
	}
	if len(engine.synthesized) != 1 {
		t.Errorf("打ち切ったのに合成を続けている: %d 件", len(engine.synthesized))
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".scenaremo", "props.json")); statErr == nil {
		t.Error("打ち切ったのに props.json を書いている")
	}
}

func TestRun_動画ディレクトリが指定されていない(t *testing.T) {
	_, err := build.Run(context.Background(), build.Options{})
	if err == nil {
		t.Fatal("ディレクトリが空なのに成功した")
	}
}

// recordingReporter は synth.Reporter への通知を記録する。
type recordingReporter struct {
	total           int
	started         []string
	done            []bool
	doneSynthesized int
	doneCached      int
}

func (r *recordingReporter) Start(total int) { r.total = total }

func (r *recordingReporter) LineStart(_ int, speaker, text string) {
	r.started = append(r.started, speaker+": "+text)
}

func (r *recordingReporter) LineDone(_ int, cached bool, _ time.Duration) {
	r.done = append(r.done, cached)
}

func (r *recordingReporter) Done(synthesized, cached int) {
	r.doneSynthesized, r.doneCached = synthesized, cached
}
