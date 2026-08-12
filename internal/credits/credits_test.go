package credits_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/takiren/scenaremo/internal/credits"
	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/script"
	"github.com/takiren/scenaremo/internal/tts"
)

// fakeLister は固定の話者一覧を返す偽のエンジン。
//
// 実物の VOICEVOX に触るとテストがエンジンの導入とバージョンに依存してしまい、
// 「どの話者が返ってくるか」を前提に置けなくなる。呼び出し回数を数えるのは、
// 問い合わせを 1 回にまとめている（Resolve の要）ことを外から確かめるため。
type fakeLister struct {
	speakers []tts.Speaker
	err      error
	calls    int
}

func (f *fakeLister) Speakers(context.Context) ([]tts.Speaker, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.speakers, nil
}

// voicevoxSpeakers は VOICEVOX の /speakers が返す形を模した話者一覧。
// UUID は実物の値を使っている（クレジットの見本 examples/minimal と突き合わせられるようにするため）。
func voicevoxSpeakers() []tts.Speaker {
	return []tts.Speaker{
		{
			Name:        "四国めたん",
			SpeakerUUID: "7ffcb7ce-00ec-4bdc-82cd-45a8889e43ff",
			Styles: []tts.Style{
				{Name: "ノーマル", ID: 2, Type: "talk"},
				{Name: "あまあま", ID: 0, Type: "talk"},
			},
		},
		{
			Name:        "ずんだもん",
			SpeakerUUID: "388f246b-8c41-4ac1-8e2d-5d79f3ff56d9",
			Styles: []tts.Style{
				{Name: "ノーマル", ID: 3, Type: "talk"},
				{Name: "あまあま", ID: 1, Type: "talk"},
			},
		},
	}
}

// baseScript は 2 話者・2 シーンの台本を返す。個々のテストは必要な部分だけ書き換えて使う。
func baseScript() *script.Script {
	return &script.Script{
		Meta: script.Meta{Title: "テスト動画"},
		Speakers: map[string]script.Speaker{
			"zundamon": {Engine: script.EngineVoicevox, StyleID: 3},
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
				Lines: []script.Line{{Text: "3つめ"}},
			},
		},
	}
}

// voicevoxOnly は VOICEVOX だけを引ける対応表と、その偽エンジンを返す。
func voicevoxOnly() (credits.Listers, *fakeLister) {
	lister := &fakeLister{speakers: voicevoxSpeakers()}
	return credits.Listers{tts.EngineVoicevox: lister}, lister
}

// fakeResolver は Lister と error をそのまま返す偽の対応表。
// Listers では作れない状態（実装が nil を返す）を試すために使う。
type fakeResolver struct {
	lister credits.Lister
	err    error
}

func (f fakeResolver) Lister(tts.EngineKind) (credits.Lister, error) {
	return f.lister, f.err
}

// wantContains はメッセージに必要な言葉が入っていることを確かめる。
//
// エラーの型ではなく文面を見るのは、このパッケージのエラーが開発者以外にも届くためである。
// 「何が起きたか」と「次に何をすればよいか」の両方が残っているかを、ここで固定しておく。
func wantContains(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("メッセージに %q が含まれていない: %s", w, got)
		}
	}
}

func TestResolve_話者エイリアスごとに名前とUUIDを返す(t *testing.T) {
	listers, _ := voicevoxOnly()

	got, err := credits.Resolve(context.Background(), baseScript(), listers)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := map[string]props.SpeakerCredit{
		"zundamon": {Name: "ずんだもん", UUID: "388f246b-8c41-4ac1-8e2d-5d79f3ff56d9"},
		"metan":    {Name: "四国めたん", UUID: "7ffcb7ce-00ec-4bdc-82cd-45a8889e43ff"},
	}
	if len(got) != len(want) {
		t.Fatalf("解決した話者の数が違う: got %d, want %d (%+v)", len(got), len(want), got)
	}
	for alias, w := range want {
		if got[alias] != w {
			t.Errorf("%s: got %+v, want %+v", alias, got[alias], w)
		}
	}
}

func TestResolve_同じ話者の別スタイルはどちらも解決できる(t *testing.T) {
	s := baseScript()
	// 同じ「ずんだもん」のノーマル (3) とあまあま (1) を別のエイリアスで使う。
	s.Speakers["zundamon_ama"] = script.Speaker{Engine: script.EngineVoicevox, StyleID: 1}
	s.Scenes[1].Lines = []script.Line{{Speaker: "zundamon_ama", Text: "3つめ"}}

	listers, _ := voicevoxOnly()
	got, err := credits.Resolve(context.Background(), s, listers)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	for _, alias := range []string{"zundamon", "zundamon_ama"} {
		if got[alias].Name != "ずんだもん" {
			t.Errorf("%s: got %+v, want ずんだもん", alias, got[alias])
		}
	}
}

func TestResolve_省略された既定値は自分で埋める(t *testing.T) {
	// Parse を通していない台本。engine も defaults も書かれていないが、
	// 台本に書かなくてよいものを書いていないだけなので、これで解決できなければならない。
	s := &script.Script{
		Meta:     script.Meta{Title: "テスト動画"},
		Speakers: map[string]script.Speaker{"zundamon": {StyleID: 3}},
		Scenes: []script.Scene{{
			Image: "assets/01.png",
			Lines: []script.Line{{Speaker: "zundamon", Text: "1つめ"}},
		}},
	}

	listers, _ := voicevoxOnly()
	got, err := credits.Resolve(context.Background(), s, listers)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got["zundamon"].Name != "ずんだもん" {
		t.Errorf("engine を省いた話者を解決できていない: %+v", got)
	}
}

func TestResolve_台本で使われていない話者は解決しない(t *testing.T) {
	s := baseScript()
	// 定義だけあって使わない話者。存在しない styleId でも、問い合わせ先の無いエンジンでも、
	// クレジットに載らない以上そのために失敗する理由はない。
	s.Speakers["unused"] = script.Speaker{Engine: script.Engine("unknown-engine"), StyleID: 999}

	listers, _ := voicevoxOnly()
	got, err := credits.Resolve(context.Background(), s, listers)
	if err != nil {
		t.Fatalf("使っていない話者のせいで失敗した: %v", err)
	}
	if _, ok := got["unused"]; ok {
		t.Errorf("使っていない話者まで解決している: %+v", got)
	}
}

func TestResolve_同じエンジンへの問い合わせは1回だけ(t *testing.T) {
	listers, lister := voicevoxOnly()

	if _, err := credits.Resolve(context.Background(), baseScript(), listers); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// baseScript は 3 セリフ・2 話者だが、エンジンは 1 つなので 1 回で足りる。
	if lister.calls != 1 {
		t.Errorf("Speakers の呼び出し回数: got %d, want 1", lister.calls)
	}
}

func TestResolve_エンジンが複数あってもそれぞれ1回だけ問い合わせる(t *testing.T) {
	s := baseScript()
	s.Speakers["metan"] = script.Speaker{Engine: script.Engine(tts.EngineCoeiroink), StyleID: 2}

	voicevox := &fakeLister{speakers: voicevoxSpeakers()}
	// styleId 2 はどちらのエンジンにもある。話者を引く先が台本の engine で決まっていないと、
	// ここで四国めたんが返ってしまう。
	coeiroink := &fakeLister{speakers: []tts.Speaker{{
		Name:        "つくよみちゃん",
		SpeakerUUID: "uuid-tsukuyomi",
		Styles:      []tts.Style{{Name: "れいせい", ID: 2}},
	}}}
	listers := credits.Listers{
		tts.EngineVoicevox:  voicevox,
		tts.EngineCoeiroink: coeiroink,
	}

	got, err := credits.Resolve(context.Background(), s, listers)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if got["zundamon"].Name != "ずんだもん" {
		t.Errorf("zundamon: got %+v, want ずんだもん", got["zundamon"])
	}
	if got["metan"].Name != "つくよみちゃん" {
		t.Errorf("metan: got %+v, want つくよみちゃん", got["metan"])
	}
	if voicevox.calls != 1 || coeiroink.calls != 1 {
		t.Errorf("Speakers の呼び出し回数: voicevox %d 回 / coeiroink %d 回, want どちらも 1 回",
			voicevox.calls, coeiroink.calls)
	}
}

func TestResolve_styleIdに当たる話者がいない(t *testing.T) {
	s := baseScript()
	s.Speakers["zundamon"] = script.Speaker{Engine: script.EngineVoicevox, StyleID: 999}

	listers, _ := voicevoxOnly()
	_, err := credits.Resolve(context.Background(), s, listers)
	if err == nil {
		t.Fatal("存在しない styleId なのに成功した")
	}
	wantContains(t, err.Error(), "zundamon", "999", "scenaremo speakers")
}

func TestResolve_話者の名前が取れない(t *testing.T) {
	// 名前の無いクレジットは props.Build が受け付けない。ここで気づかないと、
	// 「話者は見つかったのに props.json が作れない」という遠い場所での失敗になる。
	listers := credits.Listers{tts.EngineVoicevox: &fakeLister{speakers: []tts.Speaker{
		{SpeakerUUID: "uuid-only", Styles: []tts.Style{{ID: 3}}},
	}}}

	s := baseScript()
	s.Scenes = s.Scenes[:1]
	s.Scenes[0].Lines = s.Scenes[0].Lines[:1] // 既定の話者 zundamon (styleId 3) だけを使う

	_, err := credits.Resolve(context.Background(), s, listers)
	if err == nil {
		t.Fatal("名前が空なのに成功した")
	}
	wantContains(t, err.Error(), "名前")
}

func TestResolve_問い合わせ先の無いエンジン(t *testing.T) {
	lister := &fakeLister{speakers: voicevoxSpeakers()}

	tests := []struct {
		name     string
		resolver credits.ListerResolver
		contains []string
	}{
		{"対応表が空", credits.Listers{}, []string{"VOICEVOX", "speakers[].engine"}},
		{"別の種別しかない", credits.Listers{tts.EngineCoeiroink: lister}, []string{"VOICEVOX", "coeiroink"}},
		{"値が nil", credits.Listers{tts.EngineVoicevox: nil}, []string{"VOICEVOX"}},
		{"対応表そのものが nil", credits.Listers(nil), []string{"VOICEVOX"}},
		{"実装が nil を返す", fakeResolver{}, []string{"VOICEVOX"}},
		{"実装がエラーを返す", fakeResolver{err: errors.New("baseUrl が壊れています")}, []string{"baseUrl が壊れています"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := credits.Resolve(context.Background(), baseScript(), tt.resolver)
			if err == nil {
				t.Fatal("エンジンを引けないのに成功した")
			}
			wantContains(t, err.Error(), tt.contains...)
		})
	}
}

func TestResolve_話者一覧の取得に失敗したら理由をたどれる(t *testing.T) {
	// エンジン未起動のときの案内は tts 側が持っている。ここで握り潰すと利用者が次の一手を失う。
	refused := errors.New("dial tcp 127.0.0.1:50021: connect: connection refused")
	engineErr := &tts.EngineUnavailableError{
		Kind:    tts.EngineVoicevox,
		BaseURL: "http://127.0.0.1:50021",
		Err:     refused,
	}
	listers := credits.Listers{tts.EngineVoicevox: &fakeLister{err: engineErr}}

	_, err := credits.Resolve(context.Background(), baseScript(), listers)
	if err == nil {
		t.Fatal("取得に失敗したのに成功した")
	}

	var unavailable *tts.EngineUnavailableError
	if !errors.As(err, &unavailable) {
		t.Errorf("EngineUnavailableError をたどれない: %v", err)
	}
	if !errors.Is(err, refused) {
		t.Errorf("元の原因をたどれない: %v", err)
	}
	// 何をしようとして失敗したのかと、エンジン側の案内の両方が残っていること。
	wantContains(t, err.Error(), "話者一覧", "エンジンを起動")
}

func TestResolve_未定義の話者エイリアス(t *testing.T) {
	s := baseScript()
	s.Scenes[1].Lines[0].Speaker = "unknown"

	listers, _ := voicevoxOnly()
	_, err := credits.Resolve(context.Background(), s, listers)
	if err == nil {
		t.Fatal("未定義の話者なのに成功した")
	}
	wantContains(t, err.Error(), "scenes[1].lines[0]", "unknown", "speakers")
}

func TestResolve_話者が決まらないセリフ(t *testing.T) {
	s := baseScript()
	s.Defaults = &script.Defaults{} // 既定の話者が無いので、speaker を省いたセリフは決まらない

	listers, _ := voicevoxOnly()
	_, err := credits.Resolve(context.Background(), s, listers)
	if err == nil {
		t.Fatal("話者が決まらないのに成功した")
	}
	wantContains(t, err.Error(), "scenes[0].lines[0]", "defaults.speaker")
}

func TestResolve_nil引数でも落ちない(t *testing.T) {
	listers, _ := voicevoxOnly()

	t.Run("台本が nil", func(t *testing.T) {
		_, err := credits.Resolve(context.Background(), nil, listers)
		if err == nil {
			t.Fatal("台本が nil なのに成功した")
		}
		wantContains(t, err.Error(), "台本")
	})

	t.Run("問い合わせ先が nil", func(t *testing.T) {
		_, err := credits.Resolve(context.Background(), baseScript(), nil)
		if err == nil {
			t.Fatal("問い合わせ先が nil なのに成功した")
		}
		wantContains(t, err.Error(), "問い合わせ先")
	})
}

func TestResolve_打ち切られたら問い合わせない(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	listers, lister := voicevoxOnly()
	_, err := credits.Resolve(ctx, baseScript(), listers)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("打ち切りをたどれない: %v", err)
	}
	// 偽のエンジンは ctx を見ない。Resolve 自身が打ち切りを確かめていないとここが 1 になる。
	if lister.calls != 0 {
		t.Errorf("打ち切られているのに問い合わせた: %d 回", lister.calls)
	}
}

// TestResolve_解決結果からクレジットが組み上がる は Resolve の戻り値が
// props.Build の入力としてそのまま通ることを確かめる。
//
// このパッケージが存在する理由は props.Build に材料を渡すことだけなので、
// 両者の受け渡しが噛み合っているかは片方だけを見ていても分からない。
func TestResolve_解決結果からクレジットが組み上がる(t *testing.T) {
	s := baseScript()
	// 同じ「ずんだもん」を別スタイルでも使う。規約が求めるのは音声ライブラリ単位の表記なので、
	// クレジットは 1 件にまとまり、styleIds だけが増える。
	s.Speakers["zundamon_ama"] = script.Speaker{Engine: script.EngineVoicevox, StyleID: 1}
	s.Scenes[1].Lines = []script.Line{{Speaker: "zundamon_ama", Text: "3つめ"}}

	listers, _ := voicevoxOnly()
	resolved, err := credits.Resolve(context.Background(), s, listers)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	got, err := props.Build(props.Input{
		Script: s,
		Audio: [][]props.LineAudio{
			{
				{Path: ".scenaremo/audio/aaa.wav", Duration: 2 * time.Second},
				{Path: ".scenaremo/audio/bbb.wav", Duration: time.Second},
			},
			{
				{Path: ".scenaremo/audio/ccc.wav", Duration: time.Second},
			},
		},
		Credits:     resolved,
		GeneratedBy: "scenaremo v0.0.0-test",
	})
	if err != nil {
		t.Fatalf("props.Build: %v", err)
	}

	want := []props.Entry{
		{
			Engine:      "voicevox",
			SpeakerName: "ずんだもん",
			SpeakerUUID: "388f246b-8c41-4ac1-8e2d-5d79f3ff56d9",
			StyleIDs:    []int{1, 3},
			Text:        "VOICEVOX:ずんだもん",
		},
		{
			Engine:      "voicevox",
			SpeakerName: "四国めたん",
			SpeakerUUID: "7ffcb7ce-00ec-4bdc-82cd-45a8889e43ff",
			StyleIDs:    []int{2},
			Text:        "VOICEVOX:四国めたん",
		},
	}
	if !reflect.DeepEqual(got.Credits.Entries, want) {
		t.Errorf("Credits.Entries:\n got %+v\nwant %+v", got.Credits.Entries, want)
	}
}
