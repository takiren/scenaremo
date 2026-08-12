package main

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/takiren/scenaremo/internal/doctor"
	"github.com/takiren/scenaremo/internal/tts"
)

// runDiagnostics は診断の本体。
// テストが実機の node や VOICEVOX の起動状態に左右されないよう、差し替えられる形にしている。
var runDiagnostics = doctor.Run

// newDoctorCommand は前提条件の診断コマンドを組み立てる。
func newDoctorCommand() *cobra.Command {
	var voicevoxURL string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Node / VOICEVOX / 依存関係を診断する",
		Long: "Node.js / pnpm / renderer の依存 / VOICEVOX ENGINE / 書き込み権限を診断します。\n" +
			"対処が必要な項目があると終了コードは 1 になるので、\n" +
			"scenaremo doctor && scenaremo render videos/ep01 のように繋げて使えます。",
		Args:                  noArgs,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report := runDiagnostics(cmd.Context(), doctor.Options{VoicevoxURL: voicevoxURL})
			if _, err := io.WriteString(cmd.OutOrStdout(), report.Text()); err != nil {
				return err
			}
			if !report.OK() {
				// 何を直せばよいかは診断結果に書いてある。ここで返すのは終了コードのためだけで、
				// 使い方を間違えたわけではないので usage も出さない（→ usageError）。
				return errReported
			}
			return nil
		},
	}

	// 既定のポートで動かしていない利用者にとっては、接続先を変えられないと診断結果そのものが誤りになる。
	cmd.Flags().StringVar(&voicevoxURL, "voicevox-url", tts.DefaultBaseURL(tts.EngineVoicevox), "VOICEVOX ENGINE の URL")

	return cmd
}
