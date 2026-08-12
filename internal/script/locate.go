package script

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/printer"
)

// locator は台本のソースを構文木として保持し、
// 「台本の中のどこか」を示す位置指定から行番号と注釈つきソース片を引く。
//
// スキーマ検証は値だけを見るので位置情報を持たない。
// ここで構文木と突き合わせて「どの行のどこが悪いか」を復元する。
// 開発者以外にとってはエラーメッセージが唯一の道しるべになるため、
// 位置を示せるかどうかがそのまま使い勝手になる。
type locator struct {
	// root は台本の最上位ノード。構文木を組み立てられなければ nil。
	// nil でも位置無しの Issue を作れるよう、locator 自体は必ず使える。
	root    ast.Node
	colored bool
}

// newLocator はソースを解析して locator を作る。
// 解析できなくても（構文エラーなど）nil は返さず、位置を引けない locator を返す。
func newLocator(src []byte, colored bool) *locator {
	l := &locator{colored: colored}
	file, err := parser.ParseBytes(src, 0)
	if err != nil || file == nil || len(file.Docs) == 0 {
		return l
	}
	l.root = file.Docs[0].Body
	return l
}

// step は位置指定の1要素。マップのキーか、配列の添字かを区別する。
type step struct {
	name    string
	isIndex bool
}

// key はマップのキーを1つ辿る。
func key(name string) step { return step{name: name} }

// index は配列の添字を1つ辿る。
func index(i int) step { return step{name: strconv.Itoa(i), isIndex: true} }

// stepsFromPointer は jsonschema の InstanceLocation を位置指定へ変換する。
// InstanceLocation は要素へ分解済みの JSON Pointer なので、
// 配列かマップかは構文木を辿るときに判定する。
func stepsFromPointer(pointer []string) []step {
	steps := make([]step, len(pointer))
	for i, seg := range pointer {
		steps[i] = step{name: seg}
	}
	return steps
}

// place は台本の中の1箇所を指す。
type place struct {
	// Label は人間向けの位置表記 (例 "scenes[0].lines[1].text")。
	Label string
	// Line と Column は 1 起点。特定できなければ 0。
	Line, Column int
	// Snippet は問題箇所を指し示す注釈つきのソース片。特定できなければ空。
	Snippet string
}

// at は位置指定に対応する場所を返す。
//
// 指定の途中までしか辿れない場合（まだ書かれていないフィールドを指すときなど）は、
// 辿れた一番深いところを指す。preferKey が true なら値ではなくキーを指す。
// 未知のフィールドのように、キーそのものが問題である場合に使う。
func (l *locator) at(steps []step, preferKey bool) place {
	p := place{Label: label(steps)}
	if l.root == nil {
		return p
	}

	node, keyNode, matched := l.find(steps)
	// 辿り切れていれば指定どおりの場所を、途中で止まったならその手前を指す。
	// どちらの場合も「その行を見れば分かる」ところに落ちる。
	target := node
	if preferKey && matched == len(steps) && keyNode != nil {
		target = keyNode
	}
	if target == nil {
		return p
	}
	tk := target.GetToken()
	if tk == nil {
		return p
	}
	p.Line = tk.Position.Line
	p.Column = tk.Position.Column

	var pp printer.Printer
	p.Snippet = trimTrailingBlankLines(pp.PrintErrorToken(tk, l.colored))
	return p
}

// ansiEscape は色付けの制御文字。
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// trimTrailingBlankLines はソース片の末尾に残る中身の無い行を落とす。
// 注釈は問題箇所の前後を一定行数ぶん出すため、ファイル末尾付近では余白だけの行が残る。
func trimTrailingBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 0 {
		last := ansiEscape.ReplaceAllString(lines[len(lines)-1], "")
		if _, content, found := strings.Cut(last, "|"); found {
			last = content
		}
		if strings.TrimSpace(last) != "" {
			break
		}
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// find は位置指定を構文木の上で辿る。
// 辿り着いたノード、その親マップでのキー、辿れた要素数を返す。
func (l *locator) find(steps []step) (node, keyNode ast.Node, matched int) {
	node = l.root
	for i, s := range steps {
		var nextKey, next ast.Node
		switch t := unwrap(node).(type) {
		case *ast.MappingNode:
			for _, v := range t.Values {
				if mapKeyName(v.Key) == s.name {
					nextKey, next = v.Key, v.Value
					break
				}
			}
		case *ast.MappingValueNode:
			// 要素が1つだけのマップはこの形になることがある。
			if mapKeyName(t.Key) == s.name {
				nextKey, next = t.Key, t.Value
			}
		case *ast.SequenceNode:
			idx, err := strconv.Atoi(s.name)
			if err == nil && idx >= 0 && idx < len(t.Values) {
				next = t.Values[idx]
			}
		}
		if next == nil {
			return node, keyNode, i
		}
		node, keyNode = next, nextKey
	}
	return node, keyNode, len(steps)
}

// unwrap はドキュメントやアンカーの包みを外して中身のノードを返す。
func unwrap(n ast.Node) ast.Node {
	for {
		switch t := n.(type) {
		case *ast.DocumentNode:
			n = t.Body
		case *ast.AnchorNode:
			n = t.Value
		case *ast.TagNode:
			n = t.Value
		default:
			return n
		}
		if n == nil {
			return nil
		}
	}
}

// mapKeyName はマップのキーノードから、引用符を外した名前を取り出す。
func mapKeyName(n ast.Node) string {
	switch k := n.(type) {
	case *ast.StringNode:
		return k.Value
	case nil:
		return ""
	}
	tk := n.GetToken()
	if tk == nil {
		return ""
	}
	return tk.Value
}

// label は位置指定を人間向けの表記へ直す (例 "scenes[0].lines[1].text")。
func label(steps []step) string {
	var b strings.Builder
	for _, s := range steps {
		switch {
		case s.isIndex:
			b.WriteString("[")
			b.WriteString(s.name)
			b.WriteString("]")
		case b.Len() == 0:
			b.WriteString(s.name)
		default:
			b.WriteString(".")
			b.WriteString(s.name)
		}
	}
	return b.String()
}

// resolveIndexes は構文木を見て、位置指定のどの要素が配列の添字だったかを埋める。
// jsonschema から来る位置指定はマップと配列を区別しないため、表記を整えるのに使う。
func (l *locator) resolveIndexes(steps []step) []step {
	out := make([]step, len(steps))
	copy(out, steps)
	if l.root == nil {
		// 構文木が無いときは数字を添字とみなす。台本の配列は scenes と lines だけなので実害は小さい。
		for i := range out {
			if _, err := strconv.Atoi(out[i].name); err == nil && i > 0 {
				out[i].isIndex = true
			}
		}
		return out
	}
	node := l.root
	for i := range out {
		var next ast.Node
		switch t := unwrap(node).(type) {
		case *ast.MappingNode:
			for _, v := range t.Values {
				if mapKeyName(v.Key) == out[i].name {
					next = v.Value
					break
				}
			}
		case *ast.MappingValueNode:
			if mapKeyName(t.Key) == out[i].name {
				next = t.Value
			}
		case *ast.SequenceNode:
			out[i].isIndex = true
			if idx, err := strconv.Atoi(out[i].name); err == nil && idx >= 0 && idx < len(t.Values) {
				next = t.Values[idx]
			}
		}
		if next == nil {
			// 辿れなくなった先は数字を添字とみなす。
			for j := i; j < len(out); j++ {
				if _, err := strconv.Atoi(out[j].name); err == nil && j > 0 {
					out[j].isIndex = true
				}
			}
			return out
		}
		node = next
	}
	return out
}

// resolve は位置指定を解決する。
// メッセージに位置表記を埋め込めるよう、Issue を組み立てる前に呼ぶ。
func (l *locator) resolve(steps []step, preferKey bool) place {
	return l.at(l.resolveIndexes(steps), preferKey)
}

// issueAt は解決済みの位置から Issue を1件作る。
func issueAt(p place, message, hint string) Issue {
	return Issue{
		Path:    p.Label,
		Line:    p.Line,
		Column:  p.Column,
		Message: message,
		Hint:    hint,
		Snippet: p.Snippet,
	}
}

// subject は位置表記を文の主語として返す。台本全体を指すときは「台本」と呼ぶ。
//
// 英数字の位置表記は日本語と地続きだと読みにくいため、後ろに空白を1つ入れる。
// 呼び出す側は助詞をそのまま続けて書けばよい (例 fmt.Sprintf("%sには…", p.subject()))。
func (p place) subject() string {
	if p.Label == "" {
		return "台本"
	}
	return p.Label + " "
}

// sourceSnippet は構文木を使わずに、行番号だけからソース片を組み立てる。
// 構文エラーのように構文木が作れない場合に使う。
func sourceSnippet(src []byte, line, column int) string {
	if line <= 0 {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	if line > len(lines) {
		return ""
	}
	first := max(line-3, 1)
	last := min(line+3, len(lines))

	// 行頭の書式は goccy の注釈と揃える（見た目が混ざらないようにするため）。
	var b strings.Builder
	for i := first; i <= last; i++ {
		head := fmt.Sprintf("  %2d | ", i)
		if i == line {
			head = ">" + head[1:]
		}
		b.WriteString(head)
		b.WriteString(lines[i-1])
		b.WriteString("\n")
		if i == line && column > 0 {
			b.WriteString(strings.Repeat(" ", len(head)+column-1))
			b.WriteString("^\n")
		}
	}
	return trimTrailingBlankLines(strings.TrimRight(b.String(), "\n"))
}
