package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/takiren/scenaremo/internal/progress"
	"github.com/takiren/scenaremo/internal/render"
	"github.com/takiren/scenaremo/internal/tts"
)

// runRender は render の本体。テスト時に差し替えられるようにしている。
var runRender = render.Run

// newRenderCommand は Remotion で mp4 を書き出すコマンドを組み立てる。
func newRenderCommand() *cobra.Command {
	var (
		out         string
		codec       string
		crf         int
		voicevoxURL string
		noCache     bool
		quiet       bool
		parallel    int
	)

	cmd := &cobra.Command{
		Use:   "render <ディレクトリ>",
		Short: "音声を合成し、Remotion で mp4 動画を書き出す",
		Long: "台本から音声を合成し、Remotion で動画 (mp4) を書き出します。\n" +
			"書き出した動画のパスは標準出力へ、進捗は標準エラー出力へ出ます。",
		Example: "  scenaremo render videos/ep01\n" +
			"  scenaremo render videos/ep01 --out out/custom.mp4 --codec h264 --crf 18",
		Args:                  exactlyOneRenderDir,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var reporter progressReporter = progress.Discard
			logWriter := io.Discard
			if !quiet {
				reporter = progress.New(cmd.ErrOrStderr())
				logWriter = cmd.ErrOrStderr()
			}

			var crfPtr *int
			if cmd.Flags().Changed("crf") {
				crfPtr = &crf
			}

			res, err := runRender(cmd.Context(), render.Options{
				Dir:         args[0],
				Out:         out,
				Codec:       codec,
				CRF:         crfPtr,
				VoicevoxURL: voicevoxURL,
				NoCache:     noCache,
				Color:       colorize(cmd.ErrOrStderr()),
				Reporter:    reporter,
				Workers:     parallel,
				Stdout:      logWriter,
				Stderr:      logWriter,
			})
			if err != nil {
				reporter.End()
				return reportBuildError(cmd.ErrOrStderr(), err)
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), res.OutPath); err != nil {
				return err
			}
			printRenderSummary(cmd.ErrOrStderr(), res)
			return nil
		},
	}

	cmd.Flags().StringVarP(&out, "out", "o", "", "出力ファイルパス (既定値: out/<ディレクトリ名>.mp4)")
	cmd.Flags().StringVarP(&codec, "codec", "c", "", "動画・音声のコーデック (h264, h265, vp8, vp9, av1 等)")
	cmd.Flags().IntVar(&crf, "crf", 0, "画質パラメータ CRF (Constant Rate Factor)")
	cmd.Flags().StringVar(&voicevoxURL, "voicevox-url", tts.DefaultBaseURL(tts.EngineVoicevox), "VOICEVOX ENGINE の URL")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "キャッシュを使わず、すべてのセリフを合成し直す")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "進捗を表示しない")
	cmd.Flags().IntVarP(&parallel, "parallel", "p", 1, "音声合成の並列数 (エンジンの負荷とトレードオフ)")

	return cmd
}

// exactlyOneRenderDir は動画ディレクトリがちょうど 1 つ指定されていることを確かめる。
func exactlyOneRenderDir(_ *cobra.Command, args []string) error {
	switch {
	case len(args) == 0:
		return &usageError{errors.New("動画ディレクトリを指定してください (例: scenaremo render videos/ep01)")}
	case len(args) > 1:
		return &usageError{fmt.Errorf("動画ディレクトリは1つだけ指定してください: %s", strings.Join(args, " "))}
	}
	return nil
}

// printRenderSummary は render の結果を 1 行にまとめて書く。
func printRenderSummary(w io.Writer, res *render.Result) {
	_, _ = fmt.Fprintf(w, "動画を書き出しました (%s)\n", res.OutPath)
}
