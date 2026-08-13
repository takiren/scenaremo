package render_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/takiren/scenaremo/internal/build"
	"github.com/takiren/scenaremo/internal/project"
	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/render"
)

func TestRun_Success_DefaultOut(t *testing.T) {
	tmpDir := t.TempDir()
	videoDir := filepath.Join(tmpDir, "ep01")
	if err := os.MkdirAll(videoDir, 0755); err != nil {
		t.Fatal(err)
	}

	// renderer/ ディレクトリと package.json、bin/remotion を作成
	rendererDir := filepath.Join(tmpDir, "renderer")
	binDir := filepath.Join(rendererDir, "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rendererDir, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(binDir, "remotion")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	propsPath := filepath.Join(videoDir, ".scenaremo", "props.json")

	var buildCalled bool
	var mockCommandName string
	var mockCommandArgs []string
	var mockCommandDir string

	opts := render.Options{
		Dir:         videoDir,
		RendererDir: rendererDir,
		BuildRunner: func(ctx context.Context, bOpts build.Options) (*build.Result, error) {
			buildCalled = true
			if bOpts.Dir != videoDir {
				t.Errorf("got Build Dir %q, want %q", bOpts.Dir, videoDir)
			}
			return &build.Result{
				Props: &props.Props{},
				Layout: &project.Layout{
					PropsPath: propsPath,
				},
			}, nil
		},
		CommandRunner: func(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
			mockCommandName = name
			mockCommandArgs = args
			mockCommandDir = dir
			return nil
		},
	}

	res, err := render.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !buildCalled {
		t.Error("expected BuildRunner to be called")
	}

	expectedOutPath := filepath.Join("out", "ep01.mp4")
	if res.OutPath != expectedOutPath {
		t.Errorf("got OutPath %q, want %q", res.OutPath, expectedOutPath)
	}

	if mockCommandName != binPath {
		t.Errorf("got CommandName %q, want %q", mockCommandName, binPath)
	}

	if mockCommandDir != rendererDir {
		t.Errorf("got CommandDir %q, want %q", mockCommandDir, rendererDir)
	}

	// 引数のチェック (render, entrypoint, composition, outPath, --public-dir, --props)
	if len(mockCommandArgs) < 4 {
		t.Fatalf("not enough arguments: %v", mockCommandArgs)
	}
	if mockCommandArgs[0] != "render" {
		t.Errorf("got sub-command %q, want \"render\"", mockCommandArgs[0])
	}
	if mockCommandArgs[3] != expectedOutPath {
		t.Errorf("got output arg %q, want %q", mockCommandArgs[3], expectedOutPath)
	}
	hasPublicDir := false
	hasProps := false
	for _, arg := range mockCommandArgs {
		if arg == "--public-dir="+videoDir {
			hasPublicDir = true
		}
		if arg == "--props="+propsPath {
			hasProps = true
		}
	}
	if !hasPublicDir {
		t.Errorf("missing or incorrect --public-dir flag in %v", mockCommandArgs)
	}
	if !hasProps {
		t.Errorf("missing or incorrect --props flag in %v", mockCommandArgs)
	}
}

func TestRun_Success_CustomOut_Codec_CRF(t *testing.T) {
	tmpDir := t.TempDir()
	videoDir := filepath.Join(tmpDir, "ep02")
	if err := os.MkdirAll(videoDir, 0755); err != nil {
		t.Fatal(err)
	}
	rendererDir := filepath.Join(tmpDir, "renderer")
	binDir := filepath.Join(rendererDir, "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rendererDir, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "remotion"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	customOut := filepath.Join(tmpDir, "output", "custom.mp4")
	crfVal := 18

	var mockCommandArgs []string

	opts := render.Options{
		Dir:         videoDir,
		Out:         customOut,
		Codec:       "h264",
		CRF:         &crfVal,
		RendererDir: rendererDir,
		BuildRunner: func(ctx context.Context, bOpts build.Options) (*build.Result, error) {
			return &build.Result{
				Props: &props.Props{},
				Layout: &project.Layout{
					PropsPath: filepath.Join(videoDir, ".scenaremo", "props.json"),
				},
			}, nil
		},
		CommandRunner: func(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
			mockCommandArgs = args
			return nil
		},
	}

	res, err := render.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.OutPath != customOut {
		t.Errorf("got OutPath %q, want %q", res.OutPath, customOut)
	}

	// 出力先親ディレクトリが作成されたか確認
	if _, err := os.Stat(filepath.Dir(customOut)); err != nil {
		t.Errorf("expected parent directory of customOut to exist: %v", err)
	}

	hasCodec := false
	hasCRF := false
	for _, arg := range mockCommandArgs {
		if arg == "--codec=h264" {
			hasCodec = true
		}
		if arg == "--crf=18" {
			hasCRF = true
		}
	}
	if !hasCodec {
		t.Errorf("missing --codec=h264 in %v", mockCommandArgs)
	}
	if !hasCRF {
		t.Errorf("missing --crf=18 in %v", mockCommandArgs)
	}
}

func TestRun_BuildError(t *testing.T) {
	expectedErr := errors.New("build failed")
	opts := render.Options{
		Dir: "ep01",
		BuildRunner: func(ctx context.Context, bOpts build.Options) (*build.Result, error) {
			return nil, expectedErr
		},
	}

	_, err := render.Run(context.Background(), opts)
	if !errors.Is(err, expectedErr) {
		t.Errorf("got error %v, want %v", err, expectedErr)
	}
}

func TestRun_RendererNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	videoDir := filepath.Join(tmpDir, "sub", "ep01")
	if err := os.MkdirAll(videoDir, 0755); err != nil {
		t.Fatal(err)
	}

	opts := render.Options{
		Dir: videoDir,
		BuildRunner: func(ctx context.Context, bOpts build.Options) (*build.Result, error) {
			return &build.Result{
				Props: &props.Props{},
				Layout: &project.Layout{
					PropsPath: filepath.Join(videoDir, ".scenaremo", "props.json"),
				},
			}, nil
		},
	}

	_, err := render.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error when renderer directory is not found, got nil")
	}
}

func TestRun_RemotionFailure(t *testing.T) {
	tmpDir := t.TempDir()
	videoDir := filepath.Join(tmpDir, "ep01")
	if err := os.MkdirAll(videoDir, 0755); err != nil {
		t.Fatal(err)
	}
	rendererDir := filepath.Join(tmpDir, "renderer")
	binDir := filepath.Join(rendererDir, "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rendererDir, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "remotion"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	cmdErr := errors.New("exit code 1")

	opts := render.Options{
		Dir:         videoDir,
		RendererDir: rendererDir,
		BuildRunner: func(ctx context.Context, bOpts build.Options) (*build.Result, error) {
			return &build.Result{
				Props: &props.Props{},
				Layout: &project.Layout{
					PropsPath: filepath.Join(videoDir, ".scenaremo", "props.json"),
				},
			}, nil
		},
		CommandRunner: func(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
			return cmdErr
		},
	}

	_, err := render.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error from failed command, got nil")
	}
}
