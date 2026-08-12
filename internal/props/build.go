package props

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"time"

	"github.com/takiren/scenaremo/internal/script"
	"github.com/takiren/scenaremo/internal/timeline"
)

// DefaultTransitionMs はシーンを繋ぐのに掛ける時間の既定値（ミリ秒）。
//
// 台本にはまだこれを変えるノブが無い。長さを ms で指定する `transitionMs` を台本へ足すのは
// トランジションの実装（issue #10）と一緒に行う。フレームではなく ms なのは、
// フレームで書かせると fps を変えた瞬間に体感の速さが変わってしまうためで、
// ms → フレームの変換は CLI の仕事という層の分け方とも一致する。
const DefaultTransitionMs = 400

// resolutions はアスペクト比から解像度への対応表。
//
// この表を CLI 側に置くことで、renderer は props.json の width / height を読むだけで済む。
// 解像度の決め方が 2 箇所にあると、どちらが正なのか分からなくなる。
var resolutions = map[script.Aspect]struct{ width, height int }{
	script.Aspect16x9: {1920, 1080},
	script.Aspect9x16: {1080, 1920},
}

// LineAudio はセリフ1つ分の合成結果。
type LineAudio struct {
	// Path は props.json に載せる wav のパス。動画ディレクトリからの相対で / 区切り。
	Path string
	// Duration は wav の実測長。フレーム数はここから決まる。
	Duration time.Duration
}

// Input は props.json を組み立てるための材料。
type Input struct {
	// Script は検証を終えた台本。既定値は Build が埋めるので、埋まっていなくてもよい。
	Script *script.Script

	// Audio は各セリフの合成結果。Script.Scenes と同じ形（シーンの数、各シーンのセリフの数）であること。
	Audio [][]LineAudio

	// GeneratedBy は生成した scenaremo のバージョン。
	GeneratedBy string
}

// Build は台本と合成結果から props.json の内容を組み立てる。
//
// 音声合成そのものからは切り離してある。エンジンを起動しなくても、実測長を並べれば
// タイムラインとクレジットの組み立てだけを確かめられる。
func Build(in Input) (*Props, error) {
	s := in.Script
	if s == nil {
		return nil, errors.New("台本がありません")
	}
	if len(s.Scenes) == 0 {
		return nil, errors.New("シーンが1つもありません")
	}
	// この先は「省略されているかもしれない」を考えなくてよい状態にする。
	// Parse を通っていれば既に埋まっているが、冪等なので二度呼んでも変わらない。
	script.ApplyDefaults(s)

	size, ok := resolutions[s.Meta.Aspect]
	if !ok {
		return nil, fmt.Errorf("解像度を決められないアスペクト比です: %q", s.Meta.Aspect)
	}
	if len(in.Audio) != len(s.Scenes) {
		return nil, fmt.Errorf("音声の数がシーンの数と合いません: シーン %d 個に対して音声 %d 個",
			len(s.Scenes), len(in.Audio))
	}

	tlIn, err := timelineInput(s, in.Audio)
	if err != nil {
		return nil, err
	}
	tl := timeline.Calculate(tlIn)

	scenes, err := buildScenes(s, in.Audio, tl)
	if err != nil {
		return nil, err
	}

	return &Props{
		Version:     Version,
		GeneratedBy: in.GeneratedBy,
		Note:        Note,
		Meta: Meta{
			Title:            s.Meta.Title,
			Aspect:           string(s.Meta.Aspect),
			Width:            size.width,
			Height:           size.height,
			FPS:              s.Meta.FPS,
			DurationInFrames: tl.TotalFrames,
		},
		Scenes: scenes,
	}, nil
}

// timelineInput は台本と合成結果をタイムライン計算の入力へ直す。
// 音声の数と形がここで台本と合っているかを確かめる。
func timelineInput(s *script.Script, audio [][]LineAudio) (timeline.Input, error) {
	in := timeline.Input{
		FPS:    s.Meta.FPS,
		GapMs:  *s.Defaults.GapMs,
		Scenes: make([]timeline.SceneInput, 0, len(s.Scenes)),
	}

	for i, scene := range s.Scenes {
		// 喋らないシーンは尺が 0 になり、契約 (scenes[].durationInFrames >= 1) を満たせない。
		// 台本のスキーマも minItems: 1 で弾いているが、ここを通す道があると壊れた props.json が出る。
		if len(scene.Lines) == 0 {
			return timeline.Input{}, fmt.Errorf("scenes[%d]: セリフが1つもありません。シーンの尺は音声で決まるため、喋らないシーンは置けません", i)
		}
		if len(audio[i]) != len(scene.Lines) {
			return timeline.Input{}, fmt.Errorf("scenes[%d]: 音声の数がセリフの数と合いません: セリフ %d 個に対して音声 %d 個",
				i, len(scene.Lines), len(audio[i]))
		}

		lines := make([]timeline.LineInput, 0, len(audio[i]))
		for j, a := range audio[i] {
			if a.Duration <= 0 {
				return timeline.Input{}, fmt.Errorf("scenes[%d].lines[%d]: 音声の長さが 0 です。合成に失敗した wav ではありませんか", i, j)
			}
			lines = append(lines, timeline.LineInput{AudioDuration: a.Duration})
		}

		in.Scenes = append(in.Scenes, timeline.SceneInput{
			TransitionMs: transitionMs(scene.Transition),
			Lines:        lines,
		})
	}
	return in, nil
}

// transitionMs は台本のトランジション指定を時間へ直す。
func transitionMs(t script.Transition) int {
	if t == script.TransitionNone {
		return 0
	}
	return DefaultTransitionMs
}

// buildScenes は台本・合成結果・タイムラインを突き合わせて scenes を組み立てる。
func buildScenes(s *script.Script, audio [][]LineAudio, tl timeline.Timeline) ([]Scene, error) {
	scenes := make([]Scene, 0, len(s.Scenes))

	for i, scene := range s.Scenes {
		image, err := assetPath(scene.Image)
		if err != nil {
			return nil, fmt.Errorf("scenes[%d].image: %w", i, err)
		}

		lines := make([]Line, 0, len(scene.Lines))
		for j, line := range scene.Lines {
			audioPath, err := assetPath(audio[i][j].Path)
			if err != nil {
				return nil, fmt.Errorf("scenes[%d].lines[%d].audio: %w", i, j, err)
			}
			lines = append(lines, Line{
				Speaker:          line.Speaker,
				Text:             line.Text,
				Audio:            audioPath,
				StartFrame:       tl.Scenes[i].Lines[j].StartFrame,
				DurationInFrames: tl.Scenes[i].Lines[j].DurationFrames,
			})
		}

		scenes = append(scenes, Scene{
			Image:     image,
			Component: scene.Component,
			// Props は中身を見ずにそのまま渡す。何が正しいかを知っているのは
			// コンポーネント側だけなので、CLI が検証すると逃げ道を塞ぐことになる（→ issue #34）。
			Props:            scene.Props,
			DurationInFrames: tl.Scenes[i].DurationFrames,
			Transition: Transition{
				Type:             string(scene.Transition),
				DurationInFrames: tl.Scenes[i].TransitionFrames,
			},
			Lines: lines,
		})
	}
	return scenes, nil
}

// assetPath は台本や合成結果のパスを props.json に載せる形へ直す。
//
// 区切りを / に揃えるのは、Windows で作った props.json を Linux でレンダリングできるようにするため。
// 絶対パスを弾くのは、それが別のマシンでも CI でも eject 後のプロジェクトでも成り立たないため。
// 動画ディレクトリからの相対でありさえすれば、renderer 側がどうアセットを解決することにしても
// （--public-dir を差し替えても、public へコピーしても）そのまま通る（→ issue #12）。
func assetPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("パスが空です")
	}
	slashed := filepath.ToSlash(p)
	if path.IsAbs(slashed) || filepath.IsAbs(p) {
		return "", fmt.Errorf("パスが絶対パスです: %s (動画ディレクトリからの相対パスで書いてください。"+
			"props.json は別のマシンでも読めるように絶対パスを持ちません)", p)
	}
	return slashed, nil
}
