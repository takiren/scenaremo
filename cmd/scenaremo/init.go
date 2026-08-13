package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/takiren/scenaremo/internal/project"
)

// runInit は init の本体。
// テストが実際のファイル書き出しに左右されないよう、差し替えられる形にしている。
var runInit = project.Init

// newInitCommand は動画 1 本ぶんの雛形を作るコマンドを組み立てる。
func newInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init <ディレクトリ>",
		Short: "台本の雛形と assets/ を作る",
		Long: "動画 1 本ぶんのディレクトリを作り、台本の雛形 (script.yaml) と画像置き場 (assets/) を用意します。\n" +
			"雛形は CLI に焼き込んであるので、ネットワークにも Node にも繋がずに実行できます。\n\n" +
			"既にあるファイルは上書きしません。作った台本のパスは標準出力へ、\n" +
			"何を作ったかは標準エラー出力へ出ます。",
		Example:               "  scenaremo init videos/ep01",
		Args:                  exactlyOneNewDir,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := runInit(args[0])
			if err != nil {
				return err
			}

			// 作った台本のパスは標準出力へ 1 行だけ。$(scenaremo init videos/ep01) で
			// そのままエディタへ渡せるようにしておく（build が props.json のパスを出すのと同じ約束）。
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), res.ScriptPath); err != nil {
				return err
			}
			printInitSummary(cmd.ErrOrStderr(), res)
			return nil
		},
	}

	return cmd
}

// exactlyOneNewDir は作る場所がちょうど 1 つ指定されていることを確かめる。
//
// build の exactlyOneDir と別に持つのは、例に出すコマンド名を実際に打たれたものへ合わせるため。
// 引数を間違えた人に見せる例が scenaremo build では、そのまま打ち直せない。
func exactlyOneNewDir(cmd *cobra.Command, args []string) error {
	switch {
	case len(args) == 0:
		return &usageError{fmt.Errorf("作る場所を指定してください (例: %s videos/ep01)", cmd.CommandPath())}
	case len(args) > 1:
		// まとめて作れるようにはしていない。黙って最初の 1 つだけを作ると、
		// 残りが無いことに利用者が気づくのは build まで進んだ後になる。
		return &usageError{fmt.Errorf("ディレクトリは1つだけ指定してください: %s", strings.Join(args, " "))}
	}
	return nil
}

// printInitSummary は init が何を作ったかを書く。
//
// 作ったファイルを並べるのは、雛形の画像がどこに置かれたのかを知らないままでは差し替えようがないため。
// 最後に次の一手を添えるのは、init だけで完結する作業が無いからで、
// ここで手が止まると台本の書き方を README から探し直すことになる。
func printInitSummary(w io.Writer, res *project.InitResult) {
	// 書き出しに失敗しても、報告先がもう無いので握り潰すほかない（→ main.go の同じ扱い）。
	_, _ = fmt.Fprintf(w, "%s に雛形を作りました\n", res.Dir)
	for _, path := range res.Created {
		_, _ = fmt.Fprintf(w, "  %s\n", path)
	}
	if len(res.Skipped) > 0 {
		// 触らなかったものを黙っていると、雛形の画像に差し替わったものとして扱われてしまう。
		_, _ = fmt.Fprintln(w, "既にあったので触っていません:")
		for _, path := range res.Skipped {
			_, _ = fmt.Fprintf(w, "  %s\n", path)
		}
	}
	_, _ = fmt.Fprintf(w, "%s を書き換えたら、scenaremo build %s で音声を合成できます\n",
		res.ScriptPath, res.Dir)
}
