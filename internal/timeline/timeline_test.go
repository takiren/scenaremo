package timeline_test

import (
	"testing"
	"time"

	"github.com/takiren/scenaremo/internal/timeline"
)

// checkInvariants はどの入力でも成り立つべき性質を確かめる。
//
// 個々の期待値を並べるだけだと、関係のほうが崩れていてもテストだけ通る状態になりうる。
func checkInvariants(t *testing.T, got timeline.Timeline) {
	t.Helper()

	sumDuration := 0
	for _, scene := range got.Scenes {
		sumDuration += scene.DurationFrames
	}
	if sumDuration != got.TotalFrames {
		t.Errorf("尺の合計が合わない: Σ尺 %d, TotalFrames は %d", sumDuration, got.TotalFrames)
	}

	for i, scene := range got.Scenes {
		if len(scene.Lines) == 0 {
			continue
		}
		// セリフはシーンの先頭から数え直す。
		if scene.Lines[0].StartFrame != 0 {
			t.Errorf("Scenes[%d]: 最初のセリフが %d フレーム目から", i, scene.Lines[0].StartFrame)
		}
		// セリフはシーンの尺に収まる。
		last := scene.Lines[len(scene.Lines)-1]
		if end := last.StartFrame + last.DurationFrames; end > scene.DurationFrames {
			t.Errorf("Scenes[%d]: セリフがシーンの尺 %d を超えている (末尾 %d)", i, scene.DurationFrames, end)
		}
	}
}

func TestCalculate(t *testing.T) {
	tests := []struct {
		name string
		in   timeline.Input
		want timeline.Timeline
	}{
		{
			name: "1 シーン 1 セリフ",
			in: timeline.Input{
				FPS:   30,
				GapMs: 1000,
				Scenes: []timeline.SceneInput{
					{Lines: []timeline.LineInput{{AudioDuration: 2 * time.Second}}}, // 60 フレーム
				},
			},
			want: timeline.Timeline{
				TotalFrames: 60,
				Scenes: []timeline.SceneTimeline{
					{
						DurationFrames: 60,
						Lines:          []timeline.LineTimeline{{StartFrame: 0, DurationFrames: 60}},
					},
				},
			},
		},
		{
			name: "セリフの間に余白が入る",
			in: timeline.Input{
				FPS:   30,
				GapMs: 1000, // 30 フレーム
				Scenes: []timeline.SceneInput{
					{Lines: []timeline.LineInput{
						{AudioDuration: 2 * time.Second}, // 60 フレーム
						{AudioDuration: 1 * time.Second}, // 30 フレーム
					}},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 120, // 60 + 30 (余白) + 30
				Scenes: []timeline.SceneTimeline{
					{
						DurationFrames: 120,
						Lines: []timeline.LineTimeline{
							{StartFrame: 0, DurationFrames: 60},
							{StartFrame: 90, DurationFrames: 30},
						},
					},
				},
			},
		},
		{
			// 余白はシーンの境目には入らない。セリフの位置はシーンごとに 0 から数え直す。
			name: "シーンの境目には余白が入らない",
			in: timeline.Input{
				FPS:   30,
				GapMs: 500, // 15 フレーム
				Scenes: []timeline.SceneInput{
					{Lines: []timeline.LineInput{{AudioDuration: 1 * time.Second}}}, // 30 フレーム
					{Lines: []timeline.LineInput{{AudioDuration: 2 * time.Second}}}, // 60 フレーム
				},
			},
			want: timeline.Timeline{
				TotalFrames: 90, // 30 + 60
				Scenes: []timeline.SceneTimeline{
					{
						DurationFrames: 30,
						Lines:          []timeline.LineTimeline{{StartFrame: 0, DurationFrames: 30}},
					},
					{
						DurationFrames: 60,
						Lines:          []timeline.LineTimeline{{StartFrame: 0, DurationFrames: 60}},
					},
				},
			},
		},
		{
			name: "複数シーン 複数セリフ",
			in: timeline.Input{
				FPS:   30,
				GapMs: 500, // 15 フレーム
				Scenes: []timeline.SceneInput{
					{Lines: []timeline.LineInput{
						{AudioDuration: 1 * time.Second},
						{AudioDuration: 1 * time.Second},
					}},
					{Lines: []timeline.LineInput{
						{AudioDuration: 1 * time.Second},
						{AudioDuration: 2 * time.Second},
					}},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 180, // 75 + 105
				Scenes: []timeline.SceneTimeline{
					{
						DurationFrames: 75, // 30 + 15 + 30
						Lines: []timeline.LineTimeline{
							{StartFrame: 0, DurationFrames: 30},
							{StartFrame: 45, DurationFrames: 30},
						},
					},
					{
						DurationFrames: 105, // 30 + 15 + 60
						Lines: []timeline.LineTimeline{
							{StartFrame: 0, DurationFrames: 30},
							{StartFrame: 45, DurationFrames: 60},
						},
					},
				},
			},
		},
		{
			name: "余白なし",
			in: timeline.Input{
				FPS:   30,
				GapMs: 0,
				Scenes: []timeline.SceneInput{
					{Lines: []timeline.LineInput{
						{AudioDuration: 1 * time.Second},
						{AudioDuration: 1 * time.Second},
					}},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 60,
				Scenes: []timeline.SceneTimeline{
					{
						DurationFrames: 60,
						Lines: []timeline.LineTimeline{
							{StartFrame: 0, DurationFrames: 30},
							{StartFrame: 30, DurationFrames: 30},
						},
					},
				},
			},
		},
		{
			// 1.001 秒 × 30fps = 30.03 → 切り上げて 31 フレーム。余白も同じ規則で丸める。
			name: "フレーム数は切り上げる",
			in: timeline.Input{
				FPS:   30,
				GapMs: 1001,
				Scenes: []timeline.SceneInput{
					{Lines: []timeline.LineInput{
						{AudioDuration: 1001 * time.Millisecond},
						{AudioDuration: 1001 * time.Millisecond},
					}},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 93, // 31 + 31 (余白) + 31
				Scenes: []timeline.SceneTimeline{
					{
						DurationFrames: 93,
						Lines: []timeline.LineTimeline{
							{StartFrame: 0, DurationFrames: 31},
							{StartFrame: 62, DurationFrames: 31},
						},
					},
				},
			},
		},
		{
			// 切り上げるので、どんなに短い音声でも 1 フレームは与えられる。
			name: "とても短い音声",
			in: timeline.Input{
				FPS:   30,
				GapMs: 1000,
				Scenes: []timeline.SceneInput{
					{Lines: []timeline.LineInput{{AudioDuration: 1 * time.Millisecond}}},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 1,
				Scenes: []timeline.SceneTimeline{
					{
						DurationFrames: 1,
						Lines:          []timeline.LineTimeline{{StartFrame: 0, DurationFrames: 1}},
					},
				},
			},
		},
		{
			name: "fps 60",
			in: timeline.Input{
				FPS:   60,
				GapMs: 1000, // 60 フレーム
				Scenes: []timeline.SceneInput{
					{Lines: []timeline.LineInput{
						{AudioDuration: 2 * time.Second}, // 120 フレーム
						{AudioDuration: 1 * time.Second}, // 60 フレーム
					}},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 240,
				Scenes: []timeline.SceneTimeline{
					{
						DurationFrames: 240,
						Lines: []timeline.LineTimeline{
							{StartFrame: 0, DurationFrames: 120},
							{StartFrame: 180, DurationFrames: 60},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := timeline.Calculate(tt.in)
			checkInvariants(t, got)

			if got.TotalFrames != tt.want.TotalFrames {
				t.Errorf("TotalFrames: got %d, want %d", got.TotalFrames, tt.want.TotalFrames)
			}
			if len(got.Scenes) != len(tt.want.Scenes) {
				t.Fatalf("シーンの数: got %d, want %d", len(got.Scenes), len(tt.want.Scenes))
			}
			for i, gotScene := range got.Scenes {
				wantScene := tt.want.Scenes[i]
				if gotScene.DurationFrames != wantScene.DurationFrames {
					t.Errorf("Scenes[%d].DurationFrames: got %d, want %d",
						i, gotScene.DurationFrames, wantScene.DurationFrames)
				}
				if len(gotScene.Lines) != len(wantScene.Lines) {
					t.Fatalf("Scenes[%d] のセリフの数: got %d, want %d", i, len(gotScene.Lines), len(wantScene.Lines))
				}
				for j, gotLine := range gotScene.Lines {
					if gotLine != wantScene.Lines[j] {
						t.Errorf("Scenes[%d].Lines[%d]: got %+v, want %+v", i, j, gotLine, wantScene.Lines[j])
					}
				}
			}
		})
	}
}
