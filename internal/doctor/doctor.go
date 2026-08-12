// Package doctor は scenaremo を動かすための前提条件を診断する。
//
// この診断の価値は「満たしているかどうか」ではなく「満たしていないとき何をすればよいか」にある
// （→ README「ロードマップ」）。開発者以外に配ることを見据えると、詰まった利用者が持っている情報は
// この出力だけになるためである。そのため Check は失敗したときに必ず次の一手 (Actions) を持ち、
// 判定できなかった場合も黙って握り潰さず StatusWarn として残す。
//
// 1 項目が失敗しても打ち切らない。Node も VOICEVOX も入っていない環境で 1 つずつ潰させると
// 「直す → 実行 → 次の失敗」を人が何度も往復することになり、全体像がいつまでも見えないためである。
// したがって Run はエラーを返さず、失敗はすべて Report の中へ畳み込む。
package doctor

import (
	"context"
	"os"
	"time"

	"github.com/takiren/scenaremo/internal/tts"
)

// DefaultTimeout は 1 項目あたりに待つ上限。
//
// 起動していないローカルのポートは即座に接続を拒否されるので待つ必要はないが、
// Docker や別マシンのエンジンを指すこともあるので数秒だけ余裕を持たせている。
// 診断は「待たされずに全体像が返ってくる」ことに意味があるため、これ以上長くはしない。
const DefaultTimeout = 5 * time.Second

// Status は 1 項目の診断結果。
type Status int

const (
	// StatusOK は前提条件を満たしていること。
	StatusOK Status = iota
	// StatusWarn は動作を妨げないが伝えておきたいこと。終了コードには影響させない。
	// 「判定できなかった」ことを OK と偽らず、かといって動くかもしれない環境を止めもしないための段。
	StatusWarn
	// StatusNG は前提条件を満たしておらず、対処しないと動かないこと。
	StatusNG
)

// Label は出力に使う表示名。
//
// 端末上の見た目の幅を揃えたいので、記号を含めて OK / NG は 2 文字、
// 全角の「注意」は 2 文字（幅 4）に対して前後の空白で調整する（→ report.go）。
func (s Status) Label() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusWarn:
		return "注意"
	case StatusNG:
		return "NG"
	default:
		return "??"
	}
}

// Check は診断 1 項目の結果。
type Check struct {
	// Name は項目名。利用者が README の「前提条件」と突き合わせられる言葉を使う。
	Name string

	// Status は判定。
	Status Status

	// Detail は分かったこと。成功ならバージョンやパス、失敗ならその理由を入れる。
	Detail string

	// Actions は次にやること。1 要素 1 手順で、失敗した項目には必ず 1 つ以上入れる。
	//
	// 「NG」とだけ伝えても利用者は動けない。ここがこのパッケージの本体であり、
	// 判定ロジックのほうが付随物だと考えてよい。
	Actions []string
}

// Report は診断全体の結果。
type Report struct {
	// Checks は診断した項目。診断した順に並ぶ。
	Checks []Check
}

// OK は対処が必要な項目が 1 つも無いことを返す。CLI の終了コードはこの値で決まる。
// StatusWarn は「動くかもしれない」状態なので、スクリプトを止めるほどではないと扱う。
func (r Report) OK() bool {
	return len(r.Failures()) == 0
}

// Failures は対処が必要な項目を返す。
func (r Report) Failures() []Check {
	var failed []Check
	for _, c := range r.Checks {
		if c.Status == StatusNG {
			failed = append(failed, c)
		}
	}
	return failed
}

// CommandRunner は外部コマンドを実行して標準出力（前後の空白を除いたもの）を返す。
//
// node / pnpm の有無とバージョンはこれで調べる。実物の exec を直に呼ぶと
// 「Node が入っていない環境」をテストで再現できないため、差し替え口として切り出している。
type CommandRunner func(ctx context.Context, name string, args ...string) (string, error)

// EnginePinger は音声合成エンジンへの疎通確認。戻り値はエンジンのバージョン文字列。
//
// 既定では internal/tts の Client.Ping を呼ぶ。tts 側が「未起動」と「エンジンが 200 以外を返した」を
// 区別できるエラーにしてくれるので、診断はその区別をそのまま案内の出し分けに使える。
type EnginePinger func(ctx context.Context) (string, error)

// Options は診断の設定。ゼロ値のまま使えて、そのときは実環境を対象に既定値で診断する。
type Options struct {
	// VoicevoxURL は疎通を確認するエンジンの baseURL。空なら VOICEVOX の既定値。
	VoicevoxURL string

	// WorkDir は書き込み権限を確かめるディレクトリ兼、renderer/ を探し始める場所。
	// 空ならカレントディレクトリ。生成物 (.scenaremo/ と out/) はここへ書かれる。
	WorkDir string

	// RendererDir は共有 Remotion プロジェクトの場所。
	// 空なら WorkDir から親をたどって探す（videos/ep01 のような作業中の場所から実行されるため）。
	RendererDir string

	// Timeout は 1 項目あたりの上限。0 なら DefaultTimeout。
	Timeout time.Duration

	// RunCommand は外部コマンドの実行。nil なら実物の exec を使う。
	RunCommand CommandRunner

	// Ping はエンジンへの疎通確認。nil なら VoicevoxURL 宛の tts クライアントを使う。
	Ping EnginePinger
}

// withDefaults は未設定の項目を実環境向けの既定値で埋めた Options を返す。
func (o Options) withDefaults() Options {
	if o.VoicevoxURL == "" {
		o.VoicevoxURL = tts.DefaultBaseURL(tts.EngineVoicevox)
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.WorkDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			// カレントディレクトリが取れない状況でも診断そのものは続ける。
			// "." のまま進めれば、書き込み権限の項目が実際の失敗として報告される。
			wd = "."
		}
		o.WorkDir = wd
	}
	if o.RunCommand == nil {
		o.RunCommand = execCommand
	}
	if o.Ping == nil {
		o.Ping = voicevoxPinger(o.VoicevoxURL, o.Timeout)
	}
	return o
}

// Run はすべての項目を診断して結果を返す。
//
// 途中で失敗しても最後まで続ける。並べる順は README「セットアップ」で利用者が用意する順
// （Node → pnpm → 依存 → VOICEVOX → 作業場所）に合わせてあり、上から順に潰せば動く状態になる。
func Run(ctx context.Context, opts Options) Report {
	opts = opts.withDefaults()
	return Report{Checks: []Check{
		checkNode(ctx, opts),
		checkPnpm(ctx, opts),
		checkRendererDeps(opts),
		checkVoicevox(ctx, opts),
		checkWritable(opts),
	}}
}
