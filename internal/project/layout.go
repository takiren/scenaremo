// Package project は動画ディレクトリの中の「どこに何があるか」を決める。
//
// 台本も wav も props.json も、置き場所を知っているのは1箇所であるべきだという理由でここに集めた。
// build が「台本を探す」「wav を書く」「props.json を書く」でそれぞれ組み立てていると、
// props.json に載せるパスと実際に書いた場所がずれても誰も気付けない。
// 決めるのはパスだけで、ディレクトリは作らないし台本の中身も読まない（読むのは script.Load の仕事）。
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/takiren/scenaremo/internal/props"
)

// OutDirName は生成物を置くディレクトリ名。
//
// ドット始まりなのは、動画ディレクトリを開いたときに人が書く script.yaml と assets/ が
// 埋もれないようにするため。Remotion の public dir はドット始まりのディレクトリも
// 配信することを確認済みなので、ここに wav を置いてもレンダリングできる（→ README「アセット解決」）。
const OutDirName = ".scenaremo"

// AudioDirName は OutDir の中の wav 置き場。
const AudioDirName = "audio"

// ScriptNames は台本として探すファイル名。先にあるものを優先する。
//
// 拡張子を1つに絞らないのは、yaml と yml のどちらで書くかが人によって違うため。
// json を含むのは、台本を別のツールから生成する道を塞がないため（script.Parse は両方読める）。
var ScriptNames = []string{"script.yaml", "script.yml", "script.json"}

// Layout は1本の動画についての置き場所。
//
// パスはすべて Resolve に渡されたパスの形をそのまま引き継ぐ。相対で渡されれば相対のまま持つ。
type Layout struct {
	// Dir は動画ディレクトリ (videos/ep01)。台本と assets/ がある場所。
	Dir string
	// ScriptPath は台本ファイル。Dir の中にあるとは限らない（台本を直に指定できるため）。
	ScriptPath string
	// OutDir は生成物の置き場所 (<Dir>/.scenaremo)。
	OutDir string
	// AudioDir は wav の置き場所 (<Dir>/.scenaremo/audio)。音声キャッシュもここを使う。
	AudioDir string
	// PropsPath は renderer へ渡す props.json (<Dir>/.scenaremo/props.json)。
	PropsPath string
}

// audioExt は wav のファイル名に付ける拡張子。
//
// cache.Store も同じ規則でキャッシュのファイル名を組み立てる。wav の置き場所とキャッシュは
// 同じディレクトリ (AudioDir) なので、片方だけ変えると props.json に載せたパスと
// 実際に書かれたファイルがずれる（→ layout_test.go の一致テスト）。
const audioExt = ".wav"

// relAudioDir は動画ディレクトリから見た wav 置き場。区切りは "/" で固定する。
//
// props.json に載るパスは OS に依らず / 区切りでなければならない。Windows で作った props.json を
// Linux でレンダリングできるようにするためで、filepath.Join で組み立てるとここが "\" になり、
// その props.json は持ち運べなくなる（→ props の assetPath、examples/minimal/props.json）。
// path.Join ではなく連結にしてあるのは、区切りが定数として目に見えるほうが、
// うっかり filepath.Join へ書き換えられたときに気付きやすいため。
const relAudioDir = OutDirName + "/" + AudioDirName

// RelAudioDir は動画ディレクトリから見た wav 置き場を返す。常に / 区切り。
//
// 値は Layout に依らないが、メソッドにしてあるのは呼ぶ側が Layout だけ持って回れるようにするため。
// OutDirName と AudioDirName を呼び出し側で繋がせると、区切りをどうするかの判断がそこら中に散らばる。
func (l *Layout) RelAudioDir() string {
	return relAudioDir
}

// RelAudioPath は動画ディレクトリから見た wav のパスを返す。props.json にはこの形で載せる。
// key は音声キャッシュのキー（合成パラメータのハッシュ）。
func (l *Layout) RelAudioPath(key string) string {
	return relAudioDir + "/" + key + audioExt
}

// AudioPath は wav を読み書きする実際のパスを返す。OS のパス区切りを使う。
//
// props.json に載せるパスは RelAudioPath のほう。ファイルを開くのはこちら、と分けてあるのは、
// 両者を1つの関数で兼ねると必ずどちらかの用途で間違った区切りが混ざるためである。
func (l *Layout) AudioPath(key string) string {
	return filepath.Join(l.AudioDir, key+audioExt)
}

// Resolve は指定されたパスから動画ディレクトリの置き場所を決める。
//
// path にはディレクトリと台本ファイルのどちらも渡せる。ファイルを渡せるようにしたのは、
// エディタや補完から出てくるのは script.yaml のパスであることが多く、
// `scenaremo build videos/ep01/script.yaml` が動かない理由を利用者に説明できないため。
//
// 絶対パスへは直さない。以降のエラーやログに出るのは利用者が打ったパスそのままのほうが、
// 自分がどこを指したのかと突き合わせやすい。
// ディレクトリも作らない。掘るのは書き出す側（props.WriteFile と cache.Store.Put）の責務で、
// 場所を決めただけの段階で空の .scenaremo/ が残るのは筋が悪い。
func Resolve(path string) (*Layout, error) {
	if path == "" {
		return nil, errors.New("動画ディレクトリが指定されていません。" +
			"scenaremo build videos/ep01 のように、台本のあるディレクトリを指定してください")
	}
	clean := filepath.Clean(path)

	info, err := os.Stat(clean)
	if err != nil {
		if os.IsNotExist(err) {
			// fs.ErrNotExist は包まない。このエラーは端末にそのまま出るものなので、
			// 案内の後ろに "file does not exist" が続くと読み手の視線が英語で終わる。
			// 呼び出し側が種類で分岐したくなったら、そのとき専用のエラー値を足すほうが素直。
			return nil, fmt.Errorf("%s が見つかりません。パスの綴りを確かめるか、"+
				"新しく作るなら scenaremo init %s を実行してください", clean, clean)
		}
		return nil, fmt.Errorf("%s を確認できません: %w", clean, err)
	}

	if info.IsDir() {
		script, err := findScript(clean)
		if err != nil {
			return nil, err
		}
		return newLayout(clean, script), nil
	}
	// 通常ファイル以外（名前付きパイプやデバイス）は台本として読めない。
	// os.Stat はシンボリックリンクを辿った先を返すので、台本へのリンクはここを通る。
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s は台本として読めるファイルではありません。"+
			"台本のあるディレクトリか、台本ファイルそのものを指定してください", clean)
	}
	// 台本を直に渡されたときの動画ディレクトリは、その親。
	// assets/ の相対パスも .scenaremo/ も台本の隣を基準にするのが利用者の理解と一致する。
	return newLayout(filepath.Dir(clean), clean), nil
}

// findScript は dir の中から台本を探す。
func findScript(dir string) (string, error) {
	// 「無いから見に行けなかった」以外の理由（権限など）は覚えておく。
	// それを黙って「見つかりません」に畳むと、置き忘れたのか読めないのかを利用者が区別できず、
	// 台本を置き直すという的外れな対処をさせてしまう。
	var blocked error

	for _, name := range ScriptNames {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil {
			if !os.IsNotExist(err) && blocked == nil {
				blocked = err
			}
			continue
		}
		// 同じ名前のディレクトリは台本にしない。選んでしまうと、この先で
		// 「台本を読めません」とだけ言われ、原因が名前の衝突だと分からなくなる。
		if info.Mode().IsRegular() {
			return candidate, nil
		}
	}

	if blocked != nil {
		return "", fmt.Errorf("%s の中の台本を確認できません。"+
			"ディレクトリの読み取り権限を確かめてください: %w", dir, blocked)
	}
	// 探した名前を全部挙げる。「台本がありません」だけでは、拡張子を間違えたのか
	// 場所を間違えたのかが利用者から見て区別できない。
	return "", fmt.Errorf("%s に台本が見つかりません。探したのは %s です。"+
		"雛形を作るなら scenaremo init %s を実行してください",
		dir, strings.Join(ScriptNames, ", "), dir)
}

// newLayout は動画ディレクトリと台本のパスから置き場所を組み立てる。
func newLayout(dir, script string) *Layout {
	outDir := filepath.Join(dir, OutDirName)
	return &Layout{
		Dir:        dir,
		ScriptPath: script,
		OutDir:     outDir,
		AudioDir:   filepath.Join(outDir, AudioDirName),
		PropsPath:  filepath.Join(outDir, props.FileName),
	}
}
