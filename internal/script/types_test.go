package script_test

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/takiren/scenaremo/internal/script"
)

// TestUnmarshalMinimalExample は最小の台本が Go の型へそのまま読めることを確かめる。
func TestUnmarshalMinimalExample(t *testing.T) {
	src, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("台本の読み込みに失敗した: %v", err)
	}

	var got script.Script
	if err := yaml.Unmarshal(src, &got); err != nil {
		t.Fatalf("台本の読み込みに失敗した: %v", err)
	}

	if got.Meta.Title != "Remotionで解説動画を作る" {
		t.Errorf("meta.title = %q", got.Meta.Title)
	}
	if got.Meta.Aspect != script.Aspect16x9 {
		t.Errorf("meta.aspect = %q", got.Meta.Aspect)
	}
	if got.Meta.FPS != 30 {
		t.Errorf("meta.fps = %d", got.Meta.FPS)
	}

	metan, ok := got.Speakers["metan"]
	if !ok {
		t.Fatalf("speakers に metan がない: %v", got.Speakers)
	}
	if metan.Engine != script.EngineVoicevox || metan.StyleID != 2 {
		t.Errorf("metan = %+v", metan)
	}
	if metan.SpeedScale == nil || *metan.SpeedScale != 1.05 {
		t.Errorf("metan.speedScale = %v", metan.SpeedScale)
	}
	// 未指定の音声パラメータは nil のまま（0 を明示した場合と区別できること）。
	if metan.PitchScale != nil {
		t.Errorf("metan.pitchScale は未指定なので nil のはずだが %v", *metan.PitchScale)
	}
	if zundamon := got.Speakers["zundamon"]; zundamon.SpeedScale != nil {
		t.Errorf("zundamon.speedScale は未指定なので nil のはずだが %v", *zundamon.SpeedScale)
	}

	if got.Defaults == nil {
		t.Fatalf("defaults が読めていない")
	}
	if got.Defaults.Speaker != "zundamon" || got.Defaults.Transition != script.TransitionFade {
		t.Errorf("defaults = %+v", *got.Defaults)
	}
	if got.Defaults.GapMs == nil || *got.Defaults.GapMs != 300 {
		t.Errorf("defaults.gapMs = %v", got.Defaults.GapMs)
	}
	if got.Defaults.SceneGapMs == nil || *got.Defaults.SceneGapMs != 100 {
		t.Errorf("defaults.sceneGapMs = %v", got.Defaults.SceneGapMs)
	}

	if len(got.Scenes) != 2 {
		t.Fatalf("scenes の数 = %d", len(got.Scenes))
	}
	if got.Scenes[0].Image != "assets/01-title.png" || got.Scenes[0].Transition != script.TransitionFade {
		t.Errorf("scenes[0] = %+v", got.Scenes[0])
	}
	// 予約フィールドは台本に書かれていないので空のまま。
	if got.Scenes[0].Component != "" || got.Scenes[0].Props != nil {
		t.Errorf("scenes[0] の予約フィールドが空でない: %+v", got.Scenes[0])
	}
	if len(got.Scenes[0].Lines) != 2 {
		t.Fatalf("scenes[0].lines の数 = %d", len(got.Scenes[0].Lines))
	}
	if got.Scenes[0].Lines[0].Speaker != "" {
		t.Errorf("省略された speaker は空のはずだが %q", got.Scenes[0].Lines[0].Speaker)
	}
	if got.Scenes[0].Lines[1].Speaker != "metan" {
		t.Errorf("scenes[0].lines[1].speaker = %q", got.Scenes[0].Lines[1].Speaker)
	}
	if want := "スライドショー形式の\n解説動画を作りますね\n"; got.Scenes[0].Lines[1].Text != want {
		t.Errorf("複数行のセリフ = %q, want %q", got.Scenes[0].Lines[1].Text, want)
	}
}

// TestUnmarshalReservedFields は issue #34 の予約フィールドが読めることを確かめる。
func TestUnmarshalReservedFields(t *testing.T) {
	const src = `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
defaults: {gapMs: 0, sceneGapMs: 0}
scenes:
  - image: a.png
    component: zoom
    props: {focus: [0.3, 0.6], label: {text: ここ}}
    lines: [{text: こんにちは}]
`
	var got script.Script
	if err := yaml.Unmarshal([]byte(src), &got); err != nil {
		t.Fatalf("台本の読み込みに失敗した: %v", err)
	}

	// gapMs / sceneGapMs は 0 の明示と未指定を区別できること。
	if got.Defaults.GapMs == nil || *got.Defaults.GapMs != 0 {
		t.Errorf("defaults.gapMs = %v, want 0 の明示", got.Defaults.GapMs)
	}
	if got.Defaults.SceneGapMs == nil || *got.Defaults.SceneGapMs != 0 {
		t.Errorf("defaults.sceneGapMs = %v, want 0 の明示", got.Defaults.SceneGapMs)
	}
	if got.Scenes[0].Component != "zoom" {
		t.Errorf("component = %q", got.Scenes[0].Component)
	}
	// props の中身は解釈せずそのまま保持する。
	if len(got.Scenes[0].Props) != 2 {
		t.Fatalf("props = %#v", got.Scenes[0].Props)
	}
	if _, ok := got.Scenes[0].Props["focus"]; !ok {
		t.Errorf("props.focus が失われた: %#v", got.Scenes[0].Props)
	}
}

// TestScriptMarshalsToValidJSON は Go の型から書き出した JSON が
// スキーマを満たすことを確かめる。yaml/json タグのずれを検出する。
func TestScriptMarshalsToValidJSON(t *testing.T) {
	src, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("台本の読み込みに失敗した: %v", err)
	}
	var s script.Script
	if err := yaml.Unmarshal(src, &s); err != nil {
		t.Fatalf("台本の読み込みに失敗した: %v", err)
	}

	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("JSON への書き出しに失敗した: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("JSON の読み込みに失敗した: %v", err)
	}
	if err := compileSchema(t).Validate(decoded); err != nil {
		t.Fatalf("Go の型から書き出した JSON がスキーマ検証に落ちた:\n%v", err)
	}
}

// TestZeroValueScriptMarshalsWithoutEmptyFields は空の Script を書き出したときに
// 省略可能なフィールドが出力されないことを確かめる（omitempty の付け忘れ検出）。
func TestZeroValueScriptMarshalsWithoutEmptyFields(t *testing.T) {
	encoded, err := json.Marshal(script.Script{})
	if err != nil {
		t.Fatalf("JSON への書き出しに失敗した: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("JSON の読み込みに失敗した: %v", err)
	}
	want := []string{"meta", "scenes", "speakers"}
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, want) {
		t.Errorf("空の台本の出力キー = %v, want %v", keys, want)
	}
}

// --- ここから docs/schema.json と Go の型定義のずれを検出するテスト ---

// loadSchemaDoc は docs/schema.json を素の JSON として読む。
func loadSchemaDoc(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("スキーマの読み込みに失敗した: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("スキーマの読み込みに失敗した: %v", err)
	}
	return doc
}

// objAt はスキーマの中を辿ってオブジェクトを取り出す。
func objAt(t *testing.T, doc map[string]any, path ...string) map[string]any {
	t.Helper()
	cur := doc
	for i, key := range path {
		next, ok := cur[key].(map[string]any)
		if !ok {
			t.Fatalf("スキーマに %s が無い", strings.Join(path[:i+1], "/"))
		}
		cur = next
	}
	return cur
}

// goField は Go の構造体フィールド1つぶんのタグ情報。
type goField struct {
	name      string // yaml/json でのフィールド名
	omitempty bool
}

// goFields は構造体の yaml タグを読み、フィールド名の一覧を返す。
// yaml タグと json タグが食い違っていればエラーにする。
func goFields(t *testing.T, typ reflect.Type) map[string]goField {
	t.Helper()
	out := make(map[string]goField, typ.NumField())
	for i := range typ.NumField() {
		f := typ.Field(i)
		yamlTag := f.Tag.Get("yaml")
		jsonTag := f.Tag.Get("json")
		if yamlTag != jsonTag {
			t.Errorf("%s.%s: yaml タグ %q と json タグ %q が一致しない", typ.Name(), f.Name, yamlTag, jsonTag)
			continue
		}
		if yamlTag == "" {
			t.Errorf("%s.%s: yaml タグが無い", typ.Name(), f.Name)
			continue
		}
		name, opts, _ := strings.Cut(yamlTag, ",")
		out[name] = goField{name: name, omitempty: strings.Contains(opts, "omitempty")}
	}
	return out
}

// TestGoTypesMatchSchema は docs/schema.json と Go の型定義が一致していることを確かめる。
// スキーマが唯一の正なので、ずれたらスキーマ側に合わせること。
func TestGoTypesMatchSchema(t *testing.T) {
	doc := loadSchemaDoc(t)

	tests := []struct {
		name string
		typ  reflect.Type
		node map[string]any
	}{
		{"Script", reflect.TypeOf(script.Script{}), doc},
		{"Meta", reflect.TypeOf(script.Meta{}), objAt(t, doc, "$defs", "meta")},
		{"Speaker", reflect.TypeOf(script.Speaker{}), objAt(t, doc, "$defs", "speaker")},
		{"Defaults", reflect.TypeOf(script.Defaults{}), objAt(t, doc, "$defs", "defaults")},
		{"Scene", reflect.TypeOf(script.Scene{}), objAt(t, doc, "$defs", "scene")},
		{"Line", reflect.TypeOf(script.Line{}), objAt(t, doc, "$defs", "line")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := goFields(t, tt.typ)
			props := objAt(t, tt.node, "properties")

			var goNames, schemaNames []string
			for name := range fields {
				goNames = append(goNames, name)
			}
			for name := range props {
				schemaNames = append(schemaNames, name)
			}
			slices.Sort(goNames)
			slices.Sort(schemaNames)
			if !slices.Equal(goNames, schemaNames) {
				t.Fatalf("フィールドがスキーマとずれている\n  Go:     %v\n  schema: %v", goNames, schemaNames)
			}

			// 未知のフィールドはスキーマ側で弾く方針。
			if allow, ok := tt.node["additionalProperties"].(bool); !ok || allow {
				t.Errorf("additionalProperties: false になっていない")
			}

			// 必須フィールドは omitempty を付けない（常に出力されないと検証に落ちるため）。
			// 逆に任意フィールドは omitempty を付ける（空の値を出力しないため）。
			required := map[string]bool{}
			if list, ok := tt.node["required"].([]any); ok {
				for _, name := range list {
					required[name.(string)] = true
				}
			}
			for name, f := range fields {
				if required[name] && f.omitempty {
					t.Errorf("%s は必須なので omitempty を付けてはいけない", name)
				}
				if !required[name] && !f.omitempty {
					t.Errorf("%s は任意なので omitempty を付けること", name)
				}
			}
		})
	}
}

// TestGoConstantsMatchSchema は Go の定数がスキーマの enum / default と一致していることを確かめる。
func TestGoConstantsMatchSchema(t *testing.T) {
	doc := loadSchemaDoc(t)

	enums := []struct {
		name string
		node map[string]any
		want []string
	}{
		{
			name: "meta.aspect",
			node: objAt(t, doc, "$defs", "meta", "properties", "aspect"),
			want: []string{string(script.Aspect16x9), string(script.Aspect9x16)},
		},
		{
			name: "speakers[].engine",
			node: objAt(t, doc, "$defs", "speaker", "properties", "engine"),
			want: []string{string(script.EngineVoicevox)},
		},
		{
			name: "defaults.transition",
			node: objAt(t, doc, "$defs", "defaults", "properties", "transition"),
			want: []string{string(script.TransitionFade), string(script.TransitionNone)},
		},
		{
			name: "scenes[].transition",
			node: objAt(t, doc, "$defs", "scene", "properties", "transition"),
			want: []string{string(script.TransitionFade), string(script.TransitionNone)},
		},
	}
	for _, tt := range enums {
		list, ok := tt.node["enum"].([]any)
		if !ok {
			t.Errorf("%s に enum が無い", tt.name)
			continue
		}
		var got []string
		for _, v := range list {
			got = append(got, v.(string))
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s の enum = %v, Go の定数 = %v", tt.name, got, tt.want)
		}
	}

	defaults := []struct {
		name string
		node map[string]any
		want any
	}{
		{"meta.aspect", objAt(t, doc, "$defs", "meta", "properties", "aspect"), string(script.DefaultAspect)},
		{"meta.fps", objAt(t, doc, "$defs", "meta", "properties", "fps"), float64(script.DefaultFPS)},
		{"speakers[].engine", objAt(t, doc, "$defs", "speaker", "properties", "engine"), string(script.DefaultEngine)},
		{"defaults.transition", objAt(t, doc, "$defs", "defaults", "properties", "transition"), string(script.DefaultTransition)},
		{"defaults.gapMs", objAt(t, doc, "$defs", "defaults", "properties", "gapMs"), float64(script.DefaultGapMs)},
		{"defaults.sceneGapMs", objAt(t, doc, "$defs", "defaults", "properties", "sceneGapMs"), float64(script.DefaultSceneGapMs)},
		{"scenes[].transition", objAt(t, doc, "$defs", "scene", "properties", "transition"), string(script.DefaultTransition)},
		{"scenes[].component", objAt(t, doc, "$defs", "scene", "properties", "component"), script.DefaultComponent},
	}
	for _, tt := range defaults {
		got, ok := tt.node["default"]
		if !ok {
			t.Errorf("%s に default が無い", tt.name)
			continue
		}
		if got != tt.want {
			t.Errorf("%s の default = %v, Go の定数 = %v", tt.name, got, tt.want)
		}
	}
}
