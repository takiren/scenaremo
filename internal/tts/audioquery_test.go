package tts

import (
	"encoding/json"
	"math"
	"testing"
)

func TestApplyParams_上書きなしなら原文のまま(t *testing.T) {
	raw := []byte(sampleAudioQueryJSON)
	got, err := applyParams(raw, Params{})
	if err != nil {
		t.Fatalf("applyParams が失敗した: %v", err)
	}
	if string(got) != sampleAudioQueryJSON {
		t.Errorf("原文が変わった:\n%s", got)
	}
}

func TestApplyParams_未知フィールドを落とさない(t *testing.T) {
	got, err := applyParams([]byte(sampleAudioQueryJSON), Params{VolumeScale: new(0.5)})
	if err != nil {
		t.Fatalf("applyParams が失敗した: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("結果が JSON でない: %v", err)
	}
	if v, _ := m["volumeScale"].(float64); v != 0.5 {
		t.Errorf("volumeScale が上書きされていない: %v", m["volumeScale"])
	}
	future, ok := m["futureField"].(map[string]any)
	if !ok {
		t.Fatalf("未知フィールドが落ちた: %v", m["futureField"])
	}
	if nested, ok := future["nested"].([]any); !ok || len(nested) != 3 {
		t.Errorf("未知フィールドの中身が壊れた: %v", future)
	}
}

func TestApplyParams_JSONでなければエラー(t *testing.T) {
	if _, err := applyParams([]byte("not json"), Params{SpeedScale: new(1.2)}); err == nil {
		t.Fatal("不正な JSON なのにエラーにならなかった")
	}
}

func TestParams_IsZero(t *testing.T) {
	if !(Params{}).IsZero() {
		t.Error("空の Params が IsZero でない")
	}
	if (Params{PitchScale: new(float64(0))}).IsZero() {
		t.Error("0 を明示指定した Params が IsZero になった（未指定と区別できていない）")
	}
}

func TestParams_Fingerprint(t *testing.T) {
	if got := (Params{}).Fingerprint(); (Params{SpeedScale: nil}).Fingerprint() != got {
		t.Errorf("空の Params の Fingerprint が安定していない")
	}

	nilFP := (Params{SpeedScale: nil}).Fingerprint()
	zeroFP := (Params{SpeedScale: new(float64(0))}).Fingerprint()
	if nilFP == zeroFP {
		t.Errorf("nil と 0 の Fingerprint が同じ: %q", nilFP)
	}

	// 合成結果を変える全フィールドが Fingerprint に反映されていることを確認する（#65）。
	fields := []struct {
		name   string
		params Params
	}{
		{"SpeedScale", Params{SpeedScale: new(1.1)}},
		{"PitchScale", Params{PitchScale: new(1.1)}},
		{"IntonationScale", Params{IntonationScale: new(1.1)}},
		{"VolumeScale", Params{VolumeScale: new(1.1)}},
		{"PrePhonemeLength", Params{PrePhonemeLength: new(1.1)}},
		{"PostPhonemeLength", Params{PostPhonemeLength: new(1.1)}},
	}
	base := Params{}.Fingerprint()
	for _, f := range fields {
		t.Run(f.name, func(t *testing.T) {
			if got := f.params.Fingerprint(); got == base {
				t.Errorf("%s が Fingerprint に反映されていない", f.name)
			}
		})
	}
}

func TestAudioQuery_JSONの往復(t *testing.T) {
	var q AudioQuery
	if err := json.Unmarshal([]byte(sampleAudioQueryJSON), &q); err != nil {
		t.Fatalf("デコードに失敗した: %v", err)
	}
	// エンジンが返さなかった任意フィールドは nil のままであること
	if q.PauseLength != nil || q.PauseLengthScale != nil {
		t.Errorf("存在しないフィールドが nil でない: %v %v", q.PauseLength, q.PauseLengthScale)
	}

	encoded, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("エンコードに失敗した: %v", err)
	}
	var back AudioQuery
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("再デコードに失敗した: %v", err)
	}
	if len(back.AccentPhrases) != len(q.AccentPhrases) {
		t.Fatalf("アクセント句が失われた: %d -> %d", len(q.AccentPhrases), len(back.AccentPhrases))
	}
	if back.AccentPhrases[0].Moras[0].Text != "コ" {
		t.Errorf("モーラの text が失われた: %+v", back.AccentPhrases[0].Moras[0])
	}
	if back.AccentPhrases[0].PauseMora == nil {
		t.Error("pause_mora が往復で失われた")
	}
}

func TestMora_Duration(t *testing.T) {
	tests := []struct {
		name string
		mora Mora
		want float64
	}{
		{"子音あり", Mora{ConsonantLength: new(0.05), VowelLength: 0.1}, 0.15},
		{"子音なし", Mora{VowelLength: 0.07}, 0.07},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mora.Duration(); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("Duration が違う: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKnownKinds(t *testing.T) {
	kinds := KnownKinds()
	if len(kinds) != 3 {
		t.Fatalf("既知のエンジン数が違う: %v", kinds)
	}
	for i := 1; i < len(kinds); i++ {
		if kinds[i-1] >= kinds[i] {
			t.Errorf("名前順になっていない: %v", kinds)
		}
	}
	for _, k := range kinds {
		if DefaultBaseURL(k) == "" {
			t.Errorf("%s の既定 baseURL が無い", k)
		}
		if DisplayName(k) == "" {
			t.Errorf("%s の表示名が無い", k)
		}
	}
	if DisplayName("unknown") != "unknown" {
		t.Errorf("未知の種別の表示名がフォールバックしない: %q", DisplayName("unknown"))
	}
}
