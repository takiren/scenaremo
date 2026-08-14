package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	scenaremo "github.com/takiren/scenaremo"
)

// EjectResult は Eject が動画ディレクトリへ何をしたかを表す。
type EjectResult struct {
	Dir         string   // 渡された動画ディレクトリ（Resolve と同じく形をそのまま引き継ぐ）
	RendererDir string   // <Dir>/renderer
	Created     []string // 実際に書き出したファイルのパス
}

// Eject は dir に共有 renderer のソースをコピーして独立させる。
func Eject(dir string) (*EjectResult, error) {
	if dir == "" {
		return nil, errors.New("動画ディレクトリが指定されていません")
	}
	clean := filepath.Clean(dir)
	rendererDir := filepath.Join(clean, "renderer")

	if _, err := os.Stat(rendererDir); err == nil {
		return nil, fmt.Errorf("%s には既に renderer/ があります", dir)
	}

	src, err := fs.Sub(scenaremo.Renderer, "renderer")
	if err != nil {
		return nil, fmt.Errorf("雛形を取り出せません: %w", err)
	}

	if err := os.MkdirAll(rendererDir, 0o755); err != nil {
		return nil, fmt.Errorf("%s を作れません: %w", rendererDir, err)
	}

	res := &EjectResult{Dir: clean, RendererDir: rendererDir}
	if err := fs.WalkDir(src, ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		dest := filepath.Join(rendererDir, filepath.FromSlash(name))
		if d.IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return fmt.Errorf("%s を作れません: %w", dest, err)
			}
			return nil
		}

		data, err := fs.ReadFile(src, name)
		if err != nil {
			return fmt.Errorf("雛形 %s を読めません: %w", name, err)
		}

		created, err := writeNew(dest, data)
		if err != nil {
			return err
		}
		if created {
			res.Created = append(res.Created, dest)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return res, nil
}
