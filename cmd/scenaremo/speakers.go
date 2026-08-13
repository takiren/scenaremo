package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/takiren/scenaremo/internal/tts"
)

func newSpeakersCommand() *cobra.Command {
	var voicevoxURL string

	cmd := &cobra.Command{
		Use:   "speakers",
		Short: "VOICEVOX 等の話者とスタイルIDの一覧を表示する",
		Long: "接続先の音声合成エンジン（デフォルトは VOICEVOX）から話者一覧を取得し、\n" +
			"台本の styleId として指定できる値の一覧を標準出力に表示します。",
		Args:                  noArgs,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := tts.New(tts.EngineVoicevox, tts.WithBaseURL(voicevoxURL))
			if err != nil {
				return err
			}

			speakers, err := client.Speakers(cmd.Context())
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, spk := range speakers {
				_, _ = fmt.Fprintln(out, spk.Name)
				for _, style := range spk.Styles {
					_, _ = fmt.Fprintf(out, "  - %s (%d)\n", style.Name, style.ID)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&voicevoxURL, "voicevox-url", tts.DefaultBaseURL(tts.EngineVoicevox), "VOICEVOX ENGINE の URL")

	return cmd
}
