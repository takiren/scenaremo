package main

import (
	"github.com/spf13/cobra"

	"github.com/takiren/scenaremo/internal/build"
	"github.com/takiren/scenaremo/internal/progress"
	"github.com/takiren/scenaremo/internal/tts"
)

// runPreview は preview の本体。
// テストが実機の VOICEVOX や pnpm に左右されないよう、差し替えられる形にしている（→ preview_test.go）。
var runPreview = build.Preview

// newPreviewCommand は Remotion Studio を起動してプレビューするコマンドを組み立てる（→ issue #19）。
func newPreviewCommand() *cobra.Command {
	var (
		voicevoxURL string
		noCache     bool
		quiet       bool
	)

	cmd := &cobra.Command{
		Use:   "preview <ディレクトリ>",
		Short: "Remotion Studio を起動し、動画をリアルタイムにプレビューする",
		Long: "音声を合成して .scenaremo/props.json を出力したうえで、Remotion Studio を起動します。\n" +
			"台本を書き直しながらブラウザで見た目と音声をリアルタイムに確認できます。\n\n" +
			"Ctrl-C で終了します。",
		Example: "  scenaremo preview videos/ep01\n" +
			"  scenaremo preview videos/ep01 --no-cache",
		Args:                  exactlyOneDir,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var reporter progressReporter = progress.Discard
			if !quiet {
				reporter = progress.New(cmd.ErrOrStderr())
			}

			_, err := runPreview(cmd.Context(), build.PreviewOptions{
				Dir:         args[0],
				VoicevoxURL: voicevoxURL,
				NoCache:     noCache,
				GeneratedBy: "scenaremo " + version,
				Color:       colorize(cmd.ErrOrStderr()),
				Reporter:    reporter,
			})
			if err != nil {
				reporter.End()
				return reportBuildError(cmd.ErrOrStderr(), err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&voicevoxURL, "voicevox-url", tts.DefaultBaseURL(tts.EngineVoicevox), "VOICEVOX ENGINE の URL")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "キャッシュを使わず、すべてのセリフを合成し直す")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "進捗を表示しない")

	return cmd
}
