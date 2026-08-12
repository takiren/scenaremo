package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/takiren/scenaremo/internal/doctor"
	"github.com/takiren/scenaremo/internal/tts"
)

// doctorCommand は前提条件の診断コマンドを組み立てる。
func doctorCommand() command {
	return command{
		name:    "doctor",
		summary: "Node / pnpm / VOICEVOX / 書き込み権限を診断する",
		run:     runDoctor,
	}
}

// runDiagnostics は診断の本体。
// テストが実機の node や VOICEVOX の起動状態に左右されないよう、差し替えられる形にしている。
var runDiagnostics = doctor.Run

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// 既定のポートで動かしていない利用者にとっては、接続先を変えられないと診断結果そのものが誤りになる。
	voicevoxURL := fs.String("voicevox-url", tts.DefaultBaseURL(tts.EngineVoicevox), "VOICEVOX ENGINE の URL")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "使い方: scenaremo doctor [オプション]\n\n")
		fmt.Fprint(fs.Output(), "Node.js / pnpm / renderer の依存 / VOICEVOX ENGINE / 書き込み権限を診断します。\n")
		fmt.Fprint(fs.Output(), "対処が必要な項目があると終了コードは 1 になります。\n\nオプション:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		// 理由と使い方は flag が既に出している。
		return errReported
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("doctor は引数を取りません: %s", strings.Join(fs.Args(), " "))
	}

	report := runDiagnostics(ctx, doctor.Options{VoicevoxURL: *voicevoxURL})
	if _, err := io.WriteString(stdout, report.Text()); err != nil {
		return err
	}
	if !report.OK() {
		// 何を直せばよいかは診断結果に書いてあるので、ここでは終了コードだけを変える。
		// `scenaremo doctor && scenaremo render videos/ep01` のように繋げて使えるようにするため。
		return errReported
	}
	return nil
}
