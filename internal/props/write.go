package props

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FileName は動画ディレクトリの .scenaremo/ 以下に置く props.json のファイル名。
const FileName = "props.json"

// Marshal は props.json のバイト列を作る。
//
// 同じ入力からは必ず同じバイト列になる。台本を1文字も変えていないのに props.json が
// 毎回変わると、build のたびに何が変わったのかを追えなくなるため。
// 構造体の並びがそのままキーの並びになり、map (scenes[].props) は encoding/json がキー順に並べる。
func Marshal(p *Props) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// 既定では < > & が < のような表記へ置き換えられる。props.json はブラウザへ直接埋め込むものではなく、
	// 不具合の調査で人が読むほうが多いので、台本に書いたままの文字で出す。
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return nil, fmt.Errorf("props.json を組み立てられません: %w", err)
	}
	// Encode は末尾に改行を付ける。テキストファイルとして素直な形なのでそのまま使う。
	return buf.Bytes(), nil
}

// WriteFile は path へ props.json を書き出す。親ディレクトリが無ければ作る。
//
// 一時ファイルへ書いてから rename するのは、書き込みの途中で中断したときに
// 壊れた props.json を残さないため。壊れた JSON が残ると、次に renderer が読んだときの
// エラーが「合成に失敗した」ではなく「JSON が壊れている」になり、原因が分からなくなる。
//
// io.Writer を受け取る形にはしていない。writer を受け取る時点で出力先は既に開かれており、
// 一時ファイルへの書き込みと rename も、親ディレクトリの作成も表現できなくなるためである。
// バイト列が欲しいだけなら Marshal を使えばよく、writer へ流すのも呼び出し側で 1 行で済む。
func WriteFile(path string, p *Props) error {
	data, err := Marshal(p)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("出力先のディレクトリを作れません: %w", err)
	}

	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return fmt.Errorf("props.json を書き出せません: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("props.json を保存できません: %w", err)
	}
	return nil
}
