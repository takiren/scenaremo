package script

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Issue は台本に見つかった問題1件を表す。
type Issue struct {
	// Path は台本の中の位置 (例 "scenes[0].lines[1].text")。台本全体を指すときは空。
	Path string
	// Line と Column はソース上の位置。1 起点で、特定できなければ 0。
	Line, Column int
	// Message は何が問題かの説明。
	Message string
	// Hint はどう直すかの案内。無ければ空。
	Hint string
	// Snippet は問題箇所を指し示す注釈つきのソース片。無ければ空。
	Snippet string
}

// Error は台本の検証で見つかった問題をまとめたエラー。
//
// 問題を1件見つけるたびに返していては、直す→再実行を繰り返させることになり
// 量産の妨げになる。そのため、同じ段階で見つかった問題はすべてここに入れて返す。
type Error struct {
	// Filename は台本のファイル名。分からなければ空。
	Filename string
	// Issues は見つかった問題。ソース上の位置順に並ぶ。
	Issues []Issue
}

// newError は Issue をまとめてエラーにする。Issue が無ければ nil を返す。
func newError(filename string, issues []Issue) error {
	if len(issues) == 0 {
		return nil
	}
	sortIssues(issues)
	return &Error{Filename: filename, Issues: issues}
}

// sortIssues は問題をソース上の位置順に並べ替える。
// スキーマ検証はマップを走査する順に問題を報告するため、そのままでは順序が安定しない。
// 上から順に直していけるようにする意味もある。
func sortIssues(issues []Issue) {
	slices.SortStableFunc(issues, func(a, b Issue) int {
		if c := cmp.Compare(a.Line, b.Line); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Column, b.Column); c != 0 {
			return c
		}
		return cmp.Compare(a.Path, b.Path)
	})
}

// Error は見つかった問題をすべて並べた報告文を返す。
// そのまま端末へ出せる形にしてある。
func (e *Error) Error() string {
	name := e.Filename
	if name == "" {
		name = "台本"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s の検証に失敗しました (%d 件)\n", name, len(e.Issues))
	for _, issue := range e.Issues {
		b.WriteString("\n")
		b.WriteString(issue.format(e.Filename))
	}
	return b.String()
}

// format は問題1件を報告文へ整形する。
func (i Issue) format(filename string) string {
	var b strings.Builder

	// 1行目は「どこで何が起きたか」。エディタが飛べるよう file:line:col の順に並べる。
	if head := i.location(filename); head != "" {
		b.WriteString(head)
		b.WriteString(": ")
	}
	b.WriteString(i.Message)
	b.WriteString("\n")

	if i.Snippet != "" {
		b.WriteString(i.Snippet)
		b.WriteString("\n")
	}
	if i.Hint != "" {
		b.WriteString("  ヒント: ")
		b.WriteString(i.Hint)
		b.WriteString("\n")
	}
	return b.String()
}

// location は "script.yaml:12:5" のような位置表記を返す。
// ファイル名も行番号も分からなければ空を返す。
func (i Issue) location(filename string) string {
	var parts []string
	if filename != "" {
		parts = append(parts, filename)
	}
	if i.Line > 0 {
		parts = append(parts, fmt.Sprint(i.Line))
		if i.Column > 0 {
			parts = append(parts, fmt.Sprint(i.Column))
		}
	}
	return strings.Join(parts, ":")
}

// ShouldColorize は出力先が端末かどうかを見て、色を付けてよいかを返す。
// 呼び出し側が WithColor へ渡すことを想定している。
// パイプやファイルへ出すときに制御文字が混ざらないようにするため。
func ShouldColorize(f *os.File) bool {
	if f == nil {
		return false
	}
	// https://no-color.org/ の慣習に従う。
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if term := os.Getenv("TERM"); term == "" || term == "dumb" {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
