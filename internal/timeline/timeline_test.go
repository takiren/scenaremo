package timeline_test

import (
	"testing"
	"time"

	"github.com/takiren/scenaremo/internal/timeline"
)

func TestCalculate(t *testing.T) {
	tests := []struct {
		name string
		in   timeline.Input
		want timeline.Timeline
	}{
		{
			name: "1 scene, 1 line",
			in: timeline.Input{
				FPS:   30,
				GapMs: 1000,
				Scenes: []timeline.SceneInput{
					{
						Lines: []timeline.LineInput{
							{AudioDuration: 2 * time.Second}, // 60 frames
						},
					},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 60,
				Scenes: []timeline.SceneTimeline{
					{
						StartFrame:     0,
						DurationFrames: 60,
						Lines: []timeline.LineTimeline{
							{StartFrame: 0, DurationFrames: 60},
						},
					},
				},
			},
		},
		{
			name: "1 scene, 2 lines",
			in: timeline.Input{
				FPS:   30,
				GapMs: 1000, // 30 frames gap
				Scenes: []timeline.SceneInput{
					{
						Lines: []timeline.LineInput{
							{AudioDuration: 2 * time.Second}, // 60 frames
							{AudioDuration: 1 * time.Second}, // 30 frames
						},
					},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 120, // 60 + 30 (gap) + 30
				Scenes: []timeline.SceneTimeline{
					{
						StartFrame:     0,
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
			name: "2 scenes, 1 line each",
			in: timeline.Input{
				FPS:   30,
				GapMs: 500, // 15 frames gap, but not applied at end of scene
				Scenes: []timeline.SceneInput{
					{
						Lines: []timeline.LineInput{
							{AudioDuration: 1 * time.Second}, // 30 frames
						},
					},
					{
						Lines: []timeline.LineInput{
							{AudioDuration: 2 * time.Second}, // 60 frames
						},
					},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 90, // 30 + 60
				Scenes: []timeline.SceneTimeline{
					{
						StartFrame:     0,
						DurationFrames: 30,
						Lines: []timeline.LineTimeline{
							{StartFrame: 0, DurationFrames: 30},
						},
					},
					{
						StartFrame:     30,
						DurationFrames: 60,
						Lines: []timeline.LineTimeline{
							{StartFrame: 30, DurationFrames: 60},
						},
					},
				},
			},
		},
		{
			name: "2 scenes, multiple lines each",
			in: timeline.Input{
				FPS:   30,
				GapMs: 500, // 15 frames gap
				Scenes: []timeline.SceneInput{
					{
						Lines: []timeline.LineInput{
							{AudioDuration: 1 * time.Second}, // 30 frames
							{AudioDuration: 1 * time.Second}, // 30 frames
						},
					},
					{
						Lines: []timeline.LineInput{
							{AudioDuration: 1 * time.Second}, // 30 frames
							{AudioDuration: 2 * time.Second}, // 60 frames
						},
					},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 180, // scene1: 30 + 15 + 30 = 75. scene2: 30 + 15 + 60 = 105. total: 180
				Scenes: []timeline.SceneTimeline{
					{
						StartFrame:     0,
						DurationFrames: 75,
						Lines: []timeline.LineTimeline{
							{StartFrame: 0, DurationFrames: 30},
							{StartFrame: 45, DurationFrames: 30},
						},
					},
					{
						StartFrame:     75,
						DurationFrames: 105,
						Lines: []timeline.LineTimeline{
							{StartFrame: 75, DurationFrames: 30},
							{StartFrame: 120, DurationFrames: 60},
						},
					},
				},
			},
		},
		{
			name: "gapMs = 0",
			in: timeline.Input{
				FPS:   30,
				GapMs: 0,
				Scenes: []timeline.SceneInput{
					{
						Lines: []timeline.LineInput{
							{AudioDuration: 1 * time.Second}, // 30 frames
							{AudioDuration: 1 * time.Second}, // 30 frames
						},
					},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 60,
				Scenes: []timeline.SceneTimeline{
					{
						StartFrame:     0,
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
			name: "Ceiling verification",
			in: timeline.Input{
				FPS:   30,
				GapMs: 1001, // 1.001 * 30 = 30.03 -> ceil -> 31 frames
				Scenes: []timeline.SceneInput{
					{
						Lines: []timeline.LineInput{
							{AudioDuration: 1001 * time.Millisecond}, // 1.001 * 30 = 30.03 -> 31 frames
							{AudioDuration: 1001 * time.Millisecond}, // 31 frames
						},
					},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 93, // 31 + 31 (gap) + 31
				Scenes: []timeline.SceneTimeline{
					{
						StartFrame:     0,
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
			name: "Very short audio",
			in: timeline.Input{
				FPS:   30,
				GapMs: 1000,
				Scenes: []timeline.SceneInput{
					{
						Lines: []timeline.LineInput{
							{AudioDuration: 1 * time.Millisecond}, // 0.001 * 30 = 0.03 -> 1 frame
						},
					},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 1,
				Scenes: []timeline.SceneTimeline{
					{
						StartFrame:     0,
						DurationFrames: 1,
						Lines: []timeline.LineTimeline{
							{StartFrame: 0, DurationFrames: 1},
						},
					},
				},
			},
		},
		{
			name: "fps = 60",
			in: timeline.Input{
				FPS:   60,
				GapMs: 1000, // 60 frames gap
				Scenes: []timeline.SceneInput{
					{
						Lines: []timeline.LineInput{
							{AudioDuration: 2 * time.Second}, // 120 frames
							{AudioDuration: 1 * time.Second}, // 60 frames
						},
					},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 240, // 120 + 60 (gap) + 60
				Scenes: []timeline.SceneTimeline{
					{
						StartFrame:     0,
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
			
			if got.TotalFrames != tt.want.TotalFrames {
				t.Errorf("TotalFrames: got %d, want %d", got.TotalFrames, tt.want.TotalFrames)
			}
			if len(got.Scenes) != len(tt.want.Scenes) {
				t.Fatalf("Scenes length: got %d, want %d", len(got.Scenes), len(tt.want.Scenes))
			}
			for i, gotScene := range got.Scenes {
				wantScene := tt.want.Scenes[i]
				if gotScene.StartFrame != wantScene.StartFrame {
					t.Errorf("Scenes[%d].StartFrame: got %d, want %d", i, gotScene.StartFrame, wantScene.StartFrame)
				}
				if gotScene.DurationFrames != wantScene.DurationFrames {
					t.Errorf("Scenes[%d].DurationFrames: got %d, want %d", i, gotScene.DurationFrames, wantScene.DurationFrames)
				}
				if len(gotScene.Lines) != len(wantScene.Lines) {
					t.Fatalf("Scenes[%d].Lines length: got %d, want %d", i, len(gotScene.Lines), len(wantScene.Lines))
				}
				for j, gotLine := range gotScene.Lines {
					wantLine := wantScene.Lines[j]
					if gotLine.StartFrame != wantLine.StartFrame {
						t.Errorf("Scenes[%d].Lines[%d].StartFrame: got %d, want %d", i, j, gotLine.StartFrame, wantLine.StartFrame)
					}
					if gotLine.DurationFrames != wantLine.DurationFrames {
						t.Errorf("Scenes[%d].Lines[%d].DurationFrames: got %d, want %d", i, j, gotLine.DurationFrames, wantLine.DurationFrames)
					}
				}
			}
		})
	}
}
