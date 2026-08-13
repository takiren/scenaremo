package build_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/takiren/scenaremo/internal/build"
	"github.com/takiren/scenaremo/internal/script"
	"github.com/takiren/scenaremo/internal/tts"
)

// TestCredits_合成せずにクレジットを集計する は、このコマンドが build と分かれている理由を固定する。
//
// 音声を合成し始めた時点で `scenaremo credits` は「build を待つのと同じ」になり、
// 公開直前に手軽に確かめるという使い方ができなくなる。
func TestCredits_合成せずにクレジットを集計する(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, log := factory(engine)

	res, err := build.Credits(context.Background(), build.CreditsOptions{Dir: dir, NewEngine: newEngine})
	if err != nil {
		t.Fatalf("クレジットの集計に失敗した: %v", err)
	}

	if len(engine.synthesized) != 0 {
		t.Errorf("クレジットを出すだけなのに合成している: %d 件", len(engine.synthesized))
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".scenaremo", "props.json")); statErr == nil {
		t.Error("クレジットを出すだけなのに props.json を書いている")
	}

	// 台本での登場順。ずんだもんが先に喋る。
	if len(res.Credits.Entries) != 2 {
		t.Fatalf("クレジットの件数が違う: %d (%+v)", len(res.Credits.Entries), res.Credits.Entries)
	}
	if got, want := res.Credits.Entries[0].Text, "VOICEVOX:ずんだもん"; got != want {
		t.Errorf("Entries[0].Text が違う: %q (want %q)", got, want)
	}
	if got, want := res.Credits.Entries[1].Text, "VOICEVOX:四国めたん"; got != want {
		t.Errorf("Entries[1].Text が違う: %q (want %q)", got, want)
	}

	// エンジンは種別ごとに1つ、話者一覧の問い合わせも1回で足りる。
	if len(*log) != 1 {
		t.Errorf("エンジンを開いた回数が違う: %v", *log)
	}
	if engine.listed != 1 {
		t.Errorf("話者一覧の問い合わせ回数が違う: %d", engine.listed)
	}
}

// TestCredits_buildと同じクレジットになる は、2 つの出力が食い違わないことを確かめる。
//
// 食い違えば、利用者は props.json の credits と `scenaremo credits` のどちらを信じるかを
// 自分で判断させられる。それは表記漏れを機械的に防ぐというこの機能の目的そのものを壊すので、
// 集計を別に書こうとした瞬間にここで落ちるようにしておく。
func TestCredits_buildと同じクレジットになる(t *testing.T) {
	dir := videoDir(t, scriptYAML)

	fromCredits, err := build.Credits(context.Background(), build.CreditsOptions{
		Dir:       dir,
		NewEngine: mustFactory(&fakeEngine{kind: tts.EngineVoicevox}),
	})
	if err != nil {
		t.Fatalf("クレジットの集計に失敗した: %v", err)
	}

	fromBuild, err := build.Run(context.Background(), build.Options{
		Dir:       dir,
		NewEngine: mustFactory(&fakeEngine{kind: tts.EngineVoicevox}),
	})
	if err != nil {
		t.Fatalf("build に失敗した: %v", err)
	}

	if !reflect.DeepEqual(fromCredits.Credits, fromBuild.Props.Credits) {
		t.Errorf("credits と build のクレジットが違う:\ncredits: %+v\nbuild:   %+v",
			fromCredits.Credits, fromBuild.Props.Credits)
	}
}

func TestCredits_接続先がエンジンへ渡る(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, log := factory(engine)

	if _, err := build.Credits(context.Background(), build.CreditsOptions{
		Dir:         dir,
		VoicevoxURL: "http://192.168.0.2:50021",
		NewEngine:   newEngine,
	}); err != nil {
		t.Fatalf("クレジットの集計に失敗した: %v", err)
	}

	if len(*log) != 1 || (*log)[0].baseURL != "http://192.168.0.2:50021" {
		t.Errorf("指定した接続先が渡っていない: %v", *log)
	}
}

func TestCredits_エンジンが起動していなければ案内が残る(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	engine := &fakeEngine{
		kind:    tts.EngineVoicevox,
		listErr: &tts.EngineUnavailableError{Kind: tts.EngineVoicevox, BaseURL: "http://127.0.0.1:50021"},
	}
	newEngine, _ := factory(engine)

	_, err := build.Credits(context.Background(), build.CreditsOptions{Dir: dir, NewEngine: newEngine})
	if err == nil {
		t.Fatal("エンジンに繋がらないのに成功した")
	}
	// 「起動してから再実行してください」はここでしか出せない案内なので、包み直して潰さない。
	var unavailable *tts.EngineUnavailableError
	if !errors.As(err, &unavailable) {
		t.Errorf("エンジン未起動の案内が失われている: %v", err)
	}
}

func TestCredits_台本が壊れていれば検証の報告が返る(t *testing.T) {
	dir := videoDir(t, `
meta:
  fps: "さんじゅう"
speakers:
  zundamon:
    styleId: 3
scenes:
  - image: assets/01.png
    lines:
      - speaker: zundamon
        text: セリフ
`)
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, log := factory(engine)

	_, err := build.Credits(context.Background(), build.CreditsOptions{Dir: dir, NewEngine: newEngine})
	if err == nil {
		t.Fatal("台本が壊れているのに成功した")
	}
	// CLI が「検証の報告はそのまま出す」と判断できるよう、型が残っていること。
	var scriptErr *script.Error
	if !errors.As(err, &scriptErr) {
		t.Fatalf("台本の検証エラーとして返っていない: %v", err)
	}
	if len(*log) != 0 {
		t.Error("台本が読めていないのにエンジンへ繋ぎに行っている")
	}
}

func TestCredits_台本が見つからない(t *testing.T) {
	dir := t.TempDir()
	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, log := factory(engine)

	_, err := build.Credits(context.Background(), build.CreditsOptions{Dir: dir, NewEngine: newEngine})
	if err == nil {
		t.Fatal("台本が無いのに成功した")
	}
	if len(*log) != 0 {
		t.Error("台本が無いのにエンジンへ繋ぎに行っている")
	}
}

func TestCredits_打ち切られたら止まる(t *testing.T) {
	dir := videoDir(t, scriptYAML)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	engine := &fakeEngine{kind: tts.EngineVoicevox}
	newEngine, _ := factory(engine)

	_, err := build.Credits(ctx, build.CreditsOptions{Dir: dir, NewEngine: newEngine})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("打ち切りとして返っていない: %v", err)
	}
}

func TestCredits_動画ディレクトリが指定されていない(t *testing.T) {
	_, err := build.Credits(context.Background(), build.CreditsOptions{})
	if err == nil {
		t.Fatal("ディレクトリが空なのに成功した")
	}
}

// mustFactory は開かれたエンジンの記録が要らない場面のための factory。
func mustFactory(e *fakeEngine) build.EngineFactory {
	newEngine, _ := factory(e)
	return newEngine
}
