package project_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	scenaremo "github.com/takiren/scenaremo"
	"github.com/takiren/scenaremo/internal/project"
	"github.com/takiren/scenaremo/internal/script"
)

// canonicalSchemaID は docs/schema.json の $id。生成された台本が指すべき公開先でもある。
//
// テスト側で URL を書き写さないのは、スキーマが引っ越したときに
// 「雛形は直したがテストだけ古い場所を期待している」という食い違いを作らないため。
func canonicalSchemaID(t *testing.T) string {
	t.Helper()
	var head struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(scenaremo.SchemaJSON, &head); err != nil {
		t.Fatalf("スキーマを読めません: %v", err)
	}
	if head.ID == "" {
		t.Fatal("docs/schema.json に $id が無い。生成する台本の $schema の指し先が決まらない")
	}
	return head.ID
}

// newVideoDir はまだ存在しない動画ディレクトリのパスを返す。
// 親ごと無いところを渡すのは、init が videos/ep01 のような 2 階層をまとめて掘れることを含めて確かめるため。
func newVideoDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "videos", "ep01")
}

// scriptText は書き出された台本の中身を返す。
func scriptText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("台本を読めません: %v", err)
	}
	return string(data)
}

func TestInit_台本と画像置き場を作る(t *testing.T) {
	dir := newVideoDir(t)

	res, err := project.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if res.Dir != dir {
		t.Errorf("Dir が違う: got %q, want %q", res.Dir, dir)
	}
	// 台本の名前は build が探す名前でなければならない。ここがずれると、
	// init の直後に build が「台本が見つかりません」と言い出す。
	if want := filepath.Join(dir, project.ScriptNames[0]); res.ScriptPath != want {
		t.Errorf("ScriptPath が違う: got %q, want %q", res.ScriptPath, want)
	}
	if _, err := os.Stat(res.ScriptPath); err != nil {
		t.Errorf("台本が書き出されていない: %v", err)
	}

	// 画像置き場は空では意味がない。台本が参照する画像が無ければ検証が通らないため、
	// 差し替える前提のプレースホルダが入っていること。
	entries, err := os.ReadDir(filepath.Join(dir, "assets"))
	if err != nil {
		t.Fatalf("assets/ が作られていない: %v", err)
	}
	if len(entries) == 0 {
		t.Error("assets/ が空になっている")
	}

	if !slices.Contains(res.Created, res.ScriptPath) {
		t.Errorf("作ったものに台本が挙がっていない: %v", res.Created)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("何も無い場所なのに飛ばしたものがある: %v", res.Skipped)
	}
}

// TestInit_作った雛形はそのまま検証を通る は、init が壊れた台本を配っていないことを確かめる。
//
// 雛形が正しいことの唯一の保証がこれである。script.Load はスキーマ・話者の参照・画像の存在まで見るので、
// 台本と添えた画像の食い違い（ファイル名の書き間違いなど）もここで落ちる。
func TestInit_作った雛形はそのまま検証を通る(t *testing.T) {
	dir := newVideoDir(t)

	res, err := project.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	s, err := script.Load(res.ScriptPath)
	if err != nil {
		t.Fatalf("生成した台本が検証を通らない:\n%v", err)
	}
	if s.Meta.Title == "" {
		t.Error("meta.title が空の雛形になっている")
	}
	if len(s.Scenes) == 0 {
		t.Error("シーンの無い雛形になっている")
	}
}

// TestInit_作ったディレクトリはそのまま build の入り口になる は、init の出口と build の入り口が
// 同じ場所を指していることを確かめる。片方だけ名前を変えても気付けるようにしておく。
func TestInit_作ったディレクトリはそのままbuildの入り口になる(t *testing.T) {
	dir := newVideoDir(t)

	res, err := project.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	l, err := project.Resolve(dir)
	if err != nil {
		t.Fatalf("init した直後の Resolve が失敗する: %v", err)
	}
	if l.ScriptPath != res.ScriptPath {
		t.Errorf("Resolve が別の台本を選んでいる: got %q, want %q", l.ScriptPath, res.ScriptPath)
	}
}

// TestInit_生成物の置き場所は作らない は、init が .scenaremo/ を先回りして掘らないことを確かめる。
// 空の .scenaremo/ が最初から居座ると、CLI が作る場所と人が書く場所の境目がぼやける。
func TestInit_生成物の置き場所は作らない(t *testing.T) {
	dir := newVideoDir(t)

	if _, err := project.Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, project.OutDirName)); !os.IsNotExist(err) {
		t.Errorf("%s ができている (err=%v)", project.OutDirName, err)
	}
}

// TestInit_台本の先頭にスキーマ参照を入れる は、エディタでの補完と検証の入り口を必ず添えることを確かめる
// （→ README「台本の書き方」）。
func TestInit_台本の先頭にスキーマ参照を入れる(t *testing.T) {
	dir := newVideoDir(t)

	res, err := project.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	head, _, _ := strings.Cut(scriptText(t, res.ScriptPath), "\n")
	if !strings.HasPrefix(head, "# yaml-language-server: $schema=") {
		t.Errorf("1 行目がスキーマ参照になっていない: %q", head)
	}
	if !strings.Contains(head, res.SchemaRef) {
		t.Errorf("1 行目に %q が入っていない: %q", res.SchemaRef, head)
	}
}

// TestInit_手元にスキーマが無ければ公開されている場所を指す は、リポジトリの外で init したときの挙動。
// 相対パスを書いても指す先が無いので、URL でなければ補完も検証も効かない。
func TestInit_手元にスキーマが無ければ公開されている場所を指す(t *testing.T) {
	dir := newVideoDir(t)

	res, err := project.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if want := canonicalSchemaID(t); res.SchemaRef != want {
		t.Errorf("SchemaRef が違う: got %q, want %q", res.SchemaRef, want)
	}
}

// TestInit_手元にスキーマがあれば相対パスで指す は、リポジトリの中で init したときの挙動。
// エディタがネットワークを見に行かずに済み、直したスキーマがその場で効く。
func TestInit_手元にスキーマがあれば相対パスで指す(t *testing.T) {
	root := t.TempDir()
	writeSchema(t, root, scenaremo.SchemaJSON)
	dir := filepath.Join(root, "videos", "ep01")

	res, err := project.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// README の台本の例と同じ形。区切りは OS に依らず "/"。
	if want := "../../docs/schema.json"; res.SchemaRef != want {
		t.Errorf("SchemaRef が違う: got %q, want %q", res.SchemaRef, want)
	}
	if !strings.Contains(scriptText(t, res.ScriptPath), res.SchemaRef) {
		t.Error("台本に相対パスが書かれていない")
	}
}

// TestInit_別物のスキーマは指さない は、たまたま上の階層にあった docs/schema.json を掴まないことを確かめる。
// 掴んでしまうと、台本を書いている最中に見当違いの検証エラーが出続ける。
func TestInit_別物のスキーマは指さない(t *testing.T) {
	root := t.TempDir()
	writeSchema(t, root, []byte(`{"$id":"https://example.com/other.schema.json"}`))
	dir := filepath.Join(root, "videos", "ep01")

	res, err := project.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if want := canonicalSchemaID(t); res.SchemaRef != want {
		t.Errorf("別物のスキーマを指している: got %q, want %q", res.SchemaRef, want)
	}
}

// writeSchema は root/docs/schema.json を置く。リポジトリの中で init した状況を作るのに使う。
func writeSchema(t *testing.T, root string, data []byte) {
	t.Helper()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatalf("docs/ を作れません: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docs, "schema.json"), data, 0o644); err != nil {
		t.Fatalf("スキーマを置けません: %v", err)
	}
}

// TestInit_既にある台本は上書きしない は、人が書いた唯一の入力を消し飛ばさないことを確かめる。
//
// 候補の名前 (ScriptNames) をすべて見るのは、script.yml がある場所へ script.yaml を作ると、
// build が新しいほうを選んで、人が書いた台本が黙って無視されるためである。
func TestInit_既にある台本は上書きしない(t *testing.T) {
	for _, name := range project.ScriptNames {
		t.Run(name, func(t *testing.T) {
			dir := newVideoDir(t)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("ディレクトリを作れません: %v", err)
			}
			const mine = "meta:\n  title: 書きかけ\n"
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
				t.Fatalf("台本を置けません: %v", err)
			}

			_, err := project.Init(dir)
			if err == nil {
				t.Fatal("既に台本があるのに成功した")
			}
			// 何が邪魔をしているのかと、どうすれば作り直せるのかを両方伝える。
			wantContains(t, err.Error(), dir, name, "上書き", "消して")

			if got := scriptText(t, path); got != mine {
				t.Errorf("台本が書き換えられている: %q", got)
			}
		})
	}
}

// TestInit_同じ場所へ二度実行しても書いたものが残る は、init を打ち直したときの事故を防げていることを確かめる。
func TestInit_同じ場所へ二度実行しても書いたものが残る(t *testing.T) {
	dir := newVideoDir(t)

	res, err := project.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	mine := scriptText(t, res.ScriptPath) + "\n# 自分で書き足した行\n"
	if err := os.WriteFile(res.ScriptPath, []byte(mine), 0o644); err != nil {
		t.Fatalf("台本を書き換えられません: %v", err)
	}

	if _, err := project.Init(dir); err == nil {
		t.Fatal("2 回目が成功している")
	}
	if got := scriptText(t, res.ScriptPath); got != mine {
		t.Error("2 回目の init で書き足した行が消えている")
	}
}

// TestInit_既にある画像は触らない は、台本が無ければ展開は進めつつ、
// 先に置かれていた画像は自分のものとして扱わないことを確かめる。
func TestInit_既にある画像は触らない(t *testing.T) {
	dir := newVideoDir(t)
	assets := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatalf("assets/ を作れません: %v", err)
	}
	mine := filepath.Join(assets, "01-title.png")
	const content = "これは自分で用意した画像"
	if err := os.WriteFile(mine, []byte(content), 0o644); err != nil {
		t.Fatalf("画像を置けません: %v", err)
	}

	res, err := project.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if !slices.Contains(res.Skipped, mine) {
		t.Errorf("触らなかったものが挙がっていない: %v", res.Skipped)
	}
	if slices.Contains(res.Created, mine) {
		t.Errorf("既にあるものを作ったことにしている: %v", res.Created)
	}
	data, err := os.ReadFile(mine)
	if err != nil {
		t.Fatalf("画像を読めません: %v", err)
	}
	if string(data) != content {
		t.Error("既にある画像が雛形で上書きされている")
	}
}

// TestInit_ディレクトリでない場所には作らない は、同じ名前のファイルがあるときに
// 「作れません」より前に理由を言えることを確かめる。
func TestInit_ディレクトリでない場所には作らない(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ep01")
	if err := os.WriteFile(path, []byte("なにか"), 0o644); err != nil {
		t.Fatalf("ファイルを置けません: %v", err)
	}

	_, err := project.Init(path)
	if err == nil {
		t.Fatal("ファイルなのに成功した")
	}
	wantContains(t, err.Error(), path, "ディレクトリ")
}

func TestInit_空文字列は受け付けない(t *testing.T) {
	_, err := project.Init("")
	if err == nil {
		t.Fatal("空文字列なのにエラーにならない")
	}
	// カレントディレクトリへ雛形を撒くよりは、指定が無いと言われたほうが直しようがある。
	wantContains(t, err.Error(), "指定")
}
