package progress

import (
	"strings"
	"unicode"
)

// textWidth はセリフに割く表示幅の上限（半角換算の桁数）。全角 24 文字にあたる。
//
// 端末の幅は見ない。出力先が端末とは限らないうえ、幅を得るには外部依存 (golang.org/x/term) が要り、
// 進捗表示のためだけに依存を増やす価値は無いと判断した。
// 80 桁の端末で、前置き "[2/4] speaker: " と末尾 " ... 12.3 秒 (キャッシュ)" を足しても大半の行が
// 折り返さない長さを採っている。切り詰めるのは行を読み流せるようにするためであって、
// 端末へ収めきることが目的ではないので、話者名が長ければ多少はみ出しても構わないと考える。
const textWidth = 48

// ellipsis は切り詰めた印。三点リーダ 1 文字にしているのは "..." だと
// 結果との区切りに使っている " ... " と見分けが付かなくなるため。
const ellipsis = "…"

// oneLine は改行や連続する空白を空白 1 つに畳み、制御文字を落とす。
//
// 台本のセリフは複数行で書けるため（→ internal/script）、そのまま出すと 1 件が数行に散らばり、
// どこまで進んだかが追えなくなる。制御文字を空白へ置き換えるのは、
// このパッケージが「制御文字を混ぜない」ことを約束している以上、
// セリフに紛れ込んだ ESC を素通しして端末を書き換えられては約束が守れないため。
// strings.Map は壊れた UTF-8 を U+FFFD へ置き換えるので、ここを通れば以降の切り詰めも安全になる。
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// truncate は表示幅が limit を超える文字列を切り詰め、末尾に ellipsis を付ける。
//
// バイト数でも文字数でもなく表示幅で数えるのは、セリフが日本語主体で、
// 文字数で揃えると行の長さが倍近くばらついて読みにくくなるため。
// 切る位置はルーンの境界に限るので、UTF-8 が壊れることはない。
func truncate(s string, limit int) string {
	if displayWidth(s) <= limit {
		return s
	}

	// ellipsis の分を空けてから切る。切り詰めた結果が上限を超えては幅で数えた意味が無い。
	budget := limit - displayWidth(ellipsis)

	cut, w := len(s), 0
	for i, r := range s {
		if w+runeWidth(r) > budget {
			cut = i
			break
		}
		w += runeWidth(r)
	}
	return s[:cut] + ellipsis
}

// displayWidth は端末に並べたときの見た目の幅を返す。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// runeWidth は 1 文字の表示幅。全角を 2、それ以外を 1 とする。
//
// 端末によって幅が変わる曖昧幅（三点リーダや罫線など）は 1 として数える。
// 正確に扱うには golang.org/x/text の表が要るが、ずれても見た目が 1 桁詰まるだけで
// 読めなくなるわけではないので、依存を増やしてまで合わせる価値は無いと判断した。
func runeWidth(r rune) int {
	for _, w := range wideRanges {
		if r < w.lo {
			break // wideRanges は昇順なので、これ以降に該当する範囲は無い
		}
		if r <= w.hi {
			return 2
		}
	}
	return 1
}

// wideRanges は表示幅 2 として数える符号位置の範囲（East Asian Width が W か F のもののうち、
// 台本に現れ得るものを拾ったもの）。昇順に並べる。
var wideRanges = [...]struct{ lo, hi rune }{
	{0x1100, 0x115F},   // ハングル字母
	{0x2E80, 0x303E},   // CJK の部首・記号・句読点（U+3000 の全角空白まで）
	{0x3041, 0x33FF},   // ひらがな・カタカナ・ハングル互換字母・囲み CJK
	{0x3400, 0x4DBF},   // CJK 統合漢字拡張 A
	{0x4E00, 0x9FFF},   // CJK 統合漢字
	{0xA960, 0xA97F},   // ハングル字母拡張 A
	{0xAC00, 0xD7A3},   // ハングル音節
	{0xF900, 0xFAFF},   // CJK 互換漢字
	{0xFE10, 0xFE19},   // 縦書き用の記号
	{0xFE30, 0xFE6F},   // CJK 互換形・小字形
	{0xFF00, 0xFF60},   // 全角英数・全角記号
	{0xFFE0, 0xFFE6},   // 全角の通貨記号など
	{0x1F300, 0x1F64F}, // 絵文字（記号・顔）
	{0x1F680, 0x1F6FF}, // 絵文字（乗り物）
	{0x1F900, 0x1F9FF}, // 絵文字（追加分）
	{0x20000, 0x3FFFD}, // CJK 統合漢字拡張 B 以降
}
