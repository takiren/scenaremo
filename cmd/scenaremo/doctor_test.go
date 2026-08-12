package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/takiren/scenaremo/internal/doctor"
)

// stubDiagnostics は診断本体を差し替える。
// 実機の node や VOICEVOX の起動状態でテストの結果が変わらないようにするため。
func stubDiagnostics(t *testing.T, fn func(context.Context, doctor.Options) doctor.Report) {
	t.Helper()
	original := runDiagnostics
	runDiagnostics = fn
	t.Cleanup(func() { runDiagnostics = original })
}

func TestDoctor_すべて満たしていれば終了コード0(t *testing.T) {
	stubDiagnostics(t, func(context.Context, doctor.Options) doctor.Report {
		return doctor.Report{Checks: []doctor.Check{
			{Name: "Node.js", Status: doctor.StatusOK, Detail: "v22.11.0"},
			{Name: "VOICEVOX ENGINE", Status: doctor.StatusOK, Detail: "0.14.4"},
		}}
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"doctor"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Errorf("終了コードが 0 でない: %d", code)
	}
	if !strings.Contains(stdout.String(), "[ OK ] Node.js: v22.11.0") {
		t.Errorf("診断結果が標準出力に出ていない: %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("成功なのに標準エラー出力へ書いている: %s", stderr.String())
	}
}

func TestDoctor_要対応があれば終了コードが0以外(t *testing.T) {
	stubDiagnostics(t, func(context.Context, doctor.Options) doctor.Report {
		return doctor.Report{Checks: []doctor.Check{
			{Name: "Node.js", Status: doctor.StatusOK, Detail: "v22.11.0"},
			{
				Name:    "VOICEVOX ENGINE",
				Status:  doctor.StatusNG,
				Detail:  "http://127.0.0.1:50021 に接続できませんでした",
				Actions: []string{"VOICEVOX アプリを起動してください"},
			},
		}}
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"doctor"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Fatal("要対応があるのに終了コードが 0 になった")
	}
	// 失敗しても診断結果そのものは読めること
	out := stdout.String()
	for _, want := range []string{"[ NG ] VOICEVOX ENGINE", "→ VOICEVOX アプリを起動してください"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が含まれない: %s", want, out)
		}
	}
	// 診断結果で説明済みなので、うしろへ "scenaremo: ..." を重ねないこと
	if stderr.Len() != 0 {
		t.Errorf("標準エラー出力へ重ねて出している: %s", stderr.String())
	}
}

func TestDoctor_接続先を指定できる(t *testing.T) {
	var got doctor.Options
	stubDiagnostics(t, func(_ context.Context, opts doctor.Options) doctor.Report {
		got = opts
		return doctor.Report{}
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"doctor", "--voicevox-url=http://192.168.0.2:50021"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Fatalf("終了コードが 0 でない: %d (%s)", code, stderr.String())
	}
	if got.VoicevoxURL != "http://192.168.0.2:50021" {
		t.Errorf("指定した URL が渡っていない: %q", got.VoicevoxURL)
	}
}

func TestDoctor_既定の接続先はVOICEVOXの既定値(t *testing.T) {
	var got doctor.Options
	stubDiagnostics(t, func(_ context.Context, opts doctor.Options) doctor.Report {
		got = opts
		return doctor.Report{}
	})

	var stdout, stderr bytes.Buffer
	run(context.Background(), []string{"doctor"}, &stdout, &stderr)

	if got.VoicevoxURL != "http://127.0.0.1:50021" {
		t.Errorf("既定の接続先が違う: %q", got.VoicevoxURL)
	}
}

func TestDoctor_知らないオプションは失敗する(t *testing.T) {
	stubDiagnostics(t, func(context.Context, doctor.Options) doctor.Report {
		t.Error("引数の解釈に失敗したのに診断が走った")
		return doctor.Report{}
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"doctor", "--voicevox"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Error("知らないオプションなのに成功として終了した")
	}
	if !strings.Contains(stderr.String(), "voicevox-url") {
		t.Errorf("使い方が出ていない: %s", stderr.String())
	}
}

func TestDoctor_余分な引数は失敗する(t *testing.T) {
	stubDiagnostics(t, func(context.Context, doctor.Options) doctor.Report {
		t.Error("引数が多いのに診断が走った")
		return doctor.Report{}
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"doctor", "videos/ep01"}, &stdout, &stderr)

	if code == exitSuccess {
		t.Error("余分な引数なのに成功として終了した")
	}
	if !strings.Contains(stderr.String(), "videos/ep01") {
		t.Errorf("何が余分だったのか分からない: %s", stderr.String())
	}
}

func TestDoctor_helpは成功する(t *testing.T) {
	stubDiagnostics(t, func(context.Context, doctor.Options) doctor.Report {
		t.Error("--help なのに診断が走った")
		return doctor.Report{}
	})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"doctor", "--help"}, &stdout, &stderr)

	if code != exitSuccess {
		t.Errorf("--help の終了コードが 0 でない: %d", code)
	}
	if !strings.Contains(stderr.String(), "使い方: scenaremo doctor") {
		t.Errorf("使い方が出ていない: %s", stderr.String())
	}
}
