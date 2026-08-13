// Package build は台本から props.json までを一気通貫で作る。
//
// このパッケージ自身は何も計算しない。台本を読む (internal/script)、音声を合成する (internal/synth)、
// クレジットを解決する (internal/credits)、タイムラインを組む (internal/props) はすべて別にあり、
// ここが持っているのは「どの順で呼ぶか」と「失敗したらどこで止めるか」だけである。
//
// 段取りだけを持つ薄い層をあえて置くのは、`scenaremo build` と `scenaremo render`（→ issue #18）が
// 同じ手順を必要とするためで、cobra のコマンドの中に手順を書くと render がそれを再現することになる。
package build

import (
	"context"
	"errors"
	"fmt"

	"github.com/takiren/scenaremo/internal/cache"
	"github.com/takiren/scenaremo/internal/credits"
	"github.com/takiren/scenaremo/internal/project"
	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/script"
	"github.com/takiren/scenaremo/internal/synth"
	"github.com/takiren/scenaremo/internal/tts"
)

// Engine は build が要求するエンジンの口。*tts.Client が満たす。
//
// 合成 (tts.Engine) と話者一覧 (Speakers) の両方を求めるのは、build がその両方を必要とするため。
// クレジットの表記に使う話者名はエンジンにしか無く、表記漏れは利用者の事故に直結するので、
// 「合成はできるが名前は分からない」相手をここで受け付けるわけにはいかない。
type Engine interface {
	tts.Engine
	Speakers(ctx context.Context) ([]tts.Speaker, error)
}

// EngineFactory はエンジン種別と接続先からクライアントを作る。
//
// これが build の唯一の差し替え口である。ここさえ差し替えれば、台本の読み込みから
// キャッシュへの書き出し、props.json の出力までを実物のまま試せる（→ build_test.go）。
// 段取りを 1 つずつ関数で差し替えられるようにすると、テストが通っても
// 「実際に繋いだら動かない」が起きるので、そうはしていない。
type EngineFactory func(kind tts.EngineKind, baseURL string) (Engine, error)

// Options は build 1 回分の設定。
type Options struct {
	// Dir は動画ディレクトリ。台本ファイルそのものを指してもよい（→ project.Resolve）。
	Dir string

	// VoicevoxURL は VOICEVOX ENGINE の接続先。空なら tts の既定値。
	VoicevoxURL string

	// NoCache は既存のキャッシュを読まずに合成し直すかどうか。
	NoCache bool

	// GeneratedBy は props.json の $generatedBy に載せる生成者。
	GeneratedBy string

	// Color は台本の検証エラーに色を付けるかどうか。出力先が端末かどうかは CLI が判断する。
	Color bool

	// Reporter は合成の進み具合の通知先。nil なら何も通知しない。
	Reporter synth.Reporter

	// Workers は音声合成の並列数。
	Workers int

	// NewEngine はエンジンを作る関数。nil なら実物の tts クライアントを作る。
	NewEngine EngineFactory
}

// Result は build の結果。
type Result struct {
	// Layout は使った置き場所。props.json のパスはここから引ける。
	Layout *project.Layout

	// Props は書き出した props.json の中身。尺やクレジットを CLI が表示するのに使う。
	Props *props.Props

	// Synthesized はエンジンを呼んだ件数。
	Synthesized int

	// Cached はキャッシュで済んだ件数。
	Cached int
}

// Run は台本を読み、音声を合成し、props.json を書き出す。
//
// 失敗したときは props.json を書かない。中途半端な props.json を残すと、
// 次に renderer が読んだときの失敗が「build が途中で落ちた」ではなく
// 「props.json の内容がおかしい」として現れ、原因から遠ざかるためである。
func Run(ctx context.Context, opts Options) (*Result, error) {
	layout, err := project.Resolve(opts.Dir)
	if err != nil {
		return nil, err
	}

	s, err := script.Load(layout.ScriptPath, script.WithColor(opts.Color))
	if err != nil {
		// 台本の検証エラー (*script.Error) はそれ自体が整形済みの報告なので、包まずにそのまま返す。
		// ここで文脈を足すと、CLI が「報告をそのまま出す」判断をするための型が見えにくくなる。
		return nil, err
	}

	engines, err := openEngines(s, opts)
	if err != nil {
		return nil, err
	}

	// クレジットの解決を合成より先に行う。
	//
	// 合成は台本 1 本で数分かかる。styleId の書き間違いやエンジンの不調で結局失敗するのなら、
	// 全セリフを喋らせ終えたあとではなく、始まってすぐに落ちなければ利用者の時間が無駄になる。
	// 同じ理由で、これはエンジンへ最初に触る処理でもあり、疎通確認を兼ねている。
	speakerCredits, err := credits.Resolve(ctx, s, engines)
	if err != nil {
		return nil, err
	}

	audio, err := synth.Run(ctx, synth.Input{
		Script:  s,
		Engines: engines,
		// wav の置き場所とキャッシュは同じディレクトリである。キャッシュは再合成を避けるための
		// 控えであると同時に、props.json が指す現物でもある（→ synth.DefaultRelAudioDir）。
		Store:       cache.NewStore(layout.AudioDir),
		RelAudioDir: layout.RelAudioDir(),
		NoCache:     opts.NoCache,
		Workers:     opts.Workers,
		Reporter:    opts.Reporter,
	})
	if err != nil {
		return nil, err
	}

	p, err := props.Build(props.Input{
		Script:      s,
		Audio:       audio.Audio,
		Credits:     speakerCredits,
		GeneratedBy: opts.GeneratedBy,
	})
	if err != nil {
		return nil, err
	}
	if err := props.WriteFile(layout.PropsPath, p); err != nil {
		return nil, err
	}

	return &Result{
		Layout:      layout,
		Props:       p,
		Synthesized: audio.Synthesized,
		Cached:      audio.Cached,
	}, nil
}

// engineSet は種別ごとに開いたエンジン。
//
// synth.EngineResolver と credits.ListerResolver の両方をこれ 1 つで満たす。
// 合成とクレジットで別々にクライアントを作ると、同じエンジンへ 2 本の接続を張ったうえに
// 話者一覧も 2 回問い合わせることになる。
type engineSet map[tts.EngineKind]Engine

// Engine は synth.EngineResolver を満たす。
func (s engineSet) Engine(kind tts.EngineKind) (tts.Engine, error) {
	engine, ok := s[kind]
	if !ok {
		return nil, unopenedEngineError(kind)
	}
	return engine, nil
}

// Lister は credits.ListerResolver を満たす。
func (s engineSet) Lister(kind tts.EngineKind) (credits.Lister, error) {
	engine, ok := s[kind]
	if !ok {
		return nil, unopenedEngineError(kind)
	}
	return engine, nil
}

// unopenedEngineError は台本を読んで開いたはずのエンジンが見つからないことを伝える。
//
// 台本に現れる種別はすべて開いてから合成に入るので、これは配線の誤りでしか起きない。
// 利用者が台本を直しても解決しないため、そう伝える。
func unopenedEngineError(kind tts.EngineKind) error {
	return fmt.Errorf("エンジン %q が用意されていません（scenaremo の不具合です。issue で報告してください）", kind)
}

// openEngines は台本で実際に使われているエンジンだけを、種別ごとに 1 つずつ開く。
//
// 台本に定義してあるだけで使っていない話者のために接続先を用意する必要はない。
// 使いもしないエンジンの baseUrl の不備で build が止まるのは、利用者から見て理不尽である。
func openEngines(s *script.Script, opts Options) (engineSet, error) {
	newEngine := opts.NewEngine
	if newEngine == nil {
		newEngine = defaultEngine
	}

	engines := engineSet{}
	for _, scene := range s.Scenes {
		for _, line := range scene.Lines {
			speaker, ok := s.Speakers[line.Speaker]
			if !ok {
				// 未定義の話者は synth と credits が台本の位置つきで報告する。
				// ここで先回りして別の文言を出すと、同じ誤りに 2 通りのメッセージが生まれる。
				continue
			}
			kind := tts.EngineKind(speaker.Engine)
			if _, opened := engines[kind]; opened {
				continue
			}

			engine, err := newEngine(kind, baseURLFor(kind, opts))
			if err != nil {
				return nil, fmt.Errorf("%s へ繋ぐ準備ができませんでした: %w", tts.DisplayName(kind), err)
			}
			if engine == nil {
				return nil, fmt.Errorf("%s のクライアントを作れませんでした"+
					"（エンジンが nil で返りました。scenaremo の不具合です。issue で報告してください）",
					tts.DisplayName(kind))
			}
			engines[kind] = engine
		}
	}

	if len(engines) == 0 {
		// 台本が検証を通っていればセリフは 1 つ以上あり、話者も定義されている。
		// それでもここへ来るなら、この先の合成でまともなエラーが出せる状態ではない。
		return nil, errors.New("音声を合成する話者が台本にありません。scenes[].lines[].speaker と speakers を確認してください")
	}
	return engines, nil
}

// baseURLFor は種別ごとの接続先を返す。指定が無ければ空文字列を返し、既定値は tts に任せる。
func baseURLFor(kind tts.EngineKind, opts Options) string {
	if kind == tts.EngineVoicevox {
		return opts.VoicevoxURL
	}
	return ""
}

// defaultEngine は実物の tts クライアントを作る。
func defaultEngine(kind tts.EngineKind, baseURL string) (Engine, error) {
	// baseURL が空なら WithBaseURL は何もしないので、種別ごとの既定値がそのまま使われる。
	return tts.New(kind, tts.WithBaseURL(baseURL))
}

var (
	_ synth.EngineResolver   = engineSet(nil)
	_ credits.ListerResolver = engineSet(nil)
	// 実物のクライアントが build の求める口を満たすこと。
	// 満たさなくなったら、CLI の配線ではなくここで気付けるようにしておく。
	_ Engine = (*tts.Client)(nil)
)
