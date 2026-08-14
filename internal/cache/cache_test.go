package cache_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/takiren/scenaremo/internal/cache"
	"github.com/takiren/scenaremo/internal/tts"
)

func TestKey_Determinism(t *testing.T) {
	req := tts.SynthesizeRequest{
		Text:    "こんにちは",
		StyleID: 1,
		Params: tts.Params{
			SpeedScale:      new(1.0),
			PitchScale:      new(0.0),
			IntonationScale: new(1.0),
			VolumeScale:     new(1.0),
		},
	}
	key1 := cache.Key(tts.EngineVoicevox, req)
	key2 := cache.Key(tts.EngineVoicevox, req)
	if key1 != key2 {
		t.Errorf("同じパラメータからは同じキーが生成される必要があります: %q != %q", key1, key2)
	}
}

func TestKey_Sensitivity(t *testing.T) {
	base := tts.SynthesizeRequest{
		Text:    "こんにちは",
		StyleID: 1,
		Params: tts.Params{
			SpeedScale: new(1.0),
		},
	}
	baseKey := cache.Key(tts.EngineVoicevox, base)

	tests := []struct {
		name string
		req  tts.SynthesizeRequest
		eng  tts.EngineKind
	}{
		{
			name: "エンジンが違う",
			req:  base,
			eng:  tts.EngineAivisSpeech,
		},
		{
			name: "テキストが違う",
			req: tts.SynthesizeRequest{
				Text:    "さようなら",
				StyleID: 1,
				Params:  base.Params,
			},
			eng: tts.EngineVoicevox,
		},
		{
			name: "スタイルIDが違う",
			req: tts.SynthesizeRequest{
				Text:    "こんにちは",
				StyleID: 2,
				Params:  base.Params,
			},
			eng: tts.EngineVoicevox,
		},
		{
			name: "パラメータが違う",
			req: tts.SynthesizeRequest{
				Text:    "こんにちは",
				StyleID: 1,
				Params: tts.Params{
					SpeedScale: new(1.1),
				},
			},
			eng: tts.EngineVoicevox,
		},
		{
			name: "パラメータがnil vs zero",
			req: tts.SynthesizeRequest{
				Text:    "こんにちは",
				StyleID: 1,
				Params: tts.Params{
					SpeedScale: new(0.0), // base は 1.0 だが、別のテストとして nil と 0 を比べる
				},
			},
			eng: tts.EngineVoicevox,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := cache.Key(tt.eng, tt.req)
			if key == baseKey {
				t.Errorf("パラメータが違う場合は別のキーが生成される必要があります: %q == %q", key, baseKey)
			}
		})
	}
}

func TestKey_NilVsZero(t *testing.T) {
	reqNil := tts.SynthesizeRequest{
		Text:    "こんにちは",
		StyleID: 1,
		Params: tts.Params{
			SpeedScale: nil,
		},
	}
	reqZero := tts.SynthesizeRequest{
		Text:    "こんにちは",
		StyleID: 1,
		Params: tts.Params{
			SpeedScale: new(0.0),
		},
	}

	keyNil := cache.Key(tts.EngineVoicevox, reqNil)
	keyZero := cache.Key(tts.EngineVoicevox, reqZero)
	if keyNil == keyZero {
		t.Errorf("nilと0は区別される必要があります: key=%q", keyNil)
	}
}

func TestStore_GetPut(t *testing.T) {
	dir := t.TempDir()
	store := cache.NewStore(dir)
	key := "testkey123"
	data := []byte("dummy wav data")

	err := store.Put(key, data)
	if err != nil {
		t.Fatalf("Putに失敗しました: %v", err)
	}

	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Getに失敗しました: %v", err)
	}

	if string(got) != string(data) {
		t.Errorf("Getで得られたデータが異なります: got %q, want %q", got, data)
	}
}

func TestStore_GetNotExist(t *testing.T) {
	dir := t.TempDir()
	store := cache.NewStore(dir)

	_, err := store.Get("notexist")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("存在しないキーのGetはos.ErrNotExistをラップしたエラーを返す必要があります: %v", err)
	}
}

func TestStore_PutCreatesDir(t *testing.T) {
	baseDir := t.TempDir()
	dir := filepath.Join(baseDir, "newdir")
	store := cache.NewStore(dir)

	err := store.Put("key1", []byte("data"))
	if err != nil {
		t.Fatalf("Putに失敗しました: %v", err)
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("Putがディレクトリを作成しませんでした")
	}
}

// 同じキーへの Put が同時に走っても、壊れた wav が残らないこと。
//
// --parallel で合成すると worker が同時に Put を呼ぶ。台本の中で同じ話者が同じ文を
// 二度喋れば、キーは同じになって同時に同じキーへ書きに行く（そもそもキャッシュは
// 「同じ入力なら同じファイル」を狙って設計されている）。
// 一時ファイルの名前がキーだけで決まっていると、2 つの Put が同じ一時ファイルへ
// 同時に書き込み、混ざった中身がそのまま本番の名前へ rename されてしまう。
// 壊れた wav はタイムラインを壊し、しかもキャッシュに残るので build のたびに再現する。
func TestStore_Put_同じキーへの同時書き込み(t *testing.T) {
	dir := t.TempDir()
	store := cache.NewStore(dir)
	key := "concurrentkey"

	// 中身の違いが混ざったときに検出できるよう、書き手ごとに別のバイトで埋める。
	// 1 回の write で終わらない大きさにしないと、途中で入れ替わる隙が生まれない。
	const writers = 8
	const size = 1 << 20
	payloads := make([][]byte, writers)
	for i := range payloads {
		payloads[i] = bytes.Repeat([]byte{byte('A' + i)}, size)
	}

	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = store.Put(key, payloads[i])
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Put[%d] に失敗しました: %v", i, err)
		}
	}

	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get に失敗しました: %v", err)
	}
	// 誰が最後に書いたかは決まらないが、**誰か 1 人ぶんがそのまま**入っていなければならない。
	if !slices.ContainsFunc(payloads, func(p []byte) bool { return bytes.Equal(got, p) }) {
		t.Errorf("どの書き手の内容とも一致しません（混ざった中身が残っています）: 長さ %d, 先頭 %q, 末尾 %q",
			len(got), first(got, 8), last(got, 8))
	}

	// 一時ファイルが残っていないこと。残ると次の build がゴミを掴む余地になる。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ディレクトリを読めませんでした: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("一時ファイルが残っています: %s", e.Name())
		}
	}
}

func first(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}

func last(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[len(b)-n:]
}
