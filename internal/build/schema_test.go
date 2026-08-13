package build_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	scenaremo "github.com/takiren/scenaremo"
)

// validateAgainstSchema は書き出された props.json を docs/props.schema.json で検証する。
//
// 組み立てそのものは internal/props のテストが確かめているが、build を通した結果まで見るのは、
// 契約を満たす props.json が「実際にファイルとして出てくる」ことこそが build の成果物だからである。
// 途中の段取りが正しくても、書き出しで壊れていれば renderer は動かない。
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
