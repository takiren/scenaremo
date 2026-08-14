package tts

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"
)

// sampleQuery は sampleAudioQueryJSON を AudioQuery にして返す。
//
// 期待値を手書きの構造体ではなく実際の応答から起こしているのは、
// モーラの並び（アクセント句をまたぐこと、pause_mora が句の後ろに入ること）が
// エンジンの返す形そのままであることを、テスト側でも取り違えないようにするため。
func sampleQuery(t *testing.T) AudioQuery {
	t.Helper()
	var q AudioQuery
	if err := json.Unmarshal([]byte(sampleAudioQueryJSON), &q); err != nil {
		t.Fatalf("サンプルの audio_query を読めない: %v", err)
	}
	return q
}

// seconds は期待値を秒で書くためのヘルパ。
func seconds(v float64) time.Duration {
	return time.Duration(math.Round(v * float64(time.Second)))
}

// assertNear は時間が期待値とほぼ一致することを確かめる。
// float64 の秒を経由する計算なので、ns 単位の完全一致までは求めない。
func assertNear(t *testing.T, label string, got, want time.Duration) {
	t.Helper()
	const tolerance = time.Microsecond
	if d := got - want; d < -tolerance || d > tolerance {
		t.Errorf("%s = %v, 期待値 %v", label, got, want)
	}
}

// assertContiguous は隣り合うモーラが隙間なく繋がっていることを確かめる。
//
// カラオケ表示も口パクも「今どのモーラか」を位置から引くので、
// 隙間や重なりがあると、そこだけ何も選ばれない・二重に選ばれるフレームができる。
func assertContiguous(t *testing.T, got []MoraTiming) {
	t.Helper()
	for i := 1; i < len(got); i++ {
		if want := got[i-1].Offset + got[i-1].Duration; got[i].Offset != want {
			t.Errorf("moras[%d].Offset = %v, 期待値 %v（前のモーラの終わりと繋がっていない）",
				i, got[i].Offset, want)
		}
	}
}

// TestMoraTimings_サンプルの並びと位置 は、エンジンの応答 1 つぶんを端から端まで押さえる。
//
// 期待値は sampleAudioQueryJSON の値を頭から足したもの。
// 先頭は prePhonemeLength (0.1) ぶん後ろから始まり、アクセント句をまたいで並び、
// 句の切れ目の pause_mora も 1 件として現れる。
func TestMoraTimings_サンプルの並びと位置(t *testing.T) {
	got := sampleQuery(t).MoraTimings()

	wants := []struct {
		text     string
		vowel    string
		offset   float64
		duration float64
	}{
		{"コ", "o", 0.1, 0.1837},    // 0.0764 + 0.1073
		{"ン", "N", 0.2837, 0.0709}, // 子音なし
		{"ニ", "i", 0.3546, 0.1349},
		{"チ", "i", 0.4895, 0.1398},
		{"ワ", "a", 0.6293, 0.2377},
		{"、", "pau", 0.867, 0.32}, // 句の切れ目の無音
		{"ナ", "a", 1.187, 0.16},
		{"ノ", "o", 1.347, 0.16},
	}

	if len(got) != len(wants) {
		t.Fatalf("モーラの数 = %d, 期待値 %d: %+v", len(got), len(wants), got)
	}
	for i, w := range wants {
		if got[i].Text != w.text {
			t.Errorf("moras[%d].Text = %q, 期待値 %q", i, got[i].Text, w.text)
		}
		if got[i].Vowel != w.vowel {
			t.Errorf("moras[%d].Vowel = %q, 期待値 %q", i, got[i].Vowel, w.vowel)
		}
		assertNear(t, fmt.Sprintf("moras[%d].Offset", i), got[i].Offset, seconds(w.offset))
		assertNear(t, fmt.Sprintf("moras[%d].Duration", i), got[i].Duration, seconds(w.duration))
	}
	assertContiguous(t, got)
}

// TestMoraTimings_speedScaleで縮む は、話速の指定が実時間に効くことを確かめる。
//
// エンジンは前後の無音も含めて長さを speedScale で割るので、
// ここを掛け忘れると、速く喋らせた台本だけモーラの位置が後ろへずれる。
func TestMoraTimings_speedScaleで縮む(t *testing.T) {
	base := sampleQuery(t).MoraTimings()

	q := sampleQuery(t)
	q.SpeedScale = 2.0
	got := q.MoraTimings()

	if len(got) != len(base) {
		t.Fatalf("モーラの数 = %d, 期待値 %d", len(got), len(base))
	}
	for i := range got {
		// 先頭の無音も縮むので、位置そのものが半分になる。
		assertNear(t, fmt.Sprintf("moras[%d].Offset", i), got[i].Offset, base[i].Offset/2)
		assertNear(t, fmt.Sprintf("moras[%d].Duration", i), got[i].Duration, base[i].Duration/2)
	}
	assertContiguous(t, got)
}

// TestMoraTimings_speedScaleが0でも壊れない は、エンジンが妙な値を返しても
// props.json に意味不明な数字を載せないことを確かめる。0 除算の結果を丸めると、
// フレーム番号が桁の壊れた整数になって、どこが原因なのか分からない不具合になる。
func TestMoraTimings_speedScaleが0でも壊れない(t *testing.T) {
	q := sampleQuery(t)
	q.SpeedScale = 0
	got := q.MoraTimings()

	want := sampleQuery(t).MoraTimings() // speedScale 1.0 と同じ扱い
	if len(got) != len(want) {
		t.Fatalf("モーラの数 = %d, 期待値 %d", len(got), len(want))
	}
	for i := range got {
		assertNear(t, fmt.Sprintf("moras[%d].Offset", i), got[i].Offset, want[i].Offset)
		assertNear(t, fmt.Sprintf("moras[%d].Duration", i), got[i].Duration, want[i].Duration)
	}
}

// TestMoraTimings_無音の長さ指定 は pauseLength / pauseLengthScale が
// pau のモーラにだけ効くことを確かめる。
//
// エンジンは「置き換えてから倍率を掛け、最後に速度で割る」の順で適用する。
// 順序を取り違えると、句読点の多い台本だけが後半へ行くほどずれる。
func TestMoraTimings_無音の長さ指定(t *testing.T) {
	// サンプルの pause_mora は 6 件目 (index 5)、素の長さは 0.32 秒。
	const pauseIndex = 5

	tests := []struct {
		name      string
		mutate    func(q *AudioQuery)
		wantPause float64 // pau モーラの長さ（秒）
		wantNext  float64 // 次のモーラ「ナ」の位置（秒）
		wantFirst float64 // 先頭のモーラ「コ」の長さ（秒）。無音の指定に巻き込まれないこと
	}{
		{
			name:      "pauseLength は置き換える",
			mutate:    func(q *AudioQuery) { q.PauseLength = new(0.5) },
			wantPause: 0.5,
			wantNext:  1.367, // 0.867 + 0.5
			wantFirst: 0.1837,
		},
		{
			name:      "pauseLengthScale は掛ける",
			mutate:    func(q *AudioQuery) { q.PauseLengthScale = new(2.0) },
			wantPause: 0.64,
			wantNext:  1.507, // 0.867 + 0.64
			wantFirst: 0.1837,
		},
		{
			name: "併用すると置き換えてから掛ける",
			mutate: func(q *AudioQuery) {
				q.PauseLength = new(0.5)
				q.PauseLengthScale = new(2.0)
			},
			wantPause: 1.0,
			wantNext:  1.867, // 0.867 + 1.0
			wantFirst: 0.1837,
		},
		{
			name: "speedScale はそのあとに効く",
			mutate: func(q *AudioQuery) {
				q.PauseLength = new(0.5)
				q.SpeedScale = 2.0
			},
			wantPause: 0.25,
			wantNext:  0.6835, // 0.867/2 + 0.25
			wantFirst: 0.09185,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := sampleQuery(t)
			tt.mutate(&q)
			got := q.MoraTimings()

			if len(got) < pauseIndex+2 {
				t.Fatalf("モーラの数 = %d, %d 件以上あるはず: %+v", len(got), pauseIndex+2, got)
			}
			if got[pauseIndex].Vowel != "pau" {
				t.Fatalf("moras[%d] が pau ではない: %+v", pauseIndex, got[pauseIndex])
			}
			assertNear(t, "pau モーラの長さ", got[pauseIndex].Duration, seconds(tt.wantPause))
			assertNear(t, "次のモーラの位置", got[pauseIndex+1].Offset, seconds(tt.wantNext))
			// 無音の指定は pau にだけ効く。ほかのモーラまで置き換わってはいけない。
			assertNear(t, "先頭のモーラの長さ", got[0].Duration, seconds(tt.wantFirst))
			assertContiguous(t, got)
		})
	}
}

// TestMoraTimings_アクセント句が無ければ空 は、モーラを返さないエンジンでも
// 呼び出し側が場合分けせずに済むことを確かめる。
func TestMoraTimings_アクセント句が無ければ空(t *testing.T) {
	q := AudioQuery{SpeedScale: 1.0, PrePhonemeLength: 0.1}
	if got := q.MoraTimings(); len(got) != 0 {
		t.Errorf("モーラが無いのに %d 件返った: %+v", len(got), got)
	}
}
