// Package credits は台本の話者エイリアスを、クレジット表記に必要な話者情報へ解決する。
//
// VOICEVOX は音声ライブラリごとのクレジット表記を規約で求めており、表記漏れは利用者の事故に直結する。
// props.Build は「使ったすべての話者の名前が揃うまで props.json を作らない」という作りになっているので、
// その名前を用意するのがこのパッケージの役目である。
//
// props から切り離してあるのは、名前の解決だけがエンジンへの問い合わせ（= ネットワーク）を必要とするため。
// props.Build に取り込むと、props.json の組み立てまでエンジンを起動しないとテストできなくなる
// （→ props.SpeakerCredit のコメント）。
package credits

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/script"
	"github.com/takiren/scenaremo/internal/tts"
)

// Lister は話者一覧を取得できるエンジン。tts.SpeakerLister（*tts.Client）がそのまま満たす。
//
// tts の型をそのまま使わず同じ形の口をここに置くのは、使う側でインターフェースを定義するという
// Go の流儀に従ったもの。credits が要るのは話者一覧だけなので、合成も疎通確認もできない相手
// （テストの偽物や、話者一覧だけを持つ将来のクラウド TTS）をそのまま渡せる。
type Lister interface {
	Speakers(ctx context.Context) ([]tts.Speaker, error)
}

// ListerResolver はエンジン種別から Lister を引く。
//
// 台本は話者ごとに engine を持てるため、どのエンジンが要るかは台本を読むまで決まらない。
// Lister を直接受け取る形にすると呼び出し側が全エンジンぶんのクライアントを先に作ることになり、
// 使いもしないエンジンの baseUrl の不備で build が止まってしまう。
type ListerResolver interface {
	Lister(kind tts.EngineKind) (Lister, error)
}

// Listers は種別から Lister への素朴な対応表。ListerResolver を満たす。
//
//	credits.Resolve(ctx, s, credits.Listers{tts.EngineVoicevox: client})
type Listers map[tts.EngineKind]Lister

var _ ListerResolver = Listers(nil)

// Lister は対応表から kind のエンジンを引く。
// 値が nil のときも「無い」として扱う。呼び出し側は取れた Lister をそのまま使えるべきなので、
// ここで nil を返すと結局その先で nil 参照になる。
func (l Listers) Lister(kind tts.EngineKind) (Lister, error) {
	lister, ok := l[kind]
	if !ok || lister == nil {
		return nil, fmt.Errorf("%s へ話者一覧を問い合わせる先が設定されていません。台本の speakers[].engine を確認してください%s",
			tts.DisplayName(kind), availableHint(l))
	}
	return lister, nil
}

// availableHint は対応表にあるエンジンを並べた案内を作る。
// 打ち間違いのときに正しい綴りをその場で示せるようにするため（→ script.speakerHint と同じ考え）。
func availableHint(l Listers) string {
	if len(l) == 0 {
		return ""
	}
	kinds := make([]string, 0, len(l))
	for kind := range l {
		kinds = append(kinds, string(kind))
	}
	slices.Sort(kinds)
	return "（問い合わせできるエンジン: " + strings.Join(kinds, " / ") + "）"
}

// Resolve は台本で実際に使われている話者エイリアスについて、props.Build が要求するクレジット情報を返す。
//
// 解決するのは lines[].speaker に現れたエイリアスだけである。台本に定義してあるだけで使っていない話者は、
// クレジットにも載らないのだから、そのためにエンジンへ問い合わせて失敗する理由がない。
//
// 戻り値はそのまま props.Input.Credits へ渡せる。
func Resolve(ctx context.Context, s *script.Script, r ListerResolver) (map[string]props.SpeakerCredit, error) {
	if s == nil {
		return nil, errors.New("台本がありません")
	}
	if r == nil {
		return nil, errors.New("話者一覧の問い合わせ先がありません")
	}
	// 既定値を埋めてから読む。これで line.Speaker と speaker.Engine が必ず入っている状態になる。
	// Parse を通っていれば済んでいるが、冪等なので二度呼んでも変わらない。
	script.ApplyDefaults(s)

	resolved := make(map[string]props.SpeakerCredit)
	// 問い合わせの結果はエンジン種別ごとに控えて使い回す。セリフごとに問い合わせると
	// build が遅くなるうえ、同じ答えを返すだけのエンジンに無駄な負荷をかける。
	listed := make(map[tts.EngineKind][]tts.Speaker)

	// 台本での登場順に見る。複数の話者が解決できないとき、先に直すべきもの（台本の上にあるもの）から
	// 報告されるようにするため。
	for i, scene := range s.Scenes {
		for j, line := range scene.Lines {
			if _, done := resolved[line.Speaker]; done {
				continue
			}
			speaker, ok := s.Speakers[line.Speaker]
			if !ok {
				return nil, undefinedSpeakerError(i, j, line.Speaker)
			}
			// 打ち切りはここで見る。問い合わせは種別ごとに 1 回しか起きないので、
			// エンジン側の ctx 確認に任せると 2 人目以降は打ち切りに気づけない。
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("クレジットに載せる話者の解決を中断しました: %w", err)
			}

			kind := tts.EngineKind(speaker.Engine)
			speakers, err := speakersOf(ctx, r, kind, listed)
			if err != nil {
				return nil, err
			}
			credit, err := creditOf(speakers, kind, line.Speaker, speaker.StyleID)
			if err != nil {
				return nil, err
			}
			resolved[line.Speaker] = credit
		}
	}
	return resolved, nil
}

// speakersOf はエンジン種別の話者一覧を返す。取得済みなら listed の内容を使い、
// 初めての種別だけ問い合わせる。
func speakersOf(ctx context.Context, r ListerResolver, kind tts.EngineKind, listed map[tts.EngineKind][]tts.Speaker) ([]tts.Speaker, error) {
	if speakers, ok := listed[kind]; ok {
		return speakers, nil
	}

	lister, err := r.Lister(kind)
	if err != nil {
		return nil, err
	}
	if lister == nil {
		// ListerResolver の実装が nil を返してくることまでは型で防げない。
		// ここで弾いておかないと、この直後の Speakers 呼び出しが nil 参照で落ちる。
		return nil, fmt.Errorf("%s へ話者一覧を問い合わせる先が設定されていません", tts.DisplayName(kind))
	}

	speakers, err := lister.Speakers(ctx)
	if err != nil {
		// エンジンが起動していないときの案内は tts.EngineUnavailableError が持っている。
		// ここで文面を作り直すとその案内を潰してしまうので、%w で包むだけにする。
		return nil, fmt.Errorf("%s から話者一覧を取得できませんでした（クレジット表記に必要です）: %w",
			tts.DisplayName(kind), err)
	}

	listed[kind] = speakers
	return speakers, nil
}

// creditOf は話者一覧から styleID に当たる話者を探し、クレジット情報へ直す。
//
// スタイルではなく話者（キャラクター）を返すのは、規約が求めるクレジットが音声ライブラリ単位だから。
// 同じ話者のノーマルとあまあまを使い分けても、書くべきクレジットは 1 つになる（→ props.BuildCredits）。
func creditOf(speakers []tts.Speaker, kind tts.EngineKind, alias string, styleID int) (props.SpeakerCredit, error) {
	for _, sp := range speakers {
		for _, style := range sp.Styles {
			if style.ID != styleID {
				continue
			}
			if sp.Name == "" {
				// props.Build は名前の無いクレジットを受け付けない。ここで気づいておかないと、
				// 「話者は見つかったのにクレジットが作れない」という分かりにくい失敗になる。
				return props.SpeakerCredit{}, fmt.Errorf(
					"%s が styleId %d の話者の名前を返しませんでした。エンジンのバージョンが古い可能性があります",
					tts.DisplayName(kind), styleID)
			}
			return props.SpeakerCredit{Name: sp.Name, UUID: sp.SpeakerUUID}, nil
		}
	}
	return props.SpeakerCredit{}, fmt.Errorf(
		"話者エイリアス %q の styleId %d に当たる話者が %s に見つかりません。`scenaremo speakers` で一覧を確認してください",
		alias, styleID, tts.DisplayName(kind))
}

// undefinedSpeakerError は lines[].speaker を speakers から引けなかったことを伝える。
//
// 台本を読み込んだ経路によっては script.Validate を通っていないことがあるため、ここでも同じ誤りを扱う。
// 場所 (scenes[i].lines[j]) を添えるのは、同じエイリアスが何度も出てくる台本で
// どの行を直せばよいのかを示すため。
func undefinedSpeakerError(scene, line int, alias string) error {
	if alias == "" {
		return fmt.Errorf("scenes[%d].lines[%d]: 話者が決まりません。このセリフに speaker を書くか、defaults.speaker を設定してください",
			scene, line)
	}
	return fmt.Errorf("scenes[%d].lines[%d]: 話者 %q が speakers に定義されていません。speakers に定義するか、綴りを確認してください",
		scene, line, alias)
}
