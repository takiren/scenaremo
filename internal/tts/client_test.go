package tts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// VOICEVOX が実際に返す形に寄せた /audio_query の応答。
// 未知フィールド futureField は、将来エンジンが増やすフィールドの代役として置いている。
const sampleAudioQueryJSON = `{
  "accent_phrases": [
    {
      "moras": [
        {"text": "コ", "consonant": "k", "consonant_length": 0.0764, "vowel": "o", "vowel_length": 0.1073, "pitch": 5.4098},
        {"text": "ン", "consonant": null, "consonant_length": null, "vowel": "N", "vowel_length": 0.0709, "pitch": 5.6019},
        {"text": "ニ", "consonant": "n", "consonant_length": 0.0402, "vowel": "i", "vowel_length": 0.0947, "pitch": 5.8113},
        {"text": "チ", "consonant": "ch", "consonant_length": 0.0624, "vowel": "i", "vowel_length": 0.0774, "pitch": 5.85},
        {"text": "ワ", "consonant": "w", "consonant_length": 0.071, "vowel": "a", "vowel_length": 0.1667, "pitch": 5.6394}
      ],
      "accent": 5,
      "pause_mora": {"text": "、", "consonant": null, "consonant_length": null, "vowel": "pau", "vowel_length": 0.32, "pitch": 0.0},
      "is_interrogative": false
    },
    {
      "moras": [
        {"text": "ナ", "consonant": "n", "consonant_length": 0.05, "vowel": "a", "vowel_length": 0.11, "pitch": 5.7},
        {"text": "ノ", "consonant": "n", "consonant_length": 0.04, "vowel": "o", "vowel_length": 0.12, "pitch": 5.5}
      ],
      "accent": 1,
      "pause_mora": null,
      "is_interrogative": true
    }
  ],
  "speedScale": 1.0,
  "pitchScale": 0.0,
  "intonationScale": 1.0,
  "volumeScale": 1.0,
  "prePhonemeLength": 0.1,
  "postPhonemeLength": 0.1,
  "outputSamplingRate": 24000,
  "outputStereo": false,
  "kana": "コンニチワ'、ナ'ノ",
  "futureField": {"nested": [1, 2, 3]}
}`

var fakeWAV = []byte("RIFF\x00\x00\x00\x00WAVEfake-pcm-data")

// recordedRequest はモックエンジンが受け取ったリクエストの記録。
type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// mockEngine は VOICEVOX ENGINE の代わりに立てる httptest 用ハンドラ。
type mockEngine struct {
	mu       sync.Mutex
	requests []recordedRequest
	routes   map[string]http.HandlerFunc
}

func (m *mockEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	m.mu.Lock()
	m.requests = append(m.requests, recordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.Query(),
		Header: r.Header.Clone(),
		Body:   body,
	})
	m.mu.Unlock()

	if h, ok := m.routes[r.URL.Path]; ok {
		h(w, r)
		return
	}
	http.Error(w, "未定義のパス: "+r.URL.Path, http.StatusNotFound)
}

func (m *mockEngine) recorded() []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recordedRequest(nil), m.requests...)
}

// startMock はモックエンジンを立て、そこを向いた Client を返す。
func startMock(t *testing.T, routes map[string]http.HandlerFunc) (*mockEngine, *Client) {
	t.Helper()
	m := &mockEngine{routes: routes}
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)

	c, err := New(EngineVoicevox, WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New が失敗した: %v", err)
	}
	return m, c
}

// okRoutes は audio_query と synthesis が正常に応答するルート定義を返す。
func okRoutes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/audio_query": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, sampleAudioQueryJSON)
		},
		"/synthesis": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write(fakeWAV)
		},
	}
}

func TestSynthesize_呼び出し順序とリクエスト内容(t *testing.T) {
	m, c := startMock(t, okRoutes())

	got, err := c.Synthesize(context.Background(), SynthesizeRequest{
		Text:    "こんにちは、なの",
		StyleID: 3,
	})
	if err != nil {
		t.Fatalf("Synthesize が失敗した: %v", err)
	}
	if string(got.WAV) != string(fakeWAV) {
		t.Errorf("wav が一致しない: got %q", got.WAV)
	}

	reqs := m.recorded()
	if len(reqs) != 2 {
		t.Fatalf("リクエスト数が 2 でない: %d 件 %+v", len(reqs), reqs)
	}

	// 1 回目: audio_query
	if reqs[0].Path != "/audio_query" {
		t.Errorf("1 回目のパスが /audio_query でない: %s", reqs[0].Path)
	}
	if reqs[0].Method != http.MethodPost {
		t.Errorf("audio_query が POST でない: %s", reqs[0].Method)
	}
	if v := reqs[0].Query.Get("text"); v != "こんにちは、なの" {
		t.Errorf("text クエリが違う: %q", v)
	}
	if v := reqs[0].Query.Get("speaker"); v != "3" {
		t.Errorf("speaker クエリが違う: %q", v)
	}

	// 2 回目: synthesis
	if reqs[1].Path != "/synthesis" {
		t.Errorf("2 回目のパスが /synthesis でない: %s", reqs[1].Path)
	}
	if reqs[1].Method != http.MethodPost {
		t.Errorf("synthesis が POST でない: %s", reqs[1].Method)
	}
	if v := reqs[1].Query.Get("speaker"); v != "3" {
		t.Errorf("synthesis の speaker クエリが違う: %q", v)
	}
	if reqs[1].Query.Has("text") {
		t.Errorf("synthesis に text クエリが付いている: %v", reqs[1].Query)
	}
	if ct := reqs[1].Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("synthesis の Content-Type が違う: %q", ct)
	}
	// audio_query の応答が body として渡っていること
	var sent map[string]any
	if err := json.Unmarshal(reqs[1].Body, &sent); err != nil {
		t.Fatalf("synthesis の body が JSON でない: %v (%s)", err, reqs[1].Body)
	}
	if _, ok := sent["accent_phrases"]; !ok {
		t.Errorf("synthesis の body に accent_phrases が無い: %s", reqs[1].Body)
	}
}

func TestSynthesize_上書き指定が無ければaudio_queryの応答をそのまま渡す(t *testing.T) {
	m, c := startMock(t, okRoutes())

	if _, err := c.Synthesize(context.Background(), SynthesizeRequest{Text: "そのまま", StyleID: 1}); err != nil {
		t.Fatalf("Synthesize が失敗した: %v", err)
	}

	reqs := m.recorded()
	if string(reqs[1].Body) != sampleAudioQueryJSON {
		t.Errorf("audio_query の応答が改変されている:\n--- got ---\n%s\n--- want ---\n%s", reqs[1].Body, sampleAudioQueryJSON)
	}
}

func TestSynthesize_パラメータ上書きがsynthesisへ反映される(t *testing.T) {
	m, c := startMock(t, okRoutes())

	res, err := c.Synthesize(context.Background(), SynthesizeRequest{
		Text:    "はやくちなのだ",
		StyleID: 3,
		Params: Params{
			SpeedScale:        new(1.25),
			PitchScale:        new(0.05),
			IntonationScale:   new(1.4),
			VolumeScale:       new(0.8),
			PrePhonemeLength:  new(0.2),
			PostPhonemeLength: new(0.35),
		},
	})
	if err != nil {
		t.Fatalf("Synthesize が失敗した: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(m.recorded()[1].Body, &sent); err != nil {
		t.Fatalf("synthesis の body が JSON でない: %v", err)
	}

	want := map[string]float64{
		"speedScale":        1.25,
		"pitchScale":        0.05,
		"intonationScale":   1.4,
		"volumeScale":       0.8,
		"prePhonemeLength":  0.2,
		"postPhonemeLength": 0.35,
	}
	for k, v := range want {
		got, ok := sent[k].(float64)
		if !ok {
			t.Errorf("synthesis の body に %s が無い: %v", k, sent[k])
			continue
		}
		if got != v {
			t.Errorf("%s が上書きされていない: got %v, want %v", k, got, v)
		}
	}

	// 上書きしていないフィールドは元のまま残ること
	if got, _ := sent["outputSamplingRate"].(float64); got != 24000 {
		t.Errorf("outputSamplingRate が失われた: %v", sent["outputSamplingRate"])
	}
	if _, ok := sent["accent_phrases"]; !ok {
		t.Error("上書きで accent_phrases が失われた")
	}
	// エンジン独自の未知フィールドも落とさないこと
	if _, ok := sent["futureField"]; !ok {
		t.Error("未知フィールド futureField が落ちた。型に無いフィールドも素通しする必要がある")
	}

	// 呼び出し元へ返る AudioQuery も上書き後の値であること
	if res.AudioQuery.SpeedScale != 1.25 {
		t.Errorf("戻り値の SpeedScale が上書き後になっていない: %v", res.AudioQuery.SpeedScale)
	}
	if res.AudioQuery.PostPhonemeLength != 0.35 {
		t.Errorf("戻り値の PostPhonemeLength が上書き後になっていない: %v", res.AudioQuery.PostPhonemeLength)
	}
}

func TestSynthesize_一部だけ上書きしても他は既定値のまま(t *testing.T) {
	m, c := startMock(t, okRoutes())

	if _, err := c.Synthesize(context.Background(), SynthesizeRequest{
		Text:    "すこしだけ",
		StyleID: 2,
		Params:  Params{SpeedScale: new(1.05)},
	}); err != nil {
		t.Fatalf("Synthesize が失敗した: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(m.recorded()[1].Body, &sent); err != nil {
		t.Fatalf("synthesis の body が JSON でない: %v", err)
	}
	if got, _ := sent["speedScale"].(float64); got != 1.05 {
		t.Errorf("speedScale が上書きされていない: %v", sent["speedScale"])
	}
	if got, _ := sent["volumeScale"].(float64); got != 1.0 {
		t.Errorf("指定していない volumeScale が変わった: %v", sent["volumeScale"])
	}
	if got, _ := sent["prePhonemeLength"].(float64); got != 0.1 {
		t.Errorf("指定していない prePhonemeLength が変わった: %v", sent["prePhonemeLength"])
	}
}

func TestSynthesize_AccentPhrasesとMorasが呼び出し元へ返る(t *testing.T) {
	_, c := startMock(t, okRoutes())

	res, err := c.Synthesize(context.Background(), SynthesizeRequest{Text: "こんにちは、なの", StyleID: 3})
	if err != nil {
		t.Fatalf("Synthesize が失敗した: %v", err)
	}

	q := res.AudioQuery
	if len(q.AccentPhrases) != 2 {
		t.Fatalf("accent_phrases の数が違う: %d", len(q.AccentPhrases))
	}

	first := q.AccentPhrases[0]
	if len(first.Moras) != 5 {
		t.Fatalf("1 つ目のアクセント句のモーラ数が違う: %d", len(first.Moras))
	}
	if first.Accent != 5 {
		t.Errorf("accent が違う: %d", first.Accent)
	}
	if first.IsInterrogative {
		t.Error("is_interrogative が true になっている")
	}

	// 子音を持つモーラ
	wantConsonant := "k"
	wantMora := Mora{
		Text:            "コ",
		Consonant:       &wantConsonant,
		ConsonantLength: new(0.0764),
		Vowel:           "o",
		VowelLength:     0.1073,
		Pitch:           5.4098,
	}
	if !reflect.DeepEqual(first.Moras[0], wantMora) {
		t.Errorf("モーラが復元できていない:\n got %+v (consonant=%v, len=%v)\nwant %+v",
			first.Moras[0], deref(first.Moras[0].Consonant), derefF(first.Moras[0].ConsonantLength), wantMora)
	}

	// 子音を持たないモーラは nil のまま（0 と区別できること）
	if first.Moras[1].Consonant != nil {
		t.Errorf("consonant が null なのに nil でない: %v", *first.Moras[1].Consonant)
	}
	if first.Moras[1].ConsonantLength != nil {
		t.Errorf("consonant_length が null なのに nil でない: %v", *first.Moras[1].ConsonantLength)
	}
	if first.Moras[1].VowelLength != 0.0709 {
		t.Errorf("vowel_length が違う: %v", first.Moras[1].VowelLength)
	}

	// pause_mora
	if first.PauseMora == nil {
		t.Fatal("pause_mora が失われた")
	}
	if first.PauseMora.Vowel != "pau" || first.PauseMora.VowelLength != 0.32 {
		t.Errorf("pause_mora の中身が違う: %+v", *first.PauseMora)
	}

	second := q.AccentPhrases[1]
	if second.PauseMora != nil {
		t.Errorf("pause_mora が null なのに nil でない: %+v", *second.PauseMora)
	}
	if !second.IsInterrogative {
		t.Error("is_interrogative が false になっている")
	}

	// 未知フィールドまで含めた原文も取り出せること（キャッシュへ無損失で残すため）
	var raw map[string]any
	if err := json.Unmarshal(res.RawAudioQuery, &raw); err != nil {
		t.Fatalf("RawAudioQuery が JSON でない: %v", err)
	}
	if _, ok := raw["futureField"]; !ok {
		t.Error("RawAudioQuery に未知フィールドが残っていない")
	}

	// その他のフィールド
	if q.OutputSamplingRate != 24000 || q.OutputStereo {
		t.Errorf("出力設定が違う: rate=%d stereo=%v", q.OutputSamplingRate, q.OutputStereo)
	}
	if q.Kana != "コンニチワ'、ナ'ノ" {
		t.Errorf("kana が違う: %q", q.Kana)
	}
}

func TestSynthesize_エンジン未起動なら起動を促すエラー(t *testing.T) {
	// 立ててすぐ閉じたサーバの URL を使い、接続拒否を起こす。
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := srv.URL
	srv.Close()

	c, err := New(EngineVoicevox, WithBaseURL(baseURL))
	if err != nil {
		t.Fatalf("New が失敗した: %v", err)
	}

	_, err = c.Synthesize(context.Background(), SynthesizeRequest{Text: "つながらないのだ", StyleID: 3})
	if err == nil {
		t.Fatal("接続できないのにエラーにならなかった")
	}

	var unavailable *EngineUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("EngineUnavailableError でない: %T %v", err, err)
	}
	if unavailable.BaseURL != baseURL {
		t.Errorf("BaseURL が記録されていない: %q", unavailable.BaseURL)
	}

	msg := err.Error()
	for _, want := range []string{"VOICEVOX ENGINE", baseURL, "起動", "scenaremo doctor"} {
		if !strings.Contains(msg, want) {
			t.Errorf("エラーメッセージに %q が含まれない: %s", want, msg)
		}
	}
}

func TestSynthesize_audio_queryが非200ならAPIError(t *testing.T) {
	m, c := startMock(t, map[string]http.HandlerFunc{
		"/audio_query": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = io.WriteString(w, `{"detail":[{"msg":"speaker not found"}]}`)
		},
		"/synthesis": func(w http.ResponseWriter, r *http.Request) {
			t.Error("audio_query が失敗したのに synthesis が呼ばれた")
		},
	})

	_, err := c.Synthesize(context.Background(), SynthesizeRequest{Text: "だめなのだ", StyleID: 9999})
	if err == nil {
		t.Fatal("422 なのにエラーにならなかった")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("APIError でない: %T %v", err, err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("StatusCode が違う: %d", apiErr.StatusCode)
	}
	if apiErr.Endpoint != "/audio_query" {
		t.Errorf("Endpoint が違う: %q", apiErr.Endpoint)
	}

	msg := err.Error()
	for _, want := range []string{"VOICEVOX ENGINE", "/audio_query", "422", "styleId=9999", "speaker not found"} {
		if !strings.Contains(msg, want) {
			t.Errorf("エラーメッセージに %q が含まれない: %s", want, msg)
		}
	}

	if len(m.recorded()) != 1 {
		t.Errorf("audio_query の失敗後もリクエストが続いている: %d 件", len(m.recorded()))
	}
}

func TestSynthesize_synthesisが非200ならAPIError(t *testing.T) {
	routes := okRoutes()
	routes["/synthesis"] = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "internal server error")
	}
	_, c := startMock(t, routes)

	_, err := c.Synthesize(context.Background(), SynthesizeRequest{Text: "こわれたのだ", StyleID: 3})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("APIError でない: %T %v", err, err)
	}
	if apiErr.Endpoint != "/synthesis" || apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("APIError の中身が違う: %+v", apiErr)
	}
	if !strings.Contains(err.Error(), "エンジンのログ") {
		t.Errorf("5xx の対処ヒントが無い: %s", err.Error())
	}
}

func TestSynthesize_contextキャンセルで打ち切られる(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	_, c := startMock(t, map[string]http.HandlerFunc{
		"/audio_query": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	_, err := c.Synthesize(ctx, SynthesizeRequest{Text: "とちゅうでやめるのだ", StyleID: 3})
	if err == nil {
		t.Fatal("キャンセルしたのにエラーにならなかった")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("context.Canceled を包んでいない: %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "キャンセル") {
		t.Errorf("キャンセルと分かるメッセージでない: %s", err.Error())
	}
	// 未起動と誤解させるメッセージを出さないこと
	var unavailable *EngineUnavailableError
	if errors.As(err, &unavailable) {
		t.Errorf("キャンセルが未起動エラーとして報告された: %v", err)
	}
}

func TestSynthesize_タイムアウトで打ち切られる(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	_, c := startMock(t, map[string]http.HandlerFunc{
		"/audio_query": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, sampleAudioQueryJSON)
		},
		"/synthesis": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.Synthesize(ctx, SynthesizeRequest{Text: "おそいのだ", StyleID: 3})
	if err == nil {
		t.Fatal("タイムアウトしたのにエラーにならなかった")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("context.DeadlineExceeded を包んでいない: %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "/synthesis") {
		t.Errorf("どの呼び出しで止まったか分からない: %s", err.Error())
	}
}

func TestSynthesize_不正な要求はリクエストを送らない(t *testing.T) {
	tests := []struct {
		name string
		req  SynthesizeRequest
		want error
	}{
		{"空テキスト", SynthesizeRequest{Text: "", StyleID: 3}, ErrEmptyText},
		{"空白のみ", SynthesizeRequest{Text: "  \n ", StyleID: 3}, ErrEmptyText},
		{"負の styleId", SynthesizeRequest{Text: "あ", StyleID: -1}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, c := startMock(t, okRoutes())
			_, err := c.Synthesize(context.Background(), tt.req)
			if err == nil {
				t.Fatal("エラーにならなかった")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("想定したエラーでない: %v", err)
			}
			if n := len(m.recorded()); n != 0 {
				t.Errorf("エンジンへリクエストを送ってしまった: %d 件", n)
			}
		})
	}
}

func TestSpeakers(t *testing.T) {
	_, c := startMock(t, map[string]http.HandlerFunc{
		"/speakers": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("/speakers が GET でない: %s", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[
			  {"name":"ずんだもん","speaker_uuid":"388f246b-8c41-4ac1-8e2d-5d79f3ff56d9","version":"0.14.4",
			   "styles":[{"name":"ノーマル","id":3,"type":"talk"},{"name":"あまあま","id":1,"type":"talk"}]}
			]`)
		},
	})

	speakers, err := c.Speakers(context.Background())
	if err != nil {
		t.Fatalf("Speakers が失敗した: %v", err)
	}
	if len(speakers) != 1 {
		t.Fatalf("話者数が違う: %d", len(speakers))
	}
	if speakers[0].Name != "ずんだもん" {
		t.Errorf("話者名が違う: %q", speakers[0].Name)
	}
	if speakers[0].SpeakerUUID == "" {
		t.Error("speaker_uuid が失われた")
	}
	if len(speakers[0].Styles) != 2 || speakers[0].Styles[0].ID != 3 || speakers[0].Styles[0].Name != "ノーマル" {
		t.Errorf("スタイルが違う: %+v", speakers[0].Styles)
	}
	if speakers[0].Styles[0].Type != "talk" {
		t.Errorf("スタイルの type が失われた: %+v", speakers[0].Styles[0])
	}
}

func TestPing(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"JSON 文字列", `"0.14.4"`, "0.14.4"},
		{"素のテキスト", "0.14.4\n", "0.14.4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := startMock(t, map[string]http.HandlerFunc{
				"/version": func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.WriteString(w, tt.body)
				},
			})
			got, err := c.Ping(context.Background())
			if err != nil {
				t.Fatalf("Ping が失敗した: %v", err)
			}
			if got != tt.want {
				t.Errorf("バージョンが違う: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPing_未起動なら起動を促すエラー(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := srv.URL
	srv.Close()

	c, err := New(EngineVoicevox, WithBaseURL(baseURL))
	if err != nil {
		t.Fatalf("New が失敗した: %v", err)
	}
	if _, err := c.Ping(context.Background()); err == nil {
		t.Fatal("接続できないのにエラーにならなかった")
	} else {
		var unavailable *EngineUnavailableError
		if !errors.As(err, &unavailable) {
			t.Fatalf("EngineUnavailableError でない: %T %v", err, err)
		}
	}
}

// stubDoer は HTTP クライアントを丸ごと差し替えられることの確認用。
type stubDoer struct {
	calls    []string
	response func(req *http.Request) *http.Response
}

func (s *stubDoer) Do(req *http.Request) (*http.Response, error) {
	s.calls = append(s.calls, req.URL.Path)
	return s.response(req), nil
}

func TestWithHTTPClient_差し替えできる(t *testing.T) {
	doer := &stubDoer{
		response: func(req *http.Request) *http.Response {
			body := sampleAudioQueryJSON
			if req.URL.Path == "/synthesis" {
				body = string(fakeWAV)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}
		},
	}

	c, err := New(EngineVoicevox, WithHTTPClient(doer))
	if err != nil {
		t.Fatalf("New が失敗した: %v", err)
	}
	res, err := c.Synthesize(context.Background(), SynthesizeRequest{Text: "さしかえ", StyleID: 3})
	if err != nil {
		t.Fatalf("Synthesize が失敗した: %v", err)
	}
	if string(res.WAV) != string(fakeWAV) {
		t.Errorf("wav が一致しない: %q", res.WAV)
	}
	if !reflect.DeepEqual(doer.calls, []string{"/audio_query", "/synthesis"}) {
		t.Errorf("呼び出し順序が違う: %v", doer.calls)
	}
}

func TestNew(t *testing.T) {
	t.Run("既定 baseURL", func(t *testing.T) {
		tests := map[EngineKind]string{
			EngineVoicevox:    "http://127.0.0.1:50021",
			EngineAivisSpeech: "http://127.0.0.1:10101",
			EngineCoeiroink:   "http://127.0.0.1:50032",
		}
		for kind, want := range tests {
			c, err := New(kind)
			if err != nil {
				t.Fatalf("%s: New が失敗した: %v", kind, err)
			}
			if c.BaseURL() != want {
				t.Errorf("%s: baseURL が違う: got %q, want %q", kind, c.BaseURL(), want)
			}
			if c.Kind() != kind {
				t.Errorf("Kind が違う: %q", c.Kind())
			}
		}
	})

	t.Run("末尾のスラッシュを落とす", func(t *testing.T) {
		c, err := New(EngineVoicevox, WithBaseURL("http://localhost:50021/"))
		if err != nil {
			t.Fatalf("New が失敗した: %v", err)
		}
		if c.BaseURL() != "http://localhost:50021" {
			t.Errorf("baseURL が違う: %q", c.BaseURL())
		}
	})

	t.Run("未知のエンジンは baseUrl が必要", func(t *testing.T) {
		_, err := New("unknown-engine")
		if err == nil {
			t.Fatal("未知のエンジンなのにエラーにならなかった")
		}
		for _, want := range []string{"unknown-engine", "baseUrl", "voicevox"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("エラーメッセージに %q が含まれない: %s", want, err.Error())
			}
		}
	})

	t.Run("未知のエンジンでも baseUrl があれば使える", func(t *testing.T) {
		c, err := New("unknown-engine", WithBaseURL("http://127.0.0.1:12345"))
		if err != nil {
			t.Fatalf("New が失敗した: %v", err)
		}
		if c.BaseURL() != "http://127.0.0.1:12345" {
			t.Errorf("baseURL が違う: %q", c.BaseURL())
		}
	})

	t.Run("不正な baseUrl", func(t *testing.T) {
		for _, bad := range []string{"127.0.0.1:50021", "ftp://127.0.0.1", "http://"} {
			if _, err := New(EngineVoicevox, WithBaseURL(bad)); err == nil {
				t.Errorf("%q が受け入れられた", bad)
			}
		}
	})
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func derefF(f *float64) any {
	if f == nil {
		return "<nil>"
	}
	return *f
}
