package scenaremo

import "embed"

// Templates は scenaremo init が展開する雛形一式（→ issue #14）。
//
// バイナリへ焼き込むのは、動画ディレクトリを 1 つ作るのにネットワークも Node も要らない状態を
// 保つため。npx create-video を毎回叩く形にしないのは、対話に付き合わされること、
// 実行のたびに install が走ること、生成物が create-video の版に引きずられて
// 同じ入力から同じ雛形が出てこなくなることの 3 つが理由である（→ issue #14）。
//
// 埋め込みディレクティブは親ディレクトリを参照できないため internal/project からは埋め込めない。
// リポジトリルートのこのパッケージが埋め込み、どこへどう展開するかは internal/project が決める
// （schema.go が docs/schema.json を埋め込んでいるのと同じ事情）。
//
// all: は付けない。付けるとドット始まりのファイルまで対象になり、macOS で紛れ込む .DS_Store を
// 利用者のディレクトリへ撒くことになる。ドット始まりの雛形（.gitignore など）が要るようになったら、
// 別の名前で置いて展開するときに付け直すこと。
//
//go:embed templates
var Templates embed.FS
