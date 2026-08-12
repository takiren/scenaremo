package props_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/script"
)

func intPtr(v int) *int { return &v }

// baseScript は 2 シーン・3 セリフの台本を返す。個々のテストは必要な部分だけ書き換えて使う。
func baseScript() *script.Script {
	return &script.Script{
		Meta: script.Meta{Title: "テスト動画", Aspect: script.Aspect16x9, FPS: 30},
		Speakers: map[string]script.Speaker{
			"zundamon": {Engine: script.EngineVoicevox, StyleID: 3},
			"metan":    {Engine: script.EngineVoicevox, StyleID: 2},
		},
		Defaults: &script.Defaults{
			Speaker:    "zundamon",
			Transition: script.TransitionFade,
			GapMs:      intPtr(300),
			SceneGapMs: intPtr(500),
		},
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

// baseAudio は baseScript と同じ形の合成結果を返す。
func baseAudio() [][]props.LineAudio {
	return [][]props.LineAudio{
		{
			{Path: ".scenaremo/audio/aaa.wav", Duration: 2 * time.Second}, // 60 フレーム
			{Path: ".scenaremo/audio/bbb.wav", Duration: 1 * time.Second}, // 30 フレーム
		},
		{
			{Path: ".scenaremo/audio/ccc.wav", Duration: 1 * time.Second}, // 30 フレーム
		},
	}
}

func baseCredits() map[string]props.SpeakerCredit {
	return map[string]props.SpeakerCredit{
		"zundamon": {Name: "ずんだもん", UUID: "uuid-zundamon"},
		"metan":    {Name: "四国めたん", UUID: "uuid-metan"},
	}
}

func baseInput() props.Input {
	return props.Input{
		Script:      baseScript(),
		Audio:       baseAudio(),
		Credits:     baseCredits(),
		GeneratedBy: "scenaremo v0.0.0-test",
	}
}

func build(t *testing.T, in props.Input) *props.Props {
	t.Helper()
	got, err := props.Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return got
}

// TestBuildTimeline はフレーム位置が台本と実測長から決まることを確かめる。
//
// gapMs は 300ms = 9 フレームで、シーン内のセリフ間にだけ入る。
// sceneGapMs は 500ms = 15 フレームで、どのシーンの尻にも付く（最後のシーンは動画末尾の余韻になる）。
// シーン 2 へは 400ms = 12 フレームの繋ぎが入るので、その分だけ尺が伸びてセリフが後ろへ下がる。
func TestBuildTimeline(t *testing.T) {
	got := build(t, baseInput())

	// 繋ぎは前のシーンと重なって消えるので、総尺は喋りとシーン末尾の余白の合計のまま。
	if got.Meta.DurationInFrames != 159 { // (60 + 9 + 30 + 15) + (30 + 15)
		t.Errorf("Meta.DurationInFrames: got %d, want 159", got.Meta.DurationInFrames)
	}

	wantScenes := []int{
		114, // 60 + 9 (セリフ間) + 30 + 15 (シーン末尾)。先頭のシーンなので繋ぎは無い
		57,  // 30 + 15 (シーン末尾) + 12 (繋ぎ)
	}
	for i, want := range wantScenes {
		if got.Scenes[i].DurationInFrames != want {
			t.Errorf("Scenes[%d].DurationInFrames: got %d, want %d", i, got.Scenes[i].DurationInFrames, want)
		}
	}

	// セリフの位置はシーンの先頭からの相対。
	wantLines := []struct{ scene, line, start, duration int }{
		{0, 0, 0, 60},
		{0, 1, 69, 30}, // 60 + 9 (余白)
		{1, 0, 12, 30}, // 繋ぎの 12 フレームぶん後ろから
	}
	for _, want := range wantLines {
		line := got.Scenes[want.scene].Lines[want.line]
		if line.StartFrame != want.start || line.DurationInFrames != want.duration {
			t.Errorf("Scenes[%d].Lines[%d]: got start=%d duration=%d, want start=%d duration=%d",
				want.scene, want.line, line.StartFrame, line.DurationInFrames, want.start, want.duration)
		}
	}

	// TransitionSeries の尺の式と一致すること。
	sumDuration, sumTransition := 0, 0
	for _, scene := range got.Scenes {
		sumDuration += scene.DurationInFrames
		sumTransition += scene.Transition.DurationInFrames
	}
	if sumDuration-sumTransition != got.Meta.DurationInFrames {
		t.Errorf("尺の式が合わない: Σ尺 %d − Σ繋ぎ %d = %d, meta.durationInFrames は %d",
			sumDuration, sumTransition, sumDuration-sumTransition, got.Meta.DurationInFrames)
	}
}

// TestBuildTransition は繋ぎが終わったところで最初のセリフが鳴り始めること、
// および繋ぎを入れても総尺が変わらないことを確かめる。
func TestBuildTransition(t *testing.T) {
	got := build(t, baseInput())

	// 先頭のシーンは繋ぐ相手がいないので長さ 0。
	if first := got.Scenes[0].Transition; first.DurationInFrames != 0 {
		t.Errorf("Scenes[0].Transition.DurationInFrames: got %d, want 0", first.DurationInFrames)
	}

	second := got.Scenes[1].Transition
	if second.Type != "fade" {
		t.Errorf("Scenes[1].Transition.Type: got %q, want \"fade\"", second.Type)
	}
	if second.DurationInFrames != 12 { // 400ms × 30fps
		t.Errorf("Scenes[1].Transition.DurationInFrames: got %d, want 12", second.DurationInFrames)
	}
	// 繋ぎが終わった瞬間に最初のセリフが鳴り始める。
	if got.Scenes[1].Lines[0].StartFrame != second.DurationInFrames {
		t.Errorf("繋ぎ %d フレームに対して最初のセリフが %d フレーム目から",
			second.DurationInFrames, got.Scenes[1].Lines[0].StartFrame)
	}

	// 繋ぎがあってもなくても総尺は変わらない（重なった分だけシーンの尺が伸びるため）。
	none := baseInput()
	none.Script.Scenes[1].Transition = script.TransitionNone
	withoutTransition := build(t, none)

	if withoutTransition.Meta.DurationInFrames != got.Meta.DurationInFrames {
		t.Errorf("繋ぎの有無で総尺が変わった: %d != %d",
			withoutTransition.Meta.DurationInFrames, got.Meta.DurationInFrames)
	}
	if d := withoutTransition.Scenes[1].Transition.DurationInFrames; d != 0 {
		t.Errorf("transition: none なのに長さが %d ある", d)
	}
	// 繋ぎが無い分シーンの尺は短くなり、セリフは先頭から鳴る。末尾の余白 15 フレームは残る。
	if withoutTransition.Scenes[1].DurationInFrames != 45 {
		t.Errorf("Scenes[1].DurationInFrames: got %d, want 45", withoutTransition.Scenes[1].DurationInFrames)
	}
	if withoutTransition.Scenes[1].Lines[0].StartFrame != 0 {
		t.Errorf("Scenes[1].Lines[0].StartFrame: got %d, want 0",
			withoutTransition.Scenes[1].Lines[0].StartFrame)
	}
}

// TestBuildSceneGap は defaults.sceneGapMs がシーンの尻と動画の末尾へ効くことを確かめる（→ issue #44）。
//
// 0 のときの期待値は sceneGapMs を入れる前の実装が出していた値そのもの。
// 既存の台本の見え方を変えていないことを、ここで固定して見張る。
func TestBuildSceneGap(t *testing.T) {
	tests := []struct {
		name       string
		sceneGapMs int
		total      int
		scenes     []int
		starts     [][]int
	}{
		{
			// この issue より前の挙動。最後のセリフの終了と同時に動画が終わる。
			name:       "0 なら以前の挙動のまま",
			sceneGapMs: 0,
			total:      129, // (60 + 9 + 30) + 30
			scenes:     []int{99, 42},
			starts:     [][]int{{0, 69}, {12}},
		},
		{
			name:       "既定の 500ms",
			sceneGapMs: 500, // 15 フレーム
			total:      159,
			scenes:     []int{114, 57},
			starts:     [][]int{{0, 69}, {12}},
		},
		{
			// 余白を伸ばしてもセリフの位置（シーン先頭からの相対）は動かない。
			// 余白は次のシーンの頭ではなく前のシーンの尻に乗るため。
			name:       "1000ms",
			sceneGapMs: 1000, // 30 フレーム
			total:      189,
			scenes:     []int{129, 72},
			starts:     [][]int{{0, 69}, {12}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := baseInput()
			in.Script.Defaults.SceneGapMs = intPtr(tt.sceneGapMs)
			got := build(t, in)

			if got.Meta.DurationInFrames != tt.total {
				t.Errorf("Meta.DurationInFrames: got %d, want %d", got.Meta.DurationInFrames, tt.total)
			}
			for i, want := range tt.scenes {
				if got.Scenes[i].DurationInFrames != want {
					t.Errorf("Scenes[%d].DurationInFrames: got %d, want %d",
						i, got.Scenes[i].DurationInFrames, want)
				}
				for j, wantStart := range tt.starts[i] {
					if start := got.Scenes[i].Lines[j].StartFrame; start != wantStart {
						t.Errorf("Scenes[%d].Lines[%d].StartFrame: got %d, want %d", i, j, start, wantStart)
					}
				}
			}

			// 動画の末尾: 最後のセリフが終わってから、余白のぶんだけ残っている。
			last := got.Scenes[len(got.Scenes)-1]
			lastLine := last.Lines[len(last.Lines)-1]
			gapFrames := (tt.sceneGapMs*got.Meta.FPS + 999) / 1000
			if rest := last.DurationInFrames - (lastLine.StartFrame + lastLine.DurationInFrames); rest != gapFrames {
				t.Errorf("動画末尾の余白: got %d, want %d", rest, gapFrames)
			}

			// 余白を足しても TransitionSeries の尺の式は崩れない。ここが崩れると音がずれる。
			sumDuration, sumTransition := 0, 0
			for _, scene := range got.Scenes {
				sumDuration += scene.DurationInFrames
				sumTransition += scene.Transition.DurationInFrames
			}
			if sumDuration-sumTransition != got.Meta.DurationInFrames {
				t.Errorf("尺の式が合わない: Σ尺 %d − Σ繋ぎ %d = %d, meta.durationInFrames は %d",
					sumDuration, sumTransition, sumDuration-sumTransition, got.Meta.DurationInFrames)
			}
		})
	}
}

// TestBuildResolution はアスペクト比が具体的な解像度へ解決されることを確かめる。
// renderer 側に解像度の対応表を持たせないための取り決め。
func TestBuildResolution(t *testing.T) {
	tests := []struct {
		aspect        script.Aspect
		width, height int
	}{
		{script.Aspect16x9, 1920, 1080},
		{script.Aspect9x16, 1080, 1920},
	}

	for _, tt := range tests {
		t.Run(string(tt.aspect), func(t *testing.T) {
			in := baseInput()
			in.Script.Meta.Aspect = tt.aspect
			got := build(t, in)

			if got.Meta.Width != tt.width || got.Meta.Height != tt.height {
				t.Errorf("解像度: got %dx%d, want %dx%d", got.Meta.Width, got.Meta.Height, tt.width, tt.height)
			}
			if got.Meta.Aspect != string(tt.aspect) {
				t.Errorf("Meta.Aspect: got %q, want %q", got.Meta.Aspect, tt.aspect)
			}
		})
	}
}

// TestBuildCredits はクレジットがキャラクター単位で集計されることを確かめる。
func TestBuildCredits(t *testing.T) {
	in := baseInput()
	// 同じ話者の別スタイルを足す。規約が求めるのはキャラクター単位の表記なので、
	// クレジットは増えず、使ったスタイル ID だけが増えるはず。
	in.Script.Speakers["zundamon_ama"] = script.Speaker{Engine: script.EngineVoicevox, StyleID: 1}
	in.Credits["zundamon_ama"] = props.SpeakerCredit{Name: "ずんだもん", UUID: "uuid-zundamon"}
	in.Script.Scenes[1].Lines = append(in.Script.Scenes[1].Lines,
		script.Line{Speaker: "zundamon_ama", Text: "4つめ"})
	in.Audio[1] = append(in.Audio[1], props.LineAudio{Path: ".scenaremo/audio/ddd.wav", Duration: time.Second})

	got := build(t, in)

	if len(got.Credits.Entries) != 2 {
		t.Fatalf("クレジットの件数: got %d, want 2 (%+v)", len(got.Credits.Entries), got.Credits.Entries)
	}

	// 並びは台本での登場順。ずんだもんが先に喋る。
	first := got.Credits.Entries[0]
	if first.Text != "VOICEVOX:ずんだもん" {
		t.Errorf("Entries[0].Text: got %q, want \"VOICEVOX:ずんだもん\"", first.Text)
	}
	if len(first.StyleIDs) != 2 || first.StyleIDs[0] != 1 || first.StyleIDs[1] != 3 {
		t.Errorf("Entries[0].StyleIDs: got %v, want [1 3]", first.StyleIDs)
	}
	if got.Credits.Entries[1].Text != "VOICEVOX:四国めたん" {
		t.Errorf("Entries[1].Text: got %q, want \"VOICEVOX:四国めたん\"", got.Credits.Entries[1].Text)
	}

	// クレジットシーンはまだ尺を持たない (issue #17)。
	if got.Credits.DurationInFrames != 0 {
		t.Errorf("Credits.DurationInFrames: got %d, want 0", got.Credits.DurationInFrames)
	}
}

// TestBuildPassesThroughComponent はシーンコンポーネントの指定と props が
// 検証されずそのまま透過することを確かめる（→ issue #34）。
func TestBuildPassesThroughComponent(t *testing.T) {
	in := baseInput()
	in.Script.Scenes[0].Component = "zoom"
	in.Script.Scenes[0].Props = map[string]any{"focus": []any{0.3, 0.6}}

	got := build(t, in)

	if got.Scenes[0].Component != "zoom" {
		t.Errorf("Scenes[0].Component: got %q, want \"zoom\"", got.Scenes[0].Component)
	}
	if _, ok := got.Scenes[0].Props["focus"]; !ok {
		t.Errorf("Scenes[0].Props が透過していない: %+v", got.Scenes[0].Props)
	}
	// 指定が無いシーンには既定のコンポーネント名が入る。
	if got.Scenes[1].Component != script.DefaultComponent {
		t.Errorf("Scenes[1].Component: got %q, want %q", got.Scenes[1].Component, script.DefaultComponent)
	}
}

// TestBuildErrors は props.json を壊すより先に止まるべき入力を確かめる。
func TestBuildErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(in *props.Input)
		wantMsg string
	}{
		{
			name:    "台本が無い",
			mutate:  func(in *props.Input) { in.Script = nil },
			wantMsg: "台本がありません",
		},
		{
			name:    "シーンが無い",
			mutate:  func(in *props.Input) { in.Script.Scenes = nil },
			wantMsg: "シーンが1つもありません",
		},
		{
			name:    "音声の数がシーンと合わない",
			mutate:  func(in *props.Input) { in.Audio = in.Audio[:1] },
			wantMsg: "音声の数がシーンの数と合いません",
		},
		{
			name:    "音声の数がセリフと合わない",
			mutate:  func(in *props.Input) { in.Audio[0] = in.Audio[0][:1] },
			wantMsg: "音声の数がセリフの数と合いません",
		},
		{
			// 喋らないシーンは尺が 0 になり、契約 (durationInFrames >= 1) を満たせない。
			name: "セリフが1つも無いシーン",
			mutate: func(in *props.Input) {
				in.Script.Scenes[1].Lines = nil
				in.Audio[1] = nil
			},
			wantMsg: "セリフが1つもありません",
		},
		{
			name:    "音声の長さが0",
			mutate:  func(in *props.Input) { in.Audio[0][0].Duration = 0 },
			wantMsg: "音声の長さが 0 です",
		},
		{
			name:    "アスペクト比が未知",
			mutate:  func(in *props.Input) { in.Script.Meta.Aspect = script.Aspect("4:3") },
			wantMsg: "解像度を決められないアスペクト比です",
		},
		{
			// 絶対パスは別のマシンでも CI でも成り立たない。props.json に載せる前に止める。
			name:    "画像が絶対パス",
			mutate:  func(in *props.Input) { in.Script.Scenes[0].Image = "/tmp/01.png" },
			wantMsg: "絶対パスです",
		},
		{
			name:    "音声のパスが空",
			mutate:  func(in *props.Input) { in.Audio[0][0].Path = "" },
			wantMsg: "パスが空です",
		},
		{
			// 黙って飛ばすとクレジットが1件足りない props.json ができる。表記漏れは利用者の事故。
			name:    "クレジット情報が欠けている",
			mutate:  func(in *props.Input) { delete(in.Credits, "metan") },
			wantMsg: "クレジット情報がありません",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := baseInput()
			tt.mutate(&in)

			got, err := props.Build(in)
			if err == nil {
				t.Fatalf("エラーになるはずが成功した: %+v", got)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("エラーメッセージ: got %q, want %q を含む", err.Error(), tt.wantMsg)
			}
		})
	}
}

// TestBuildSeparator は props.json のパスが常に / 区切りになることを確かめる。
// props.json を作ったマシンと動画をレンダリングするマシンが違っても読めるようにするため。
//
// 変換は filepath.ToSlash に任せる。Windows では \ が / へ直り、それ以外の OS では何も起きない。
// OS に依らず \ を潰してしまうと、\ を含む正当なファイル名を壊すことになる。
func TestBuildSeparator(t *testing.T) {
	in := baseInput()
	in.Script.Scenes[0].Image = filepath.Join("assets", "sub", "01.png")

	got := build(t, in)

	if want := "assets/sub/01.png"; got.Scenes[0].Image != want {
		t.Errorf("Scenes[0].Image: got %q, want %q", got.Scenes[0].Image, want)
	}
	for i, scene := range got.Scenes {
		for j, line := range scene.Lines {
			if strings.Contains(line.Audio, string(filepath.Separator)) && filepath.Separator != '/' {
				t.Errorf("Scenes[%d].Lines[%d].Audio に OS 固有の区切りが残っている: %q", i, j, line.Audio)
			}
		}
	}
}

// TestBuildFillsDefaults は既定値が埋まっていない台本でも Build が自分で埋めることを確かめる。
// Parse を通せば埋まっているが、そこに依存すると手で組んだ台本で静かに壊れる。
func TestBuildFillsDefaults(t *testing.T) {
	in := baseInput()
	in.Script.Meta.Aspect = ""
	in.Script.Meta.FPS = 0
	in.Script.Defaults = &script.Defaults{Speaker: "zundamon"}

	got := build(t, in)

	if got.Meta.FPS != script.DefaultFPS {
		t.Errorf("Meta.FPS: got %d, want %d", got.Meta.FPS, script.DefaultFPS)
	}
	if got.Meta.Aspect != string(script.DefaultAspect) {
		t.Errorf("Meta.Aspect: got %q, want %q", got.Meta.Aspect, script.DefaultAspect)
	}
	// 話者を省略したセリフには defaults.speaker が入る。字幕の話者名表示に使える。
	if got.Scenes[0].Lines[0].Speaker != "zundamon" {
		t.Errorf("Scenes[0].Lines[0].Speaker: got %q, want \"zundamon\"", got.Scenes[0].Lines[0].Speaker)
	}
	// gapMs / sceneGapMs も既定値で埋まる。埋め忘れると余白が消えて詰まった動画になる。
	if got.Meta.DurationInFrames != 159 { // gapMs 300 (9) と sceneGapMs 500 (15) が効いた尺
		t.Errorf("Meta.DurationInFrames: got %d, want 159 (既定の余白が入った尺)", got.Meta.DurationInFrames)
	}
}
