package script

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// Format は台本の記法。
type Format int

const (
	// FormatYAML は YAML。既定であり、YAML は JSON の上位集合なので JSON も読める。
	FormatYAML Format = iota
	// FormatJSON は JSON。エラーの示し方を JSON に寄せたいときに使う。
	FormatJSON
)

// options は読み込みの設定。
type options struct {
	filename  string
	baseDir   string
	format    Format
	formatSet bool
	colored   bool
}

// Option は読み込みの設定を変える。
type Option func(*options)

// WithFilename はエラー表示に使うファイル名を指定する。
// WithFormat が無ければ、この名前の拡張子から記法を決める。
func WithFilename(name string) Option {
	return func(o *options) { o.filename = name }
}

// WithBaseDir は画像パスを解決する基準ディレクトリを指定する。
// 指定した場合だけ画像の存在確認を行う。
func WithBaseDir(dir string) Option {
	return func(o *options) { o.baseDir = dir }
}

// WithFormat は台本の記法を明示する。ファイル名からの推測より優先される。
func WithFormat(f Format) Option {
	return func(o *options) { o.format, o.formatSet = f, true }
}

// WithColor はエラーのソース片に色を付けるかどうかを決める。
// 既定は色なし。端末かどうかで切り替えるには ShouldColorize を使う。
func WithColor(colored bool) Option {
	return func(o *options) { o.colored = colored }
}

// newOptions は既定の設定に Option を適用する。
// 記法の決定は Option をすべて適用したあとに行うので、指定の順序に左右されない。
func newOptions(opts []Option) *options {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	if !o.formatSet {
		o.format = formatFromName(o.filename)
	}
	return o
}

// formatFromName はファイル名から記法を推測する。
func formatFromName(name string) Format {
	if strings.EqualFold(filepath.Ext(name), ".json") {
		return FormatJSON
	}
	return FormatYAML
}

// Load は台本ファイルを読み込み、検証して返す。
//
// 画像パスは台本ファイルの位置を基準に解決し、存在も確認する。
// 記法はファイルの拡張子から決まる (.json なら JSON、それ以外は YAML)。
func Load(path string, opts ...Option) (*Script, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("台本を読み込めません: %w", err)
	}
	// 呼び出し側が明示した設定で上書きできるよう、既定を先に置く。
	base := []Option{WithFilename(path), WithBaseDir(filepath.Dir(path))}
	return Parse(src, append(base, opts...)...)
}

// Parse は台本のソースを検証し、既定値を適用した Script を返す。
//
// 検証は「記法」→「形」→「意味」の順に段階を分けて行う。
// 形（スキーマ）が壊れたまま意味の検証を続けても見当違いの指摘が増えるだけなので、
// 段階の途中では打ち切る。一方、同じ段階で見つかった問題はすべてまとめて返す。
// 1件直すたびに再実行させていては、台本を量産するどころではないため。
//
// 検証に失敗した場合のエラーは *Error であり、errors.As で個々の問題を取り出せる。
func Parse(src []byte, opts ...Option) (*Script, error) {
	o := newOptions(opts)
	loc := newLocator(src, o.colored)

	// 1. 記法に沿って汎用の値へ落とす。ここから先は YAML と JSON で処理を分けない。
	doc, err := decodeValue(src, o)
	if err != nil {
		return nil, err
	}

	// 2. スキーマ検証。
	issues, err := validateDocument(doc, loc)
	if err != nil {
		return nil, err
	}
	if len(issues) > 0 {
		return nil, newError(o.filename, issues)
	}

	// 3. 構造体へ写す。スキーマを通っているので、ここで失敗するのは想定外の事態。
	var s Script
	if err := decodeScript(src, &s, o); err != nil {
		return nil, err
	}

	// 4. 意味の検証。既定値を埋める前に行う（どこが省略されていたかを示し分けるため）。
	semantic := checkSpeakers(&s, loc)
	if o.baseDir != "" {
		semantic = append(semantic, checkImages(&s, o.baseDir, loc)...)
	}
	if len(semantic) > 0 {
		return nil, newError(o.filename, semantic)
	}

	// 5. 既定値の適用。
	ApplyDefaults(&s)
	return &s, nil
}

// decodeValue はソースを汎用の値へ落とす。スキーマ検証はこの形の値に対して行う。
func decodeValue(src []byte, o *options) (any, error) {
	var v any
	if o.format == FormatJSON {
		if err := json.Unmarshal(src, &v); err != nil {
			return nil, jsonSyntaxError(src, err, o)
		}
		return v, nil
	}
	if err := yaml.Unmarshal(src, &v); err != nil {
		return nil, yamlSyntaxError(err, o)
	}
	return v, nil
}

// decodeScript はソースを Script へ写す。
func decodeScript(src []byte, s *Script, o *options) error {
	if o.format == FormatJSON {
		if err := json.Unmarshal(src, s); err != nil {
			return jsonSyntaxError(src, err, o)
		}
		return nil
	}
	if err := yaml.Unmarshal(src, s); err != nil {
		return yamlSyntaxError(err, o)
	}
	return nil
}

// yamlErrorHead は goccy が付ける "[行:桁] " の見出しを取り出す。
var yamlErrorHead = regexp.MustCompile(`^\[(\d+):(\d+)\] `)

// yamlSyntaxError は YAML の構文エラーを Issue へ直す。
//
// goccy のメッセージは英語だが、位置と注釈つきソースはそのまま使えるので、
// 日本語の見出しを付けたうえで元の説明を残す。
func yamlSyntaxError(err error, o *options) error {
	plain := yaml.FormatError(err, false, false)
	detail, line, column := plain, 0, 0
	if m := yamlErrorHead.FindStringSubmatch(plain); m != nil {
		line, _ = strconv.Atoi(m[1])
		column, _ = strconv.Atoi(m[2])
		detail = plain[len(m[0]):]
	}

	// 注釈つきの部分だけを取り出す（見出しは日本語のものへ置き換えるため）。
	snippet := ""
	if _, rest, found := strings.Cut(yaml.FormatError(err, o.colored, true), "\n"); found {
		snippet = strings.TrimRight(rest, "\n")
	}

	return newError(o.filename, []Issue{{
		Line:    line,
		Column:  column,
		Message: fmt.Sprintf("YAML として読めません: %s", strings.TrimSpace(detail)),
		Snippet: snippet,
	}})
}

// jsonSyntaxError は JSON の構文エラーを Issue へ直す。
func jsonSyntaxError(src []byte, err error, o *options) error {
	var (
		offset  int64 = -1
		message       = fmt.Sprintf("JSON として読めません: %v", err)
	)
	switch e := err.(type) {
	case *json.SyntaxError:
		offset = e.Offset
	case *json.UnmarshalTypeError:
		offset = e.Offset
		message = fmt.Sprintf("JSON の値の型が合いません: %s には %s を書けません", e.Field, e.Value)
	}

	line, column := 0, 0
	if offset >= 0 {
		line, column = offsetToPosition(src, int(offset))
	}
	return newError(o.filename, []Issue{{
		Line:    line,
		Column:  column,
		Message: message,
		Snippet: sourceSnippet(src, line, column),
	}})
}

// offsetToPosition はバイト位置を行番号と桁へ直す。どちらも 1 起点。
func offsetToPosition(src []byte, offset int) (line, column int) {
	if offset > len(src) {
		offset = len(src)
	}
	line, column = 1, 1
	for i := range offset {
		if src[i] == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return line, column
}
