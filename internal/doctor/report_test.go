package doctor

import (
	"strings"
	"testing"
)

func TestReportText_失敗した項目には対処がぶら下がる(t *testing.T) {
	r := Report{Checks: []Check{
		{Name: "Node.js", Status: StatusOK, Detail: "v22.11.0"},
		{Name: "pnpm", Status: StatusNG, Detail: "見つかりません", Actions: []string{
			"corepack enable pnpm を実行してください",
			"npm install -g pnpm でも構いません",
		}},
	}}

	got := r.Text()
	wantContains(t, got,
		"[ OK ] Node.js: v22.11.0",
		"[ NG ] pnpm: 見つかりません",
		actionIndent+"→ corepack enable pnpm を実行してください",
		actionIndent+"→ npm install -g pnpm でも構いません",
	)

	// 末尾は、上へ戻らなくても次の一手が分かる 1 行で締める
	wantContains(t, got, "2 件中 1 件が要対応です（pnpm）", "もう一度 scenaremo doctor")

	if !strings.HasSuffix(got, "\n") {
		t.Errorf("末尾が改行で終わっていない: %q", got)
	}
}

func TestReportText_すべて満たしていれば次の一歩を示す(t *testing.T) {
	r := Report{Checks: []Check{
		{Name: "Node.js", Status: StatusOK, Detail: "v22.11.0"},
		{Name: "書き込み権限", Status: StatusOK, Detail: "/tmp に書き込めます"},
	}}

	got := r.Text()
	wantContains(t, got, "2 件すべて問題ありません", "scenaremo init")
	if strings.Contains(got, "→") {
		t.Errorf("対処が無いのに → が出ている:\n%s", got)
	}
}

func TestReportOK_注意は要対応にしない(t *testing.T) {
	r := Report{Checks: []Check{
		{Name: "Node.js", Status: StatusWarn, Detail: "バージョンを判別できませんでした"},
	}}
	if !r.OK() {
		t.Error("注意だけで要対応になっている")
	}
	if len(r.Failures()) != 0 {
		t.Errorf("注意が失敗として数えられている: %+v", r.Failures())
	}

	// ただし出力には残ること。判定できなかったことを黙って OK にはしない。
	wantContains(t, r.Text(), "[ 注意 ] Node.js")
}

func TestStatusLabel(t *testing.T) {
	tests := map[Status]string{
		StatusOK:    "OK",
		StatusWarn:  "注意",
		StatusNG:    "NG",
		Status(999): "??",
	}
	for status, want := range tests {
		if got := status.Label(); got != want {
			t.Errorf("Label が違う: got %q, want %q", got, want)
		}
	}
}
