package tts

import (
	"encoding/json"
	"fmt"
	"time"
)

// AudioQuery は /audio_query が返し、/synthesis が受け取る合成パラメータ。
//
// accent_phrases と moras まで型として持たせているのは、字幕のカラオケ表示や
// 口パク（issue #20）がモーラ単位の長さを必要とするため。
// 後から足すと、それまでに合成した全音声を作り直す羽目になる。
type AudioQuery struct {
	AccentPhrases []AccentPhrase `json:"accent_phrases"`

	// 以下は台本側から Params で上書きできる項目。
	SpeedScale        float64 `json:"speedScale"`
	PitchScale        float64 `json:"pitchScale"`
	IntonationScale   float64 `json:"intonationScale"`
	VolumeScale       float64 `json:"volumeScale"`
	PrePhonemeLength  float64 `json:"prePhonemeLength"`
	PostPhonemeLength float64 `json:"postPhonemeLength"`

	// PauseLength は句読点の無音長を秒で固定する。null ならエンジンの推定値を使う（新しめのエンジンのみ）。
	PauseLength *float64 `json:"pauseLength,omitempty"`
	// PauseLengthScale は句読点の無音長の倍率（新しめのエンジンのみ）。
	PauseLengthScale *float64 `json:"pauseLengthScale,omitempty"`

	OutputSamplingRate int  `json:"outputSamplingRate"`
	OutputStereo       bool `json:"outputStereo"`

	// Kana は読み上げに使われた AquesTalk 風記法。エンジンによっては返らない。
	Kana string `json:"kana,omitempty"`
}

// AccentPhrase はアクセント句 1 つ。
type AccentPhrase struct {
	Moras []Mora `json:"moras"`
	// Accent はアクセント核の位置（1 始まり）。
	Accent int `json:"accent"`
	// PauseMora は句の直後の無音。無ければ nil。
	PauseMora *Mora `json:"pause_mora,omitempty"`
	// IsInterrogative は疑問形として語尾を上げるか。
	IsInterrogative bool `json:"is_interrogative"`
}

// Mora はモーラ 1 つ。カナ 1 文字ぶんの発話単位で、口パクと字幕の同期はこの粒度で行う。
type Mora struct {
	// Text はカナ表記（「コ」「ン」など）。
	Text string `json:"text"`
	// Consonant は子音の音素。母音のみのモーラでは nil。
	Consonant *string `json:"consonant"`
	// ConsonantLength は子音の長さ（秒）。Consonant が nil なら nil。
	ConsonantLength *float64 `json:"consonant_length"`
	// Vowel は母音の音素（a/i/u/e/o/N/pau/cl など）。
	Vowel string `json:"vowel"`
	// VowelLength は母音の長さ（秒）。
	VowelLength float64 `json:"vowel_length"`
	// Pitch は音高（無声化モーラは 0）。
	Pitch float64 `json:"pitch"`
}

// MoraTiming は 1 モーラが wav のどこで鳴るか。Offset は wav の先頭からの位置。
type MoraTiming struct {
	Text     string        // カナ表記（「コ」「、」など）
	Vowel    string        // 母音の音素 (a/i/u/e/o/N/pau/cl)
	Offset   time.Duration // wav の先頭からこのモーラが鳴り始めるまで
	Duration time.Duration // このモーラの発話長（子音 + 母音）
}

// MoraTimings は 1 モーラが wav のどこで鳴るかの実時間を返す。
func (q AudioQuery) MoraTimings() []MoraTiming {
	if len(q.AccentPhrases) == 0 {
		return nil
	}

	speed := q.SpeedScale
	if speed <= 0 {
		speed = 1.0
	}

	var timings []MoraTiming
	currentSec := q.PrePhonemeLength

	addMora := func(m Mora) {
		dur := m.Duration()
		if m.Vowel == "pau" {
			if q.PauseLength != nil {
				dur = *q.PauseLength
			}
			if q.PauseLengthScale != nil {
				dur *= *q.PauseLengthScale
			}
		}

		startSec := currentSec / speed
		currentSec += dur
		endSec := currentSec / speed

		startDur := time.Duration(startSec * float64(time.Second))
		endDur := time.Duration(endSec * float64(time.Second))

		timings = append(timings, MoraTiming{
			Text:     m.Text,
			Vowel:    m.Vowel,
			Offset:   startDur,
			Duration: endDur - startDur,
		})
	}

	for _, ap := range q.AccentPhrases {
		for _, m := range ap.Moras {
			addMora(m)
		}
		if ap.PauseMora != nil {
			addMora(*ap.PauseMora)
		}
	}
	return timings
}

// Duration はモーラの発話長を秒で返す。
func (m Mora) Duration() float64 {
	d := m.VowelLength
	if m.ConsonantLength != nil {
		d += *m.ConsonantLength
	}
	return d
}

// Params は台本側から AudioQuery を上書きするための値。
// nil のフィールドはエンジンが返した既定値をそのまま使う。
type Params struct {
	SpeedScale        *float64
	PitchScale        *float64
	IntonationScale   *float64
	VolumeScale       *float64
	PrePhonemeLength  *float64
	PostPhonemeLength *float64
}

// overrides は上書き対象を AudioQuery の JSON キー名で返す。指定の無いものは含まない。
func (p Params) overrides() map[string]float64 {
	src := map[string]*float64{
		"speedScale":        p.SpeedScale,
		"pitchScale":        p.PitchScale,
		"intonationScale":   p.IntonationScale,
		"volumeScale":       p.VolumeScale,
		"prePhonemeLength":  p.PrePhonemeLength,
		"postPhonemeLength": p.PostPhonemeLength,
	}
	out := make(map[string]float64, len(src))
	for k, v := range src {
		if v != nil {
			out[k] = *v
		}
	}
	return out
}

// IsZero は上書き指定が 1 つも無いことを返す。
func (p Params) IsZero() bool { return len(p.overrides()) == 0 }

// applyParams は /audio_query が返した JSON へ台本側の上書きを適用する。
//
// 型付きの AudioQuery を再シリアライズすると、エンジン独自の未知フィールドを落としてしまう。
// そこで map 経由で対象キーだけを差し替え、それ以外は受け取った JSON のまま /synthesis へ渡す。
func applyParams(raw []byte, p Params) ([]byte, error) {
	ov := p.overrides()
	if len(ov) == 0 {
		return raw, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("audio_query の応答が JSON オブジェクトとして読めませんでした: %w", err)
	}
	for k, v := range ov {
		m[k] = v
	}
	out, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("audio_query の再構築に失敗しました: %w", err)
	}
	return out, nil
}
