package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takiren/scenaremo/internal/project"
)

// stubEject は eject の本体を差し替える。テストが実際のファイル書き出しに左右されないようにするため。
func stubEject(t *testing.T, fn func(string) (*project.EjectResult, error)) {
	t.Helper()
	original := runEject
	runEject = fn
	t.Cleanup(func() { runEject = original })
}

func fakeEjectResult(dir string) *project.EjectResult {
	rendererDir := filepath.Join(dir, "renderer")
	return &project.EjectResult{
		Dir:         dir,
		RendererDir: rendererDir,
		Created: []string{
			filepath.Join(rendererDir, "package.json"),
			filepath.Join(rendererDir, "src", "index.ts"),
		},
	}
}

func TestEject_成功すればrendererディレクトリを標準出力へ出す(t *testing.T) {
	stubEject(t, func(dir string) (*project.EjectResult, error) {
		return fakeEjectResult(dir), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"eject", "videos/ep01"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	// 標準出力は次の工程へ渡せる 1 行だけ。init や build と同じ約束。
	want := filepath.Join("videos/ep01", "renderer") + "\n"
	if stdout.String() != want {
		t.Errorf("標準出力が違う: %q (want %q)", stdout.String(), want)
	}
}

func TestEject_作ったものと次の一手は標準エラーへ出す(t *testing.T) {
	stubEject(t, func(dir string) (*project.EjectResult, error) {
		return fakeEjectResult(dir), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"eject", "videos/ep01"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	msg := stderr.String()
	// pnpm install を促さないと、切り出した直後に studio も render も動かせない。
	for _, want := range []string{"package.json", "pnpm install"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q が含まれない: %s", want, msg)
		}
	}
	if strings.Contains(stdout.String(), "package.json") {
		t.Errorf("一覧が標準出力へ混ざっている: %s", stdout.String())
	}
}

func TestEject_ディレクトリを指定しなければ使い方を出す(t *testing.T) {
	stubEject(t, func(string) (*project.EjectResult, error) {
		t.Error("引数が無いのに eject が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"eject"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Error("引数が無いのに成功として終了した")
	}
	msg := stderr.String()
	// 例が別コマンド（build 等）の使い方をそのまま流用していないか。打ったのは eject なので、
	// 例も eject でなければ打ち直せない。
	for _, want := range []string{"使い方:", "scenaremo eject videos/ep01"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q が含まれない: %s", want, msg)
		}
	}
}

func TestEject_ディレクトリは1つだけ(t *testing.T) {
	stubEject(t, func(string) (*project.EjectResult, error) {
		t.Error("引数が多いのに eject が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"eject", "videos/ep01", "videos/ep02"}, &stdout, &stderr)

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

func TestEject_失敗すれば終了コードが0以外(t *testing.T) {
	stubEject(t, func(string) (*project.EjectResult, error) {
		return nil, errors.New("videos/ep01 には既に renderer/ があります")
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"eject", "videos/ep01"}, &stdout, &stderr)

	if code != exitFailure {
		t.Errorf("終了コードが違う: %d", code)
	}
	if !strings.Contains(stderr.String(), "既に renderer/ があります") {
		t.Errorf("失敗の理由が出ていない: %s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("失敗なのに標準出力へ書いている: %s", stdout.String())
	}
	if strings.Contains(stderr.String(), "使い方:") {
		t.Errorf("使い方の誤りでないのに usage が出ている: %s", stderr.String())
	}
}

func TestEject_helpは成功する(t *testing.T) {
	stubEject(t, func(string) (*project.EjectResult, error) {
		t.Error("--help なのに eject が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"eject", "--help"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Errorf("--help の終了コードが 0 でない: %d", code)
	}
	out := stdout.String()
	for _, want := range []string{"scenaremo eject", "renderer"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が含まれない: %s", want, out)
		}
	}
	if strings.Contains(out, "[flags]") {
		t.Errorf("使い方に英語が混ざっている: %s", out)
	}
}

func TestRoot_コマンド一覧にejectが載る(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run(context.Background(), []string{"help"}, &stdout, &stderr)

	if !strings.Contains(stdout.String(), "eject") {
		t.Errorf("コマンド一覧に eject が無い: %s", stdout.String())
	}
}
