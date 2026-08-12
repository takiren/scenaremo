package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeout は HTTP クライアント既定のタイムアウト。
// 長いセリフの合成は数秒かかることがあるため、やや余裕を持たせている。
// 呼び出し側は context でより短い期限を課せる。
const DefaultTimeout = 60 * time.Second

// Client は VOICEVOX 互換 API を話すエンジンのクライアント。
// VOICEVOX / AivisSpeech / COEIROINK はこれ 1 つで賄う。
type Client struct {
	kind       EngineKind
	baseURL    string
	httpClient HTTPDoer
}

// Option は Client の設定。
type Option func(*Client)

// WithBaseURL はエンジンの baseURL を差し替える。
// 未知のエンジン種別を使う場合は必須。
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if baseURL != "" {
			c.baseURL = baseURL
		}
	}
}

// WithHTTPClient は HTTP クライアントを差し替える（テスト・リトライ層・プロキシ用）。
func WithHTTPClient(doer HTTPDoer) Option {
	return func(c *Client) {
		if doer != nil {
			c.httpClient = doer
		}
	}
}

// WithTimeout は既定の HTTP クライアントのタイムアウトを変える。
// WithHTTPClient を併用した場合は無視される。
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if hc, ok := c.httpClient.(*http.Client); ok && d > 0 {
			hc.Timeout = d
		}
	}
}

// New は VOICEVOX 互換エンジンのクライアントを作る。
//
//	c, err := tts.New(tts.EngineVoicevox)                                  // http://127.0.0.1:50021
//	c, err := tts.New(tts.EngineAivisSpeech, tts.WithBaseURL(cfg.BaseURL))
func New(kind EngineKind, opts ...Option) (*Client, error) {
	c := &Client{
		kind:       kind,
		baseURL:    DefaultBaseURL(kind),
		httpClient: &http.Client{Timeout: DefaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.baseURL == "" {
		names := make([]string, 0, len(engineDefaults))
		for _, k := range KnownKinds() {
			names = append(names, string(k))
		}
		return nil, fmt.Errorf(
			"未知のエンジン種別 %q です。既定の baseUrl が分からないため baseUrl を明示してください（既定値を持つエンジン: %s）",
			kind, strings.Join(names, ", "),
		)
	}
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("baseUrl %q を URL として解釈できませんでした: %w", c.baseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("baseUrl %q は http:// または https:// で始まる必要があります", c.baseURL)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("baseUrl %q にホストが含まれていません", c.baseURL)
	}
	c.baseURL = strings.TrimRight(c.baseURL, "/")
	return c, nil
}

// Kind はエンジン種別を返す。
func (c *Client) Kind() EngineKind { return c.kind }

// BaseURL は接続先を返す。doctor の診断表示に使う。
func (c *Client) BaseURL() string { return c.baseURL }

// Synthesize は /audio_query → /synthesis の 2 段でテキストを wav にする。
//
// 台本側の Params は audio_query の結果へ適用してから synthesis に渡す。
// 戻り値には合成に使った AudioQuery を含める（→ SynthesizeResult.AudioQuery）。
func (c *Client) Synthesize(ctx context.Context, req SynthesizeRequest) (*SynthesizeResult, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	rawQuery, err := c.audioQuery(ctx, req.Text, req.StyleID)
	if err != nil {
		return nil, err
	}

	rawQuery, err = applyParams(rawQuery, req.Params)
	if err != nil {
		return nil, err
	}

	var query AudioQuery
	if err := json.Unmarshal(rawQuery, &query); err != nil {
		return nil, fmt.Errorf("%s の audio_query 応答を解釈できませんでした: %w", DisplayName(c.kind), err)
	}

	wav, err := c.synthesis(ctx, rawQuery, req.StyleID)
	if err != nil {
		return nil, err
	}

	return &SynthesizeResult{WAV: wav, AudioQuery: query, RawAudioQuery: rawQuery}, nil
}

// audioQuery は POST /audio_query?text=...&speaker=... を叩き、応答 JSON をそのまま返す。
func (c *Client) audioQuery(ctx context.Context, text string, styleID int) ([]byte, error) {
	return c.do(ctx, apiCall{
		method: http.MethodPost,
		path:   "/audio_query",
		query: url.Values{
			"text":    {text},
			"speaker": {strconv.Itoa(styleID)},
		},
		accept:  "application/json",
		styleID: styleID,
	})
}

// synthesis は POST /synthesis?speaker=... へ AudioQuery を送り、wav バイト列を得る。
func (c *Client) synthesis(ctx context.Context, audioQuery []byte, styleID int) ([]byte, error) {
	return c.do(ctx, apiCall{
		method:      http.MethodPost,
		path:        "/synthesis",
		query:       url.Values{"speaker": {strconv.Itoa(styleID)}},
		body:        audioQuery,
		contentType: "application/json",
		accept:      "audio/wav",
		styleID:     styleID,
	})
}

// Speakers は GET /speakers で話者とスタイルの一覧を取得する。
func (c *Client) Speakers(ctx context.Context) ([]Speaker, error) {
	data, err := c.do(ctx, apiCall{method: http.MethodGet, path: "/speakers", accept: "application/json", styleID: noStyleID})
	if err != nil {
		return nil, err
	}
	var speakers []Speaker
	if err := json.Unmarshal(data, &speakers); err != nil {
		return nil, fmt.Errorf("%s の /speakers 応答を解釈できませんでした: %w", DisplayName(c.kind), err)
	}
	return speakers, nil
}

// Ping は GET /version でエンジンとの疎通を確認し、バージョン文字列を返す。
// `scenaremo doctor` はこれで起動の有無を判定する。
func (c *Client) Ping(ctx context.Context) (string, error) {
	data, err := c.do(ctx, apiCall{method: http.MethodGet, path: "/version", accept: "application/json", styleID: noStyleID})
	if err != nil {
		return "", err
	}
	// /version は JSON 文字列（"0.14.4"）を返すが、素のテキストを返す実装もある。
	var version string
	if err := json.Unmarshal(data, &version); err == nil {
		return version, nil
	}
	return strings.TrimSpace(string(data)), nil
}

// apiCall は 1 回の HTTP 呼び出しの記述。
type apiCall struct {
	method      string
	path        string
	query       url.Values
	body        []byte
	contentType string
	accept      string
	// styleID は非 200 応答時のヒントに使う。話者に無関係な呼び出しでは noStyleID。
	styleID int
}

// noStyleID は apiCall が話者スタイルに紐づかないことを表す。
const noStyleID = -1

// do は HTTP 呼び出しを行い、200 のときだけ本文を返す。
// 接続不能・非 200・context の打ち切りを、それぞれ区別できるエラーに変換する。
func (c *Client) do(ctx context.Context, call apiCall) ([]byte, error) {
	endpoint := c.baseURL + call.path
	if len(call.query) > 0 {
		endpoint += "?" + call.query.Encode()
	}

	var body io.Reader
	if call.body != nil {
		body = bytes.NewReader(call.body)
	}
	req, err := http.NewRequestWithContext(ctx, call.method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("%s へのリクエストを組み立てられませんでした: %w", endpoint, err)
	}
	if call.contentType != "" {
		req.Header.Set("Content-Type", call.contentType)
	}
	if call.accept != "" {
		req.Header.Set("Accept", call.accept)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, c.contextError(call.path, ctxErr)
		}
		return nil, &EngineUnavailableError{Kind: c.kind, BaseURL: c.baseURL, Err: err}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, c.contextError(call.path, ctxErr)
		}
		return nil, fmt.Errorf("%s の %s の応答を読み取れませんでした: %w", DisplayName(c.kind), call.path, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{
			Kind:       c.kind,
			Endpoint:   call.path,
			StatusCode: resp.StatusCode,
			Body:       excerptBody(data),
			Hint:       statusHint(resp.StatusCode, call.styleID),
		}
	}
	return data, nil
}

// contextError は context の打ち切りを、原因が分かるエラーに包む。
// errors.Is(err, context.Canceled) / context.DeadlineExceeded は引き続き成立する。
func (c *Client) contextError(path string, ctxErr error) error {
	reason := "キャンセルされました"
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		reason = "時間切れになりました。エンジンの応答が遅いか、タイムアウトが短すぎます"
	}
	return fmt.Errorf("%s の %s への通信が%s: %w", DisplayName(c.kind), path, reason, ctxErr)
}
