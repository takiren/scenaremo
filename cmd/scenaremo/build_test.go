package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takiren/scenaremo/internal/build"
	"github.com/takiren/scenaremo/internal/project"
	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/script"
)

// stubBuild は build の本体を差し替える。
// テストが実機の VOICEVOX や台本の有無に左右されないようにするため。
func stubBuild(t *testing.T, fn func(context.Context, build.Options) (*build.Result, error)) {
	t.Helper()
	original := runBuild
	runBuild = fn
	t.Cleanup(func() { runBuild = original })
}

// fakeResult は build が成功したときの戻り値。尺は 330 フレーム (30fps で 11 秒)。
func fakeResult(dir string) *build.Result {
	return &build.Result{
		Layout: &project.Layout{
			Dir:       dir,
			OutDir:    filepath.Join(dir, project.OutDirName),
			PropsPath: filepath.Join(dir, project.OutDirName, props.FileName),
		},
		Props:       &props.Props{Meta: props.Meta{FPS: 30, DurationInFrames: 330}},
		Synthesized: 3,
		Cached:      1,
	}
}

func TestBuild_成功すれば書き出したpropsjsonのパスを標準出力へ出す(t *testing.T) {
	stubBuild(t, func(context.Context, build.Options) (*build.Result, error) {
		return fakeResult("videos/ep01"), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"build", "videos/ep01"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	// 標準出力は次の工程へ渡せる 1 行だけ。ここへ飾りを混ぜると
	// scenaremo render や利用者のスクリプトがそのまま使えなくなる。
	want := filepath.Join("videos/ep01", ".scenaremo", "props.json") + "\n"
	if stdout.String() != want {
		t.Errorf("標準出力が違う: %q (want %q)", stdout.String(), want)
	}
	// 人が読む要約は標準エラーへ。
	for _, s := range []string{"props.json", "330 フレーム", "11.0 秒"} {
		if !strings.Contains(stderr.String(), s) {
			t.Errorf("要約に %q が含まれない: %s", s, stderr.String())
		}
	}
}

func TestBuild_進捗は標準エラーへ出る(t *testing.T) {
	stubBuild(t, func(_ context.Context, opts build.Options) (*build.Result, error) {
		if opts.Reporter == nil {
			t.Fatal("進捗の通知先が渡っていない")
		}
		opts.Reporter.Start(1)
		opts.Reporter.LineStart(0, "zundamon", "セリフ")
		opts.Reporter.LineDone(0, true, 0)
		opts.Reporter.Done(0, 1)
		return fakeResult("videos/ep01"), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"build", "videos/ep01"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	for _, s := range []string{"音声を合成します", "zundamon", "セリフ", "キャッシュ"} {
		if !strings.Contains(stderr.String(), s) {
			t.Errorf("進捗に %q が含まれない: %s", s, stderr.String())
		}
	}
	// 進捗は成果物ではないので標準出力へは出さない。
	if strings.Contains(stdout.String(), "zundamon") {
		t.Errorf("進捗が標準出力へ混ざっている: %s", stdout.String())
	}
}

func TestBuild_quietなら進捗を出さない(t *testing.T) {
	for _, flag := range []string{"--quiet", "-q"} {
		t.Run(flag, func(t *testing.T) {
			stubBuild(t, func(_ context.Context, opts build.Options) (*build.Result, error) {
				opts.Reporter.Start(1)
				opts.Reporter.LineStart(0, "zundamon", "セリフ")
				opts.Reporter.LineDone(0, false, 0)
				opts.Reporter.Done(1, 0)
				return fakeResult("videos/ep01"), nil
			})

			var stdout, stderr bytes.Buffer
			code := run(context.Background(), []string{"build", "videos/ep01", flag}, &stdout, &stderr)

			if code != exitSuccess {
				t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
			}
			if strings.Contains(stderr.String(), "zundamon") {
				t.Errorf("--quiet なのに進捗が出ている: %s", stderr.String())
			}
			// 静かにしても、成果物のパスは要る。
			if !strings.Contains(stdout.String(), "props.json") {
				t.Errorf("--quiet で成果物のパスまで消えている: %q", stdout.String())
			}
		})
	}
}

func TestBuild_オプションが本体へ伝わる(t *testing.T) {
	var got build.Options
	stubBuild(t, func(_ context.Context, opts build.Options) (*build.Result, error) {
		got = opts
		return fakeResult("videos/ep01"), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"build", "videos/ep01", "--voicevox-url=http://192.168.0.2:50021", "--no-cache",
	}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	if got.Dir != "videos/ep01" {
		t.Errorf("動画ディレクトリが渡っていない: %q", got.Dir)
	}
	if got.VoicevoxURL != "http://192.168.0.2:50021" {
		t.Errorf("接続先が渡っていない: %q", got.VoicevoxURL)
	}
	if !got.NoCache {
		t.Error("--no-cache が渡っていない")
	}
	// 不具合の報告からどの版が吐いた props.json か分かるように、生成者を必ず載せる。
	if !strings.Contains(got.GeneratedBy, "scenaremo") {
		t.Errorf("生成者が渡っていない: %q", got.GeneratedBy)
	}
}

func TestBuild_既定の接続先はVOICEVOXの既定値(t *testing.T) {
	var got build.Options
	stubBuild(t, func(_ context.Context, opts build.Options) (*build.Result, error) {
		got = opts
		return fakeResult("videos/ep01"), nil
	})

	var stdout, stderr bytes.Buffer
	run(context.Background(), []string{"build", "videos/ep01"}, &stdout, &stderr)

	if got.VoicevoxURL != "http://127.0.0.1:50021" {
		t.Errorf("既定の接続先が違う: %q", got.VoicevoxURL)
	}
	if got.NoCache {
		t.Error("既定でキャッシュを無視している")
	}
}

func TestBuild_ディレクトリを指定しなければ使い方を出す(t *testing.T) {
	stubBuild(t, func(context.Context, build.Options) (*build.Result, error) {
		t.Error("引数が無いのに build が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"build"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Error("引数が無いのに成功として終了した")
	}
	msg := stderr.String()
	for _, want := range []string{"使い方:", "videos/ep01"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q が含まれない: %s", want, msg)
		}
	}
}

func TestBuild_ディレクトリは1つだけ(t *testing.T) {
	stubBuild(t, func(context.Context, build.Options) (*build.Result, error) {
		t.Error("引数が多いのに build が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"build", "videos/ep01", "videos/ep02"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Error("引数が多いのに成功として終了した")
	}
	msg := stderr.String()
	if !strings.Contains(msg, "videos/ep02") {
		t.Errorf("何が余分だったのか分からない: %s", msg)
	}
	if !strings.Contains(msg, "使い方:") {
		t.Errorf("使い方が添えられていない: %s", msg)
	}
}

func TestBuild_台本の検証エラーは報告だけを出す(t *testing.T) {
	stubBuild(t, func(context.Context, build.Options) (*build.Result, error) {
		return nil, &script.Error{
			Filename: "videos/ep01/script.yaml",
			Issues: []script.Issue{{
				Path:    "meta.title",
				Line:    3,
				Message: "title は必須です",
				Hint:    "meta.title に動画のタイトルを書いてください",
			}},
		}
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"build", "videos/ep01"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Fatal("台本が不正なのに成功として終了した")
	}
	msg := stderr.String()
	for _, want := range []string{"videos/ep01/script.yaml", "title は必須です", "ヒント:"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q が含まれない: %s", want, msg)
		}
	}
	// 報告はそれ自体が整形済みなので、頭に "scenaremo: " を重ねない。
	if strings.Contains(msg, "scenaremo: videos/ep01/script.yaml の検証に失敗") {
		t.Errorf("報告の頭に scenaremo: を重ねている: %s", msg)
	}
	// 台本の誤りは使い方の誤りではないので、使い方は突きつけない。
	if strings.Contains(msg, "使い方:") {
		t.Errorf("台本の誤りで usage が出ている: %s", msg)
	}
}

func TestBuild_失敗すれば終了コードが0以外(t *testing.T) {
	stubBuild(t, func(context.Context, build.Options) (*build.Result, error) {
		return nil, errors.New("VOICEVOX ENGINE に接続できませんでした")
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"build", "videos/ep01"}, &stdout, &stderr)

	if code != exitFailure {
		t.Errorf("終了コードが違う: %d", code)
	}
	if !strings.Contains(stderr.String(), "VOICEVOX ENGINE に接続できませんでした") {
		t.Errorf("失敗の理由が出ていない: %s", stderr.String())
	}
	// 失敗したのに成果物のパスを出すと、次の工程が空振りする。
	if stdout.Len() != 0 {
		t.Errorf("失敗なのに標準出力へ書いている: %s", stdout.String())
	}
}

func TestBuild_合成の途中で失敗しても報告が行頭から始まる(t *testing.T) {
	stubBuild(t, func(_ context.Context, opts build.Options) (*build.Result, error) {
		// 1 件目の合成に入ったところで落ちる。進捗は行を書きかけのまま止まる。
		opts.Reporter.Start(2)
		opts.Reporter.LineStart(0, "zundamon", "セリフ")
		return nil, errors.New("VOICEVOX ENGINE に接続できませんでした")
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"build", "videos/ep01"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Fatal("失敗したのに成功として終了した")
	}
	// 失敗の理由は利用者が最も注意して読む文なので、セリフの続きに繋げない。
	for _, line := range strings.Split(stderr.String(), "\n") {
		if strings.Contains(line, "VOICEVOX ENGINE に接続できませんでした") && strings.Contains(line, "セリフ") {
			t.Errorf("失敗の報告が書きかけの行に繋がっている: %q", line)
		}
	}
}

func TestBuild_知らないオプションは失敗する(t *testing.T) {
	stubBuild(t, func(context.Context, build.Options) (*build.Result, error) {
		t.Error("引数の解釈に失敗したのに build が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"build", "videos/ep01", "--nocache"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Error("知らないオプションなのに成功として終了した")
	}
	msg := stderr.String()
	for _, want := range []string{"使い方:", "--no-cache"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q が含まれない: %s", want, msg)
		}
	}
}

func TestBuild_helpは成功する(t *testing.T) {
	stubBuild(t, func(context.Context, build.Options) (*build.Result, error) {
		t.Error("--help なのに build が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"build", "--help"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Errorf("--help の終了コードが 0 でない: %d", code)
	}
	out := stdout.String()
	for _, want := range []string{"scenaremo build", "--voicevox-url", "--no-cache", "--quiet"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が含まれない: %s", want, out)
		}
	}
	// 使い方の行に cobra 既定の英語 ([flags]) を混ぜない。
	if strings.Contains(out, "[flags]") {
		t.Errorf("使い方に英語が混ざっている: %s", out)
	}
}

func TestRoot_コマンド一覧にbuildが載る(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run(context.Background(), []string{"help"}, &stdout, &stderr)

	if !strings.Contains(stdout.String(), "build") {
		t.Errorf("コマンド一覧に build が無い: %s", stdout.String())
	}
}
