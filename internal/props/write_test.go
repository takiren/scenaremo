package props_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takiren/scenaremo/internal/props"
)

// sample は書き出しを試すための props。
//
// 台本から組み立てる経路（Build）はここでは通さない。書き出しの責務は
// 「渡された Props を決定論的に、壊れない形でファイルにする」ことだけなので、
// 組み立て側の都合でこのテストが動かなくなるのは筋が悪い。
func sample() *props.Props {
	return &props.Props{
		Version:     props.Version,
		GeneratedBy: "scenaremo v0.0.0-test",
		Note:        props.Note,
		Meta: props.Meta{
			Title:            "テスト動画",
			Aspect:           "16:9",
			Width:            1920,
			Height:           1080,
			FPS:              30,
			DurationInFrames: 129,
		},
		Scenes: []props.Scene{
			{
				Image:            "assets/01.png",
				Component:        "default",
				DurationInFrames: 99,
				Transition:       props.Transition{Type: "fade", DurationInFrames: 0},
				Lines: []props.Line{
					{
						Speaker:          "zundamon",
						Text:             "1つめ",
						Audio:            ".scenaremo/audio/aaa.wav",
						StartFrame:       0,
						DurationInFrames: 60,
					},
				},
			},
			{
				Image:            "assets/02.png",
				Component:        "zoom",
				Props:            map[string]any{"zoom": 1.5, "focus": []any{0.3, 0.6}, "align": "center", "blur": 2},
				DurationInFrames: 42,
				Transition:       props.Transition{Type: "fade", DurationInFrames: 12},
				Lines: []props.Line{
					{
						Speaker:          "metan",
						Text:             "2つめ",
						Audio:            ".scenaremo/audio/bbb.wav",
						StartFrame:       12,
						DurationInFrames: 30,
					},
				},
			},
		},
	}
}

// TestMarshalDeterministic は同じ入力から必ず同じバイト列が出ることを確かめる。
//
// 台本を1文字も変えていないのに props.json が毎回変わると、build のたびに
// 何が変わったのかを追えなくなる。scenes[].props は map なので、順序が揺れるならここで出る。
func TestMarshalDeterministic(t *testing.T) {
	first, err := props.Marshal(sample())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for i := range 20 {
		again, err := props.Marshal(sample())
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("%d 回目の Marshal が 1 回目と違う", i+2)
		}
	}
}

// TestMarshalFormat は人が読める形で出ることを確かめる。
func TestMarshalFormat(t *testing.T) {
	p := sample()
	// エスケープされると読めなくなる文字を混ぜる。
	p.Scenes[0].Lines[0].Text = "AとB<C>D&E"

	data, err := props.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(data)

	if !strings.HasSuffix(text, "\n") {
		t.Error("末尾に改行が無い")
	}
	if !strings.Contains(text, "\n  \"version\": 1") {
		t.Error("2 スペースのインデントになっていない")
	}
	// encoding/json は既定で < > & を < のような表記へ置き換える。
	// props.json は不具合の調査で人が読むほうが多いので、台本に書いたままの文字で出す。
	if !strings.Contains(text, "AとB<C>D&E") {
		t.Errorf("記号がエスケープされている: %s", text)
	}
	if !strings.Contains(text, "手で編集しないでください") {
		t.Error("手で編集しない旨の注意書きが入っていない")
	}
}

// TestWriteFile は書き出しがディレクトリを作り、一時ファイルを残さないことを確かめる。
func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".scenaremo", props.FileName)

	p := sample()
	if err := props.WriteFile(path, p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("書き出した props.json を読めない: %v", err)
	}
	want, err := props.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Error("書き出した内容が Marshal の結果と違う")
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("一時ファイルが残っている: %s", e.Name())
		}
	}
}

// TestWriteFileOverwrites は 2 回目の build が前回の props.json を置き換えることを確かめる。
// .scenaremo/ は毎回作り直す層なので、古い内容が混ざってはいけない。
func TestWriteFileOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, props.FileName)

	if err := props.WriteFile(path, sample()); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	p := sample()
	p.Meta.Title = "書き換えたタイトル"
	if err := props.WriteFile(path, p); err != nil {
		t.Fatalf("WriteFile (2 回目): %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(got), "書き換えたタイトル") {
		t.Error("2 回目の内容で上書きされていない")
	}
	if strings.Contains(string(got), "テスト動画") {
		t.Error("1 回目の内容が残っている")
	}
}
