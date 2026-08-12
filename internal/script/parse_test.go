package script_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takiren/scenaremo/internal/script"
)

// parseErr は台本の検証が失敗することを確かめ、その *script.Error を返す。
func parseErr(t *testing.T, src string, opts ...script.Option) *script.Error {
	t.Helper()
	_, err := script.Parse([]byte(src), opts...)
	if err == nil {
		t.Fatalf("エラーになるはずが通ってしまった")
	}
	var e *script.Error
	if !errors.As(err, &e) {
		t.Fatalf("*script.Error ではないエラーが返った: %v", err)
	}
	return e
}

// messages は Issue のメッセージだけを取り出す。
func messages(e *script.Error) []string {
	out := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		out = append(out, issue.Message)
	}
	return out
}

// containsMessage はメッセージのどれかが部分文字列を含むかを返す。
func containsMessage(e *script.Error, substr string) bool {
	for _, m := range messages(e) {
		if strings.Contains(m, substr) {
			return true
		}
	}
	return false
}

// writeScript は一時ディレクトリに台本と画像を書き出し、台本のパスを返す。
func writeScript(t *testing.T, name, src string, images ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("台本を書けない: %v", err)
	}
	for _, image := range images {
		full := filepath.Join(dir, filepath.FromSlash(image))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatalf("画像の置き場を作れない: %v", err)
		}
		if err := os.WriteFile(full, []byte("dummy"), 0o600); err != nil {
			t.Fatalf("画像を書けない: %v", err)
		}
	}
	return path
}

const validScript = `meta:
  title: テスト
speakers:
  zundamon:
    styleId: 3
  metan:
    styleId: 2
defaults:
  speaker: zundamon
scenes:
  - image: assets/01.png
    lines:
      - text: こんにちは
      - speaker: metan
        text: またね
`

// TestParseMinimalExample は README の例（examples/minimal）が読めることを確かめる。
func TestParseMinimalExample(t *testing.T) {
	src, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("台本の読み込みに失敗した: %v", err)
	}
	// 画像は examples に置いていないので、基準ディレクトリを与えず存在確認は行わない。
	s, err := script.Parse(src, script.WithFilename(examplePath))
	if err != nil {
		t.Fatalf("最小の台本が読めない:\n%v", err)
	}
	if s.Meta.Title != "Remotionで解説動画を作る" {
		t.Errorf("title = %q", s.Meta.Title)
	}
	if len(s.Scenes) != 2 {
		t.Fatalf("scenes の数 = %d", len(s.Scenes))
	}
	// 既定値と話者が解決済みであること。
	if got := s.Scenes[0].Lines[0].Speaker; got != "zundamon" {
		t.Errorf("省略された speaker が解決されていない: %q", got)
	}
	if got := s.Scenes[1].Transition; got != script.DefaultTransition {
		t.Errorf("省略された transition が解決されていない: %q", got)
	}
	if got := s.Scenes[1].Component; got != script.DefaultComponent {
		t.Errorf("省略された component が解決されていない: %q", got)
	}
}

// TestApplyDefaults は省略されたフィールドに既定値が入ることを確かめる。
func TestApplyDefaults(t *testing.T) {
	const src = `meta:
  title: t
speakers:
  zundamon:
    styleId: 3
scenes:
  - image: a.png
    lines:
      - speaker: zundamon
        text: こんにちは
`
	s, err := script.Parse([]byte(src))
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	if s.Meta.Aspect != script.DefaultAspect {
		t.Errorf("aspect = %q", s.Meta.Aspect)
	}
	if s.Meta.FPS != script.DefaultFPS {
		t.Errorf("fps = %d", s.Meta.FPS)
	}
	if s.Speakers["zundamon"].Engine != script.DefaultEngine {
		t.Errorf("engine = %q", s.Speakers["zundamon"].Engine)
	}
	if s.Defaults == nil {
		t.Fatalf("defaults が nil のまま")
	}
	if s.Defaults.Transition != script.DefaultTransition {
		t.Errorf("defaults.transition = %q", s.Defaults.Transition)
	}
	if s.Defaults.GapMs == nil || *s.Defaults.GapMs != script.DefaultGapMs {
		t.Errorf("defaults.gapMs = %v", s.Defaults.GapMs)
	}
	if s.Scenes[0].Transition != script.DefaultTransition {
		t.Errorf("scenes[0].transition = %q", s.Scenes[0].Transition)
	}
	if s.Scenes[0].Component != script.DefaultComponent {
		t.Errorf("scenes[0].component = %q", s.Scenes[0].Component)
	}
}

// TestApplyDefaultsKeepsExplicitValues は明示された値を既定値で上書きしないことを確かめる。
func TestApplyDefaultsKeepsExplicitValues(t *testing.T) {
	const src = `meta:
  title: t
  aspect: "9:16"
  fps: 60
speakers:
  zundamon:
    styleId: 3
defaults:
  speaker: zundamon
  transition: none
  gapMs: 0
scenes:
  - image: a.png
    transition: fade
    component: zoom
    lines:
      - text: こんにちは
`
	s, err := script.Parse([]byte(src))
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	if s.Meta.Aspect != script.Aspect9x16 || s.Meta.FPS != 60 {
		t.Errorf("meta = %+v", s.Meta)
	}
	if s.Defaults.Transition != script.TransitionNone {
		t.Errorf("defaults.transition = %q", s.Defaults.Transition)
	}
	// gapMs: 0 は「余白なし」の明示であって未指定ではない。
	if s.Defaults.GapMs == nil || *s.Defaults.GapMs != 0 {
		t.Errorf("defaults.gapMs = %v, want 0", s.Defaults.GapMs)
	}
	if s.Scenes[0].Transition != script.TransitionFade {
		t.Errorf("scenes[0].transition = %q", s.Scenes[0].Transition)
	}
	if s.Scenes[0].Component != "zoom" {
		t.Errorf("scenes[0].component = %q", s.Scenes[0].Component)
	}
}

// TestLoadChecksImages は台本からの相対パスで画像を解決し、存在を確かめることを確認する。
func TestLoadChecksImages(t *testing.T) {
	path := writeScript(t, "script.yaml", validScript, "assets/01.png")
	if _, err := script.Load(path); err != nil {
		t.Fatalf("読めない:\n%v", err)
	}
}

// TestLoadReportsMissingImage は画像が無ければ、解決後のパスつきで報告することを確かめる。
func TestLoadReportsMissingImage(t *testing.T) {
	path := writeScript(t, "script.yaml", validScript)
	_, err := script.Load(path)
	if err == nil {
		t.Fatalf("画像が無いのに通ってしまった")
	}
	var e *script.Error
	if !errors.As(err, &e) {
		t.Fatalf("*script.Error ではない: %v", err)
	}
	if len(e.Issues) != 1 {
		t.Fatalf("問題の数 = %d: %v", len(e.Issues), messages(e))
	}
	issue := e.Issues[0]
	if !strings.Contains(issue.Message, "画像が見つかりません") {
		t.Errorf("メッセージ = %q", issue.Message)
	}
	// 解決後のパスをそのまま示す（どこを探したのかが分かるように）。
	want := filepath.Join(filepath.Dir(path), "assets", "01.png")
	if !strings.Contains(issue.Message, want) {
		t.Errorf("メッセージに解決後のパス %q が無い: %q", want, issue.Message)
	}
	if issue.Line != 11 {
		t.Errorf("行番号 = %d, want 11", issue.Line)
	}
	if issue.Path != "scenes[0].image" {
		t.Errorf("位置 = %q", issue.Path)
	}
}

// TestLoadReportsMissingFile は台本そのものが無い場合を確かめる。
func TestLoadReportsMissingFile(t *testing.T) {
	_, err := script.Load(filepath.Join(t.TempDir(), "no-such.yaml"))
	if err == nil {
		t.Fatalf("エラーになるはず")
	}
	if !strings.Contains(err.Error(), "台本を読み込めません") {
		t.Errorf("メッセージ = %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("os.ErrNotExist を包んでいない: %v", err)
	}
}

// TestUndefinedSpeaker は未定義の話者を、使える話者を並べて弾くことを確かめる。
func TestUndefinedSpeaker(t *testing.T) {
	const src = `meta:
  title: t
speakers:
  zundamon:
    styleId: 3
  metan:
    styleId: 2
scenes:
  - image: a.png
    lines:
      - speaker: zundamom
        text: こんにちは
`
	e := parseErr(t, src)
	if len(e.Issues) != 1 {
		t.Fatalf("問題の数 = %d: %v", len(e.Issues), messages(e))
	}
	issue := e.Issues[0]
	if !strings.Contains(issue.Message, `話者 "zundamom" は speakers に定義されていません`) {
		t.Errorf("メッセージ = %q", issue.Message)
	}
	if issue.Hint != "使える話者: metan / zundamon" {
		t.Errorf("ヒント = %q", issue.Hint)
	}
	if issue.Line != 11 {
		t.Errorf("行番号 = %d, want 11", issue.Line)
	}
	if issue.Path != "scenes[0].lines[0].speaker" {
		t.Errorf("位置 = %q", issue.Path)
	}
}

// TestUndefinedDefaultSpeaker は defaults.speaker の誤りを、
// それを使うセリフの数だけ繰り返さず1件で報告することを確かめる。
func TestUndefinedDefaultSpeaker(t *testing.T) {
	const src = `meta:
  title: t
speakers:
  zundamon:
    styleId: 3
defaults:
  speaker: unknown
scenes:
  - image: a.png
    lines:
      - text: ひとつめ
      - text: ふたつめ
      - text: みっつめ
`
	e := parseErr(t, src)
	if len(e.Issues) != 1 {
		t.Fatalf("問題の数 = %d（1件にまとめるべき）: %v", len(e.Issues), messages(e))
	}
	if !strings.Contains(e.Issues[0].Message, "既定の話者") {
		t.Errorf("メッセージ = %q", e.Issues[0].Message)
	}
	if e.Issues[0].Path != "defaults.speaker" {
		t.Errorf("位置 = %q", e.Issues[0].Path)
	}
}

// TestSpeakerNotDetermined は speaker も defaults.speaker も無い場合を確かめる。
func TestSpeakerNotDetermined(t *testing.T) {
	const src = `meta:
  title: t
speakers:
  zundamon:
    styleId: 3
scenes:
  - image: a.png
    lines:
      - text: こんにちは
`
	e := parseErr(t, src)
	if len(e.Issues) != 1 {
		t.Fatalf("問題の数 = %d: %v", len(e.Issues), messages(e))
	}
	if !strings.Contains(e.Issues[0].Message, "話者が決まりません") {
		t.Errorf("メッセージ = %q", e.Issues[0].Message)
	}
	if !strings.Contains(e.Issues[0].Hint, "defaults.speaker") {
		t.Errorf("ヒント = %q", e.Issues[0].Hint)
	}
}

// TestIssuesAreBatched は複数の問題をまとめて、ソースの上から順に報告することを確かめる。
func TestIssuesAreBatched(t *testing.T) {
	const src = `meta:
  titel: t
  aspect: "4:3"
speakers:
  zundamon:
    styleId: "3"
scenes:
  - lines:
      - txet: こんにちは
`
	e := parseErr(t, src)
	if len(e.Issues) < 5 {
		t.Fatalf("まとめて報告されていない (%d 件): %v", len(e.Issues), messages(e))
	}
	// 行番号が単調増加していること（上から順に直せるように）。
	for i := 1; i < len(e.Issues); i++ {
		if e.Issues[i-1].Line > e.Issues[i].Line {
			t.Errorf("位置順に並んでいない: %d 行目のあとに %d 行目", e.Issues[i-1].Line, e.Issues[i].Line)
		}
	}
	for _, want := range []string{
		`meta に必須の項目 "title" がありません`,
		"meta.titel は知らない項目です",
		"meta.aspect の値が正しくありません",
		"speakers.zundamon.styleId には整数を書いてください",
		`scenes[0] に必須の項目 "image" がありません`,
		"scenes[0].lines[0].txet は知らない項目です",
	} {
		if !containsMessage(e, want) {
			t.Errorf("報告されていない: %q\n実際: %v", want, messages(e))
		}
	}
}

// TestUnknownFieldHintListsSchemaFields は未知の項目に対して、
// スキーマから使える項目名を並べて示すことを確かめる。
func TestUnknownFieldHintListsSchemaFields(t *testing.T) {
	const src = `meta:
  title: t
speakers:
  zundamon:
    styleId: 3
    speedscale: 1.1
defaults:
  speaker: zundamon
scenes:
  - image: a.png
    lines:
      - text: こんにちは
`
	e := parseErr(t, src)
	if len(e.Issues) != 1 {
		t.Fatalf("問題の数 = %d: %v", len(e.Issues), messages(e))
	}
	hint := e.Issues[0].Hint
	for _, want := range []string{"engine", "intonationScale", "pitchScale", "speedScale", "styleId", "volumeScale"} {
		if !strings.Contains(hint, want) {
			t.Errorf("ヒントに %q が無い: %q", want, hint)
		}
	}
}

// TestJSONScript は JSON の台本も同じスキーマで読めることを確かめる。
func TestJSONScript(t *testing.T) {
	const src = `{
  "$schema": "../../docs/schema.json",
  "meta": {"title": "テスト", "fps": 24},
  "speakers": {"zundamon": {"styleId": 3}},
  "defaults": {"speaker": "zundamon"},
  "scenes": [
    {"image": "a.png", "lines": [{"text": "こんにちは"}]}
  ]
}
`
	s, err := script.Parse([]byte(src), script.WithFilename("script.json"))
	if err != nil {
		t.Fatalf("JSON の台本が読めない:\n%v", err)
	}
	if s.Meta.FPS != 24 {
		t.Errorf("fps = %d", s.Meta.FPS)
	}
	if s.Scenes[0].Lines[0].Speaker != "zundamon" {
		t.Errorf("speaker が解決されていない: %q", s.Scenes[0].Lines[0].Speaker)
	}
}

// TestJSONScriptReportsPosition は JSON でも位置を示せることを確かめる。
func TestJSONScriptReportsPosition(t *testing.T) {
	const src = `{
  "meta": {"title": "テスト", "aspect": "4:3"},
  "speakers": {"zundamon": {"styleId": 3}},
  "defaults": {"speaker": "zundamon"},
  "scenes": [
    {"image": "a.png", "lines": [{"text": "こんにちは"}]}
  ]
}
`
	e := parseErr(t, src, script.WithFilename("script.json"))
	if len(e.Issues) != 1 {
		t.Fatalf("問題の数 = %d: %v", len(e.Issues), messages(e))
	}
	if e.Issues[0].Line != 2 {
		t.Errorf("行番号 = %d, want 2", e.Issues[0].Line)
	}
	if e.Issues[0].Snippet == "" {
		t.Errorf("ソース片が空")
	}
}

// TestJSONSyntaxError は JSON の構文エラーに行番号を付けることを確かめる。
func TestJSONSyntaxError(t *testing.T) {
	const src = `{
  "meta": {"title": "t"},
  "speakers": ,
}
`
	e := parseErr(t, src, script.WithFilename("script.json"))
	if len(e.Issues) != 1 {
		t.Fatalf("問題の数 = %d", len(e.Issues))
	}
	if !strings.Contains(e.Issues[0].Message, "JSON として読めません") {
		t.Errorf("メッセージ = %q", e.Issues[0].Message)
	}
	if e.Issues[0].Line != 3 {
		t.Errorf("行番号 = %d, want 3", e.Issues[0].Line)
	}
	if !strings.Contains(e.Issues[0].Snippet, `"speakers"`) {
		t.Errorf("ソース片 = %q", e.Issues[0].Snippet)
	}
}

// TestYAMLSyntaxError は YAML の構文エラーに位置とソース片を付けることを確かめる。
func TestYAMLSyntaxError(t *testing.T) {
	const src = `meta:
  title: "閉じていない
scenes:
`
	e := parseErr(t, src, script.WithFilename("script.yaml"))
	if len(e.Issues) != 1 {
		t.Fatalf("問題の数 = %d", len(e.Issues))
	}
	if !strings.Contains(e.Issues[0].Message, "YAML として読めません") {
		t.Errorf("メッセージ = %q", e.Issues[0].Message)
	}
	if e.Issues[0].Line != 2 {
		t.Errorf("行番号 = %d, want 2", e.Issues[0].Line)
	}
	if e.Issues[0].Snippet == "" {
		t.Errorf("ソース片が空")
	}
}

// TestDuplicateKey は同じキーを二度書いた場合に弾くことを確かめる。
func TestDuplicateKey(t *testing.T) {
	const src = `meta:
  title: ひとつめ
  title: ふたつめ
`
	e := parseErr(t, src, script.WithFilename("script.yaml"))
	if !containsMessage(e, "YAML として読めません") {
		t.Errorf("メッセージ = %v", messages(e))
	}
}

// TestColorOption は色付けを切り替えられることを確かめる。
func TestColorOption(t *testing.T) {
	const src = `meta:
  title: t
  aspect: "4:3"
speakers:
  zundamon:
    styleId: 3
defaults:
  speaker: zundamon
scenes:
  - image: a.png
    lines:
      - text: こんにちは
`
	plain := parseErr(t, src)
	if strings.Contains(plain.Error(), "\x1b[") {
		t.Errorf("色を付けていないのに制御文字が入っている")
	}
	colored := parseErr(t, src, script.WithColor(true))
	if !strings.Contains(colored.Error(), "\x1b[") {
		t.Errorf("色を付けたのに制御文字が無い")
	}
}

// TestErrorMessageLayout は報告文の体裁を確かめる。
func TestErrorMessageLayout(t *testing.T) {
	const src = `meta:
  title: t
  aspect: "4:3"
speakers:
  zundamon:
    styleId: 3
defaults:
  speaker: zundamon
scenes:
  - image: a.png
    lines:
      - text: こんにちは
`
	e := parseErr(t, src, script.WithFilename("videos/ep01/script.yaml"))
	got := e.Error()
	const want = `videos/ep01/script.yaml の検証に失敗しました (1 件)

videos/ep01/script.yaml:3:11: meta.aspect の値が正しくありません: "4:3"
   1 | meta:
   2 |   title: t
>  3 |   aspect: "4:3"
                 ^
   4 | speakers:
   5 |   zundamon:
   6 |     styleId: 3
  ヒント: 使えるのは "16:9" / "9:16" です
`
	if got != want {
		t.Errorf("報告文が違う\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestImagePath は画像パスの解決規則を確かめる。
func TestImagePath(t *testing.T) {
	got := script.ImagePath(filepath.Join("videos", "ep01", "script.yaml"), "assets/01.png")
	want := filepath.Join("videos", "ep01", "assets", "01.png")
	if got != want {
		t.Errorf("ImagePath = %q, want %q", got, want)
	}

	abs := filepath.Join(string(filepath.Separator), "tmp", "a.png")
	if got := script.ImagePath("videos/ep01/script.yaml", abs); got != abs {
		t.Errorf("絶対パスはそのまま使うべき: %q", got)
	}
}
