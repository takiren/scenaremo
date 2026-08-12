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
func checkInvariants(t *testing.T, in timeline.Input, got timeline.Timeline) {
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

	// シーン末尾の余白は切り上げで丸めた 1 つの値。どのシーンの尻にも同じだけ付く。
	sceneGap := ceilFrames(in.SceneGapMs, in.FPS)

	for i, scene := range got.Scenes {
		// 先頭のシーンは繋ぐ相手がいない。
		if i == 0 && scene.TransitionFrames != 0 {
			t.Errorf("Scenes[0].TransitionFrames: got %d, want 0", scene.TransitionFrames)
		}
		if len(scene.Lines) == 0 {
			continue
		}
		// 繋ぎが終わったところで最初のセリフが鳴り始める。
		// 余白を次のシーンの頭ではなく前のシーンの尻に置いているので、
		// sceneGapMs をいくつにしてもこの関係は動かない。
		if scene.Lines[0].StartFrame != scene.TransitionFrames {
			t.Errorf("Scenes[%d]: 繋ぎ %d フレームに対して最初のセリフが %d フレーム目から",
				i, scene.TransitionFrames, scene.Lines[0].StartFrame)
		}
		// セリフが終わったあと、シーンの尻にはちょうど余白のぶんだけ残っている。
		last := scene.Lines[len(scene.Lines)-1]
		end := last.StartFrame + last.DurationFrames
		if rest := scene.DurationFrames - end; rest != sceneGap {
			t.Errorf("Scenes[%d]: セリフの終わり %d からシーンの終わり %d までが %d フレーム, want %d",
				i, end, scene.DurationFrames, rest, sceneGap)
		}
	}
}

// ceilFrames はミリ秒をフレーム数へ切り上げる。timeline 側の丸めと同じ規約。
func ceilFrames(ms, fps int) int {
	if ms <= 0 || fps <= 0 {
		return 0
	}
	return (ms*fps + 999) / 1000
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
			// gapMs はシーンの境目には効かない。境目の余白は sceneGapMs の担当。
			// セリフの位置はシーンごとに 0 から数え直す。
			name: "sceneGapMs が 0 ならシーンの境目に余白は入らない",
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
			// 余白は前のシーンの尻に乗る。次のシーンのセリフは 0 フレーム目のまま動かない。
			name: "シーンの境目に余白が入る",
			in: timeline.Input{
				FPS:        30,
				GapMs:      500, // 15 フレーム
				SceneGapMs: 500, // 15 フレーム
				Scenes: []timeline.SceneInput{
					{Lines: []timeline.LineInput{{AudioDuration: 1 * time.Second}}}, // 30 フレーム
					{Lines: []timeline.LineInput{{AudioDuration: 2 * time.Second}}}, // 60 フレーム
				},
			},
			want: timeline.Timeline{
				TotalFrames: 120, // (30 + 15) + (60 + 15)
				Scenes: []timeline.SceneTimeline{
					{
						DurationFrames: 45, // 30 + 15 (末尾の余白)
						Lines:          []timeline.LineTimeline{{StartFrame: 0, DurationFrames: 30}},
					},
					{
						DurationFrames: 75, // 60 + 15 (動画末尾の余韻)
						Lines:          []timeline.LineTimeline{{StartFrame: 0, DurationFrames: 60}},
					},
				},
			},
		},
		{
			// シーンが1つでも末尾の余白は付く。最後のセリフと同時に動画が切れないようにするため。
			name: "シーンが1つでも末尾に余白が入る",
			in: timeline.Input{
				FPS:        30,
				SceneGapMs: 1000, // 30 フレーム
				Scenes: []timeline.SceneInput{
					{Lines: []timeline.LineInput{{AudioDuration: 1 * time.Second}}}, // 30 フレーム
				},
			},
			want: timeline.Timeline{
				TotalFrames: 60,
				Scenes: []timeline.SceneTimeline{
					{
						DurationFrames: 60,
						Lines:          []timeline.LineTimeline{{StartFrame: 0, DurationFrames: 30}},
					},
				},
			},
		},
		{
			// 501ms × 30fps = 15.03 → 切り上げて 16 フレーム。音声もセリフ間の余白も同じ規則。
			name: "シーン末尾の余白も切り上げる",
			in: timeline.Input{
				FPS:        30,
				SceneGapMs: 501,
				Scenes: []timeline.SceneInput{
					{Lines: []timeline.LineInput{{AudioDuration: 1 * time.Second}}},
					{Lines: []timeline.LineInput{{AudioDuration: 1 * time.Second}}},
				},
			},
			want: timeline.Timeline{
				TotalFrames: 92, // (30 + 16) × 2
				Scenes: []timeline.SceneTimeline{
					{
						DurationFrames: 46,
						Lines:          []timeline.LineTimeline{{StartFrame: 0, DurationFrames: 30}},
					},
					{
						DurationFrames: 46,
						Lines:          []timeline.LineTimeline{{StartFrame: 0, DurationFrames: 30}},
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
			checkInvariants(t, tt.in, got)

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
			checkInvariants(t, tt.in, got)

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

// TestCalculateSceneGap は sceneGapMs を変えても崩れてはいけない関係を確かめる。
//
// 個々のフレーム数はテーブル側で押さえているので、ここでは「余白がどちら側に乗るか」を見る。
// 頭に乗せてしまうとセリフの位置が動き、繋ぎは前のシーンの語尾に被ったままになる。
func TestCalculateSceneGap(t *testing.T) {
	// 喋りの尺が 30 フレームのシーンを 3 つ。繋ぎ 400ms (12 フレーム) は喋りより短いので、
	// 余白をいくつにしても頭打ちは働かない（比較の邪魔をしない）。
	input := func(sceneGapMs int) timeline.Input {
		return timeline.Input{
			FPS:        30,
			GapMs:      300,
			SceneGapMs: sceneGapMs,
			Scenes: []timeline.SceneInput{
				{TransitionMs: 400, Lines: []timeline.LineInput{{AudioDuration: 1 * time.Second}}},
				{TransitionMs: 400, Lines: []timeline.LineInput{{AudioDuration: 1 * time.Second}}},
				{TransitionMs: 400, Lines: []timeline.LineInput{{AudioDuration: 1 * time.Second}}},
			},
		}
	}

	// 余白なし = この issue より前の挙動。ここを基準に差分を見る。
	base := timeline.Calculate(input(0))
	checkInvariants(t, input(0), base)

	for _, sceneGapMs := range []int{0, 1, 400, 500, 1000, 5000} {
		in := input(sceneGapMs)
		got := timeline.Calculate(in)
		checkInvariants(t, in, got)

		gap := ceilFrames(sceneGapMs, in.FPS)

		// 総尺は「喋りの尺の合計 + 余白 × シーン数」。最後のシーンの余白も含む。
		if want := base.TotalFrames + gap*len(in.Scenes); got.TotalFrames != want {
			t.Errorf("sceneGapMs=%d: TotalFrames: got %d, want %d", sceneGapMs, got.TotalFrames, want)
		}

		for i, scene := range got.Scenes {
			// 伸びるのはシーンの尻だけ。繋ぎの長さもセリフの位置も動かない。
			if want := base.Scenes[i].DurationFrames + gap; scene.DurationFrames != want {
				t.Errorf("sceneGapMs=%d: Scenes[%d].DurationFrames: got %d, want %d",
					sceneGapMs, i, scene.DurationFrames, want)
			}
			if scene.TransitionFrames != base.Scenes[i].TransitionFrames {
				t.Errorf("sceneGapMs=%d: Scenes[%d].TransitionFrames が %d から %d へ動いた",
					sceneGapMs, i, base.Scenes[i].TransitionFrames, scene.TransitionFrames)
			}
			for j, line := range scene.Lines {
				if line != base.Scenes[i].Lines[j] {
					t.Errorf("sceneGapMs=%d: Scenes[%d].Lines[%d] が %+v から %+v へ動いた",
						sceneGapMs, i, j, base.Scenes[i].Lines[j], line)
				}
			}
		}
	}
}

// TestCalculateSceneGapCoversTransition は余白が繋ぎ以上あるとき、
// フェードが前のシーンの語尾に被らないことを確かめる。issue #44 の目的そのもの。
//
// TransitionSeries は次のシーンの先頭 TransitionFrames を前のシーンの末尾へ重ねるので、
// 「前のシーンの最後のセリフが終わってからシーンが終わるまで」が繋ぎ以上あればよい。
func TestCalculateSceneGapCoversTransition(t *testing.T) {
	tests := []struct {
		name       string
		sceneGapMs int
		covered    bool
	}{
		{"余白が繋ぎより長い", 500, true},
		{"余白と繋ぎが同じ長さ", 400, true},
		{"余白が繋ぎより短い", 200, false},
		{"余白なし", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := timeline.Input{
				FPS:        30,
				SceneGapMs: tt.sceneGapMs,
				Scenes: []timeline.SceneInput{
					{Lines: []timeline.LineInput{{AudioDuration: 2 * time.Second}}},
					{TransitionMs: 400, Lines: []timeline.LineInput{{AudioDuration: 2 * time.Second}}},
				},
			}
			got := timeline.Calculate(in)
			checkInvariants(t, in, got)

			prev := got.Scenes[0]
			last := prev.Lines[len(prev.Lines)-1]
			silence := prev.DurationFrames - (last.StartFrame + last.DurationFrames)

			if covered := silence >= got.Scenes[1].TransitionFrames; covered != tt.covered {
				t.Errorf("前のシーンの尻の無音 %d フレームに対して繋ぎ %d フレーム: 収まった=%v, want %v",
					silence, got.Scenes[1].TransitionFrames, covered, tt.covered)
			}
		})
	}
}

// TestCalculateSceneGapWidensTransitionClamp は繋ぎの頭打ちが
// 「前のシーンの喋り + 末尾の余白」で効くことを確かめる。
//
// 余白も前のシーンが自分のものとして持っている時間なので、そこまでは繋ぎに使える。
// 喋りの尺だけで頭打ちにすると、短いシーンのあとで繋ぎが不必要に削られる。
func TestCalculateSceneGapWidensTransitionClamp(t *testing.T) {
	// 前のシーンの喋りは 1 フレーム (1ms を切り上げ)。繋ぎの希望は 2000ms = 60 フレーム。
	in := timeline.Input{
		FPS:        30,
		SceneGapMs: 1000, // 30 フレーム
		Scenes: []timeline.SceneInput{
			{Lines: []timeline.LineInput{{AudioDuration: 1 * time.Millisecond}}},
			{TransitionMs: 2000, Lines: []timeline.LineInput{{AudioDuration: 2 * time.Second}}},
		},
	}
	got := timeline.Calculate(in)
	checkInvariants(t, in, got)

	// 1 (喋り) + 30 (余白) = 31 フレームまで。それ以上は前のシーンを食い尽くしてしまう。
	if want := 31; got.Scenes[1].TransitionFrames != want {
		t.Errorf("Scenes[1].TransitionFrames: got %d, want %d", got.Scenes[1].TransitionFrames, want)
	}
	// 頭打ちが効いても尺の式は崩れない（checkInvariants でも見ているが、意図として明示しておく）。
	if want := (1 + 30) + (60 + 30); got.TotalFrames != want {
		t.Errorf("TotalFrames: got %d, want %d", got.TotalFrames, want)
	}
}
