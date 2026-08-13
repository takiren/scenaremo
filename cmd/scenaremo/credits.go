package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/takiren/scenaremo/internal/build"
	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/tts"
)

// runCredits はクレジット集計の本体。
// テストが実機の VOICEVOX や台本の有無に左右されないよう、差し替えられる形にしている。
var runCredits = build.Credits

// newCreditsCommand は使用話者のクレジット表記を出すコマンドを組み立てる。
func newCreditsCommand() *cobra.Command {
	var (
		voicevoxURL string
		asJSON      bool
	)

	cmd := &cobra.Command{
		Use:   "credits <ディレクトリ>",
		Short: "使用話者のクレジット表記を出力する",
		Long: "台本で使われている話者を集計し、そのまま貼れるクレジット表記を出力します。\n" +
			"音声は合成しないので、エンジンへ問い合わせるのは話者一覧だけです。\n" +
			"VOICEVOX は音声ライブラリごとにクレジット表記を求めており、表記漏れは事故に直結します。\n\n" +
			"クレジット表記は標準出力へ1行ずつ、補足は標準エラー出力へ出ます。",
		Example: "  scenaremo credits videos/ep01\n" +
			"  scenaremo credits videos/ep01 --json",
		Args:                  exactlyOneDir,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := runCredits(cmd.Context(), build.CreditsOptions{
				Dir:         args[0],
				VoicevoxURL: voicevoxURL,
				Color:       colorize(cmd.ErrOrStderr()),
			})
			if err != nil {
				// 台本の検証エラーの扱いは build と同じ。同じ台本の同じ誤りが、
				// コマンドによって違う見た目で報告されるようでは直し方を覚えられない（→ reportBuildError）。
				return reportBuildError(cmd.ErrOrStderr(), err)
			}
			if len(res.Credits.Entries) == 0 {
				// 話者が1人も居ない台本はここへ来る前に落ちるので、これは配線の誤りでしか起きない。
				// それでも確かめるのは、黙って何も出さないと「クレジットは要らない」と読めてしまい、
				// 表記漏れを防ぐというこのコマンドの目的がちょうど裏返るためである。
				return errors.New("クレジットに載せる話者が1人も見つかりませんでした" +
					"（scenaremo の不具合です。issue で報告してください）")
			}

			if err := writeCredits(cmd.OutOrStdout(), res.Credits.Entries, asJSON); err != nil {
				return err
			}
			printCreditsNote(cmd.ErrOrStderr(), len(res.Credits.Entries))
			return nil
		},
	}

	// 既定のポートで動かしていない利用者にとっては、接続先を変えられないとクレジットを引けない。
	cmd.Flags().StringVar(&voicevoxURL, "voicevox-url", tts.DefaultBaseURL(tts.EngineVoicevox), "VOICEVOX ENGINE の URL")
	cmd.Flags().BoolVar(&asJSON, "json", false, "話者 UUID や使用したスタイル ID も含めて JSON で出力する")

	return cmd
}

// writeCredits はクレジット表記を標準出力へ書く。
//
// 既定を「1行に1表記だけ」にしてあるのは、最も多い使い道が概要欄への貼り付けだからである。
// 番号や見出しを添えると、貼ったあとに人が消して回ることになる。
// この形はそのまま機械にも渡せるので、貼る人と grep する人で出力を分ける必要がない。
func writeCredits(w io.Writer, entries []props.Entry, asJSON bool) error {
	if asJSON {
		// JSON の形は props.json の credits.entries と同じにする。同じものを2通りに表すと、
		// 台本を変えたときにどちらへ追従すればよいのかが分からなくなる。
		// スタイル ID や UUID を必要とする道具のために、合成せずにそれを取れる口をここに残しておく。
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return fmt.Errorf("クレジットを JSON にできませんでした: %w", err)
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	}

	for _, e := range entries {
		if _, err := fmt.Fprintln(w, e.Text); err != nil {
			return err
		}
	}
	return nil
}

// printCreditsNote は集計の結果に人向けの1行を添える。
//
// 標準出力へ混ぜないのは、そこが概要欄へそのまま貼る文字列だからである（→ build の成果物のパスと同じ扱い）。
// 件数を書くのは、台本に居るはずの話者が抜けていないかを利用者がその場で数え合わせられるようにするため。
// 規約そのものをここに要約はしない。音声ライブラリごとに違うものを1行にまとめると、
// 読んだ人がそれで確認を済ませてしまう。
func printCreditsNote(w io.Writer, n int) {
	// 書き出しに失敗しても、報告先がもう無いので握り潰すほかない（→ main.go の同じ扱い）。
	_, _ = fmt.Fprintf(w, "使用した音声ライブラリ %d 件のクレジット表記です。"+
		"公開する動画には必ず記載してください（規約は音声ライブラリごとに異なります）\n", n)
}
