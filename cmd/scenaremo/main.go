// Command scenaremo は台本 (YAML) から解説動画を作る CLI。
//
// このパッケージが持つのはサブコマンドの受け付けと終了コードだけで、処理の中身は internal/ 以下にある。
// 薄く保つのは、CLI としての振る舞い（引数の解釈・出力先・終了コード）を
// 中身のロジックと混ぜずにテストできるようにするためである。
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
)

// 終了コード。診断や検証の失敗を呼び出し元のスクリプトが見分けられるよう、
// 成功以外は必ず 0 以外を返す。
const (
	exitSuccess = 0
	exitFailure = 1
)

// errReported は「利用者向けの説明はコマンド自身が出力済み」であることを表す番兵。
//
// doctor のように失敗の中身を丁寧に出しているコマンドが、そのうしろへ
// "scenaremo: ..." を重ねて出さないために使う。伝えたいのは終了コードだけ、という場合もこれ。
var errReported = errors.New("報告済み")

// command は 1 つのサブコマンド。
//
// build / init / credits を足すときは、この形の値を commands へ 1 つ増やすだけで済む。
// 引数の解釈は各コマンドが自分の flag.FlagSet で行う。サブコマンドより前に共通フラグを置く形にしないのは、
// コマンドごとに必要な設定が違い、共通化した途端にどのコマンドでも使われないフラグが増えるためである。
type command struct {
	// name はサブコマンド名。
	name string
	// summary はヘルプに 1 行で出す説明。
	summary string
	// run は実処理。サブコマンド名を除いた引数を受け取る。
	//
	// 出力先を引数で受けるのは、テストから終了コードと出力の両方を検査できるようにするため。
	// os.Stdout を直に参照するコマンドが 1 つでもあると、この形は崩れる。
	run func(ctx context.Context, args []string, stdout, stderr io.Writer) error
}

// commands は使えるサブコマンド。ヘルプへ出る順もこの並び。
var commands = []command{
	doctorCommand(),
}

func main() {
	// Ctrl-C を context の打ち切りとして扱う。音声合成やレンダリングは分単位で走るので、
	// 止めたときに走らせている HTTP や外部プロセスを道連れに畳めるようにしておく。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run はサブコマンドを解決して実行し、プロセスの終了コードを返す。
// os.Exit を main に閉じ込めているので、テストからは戻り値でそのまま終了コードを確かめられる。
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage())
		return exitFailure
	}

	name := args[0]
	if name == "help" || name == "-h" || name == "--help" {
		fmt.Fprint(stdout, usage())
		return exitSuccess
	}

	cmd, ok := lookup(name)
	if !ok {
		fmt.Fprintf(stderr, "scenaremo: 知らないコマンドです: %s\n\n", name)
		fmt.Fprint(stderr, usage())
		return exitFailure
	}

	if err := cmd.run(ctx, args[1:], stdout, stderr); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintln(stderr, "scenaremo:", err)
		}
		return exitFailure
	}
	return exitSuccess
}

// lookup は名前からサブコマンドを引く。
func lookup(name string) (command, bool) {
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

// usage は使い方の説明を組み立てる。一覧は commands から作るので、
// サブコマンドを足したときにヘルプの更新を忘れることがない。
func usage() string {
	var b strings.Builder
	b.WriteString("scenaremo: 台本 (YAML) から解説動画を作ります\n\n")
	b.WriteString("使い方:\n")
	b.WriteString("  scenaremo <コマンド> [オプション]\n\n")
	b.WriteString("コマンド:\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "  %-10s %s\n", c.name, c.summary)
	}
	b.WriteString("\n各コマンドのオプションは scenaremo <コマンド> --help で見られます。\n")
	return b.String()
}
