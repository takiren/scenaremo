package script_test

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	// schemaPath は台本スキーマの唯一の正。
	schemaPath = "../../docs/schema.json"
	// examplePath は README の例に対応する最小の台本。
	examplePath = "../../examples/minimal/script.yaml"
)

// compileSchema は docs/schema.json をコンパイルする。
func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	c := jsonschema.NewCompiler()
	sch, err := c.Compile(schemaPath)
	if err != nil {
		t.Fatalf("docs/schema.json のコンパイルに失敗した: %v", err)
	}
	return sch
}

// decodeYAML は YAML を JSON 相当の汎用値（map[string]any など）へ読み込む。
// jsonschema はこの形の値を検証する。
func decodeYAML(t *testing.T, src []byte) any {
	t.Helper()
	var v any
	if err := yaml.Unmarshal(src, &v); err != nil {
		t.Fatalf("YAML の読み込みに失敗した: %v", err)
	}
	return v
}

// failures は検証エラーを "インスタンス位置:キーワード" の一覧へ平坦化する。
// 例: "/meta:required", "/scenes/0/lines/0/text:minLength"
func failures(t *testing.T, err error) []string {
	t.Helper()
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("*jsonschema.ValidationError ではないエラーが返った: %v", err)
	}
	var out []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if kw := e.ErrorKind.KeywordPath(); len(kw) > 0 {
			out = append(out, "/"+strings.Join(e.InstanceLocation, "/")+":"+strings.Join(kw, "/"))
		}
		for _, cause := range e.Causes {
			walk(cause)
		}
	}
	walk(ve)
	return out
}

// TestSchemaCompiles はスキーマ自体が draft 2020-12 として妥当であることを確かめる。
func TestSchemaCompiles(t *testing.T) {
	compileSchema(t)
}

// TestMinimalExampleIsValid は examples/minimal/script.yaml がスキーマを満たすことを確かめる。
func TestMinimalExampleIsValid(t *testing.T) {
	src, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("台本の読み込みに失敗した: %v", err)
	}
	if err := compileSchema(t).Validate(decodeYAML(t, src)); err != nil {
		t.Fatalf("最小の台本がスキーマ検証に落ちた:\n%v", err)
	}
}

// TestMinimalExampleHasSchemaComment は台本の先頭に
// yaml-language-server 向けのスキーマ参照コメントがあることを確かめる。
// これが VS Code での補完・検証の入り口になる。
func TestMinimalExampleHasSchemaComment(t *testing.T) {
	src, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("台本の読み込みに失敗した: %v", err)
	}
	first, _, _ := strings.Cut(string(src), "\n")
	const want = "# yaml-language-server: $schema=../../docs/schema.json"
	if strings.TrimRight(first, "\r") != want {
		t.Errorf("先頭行が %q ではなく %q だった", want, first)
	}
}

// TestValidScripts は妥当な台本が通ることを確かめる。
func TestValidScripts(t *testing.T) {
	sch := compileSchema(t)

	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "必須フィールドだけ",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "話者エイリアスは日本語でもよい",
			yaml: `
meta: {title: t}
speakers: {ずんだもん: {styleId: 3}}
scenes: [{image: a.png, lines: [{speaker: ずんだもん, text: こんにちは}]}]
`,
		},
		{
			name: "音声パラメータを指定できる",
			yaml: `
meta: {title: t, aspect: "9:16", fps: 60}
speakers:
  metan: {engine: voicevox, styleId: 2, speedScale: 1.05, pitchScale: -0.05, intonationScale: 1.2, volumeScale: 0.8}
defaults: {speaker: metan, transition: none, gapMs: 0, sceneGapMs: 0}
scenes: [{image: a.png, transition: none, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "component と props は予約済みで受け付ける",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes:
  - image: a.png
    component: zoom
    props: {focus: [0.3, 0.6], label: {text: ここ, bold: true}, scale: 1.5, count: 3, flag: null}
    lines: [{text: こんにちは}]
`,
		},
		{
			name: "props の中身は自由なので未知のキーでも弾かない",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, props: {なんでも: [1, {深い: {入れ子: true}}]}, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "JSON 台本向けに $schema キーを書ける",
			yaml: `
$schema: ../../docs/schema.json
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := sch.Validate(decodeYAML(t, []byte(tt.yaml))); err != nil {
				t.Errorf("妥当な台本が弾かれた:\n%v", err)
			}
		})
	}
}

// TestInvalidScripts は不正な台本がきちんと弾かれることを確かめる。
// want には「どこで」「どのキーワードに」引っかかってほしいかを書く。
func TestInvalidScripts(t *testing.T) {
	sch := compileSchema(t)

	tests := []struct {
		name string
		want string
		yaml string
	}{
		{
			name: "meta がない",
			want: "/:required",
			yaml: `
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "meta.title がない",
			want: "/meta:required",
			yaml: `
meta: {fps: 30}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "meta.title が空",
			want: "/meta/title:minLength",
			yaml: `
meta: {title: ""}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "aspect の値が不正",
			want: "/meta/aspect:enum",
			yaml: `
meta: {title: t, aspect: "4:3"}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "fps が数値でない",
			want: "/meta/fps:type",
			yaml: `
meta: {title: t, fps: "30"}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "fps が 0",
			want: "/meta/fps:minimum",
			yaml: `
meta: {title: t, fps: 0}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "meta のフィールド名の打ち間違い",
			want: "/meta:additionalProperties",
			yaml: `
meta: {title: t, ftp: 30}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "speakers がない",
			want: "/:required",
			yaml: `
meta: {title: t}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "speakers が空",
			want: "/speakers:minProperties",
			yaml: `
meta: {title: t}
speakers: {}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "styleId がない",
			want: "/speakers/zundamon:required",
			yaml: `
meta: {title: t}
speakers: {zundamon: {engine: voicevox}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "styleId が整数でない",
			want: "/speakers/zundamon/styleId:type",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: "3"}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "engine が未知",
			want: "/speakers/zundamon/engine:enum",
			yaml: `
meta: {title: t}
speakers: {zundamon: {engine: aivisspeech, styleId: 3}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "音声パラメータ名の打ち間違い",
			want: "/speakers/zundamon:additionalProperties",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3, speedscale: 1.1}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "pitchScale が範囲外",
			want: "/speakers/zundamon/pitchScale:maximum",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3, pitchScale: 1.0}}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "defaults のフィールド名の打ち間違い",
			want: "/defaults:additionalProperties",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
defaults: {gapms: 300}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "defaults.transition の値が不正",
			want: "/defaults/transition:enum",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
defaults: {transition: dissolve}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "gapMs が負",
			want: "/defaults/gapMs:minimum",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
defaults: {gapMs: -1}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "sceneGapMs が負",
			want: "/defaults/sceneGapMs:minimum",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
defaults: {sceneGapMs: -1}
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "scenes がない",
			want: "/:required",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
`,
		},
		{
			name: "scenes が空",
			want: "/scenes:minItems",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes: []
`,
		},
		{
			name: "scenes[].image がない",
			want: "/scenes/0:required",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes: [{lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "scenes[].lines がない",
			want: "/scenes/0:required",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png}]
`,
		},
		{
			name: "scenes[].lines が空",
			want: "/scenes/0/lines:minItems",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, lines: []}]
`,
		},
		{
			name: "scenes[].transition の値が不正",
			want: "/scenes/1/transition:enum",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes:
  - {image: a.png, lines: [{text: こんにちは}]}
  - {image: b.png, transition: wipe, lines: [{text: またね}]}
`,
		},
		{
			name: "scenes[].component が識別子として不正",
			want: "/scenes/0/component:pattern",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, component: "zoom scene!", lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "scenes[].props がオブジェクトでない",
			want: "/scenes/0/props:type",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, props: [1, 2], lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "シーンのフィールド名の打ち間違い",
			want: "/scenes/0:additionalProperties",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, line: [{text: こんにちは}], lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "lines[].text がない",
			want: "/scenes/0/lines/0:required",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, lines: [{speaker: zundamon}]}]
`,
		},
		{
			name: "lines[].text が空",
			want: "/scenes/0/lines/1/text:minLength",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, lines: [{text: こんにちは}, {text: ""}]}]
`,
		},
		{
			name: "セリフのフィールド名の打ち間違い",
			want: "/scenes/0/lines/0:additionalProperties",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scenes: [{image: a.png, lines: [{txet: こんにちは}]}]
`,
		},
		{
			name: "トップレベルのフィールド名の打ち間違い",
			want: "/:additionalProperties",
			yaml: `
meta: {title: t}
speakers: {zundamon: {styleId: 3}}
scene: [{image: a.png, lines: [{text: こんにちは}]}]
scenes: [{image: a.png, lines: [{text: こんにちは}]}]
`,
		},
		{
			name: "台本がオブジェクトでない",
			want: "/:type",
			yaml: `- image: a.png
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sch.Validate(decodeYAML(t, []byte(tt.yaml)))
			if err == nil {
				t.Fatalf("不正な台本が通ってしまった")
			}
			got := failures(t, err)
			if slices.Contains(got, tt.want) {
				return
			}
			t.Errorf("期待した検証エラー %q が出なかった。実際: %v", tt.want, got)
		})
	}
}
