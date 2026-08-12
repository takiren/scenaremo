package cache_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/takiren/scenaremo/internal/cache"
	"github.com/takiren/scenaremo/internal/tts"
)

func TestKey_Determinism(t *testing.T) {
	req := tts.SynthesizeRequest{
		Text:    "こんにちは",
		StyleID: 1,
		Params: tts.Params{
			SpeedScale:      tts.Float64(1.0),
			PitchScale:      tts.Float64(0.0),
			IntonationScale: tts.Float64(1.0),
			VolumeScale:     tts.Float64(1.0),
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
			SpeedScale: tts.Float64(1.0),
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
					SpeedScale: tts.Float64(1.1),
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
					SpeedScale: tts.Float64(0.0), // base は 1.0 だが、別のテストとして nil と 0 を比べる
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
			SpeedScale: tts.Float64(0.0),
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
