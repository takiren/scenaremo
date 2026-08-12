package timeline_test

import (
	"testing"
	"time"

	"github.com/takiren/scenaremo/internal/timeline"
)

// checkInvariants はどの入力でも成り立つべき性質を確かめる。
//
// 個々の期待値を並べるだけだと、TransitionSeries の尺の式と食い違ったまま
// テストだけ通る状態になりうるので、関係のほうを直接見る。
func checkInvariants(t *testing.T, got timeline.Timeline) {
	t.Helper()

	// TransitionSeries の尺の式: 各シーケンスの尺の合計 − トランジションの尺の合計。
	sumDuration, sumTransition := 0, 0
	for _, scene := range got.Scenes {
		sumDuration += scene.DurationFrames
		sumTransition += scene.TransitionFrames
	}
	if sumDuration-sumTransition != got.TotalFrames {
		t.Errorf("尺の式が合わない: Σ尺 %d − Σ繋ぎ %d = %d, TotalFrames は %d",
			sumDuration, sumTransition, sumDuration-sumTransition, got.TotalFrames)
	}

	for i, scene := range got.Scenes {
		// 先頭のシーンは繋ぐ相手がいない。
		if i == 0 && scene.TransitionFrames != 0 {
			t.Errorf("Scenes[0].TransitionFrames: got %d, want 0", scene.TransitionFrames)
		}
		if len(scene.Lines) == 0 {
			continue
		}
		// 繋ぎが終わったところで最初のセリフが鳴り始める。
		if scene.Lines[0].StartFrame != scene.TransitionFrames {
			t.Errorf("Scenes[%d]: 繋ぎ %d フレームに対して最初のセリフが %d フレーム目から",
				i, scene.TransitionFrames, scene.Lines[0].StartFrame)
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
				if gotScene.TransitionFrames != wantScene.TransitionFrames {
					t.Errorf("Scenes[%d].TransitionFrames: got %d, want %d",
						i, gotScene.TransitionFrames, wantScene.TransitionFrames)
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

// TestCalculateTransition は繋ぎの長さと、それがシーンの尺へ織り込まれることを確かめる。
func TestCalculateTransition(t *testing.T) {
	// 1 秒 = 30 フレームのシーンを 3 つ並べた土台。喋りの尺はどのシーンも 30。
	scenes := func(ms ...int) []timeline.SceneInput {
		in := make([]timeline.SceneInput, 0, len(ms))
		for _, m := range ms {
			in = append(in, timeline.SceneInput{
				TransitionMs: m,
				Lines:        []timeline.LineInput{{AudioDuration: 1 * time.Second}},
			})
		}
		return in
	}

	tests := []struct {
		name       string
		in         timeline.Input
		transition []int // 各シーンへ入る繋ぎのフレーム数
		duration   []int // 各シーンの DurationFrames
	}{
		{
			// 先頭のシーンは繋ぐ相手がいないので、指定があっても 0 になる。
			name:       "先頭シーンは繋がない",
			in:         timeline.Input{FPS: 30, Scenes: scenes(500, 0, 0)},
			transition: []int{0, 0, 0},
			duration:   []int{30, 30, 30},
		},
		{
			// 400ms = 12 フレーム。重なる分をシーンの尺へ足しておく。
			name:       "繋ぎの分だけシーンの尺が伸びる",
			in:         timeline.Input{FPS: 30, Scenes: scenes(0, 400, 400)},
			transition: []int{0, 12, 12},
			duration:   []int{30, 42, 42},
		},
		{
			name:       "0ms なら繋がない",
			in:         timeline.Input{FPS: 30, Scenes: scenes(0, 0, 0)},
			transition: []int{0, 0, 0},
			duration:   []int{30, 30, 30},
		},
		{
			// TransitionSeries の制約により、繋ぎは前のシーケンスより長くできない。
			// 2 秒 = 60 フレームは前のシーンの喋り (30) より長いので頭打ちになる。
			name:       "前のシーンより長い繋ぎは頭打ちにする",
			in:         timeline.Input{FPS: 30, Scenes: scenes(0, 2000, 2000)},
			transition: []int{0, 30, 30},
			duration:   []int{30, 60, 60},
		},
		{
			// 401ms × 30fps = 12.03 → 切り上げて 13 フレーム。
			name:       "繋ぎのフレーム数も切り上げる",
			in:         timeline.Input{FPS: 30, Scenes: scenes(0, 401, 0)},
			transition: []int{0, 13, 0},
			duration:   []int{30, 43, 30},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := timeline.Calculate(tt.in)
			checkInvariants(t, got)

			// 繋ぎを何フレーム掛けても、総尺は喋りの尺の合計のまま変わらない。
			if got.TotalFrames != 90 {
				t.Errorf("TotalFrames: got %d, want 90 (繋ぎは総尺を変えないはず)", got.TotalFrames)
			}
			for i, want := range tt.transition {
				if got.Scenes[i].TransitionFrames != want {
					t.Errorf("Scenes[%d].TransitionFrames: got %d, want %d",
						i, got.Scenes[i].TransitionFrames, want)
				}
			}
			for i, want := range tt.duration {
				if got.Scenes[i].DurationFrames != want {
					t.Errorf("Scenes[%d].DurationFrames: got %d, want %d",
						i, got.Scenes[i].DurationFrames, want)
				}
			}
		})
	}
}
