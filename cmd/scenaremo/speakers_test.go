package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpeakers_一覧が標準出力に出る(t *testing.T) {
	// 1. テスト用のモックサーバーを立てる
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/speakers" {
			t.Errorf("予期しないパス: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// モックのレスポンス（tts.SpeakerのJSON配列）
		_, _ = w.Write([]byte(`[
			{
				"name": "四国めたん",
				"speaker_uuid": "...",
				"version": "1.0",
				"styles": [
					{"name": "ノーマル", "id": 2},
					{"name": "あまあま", "id": 0}
				]
			},
			{
				"name": "ずんだもん",
				"speaker_uuid": "...",
				"version": "1.0",
				"styles": [
					{"name": "ノーマル", "id": 3},
					{"name": "あまあま", "id": 1}
				]
			}
		]`))
	}))
	t.Cleanup(ts.Close)

	var stdout, stderr bytes.Buffer
	// 2. コマンドを実行
	code := run(context.Background(), []string{"speakers", "--voicevox-url=" + ts.URL}, &stdout, &stderr)

	// 3. 終了コードと出力の検証
	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("成功なのに標準エラー出力へ書いている: %s", stderr.String())
	}

	out := stdout.String()
	wants := []string{
		"四国めたん",
		"  - ノーマル (2)",
		"  - あまあま (0)",
		"ずんだもん",
		"  - ノーマル (3)",
		"  - あまあま (1)",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出力に含まれない:\n%s", want, out)
		}
	}
}
