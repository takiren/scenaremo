// Package tts は台本のテキストを音声へ合成するエンジンのクライアントを提供する。
//
// VOICEVOX / AivisSpeech / COEIROINK は HTTP API がほぼ同一なので、
// EngineKind と baseURL の差し替えだけで同じ Client が扱える（→ README「設計方針 4」）。
// 将来のクラウド TTS は Engine を実装して差し込む想定で、それ以上の抽象化はしない。
package tts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// EngineKind は音声合成エンジンの種別。台本の speakers[].engine に対応する。
type EngineKind string

const (
	// EngineVoicevox は VOICEVOX ENGINE。
	EngineVoicevox EngineKind = "voicevox"
	// EngineAivisSpeech は AivisSpeech Engine。VOICEVOX 互換 API を持つ。
	EngineAivisSpeech EngineKind = "aivisspeech"
	// EngineCoeiroink は COEIROINK v2。VOICEVOX 互換 API を持つ。
	EngineCoeiroink EngineKind = "coeiroink"
)

// engineDefaults は VOICEVOX 互換エンジンの既定値。
// ここに載っているエンジンは Client がそのまま扱える。
var engineDefaults = map[EngineKind]struct {
	baseURL     string
	displayName string
}{
	EngineVoicevox:    {"http://127.0.0.1:50021", "VOICEVOX ENGINE"},
	EngineAivisSpeech: {"http://127.0.0.1:10101", "AivisSpeech Engine"},
	EngineCoeiroink:   {"http://127.0.0.1:50032", "COEIROINK"},
}

// DefaultBaseURL は kind の既定 baseURL を返す。未知の種別なら空文字列を返す。
func DefaultBaseURL(kind EngineKind) string {
	return engineDefaults[kind].baseURL
}

// DisplayName はエラーメッセージ用のエンジン表示名を返す。
// 未知の種別なら kind をそのまま返す。
func DisplayName(kind EngineKind) string {
	if d, ok := engineDefaults[kind]; ok {
		return d.displayName
	}
	return string(kind)
}

// KnownKinds は Client が既定値を持つエンジン種別を名前順で返す。
func KnownKinds() []EngineKind {
	kinds := make([]EngineKind, 0, len(engineDefaults))
	for k := range engineDefaults {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return kinds
}

// Engine は音声合成エンジンの最小の口。
// build が必要とするのは「テキストを wav にする」ことだけなので、ここには合成しか置かない。
type Engine interface {
	// Kind はエンジン種別を返す。キャッシュキーとエラーメッセージに使う。
	Kind() EngineKind
	// Synthesize はテキストを合成し、wav バイト列と実際に使われた AudioQuery を返す。
	Synthesize(ctx context.Context, req SynthesizeRequest) (*SynthesizeResult, error)
}

// SpeakerLister は話者一覧を取得できるエンジン。`scenaremo speakers` と credits が使う。
// クラウド TTS が対応できるとは限らないため Engine とは分けている。
type SpeakerLister interface {
	Speakers(ctx context.Context) ([]Speaker, error)
}

// Pinger は疎通確認に対応するエンジン。`scenaremo doctor` が使う。
// 戻り値はエンジンのバージョン文字列。
type Pinger interface {
	Ping(ctx context.Context) (string, error)
}

// SynthesizeRequest は 1 セリフ分の合成要求。
type SynthesizeRequest struct {
	// Text は喋らせるテキスト。
	Text string
	// StyleID は話者スタイル ID（VOICEVOX の speaker クエリに渡す値）。
	StyleID int
	// Params は台本側からの AudioQuery 上書き。nil のフィールドはエンジンの既定値を使う。
	Params Params
}

// ErrEmptyText は合成テキストが空だったことを表す。
var ErrEmptyText = errors.New("合成するテキストが空です。台本の text を確認してください")

func (r SynthesizeRequest) validate() error {
	if strings.TrimSpace(r.Text) == "" {
		return ErrEmptyText
	}
	if r.StyleID < 0 {
		return fmt.Errorf("styleId は 0 以上である必要があります: %d", r.StyleID)
	}
	return nil
}

// SynthesizeResult は合成結果。
type SynthesizeResult struct {
	// WAV は合成された wav ファイルのバイト列。
	WAV []byte
	// AudioQuery は synthesis に渡した AudioQuery（上書き適用後）。
	//
	// モーラ単位の長さを持つため、字幕のカラオケ表示や口パク（issue #20）はこれを使う。
	// 後から取り回せるようにしておかないと、全セリフの再合成が必要になる。
	AudioQuery AudioQuery
	// RawAudioQuery は AudioQuery と同じ内容の、synthesis へ実際に送った JSON。
	// キャッシュへ保存するならこちらを使うと、型に無いエンジン独自のフィールドも失われない。
	RawAudioQuery json.RawMessage
}

// Speaker は /speakers が返す話者 1 件。
type Speaker struct {
	Name        string  `json:"name"`
	SpeakerUUID string  `json:"speaker_uuid"`
	Styles      []Style `json:"styles"`
	Version     string  `json:"version"`
}

// Style は話者のスタイル（「ノーマル」「あまあま」など）。
type Style struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
	// Type は talk / singing_teacher / frame_decode / sing のいずれか。
	// 古いエンジンは返さないため空になりうる。
	Type string `json:"type,omitempty"`
}

// Float64 は Params のフィールドへ値を渡すためのヘルパ。
//
//	tts.Params{SpeedScale: tts.Float64(1.05)}
func Float64(v float64) *float64 { return &v }

// HTTPDoer は http.Client の差し替え口。テストや将来のリトライ層のために interface にしている。
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

var (
	_ Engine        = (*Client)(nil)
	_ SpeakerLister = (*Client)(nil)
	_ Pinger        = (*Client)(nil)
)
