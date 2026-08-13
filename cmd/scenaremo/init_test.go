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

// stubInit は init の本体を差し替える。
// テストが実際のファイル書き出しに左右されないようにするため。
func stubInit(t *testing.T, fn func(string) (*project.InitResult, error)) {
	t.Helper()
	original := runInit
	runInit = fn
	t.Cleanup(func() { runInit = original })
}

// fakeInitResult は init が成功したときの戻り値。
func fakeInitResult(dir string) *project.InitResult {
	return &project.InitResult{
		Dir:        dir,
		ScriptPath: filepath.Join(dir, "script.yaml"),
		Created: []string{
			filepath.Join(dir, "assets", "01-title.png"),
			filepath.Join(dir, "assets", "02-overview.png"),
			filepath.Join(dir, "script.yaml"),
		},
		SchemaRef: "https://example.com/schema.json",
	}
}

func TestInit_成功すれば作った台本のパスを標準出力へ出す(t *testing.T) {
	stubInit(t, func(dir string) (*project.InitResult, error) {
		return fakeInitResult(dir), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "videos/ep01"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	// 標準出力は次の工程へ渡せる 1 行だけ。$(scenaremo init videos/ep01) でエディタへ渡せるようにしておく。
	want := filepath.Join("videos/ep01", "script.yaml") + "\n"
	if stdout.String() != want {
		t.Errorf("標準出力が違う: %q (want %q)", stdout.String(), want)
	}
}

func TestInit_作ったものと次の一手は標準エラーへ出す(t *testing.T) {
	stubInit(t, func(dir string) (*project.InitResult, error) {
		return fakeInitResult(dir), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "videos/ep01"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	msg := stderr.String()
	// 画像の置き場所が分からなければ差し替えようがない。
	for _, want := range []string{"videos/ep01", "01-title.png", "02-overview.png"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q が含まれない: %s", want, msg)
		}
	}
	// init だけで終わる作業は無いので、次に何をすればよいかまで書く。
	if !strings.Contains(msg, "scenaremo build videos/ep01") {
		t.Errorf("次の一手が書かれていない: %s", msg)
	}
	// 一覧は成果物ではないので標準出力へは混ぜない。
	if strings.Contains(stdout.String(), "01-title.png") {
		t.Errorf("一覧が標準出力へ混ざっている: %s", stdout.String())
	}
}

// TestInit_触らなかったファイルも知らせる は、既にあったものを黙って飛ばさないことを確かめる。
// 黙っていると、雛形の画像に差し替わったものとして扱われる。
func TestInit_触らなかったファイルも知らせる(t *testing.T) {
	stubInit(t, func(dir string) (*project.InitResult, error) {
		res := fakeInitResult(dir)
		res.Created = []string{res.ScriptPath}
		res.Skipped = []string{filepath.Join(dir, "assets", "01-title.png")}
		return res, nil
	})

	var stdout, stderr bytes.Buffer
	run(context.Background(), []string{"init", "videos/ep01"}, &stdout, &stderr)

	msg := stderr.String()
	for _, want := range []string{"既にあった", "01-title.png"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q が含まれない: %s", want, msg)
		}
	}
}

func TestInit_ディレクトリを指定しなければ使い方を出す(t *testing.T) {
	stubInit(t, func(string) (*project.InitResult, error) {
		t.Error("引数が無いのに init が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Error("引数が無いのに成功として終了した")
	}
	msg := stderr.String()
	for _, want := range []string{"使い方:", "scenaremo init videos/ep01"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q が含まれない: %s", want, msg)
		}
	}
	// 打たれたのは init なので、例に build を出さない。
	if strings.Contains(msg, "scenaremo build") {
		t.Errorf("別のコマンドの例を見せている: %s", msg)
	}
}

func TestInit_ディレクトリは1つだけ(t *testing.T) {
	stubInit(t, func(string) (*project.InitResult, error) {
		t.Error("引数が多いのに init が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "videos/ep01", "videos/ep02"}, &stdout, &stderr)

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

func TestInit_失敗すれば終了コードが0以外(t *testing.T) {
	stubInit(t, func(string) (*project.InitResult, error) {
		return nil, errors.New("videos/ep01 には既に script.yaml があります")
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "videos/ep01"}, &stdout, &stderr)

	if code != exitFailure {
		t.Errorf("終了コードが違う: %d", code)
	}
	if !strings.Contains(stderr.String(), "既に script.yaml があります") {
		t.Errorf("失敗の理由が出ていない: %s", stderr.String())
	}
	// 失敗したのに台本のパスを出すと、次の工程が空振りする。
	if stdout.Len() != 0 {
		t.Errorf("失敗なのに標準出力へ書いている: %s", stdout.String())
	}
	// 上書きを断ったのは使い方の誤りではないので、使い方は突きつけない。
	if strings.Contains(stderr.String(), "使い方:") {
		t.Errorf("使い方の誤りでないのに usage が出ている: %s", stderr.String())
	}
}

func TestInit_helpは成功する(t *testing.T) {
	stubInit(t, func(string) (*project.InitResult, error) {
		t.Error("--help なのに init が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "--help"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Errorf("--help の終了コードが 0 でない: %d", code)
	}
	out := stdout.String()
	for _, want := range []string{"scenaremo init", "script.yaml", "assets/"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が含まれない: %s", want, out)
		}
	}
	// 使い方の行に cobra 既定の英語 ([flags]) を混ぜない。
	if strings.Contains(out, "[flags]") {
		t.Errorf("使い方に英語が混ざっている: %s", out)
	}
}

func TestRoot_コマンド一覧にinitが載る(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run(context.Background(), []string{"help"}, &stdout, &stderr)

	if !strings.Contains(stdout.String(), "init") {
		t.Errorf("コマンド一覧に init が無い: %s", stdout.String())
	}
}
