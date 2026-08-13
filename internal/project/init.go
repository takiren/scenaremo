package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	scenaremo "github.com/takiren/scenaremo"
)

// videoTemplateDir は埋め込まれた雛形のうち、動画 1 本ぶんに使う枝（→ templates.go）。
//
// templates/ を丸ごと展開しないのは、eject（→ issue #22）が renderer の雛形をここへ足すためで、
// 動画ディレクトリに renderer 一式が落ちてくるようなことにならないよう、枝を明示して取り出す。
const videoTemplateDir = "templates/video"

// InitResult は Init が動画ディレクトリへ何をしたかを表す。
type InitResult struct {
	// Dir は雛形を展開した動画ディレクトリ。渡されたパスの形をそのまま引き継ぐ（→ Resolve）。
	Dir string

	// ScriptPath は書き出した台本。人が次に開くファイル。
	ScriptPath string

	// Created は新しく作ったファイル。
	Created []string

	// Skipped は既にあったので触らなかったファイル。
	// 黙って飛ばすと「雛形の画像に差し替わっているはず」という誤解を生むので、呼び出し側へ返す。
	Skipped []string

	// SchemaRef は台本の先頭に書いた $schema の指し先（→ schemaRef）。
	SchemaRef string
}

// Init は dir に動画 1 本ぶんの雛形（台本と assets/）を展開する。
//
// パッケージの説明にある「ディレクトリは作らない」は Resolve の話である。場所を決めるだけの Resolve と、
// 決めた場所を実際に作る Init を同じパッケージに置いているのは、台本の名前 (ScriptNames) を知っているのが
// このパッケージ 1 つであってほしいためで、作る側を別に出すと「探す場所」と「作る場所」がずれても
// 誰も気付けない（→ README のディレクトリ構成。project の役割は init / eject）。
//
// 動画ディレクトリの中の形（assets/ という名前や画像の枚数）は雛形の側にしか書いていない。
// CLI は台本に書かれたパスで画像を読むだけで assets/ を特別扱いしないので、
// Go に定数を置いても誰も参照せず、雛形と定数のどちらが正なのか分からなくなる。
//
// 生成物の置き場所 (OutDirName) はここでは作らない。掘るのは書き出す側の責務であり、
// 空の .scenaremo/ が最初から居座ると「CLI が作る場所」と「人が書く場所」の境目がぼやける。
//
// 既にあるファイルは上書きしない（→ checkTarget と writeNew）。
func Init(dir string) (*InitResult, error) {
	if dir == "" {
		return nil, errors.New("動画ディレクトリが指定されていません。" +
			"scenaremo init videos/ep01 のように、作る場所を指定してください")
	}
	clean := filepath.Clean(dir)

	if err := checkTarget(clean); err != nil {
		return nil, err
	}

	// 雛形を取り出せるかどうかは、ディレクトリを作る前に確かめる。
	// 焼き込みそこねたバイナリで空のディレクトリだけが残るのは、後始末を利用者に押し付けることになる。
	src, err := fs.Sub(scenaremo.Templates, videoTemplateDir)
	if err != nil {
		return nil, fmt.Errorf("雛形を取り出せません: %w", err)
	}

	if err := os.MkdirAll(clean, 0o755); err != nil {
		return nil, fmt.Errorf("%s を作れません: %w", clean, err)
	}

	res := &InitResult{Dir: clean, SchemaRef: schemaRef(clean)}
	if err := fs.WalkDir(src, ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		dest := filepath.Join(clean, filepath.FromSlash(name))
		if d.IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return fmt.Errorf("%s を作れません: %w", dest, err)
			}
			return nil
		}

		data, err := fs.ReadFile(src, name)
		if err != nil {
			return fmt.Errorf("雛形 %s を読めません: %w", name, err)
		}
		// 台本かどうかは ScriptNames で判断する。雛形のファイル名をここで書き写すと、
		// build が探す名前と雛形の名前が別々に決まることになる。
		isScript := slices.Contains(ScriptNames, name)
		if isScript {
			data, err = withSchemaRef(data, res.SchemaRef)
			if err != nil {
				return err
			}
		}

		created, err := writeNew(dest, data)
		if err != nil {
			return err
		}
		if created {
			res.Created = append(res.Created, dest)
		} else {
			res.Skipped = append(res.Skipped, dest)
		}
		if isScript {
			res.ScriptPath = dest
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if res.ScriptPath == "" {
		// 雛形に台本が無ければ、作ったのは画像置き場だけということになる。
		// 成功として返すと、利用者は空のディレクトリを前に何をすればよいか分からない。
		return nil, fmt.Errorf("雛形に台本が含まれていません。探したのは %s です",
			strings.Join(ScriptNames, ", "))
	}
	return res, nil
}

// checkTarget は雛形を展開してよい場所かどうかを確かめる。
//
// 既にある台本は上書きしない。--force のようなフラグも用意していない。台本は「人が書く唯一の入力」で、
// CLI が作り直せるものは一つも入っていないためである（→ README「設計方針 1」）。
// 取り返しの付かない上書きをフラグ 1 つで踏めるようにする価値があるのは、上書きが日常の操作である場合だけで、
// init は動画 1 本につき一度しか打たない。消してからやり直す手間より、
// 消し飛ばした台本を書き直す手間のほうがはるかに大きい。
func checkTarget(dir string) error {
	info, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		// これから作る場所。何も無いのが普通の入り口。
		return nil
	case err != nil:
		return fmt.Errorf("%s を確認できません: %w", dir, err)
	case !info.IsDir():
		return fmt.Errorf("%s は既にあるファイルです。動画ディレクトリの名前を指定してください "+
			"(例: scenaremo init videos/ep01)", dir)
	}

	for _, name := range ScriptNames {
		path := filepath.Join(dir, name)
		// Lstat なのは、壊れたシンボリックリンクでも「その名前は既に使われている」と伝えるため。
		// 種類を問わず、その名前で台本を書き出せない状況に変わりはない。
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("%s には既に %s があります。scenaremo init は人が書いたものを上書きしません。"+
				"別のディレクトリを指定するか、雛形から作り直すなら %s を消してから実行してください",
				dir, name, path)
		}
	}
	return nil
}

// writeNew は path へ書き出す。既に何かあれば触らず false を返す。
//
// O_EXCL で開くのは、「無いことを確かめてから書く」の間にエディタや別のシェルが同じ名前を作る隙を
// 無くすため。先に Stat して分岐すると、その隙間に置かれたファイルを上書きしてしまう。
//
// 一時ファイルへ書いてから rename する props.WriteFile の作りは採らない。rename は同じ名前のものを
// 黙って置き換えるので、「既にあるものは触らない」という約束と噛み合わない。
func writeNew(path string, data []byte) (bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("%s を作れません: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return false, fmt.Errorf("%s を書き出せません: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return false, fmt.Errorf("%s を保存できません: %w", path, err)
	}
	return true, nil
}

// canonicalSchemaURL は公開されているスキーマの場所。docs/schema.json の $id をそのまま使う。
//
// ここへ URL を書き写すと、スキーマの引っ越しに追従しそこねる写しが 1 つ増える。
// 雛形 (templates/video/script.yaml) が先頭に書いているのもこの URL で、
// 一致していることは init のたびに確かめる（→ withSchemaRef）。
var canonicalSchemaURL = schemaID(scenaremo.SchemaJSON)

// schemaID は JSON Schema の $id を取り出す。読めなければ空文字列。
func schemaID(data []byte) string {
	var head struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return ""
	}
	return head.ID
}

// schemaRef は生成する台本の先頭に書く $schema の指し先を決める。
//
// 手元に docs/schema.json があればそこへの相対パスを、無ければ公開されている URL を返す。
// 使い分けるのは、init がリポジトリの中でも外でも打たれるためである。
// README のディレクトリ構成どおり videos/ をリポジトリの中に置く使い方では、相対パスのほうが
// エディタをネットワークに繋がずに済み、スキーマを直した瞬間にその変更が補完と検証へ反映される
// （README の台本の例が ../../docs/schema.json なのはこの形）。
// 一方、go install したバイナリで ~/videos/ep01 のような場所へ作るときに相対パスを書くと、
// 何も無い場所を指す参照が残るだけなので、公開されている URL を指すほかない。
//
// 埋め込んだスキーマを動画ディレクトリへ書き出して指す案は採らない。正が二つになる（→ schema.go）。
func schemaRef(dir string) string {
	if ref, ok := localSchemaRef(dir); ok {
		return ref
	}
	return canonicalSchemaURL
}

// localSchemaRef は dir から親をたどって docs/schema.json を探し、動画ディレクトリからの相対パスを返す。
//
// 見つけただけでは採らず、$id が埋め込みスキーマのものと一致することまで確かめる。
// docs/schema.json はありふれた名前で、たまたま上の階層にあった別物を指してしまうと、
// 台本を書いている最中に見当違いの検証エラーが出続けることになる。
func localSchemaRef(dir string) (string, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	if canonicalSchemaURL == "" {
		return "", false
	}

	for cur := abs; ; {
		candidate := filepath.Join(cur, "docs", "schema.json")
		if data, err := os.ReadFile(candidate); err == nil && schemaID(data) == canonicalSchemaURL {
			rel, err := filepath.Rel(abs, candidate)
			if err != nil {
				return "", false
			}
			// yaml-language-server が読むのは台本の中の文字列なので、区切りは OS に依らず "/" にする。
			// Windows で作った台本を macOS で開いても同じ場所を指すようにするため。
			return filepath.ToSlash(rel), true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", false
		}
		cur = parent
	}
}

// withSchemaRef は雛形の $schema の指し先を差し替える。
//
// 雛形にはあらかじめ本物の URL を書いてある。置換前の状態でも yaml-language-server が働くので、
// 雛形そのものを開いて直すときに補完と検証が効く。差し替えは 1 箇所だけを狙うが、
// 見つからなければ黙って進めずに失敗する。$schema コメントの無い台本を配ると、
// 補完も検証も効かない状態を「そういうもの」として利用者に押し付けることになるためである。
func withSchemaRef(data []byte, ref string) ([]byte, error) {
	if canonicalSchemaURL == "" {
		return nil, errors.New("docs/schema.json に $id がありません。" +
			"スキーマの $id を戻すか、雛形の $schema の指し先を見直してください")
	}
	s := string(data)
	if n := strings.Count(s, canonicalSchemaURL); n != 1 {
		return nil, fmt.Errorf("雛形の $schema の指し先が %d 箇所あります。"+
			"%s/script.yaml の先頭行が %s を指しているか確かめてください",
			n, videoTemplateDir, canonicalSchemaURL)
	}
	if ref == canonicalSchemaURL {
		return data, nil
	}
	return []byte(strings.Replace(s, canonicalSchemaURL, ref, 1)), nil
}
