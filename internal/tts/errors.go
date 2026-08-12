package tts

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

// EngineUnavailableError はエンジンの HTTP サーバへ到達できなかったことを表す。
//
// 原因の大半は「エンジンを起動していない」なので、何をすればよいかまで書く。
// Phase 3（開発者以外が使う）では、このメッセージが唯一の道しるべになる。
type EngineUnavailableError struct {
	Kind    EngineKind
	BaseURL string
	Err     error
}

func (e *EngineUnavailableError) Error() string {
	return fmt.Sprintf(
		"%s (%s) に接続できませんでした。エンジンを起動してから再実行してください（`scenaremo doctor` で診断できます）: %v",
		DisplayName(e.Kind), e.BaseURL, e.Err,
	)
}

func (e *EngineUnavailableError) Unwrap() error { return e.Err }

// APIError はエンジンが 200 以外を返したことを表す。
type APIError struct {
	Kind       EngineKind
	Endpoint   string // "/audio_query" など
	StatusCode int
	// Body は応答本文の抜粋。エンジンは FastAPI の detail を返すことが多く、原因の特定に役立つ。
	Body string
	// Hint は呼び出し箇所ごとの対処ヒント。空なら出力しない。
	Hint string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s の %s が HTTP %d", DisplayName(e.Kind), e.Endpoint, e.StatusCode)
	if text := http.StatusText(e.StatusCode); text != "" {
		fmt.Fprintf(&b, " (%s)", text)
	}
	b.WriteString(" を返しました")
	if e.Hint != "" {
		b.WriteString("。")
		b.WriteString(e.Hint)
	}
	if e.Body != "" {
		b.WriteString(": ")
		b.WriteString(e.Body)
	}
	return b.String()
}

// statusHint はステータスコードから定型の対処ヒントを組み立てる。
// styleID が noStyleID のときは話者に触れない一般的な文面にする。
func statusHint(status, styleID int) string {
	switch {
	case status == http.StatusNotFound, status == http.StatusUnprocessableEntity, status == http.StatusBadRequest:
		if styleID == noStyleID {
			return "エンジンがリクエストを受け付けませんでした。エンジンのバージョンが古い可能性があります"
		}
		return fmt.Sprintf("styleId=%d が存在しないか、テキストが不正な可能性があります（`scenaremo speakers` で利用できるスタイルを確認してください）", styleID)
	case status >= 500:
		return "エンジン側でエラーが発生しています。エンジンのログを確認してください"
	default:
		return ""
	}
}

const bodyExcerptLimit = 300

// excerptBody は応答本文をエラーメッセージへ載せられる長さに丸める。
func excerptBody(data []byte) string {
	s := strings.TrimSpace(string(data))
	if s == "" {
		return ""
	}
	if !utf8.ValidString(s) {
		return fmt.Sprintf("(バイナリ %d バイト)", len(data))
	}
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > bodyExcerptLimit {
		s = string([]rune(s)[:bodyExcerptLimit]) + "…"
	}
	return s
}
