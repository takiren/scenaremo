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
// 実効の余白は指定値 + 200ms になり、0 を指定しても間は消えない。
//
// # 余白の置き場所
//
// 余白は 2 種類ある。セリフとセリフの間 (GapMs) と、シーンの末尾 (SceneGapMs) である。
//
// シーンの境目の余白は「次のシーンの頭」ではなく「前のシーンの尻」に付ける。
// 繋ぎは次のシーンの先頭で行われ、TransitionSeries はそのぶんを前のシーンの末尾に重ねるので、
// 余白を尻に置いておくと繋ぎが無音の中で完結する。頭に置くと繋ぎは結局前のシーンの語尾に被り、
// 「無音の間に絵が切り替わる」という一番きれいな形にならない（→ issue #44）。
// 尻に置くほうが「最後の一言を言い終えた絵をしばらく見せる」という見え方にもなる。
//
// 末尾の余白は最後のシーンにも同じように付ける。「シーンとシーンの間」と「動画の末尾」は
// どちらも『喋り終わってからシーンが終わるまで』であって、別の値にする理由が無いためである。
// おかげで最後のセリフの終了と同時に動画が切れることもなくなる（→ issue #44）。
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
	GapMs int
	// SceneGapMs はシーンの末尾に入れる余白（ミリ秒）。
	//
	// シーンとシーンの間の間（ま）と、動画末尾の余韻の両方がこの値で決まる。
	// なぜ前のシーンの尻に付けるのかはパッケージのコメント（余白の置き場所）を参照。
	SceneGapMs int
	// Scenes はシーンの並び。台本の scenes と同じ順序・同じ個数。
	Scenes []SceneInput
}

// SceneInput はシーン 1 つ分の入力。
type SceneInput struct {
	// TransitionMs は前のシーンからこのシーンへ繋ぐのに掛ける時間（ミリ秒）。
	// 0 なら繋ぎの演出を入れない。先頭のシーンは繋ぐ相手がいないため、値によらず 0 として扱う。
	TransitionMs int
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
	// TransitionSeries の尺の式（各シーケンスの尺の合計 − トランジションの尺の合計）と一致する。
	// トランジションは前のシーンの尻を食うので、繋ぎを増やしても総尺は変わらず、
	// 喋りとシーン末尾の余白の合計そのものになる。
	// クレジットシーンの分はここには含まれない（尺を持つかどうかは props 側の判断のため）。
	TotalFrames int
	// Scenes は各シーンの尺。入力と同じ順序・同じ個数。
	Scenes []SceneTimeline
}

// SceneTimeline はシーン 1 つの尺。
type SceneTimeline struct {
	// DurationFrames は TransitionSeries.Sequence へ渡す尺。
	//
	// 喋りの尺そのものではなく、そこへ末尾の余白 (Input.SceneGapMs) と TransitionFrames を足した値になる。
	// TransitionSeries は隣り合うシーケンスを繋ぎのぶん重ねて詰めるので、
	// 重なる分をあらかじめ申告しておかないと、シーンが繋ぎのぶんだけ前へずれてしまう。
	DurationFrames int

	// TransitionFrames は前のシーンからこのシーンへの繋ぎに掛けるフレーム数。0 なら繋ぎ無し。
	//
	// この繋ぎはシーケンスの先頭 TransitionFrames フレームで行われ、
	// ちょうど終わったところで最初のセリフが鳴り始める（Lines[0].StartFrame と一致する）。
	// 次の声が鳴り始めた時点で新しい画像が出揃っている状態にするための置き方で、
	// 逆にすると新しい声が喋っている間まだ前の画像が透けていて、同期ずれとして見える。
	TransitionFrames int

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
	sceneGapFrames := framesFor(time.Duration(in.SceneGapMs)*time.Millisecond, in.FPS)

	// まず各シーンについて「繋ぎを除いた尺」と、その中でのセリフの位置を出す。
	// 繋ぎの分をずらすのは、隣のシーンの尺が分かってからでないと決められない。
	body := make([]int, len(in.Scenes))
	lines := make([][]LineTimeline, len(in.Scenes))
	for i, scene := range in.Scenes {
		current := 0
		placed := make([]LineTimeline, 0, len(scene.Lines))
		for j, line := range scene.Lines {
			duration := framesFor(line.AudioDuration, in.FPS)
			placed = append(placed, LineTimeline{
				StartFrame:     current,
				DurationFrames: duration,
			})
			current += duration

			// セリフ間の余白は文の切れ目のためのもので、シーンの切れ目には SceneGapMs を使う。
			// 末尾にも入れてしまうと、話題の切れ目に 2 種類の余白が積み上がって効きが読めなくなる。
			if j < len(scene.Lines)-1 {
				current += gapFrames
			}
		}
		// シーンの末尾に余白を付ける。これがシーンとシーンの間の間（ま）になり、
		// 最後のシーンではそのまま動画末尾の余韻になる（→ パッケージのコメント「余白の置き場所」）。
		body[i] = current + sceneGapFrames
		lines[i] = placed
	}

	out := Timeline{Scenes: make([]SceneTimeline, 0, len(in.Scenes))}
	for i, scene := range in.Scenes {
		transition := transitionFrames(scene, i, body, in.FPS)

		// 繋ぎはシーケンスの先頭で行われるので、セリフはその分だけ後ろへ下がる。
		// こうすると繋ぎが終わった瞬間に最初のセリフが鳴り始める。
		for j := range lines[i] {
			lines[i][j].StartFrame += transition
		}

		out.Scenes = append(out.Scenes, SceneTimeline{
			DurationFrames:   body[i] + transition,
			TransitionFrames: transition,
			Lines:            lines[i],
		})
		// 総尺は喋りと末尾の余白の合計。繋ぎのぶんは前のシーンと重なって消えるので足さない。
		out.TotalFrames += body[i]
	}
	return out
}

// transitionFrames はシーンへ入る繋ぎの長さを決める。
// body は各シーンの繋ぎを除いた尺（喋り + 末尾の余白）。
func transitionFrames(scene SceneInput, index int, body []int, fps int) int {
	// 先頭のシーンは繋ぐ相手がいない。黒からのフェードインもしない（冒頭の喋りに被るだけのため）。
	if index == 0 {
		return 0
	}

	frames := framesFor(time.Duration(scene.TransitionMs)*time.Millisecond, fps)

	// TransitionSeries の制約: 繋ぎは前後どちらのシーケンスよりも長くてはいけない。
	// 後ろ側は DurationFrames が自分の尺に繋ぎを足した値になるので自動的に満たされる。
	// 前側は満たされないことがあるため、前のシーンの尺で頭打ちにする。
	// これは「シーン 1 つを飛び越えて 3 枚が重なる」ことを防ぐ意味でもある。
	//
	// 頭打ちの相手を「喋りの尺」ではなく「喋り + 末尾の余白」にしているのは、
	// 余白も前のシーンが自分のものとして持っている時間だからである。
	// 繋ぎが余白に収まっているうちは、フェードは無音の中だけで完結する。
	return min(frames, body[index-1])
}

// framesFor は時間をフレーム数へ直す。切り上げる理由はパッケージのコメントを参照。
func framesFor(d time.Duration, fps int) int {
	if d <= 0 || fps <= 0 {
		return 0
	}
	return int(math.Ceil(d.Seconds() * float64(fps)))
}
