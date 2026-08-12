// Package scenaremo は、CLI 全体で共有する資産を提供する。
package scenaremo

import _ "embed"

// SchemaJSON は台本の JSON Schema。
//
// docs/schema.json がスキーマの唯一の正であり、実行時の検証にも同じファイルを使う。
// エディタ (yaml-language-server) と CLI が同じ定義を見ることで、
// 「エディタでは通るのに CLI で落ちる」という食い違いが起きないようにするため。
//
// 埋め込みディレクティブは親ディレクトリを参照できないため internal/script からは埋め込めない。
// リポジトリルートのこのパッケージが埋め込み、各パッケージはここから受け取る。
// docs/schema.json を別の場所へ複製すると正が二つになるので、複製はしないこと。
//
//go:embed docs/schema.json
var SchemaJSON []byte

// PropsSchemaJSON は props.json の JSON Schema。
//
// props.json は CLI が生成して Remotion が読むだけなので、台本と違って実行時に検証する相手はいない。
// それでもスキーマを置くのは、これが CLI と renderer の契約書そのものだからで、
// Go の型 (internal/props) と renderer 側の zod はどちらもこの定義に従う。
// CLI 側は自分の出力がこの契約を満たすことをテストで確かめる（→ internal/props の schema_test.go）。
//
//go:embed docs/props.schema.json
var PropsSchemaJSON []byte
