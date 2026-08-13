package progress_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/takiren/scenaremo/internal/progress"
)

// fixedClock は Start からの経過時間をテストから決めるための時計。
// 呼ばれるたびに刻みを進めるのではなく、渡した時刻を順に返す。
// 「何回目の呼び出しか」を数えずに済み、テストの意図（Start と Done の差）が読み取れるため。
func fixedClock(times ...time.Time) func() time.Time {
	i := 0
	return func() time.Time {
		t := times[min(i, len(times)-1)]
		i++
		return t
	}
}

func TestPrinter_1件ごとに何件目と話者とセリフと長さが出る(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf)

	p.Start(4)
	p.LineStart(0, "zundamon", "今日はRemotionの話をするのだ")
	p.LineDone(0, false, 2400*time.Millisecond)

	got := buf.String()
	wantContains(t, got, "音声を合成します (4 件)")
	wantContains(t, got, "[1/4]", "zundamon", "今日はRemotionの話をするのだ", "2.4 秒")
}

func TestPrinter_indexは0起点で受け取り1起点で表示する(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf)

	p.Start(2)
	p.LineStart(1, "metan", "2 件目です")
	p.LineDone(1, false, time.Second)

	if got := buf.String(); !strings.Contains(got, "[2/2]") {
		t.Errorf("2 件目が [2/2] になっていない:\n%s", got)
	}
}

func TestPrinter_キャッシュを使ったセリフはそれと分かる(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf)

	p.Start(2)
	p.LineStart(0, "zundamon", "合成した")
	p.LineDone(0, false, time.Second)
	p.LineStart(1, "metan", "キャッシュから読んだ")
	p.LineDone(1, true, 10*time.Millisecond)

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("1 件 1 行になっていない (%d 行):\n%s", len(lines), buf.String())
	}
	if strings.Contains(lines[1], "キャッシュ)") {
		t.Errorf("合成した行にキャッシュと出ている: %q", lines[1])
	}
	if !strings.Contains(lines[2], "(キャッシュ)") {
		t.Errorf("キャッシュを使った行にそれと分かる印が無い: %q", lines[2])
	}
}

func TestPrinter_LineStartの時点でセリフが見えている(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf)

	p.Start(1)
	p.LineStart(0, "zundamon", "合成には数秒かかるのだ")

	// 合成を待っている間に「いま何を喋らせているか」が見えることが進捗表示の要点なので、
	// LineDone を待たずに話者とセリフが書き出されていなければならない。
	wantContains(t, buf.String(), "[1/1]", "zundamon", "合成には数秒かかるのだ")

	// ただし行はまだ閉じない。結果（長さ・キャッシュ）を同じ行へ書き足すため。
	if strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("LineStart で行が閉じられている: %q", buf.String())
	}

	p.LineDone(0, false, time.Second)
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("LineDone で行が閉じられていない: %q", buf.String())
	}
}

func TestPrinter_制御文字で上書きしない(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf)

	p.Start(1)
	// セリフに紛れ込んだ制御文字もそのまま流さない（端末を書き換えられてしまう）。
	p.LineStart(0, "zundamon\x1b[31m", "のだ\x1b[2K\rなのだ")
	p.LineDone(0, true, time.Second)
	p.Done(0, 1)

	// 出力先はファイルや CI のログでもあり得るので、\r や ANSI で行を上書きしない。
	for _, bad := range []string{"\r", "\x1b"} {
		if strings.Contains(buf.String(), bad) {
			t.Errorf("制御文字 %q が混ざっている: %q", bad, buf.String())
		}
	}
}

func TestPrinter_改行を含むセリフは1行に畳まれる(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf)

	p.Start(1)
	p.LineStart(0, "metan", "スライドショー形式の\n  解説動画を\t作りますね\n")
	p.LineDone(0, false, time.Second)

	got := buf.String()
	wantContains(t, got, "スライドショー形式の 解説動画を 作りますね")
	if lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n"); len(lines) != 2 {
		t.Errorf("セリフの改行で行が増えている (%d 行):\n%s", len(lines), got)
	}
}

func TestPrinter_長いセリフは表示幅で切り詰める(t *testing.T) {
	limit := displayWidth(spokenText(t, strings.Repeat("a", 500)))

	t.Run("上限を超えたら省略記号が付く", func(t *testing.T) {
		got := spokenText(t, strings.Repeat("あ", 500))
		if !strings.HasSuffix(got, "…") {
			t.Errorf("切り詰めたのに省略記号が無い: %q", got)
		}
		if w := displayWidth(got); w > limit {
			t.Errorf("表示幅 %d が上限 %d を超えている: %q", w, limit, got)
		}
	})

	t.Run("収まるセリフはそのまま出す", func(t *testing.T) {
		text := "短いセリフなのだ"
		if got := spokenText(t, text); got != text {
			t.Errorf("収まるセリフが書き換えられている: got %q, want %q", got, text)
		}
	})

	t.Run("全角と半角が混ざっても文字が壊れない", func(t *testing.T) {
		// 切り詰める位置が全角の途中に来る組み合わせを総当たりする。
		// バイト数で切ると、ここで壊れた UTF-8（U+FFFD）が出る。
		for pad := 0; pad <= limit; pad++ {
			text := strings.Repeat("a", pad) + strings.Repeat("あいうえお", 100)
			got := spokenText(t, text)
			if !utf8.ValidString(got) {
				t.Errorf("pad=%d: 壊れた UTF-8 が出ている: %q", pad, got)
			}
			if strings.ContainsRune(got, utf8.RuneError) {
				t.Errorf("pad=%d: 置換文字が出ている: %q", pad, got)
			}
			if w := displayWidth(got); w > limit {
				t.Errorf("pad=%d: 表示幅 %d が上限 %d を超えている: %q", pad, w, limit, got)
			}
		}
	})
}

func TestPrinter_Doneは合成とキャッシュの件数と経過時間を出す(t *testing.T) {
	start := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	p := progress.New(&buf, progress.WithClock(fixedClock(start, start.Add(12300*time.Millisecond))))

	p.Start(4)
	p.Done(3, 1)

	got := lastLine(t, buf.String())
	wantContains(t, got, "合成 3 件", "キャッシュ 1 件", "4 件", "12.3 秒")
}

// reporter は internal/synth が進捗の受け取り口として定義しているインターフェースと同じ形。
// synth 側はこのパッケージを知らずに *progress.Printer を受け取るので、
// メソッド集合がずれていないことをコンパイル時に確かめる。
// （internal/synth は並行して作られているため、ここから import はしない）
type reporter interface {
	Start(total int)
	LineStart(index int, speaker, text string)
	LineDone(index int, cached bool, d time.Duration)
	Done(synthesized, cached int)
}

var (
	_ reporter = (*progress.Printer)(nil)
	_ reporter = progress.Discard
)

func TestDiscard_何も書かない(t *testing.T) {
	// 書き出し先を持たない実装なので、外から観測できるのは「呼んでも壊れない」ことまで。
	// 何を書かないかではなく、Printer と同じ呼ばれ方に耐えることを確かめている
	// （メソッド集合が揃っていることは上の var _ reporter で押さえてある）。
	progress.Discard.Start(2)
	progress.Discard.LineStart(0, "zundamon", "のだ")
	progress.Discard.LineDone(0, true, time.Second)
	progress.Discard.Done(1, 1)
}

func TestPrinter_Endは書きかけの行を閉じる(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf)

	p.Start(2)
	p.LineStart(0, "zundamon", "セリフ")
	p.End()

	if !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("書きかけの行が閉じられていない: %q", buf.String())
	}

	// 書きかけが無いときは何も足さない。失敗しなかった場合に空行が残らないようにするため。
	before := buf.Len()
	p.End()
	if buf.Len() != before {
		t.Errorf("書きかけが無いのに書き足している: %q", buf.String()[before:])
	}
}

func TestPrinter_総数が0件でも壊れない(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf)

	p.Start(0)
	p.Done(0, 0)

	wantContains(t, buf.String(), "(0 件)", "合成 0 件・キャッシュ 0 件")
}

func TestPrinter_Startを呼ばずに使っても壊れない(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf)

	// 総数が分からないので分母は出さない。経過時間も測りようがないので 0 とする。
	p.LineStart(0, "zundamon", "のだ")
	p.LineDone(0, false, 2*time.Second)
	p.Done(1, 0)

	wantContains(t, buf.String(), "[1] zundamon: のだ ... 2.0 秒")
	wantContains(t, lastLine(t, buf.String()), "0.0 秒")
}

func TestPrinter_想定外の順序で呼ばれても1件1行を崩さない(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf)

	p.Start(2)
	p.LineDone(0, false, time.Second)       // LineStart を経ていない
	p.LineStart(1, "zundamon", "書きかけのまま")   // 閉じられないまま
	p.LineStart(9, "metan", "総数を超えた index") // 次が始まる
	p.Done(1, 1)                            // 書きかけのまま終わる

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("行数が合わない (%d 行):\n%s", len(lines), buf.String())
	}
	wantContains(t, lines[1], "[1/2]", "1.0 秒")
	wantContains(t, lines[2], "[2/2]", "書きかけのまま")
	wantContains(t, lines[3], "[10/2]", "総数を超えた index")
	wantContains(t, lines[4], "合成 1 件・キャッシュ 1 件")
}

func TestPrinter_時計が巻き戻っても負の秒数を出さない(t *testing.T) {
	// NTP の補正などで時計が戻ることはある。進捗表示のために止まったり
	// "-3.0 秒" のような読めない値を出したりはしない。
	start := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	p := progress.New(&buf, progress.WithClock(fixedClock(start, start.Add(-3*time.Second))))

	p.Start(1)
	p.LineStart(0, "zundamon", "のだ")
	p.LineDone(0, false, -time.Second)
	p.Done(1, 0)

	if strings.Contains(buf.String(), "-") {
		t.Errorf("負の秒数が出ている:\n%s", buf.String())
	}
	wantContains(t, buf.String(), "0.0 秒")
}

func TestNew_nilを渡しても壊れない(t *testing.T) {
	// 書き出し先や時計を渡し忘れただけで合成そのものが止まっては困る。
	p := progress.New(nil, progress.WithClock(nil))
	p.Start(1)
	p.LineStart(0, "zundamon", "のだ")
	p.LineDone(0, false, time.Second)
	p.Done(1, 0)
}

func TestPrinter_書き込みに失敗しても止まらない(t *testing.T) {
	// 進捗が表示できないことを理由に合成を止めるのは本末転倒なので、書き込みエラーは握り潰す。
	p := progress.New(errWriter{})
	p.Start(1)
	p.LineStart(0, "zundamon", "のだ")
	p.LineDone(0, false, time.Second)
	p.Done(1, 0)
}

// errWriter は必ず書き込みに失敗する io.Writer。
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("書き込めません") }

// spokenText は 1 件を書かせて、行に載ったセリフの部分だけを取り出す。
// 切り詰めの上限は実装の都合なのでテストからは触らず、実際の出力から測る。
func spokenText(t *testing.T, text string) string {
	t.Helper()
	var buf bytes.Buffer
	p := progress.New(&buf)
	p.Start(1)
	p.LineStart(0, "s", text)
	p.LineDone(0, false, time.Second)

	line := lastLine(t, buf.String())
	const head = "[1/1] s: "
	if !strings.HasPrefix(line, head) {
		t.Fatalf("行の形が想定と違う: %q", line)
	}
	body, _, ok := strings.Cut(strings.TrimPrefix(line, head), " ... ")
	if !ok {
		t.Fatalf("セリフと結果の区切りが見つからない: %q", line)
	}
	return body
}

// displayWidth はテストから見た表示幅。全角を 2、それ以外を 1 と数えるだけの素朴な実装にして、
// 実装側の判定表を写さずに済むよう、テストで渡す文字は ASCII と全角かなに限っている。
// 省略記号は実装では曖昧幅として 1 と数えるが、比べる文字列のどちらにも 1 つずつ入るので差は出ない。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r > 0xff {
			w += 2
			continue
		}
		w++
	}
	return w
}

// wantContains は出力に含まれていてほしい文字列をまとめて確かめる。
func wantContains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("%q が出力に含まれていない:\n%s", want, got)
		}
	}
}

// lastLine は末尾の 1 行を返す。出力が改行で終わっていないことも失敗として扱う。
func lastLine(t *testing.T, got string) string {
	t.Helper()
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("出力が改行で終わっていない: %q", got)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	return lines[len(lines)-1]
}
