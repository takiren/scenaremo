// Package timeline はスライドショー動画のタイムライン（各シーンや音声の開始フレーム・尺）を計算する機能を提供します。
package timeline

import (
	"math"
	"time"
)

// Input はタイムライン計算の入力。
type Input struct {
	FPS    int
	GapMs  int
	Scenes []SceneInput
}

type SceneInput struct {
	Lines []LineInput
}

type LineInput struct {
	AudioDuration time.Duration
}

// Timeline は計算結果。props.json の生成に使う。
type Timeline struct {
	TotalFrames int
	Scenes      []SceneTimeline
}

type SceneTimeline struct {
	StartFrame     int
	DurationFrames int
	Lines          []LineTimeline
}

type LineTimeline struct {
	StartFrame     int
	DurationFrames int
}

// Calculate は入力に基づき、各シーンおよびセリフのフレーム位置と尺を計算します。
func Calculate(in Input) Timeline {
	var timeline Timeline
	currentFrame := 0

	gapSeconds := float64(in.GapMs) / 1000.0
	gapFrames := int(math.Ceil(gapSeconds * float64(in.FPS)))

	for _, sceneIn := range in.Scenes {
		sceneStartFrame := currentFrame
		var lines []LineTimeline

		for j, lineIn := range sceneIn.Lines {
			durationFrames := int(math.Ceil(lineIn.AudioDuration.Seconds() * float64(in.FPS)))
			lines = append(lines, LineTimeline{
				StartFrame:     currentFrame,
				DurationFrames: durationFrames,
			})
			currentFrame += durationFrames

			// シーン内の最後の行でなければギャップを追加
			if j < len(sceneIn.Lines)-1 {
				currentFrame += gapFrames
			}
		}

		timeline.Scenes = append(timeline.Scenes, SceneTimeline{
			StartFrame:     sceneStartFrame,
			DurationFrames: currentFrame - sceneStartFrame,
			Lines:          lines,
		})
	}

	timeline.TotalFrames = currentFrame
	return timeline
}
