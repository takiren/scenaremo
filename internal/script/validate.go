package script

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	scenaremo "github.com/takiren/scenaremo"
)

// スキーマは実行のたびに変わらないので一度だけ組み立てて使い回す。
var (
	schemaOnce     sync.Once
	compiledSchema *jsonschema.Schema
	rawSchema      map[string]any
	schemaLoadErr  error
)

// fallbackSchemaID はスキーマに $id が無かった場合に使う識別子。
// 実際にこの URL を取りに行くことはない（埋め込んだ内容を登録して使う）。
const fallbackSchemaID = "scenaremo:///schema.json"

// loadSchema は埋め込まれた docs/schema.json をコンパイルする。
// エディタ (yaml-language-server) と CLI が同じ定義を見るように、
// 実行時の検証にもリポジトリ唯一のスキーマを使う。
func loadSchema() (*jsonschema.Schema, map[string]any, error) {
	schemaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(scenaremo.SchemaJSON))
		if err != nil {
			schemaLoadErr = fmt.Errorf("スキーマを読み込めません: %w", err)
			return
		}
		obj, ok := doc.(map[string]any)
		if !ok {
			schemaLoadErr = errors.New("スキーマがマップではありません")
			return
		}
		id, _ := obj["$id"].(string)
		if id == "" {
			id = fallbackSchemaID
		}

		c := jsonschema.NewCompiler()
		if err := c.AddResource(id, doc); err != nil {
			schemaLoadErr = fmt.Errorf("スキーマを登録できません: %w", err)
			return
		}
		sch, err := c.Compile(id)
		if err != nil {
			schemaLoadErr = fmt.Errorf("スキーマをコンパイルできません: %w", err)
			return
		}
		compiledSchema, rawSchema = sch, obj
	})
	return compiledSchema, rawSchema, schemaLoadErr
}

// validateDocument は台本の値をスキーマで検証し、見つかった問題を Issue にして返す。
func validateDocument(doc any, loc *locator) ([]Issue, error) {
	sch, raw, err := loadSchema()
	if err != nil {
		return nil, err
	}
	verr := sch.Validate(doc)
	if verr == nil {
		return nil, nil
	}

	var ve *jsonschema.ValidationError
	if !errors.As(verr, &ve) {
		// 想定外の形のエラー。握りつぶさずそのまま伝える。
		return nil, fmt.Errorf("台本を検証できません: %w", verr)
	}

	var issues []Issue
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		// 途中のノードは $ref をたどった経路を示すだけなので、葉だけを報告する。
		if len(e.Causes) > 0 {
			for _, cause := range e.Causes {
				walk(cause)
			}
			return
		}
		issues = append(issues, issuesFromError(e, raw, loc)...)
	}
	walk(ve)
	return issues, nil
}

// issuesFromError は検証エラー1件を日本語の Issue へ直す。
// 未知のフィールドのようにまとめて報告されるものは、位置を個別に示すため複数件に分ける。
func issuesFromError(e *jsonschema.ValidationError, raw map[string]any, loc *locator) []Issue {
	steps := stepsFromPointer(e.InstanceLocation)

	switch k := e.ErrorKind.(type) {
	case *kind.Required:
		p := loc.resolve(steps, true)
		return []Issue{issueAt(p,
			fmt.Sprintf("%sに必須の項目 %s がありません", p.subject(), quoteNames(k.Missing)),
			"")}

	case *kind.AdditionalProperties:
		parent := loc.resolve(steps, false)
		allowed := schemaProperties(raw, e.SchemaURL)
		hint := ""
		if len(allowed) > 0 {
			hint = fmt.Sprintf("%sで使えるのは %s です。打ち間違いではありませんか", parent.subject(), strings.Join(allowed, " / "))
		}
		issues := make([]Issue, 0, len(k.Properties))
		for _, name := range k.Properties {
			p := loc.resolve(append(slices.Clone(steps), key(name)), true)
			issues = append(issues, issueAt(p, fmt.Sprintf("%sは知らない項目です", p.subject()), hint))
		}
		return issues

	case *kind.Enum:
		p := loc.resolve(steps, false)
		return []Issue{issueAt(p,
			fmt.Sprintf("%sの値が正しくありません: %s", p.subject(), displayValue(k.Got)),
			fmt.Sprintf("使えるのは %s です", joinValues(k.Want)))}

	case *kind.Type:
		p := loc.resolve(steps, false)
		return []Issue{issueAt(p,
			fmt.Sprintf("%sには%sを書いてください（いまは%sです）", p.subject(), joinTypeNames(k.Want), typeName(k.Got)),
			"")}

	case *kind.MinLength:
		p := loc.resolve(steps, false)
		if k.Want == 1 {
			return []Issue{issueAt(p, fmt.Sprintf("%sが空です", p.subject()), "")}
		}
		return []Issue{issueAt(p, fmt.Sprintf("%sは %d 文字以上にしてください", p.subject(), k.Want), "")}

	case *kind.MaxLength:
		p := loc.resolve(steps, false)
		return []Issue{issueAt(p, fmt.Sprintf("%sは %d 文字以内にしてください", p.subject(), k.Want), "")}

	case *kind.MinItems:
		p := loc.resolve(steps, true)
		return []Issue{issueAt(p, fmt.Sprintf("%sには少なくとも %d 個必要です", p.subject(), k.Want), "")}

	case *kind.MaxItems:
		p := loc.resolve(steps, true)
		return []Issue{issueAt(p, fmt.Sprintf("%sは %d 個までです", p.subject(), k.Want), "")}

	case *kind.MinProperties:
		p := loc.resolve(steps, true)
		return []Issue{issueAt(p, fmt.Sprintf("%sには少なくとも %d 個の項目が必要です", p.subject(), k.Want), "")}

	case *kind.MaxProperties:
		p := loc.resolve(steps, true)
		return []Issue{issueAt(p, fmt.Sprintf("%sの項目は %d 個までです", p.subject(), k.Want), "")}

	case *kind.Minimum:
		p := loc.resolve(steps, false)
		return []Issue{issueAt(p,
			fmt.Sprintf("%sは %s 以上にしてください（いまは %s です）", p.subject(), ratString(k.Want), ratString(k.Got)),
			"")}

	case *kind.Maximum:
		p := loc.resolve(steps, false)
		return []Issue{issueAt(p,
			fmt.Sprintf("%sは %s 以下にしてください（いまは %s です）", p.subject(), ratString(k.Want), ratString(k.Got)),
			"")}

	case *kind.Pattern:
		p := loc.resolve(steps, false)
		return []Issue{issueAt(p,
			fmt.Sprintf("%sの書き方が正しくありません: %s", p.subject(), displayValue(k.Got)),
			fmt.Sprintf("次の形に合う必要があります: %s", k.Want))}

	case *kind.PropertyNames:
		p := loc.resolve(steps, true)
		return []Issue{issueAt(p,
			fmt.Sprintf("%sのキー %s が使えません", p.subject(), strconv.Quote(k.Property)),
			"")}

	default:
		// スキーマに新しいキーワードを足したのに日本語の文言を用意していない場合に来る。
		// 位置とキーワードだけでも示す。
		p := loc.resolve(steps, false)
		keyword := strings.Join(e.ErrorKind.KeywordPath(), "/")
		msg := fmt.Sprintf("%sの値がスキーマに合いません", p.subject())
		if keyword != "" {
			msg = fmt.Sprintf("%s（%s）", msg, keyword)
		}
		return []Issue{issueAt(p, msg, "")}
	}
}

// schemaProperties は SchemaURL が指すスキーマ位置の properties 名を並べて返す。
// 「使えるのは a / b / c です」という案内を、スキーマ自身から作るために使う。
func schemaProperties(raw map[string]any, schemaURL string) []string {
	if raw == nil {
		return nil
	}
	_, fragment, found := strings.Cut(schemaURL, "#")
	if !found {
		return nil
	}

	node := any(raw)
	for seg := range strings.SplitSeq(fragment, "/") {
		if seg == "" {
			continue
		}
		// JSON Pointer の書き換え規則を戻す。
		seg = strings.ReplaceAll(seg, "~1", "/")
		seg = strings.ReplaceAll(seg, "~0", "~")
		obj, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		node, ok = obj[seg]
		if !ok {
			return nil
		}
	}

	obj, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// typeName は JSON の型名を日本語にする。
func typeName(t string) string {
	switch t {
	case "string":
		return "文字列"
	case "integer":
		return "整数"
	case "number":
		return "数値"
	case "boolean":
		return "true / false"
	case "object":
		return "マップ"
	case "array":
		return "配列"
	case "null":
		return "空"
	default:
		return t
	}
}

// joinTypeNames は許される型名を並べる。
func joinTypeNames(types []string) string {
	names := make([]string, 0, len(types))
	for _, t := range types {
		names = append(names, typeName(t))
	}
	return strings.Join(names, " または ")
}

// displayValue は台本に書かれていた値を、そのまま見せられる形にする。
func displayValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "空"
	case string:
		return strconv.Quote(t)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprint(v)
	}
}

// joinValues は候補の値を並べる。
func joinValues(values []any) string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, displayValue(v))
	}
	return strings.Join(out, " / ")
}

// quoteNames は項目名を引用符つきで並べる。
func quoteNames(names []string) string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, strconv.Quote(name))
	}
	return strings.Join(out, " / ")
}

// ratString は有理数を台本に書ける形の数値表記へ直す。
// RatString だと 0.15 が "3/20" になってしまい、台本の書き方と対応しないため。
func ratString(r *big.Rat) string {
	if r == nil {
		return "?"
	}
	if r.IsInt() {
		return r.Num().String()
	}
	f, _ := r.Float64()
	return strconv.FormatFloat(f, 'g', -1, 64)
}
