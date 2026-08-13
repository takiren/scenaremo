package progress

import "time"

// Discard は何も書かない進捗表示。--quiet のときに使う。
//
// 呼び出し側に「進捗表示があるか」の分岐（nil チェック）を書かせないために用意している。
// io.Discard へ書く Printer でも同じことはできるが、それだと黙らせたい場面で
// 整形の手間だけが残るうえ、全体で 1 つの Printer の状態を共有することになるため型を分けた。
var Discard discardPrinter

// discardPrinter は Printer と同じ呼ばれ方をして何もしない実装。
//
// メソッドを値レシーバにしているのは、Discard を値のまま渡しても
// reporter を満たすようにするため（ポインタレシーバだと &Discard が要る）。
type discardPrinter struct{}

func (discardPrinter) Start(int)                         {}
func (discardPrinter) LineStart(int, string, string)     {}
func (discardPrinter) LineDone(int, bool, time.Duration) {}
func (discardPrinter) Done(int, int)                     {}

// reporter は進捗の受け取り口。呼び出し側（internal/synth）が同じ形のインターフェースを
// 自分で定義して *Printer や Discard を受け取るため、ここでは公開しない。
// Printer と Discard のメソッド集合がずれたらコンパイルを失敗させるための見張りとして置いている。
type reporter interface {
	Start(total int)
	LineStart(index int, speaker, text string)
	LineDone(index int, cached bool, d time.Duration)
	Done(synthesized, cached int)
}

var (
	_ reporter = (*Printer)(nil)
	_ reporter = Discard
)
