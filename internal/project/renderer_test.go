package project_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/takiren/scenaremo/internal/project"
)

// TestFindRendererDir_親をたどって共有レンダラを見つける は、動画ディレクトリから実行しても
// renderer/ にたどり着けることを固定する。
//
// 利用者は videos/ep01 を相手に作業しているので、preview も doctor もそこから打たれる。
// リポジトリの根まで戻らないと動かないのでは、確かめるサイクルを最短にするという目的に届かない。
func TestFindRendererDir_親をたどって共有レンダラを見つける(t *testing.T) {
	root := t.TempDir()
	renderer := filepath.Join(root, "renderer")
	mkdirAll(t, renderer)
	writeFile(t, filepath.Join(renderer, "package.json"), `{"name":"scenaremo-renderer"}`)

	start := filepath.Join(root, "videos", "ep01")
	mkdirAll(t, start)

	got, ok := project.FindRendererDir(start)
	if !ok {
		t.Fatalf("renderer/ を見つけられなかった: start=%s", start)
	}
	if got != renderer {
		t.Errorf("見つけた場所が違う: %s (want %s)", got, renderer)
	}
}

// TestFindRendererDir_自分の直下にあるものも見つける は、リポジトリの根から打った場合を固定する。
func TestFindRendererDir_自分の直下にあるものも見つける(t *testing.T) {
	root := t.TempDir()
	renderer := filepath.Join(root, "renderer")
	mkdirAll(t, renderer)
	writeFile(t, filepath.Join(renderer, "package.json"), `{}`)

	got, ok := project.FindRendererDir(root)
	if !ok {
		t.Fatal("直下の renderer/ を見つけられなかった")
	}
	if got != renderer {
		t.Errorf("見つけた場所が違う: %s (want %s)", got, renderer)
	}
}

// TestFindRendererDir_packagejsonの無いrendererは掴まない は、名前だけが一致する
// ディレクトリを共有レンダラと取り違えないことを固定する。
//
// 掴んでしまうと、そこで pnpm を動かして「remotion が無い」という、原因から遠いエラーが出る。
func TestFindRendererDir_packagejsonの無いrendererは掴まない(t *testing.T) {
	root := t.TempDir()

	// 手前にある紛らわしい renderer/（package.json が無い）。
	decoy := filepath.Join(root, "work", "renderer")
	mkdirAll(t, decoy)

	// 本物はその親にある。
	real := filepath.Join(root, "renderer")
	mkdirAll(t, real)
	writeFile(t, filepath.Join(real, "package.json"), `{}`)

	start := filepath.Join(root, "work", "videos")
	mkdirAll(t, start)

	got, ok := project.FindRendererDir(start)
	if !ok {
		t.Fatal("本物の renderer/ を見つけられなかった")
	}
	if got != real {
		t.Errorf("package.json の無い renderer/ を掴んでいる: %s (want %s)", got, real)
	}
}

// TestFindRendererDir_見つからなければfalse は、根まで戻っても無い場合を固定する。
func TestFindRendererDir_見つからなければfalse(t *testing.T) {
	// t.TempDir() の上には利用者の環境次第で renderer/ がありうるので、
	// 「見つからない」を確かめるには根まで戻っても無いことが保証された場所が要る。
	// ここでは実在しないパスを渡す。Abs は失敗しないので、探索そのものが空振りする経路を通る。
	start := filepath.Join(t.TempDir(), "no-such-dir", "deeper")

	if got, ok := project.FindRendererDir(start); ok {
		t.Errorf("無いはずの renderer/ を見つけたと言っている: %s", got)
	}
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("%s を作れない: %v", dir, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("%s を置けない: %v", path, err)
	}
}
