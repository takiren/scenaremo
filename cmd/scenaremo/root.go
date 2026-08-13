package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// usageTemplate は cobra の usage を日本語にしたもの。
//
// 既定のテンプレートは英語で、この CLI が対象にする利用者（→ README「ロードマップ」Phase 3）とは合わない。
// 差し替えは root にだけ行えばよい。子コマンドは親のテンプレートをたどって使う。
//
// 別名・グループ・追加ヘルプトピックは使っていないので、その分岐は持たせていない。
// 使い始めたらここへ足すこと。
const usageTemplate = `使い方:{{if .HasAvailableSubCommands}}
  {{.CommandPath}} <コマンド> [オプション]{{else}}
  {{.UseLine}}{{if .HasAvailableLocalFlags}} [オプション]{{end}}{{end}}{{if .HasExample}}

例:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

コマンド:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

オプション:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

共通のオプション:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

各コマンドの詳しい使い方は "{{.CommandPath}} <コマンド> --help" で見られます。{{end}}
`

// newRootCommand は scenaremo コマンドを組み立てる。
//
// サブコマンドを足すときは new<名前>Command() を 1 ファイル書き、ここへ 1 行足す
// （build → #13、init → #14、credits → #16、render → #18、preview → #19、eject → #22、speakers → #23）。
// コマンドごとの引数やフラグはそのファイルの中で完結させ、ここには配線だけを置く。
func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "scenaremo",
		Short: "台本 (YAML) から解説動画を作る",
		Long: "scenaremo は台本 (YAML) から VOICEVOX で音声を合成し、Remotion で解説動画を書き出す CLI です。\n" +
			"まずは scenaremo doctor で前提条件が揃っているか確かめてください。",

		// エラーの表示と usage の出し分けは run が行う（→ usageError）。
		SilenceErrors: true,
		SilenceUsage:  true,

		// 引数だけ渡されても黙って何もしないより、知らないコマンドだと伝えたほうが早い。
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return &usageError{fmt.Errorf("知らないコマンドです: %s", args[0])}
			}
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			// 何も指定されていない状態を成功で終えると、スクリプトの書き間違いに気づけない。
			return &usageError{errors.New("コマンドを指定してください")}
		},
	}

	root.SetUsageTemplate(usageTemplate)
	// フラグの誤りは使い方の誤りなので、usage を添えて返す種類のエラーに包む。
	// 子コマンドは親をたどってこの関数を使う。
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err}
	})
	// 既定の -h は "help for scenaremo" という英語の説明を持つので、先に日本語で定義して差し替える。
	root.PersistentFlags().BoolP("help", "h", false, "使い方を表示する")
	// シェル補完は Phase 1 の利用者には不要で、英語のコマンドが一覧に混ざる分だけ邪魔になる。
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetHelpCommand(newHelpCommand(root))

	root.AddCommand(newBuildCommand())
	root.AddCommand(newDoctorCommand())
	root.AddCommand(newInitCommand())

	return root
}

// newHelpCommand は日本語の help コマンドを組み立てる。cobra の既定は英語のため差し替える。
func newHelpCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:                   "help [コマンド]",
		Short:                 "コマンドの使い方を表示する",
		DisableFlagsInUseLine: true,
		RunE: func(_ *cobra.Command, args []string) error {
			target, rest, err := root.Find(args)
			if err != nil || len(rest) > 0 {
				return &usageError{fmt.Errorf("知らないコマンドです: %s", strings.Join(args, " "))}
			}
			target.InitDefaultHelpFlag()
			return target.Help()
		},
	}
}

// noArgs は引数を取らないコマンドの検証。
// cobra.NoArgs ではなく自前で持つのは、メッセージを日本語にするためと、
// これが usage を添えるべき誤りだと型で示すため。
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return &usageError{fmt.Errorf("%s は引数を取りません: %s", cmd.CommandPath(), strings.Join(args, " "))}
	}
	return nil
}
