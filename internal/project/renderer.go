package project

import (
	"os"
	"path/filepath"
)

// FindRendererDir は start から親をたどって renderer/ を探す（→ issue #19）。
//
// videos/ep01 のような作業中のディレクトリから preview や doctor を実行しても
// 共有レンダラを見つけられるようにするため。
// package.json の有無まで見るのは、たまたま renderer という名前のディレクトリを掴まないようにするため。
func FindRendererDir(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		candidate := filepath.Join(dir, "renderer")
		if _, err := os.Stat(filepath.Join(candidate, "package.json")); err == nil {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
