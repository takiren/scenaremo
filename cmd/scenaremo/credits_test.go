package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takiren/scenaremo/internal/build"
	"github.com/takiren/scenaremo/internal/project"
	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/script"
	"github.com/takiren/scenaremo/internal/tts"
)

// stubCredits はクレジット集計の本体を差し替える。
// テストが実機の VOICEVOX や台本の有無に左右されないようにするため。
func stubCredits(t *testing.T, fn func(context.Context, build.CreditsOptions) (*build.CreditsResult, error)) {
	t.Helper()
	original := runCredits
	runCredits = fn
	t.Cleanup(func() { runCredits = original })
}

// fakeCredits は話者2人ぶんのクレジット。ずんだもんは別スタイルも使っている。
func fakeCredits(dir string) *build.CreditsResult {
	return &build.CreditsResult{
		Layout: &project.Layout{
			Dir:       dir,
			OutDir:    filepath.Join(dir, project.OutDirName),
			PropsPath: filepath.Join(dir, project.OutDirName, props.FileName),
		},
		Credits: props.Credits{
			Entries: []props.Entry{
				{
					Engine:      "voicevox",
					SpeakerName: "ずんだもん",
					SpeakerUUID: "388f246b-8c41-4ac1-8e2d-5d79f3ff56d9",
					StyleIDs:    []int{1, 3},
					Text:        "VOICEVOX:ずんだもん",
				},
				{
					Engine:      "voicevox",
					SpeakerName: "四国めたん",
					SpeakerUUID: "7ffcb7ce-00ec-4bdc-82cd-45a8889e43ff",
					StyleIDs:    []int{2},
					Text:        "VOICEVOX:四国めたん",
				},
			},
		},
	}
}

func TestCredits_クレジット表記だけを標準出力へ1行ずつ出す(t *testing.T) {
	stubCredits(t, func(context.Context, build.CreditsOptions) (*build.CreditsResult, error) {
		return fakeCredits("videos/ep01"), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"credits", "videos/ep01"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	// 概要欄へそのまま貼れることが、この出力に求められている唯一のことである。
	// 見出しも番号も混ぜない（混ぜれば貼ったあとに人が消して回る）。
	want := "VOICEVOX:ずんだもん\nVOICEVOX:四国めたん\n"
	if stdout.String() != want {
		t.Errorf("標準出力が違う: %q (want %q)", stdout.String(), want)
	}
	// 人向けの補足は標準エラーへ。件数を添えて、台本の話者と数え合わせられるようにする。
	for _, s := range []string{"2 件", "必ず記載"} {
		if !strings.Contains(stderr.String(), s) {
			t.Errorf("補足に %q が含まれない: %s", s, stderr.String())
		}
	}
}

func TestCredits_jsonなら内訳まで出す(t *testing.T) {
	stubCredits(t, func(context.Context, build.CreditsOptions) (*build.CreditsResult, error) {
		return fakeCredits("videos/ep01"), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"credits", "videos/ep01", "--json"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}

	// 標準出力だけで JSON として読み切れること。補足が混ざっていれば解析に失敗する。
	var got []props.Entry
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("標準出力が JSON として読めない: %v (%s)", err, stdout.String())
	}
	if len(got) != 2 {
		t.Fatalf("件数が違う: %d (%+v)", len(got), got)
	}
	if got[0].Text != "VOICEVOX:ずんだもん" {
		t.Errorf("[0].text が違う: %q", got[0].Text)
	}
	// 合成せずに UUID とスタイル ID を取れることが、この形式を用意した理由である。
	if got[0].SpeakerUUID == "" {
		t.Error("[0].speakerUuid が落ちている")
	}
	if len(got[0].StyleIDs) != 2 || got[0].StyleIDs[0] != 1 || got[0].StyleIDs[1] != 3 {
		t.Errorf("[0].styleIds が違う: %v", got[0].StyleIDs)
	}
}

func TestCredits_オプションが本体へ伝わる(t *testing.T) {
	var got build.CreditsOptions
	stubCredits(t, func(_ context.Context, opts build.CreditsOptions) (*build.CreditsResult, error) {
		got = opts
		return fakeCredits("videos/ep01"), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"credits", "videos/ep01", "--voicevox-url=http://192.168.0.2:50021",
	}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	if got.Dir != "videos/ep01" {
		t.Errorf("動画ディレクトリが渡っていない: %q", got.Dir)
	}
	if got.VoicevoxURL != "http://192.168.0.2:50021" {
		t.Errorf("接続先が渡っていない: %q", got.VoicevoxURL)
	}
}

func TestCredits_既定の接続先はVOICEVOXの既定値(t *testing.T) {
	var got build.CreditsOptions
	stubCredits(t, func(_ context.Context, opts build.CreditsOptions) (*build.CreditsResult, error) {
		got = opts
		return fakeCredits("videos/ep01"), nil
	})

	var stdout, stderr bytes.Buffer
	run(context.Background(), []string{"credits", "videos/ep01"}, &stdout, &stderr)

	if got.VoicevoxURL != "http://127.0.0.1:50021" {
		t.Errorf("既定の接続先が違う: %q", got.VoicevoxURL)
	}
}

func TestCredits_ディレクトリを指定しなければ使い方を出す(t *testing.T) {
	stubCredits(t, func(context.Context, build.CreditsOptions) (*build.CreditsResult, error) {
		t.Error("引数が無いのに集計が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"credits"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Error("引数が無いのに成功として終了した")
	}
	msg := stderr.String()
	for _, want := range []string{"使い方:", "videos/ep01"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q が含まれない: %s", want, msg)
		}
	}
}

func TestCredits_ディレクトリは1つだけ(t *testing.T) {
	stubCredits(t, func(context.Context, build.CreditsOptions) (*build.CreditsResult, error) {
		t.Error("引数が多いのに集計が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"credits", "videos/ep01", "videos/ep02"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Error("引数が多いのに成功として終了した")
	}
	if !strings.Contains(stderr.String(), "videos/ep02") {
		t.Errorf("何が余分だったのか分からない: %s", stderr.String())
	}
}

func TestCredits_台本の検証エラーは報告だけを出す(t *testing.T) {
	stubCredits(t, func(context.Context, build.CreditsOptions) (*build.CreditsResult, error) {
		return nil, &script.Error{
			Filename: "videos/ep01/script.yaml",
			Issues: []script.Issue{{
				Path:    "speakers.zundamon.styleId",
				Line:    7,
				Message: "styleId は必須です",
				Hint:    "scenaremo speakers で使える styleId を確認してください",
			}},
		}
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"credits", "videos/ep01"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Fatal("台本が不正なのに成功として終了した")
	}
	msg := stderr.String()
	for _, want := range []string{"videos/ep01/script.yaml", "styleId は必須です", "ヒント:"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q が含まれない: %s", want, msg)
		}
	}
	// 報告はそれ自体が整形済みなので、頭に "scenaremo: " を重ねない。
	if strings.Contains(msg, "scenaremo: videos/ep01/script.yaml") {
		t.Errorf("報告の頭に scenaremo: を重ねている: %s", msg)
	}
	// 台本の誤りは使い方の誤りではないので、使い方は突きつけない。
	if strings.Contains(msg, "使い方:") {
		t.Errorf("台本の誤りで usage が出ている: %s", msg)
	}
}

func TestCredits_エンジンが起動していなければ案内をそのまま出す(t *testing.T) {
	stubCredits(t, func(context.Context, build.CreditsOptions) (*build.CreditsResult, error) {
		return nil, &tts.EngineUnavailableError{
			Kind:    tts.EngineVoicevox,
			BaseURL: "http://127.0.0.1:50021",
		}
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"credits", "videos/ep01"}, &stdout, &stderr)

	if code != exitFailure {
		t.Errorf("終了コードが違う: %d", code)
	}
	// 何をすればよいかは tts の案内が持っている。ここで文面を作り直して潰さない。
	for _, want := range []string{"エンジンを起動してから再実行", "scenaremo doctor"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("%q が含まれない: %s", want, stderr.String())
		}
	}
	// 失敗したのに空でない出力を残すと、貼り付ける側がそれをクレジットだと思ってしまう。
	if stdout.Len() != 0 {
		t.Errorf("失敗なのに標準出力へ書いている: %s", stdout.String())
	}
}

func TestCredits_クレジットが1件も無ければ失敗する(t *testing.T) {
	stubCredits(t, func(context.Context, build.CreditsOptions) (*build.CreditsResult, error) {
		return &build.CreditsResult{Credits: props.Credits{}}, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"credits", "videos/ep01"}, &stdout, &stderr)

	// 黙って何も出さないと「クレジットは要らない」と読めてしまう。それはこの機能の目的の裏返しである。
	if code == exitSuccess {
		t.Errorf("1 件も無いのに成功として終了した: %q", stdout.String())
	}
}

func TestCredits_helpは成功する(t *testing.T) {
	stubCredits(t, func(context.Context, build.CreditsOptions) (*build.CreditsResult, error) {
		t.Error("--help なのに集計が走った")
		return nil, nil
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"credits", "--help"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Errorf("--help の終了コードが 0 でない: %d", code)
	}
	out := stdout.String()
	for _, want := range []string{"scenaremo credits", "--voicevox-url", "--json"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が含まれない: %s", want, out)
		}
	}
	// 使い方の行に cobra 既定の英語 ([flags]) を混ぜない。
	if strings.Contains(out, "[flags]") {
		t.Errorf("使い方に英語が混ざっている: %s", out)
	}
}

func TestRoot_コマンド一覧にcreditsが載る(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run(context.Background(), []string{"help"}, &stdout, &stderr)

	if !strings.Contains(stdout.String(), "credits") {
		t.Errorf("コマンド一覧に credits が無い: %s", stdout.String())
	}
}
