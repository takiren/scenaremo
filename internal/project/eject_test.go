package project_test

import (
	"os"
	"path/filepath"
	"testing"

	scenaremo "github.com/takiren/scenaremo"
	"github.com/takiren/scenaremo/internal/project"
)

// TestEject_共有rendererを書き出す は、eject が焼き込まれた renderer 一式を
// <dir>/renderer へ実際に展開することを固定する。
//
// package.json をバイト同一で突き合わせるのは、スタブや骨組みだけのコピーで
// 済ませていないことを確かめるため。
func TestEject_共有rendererを書き出す(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "videos", "ep01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("動画ディレクトリを作れない: %v", err)
	}

	res, err := project.Eject(dir)
	if err != nil {
		t.Fatalf("Eject: %v", err)
	}

	wantRendererDir := filepath.Join(dir, "renderer")
	if res.RendererDir != wantRendererDir {
		t.Errorf("RendererDir が違う: got %q, want %q", res.RendererDir, wantRendererDir)
	}
	if res.Dir != dir {
		t.Errorf("Dir が違う: got %q, want %q", res.Dir, dir)
	}
	if len(res.Created) == 0 {
		t.Error("Created が空になっている")
	}

	wantPkg, err := scenaremo.Renderer.ReadFile("renderer/package.json")
	if err != nil {
		t.Fatalf("埋め込まれた package.json を読めない: %v", err)
	}
	gotPkg, err := os.ReadFile(filepath.Join(wantRendererDir, "package.json"))
	if err != nil {
		t.Fatalf("書き出された package.json を読めない: %v", err)
	}
	if string(gotPkg) != string(wantPkg) {
		t.Error("package.json の中身が埋め込みと一致しない")
	}

	for _, rel := range []string{
		"src/index.ts",
		"src/Root.tsx",
		"src/Slideshow.tsx",
		"src/SceneAudio.tsx",
		"src/Credits.tsx",
		"src/schema.ts",
		"src/scenes/registry.ts",
		"src/scenes/DefaultScene.tsx",
		"tsconfig.json",
		"remotion.config.ts",
		"pnpm-lock.yaml",
		"pnpm-workspace.yaml",
	} {
		p := filepath.Join(wantRendererDir, filepath.FromSlash(rel))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s が書き出されていない: %v", rel, err)
		}
	}

	// node_modules はビルドで作られるものなので焼き込まない。焼き込むとバイナリが肥大化するうえ、
	// 展開した内容が pnpm install の結果と食い違ったときに気づけない。
	if _, err := os.Stat(filepath.Join(wantRendererDir, "node_modules")); !os.IsNotExist(err) {
		t.Error("node_modules が作られている")
	}
}

// TestEject_既にrendererがあれば上書きしない は、init の checkTarget と同じ思想
// （人が手を入れたかもしれないものを CLI が黙って潰さない）を eject にも適用する。
func TestEject_既にrendererがあれば上書きしない(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "videos", "ep01")
	rendererDir := filepath.Join(dir, "renderer")
	if err := os.MkdirAll(rendererDir, 0o755); err != nil {
		t.Fatalf("下準備に失敗: %v", err)
	}
	marker := filepath.Join(rendererDir, "MARKER.txt")
	if err := os.WriteFile(marker, []byte("手で書いたファイル"), 0o644); err != nil {
		t.Fatalf("下準備に失敗: %v", err)
	}

	if _, err := project.Eject(dir); err == nil {
		t.Fatal("既に renderer/ があるのにエラーにならなかった")
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("マーカーファイルが消えている: %v", err)
	}
	if string(got) != "手で書いたファイル" {
		t.Error("マーカーファイルの中身が書き換わっている")
	}
	if _, err := os.Stat(filepath.Join(rendererDir, "package.json")); !os.IsNotExist(err) {
		t.Error("既存の renderer/ の中へ package.json が書き足されている")
	}
}

func TestEject_ディレクトリ未指定はエラー(t *testing.T) {
	if _, err := project.Eject(""); err == nil {
		t.Error("ディレクトリ未指定なのにエラーにならなかった")
	}
}

// TestFindRendererDir_eject後は動画ディレクトリ自身のrendererが優先される は、
// eject の本来の目的（この動画だけ共有レンダラから独立させる）が実際に働くことを固定する。
//
// FindRendererDir は自分の直下から親へ向かって探すので、動画ディレクトリの中に
// renderer/ を作るだけで、コードを 1 行も変えずに「その動画だけ独立」が実現するはず。
func TestFindRendererDir_eject後は動画ディレクトリ自身のrendererが優先される(t *testing.T) {
	root := t.TempDir()

	shared := filepath.Join(root, "renderer")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatalf("下準備に失敗: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shared, "package.json"), []byte(`{"name":"shared"}`), 0o644); err != nil {
		t.Fatalf("下準備に失敗: %v", err)
	}

	videoDir := filepath.Join(root, "videos", "ep01")
	if err := os.MkdirAll(videoDir, 0o755); err != nil {
		t.Fatalf("下準備に失敗: %v", err)
	}

	if _, err := project.Eject(videoDir); err != nil {
		t.Fatalf("Eject: %v", err)
	}

	got, ok := project.FindRendererDir(videoDir)
	if !ok {
		t.Fatal("renderer/ を見つけられなかった")
	}
	want := filepath.Join(videoDir, "renderer")
	if got != want {
		t.Errorf("eject した自分の renderer/ より共有 renderer/ を優先している: got %s, want %s", got, want)
	}
}
