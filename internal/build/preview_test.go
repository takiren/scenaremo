package build_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takiren/scenaremo/internal/build"
	"github.com/takiren/scenaremo/internal/tts"
)

// launchRecord は Launcher が呼ばれたときの引数 1 回分。
type launchRecord struct {
	dir  string
	name string
	args []string
}

// recorder は起動を記録するだけの Launcher を返す。
// 実物の pnpm を動かさずに、preview が studio へ何を渡すのかだけを見るため。
func recorder(records *[]launchRecord, err error) build.Launcher {
	return func(_ context.Context, dir, name string, args ...string) error {
		*records = append(*records, launchRecord{dir: dir, name: name, args: args})
		return err
	}
}

// rendererDir は共有レンダラに見えるディレクトリを作る。
// package.json の有無で本物かどうかを判断するので、体裁だけの 1 つで足りる。
func rendererDir(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "renderer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("renderer/ を作れない: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("package.json を置けない: %v", err)
	}
	return dir
}

// argValue は --name=value の形の引数から value を取り出す。
func argValue(args []string, name string) (string, bool) {
	prefix := name + "="
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return strings.TrimPrefix(a, prefix), true
		}
	}
	return "", false
}

// TestPreview_buildしてからstudioを起動する は、このコマンドの存在理由そのものを固定する。
//
// 台本を直して確かめるサイクルを最短にするためのコマンドなので、build と studio の間に
// 利用者が打ち直す手順が挟まってはならない。
func TestPreview_buildしてからstudioを起動する(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	renderer := rendererDir(t, t.TempDir())
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, _ := factory(engine)

	var records []launchRecord
	res, err := build.Preview(context.Background(), build.PreviewOptions{
		Dir:         dir,
		NewEngine:   newEngine,
		RendererDir: renderer,
		Launch:      recorder(&records, nil),
	})
	if err != nil {
		t.Fatalf("preview に失敗した: %v", err)
	}

	// build と同じ成果物が出来ていること。studio は props.json を読んで動く。
	propsPath := filepath.Join(dir, ".scenaremo", "props.json")
	if _, statErr := os.Stat(propsPath); statErr != nil {
		t.Fatalf("props.json が書かれていない: %v", statErr)
	}
	if len(engine.synthesized) == 0 {
		t.Error("合成していない。preview は build を済ませてから studio を上げる")
	}
	if res == nil || res.Props == nil {
		t.Fatal("build の結果が返っていない")
	}
	if res.RendererDir != renderer {
		t.Errorf("使った renderer が違う: %s (want %s)", res.RendererDir, renderer)
	}

	// 起動は 1 回だけ。2 回上げると port を奪い合う。
	if len(records) != 1 {
		t.Fatalf("studio の起動回数が違う: %d (%+v)", len(records), records)
	}
	got := records[0]

	// studio は renderer ディレクトリで動かす。src/index.ts も --public-dir の解決も cwd 基準のため。
	if got.dir != renderer {
		t.Errorf("studio の作業ディレクトリが違う: %s (want %s)", got.dir, renderer)
	}
	if got.name != "pnpm" {
		t.Errorf("起動するコマンドが違う: %s (want pnpm)", got.name)
	}

	joined := strings.Join(got.args, " ")
	for _, want := range []string{"exec", "remotion", "studio", "src/index.ts"} {
		if !strings.Contains(joined, want) {
			t.Errorf("引数に %q が無い: %v", want, got.args)
		}
	}

	// --props は props.json を、--public-dir は動画ディレクトリを指す。
	// 動画ディレクトリを public に差し替えるのが、共有レンダラでアセットを解決する唯一の方法である
	// （→ README「設計方針 7」）。
	gotProps, ok := argValue(got.args, "--props")
	if !ok {
		t.Fatalf("--props が渡されていない: %v", got.args)
	}
	if filepath.Base(gotProps) != "props.json" {
		t.Errorf("--props が props.json を指していない: %s", gotProps)
	}
	gotPublic, ok := argValue(got.args, "--public-dir")
	if !ok {
		t.Fatalf("--public-dir が渡されていない: %v", got.args)
	}
	if filepath.Clean(gotPublic) != filepath.Clean(dir) {
		t.Errorf("--public-dir が動画ディレクトリを指していない: %s (want %s)", gotPublic, dir)
	}
}

// TestPreview_相対パスで呼ばれても絶対パスで渡す は、studio を別のディレクトリで動かすことの帰結を固定する。
//
// studio の作業ディレクトリは renderer であって、利用者が打った場所ではない。
// 打たれた相対パスをそのまま渡すと、renderer から見た別の場所を指してしまう。
func TestPreview_相対パスで呼ばれても絶対パスで渡す(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	renderer := rendererDir(t, t.TempDir())
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, _ := factory(engine)

	// 動画ディレクトリの親へ移り、相対パスで呼ぶ。
	t.Chdir(filepath.Dir(dir))

	var records []launchRecord
	if _, err := build.Preview(context.Background(), build.PreviewOptions{
		Dir:         filepath.Base(dir),
		NewEngine:   newEngine,
		RendererDir: renderer,
		Launch:      recorder(&records, nil),
	}); err != nil {
		t.Fatalf("preview に失敗した: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("studio の起動回数が違う: %d", len(records))
	}

	gotProps, ok := argValue(records[0].args, "--props")
	if !ok {
		t.Fatalf("--props が渡されていない: %v", records[0].args)
	}
	if !filepath.IsAbs(gotProps) {
		t.Errorf("--props が絶対パスでない: %s", gotProps)
	}
	gotPublic, ok := argValue(records[0].args, "--public-dir")
	if !ok {
		t.Fatalf("--public-dir が渡されていない: %v", records[0].args)
	}
	if !filepath.IsAbs(gotPublic) {
		t.Errorf("--public-dir が絶対パスでない: %s", gotPublic)
	}
	if filepath.Base(gotPublic) != filepath.Base(dir) {
		t.Errorf("--public-dir が別の場所を指している: %s (want 末尾 %s)", gotPublic, filepath.Base(dir))
	}
}

// TestPreview_buildに失敗したらstudioを上げない は、失敗の見え方を固定する。
//
// props.json が無いまま studio を上げると、利用者は台本の誤りではなく Remotion 側のエラーを
// 読むことになり、直すべき場所から遠ざかる。
func TestPreview_buildに失敗したらstudioを上げない(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	renderer := rendererDir(t, t.TempDir())
	engine := &fakeEngine{kind: tts.EngineVoicevox, synthErr: errors.New("合成できない")}
	newEngine, _ := factory(engine)

	var records []launchRecord
	_, err := build.Preview(context.Background(), build.PreviewOptions{
		Dir:         dir,
		NewEngine:   newEngine,
		RendererDir: renderer,
		Launch:      recorder(&records, nil),
	})
	if err == nil {
		t.Fatal("build に失敗したのに成功している")
	}
	if len(records) != 0 {
		t.Errorf("build に失敗したのに studio を上げている: %+v", records)
	}
}

// TestPreview_rendererが見つからなければ合成する前に止まる は、リポジトリの外から打たれた場合を固定する。
//
// 探索を合成より先に行う。合成は台本 1 本で数分かかるので、結局 studio を上げられないのなら、
// 全セリフを喋らせ終えたあとではなく始まってすぐに落ちなければ利用者の時間が無駄になる
// （→ Run がクレジットの解決を合成より先に行っているのと同じ理由）。
func TestPreview_rendererが見つからなければ合成する前に止まる(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, _ := factory(engine)

	var records []launchRecord
	// RendererDir を空にすると探しに行く。t.TempDir() の上に共有レンダラは無い。
	_, err := build.Preview(context.Background(), build.PreviewOptions{
		Dir:       dir,
		NewEngine: newEngine,
		Launch:    recorder(&records, nil),
	})
	if err == nil {
		t.Fatal("renderer が無いのに成功している")
	}
	if len(records) != 0 {
		t.Errorf("renderer が無いのに studio を上げている: %+v", records)
	}
	if len(engine.synthesized) != 0 {
		t.Errorf("studio を上げられないと分かっているのに合成している: %d 件", len(engine.synthesized))
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".scenaremo", "props.json")); statErr == nil {
		t.Error("studio を上げられないと分かっているのに props.json を書いている")
	}
	// 次に何をすればよいかが分かる文面であること。
	if !strings.Contains(err.Error(), "renderer") {
		t.Errorf("案内に renderer が出てこない: %v", err)
	}
}

// TestPreview_CtrlCで止めたときは失敗として報告しない は、studio の終わり方を固定する。
//
// studio は利用者が止めるまで動き続けるものなので、止めたことを失敗として報告すると、
// 正常に使い終えるたびに終了コード 1 とエラー行が出ることになる。
func TestPreview_CtrlCで止めたときは失敗として報告しない(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	renderer := rendererDir(t, t.TempDir())
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, _ := factory(engine)

	ctx, cancel := context.WithCancel(context.Background())

	// Ctrl-C を受けた studio は、打ち切られた ctx とともに 0 以外で終わる。
	launch := func(_ context.Context, _, _ string, _ ...string) error {
		cancel()
		return errors.New("signal: interrupt")
	}

	res, err := build.Preview(ctx, build.PreviewOptions{
		Dir:         dir,
		NewEngine:   newEngine,
		RendererDir: renderer,
		Launch:      launch,
	})
	cancel()
	if err != nil {
		t.Fatalf("Ctrl-C を失敗として報告している: %v", err)
	}
	if res == nil {
		t.Fatal("結果が返っていない")
	}
}

// TestPreview_studioが自分から落ちたときは失敗として報告する は、上の裏返しを固定する。
//
// 打ち切っていないのに studio が終わったのなら、それは port の衝突や依存の欠落であって、
// 黙って成功にすると利用者はブラウザが開かない理由を知る手がかりを失う。
func TestPreview_studioが自分から落ちたときは失敗として報告する(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	renderer := rendererDir(t, t.TempDir())
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, _ := factory(engine)

	var records []launchRecord
	_, err := build.Preview(context.Background(), build.PreviewOptions{
		Dir:         dir,
		NewEngine:   newEngine,
		RendererDir: renderer,
		Launch:      recorder(&records, errors.New("exit status 1")),
	})
	if err == nil {
		t.Fatal("studio が落ちたのに成功している")
	}
}
