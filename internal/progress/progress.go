// Package progress は時間のかかる処理の進み具合を書き出す。
//
// 音声の合成は 1 件あたり数秒かかる（→ internal/tts）。何も出さずに黙って待たせると、
// 利用者には「止まっている」のか「進んでいる」のかが区別できない。そこで 1 件を
// 「始めたときに何を喋らせているかを書く → 終わったときに結果を書き足す」の 2 段で表示し、
// 待っている間も画面に手掛かりが残るようにしている。
//
// 出力に \r や ANSI 制御文字は使わない。書き出し先が端末とは限らず（`> log.txt` や CI のログ）、
// 上書きのための制御文字が混ざるとログとして読めなくなるためである。
// 見た目の派手さより、あとから grep できることを採る。
//
// Printer は同時に複数の goroutine から呼ばれない前提で書いてある。合成は逐次に行うためで、
// 並列合成（→ issue #24）を入れるときは、どの行を書きかけかという状態ごと設計し直すことになる。
package progress

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Printer は進み具合を io.Writer へ書き出す。ゼロ値は使えないので New で作る。
type Printer struct {
	w io.Writer

	// now は経過時間を測る時計。テストから固定できるよう差し替え口にしている。
	now func() time.Time

	// total は Start で受け取った総数。分母の表示にだけ使う。
	total int

	// startedAt は Start を呼ばれた時刻。started が false の間は意味を持たない。
	startedAt time.Time
	started   bool

	// pending は LineStart で書きかけた行がまだ閉じられていないこと。
	// 呼ばれ方が想定と違っても「1 件 1 行」を崩さないための見張りに使う。
	pending bool
}

// Option は Printer の設定。
type Option func(*Printer)

// WithClock は経過時間を測る時計を差し替える。
// 実時間に依存すると Done の経過時間をテストで突き合わせられないため。
func WithClock(now func() time.Time) Option {
	return func(p *Printer) {
		if now == nil {
			return
		}
		p.now = now
	}
}

// New は w へ書き出す Printer を作る。
//
// w が nil のときは何も書かない。進捗表示は処理の付随物であり、
// 書き出し先を渡し忘れただけで合成そのものが panic で止まるほうが困るため。
func New(w io.Writer, opts ...Option) *Printer {
	if w == nil {
		w = io.Discard
	}
	p := &Printer{w: w, now: time.Now}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Start は総数を伝える 1 行を書き、経過時間の計測を始める。
func (p *Printer) Start(total int) {
	p.endPendingLine()
	p.total = total
	p.startedAt = p.now()
	p.started = true
	p.writeLine(fmt.Sprintf("音声を合成します (%d 件)", total))
}

// LineStart は 1 件の合成を始めたことを、行の途中まで書いて伝える。
// 行を閉じないのは、待っている間ずっと「いま何を喋らせているか」が最終行に見えているようにするため。
func (p *Printer) LineStart(index int, speaker, text string) {
	p.endPendingLine()

	// 空の項目は区切りごと落とす。話者名やセリフが空のときに
	// 行頭へ ":" だけが残ったり、空白が重なったりしないようにするため。
	fields := []string{counter(index, p.total)}
	if s := oneLine(speaker); s != "" {
		fields = append(fields, s+":")
	}
	if s := truncate(oneLine(text), textWidth); s != "" {
		fields = append(fields, s)
	}
	p.write(strings.Join(fields, " ") + " ...")
	p.pending = true
}

// LineDone は 1 件の結果を同じ行へ書き足して閉じる。
// cached が true なら、その旨を添える。2 回目以降が速い理由が利用者に見えることが
// キャッシュを持つことの意味なので、ここは黙って速いだけにはしない。
func (p *Printer) LineDone(index int, cached bool, d time.Duration) {
	if !p.pending {
		// LineStart を経ずに呼ばれた場合。書きかけの行が無いので、
		// せめて何件目かが分かる形で行頭から書き始める。
		p.write(counter(index, p.total) + " ...")
	}
	p.pending = false

	line := " " + formatSeconds(d)
	if cached {
		line += " (キャッシュ)"
	}
	p.writeLine(line)
}

// Done は内訳と経過時間を 1 行で書く。
//
// 件数は synthesized+cached を出す。Start の総数を出すと、Start を呼ばれていない場合や
// 途中で打ち切られた場合に内訳と食い違った数が並ぶことになり、どちらが本当か読み手に分からなくなるため。
func (p *Printer) Done(synthesized, cached int) {
	p.endPendingLine()
	p.writeLine(fmt.Sprintf(
		"合成 %d 件・キャッシュ %d 件 (%d 件, %s)",
		synthesized, cached, synthesized+cached, formatSeconds(p.elapsed()),
	))
}

// End は書きかけの行があれば閉じる。何も書きかけでなければ何も書かない。
//
// 合成は途中で失敗しうる（エンジンが落ちた、Ctrl-C など）。そのとき LineStart で開いた行は
// LineDone を待たずに終わるので、閉じずにいると失敗の報告が「…セリフ ...」の続きに繋がってしまう。
// 失敗の報告は利用者が最も注意して読む文なので、そこだけは行頭から始まるようにしておく。
func (p *Printer) End() { p.endPendingLine() }

// elapsed は Start からの経過時間。Start を呼ばれていなければ 0 を返す。
// 時計が巻き戻った場合（NTP の補正など）も負の値は返さない。
func (p *Printer) elapsed() time.Duration {
	if !p.started {
		return 0
	}
	d := p.now().Sub(p.startedAt)
	if d < 0 {
		return 0
	}
	return d
}

// endPendingLine は書きかけの行があれば閉じる。
// LineStart が続けて呼ばれるような想定外の順序でも、2 件分が 1 行に繋がらないようにするため。
func (p *Printer) endPendingLine() {
	if p.pending {
		p.pending = false
		p.write("\n")
	}
}

func (p *Printer) writeLine(s string) { p.write(s + "\n") }

// write は書き出しの失敗を握り潰す。
// 進捗が表示できないことを理由に合成を止めるのは本末転倒であり、
// エラーを返したところで呼び出し側には「表示を諦める」以外の選択肢が無いためである。
func (p *Printer) write(s string) {
	_, _ = io.WriteString(p.w, s)
}

// counter は "[3/10]" のような何件目かの表示。index は 0 起点で受け取り、人が数える 1 起点で出す。
// 総数が分からないとき（Start を呼ばれていないとき）に "[3/0]" と出しても読み手を混乱させるだけなので、
// そのときは分母を落とす。
func counter(index, total int) string {
	if total <= 0 {
		return fmt.Sprintf("[%d]", index+1)
	}
	return fmt.Sprintf("[%d/%d]", index+1, total)
}

// formatSeconds は所要時間を秒で書く。
//
// 分や時間へ単位を切り替えないのは、この数字の使いどころが「2 回目のほうが速い」の比較であり、
// 単位が混ざると目でも grep でも比べにくくなるため。負の値は 0 に丸める。
func formatSeconds(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%.1f 秒", d.Seconds())
}
