package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/takiren/scenaremo/internal/build"
	"github.com/takiren/scenaremo/internal/project"
	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/script"
)

// stubPreview は preview の本体を差し替える。
// テストが実機の VOICEVOX や pnpm に左右されないようにするため（→ stubBuild と同じ考え）。
func stubPreview(t *testing.T, fn func(context.Context, build.PreviewOptions) (*build.PreviewResult, error)) {
	t.Helper()
	original := runPreview
	runPreview = fn
	t.Cleanup(func() { runPreview = original })
}

// fakePreviewResult は preview が成功したときの戻り値。
func fakePreviewResult(dir string) *build.PreviewResult {
	return &build.PreviewResult{
		Layout:      &project.Layout{Dir: dir},
		Props:       &props.Props{Meta: props.Meta{FPS: 30, DurationInFrames: 330}},
		RendererDir: "renderer",
	}
}

func TestPreview_動画ディレクトリを渡すと起動する(t *testing.T) {
	var got build.PreviewOptions
	stubPreview(t, func(_ context.Context, opts build.PreviewOptions) (*build.PreviewResult, error) {
		got = opts
		return fakePreviewResult(opts.Dir), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"preview", "videos/ep01"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	if got.Dir != "videos/ep01" {
		t.Errorf("動画ディレクトリが渡っていない: %q", got.Dir)
	}
	// どの版が吐いた props.json なのかを後から特定できるようにする（→ build と同じ）。
	if !strings.Contains(got.GeneratedBy, "scenaremo") {
		t.Errorf("生成者が渡っていない: %q", got.GeneratedBy)
	}
}

func TestPreview_動画ディレクトリが無ければ使い方を出す(t *testing.T) {
	stubPreview(t, func(context.Context, build.PreviewOptions) (*build.PreviewResult, error) {
		t.Fatal("引数が足りないのに本体が呼ばれた")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"preview"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Fatal("引数が足りないのに成功している")
	}
	if !strings.Contains(stderr.String(), "使い方:") {
		t.Errorf("使い方が出ていない: %s", stderr.String())
	}
}

func TestPreview_動画ディレクトリを2つ以上渡せない(t *testing.T) {
	stubPreview(t, func(context.Context, build.PreviewOptions) (*build.PreviewResult, error) {
		t.Fatal("引数が多いのに本体が呼ばれた")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"preview", "videos/ep01", "videos/ep02"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Fatal("引数が多いのに成功している")
	}
}

func TestPreview_フラグが本体まで届く(t *testing.T) {
	var got build.PreviewOptions
	stubPreview(t, func(_ context.Context, opts build.PreviewOptions) (*build.PreviewResult, error) {
		got = opts
		return fakePreviewResult(opts.Dir), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"preview", "videos/ep01",
		"--voicevox-url", "http://127.0.0.1:60000",
		"--no-cache",
	}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	if got.VoicevoxURL != "http://127.0.0.1:60000" {
		t.Errorf("接続先が届いていない: %q", got.VoicevoxURL)
	}
	if !got.NoCache {
		t.Error("--no-cache が届いていない")
	}
}

// TestPreview_台本の検証エラーはそのまま出す は、同じ誤りが build と違う見た目で
// 報告されないことを固定する。直し方を 2 通り覚えさせないため。
func TestPreview_台本の検証エラーはそのまま出す(t *testing.T) {
	scriptErr := &script.Error{
		Filename: "videos/ep01/script.yaml",
		Issues: []script.Issue{{
			Path:    "meta.title",
			Line:    3,
			Message: "title は必須です",
			Hint:    "meta.title に動画のタイトルを書いてください",
		}},
	}
	stubPreview(t, func(context.Context, build.PreviewOptions) (*build.PreviewResult, error) {
		return nil, scriptErr
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"preview", "videos/ep01"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Fatal("台本が不正なのに成功している")
	}
	// script.Error はそれ自体が整形済みの報告なので、"scenaremo: " を頭に重ねない。
	if strings.Contains(stderr.String(), "scenaremo: videos/ep01") {
		t.Errorf("整形済みの報告に接頭辞を重ねている: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "meta.title") {
		t.Errorf("検証エラーの中身が出ていない: %s", stderr.String())
	}
}

func TestPreview_本体の失敗は終了コードに出る(t *testing.T) {
	stubPreview(t, func(context.Context, build.PreviewOptions) (*build.PreviewResult, error) {
		return nil, errors.New("studio を起動できません")
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"preview", "videos/ep01"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Fatal("失敗したのに終了コードが 0")
	}
	if !strings.Contains(stderr.String(), "studio を起動できません") {
		t.Errorf("失敗の理由が出ていない: %s", stderr.String())
	}
}

// TestPreview_helpに載る は、コマンドが root へ配線されていることを固定する。
func TestPreview_helpに載る(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"help"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("help が失敗した: %d (%s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "preview") {
		t.Errorf("コマンド一覧に preview が無い: %s", stdout.String())
	}
}
