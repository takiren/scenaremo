package props_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/takiren/scenaremo/internal/props"
)

// TestBuildMoras_フレームへ直す は、合成結果のモーラが props.json のフレームになることを確かめる。
//
// 位置は**そのセリフの音声の先頭からの相対**であり、シーンや動画の先頭からではない
// （このリポジトリは相対位置に振り切る、が契約）。
// 丸めは timeline / credits と同じ切り上げで、境界を切り上げてから差を取ることで
// 隣り合うモーラが隙間なく繋がる。
//
// 台本は fps 30、1 つめのセリフは 2 秒 = 60 フレーム。
func TestBuildMoras_フレームへ直す(t *testing.T) {
	in := baseInput()
	in.Audio[0][0].Moras = []props.MoraTiming{
		{Text: "コ", Vowel: "o", Offset: 100 * time.Millisecond, Duration: 200 * time.Millisecond},
		{Text: "ン", Vowel: "N", Offset: 300 * time.Millisecond, Duration: 10 * time.Millisecond},
		{Text: "ニ", Vowel: "i", Offset: 310 * time.Millisecond, Duration: 10 * time.Millisecond},
		{Text: "チ", Vowel: "i", Offset: 320 * time.Millisecond, Duration: 1000 * time.Millisecond},
	}

	got := build(t, in).Scenes[0].Lines[0].Moras

	want := []props.Mora{
		// 0.1s → 3 フレーム、終わりは 0.3s → 9 フレーム
		{Text: "コ", Vowel: "o", StartFrame: 3, DurationInFrames: 6},
		// 0.3s → 9、終わり 0.31s → 切り上げて 10
		{Text: "ン", Vowel: "N", StartFrame: 9, DurationInFrames: 1},
		// 1 フレームに満たないモーラは 0 になる。ずらして 1 以上にすると、ずれが後続へ積み上がる。
		{Text: "ニ", Vowel: "i", StartFrame: 10, DurationInFrames: 0},
		// 0.32s → 10、終わり 1.32s → 切り上げて 40
		{Text: "チ", Vowel: "i", StartFrame: 10, DurationInFrames: 30},
	}

	if len(got) != len(want) {
		t.Fatalf("モーラの数 = %d, 期待値 %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("moras[%d] = %+v, 期待値 %+v", i, got[i], want[i])
		}
	}

	// 隙間なく繋がっていること。カラオケ表示も口パクも位置から「今どのモーラか」を引くので、
	// 隙間や重なりがあると、そこだけ何も選ばれない・二重に選ばれるフレームができる。
	for i := 1; i < len(got); i++ {
		if end := got[i-1].StartFrame + got[i-1].DurationInFrames; got[i].StartFrame != end {
			t.Errorf("moras[%d].StartFrame = %d, 期待値 %d（前のモーラの終わりと繋がっていない）",
				i, got[i].StartFrame, end)
		}
	}
}

// TestBuildMoras_セリフの尺に収める は、モーラがセリフの外へはみ出さないことを確かめる。
//
// wav の実測長はエンジンの出力であって台本から決まる値ではないので、
// audio_query から組み立てた予測とわずかにずれうる。renderer は
// セリフの Sequence の中でこの値を使うため、はみ出した分は使いようがない。
//
// 2 つめのセリフは 1 秒 = 30 フレーム。
func TestBuildMoras_セリフの尺に収める(t *testing.T) {
	in := baseInput()
	in.Audio[0][1].Moras = []props.MoraTiming{
		{Text: "ア", Vowel: "a", Offset: 0, Duration: 900 * time.Millisecond},
		// 27 フレームから始まり 42 フレームで終わる。尺の 30 までに詰める。
		{Text: "イ", Vowel: "i", Offset: 900 * time.Millisecond, Duration: 500 * time.Millisecond},
		// 尺の外から始まるので落とす。
		{Text: "ウ", Vowel: "u", Offset: 1400 * time.Millisecond, Duration: 100 * time.Millisecond},
	}

	line := build(t, in).Scenes[0].Lines[1]
	if line.DurationInFrames != 30 {
		t.Fatalf("前提が崩れている: セリフの尺 = %d, 期待値 30", line.DurationInFrames)
	}

	want := []props.Mora{
		{Text: "ア", Vowel: "a", StartFrame: 0, DurationInFrames: 27},
		{Text: "イ", Vowel: "i", StartFrame: 27, DurationInFrames: 3},
	}
	if len(line.Moras) != len(want) {
		t.Fatalf("モーラの数 = %d, 期待値 %d: %+v", len(line.Moras), len(want), line.Moras)
	}
	for i := range want {
		if line.Moras[i] != want[i] {
			t.Errorf("moras[%d] = %+v, 期待値 %+v", i, line.Moras[i], want[i])
		}
	}
}

// TestBuildMoras_無ければ載せない は、モーラを返さないエンジンでも
// これまでと同じ props.json になることを確かめる。
//
// 項目を足しただけの変更なので契約の版は上げない。
// 古い renderer は知らないキーを無視すればよく、キー自体が出なければなおよい。
func TestBuildMoras_無ければ載せない(t *testing.T) {
	got := build(t, baseInput())

	for i, scene := range got.Scenes {
		for j, line := range scene.Lines {
			if len(line.Moras) != 0 {
				t.Errorf("scenes[%d].lines[%d].Moras = %+v, 期待値 空", i, j, line.Moras)
			}
		}
	}

	data, err := props.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if bytes.Contains(data, []byte(`"moras"`)) {
		t.Errorf("モーラが無いのに moras キーが出ている:\n%s", data)
	}
}

// TestBuildMoras_スキーマに合う は、モーラを載せた props.json が契約を満たすことを確かめる。
//
// docs/props.schema.json が唯一の正で、renderer 側の zod もそこに従う。
// Go の型だけ足してスキーマを直し忘れると、乖離に気づくのがレンダリング時になる。
func TestBuildMoras_スキーマに合う(t *testing.T) {
	in := baseInput()
	in.Audio[0][0].Moras = []props.MoraTiming{
		{Text: "コ", Vowel: "o", Offset: 100 * time.Millisecond, Duration: 200 * time.Millisecond},
		{Text: "、", Vowel: "pau", Offset: 300 * time.Millisecond, Duration: 320 * time.Millisecond},
		// 1 フレームに満たないモーラ。durationInFrames が 0 でもスキーマを満たすこと。
		{Text: "ン", Vowel: "N", Offset: 620 * time.Millisecond, Duration: 10 * time.Millisecond},
	}

	data, err := props.Marshal(build(t, in))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	validateAgainstSchema(t, data)

	if !bytes.Contains(data, []byte(`"moras"`)) {
		t.Errorf("moras が出力されていない:\n%s", data)
	}
	if !bytes.Contains(data, []byte(`"vowel"`)) {
		t.Errorf("モーラの vowel が出力されていない:\n%s", data)
	}
}
