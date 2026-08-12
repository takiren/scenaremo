// Package timeline は、合成した音声の実測長からスライドショーのフレーム位置を確定させる。
//
// 台本にはフレーム数も秒数も書かないため（→ README「設計方針 2」）、動画の尺はここでの計算がすべて決める。
// 結果は props.json へそのまま写され、renderer の calculateMetadata がその総フレーム数を採用する。
//
// # 丸めの規約
//
// 秒からフレームへの変換は、音声も余白も一律で切り上げる。
//
//   - 切り上げるのは音の欠けを防ぐため。Remotion の Sequence は durationInFrames で音を打ち切るので、
//     切り捨てると 1 フレームに満たない末尾が毎回削られる。
//   - 余白は切っても音が欠けないので四捨五入でも構わないが、規則が 1 つで説明できるほうが契約として強い。
//     差は 1 フレーム分の無音（30fps で 33ms）にしかならない。
//
// 丸めた値は整数のまま積み上げ、途中で秒に戻さない。ここで生まれる誤差は「ズレ」ではなく
// 「無音がわずかに伸びる」形でしか出ない。各音声は自分の Sequence の先頭で鳴るため、
// 音と音の同期は構成上ずれようがなく、伸びるのは間だけだからである。
// props.json を受け取る側が秒に戻して計算し直すと、この丸めが再現できずに音がずれる。
//
// なお「余白ゼロ」は実際には無音ゼロではない。VOICEVOX の AudioQuery は既定で前後に 0.1 秒ずつ
// 無音を入れるため、各 wav にはその分が焼き込まれている（→ issue #44）。
//
// # 座標系
//
// 出力に動画先頭からの絶対位置は含まれない。シーンは「尺」を、セリフは「シーン先頭からの相対位置」を持つ。
//
// これは renderer が @remotion/transitions の TransitionSeries でシーンを並べるため。
// TransitionSeries は子シーケンスを前へ詰めて配置するので、絶対位置を渡しても意味を成さない。
// 絶対位置と TransitionSeries を混ぜると、props.json の数字が「実際に何フレーム目か」と食い違い、
// 字幕も口パクも音声も、各自が引き算をやり直さないと位置を知れなくなる。
// 相対に振り切れば、どの値もそれが置かれる Sequence の中でそのまま意味を持つ。
package timeline

import (
	"math"
	"time"
)

// Input はタイムライン計算の入力。
//
// 台本そのものではなく実測長を受け取るのは、この計算を音声合成から切り離すため。
// エンジンが動いていなくても、フレーム計算だけを試せるようにしてある。
type Input struct {
	// FPS はフレームレート。
	FPS int
	// GapMs は同じシーン内のセリフとセリフの間に入れる余白（ミリ秒）。
	//
	// シーンとシーンの間には今のところ余白を入れない（→ issue #44）。
	GapMs int
	// Scenes はシーンの並び。台本の scenes と同じ順序・同じ個数。
	Scenes []SceneInput
}

// SceneInput はシーン 1 つ分の入力。
type SceneInput struct {
	// Lines はこのシーンで喋るセリフの並び。
	Lines []LineInput
}

// LineInput はセリフ 1 つ分の入力。
type LineInput struct {
	// AudioDuration は合成された wav の実測長。
	AudioDuration time.Duration
}

// Timeline は計算結果。props.json の生成にそのまま使う。
type Timeline struct {
	// TotalFrames は動画全体の総フレーム数。
	//
	// クレジットシーンの分はここには含まれない（尺を持つかどうかは props 側の判断のため）。
	TotalFrames int
	// Scenes は各シーンの尺。入力と同じ順序・同じ個数。
	Scenes []SceneTimeline
}

// SceneTimeline はシーン 1 つの尺。
type SceneTimeline struct {
	// DurationFrames はこのシーンの尺。含まれる Lines と、その間の余白の合計。
	DurationFrames int

	// Lines はこのシーンのセリフの位置。
	Lines []LineTimeline
}

// LineTimeline はセリフ 1 つの位置。
type LineTimeline struct {
	// StartFrame は音声が鳴り始めるフレーム。シーンの先頭からの相対位置。
	StartFrame int
	// DurationFrames はこのセリフに与えるフレーム数。音声の実測長を切り上げた値。
	DurationFrames int
}

// Calculate は実測長から各シーンとセリフのフレーム位置を確定させる。
func Calculate(in Input) Timeline {
	gapFrames := framesFor(time.Duration(in.GapMs)*time.Millisecond, in.FPS)

	out := Timeline{Scenes: make([]SceneTimeline, 0, len(in.Scenes))}
	for _, scene := range in.Scenes {
		current := 0
		placed := make([]LineTimeline, 0, len(scene.Lines))
		for j, line := range scene.Lines {
			duration := framesFor(line.AudioDuration, in.FPS)
			placed = append(placed, LineTimeline{
				StartFrame:     current,
				DurationFrames: duration,
			})
			current += duration

			// 余白はセリフとセリフの間にだけ入れる。シーンの末尾に付けてしまうと、
			// シーンの尺に喋っていない時間が混ざり、シーンの境目がどこなのか分からなくなる。
			if j < len(scene.Lines)-1 {
				current += gapFrames
			}
		}

		out.Scenes = append(out.Scenes, SceneTimeline{
			DurationFrames: current,
			Lines:          placed,
		})
		out.TotalFrames += current
	}
	return out
}

// framesFor は時間をフレーム数へ直す。切り上げる理由はパッケージのコメントを参照。
func framesFor(d time.Duration, fps int) int {
	if d <= 0 || fps <= 0 {
		return 0
	}
	return int(math.Ceil(d.Seconds() * float64(fps)))
}
