package props_test

import (
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

func baseInput() props.Input {
	return props.Input{
		Script:      baseScript(),
		Audio:       baseAudio(),
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
// gap は 300ms = 9 フレーム。シーン内のセリフ間にだけ入り、シーンの境目には入らない。
// シーン 2 へは 400ms = 12 フレームの繋ぎが入るので、その分だけ尺が伸びてセリフが後ろへ下がる。
func TestBuildTimeline(t *testing.T) {
	got := build(t, baseInput())

	// 繋ぎは前のシーンと重なって消えるので、総尺は喋りの尺の合計のまま。
	if got.Meta.DurationInFrames != 129 { // (60 + 9 + 30) + 30
		t.Errorf("Meta.DurationInFrames: got %d, want 129", got.Meta.DurationInFrames)
	}

	wantScenes := []int{
		99, // 60 + 9 (余白) + 30。先頭のシーンなので繋ぎは無い
		42, // 30 + 12 (繋ぎ)
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
	// 繋ぎが無い分シーンの尺は短くなり、セリフは先頭から鳴る。
	if withoutTransition.Scenes[1].DurationInFrames != 30 {
		t.Errorf("Scenes[1].DurationInFrames: got %d, want 30", withoutTransition.Scenes[1].DurationInFrames)
	}
	if withoutTransition.Scenes[1].Lines[0].StartFrame != 0 {
		t.Errorf("Scenes[1].Lines[0].StartFrame: got %d, want 0",
			withoutTransition.Scenes[1].Lines[0].StartFrame)
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
}
