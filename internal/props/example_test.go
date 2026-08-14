package props_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/takiren/scenaremo/internal/cache"
	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/script"
	"github.com/takiren/scenaremo/internal/tts"
)

var updateExample = flag.Bool("update", false, "examples/minimal/props.json を書き直す")

// exampleDir は examples/minimal の位置。
const exampleDir = "../../examples/minimal"

// exampleDurations は examples/minimal の各セリフの音声長。台本に書かれた順に並べる。
//
// VOICEVOX で実際に合成した値ではなく、サンプルとして固定した値である。
// ここを実測に頼ると、エンジンのバージョンが変わるたびにサンプルが揺れてテストが落ちる。
var exampleDurations = []time.Duration{
	2400 * time.Millisecond, // 今日はリモーションの話をするのだ
	3120 * time.Millisecond, // スライドショー形式の / 解説動画を作りますね
	1880 * time.Millisecond, // まず台本を書くのだ
	2760 * time.Millisecond, // あとはCLIが音声を作ってくれるのだ
}

// exampleCredits は examples/minimal の話者エイリアスに対応するクレジット情報。
// 本来は build がエンジンの /speakers から解決する（→ issue #16）。
var exampleCredits = map[string]props.SpeakerCredit{
	"zundamon": {Name: "ずんだもん", UUID: "388f246b-8c41-4ac1-8e2d-5d79f3ff56d9"},
	"metan":    {Name: "四国めたん", UUID: "7ffcb7ce-00ec-4bdc-82cd-45a8889e43ff"},
}

// TestExampleProps は examples/minimal/props.json が台本と食い違っていないことを確かめる。
//
// このファイルは renderer 側（issue #8 / #9）が VOICEVOX を起動せずに開発を始められるようにするための
// 見本であり、契約が実際にどういう JSON になるのかを示す唯一の現物でもある。
// 台本やスキーマを直したら go test ./internal/props -update で作り直すこと。
func TestExampleProps(t *testing.T) {
	s, err := script.Load(filepath.Join(exampleDir, "script.yaml"))
	if err != nil {
		t.Fatalf("台本を読めない: %v", err)
	}

	in := props.Input{
		Script:  s,
		Audio:   exampleAudio(t, s),
		Credits: exampleCredits,
		// バージョンを埋め込むと、CLI の版が上がるたびにサンプルの差分になる。
		// 見本の役目には要らないので固定の文字列にしておく。
		GeneratedBy: "scenaremo (example)",
	}

	data, err := props.Marshal(build(t, in))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	validateAgainstSchema(t, data)

	path := filepath.Join(exampleDir, props.FileName)
	if *updateExample {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("サンプルを書き出せない: %v", err)
		}
		t.Logf("%s を書き直した", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("サンプルを読めない (go test ./internal/props -update で作れます): %v", err)
	}
	if !bytes.Equal(data, want) {
		t.Errorf("%s が台本と食い違っている。go test ./internal/props -update で作り直してください", path)
	}
}

// exampleAudio は台本の各セリフに対応する合成結果を組み立てる。
//
// wav のパスはキャッシュキーから決まる。実物の build が置く場所と同じ名前になるように、
// ここでも cache.Key を通している（サンプルのパスが絵空事にならないようにするため）。
func exampleAudio(t *testing.T, s *script.Script) [][]props.LineAudio {
	t.Helper()

	audio := make([][]props.LineAudio, 0, len(s.Scenes))
	next := 0
	for _, scene := range s.Scenes {
		lines := make([]props.LineAudio, 0, len(scene.Lines))
		for _, line := range scene.Lines {
			if next >= len(exampleDurations) {
				t.Fatalf("exampleDurations の数が台本のセリフ数に足りない (%d 件)", len(exampleDurations))
			}
			speaker := s.Speakers[line.Speaker]
			key := cache.Key(tts.EngineKind(speaker.Engine), tts.SynthesizeRequest{
				Text:    line.Text,
				StyleID: speaker.StyleID,
				Params: tts.Params{
					SpeedScale:      speaker.SpeedScale,
					PitchScale:      speaker.PitchScale,
					IntonationScale: speaker.IntonationScale,
					VolumeScale:     speaker.VolumeScale,
				},
			})
			lines = append(lines, props.LineAudio{
				Path:     ".scenaremo/audio/" + key + ".wav",
				Duration: exampleDurations[next],
			})
			next++
		}
		audio = append(audio, lines)
	}

	if next != len(exampleDurations) {
		t.Fatalf("exampleDurations が %d 件余っている", len(exampleDurations)-next)
	}
	return audio
}
