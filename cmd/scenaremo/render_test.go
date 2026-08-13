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
	"github.com/takiren/scenaremo/internal/render"
)

// stubRender は render の本体を差し替える。
func stubRender(t *testing.T, fn func(context.Context, render.Options) (*render.Result, error)) {
	t.Helper()
	original := runRender
	runRender = fn
	t.Cleanup(func() { runRender = original })
}

// fakeRenderResult は render が成功したときの戻り値。
func fakeRenderResult(dir, outPath string) *render.Result {
	if outPath == "" {
		outPath = filepath.Join("out", filepath.Base(filepath.Clean(dir))+".mp4")
	}
	return &render.Result{
		Build: &build.Result{
			Layout: &project.Layout{
				Dir:       dir,
				OutDir:    filepath.Join(dir, project.OutDirName),
				PropsPath: filepath.Join(dir, project.OutDirName, props.FileName),
			},
			Props:       &props.Props{Meta: props.Meta{FPS: 30, DurationInFrames: 330}},
			Synthesized: 3,
			Cached:      1,
		},
		OutPath: outPath,
	}
}

func TestRender_成功すれば動画のパスを標準出力へ出す(t *testing.T) {
	stubRender(t, func(context.Context, render.Options) (*render.Result, error) {
		return fakeRenderResult("videos/ep01", ""), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"render", "videos/ep01"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}

	want := filepath.Join("out", "ep01.mp4") + "\n"
	if stdout.String() != want {
		t.Errorf("標準出力が違う: %q (want %q)", stdout.String(), want)
	}
	for _, s := range []string{"動画を書き出しました", "out/ep01.mp4"} {
		if !strings.Contains(stderr.String(), s) {
			t.Errorf("要約に %q が含まれない: %s", s, stderr.String())
		}
	}
}

func TestRender_オプションが本体へ伝わる(t *testing.T) {
	var got render.Options
	stubRender(t, func(_ context.Context, opts render.Options) (*render.Result, error) {
		got = opts
		return fakeRenderResult("videos/ep01", "out/custom.mp4"), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"render", "videos/ep01",
		"-o", "out/custom.mp4",
		"-c", "h264",
		"--crf", "18",
		"--voicevox-url=http://192.168.0.2:50021",
		"--no-cache",
	}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	if got.Dir != "videos/ep01" {
		t.Errorf("動画ディレクトリが渡っていない: %q", got.Dir)
	}
	if got.Out != "out/custom.mp4" {
		t.Errorf("出力先が渡っていない: %q", got.Out)
	}
	if got.Codec != "h264" {
		t.Errorf("コーデックが渡っていない: %q", got.Codec)
	}
	if got.CRF == nil || *got.CRF != 18 {
		t.Errorf("CRF が渡っていない: %v", got.CRF)
	}
	if got.VoicevoxURL != "http://192.168.0.2:50021" {
		t.Errorf("接続先が渡っていない: %q", got.VoicevoxURL)
	}
	if !got.NoCache {
		t.Error("--no-cache が渡っていない")
	}
}

func TestRender_ディレクトリを指定しなければ使い方を出す(t *testing.T) {
	stubRender(t, func(context.Context, render.Options) (*render.Result, error) {
		t.Error("引数が無いのに render が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"render"}, &stdout, &stderr)

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

func TestRender_ディレクトリは1つだけ(t *testing.T) {
	stubRender(t, func(context.Context, render.Options) (*render.Result, error) {
		t.Error("引数が多いのに render が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"render", "videos/ep01", "videos/ep02"}, &stdout, &stderr)

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

func TestRender_失敗すれば終了コードが0以外(t *testing.T) {
	stubRender(t, func(context.Context, render.Options) (*render.Result, error) {
		return nil, errors.New("Remotion のレンダリングに失敗しました")
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"render", "videos/ep01"}, &stdout, &stderr)

	if code != exitFailure {
		t.Errorf("終了コードが違う: %d", code)
	}
	if !strings.Contains(stderr.String(), "Remotion のレンダリングに失敗しました") {
		t.Errorf("失敗の理由が出ていない: %s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("失敗なのに標準出力へ書いている: %s", stdout.String())
	}
}

func TestRender_helpは成功する(t *testing.T) {
	stubRender(t, func(context.Context, render.Options) (*render.Result, error) {
		t.Error("--help なのに render が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"render", "--help"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Errorf("--help の終了コードが 0 でない: %d", code)
	}
	out := stdout.String()
	for _, want := range []string{"scenaremo render", "--out", "--codec", "--crf", "--voicevox-url", "--no-cache", "--quiet"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が含まれない: %s", want, out)
		}
	}
}

func TestRoot_コマンド一覧にrenderが載る(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run(context.Background(), []string{"help"}, &stdout, &stderr)

	if !strings.Contains(stdout.String(), "render") {
		t.Errorf("コマンド一覧に render が無い: %s", stdout.String())
	}
}
