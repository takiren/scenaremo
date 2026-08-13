package build

import (
	"context"

	"github.com/takiren/scenaremo/internal/credits"
	"github.com/takiren/scenaremo/internal/project"
	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/script"
)

// CreditsOptions は Credits 1 回分の設定。
//
// Options を使い回さないのは、クレジットの集計に合成まわりの設定（キャッシュ・進捗・生成者）が
// 1 つも要らないため。同じ型で受けると「credits に --no-cache を渡したら何が起きるのか」に
// 答えられない状態が生まれるので、要らないものは型から消してある。
type CreditsOptions struct {
	// Dir は動画ディレクトリ。台本ファイルそのものを指してもよい（→ project.Resolve）。
	Dir string

	// VoicevoxURL は VOICEVOX ENGINE の接続先。空なら tts の既定値。
	VoicevoxURL string

	// Color は台本の検証エラーに色を付けるかどうか。出力先が端末かどうかは CLI が判断する。
	Color bool

	// NewEngine はエンジンを作る関数。nil なら実物の tts クライアントを作る。
	NewEngine EngineFactory
}

// CreditsResult は Credits の結果。
type CreditsResult struct {
	// Layout は使った置き場所。どの台本を読んだかはここから引ける。
	Layout *project.Layout

	// Credits は集計したクレジット。同じ台本なら props.json の credits と同じ値になる。
	Credits props.Credits
}

// Credits は台本を読み、実際に使われている話者のクレジットを集計する（→ issue #16）。
//
// 音声は合成しないし、props.json も書かない。エンジンへ問い合わせるのは話者一覧だけである。
// これが `scenaremo build` と分けて存在する理由そのもので、公開直前にクレジットだけ確かめたいときに
// 数分の合成を待たされるなら、確かめること自体が億劫になり、防ぎたかった表記漏れへ戻ってしまう。
//
// 集計は props.BuildCredits に任せる。ここで同じ計算を書き直すと、props.json に載るクレジットと
// `scenaremo credits` の出力が静かに食い違い、どちらを信じればよいのか利用者に判断させることになる。
//
// Run と同じくコマンドの中ではなくここに段取りを置いてあるのは、`scenaremo render`（→ issue #18）や
// クレジットシーンの挿入（→ issue #17）が同じ手順を必要とするため。
func Credits(ctx context.Context, opts CreditsOptions) (*CreditsResult, error) {
	layout, err := project.Resolve(opts.Dir)
	if err != nil {
		return nil, err
	}

	s, err := script.Load(layout.ScriptPath, script.WithColor(opts.Color))
	if err != nil {
		// 台本の検証エラー (*script.Error) はそれ自体が整形済みの報告なので、包まずにそのまま返す。
		// ここで文脈を足すと、CLI が「報告をそのまま出す」判断をするための型が見えにくくなる（→ Run）。
		return nil, err
	}

	// 開くのは台本で実際に使われているエンジンだけ。build と同じ関数を通すのは、
	// 「どのエンジンが要るか」の判断が 2 通りあると、build は通るのに credits だけ落ちる（またはその逆）が
	// 起きうるためである。
	engines, err := openEngines(s, Options{VoicevoxURL: opts.VoicevoxURL, NewEngine: opts.NewEngine})
	if err != nil {
		return nil, err
	}

	resolved, err := credits.Resolve(ctx, s, engines)
	if err != nil {
		return nil, err
	}

	collected, err := props.BuildCredits(s, resolved)
	if err != nil {
		return nil, err
	}

	return &CreditsResult{Layout: layout, Credits: collected}, nil
}
