package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/takiren/scenaremo/internal/tts"
)

// RequiredNodeMajor は必要な Node.js の最低メジャーバージョン（→ README「前提条件」）。
const RequiredNodeMajor = 20

// ErrNotInstalled はコマンドが PATH に見つからなかったことを表す。
//
// exec.ErrNotFound をそのまま流さないのは、CommandRunner を差し替えたときにも
// 「入っていない」を同じ判定で表現できるようにするため。
var ErrNotInstalled = errors.New("コマンドが見つかりません")

// execCommand は実物の外部コマンドを実行する既定の CommandRunner。
func execCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%s: %w", name, ErrNotInstalled)
		}
		// 失敗の理由はたいてい標準エラー出力にある。診断の目的からして、
		// ここを捨てると「実行できませんでした」としか言えなくなる。
		if msg := firstLine(stderr.String()); msg != "" {
			return "", fmt.Errorf("%s の実行に失敗しました: %w (%s)", name, err, msg)
		}
		return "", fmt.Errorf("%s の実行に失敗しました: %w", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// voicevoxPinger は baseURL 宛の tts クライアントを使う既定の EnginePinger を返す。
//
// クライアントの生成に失敗する（baseUrl が URL として壊れている）ことがあるので、
// 生成は呼ばれた時点で行い、失敗もそのまま診断結果の理由として使えるようにしている。
func voicevoxPinger(baseURL string, timeout time.Duration) EnginePinger {
	return func(ctx context.Context) (string, error) {
		c, err := tts.New(tts.EngineVoicevox, tts.WithBaseURL(baseURL), tts.WithTimeout(timeout))
		if err != nil {
			return "", err
		}
		return c.Ping(ctx)
	}
}

// checkNode は Node.js のバージョンを診断する。
func checkNode(ctx context.Context, opts Options) Check {
	const name = "Node.js"

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	out, err := opts.RunCommand(ctx, "node", "--version")
	if err != nil {
		if errors.Is(err, ErrNotInstalled) {
			return Check{
				Name:   name,
				Status: StatusNG,
				Detail: "見つかりません（PATH に node がありません）",
				Actions: []string{
					fmt.Sprintf("Node.js %d 以上を入れてください: https://nodejs.org/", RequiredNodeMajor),
					"Homebrew なら brew install node、mise なら mise use -g node@22 でも入ります",
					"入れたはずなのに見つからないときは、ターミナルを開き直してから再実行してください",
				},
			}
		}
		return Check{
			Name:   name,
			Status: StatusNG,
			Detail: err.Error(),
			Actions: []string{
				"node --version を手で実行して、同じエラーが出るか確かめてください",
				fmt.Sprintf("Node.js が壊れている場合は %d 以上を入れ直してください: https://nodejs.org/", RequiredNodeMajor),
			},
		}
	}

	major, ok := parseNodeMajor(out)
	if !ok {
		return Check{
			Name:   name,
			Status: StatusWarn,
			Detail: fmt.Sprintf("バージョンを判別できませんでした（node --version の出力: %q）", out),
			Actions: []string{
				fmt.Sprintf("Node.js %d 以上であることを自分で確かめてください", RequiredNodeMajor),
			},
		}
	}
	if major < RequiredNodeMajor {
		return Check{
			Name:   name,
			Status: StatusNG,
			Detail: fmt.Sprintf("%s（%d 以上が必要です）", out, RequiredNodeMajor),
			Actions: []string{
				fmt.Sprintf("Node.js を %d 以上へ更新してください。Remotion がそれ未満では動きません", RequiredNodeMajor),
				"mise use -g node@22 / nvm install 22 / brew upgrade node など、普段使っている方法で構いません",
			},
		}
	}
	return Check{Name: name, Status: StatusOK, Detail: out}
}

// checkPnpm は pnpm の有無を診断する。
func checkPnpm(ctx context.Context, opts Options) Check {
	const name = "pnpm"

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	out, err := opts.RunCommand(ctx, "pnpm", "--version")
	if err != nil {
		if errors.Is(err, ErrNotInstalled) {
			return Check{
				Name:   name,
				Status: StatusNG,
				Detail: "見つかりません（PATH に pnpm がありません）",
				Actions: []string{
					"corepack enable pnpm を実行してください。Node.js に同梱の corepack が pnpm を用意します",
					"うまくいかないときは npm install -g pnpm でも構いません",
				},
			}
		}
		return Check{
			Name:   name,
			Status: StatusNG,
			Detail: err.Error(),
			Actions: []string{
				"pnpm --version を手で実行して、同じエラーが出るか確かめてください",
				"corepack enable pnpm で入れ直せます",
			},
		}
	}
	return Check{Name: name, Status: StatusOK, Detail: out}
}

// checkRendererDeps は共有 Remotion プロジェクトの依存が入っているかを診断する。
//
// pnpm の有無とは別項目にしている。pnpm があっても pnpm install を忘れている状態は普通にあり、
// そのとき「pnpm は OK」だけを見せられても利用者は次の一手にたどり着けないためである。
func checkRendererDeps(opts Options) Check {
	const name = "renderer の依存"

	dir := opts.RendererDir
	if dir == "" {
		found, ok := findRendererDir(opts.WorkDir)
		if !ok {
			return Check{
				Name:   name,
				Status: StatusNG,
				Detail: fmt.Sprintf("renderer/ が見つかりません（%s から親をたどって探しました）", opts.WorkDir),
				Actions: []string{
					"scenaremo のリポジトリの中（renderer/ がある場所か、その下）へ移動して実行してください",
					"renderer/ は動画を何本作っても 1 つだけの共有 Remotion プロジェクトです",
				},
			}
		}
		dir = found
	}

	install := fmt.Sprintf("pnpm install --dir %s", displayPath(dir))
	modules := filepath.Join(dir, "node_modules")
	if _, err := os.Stat(modules); err != nil {
		return Check{
			Name:   name,
			Status: StatusNG,
			Detail: fmt.Sprintf("%s がありません", displayPath(modules)),
			Actions: []string{
				fmt.Sprintf("%s を実行してください", install),
				"依存は共有レンダラに 1 つだけなので、この導入は動画を何本作っても 1 回で済みます",
			},
		}
	}

	// node_modules があるだけでは足りない。install を途中で中断した木も残るため、
	// 実際に使う remotion まで入っているかを確かめる。
	version, err := readPackageVersion(filepath.Join(modules, "remotion", "package.json"))
	if err != nil {
		return Check{
			Name:   name,
			Status: StatusNG,
			Detail: fmt.Sprintf("%s はありますが remotion が入っていません", displayPath(modules)),
			Actions: []string{
				fmt.Sprintf("%s をもう一度実行してください（前回の導入が途中で止まった可能性があります）", install),
			},
		}
	}
	return Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf("導入済み（remotion %s, %s）", version, displayPath(dir))}
}

// checkVoicevox は音声合成エンジンへの疎通とバージョンを診断する。
//
// 最も頻度の高い失敗が「エンジンを起動していない」なので、案内はここを一番厚くしてある。
func checkVoicevox(ctx context.Context, opts Options) Check {
	const name = "VOICEVOX ENGINE"

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	version, err := opts.Ping(ctx)
	if err == nil {
		if version == "" {
			// 疎通はしたがバージョンが読めない。互換エンジンでも起こりうるので止めはしない。
			return Check{
				Name:   name,
				Status: StatusWarn,
				Detail: fmt.Sprintf("%s に接続できましたが、バージョンを取得できませんでした", opts.VoicevoxURL),
				Actions: []string{
					"VOICEVOX 互換の別のエンジンを指している可能性があります。合成が通るかは scenaremo build で確かめてください",
				},
			}
		}
		return Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf("%s（%s）", version, opts.VoicevoxURL)}
	}

	var unavailable *tts.EngineUnavailableError
	var apiErr *tts.APIError

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return Check{
			Name:   name,
			Status: StatusNG,
			Detail: fmt.Sprintf("%s が %s 以内に応答しませんでした", opts.VoicevoxURL, opts.Timeout),
			Actions: []string{
				"起動直後は初期化に時間がかかることがあります。少し待ってからもう一度実行してください",
				"待っても応答しないときはエンジンを再起動してください",
				overrideURLAction(),
			},
		}

	case errors.As(err, &unavailable):
		return Check{
			Name:   name,
			Status: StatusNG,
			Detail: fmt.Sprintf("%s に接続できませんでした（起動していないようです）", opts.VoicevoxURL),
			// 起動方法を 1 つに決め打ちしないのは、アプリ版・エンジン単体・Docker のどれを使っているかで
			// やることが変わるため。利用者は自分の環境に当てはまる行を 1 つ選べばよい。
			Actions: []string{
				"VOICEVOX アプリを起動してください。アプリを開いている間だけエンジンも動きます",
				"エンジン単体で使っている場合は VOICEVOX ENGINE の run（Windows は run.exe）を実行してください",
				"Docker なら docker run --rm -p 50021:50021 voicevox/voicevox_engine:cpu-latest でも起動できます",
				fmt.Sprintf("起動できたかどうかは、ブラウザで %s/docs が開けるかで確かめられます", opts.VoicevoxURL),
				overrideURLAction(),
			},
		}

	case errors.As(err, &apiErr):
		return Check{
			Name:   name,
			Status: StatusNG,
			Detail: fmt.Sprintf("%s へは繋がりましたが %s が HTTP %d を返しました", opts.VoicevoxURL, apiErr.Endpoint, apiErr.StatusCode),
			Actions: []string{
				fmt.Sprintf("%s で VOICEVOX 以外のアプリが待ち受けていないか確認してください", opts.VoicevoxURL),
				"VOICEVOX ENGINE が古いと /version を返さないことがあります。最新版へ更新してください",
				overrideURLAction(),
			},
		}

	default:
		return Check{
			Name:   name,
			Status: StatusNG,
			Detail: err.Error(),
			Actions: []string{
				fmt.Sprintf("%s が VOICEVOX ENGINE の URL として正しいか確認してください", opts.VoicevoxURL),
				overrideURLAction(),
			},
		}
	}
}

// overrideURLAction は接続先を変える方法の案内。
// 既定のポートで動かしていない利用者にとっては、これが無いと診断結果そのものが誤りになる。
func overrideURLAction() string {
	return "別のポートやマシンで動かしている場合は scenaremo doctor --voicevox-url=http://<ホスト>:<ポート> を指定してください"
}

// checkWritable は作業ディレクトリへ書き込めるかを診断する。
//
// 実際に一時ファイルを作って書いてみる。os.Stat のパーミッションを読むだけでは、
// 読み取り専用マウントや共有フォルダのように「見た目は書けるが書けない」場所を見逃すため。
func checkWritable(opts Options) Check {
	const name = "書き込み権限"

	f, err := os.CreateTemp(opts.WorkDir, ".scenaremo-doctor-*")
	if err != nil {
		return Check{Name: name, Status: StatusNG, Detail: err.Error(), Actions: writableActions(opts.WorkDir)}
	}
	path := f.Name()
	_, writeErr := f.WriteString("scenaremo doctor")
	closeErr := f.Close()
	// 診断のためだけに作ったファイルなので、判定より先に必ず片付ける。
	_ = os.Remove(path)

	if err := errors.Join(writeErr, closeErr); err != nil {
		return Check{Name: name, Status: StatusNG, Detail: err.Error(), Actions: writableActions(opts.WorkDir)}
	}
	return Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf("%s に書き込めます", displayPath(opts.WorkDir))}
}

func writableActions(dir string) []string {
	return []string{
		fmt.Sprintf("%s に書き込む権限があるか確認してください", displayPath(dir)),
		"音声と props.json は .scenaremo/ へ、動画は out/ へ書き出されます",
		"書き込めない場所で作業している場合は、書き込めるディレクトリへ移動して実行してください",
	}
}

// findRendererDir は start から親をたどって renderer/ を探す。
//
// videos/ep01 のような作業中のディレクトリから doctor を実行しても診断できるようにするため。
// package.json の有無まで見るのは、たまたま renderer という名前のディレクトリを掴まないようにするため。
func findRendererDir(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		candidate := filepath.Join(dir, "renderer")
		if _, err := os.Stat(filepath.Join(candidate, "package.json")); err == nil {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// parseNodeMajor は node --version の出力 (v22.14.0) からメジャーバージョンを取り出す。
func parseNodeMajor(out string) (int, bool) {
	s := strings.TrimSpace(out)
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, ".-+ "); i >= 0 {
		s = s[:i]
	}
	major, err := strconv.Atoi(s)
	if err != nil || major <= 0 {
		return 0, false
	}
	return major, true
}

// readPackageVersion は package.json の version を読む。
func readPackageVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", err
	}
	if pkg.Version == "" {
		return "", fmt.Errorf("%s に version がありません", path)
	}
	return pkg.Version, nil
}

// displayPath はパスを人が読みやすい形にする。
// カレントディレクトリの下なら相対パスにするのは、案内に出てくるコマンド
// （pnpm install --dir renderer）をそのまま貼り付けて実行できるようにするため。
func displayPath(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(wd, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	if rel == "." {
		return path
	}
	return rel
}

// firstLine は複数行の出力を 1 行へ丸める。エラーメッセージへ載せるため。
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
