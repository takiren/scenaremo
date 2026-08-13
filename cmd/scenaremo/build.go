package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/takiren/scenaremo/internal/build"
	"github.com/takiren/scenaremo/internal/progress"
	"github.com/takiren/scenaremo/internal/script"
	"github.com/takiren/scenaremo/internal/synth"
	"github.com/takiren/scenaremo/internal/tts"
)

// runBuild は build の本体。
// テストが実機の VOICEVOX や台本の有無に左右されないよう、差し替えられる形にしている。
var runBuild = build.Run

// progressReporter は進捗の通知先。
//
// 合成へ渡す synth.Reporter に End を足してある。合成が途中で失敗したときに、
// 書きかけの行を閉じてから失敗を報告するのはコマンド側の仕事だからである
// （合成そのものは、表示がどうなっているかを知らないし、知るべきでもない）。
type progressReporter interface {
	synth.Reporter
	End()
}

// newBuildCommand は台本から props.json までを作るコマンドを組み立てる。
func newBuildCommand() *cobra.Command {
	var (
		voicevoxURL string
		noCache     bool
		quiet       bool
	)

	cmd := &cobra.Command{
		Use:   "build <ディレクトリ>",
		Short: "音声を合成し、.scenaremo/props.json を出力する",
		Long: "台本を検証し、VOICEVOX で音声を合成して、レンダリングに必要な props.json を書き出します。\n" +
			"合成した音声は .scenaremo/audio/ に貯まり、2 回目以降は変えたセリフだけを合成し直します。\n\n" +
			"書き出した props.json のパスは標準出力へ、進捗は標準エラー出力へ出ます。",
		Example: "  scenaremo build videos/ep01\n" +
			"  scenaremo build videos/ep01 --no-cache",
		Args:                  exactlyOneDir,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 進捗は成果物ではないので標準エラーへ出す。標準出力は props.json のパス専用にしておき、
			// scenaremo render（→ issue #18）や利用者のスクリプトがそのまま受け取れるようにする。
			var reporter progressReporter = progress.Discard
			if !quiet {
				reporter = progress.New(cmd.ErrOrStderr())
			}

			res, err := runBuild(cmd.Context(), build.Options{
				Dir:         args[0],
				VoicevoxURL: voicevoxURL,
				NoCache:     noCache,
				// どの版が吐いた props.json なのかを、不具合の報告を受けたときに特定できるようにする。
				GeneratedBy: "scenaremo " + version,
				Color:       colorize(cmd.ErrOrStderr()),
				Reporter:    reporter,
			})
			if err != nil {
				// 合成の途中で落ちた場合、進捗は行を書きかけのまま止まっている。
				// 閉じてから報告しないと、失敗の理由がセリフの続きに繋がって読めなくなる。
				reporter.End()
				return reportBuildError(cmd.ErrOrStderr(), err)
			}

			// 成果物のパスは標準出力へ 1 行だけ。飾りを混ぜると次の工程がそのまま使えなくなる。
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), res.Layout.PropsPath); err != nil {
				return err
			}
			printSummary(cmd.ErrOrStderr(), res)
			return nil
		},
	}

	cmd.Flags().StringVar(&voicevoxURL, "voicevox-url", tts.DefaultBaseURL(tts.EngineVoicevox), "VOICEVOX ENGINE の URL")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "キャッシュを使わず、すべてのセリフを合成し直す")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "進捗を表示しない")

	return cmd
}

// exactlyOneDir は動画ディレクトリがちょうど 1 つ指定されていることを確かめる。
//
// cobra.ExactArgs(1) ではなく自前で持つのは、メッセージを日本語にするためと、
// これが usage を添えるべき誤りだと型で示すため（→ usageError）。
func exactlyOneDir(_ *cobra.Command, args []string) error {
	switch {
	case len(args) == 0:
		return &usageError{errors.New("動画ディレクトリを指定してください (例: scenaremo build videos/ep01)")}
	case len(args) > 1:
		// 複数を一度に build できるようには、まだしていない。黙って最初の 1 つだけを処理すると、
		// 残りが作られていないことに利用者が気づけない。
		return &usageError{fmt.Errorf("動画ディレクトリは1つだけ指定してください: %s", strings.Join(args, " "))}
	}
	return nil
}

// reportBuildError は build の失敗を利用者へ伝える。
//
// 台本の検証エラーは、どこがどう悪いかを行番号とソース片つきで並べた報告そのものである
// （→ script.Error）。頭に "scenaremo: " を足すと 1 行目だけが別の書式になって読みにくいので、
// ここで出し切って errReported を返す。それ以外は run に任せる。
func reportBuildError(w io.Writer, err error) error {
	var scriptErr *script.Error
	if !errors.As(err, &scriptErr) {
		return err
	}
	if _, writeErr := fmt.Fprintln(w, scriptErr.Error()); writeErr != nil {
		return err
	}
	return errReported
}

// printSummary は build の結果を 1 行にまとめて書く。
//
// 合成とキャッシュの内訳は progress が出しているので、ここに要るのは「どれだけの長さで出来たか」だけ。
// 書き出し先のパスを重ねて書かないのは、それを直前に標準出力へ出しているためである
// （端末では 2 行に同じパスが並び、書き損じのように見える）。
func printSummary(w io.Writer, res *build.Result) {
	seconds := 0.0
	if fps := res.Props.Meta.FPS; fps > 0 {
		seconds = float64(res.Props.Meta.DurationInFrames) / float64(fps)
	}
	// 書き出しに失敗しても、報告先がもう無いので握り潰すほかない（→ main.go の同じ扱い）。
	_, _ = fmt.Fprintf(w, "props.json を書き出しました (尺 %.1f 秒 / %d フレーム)\n",
		seconds, res.Props.Meta.DurationInFrames)
}

// colorize は書き出し先が端末なら色を付けてよいと判断する。
// バッファやファイルへ出すときに制御文字が混ざらないようにするため（→ script.ShouldColorize）。
func colorize(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && script.ShouldColorize(f)
}
