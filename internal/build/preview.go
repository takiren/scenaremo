package build

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/takiren/scenaremo/internal/project"
	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/synth"
)

// Launcher は Studio のように端末へ張り付いたまま動き続ける外部コマンドを起動する。
// dir は作業ディレクトリ。呼び出しはコマンドが終わるまで返らない（→ issue #19）。
type Launcher func(ctx context.Context, dir, name string, args ...string) error

// PreviewOptions は Preview 1 回分の設定。
//
// Options を使い回さないのは、Preview 独自の設定 (RendererDir, Launch) を持たせ、
// 必要な設定だけを明示するため（→ CreditsOptions と同じ考え）。
type PreviewOptions struct {
	Dir         string
	VoicevoxURL string
	NoCache     bool
	GeneratedBy string
	Color       bool
	Reporter    synth.Reporter
	NewEngine   EngineFactory

	// RendererDir は共有レンダラの場所。空なら探す。
	RendererDir string

	// Launch は Studio の起動。nil なら実物の外部コマンドを起動する。
	Launch Launcher
}

// PreviewResult は Preview の結果。
type PreviewResult struct {
	Layout      *project.Layout
	Props       *props.Props
	RendererDir string
	Synthesized int
	Cached      int
}

// Preview は scenaremo build と同じ手順で props.json を作ったうえで、
// その動画を映した Remotion Studio を起動する（→ issue #19）。
//
// 台本を直して確かめるサイクルを最短にするための関数なので、build と studio の間で
// 利用者が何かを打ち直す必要はない。studio は利用者が Ctrl-C で止めるまで動き続け、
// Ctrl-C で終わったときは失敗として扱わない。
//
// Run や Credits と同じくコマンドの中ではなくここに段取りを置いてあるのは、
// scenaremo render（→ issue #18）や他のツールが同じ手順を再現できるようにするため。
func Preview(ctx context.Context, opts PreviewOptions) (*PreviewResult, error) {
	// renderer ディレクトリの確定を合成より先に行う。
	//
	// 合成は台本 1 本で数分かかるため、結局 studio を上げられないと分かっているのなら、
	// 全セリフを喋らせ終えたあとではなく始まってすぐに落ちなければ利用者の時間が無駄になるためである
	// （→ build.go:105 のクレジット解決と同じ理由）。
	rendererDir := opts.RendererDir
	if rendererDir == "" {
		found, ok := project.FindRendererDir(opts.Dir)
		if !ok {
			return nil, fmt.Errorf("renderer ディレクトリが見つかりません（%s から親をたどって探しました）。"+
				"scenaremo のリポジトリの中（renderer/ がある場所か、その下）へ移動して実行してください", opts.Dir)
		}
		rendererDir = found
	}

	// build.Run と同じ手順で props.json を書く。
	// 失敗した場合は Launch を呼ばずにエラーを返す。props.json の無い状態で studio を上げると、
	// 利用者は台本の誤りではなく Remotion 側のエラーを読むことになり、原因から遠ざかるためである。
	res, err := Run(ctx, Options{
		Dir:         opts.Dir,
		VoicevoxURL: opts.VoicevoxURL,
		NoCache:     opts.NoCache,
		GeneratedBy: opts.GeneratedBy,
		Color:       opts.Color,
		Reporter:    opts.Reporter,
		NewEngine:   opts.NewEngine,
	})
	if err != nil {
		return nil, err
	}

	// studio を renderer ディレクトリで動かすため、--props と --public-dir は絶対パスで渡す
	// （README「設計方針 7」のとおり解決は cwd 基準であるため、相対パスだと別の場所を指す）。
	absProps, err := filepath.Abs(res.Layout.PropsPath)
	if err != nil {
		return nil, fmt.Errorf("props.json の絶対パスを取得できませんでした: %w", err)
	}
	absPublic, err := filepath.Abs(res.Layout.Dir)
	if err != nil {
		return nil, fmt.Errorf("動画ディレクトリの絶対パスを取得できませんでした: %w", err)
	}

	previewRes := &PreviewResult{
		Layout:      res.Layout,
		Props:       res.Props,
		RendererDir: rendererDir,
		Synthesized: res.Synthesized,
		Cached:      res.Cached,
	}

	launch := opts.Launch
	if launch == nil {
		launch = defaultLauncher
	}

	args := []string{
		"exec", "remotion", "studio", "src/index.ts",
		"--props=" + absProps,
		"--public-dir=" + absPublic,
	}

	if err := launch(ctx, rendererDir, "pnpm", args...); err != nil {
		// Studio は利用者が Ctrl-C で止めるまで動き続けるものなので、
		// 打ち切られた ctx とともに終わった場合は失敗として報告してはならない。
		if ctx.Err() != nil {
			return previewRes, nil
		}
		return nil, fmt.Errorf("%s で Remotion Studio を起動できませんでした: %w", rendererDir, err)
	}

	return previewRes, nil
}

// defaultLauncher は実物の pnpm コマンドを起動する。
//
// 標準入出力を親から引き継ぐのは、Studio が端末に出力を出し続けるプロセスだからである。
// 出力を溜め込む形にすると利用者には何も見えない。
func defaultLauncher(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
