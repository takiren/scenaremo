// Package render は台本から mp4 動画を書き出す機能を提供する。
package render

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/takiren/scenaremo/internal/build"
	"github.com/takiren/scenaremo/internal/synth"
)

// Options は render の実行オプション。
type Options struct {
	Dir         string
	Out         string
	Codec       string
	CRF         *int
	VoicevoxURL string
	NoCache     bool
	Color       bool
	Reporter    synth.Reporter
	Workers     int

	Stdout io.Writer
	Stderr io.Writer

	// テスト用の注入ポイント
	BuildRunner   func(ctx context.Context, opts build.Options) (*build.Result, error)
	CommandRunner func(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
	RendererDir   string
}

// Result は render の処理結果。
type Result struct {
	Build   *build.Result
	OutPath string
}

// Run は build と remotion render を順に実行し、動画を出力する。
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Dir == "" {
		return nil, errors.New("動画ディレクトリを指定してください")
	}

	buildRunner := opts.BuildRunner
	if buildRunner == nil {
		buildRunner = build.Run
	}

	bRes, err := buildRunner(ctx, build.Options{
		Dir:         opts.Dir,
		VoicevoxURL: opts.VoicevoxURL,
		NoCache:     opts.NoCache,
		Color:       opts.Color,
		Reporter:    opts.Reporter,
		Workers:     opts.Workers,
	})
	if err != nil {
		return nil, err
	}

	rendererDir := opts.RendererDir
	if rendererDir == "" {
		found, ok := findRendererDir(opts.Dir)
		if !ok {
			return nil, fmt.Errorf("renderer/ ディレクトリが見つかりません（%s から親をたどって探しました）", opts.Dir)
		}
		rendererDir = found
	}

	outPath := opts.Out
	if outPath == "" {
		cleanDir := filepath.Clean(opts.Dir)
		base := filepath.Base(cleanDir)
		outPath = filepath.Join("out", base+".mp4")
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return nil, fmt.Errorf("出力先ディレクトリの作成に失敗しました: %w", err)
	}

	entryPoint := filepath.Join("src", "index.ts")
	composition := "Slideshow"

	// remotion を renderer ディレクトリで動かすため、パスを受け取る引数はすべて絶対パスで渡す
	// （解決は cwd 基準であるため、相対パスだと renderer/ の下を指してしまう）。
	// とくに --props は「ファイルが無ければ JSON 文字列」と解釈されるため、
	// 相対パスのままだと JSON.parse に失敗したという原因から遠いエラーになる。
	absProps, err := filepath.Abs(bRes.Layout.PropsPath)
	if err != nil {
		return nil, fmt.Errorf("props.json の絶対パスを取得できませんでした: %w", err)
	}
	absPublic, err := filepath.Abs(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("動画ディレクトリの絶対パスを取得できませんでした: %w", err)
	}
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return nil, fmt.Errorf("出力先の絶対パスを取得できませんでした: %w", err)
	}

	args := []string{
		"render",
		entryPoint,
		composition,
		absOut,
		"--public-dir=" + absPublic,
		"--props=" + absProps,
	}

	if opts.Codec != "" {
		args = append(args, "--codec="+opts.Codec)
	}
	if opts.CRF != nil {
		args = append(args, "--crf="+strconv.Itoa(*opts.CRF))
	}

	bin := filepath.Join(rendererDir, "node_modules", ".bin", "remotion")
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(bin + ".cmd"); err == nil {
			bin += ".cmd"
		} else if _, err := os.Stat(bin + ".exe"); err == nil {
			bin += ".exe"
		}
	}

	cmdName := bin
	cmdArgs := args
	if _, err := os.Stat(bin); err != nil {
		cmdName = "npx"
		cmdArgs = append([]string{"remotion"}, args...)
	}

	cmdRunner := opts.CommandRunner
	if cmdRunner == nil {
		cmdRunner = execCommand
	}

	if err := cmdRunner(ctx, cmdName, cmdArgs, rendererDir, opts.Stdout, opts.Stderr); err != nil {
		return nil, fmt.Errorf("remotion のレンダリングに失敗しました: %w", err)
	}

	return &Result{
		Build:   bRes,
		OutPath: outPath,
	}, nil
}

// findRendererDir は start から親をたどって renderer/ を探す。
func findRendererDir(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		candidate := filepath.Join(dir, "renderer")
		if _, err := os.Stat(filepath.Join(candidate, "package.json")); err == nil {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// execCommand は外部コマンドを実行する既定の CommandRunner。
func execCommand(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
