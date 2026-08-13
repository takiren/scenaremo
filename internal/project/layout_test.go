package project_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/takiren/scenaremo/internal/cache"
	"github.com/takiren/scenaremo/internal/project"
	"github.com/takiren/scenaremo/internal/props"
)

// writeScript は dir に台本を1つ置き、そのパスを返す。
// Resolve は中身を読まない（読むのは script.Load の仕事）ので、体裁だけの1行で足りる。
func writeScript(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("meta:\n"), 0o644); err != nil {
		t.Fatalf("台本を置けません: %v", err)
	}
	return path
}

// wantContains はエラーメッセージに必要な語が揃っていることを確かめる。
// 文面そのものを固定すると言い回しを直すたびにテストが落ちるので、要素だけを見る。
func wantContains(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%q が入っていない: %s", w, got)
		}
	}
}

func TestResolve_ディレクトリを渡すと生成物の置き場所まで決まる(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "script.yaml")

	l, err := project.Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if l.Dir != dir {
		t.Errorf("Dir が違う: got %q, want %q", l.Dir, dir)
	}
	if l.ScriptPath != script {
		t.Errorf("ScriptPath が違う: got %q, want %q", l.ScriptPath, script)
	}
	// ディレクトリ名は定数ではなく直に書く。".scenaremo/audio" は README と .gitignore と
	// examples/minimal/props.json が既に前提にしている名前なので、定数を書き換えても
	// テストが一緒に付いてこないほうがよい。
	if want := filepath.Join(dir, ".scenaremo"); l.OutDir != want {
		t.Errorf("OutDir が違う: got %q, want %q", l.OutDir, want)
	}
	if want := filepath.Join(dir, ".scenaremo", "audio"); l.AudioDir != want {
		t.Errorf("AudioDir が違う: got %q, want %q", l.AudioDir, want)
	}
	// props.FileName を使う。ファイル名の正は props 側にあり、ここで "props.json" と
	// 書き写すと片方だけ変えられたときに気付けない。
	if want := filepath.Join(dir, ".scenaremo", props.FileName); l.PropsPath != want {
		t.Errorf("PropsPath が違う: got %q, want %q", l.PropsPath, want)
	}
}

// TestResolve_ディレクトリは作らない は Resolve が副作用を持たないことを確かめる。
//
// 掘るのは書き出す側 (props.WriteFile と cache.Store.Put) の責務。ここで作ってしまうと、
// 台本を読んだだけで終わった build や、失敗して途中で止まった build が
// 空の .scenaremo/ を残すことになる。
func TestResolve_ディレクトリは作らない(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "script.yaml")

	l, err := project.Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	for _, p := range []string{l.OutDir, l.AudioDir} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s ができている (err=%v)", p, err)
		}
	}
}

// TestResolve_台本は決めた順に探す は ScriptNames の優先順が効いていることを確かめる。
// 置く順を候補の逆にしてあるのは、作った順（更新時刻順）で拾っていないことを示すため。
func TestResolve_台本は決めた順に探す(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  string
	}{
		{"yaml だけ", []string{"script.yaml"}, "script.yaml"},
		{"yml だけ", []string{"script.yml"}, "script.yml"},
		{"json だけ", []string{"script.json"}, "script.json"},
		{"yaml と yml があれば yaml", []string{"script.yml", "script.yaml"}, "script.yaml"},
		{"yml と json があれば yml", []string{"script.json", "script.yml"}, "script.yml"},
		{"3つ揃っていれば yaml", []string{"script.json", "script.yml", "script.yaml"}, "script.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tt.files {
				writeScript(t, dir, name)
			}

			l, err := project.Resolve(dir)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if want := filepath.Join(dir, tt.want); l.ScriptPath != want {
				t.Errorf("ScriptPath が違う: got %q, want %q", l.ScriptPath, want)
			}
		})
	}
}

// TestResolve_台本と同じ名前のディレクトリは飛ばす は、候補の名前が付いていても
// 読めないものは台本として選ばないことを確かめる。
func TestResolve_台本と同じ名前のディレクトリは飛ばす(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "script.yaml"), 0o755); err != nil {
		t.Fatalf("ディレクトリを作れません: %v", err)
	}
	want := writeScript(t, dir, "script.yml")

	l, err := project.Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if l.ScriptPath != want {
		t.Errorf("ScriptPath が違う: got %q, want %q", l.ScriptPath, want)
	}
}

// TestResolve_台本ファイルを直に指定できる は scenaremo build videos/ep01/script.yaml を通す。
func TestResolve_台本ファイルを直に指定できる(t *testing.T) {
	dir := t.TempDir()
	// 候補の名前でなくても、指定されたファイルをそのまま台本として扱う。
	// 「どれを読むか」を利用者が明示している以上、名前で断る理由がない。
	script := writeScript(t, dir, "ep01.yaml")
	// 同じディレクトリに候補名の台本があっても、指定されたほうが勝つ。
	writeScript(t, dir, "script.yaml")

	l, err := project.Resolve(script)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if l.ScriptPath != script {
		t.Errorf("ScriptPath が違う: got %q, want %q", l.ScriptPath, script)
	}
	if l.Dir != dir {
		t.Errorf("Dir が親ディレクトリでない: got %q, want %q", l.Dir, dir)
	}
	if want := filepath.Join(dir, ".scenaremo", "audio"); l.AudioDir != want {
		t.Errorf("AudioDir が違う: got %q, want %q", l.AudioDir, want)
	}
}

func TestResolve_末尾のスラッシュがあっても同じ結果になる(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "script.yaml")

	want, err := project.Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	got, err := project.Resolve(dir + string(filepath.Separator))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if *got != *want {
		t.Errorf("末尾のスラッシュで結果が変わる:\n got %+v\nwant %+v", *got, *want)
	}
}

// TestResolve_相対パスは相対のまま返す は、絶対パスへ直していないことを確かめる。
// 利用者が打ったパスのままエラーやログに出るほうが、自分がどこを指したのかと突き合わせやすい。
func TestResolve_相対パスは相対のまま返す(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "videos", "ep01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("ディレクトリを作れません: %v", err)
	}
	writeScript(t, dir, "script.yaml")
	t.Chdir(root)

	rel := filepath.Join("videos", "ep01")
	l, err := project.Resolve(rel)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if l.Dir != rel {
		t.Errorf("Dir が違う: got %q, want %q", l.Dir, rel)
	}
	if want := filepath.Join(rel, ".scenaremo", props.FileName); l.PropsPath != want {
		t.Errorf("PropsPath が違う: got %q, want %q", l.PropsPath, want)
	}
}

// TestResolve_カレントディレクトリの台本を直に指定できる は、動画ディレクトリが "." になる場合を確かめる。
// filepath.Dir("script.yaml") が "." を返すため、生成物のパスに "./" が混ざらないかがここで分かる。
func TestResolve_カレントディレクトリの台本を直に指定できる(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "script.yaml")
	t.Chdir(dir)

	l, err := project.Resolve("script.yaml")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if l.Dir != "." {
		t.Errorf("Dir が違う: got %q, want %q", l.Dir, ".")
	}
	if l.OutDir != ".scenaremo" {
		t.Errorf("OutDir が違う: got %q, want %q", l.OutDir, ".scenaremo")
	}
}

func TestResolve_台本が見つからなければ探した名前を挙げる(t *testing.T) {
	dir := t.TempDir()
	// 台本らしく見えるが候補ではない名前を置く。拡張子を間違えた人がここに来る。
	writeScript(t, dir, "script.yamll")

	_, err := project.Resolve(dir)
	if err == nil {
		t.Fatal("台本が無いのにエラーにならない")
	}
	wantContains(t, err.Error(), dir, "script.yaml", "script.yml", "script.json", "scenaremo init")
}

// TestResolve_通常ファイルでなければ台本として扱わない は、デバイスファイルなどを指されたときに
// 台本として抱え込まないことを確かめる。ここを通すと、失敗するのは台本のパースまで遅れる。
func TestResolve_通常ファイルでなければ台本として扱わない(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("/dev/null に相当するものの見せ方が違うため")
	}

	_, err := project.Resolve("/dev/null")
	if err == nil {
		t.Fatal("通常ファイルでないのにエラーにならない")
	}
	wantContains(t, err.Error(), "/dev/null", "ディレクトリ")
}

// TestResolve_台本を確認できないときは見つからないと言わない は、権限で中を見られない場合に
// 「台本がありません」と誤解させないことを確かめる。置き忘れと読めないのとでは対処が違う。
func TestResolve_台本を確認できないときは見つからないと言わない(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ファイルの権限の扱いが違うため")
	}
	if os.Geteuid() == 0 {
		t.Skip("root は権限に関係なく読めてしまうため")
	}
	dir := t.TempDir()
	writeScript(t, dir, "script.yaml")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("権限を変えられません: %v", err)
	}
	// t.TempDir の後片付けが権限で失敗しないよう戻しておく。
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := project.Resolve(dir)
	if err == nil {
		t.Fatal("中を見られないのにエラーにならない")
	}
	wantContains(t, err.Error(), dir, "権限")
	if strings.Contains(err.Error(), "見つかりません") {
		t.Errorf("置き忘れたかのように読めるメッセージになっている: %s", err)
	}
}

func TestResolve_存在しないパスは何が無いかを伝える(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "videos", "ep99")

	_, err := project.Resolve(missing)
	if err == nil {
		t.Fatal("存在しないパスなのにエラーにならない")
	}
	wantContains(t, err.Error(), missing, "見つかりません")
}

func TestResolve_空文字列は受け付けない(t *testing.T) {
	_, err := project.Resolve("")
	if err == nil {
		t.Fatal("空文字列なのにエラーにならない")
	}
	// カレントディレクトリを指したものとして扱わない。build の出力先が思わぬ場所になるより、
	// 指定が無いと言われたほうが直しようがある。
	wantContains(t, err.Error(), "指定")
}

// resolved は動画ディレクトリを1つ作って Resolve した結果を返す。
func resolved(t *testing.T) *project.Layout {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "videos", "ep01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("ディレクトリを作れません: %v", err)
	}
	writeScript(t, dir, "script.yaml")

	l, err := project.Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return l
}

// TestLayout_props_json に載せるパスは常にスラッシュ区切り は、Windows で作った props.json を
// Linux でレンダリングできるという契約を守れていることを確かめる（→ props の assetPath）。
//
// filepath.Join で組み立てると Windows で "\" になり、props.json を持ち運べなくなる。
// この期待値は OS を問わず同じでなければならないので、テスト側でも filepath.Join を使わない。
func TestLayout_props_jsonに載せるパスは常にスラッシュ区切り(t *testing.T) {
	l := resolved(t)

	if got, want := l.RelAudioDir(), ".scenaremo/audio"; got != want {
		t.Errorf("RelAudioDir が違う: got %q, want %q", got, want)
	}
	const key = "99f8961035658d8c74f292bf798d1432bd6dde477ccea933cda390e6f194b691"
	if got, want := l.RelAudioPath(key), ".scenaremo/audio/"+key+".wav"; got != want {
		t.Errorf("RelAudioPath が違う: got %q, want %q", got, want)
	}
	for _, got := range []string{l.RelAudioDir(), l.RelAudioPath(key)} {
		// 動画ディレクトリからの相対であること。絶対パスは別のマシンでは成り立たない。
		if strings.HasPrefix(got, "/") || strings.Contains(got, ":") {
			t.Errorf("相対パスでない: %q", got)
		}
		// 動画ディレクトリの名前が混ざっていないこと。基準は動画ディレクトリ自身なので、
		// videos/ep01 を含めてしまうと renderer 側の解決とずれる。
		if strings.Contains(got, "ep01") {
			t.Errorf("動画ディレクトリからの相対になっていない: %q", got)
		}
	}
}

// TestLayout_AudioPath は wav を実際に書く場所を指す は、props.json に載る相対パスと
// 実際の書き出し先が同じ場所を指していることを確かめる。ここがずれると
// props.json は出来上がるのにレンダリング時に音声だけが見つからない、という壊れ方をする。
func TestLayout_AudioPathはwavを実際に書く場所を指す(t *testing.T) {
	l := resolved(t)
	const key = "abc123"

	if want := filepath.Join(l.AudioDir, key+".wav"); l.AudioPath(key) != want {
		t.Errorf("AudioPath が違う: got %q, want %q", l.AudioPath(key), want)
	}
	// 動画ディレクトリに相対パスを繋ぎ直すと実パスに戻る。
	// これが props.json の読み手（renderer）がやることそのもの。
	if want := filepath.Join(l.Dir, filepath.FromSlash(l.RelAudioPath(key))); l.AudioPath(key) != want {
		t.Errorf("相対パスと実パスがずれている: got %q, want %q", l.AudioPath(key), want)
	}
}

// TestLayout_AudioPath は音声キャッシュの置き場所と一致する は、AudioDir を cache.Store に渡したときに
// 実際に書かれるファイルを AudioPath が言い当てられることを確かめる。
//
// wav の置き場所とキャッシュは同じディレクトリで、ファイル名はどちらも合成パラメータのハッシュ
// （→ examples/minimal/props.json）。".wav" を付ける規則が cache 側と 2 箇所にあるため、
// 片方だけ変わったことにここで気付けるようにしておく。
func TestLayout_AudioPathは音声キャッシュの置き場所と一致する(t *testing.T) {
	l := resolved(t)
	const key = "0123456789abcdef"

	if err := cache.NewStore(l.AudioDir).Put(key, []byte("RIFF....WAVE")); err != nil {
		t.Fatalf("キャッシュに書けません: %v", err)
	}
	if _, err := os.Stat(l.AudioPath(key)); err != nil {
		t.Errorf("AudioPath が書かれた wav を指していない: %v", err)
	}
}
