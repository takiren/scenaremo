// Command scenaremo は台本 (YAML) から解説動画を作る CLI。
//
// このパッケージが持つのはサブコマンドの配線と終了コードだけで、処理の中身は internal/ 以下にある。
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
)

// 終了コード。診断や検証の失敗を呼び出し元のスクリプトが見分けられるよう、成功以外は必ず 0 以外を返す。
const (
	exitSuccess = 0
	exitFailure = 1
)

// errReported は「利用者向けの説明はコマンド自身が出力済み」であることを表す番兵。
//
// doctor のように失敗の中身を丁寧に出しているコマンドが、そのうしろへ
// "scenaremo: ..." を重ねて出さないために使う。伝えたいのは終了コードだけ、という場合もこれ。
var errReported = errors.New("報告済み")

// usageError は引数やフラグの誤りを表す。
//
// cobra は RunE がエラーを返すと既定で usage を丸ごと出すが、診断に失敗したときのように
// 「使い方は合っているが結果が悪い」場合に使い方を突きつけても邪魔になるだけである。
// そこで cobra 側の自動表示は止め（SilenceUsage / SilenceErrors）、
// usage を出すのは使い方の誤りに限る。その判定をこの型で行う。
type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

func main() {
	// Ctrl-C を context の打ち切りとして扱う。音声合成やレンダリングは分単位で走るので、
	// 止めたときに走らせている HTTP や外部プロセスを道連れに畳めるようにしておく。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run は CLI を実行して終了コードを返す。
// os.Exit を main に閉じ込めているので、テストからは戻り値と出力の両方をそのまま検査できる。
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root := newRootCommand()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	// ExecuteContextC は失敗したコマンドを返す。usage を出すときに、
	// root ではなく実際に打たれたコマンドのものを見せるために必要。
	cmd, err := root.ExecuteContextC(ctx)
	if err == nil {
		return exitSuccess
	}
	if cmd == nil {
		cmd = root
	}

	var usageErr *usageError
	switch {
	case errors.Is(err, errReported):
		// コマンドが説明を出し終えている。重ねて出さない。
	case errors.As(err, &usageErr):
		// 標準エラーへの書き出しが失敗しても、報告先がもう無いので握り潰すほかない。
		_, _ = fmt.Fprintln(stderr, "scenaremo:", err)
		_, _ = fmt.Fprint(stderr, cmd.UsageString())
	default:
		_, _ = fmt.Fprintln(stderr, "scenaremo:", err)
	}
	return exitFailure
}
