package props_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	scenaremo "github.com/takiren/scenaremo"
	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/script"
)

// validateAgainstSchema は props.json のバイト列を docs/props.schema.json で検証する。
//
// props.json は CLI が作って Remotion が読むだけなので、実行時に検証する相手はいない。
// それでもここで確かめるのは、docs/props.schema.json が renderer 側 zod の拠り所であり、
// Go の型がそこから静かにずれると、乖離に気づくのがレンダリング時になってしまうため。
func validateAgainstSchema(t *testing.T, data []byte) {
	t.Helper()

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("props.json を JSON として読めない: %v", err)
	}

	schemaDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(scenaremo.PropsSchemaJSON))
	if err != nil {
		t.Fatalf("props.schema.json を読めない: %v", err)
	}
	const id = "https://raw.githubusercontent.com/takiren/scenaremo/main/docs/props.schema.json"
	c := jsonschema.NewCompiler()
	if err := c.AddResource(id, schemaDoc); err != nil {
		t.Fatalf("スキーマを登録できない: %v", err)
	}
	sch, err := c.Compile(id)
	if err != nil {
		t.Fatalf("スキーマをコンパイルできない: %v", err)
	}

	if err := sch.Validate(doc); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			t.Fatalf("props.json がスキーマに合わない:\n%v", ve)
		}
		t.Fatalf("props.json を検証できない: %v", err)
	}
}

// TestBuildMatchesSchema は Build の出力が契約を満たすことを確かめる。
func TestBuildMatchesSchema(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(in *props.Input)
	}{
		{
			name:   "基本の形",
			mutate: func(*props.Input) {},
		},
		{
			name: "縦型",
			mutate: func(in *props.Input) {
				in.Script.Meta.Aspect = script.Aspect9x16
			},
		},
		{
			name: "コンポーネント指名と props",
			mutate: func(in *props.Input) {
				in.Script.Scenes[0].Component = "zoom"
				in.Script.Scenes[0].Props = map[string]any{"focus": []any{0.3, 0.6}}
			},
		},
		{
			name: "トランジション無し",
			mutate: func(in *props.Input) {
				in.Script.Scenes[0].Transition = script.TransitionNone
				in.Script.Scenes[1].Transition = script.TransitionNone
			},
		},
		{
			name: "話者が1人だけ",
			mutate: func(in *props.Input) {
				in.Script.Scenes[0].Lines = in.Script.Scenes[0].Lines[:1]
				in.Audio[0] = in.Audio[0][:1]
			},
		},
		{
			// UUID を返さないエンジンでもクレジットは成立する。
			name: "話者の UUID が無い",
			mutate: func(in *props.Input) {
				for alias, c := range in.Credits {
					in.Credits[alias] = props.SpeakerCredit{Name: c.Name}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := baseInput()
			tt.mutate(&in)

			data, err := props.Marshal(build(t, in))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			validateAgainstSchema(t, data)
		})
	}
}
