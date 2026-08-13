// Package synth は台本のセリフを音声へ合成し、キャッシュを介して wav を用意する。
//
// build の中で最も時間がかかるのがここであり、同時に最も何度も繰り返される場所でもある。
// 台本を 1 行直すたびに全セリフを合成し直していては量産が回らないため、
// 合成の前後には必ずキャッシュを挟む（→ README「設計方針 3」）。
//
// 保管庫と合成エンジンはどちらも interface で受け取る。実物は internal/cache と internal/tts だが、
// そこへ直に依存すると「ディスクとエンジンが揃っていないと 1 行も試せない」ことになり、
// 段取り（どの順で読み、測り、保存するか）そのものを確かめられなくなるためである。
package synth

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/takiren/scenaremo/internal/audio"
	"github.com/takiren/scenaremo/internal/cache"
	"github.com/takiren/scenaremo/internal/props"
	"github.com/takiren/scenaremo/internal/script"
	"github.com/takiren/scenaremo/internal/tts"
)

// DefaultRelAudioDir は props.json に載せる wav のパスの既定の接頭辞。
//
// 保管庫が書き出す先と同じ場所を指していること。キャッシュのファイル名はキャッシュキーそのもので、
// props.json はそのファイルを指す。つまりキャッシュは「再合成を避けるための控え」であると同時に
// renderer が読む現物でもあり、この 2 つがずれるとレンダリング時に音声が見つからない。
const DefaultRelAudioDir = ".scenaremo/audio"

// Store は合成済み wav の保管庫。internal/cache.Store がそのまま満たす。
type Store interface {
	// Get はキーに対応する wav を返す。無ければ os.ErrNotExist をラップしたエラーを返す。
	Get(key string) ([]byte, error)
	// Put は wav を保存する。
	Put(key string, wav []byte) error
}

// EngineResolver は話者のエンジン種別から合成エンジンを引く。
//
// 台本は 1 本の中で複数のエンジンを使えるため、Run はエンジンを 1 つ受け取るのではなく
// 「種別から引く口」を受け取る。接続先 (baseUrl) をどう決めるかは CLI の設定の話であり、
// 合成の段取りには関係しないので、ここでは引けるかどうかだけを知っていればよい。
type EngineResolver interface {
	Engine(kind tts.EngineKind) (tts.Engine, error)
}

// Engines は種別から Engine への素朴な対応表。EngineResolver を満たす。
type Engines map[tts.EngineKind]tts.Engine

// Engine は kind に対応するエンジンを返す。用意されていなければエラーを返す。
func (e Engines) Engine(kind tts.EngineKind) (tts.Engine, error) {
	if engine := e[kind]; engine != nil {
		return engine, nil
	}
	if len(e) == 0 {
		return nil, fmt.Errorf("エンジン %q は用意されていません（使えるエンジンが1つも登録されていません）", kind)
	}
	// 何が使えるのかを併記する。台本の engine の綴り間違いはここでしか気付けない。
	available := make([]string, 0, len(e))
	for k := range e {
		available = append(available, string(k))
	}
	slices.Sort(available) // map の反復順で文言が揺れると、同じ失敗が別のメッセージに見える
	return nil, fmt.Errorf("エンジン %q は用意されていません。台本の speakers[].engine を確認してください（使えるエンジン: %s）",
		kind, strings.Join(available, ", "))
}

// Reporter は進捗の通知先。index はすべて 0 起点の通し番号。
//
// 合成は数分かかることがあるので、何も出ないと止まって見える。
// ただし何をどう見せるかは表示側の判断なので、ここは通知するだけで書式には関与しない。
type Reporter interface {
	// Start は合成を始めるときに、総セリフ数を伴って呼ばれる。
	Start(total int)
	// LineStart は 1 セリフの処理に入るときに呼ばれる。この時点ではキャッシュを見たかどうかは決まっていない。
	LineStart(index int, speaker, text string)
	// LineDone は 1 セリフを終えたときに呼ばれる。cached はエンジンを呼ばずに済んだことを表す。
	LineDone(index int, cached bool, d time.Duration)
	// Done は全件を終えたときに呼ばれる。途中で失敗した回には呼ばれない。
	Done(synthesized, cached int)
}

// Input は合成の入力。
type Input struct {
	// Script は検証を終えた台本。既定値は Run が埋めるので、埋まっていなくてもよい。
	Script *script.Script

	// Engines は話者のエンジン種別から合成エンジンを引く口。
	Engines EngineResolver

	// Store は合成済み wav の保管庫。
	Store Store

	// RelAudioDir は props.json に載せるパスの接頭辞。空なら DefaultRelAudioDir。
	RelAudioDir string

	// NoCache は既存のキャッシュを読まずに合成し直すかどうか。保存は変わらず行う。
	//
	// 読み込みだけを止めて保存は続けるのは、これが「キャッシュを疑うためのスイッチ」だからである。
	// 保存まで止めると、出来上がった props.json が指す wav がどこにも無いことになる。
	NoCache bool

	// Workers は音声合成の並列数。0 または 1 の場合は順次実行される。
	Workers int

	// Reporter は進捗の通知先。nil なら何も通知しない。
	Reporter Reporter
}

// Result は合成の結果。
type Result struct {
	// Audio は各セリフの合成結果。台本の scenes / lines と同じ形で、そのまま props.Build へ渡せる。
	Audio [][]props.LineAudio
	// Synthesized はエンジンを呼んだ件数。
	Synthesized int
	// Cached はキャッシュで済んだ件数。
	Cached int
}

// Run は台本のすべてのセリフを音声にし、props.Build へそのまま渡せる形で返す。
//
// 処理は台本の順に 1 件ずつ行う。エンジンは 1 プロセスで動く前提なので並列に投げても速くなるとは限らず、
// 一方で進捗の見え方と失敗時の後始末は確実に複雑になるため、ここでは並べない（→ issue #24）。
//
// 同じセリフ（同じ話者・同じパラメータ・同じテキスト）が台本に 2 回現れた場合、
// 2 回目は 1 回目が保存したものを読むのでキャッシュヒットになる。キーは合成の入力だけで決まり、
// 出来上がる wav も同一なので、これは意図した振る舞いである（Audio には両方とも同じパスが載る）。
//
// 途中で失敗したときは Reporter.Done を呼ばずにエラーを返す。完了の通知は「全件そろった」合図であり、
// 失敗した回にそれを出すと、表示だけを見ている利用者に嘘をつくことになる。
func Run(ctx context.Context, in Input) (*Result, error) {
	s := in.Script
	if s == nil {
		return nil, errors.New("台本がありません")
	}
	if len(s.Scenes) == 0 {
		return nil, errors.New("シーンが1つもありません")
	}
	// この 2 つは CLI の配線ミスでしか起きない。利用者に台本を直させても解決しないので、そう伝える。
	if in.Engines == nil {
		return nil, errors.New("音声合成エンジンが渡されていません（scenaremo の不具合です。issue で報告してください）")
	}
	if in.Store == nil {
		return nil, errors.New("音声の保管庫が渡されていません（scenaremo の不具合です。issue で報告してください）")
	}
	// この先は「省略されているかもしれない」を考えなくてよい状態にする。
	// Parse を通っていれば既に埋まっているが、冪等なので二度呼んでも変わらない。
	script.ApplyDefaults(s)

	dir := relAudioDir(in.RelAudioDir)
	reporter := in.Reporter
	if reporter == nil {
		reporter = nopReporter{}
	}

	res := &Result{Audio: make([][]props.LineAudio, len(s.Scenes))}
	reporter.Start(countLines(s))

	index := 0
	for i, scene := range s.Scenes {
		res.Audio[i] = make([]props.LineAudio, len(scene.Lines))
		for j, line := range scene.Lines {
			ref := lineRef(i, j, line.Text)

			// ここで見ないと、キャッシュヒットばかりの回は Synthesize を通らないため
			// ctx を確認する機会が無く、Ctrl-C が効かなくなる。
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("%s: 音声の合成を中断しました。ここまでの音声はキャッシュに残っているので、"+
					"再実行すれば続きから進みます: %w", ref, err)
			}

			kind, req, err := lineRequest(s, line)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", ref, err)
			}
			engine, err := in.Engines.Engine(kind)
			if err != nil {
				return nil, fmt.Errorf("%s: 話者 %q のエンジンを引けませんでした: %w", ref, line.Speaker, err)
			}
			if engine == nil {
				// EngineResolver は差し替えられる口なので、(nil, nil) を返す実装に当たっても
				// nil 参照で落ちずに、どこが悪いのかを言えるようにしておく。
				return nil, fmt.Errorf("%s: 話者 %q のエンジン %q を引けませんでした"+
					"（エンジンが nil で返りました。scenaremo の不具合です。issue で報告してください）", ref, line.Speaker, kind)
			}

			// wav のファイル名はこのキーそのもの。examples/minimal/props.json も同じ規則で組み立てられている。
			key := cache.Key(kind, req)

			reporter.LineStart(index, line.Speaker, line.Text)
			d, cached, err := prepareWAV(ctx, in, engine, key, req)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", ref, err)
			}
			reporter.LineDone(index, cached, d)

			if cached {
				res.Cached++
			} else {
				res.Synthesized++
			}
			res.Audio[i][j] = props.LineAudio{
				// 区切りは常に / 。props.json は別の OS でも読めなければならない（→ internal/props の assetPath）。
				Path:     dir + "/" + key + ".wav",
				Duration: d,
			}
			index++
		}
	}

	reporter.Done(res.Synthesized, res.Cached)
	return res, nil
}

// prepareWAV はキャッシュを見て、無ければ合成して wav を用意し、その実測長を返す。
// 戻り値の cached は「エンジンを呼ばずに済んだ」ことを表す。
func prepareWAV(ctx context.Context, in Input, engine tts.Engine, key string, req tts.SynthesizeRequest) (time.Duration, bool, error) {
	if !in.NoCache {
		if wav, err := in.Store.Get(key); err == nil {
			// 読めた wav が測れなかった場合も、黙って合成し直す。
			// 長さが測れない wav はタイムラインを壊すうえ、一度紛れ込むと build のたびに同じ失敗を繰り返す。
			// 途中で電源が落ちた等で壊れるのは利用者の落ち度ではないので、作り直せる限りエラーにはしない。
			if info, err := audio.MeasureBytes(wav); err == nil {
				return info.Duration, true, nil
			}
		}
	}

	result, err := engine.Synthesize(ctx, req)
	if err != nil {
		return 0, false, fmt.Errorf("音声の合成に失敗しました: %w", err)
	}
	if result == nil {
		// Engine も差し替えられる口なので、(nil, nil) で落ちないようにしておく。
		return 0, false, errors.New("音声の合成に失敗しました（エンジンが結果を返しませんでした。scenaremo の不具合です。issue で報告してください）")
	}

	// 測るのは保存する前。壊れた wav をキャッシュへ置くと、次の build がそれを掴んでまた作り直す羽目になる。
	info, err := audio.MeasureBytes(result.WAV)
	if err != nil {
		return 0, false, fmt.Errorf("合成された音声を読み取れませんでした。エンジンが壊れた wav を返しています: %w", err)
	}

	// 保存できないことは致命的として扱う。ここに置いた wav は控えであると同時に
	// props.json が指す現物でもあり、無いままレンダリングへ進んでも音の出ない動画になるだけだからである。
	if err := in.Store.Put(key, result.WAV); err != nil {
		return 0, false, fmt.Errorf("音声を保存できませんでした: %w", err)
	}
	return info.Duration, false, nil
}

// lineRequest は台本のセリフから、エンジン種別と合成要求を組み立てる。
func lineRequest(s *script.Script, line script.Line) (tts.EngineKind, tts.SynthesizeRequest, error) {
	if line.Speaker == "" {
		// ApplyDefaults を通しても空なら、台本に defaults.speaker が無い。
		// 「話者 "" が定義されていません」では何を書けばよいのか分からないので、別の文言にする。
		return "", tts.SynthesizeRequest{}, errors.New("話者が指定されていません。" +
			"このセリフに speaker を書くか、defaults.speaker を設定してください")
	}
	speaker, ok := s.Speakers[line.Speaker]
	if !ok {
		return "", tts.SynthesizeRequest{}, fmt.Errorf("話者 %q が speakers に定義されていません", line.Speaker)
	}
	return tts.EngineKind(speaker.Engine), tts.SynthesizeRequest{
		Text:    line.Text,
		StyleID: speaker.StyleID,
		// ポインタのまま渡す。nil は「エンジンの既定値を使う」という意味であり、
		// ここで 0 や 1.0 を埋めると台本が指定していない値を指定したことにしてしまう。
		Params: tts.Params{
			SpeedScale:      speaker.SpeedScale,
			PitchScale:      speaker.PitchScale,
			IntonationScale: speaker.IntonationScale,
			VolumeScale:     speaker.VolumeScale,
		},
	}, nil
}

// relAudioDir は props.json に載せるパスの接頭辞を整える。
//
// 呼び出し側が filepath.Join で組み立てた値をそのまま渡してくることを想定して / に揃える。
// Windows で作った props.json を Linux でレンダリングできる、が契約なので、
// 区切りが混ざったパスをここから先へ流さない。
func relAudioDir(dir string) string {
	if dir == "" {
		return DefaultRelAudioDir
	}
	return strings.TrimRight(filepath.ToSlash(dir), "/")
}

// countLines は台本の総セリフ数を返す。
func countLines(s *script.Script) int {
	total := 0
	for _, scene := range s.Scenes {
		total += len(scene.Lines)
	}
	return total
}

// textExcerptLimit はエラーメッセージに載せるセリフの上限文字数。
const textExcerptLimit = 20

// lineRef はエラーメッセージ用に「台本のどこの話か」を組み立てる。
//
// 合成は数十行を一息に回すので、位置が分からないと利用者は台本のどこを直せばよいのか探すことになる。
// 添字だけでなくセリフの頭も出すのは、台本を見ながら数えなくても目で見つけられるようにするため。
func lineRef(scene, line int, text string) string {
	return fmt.Sprintf("scenes[%d].lines[%d] (「%s」)", scene, line, excerptText(text))
}

// excerptText はセリフをエラーメッセージに載せられる長さへ丸める。
// 改行を含む長いセリフをそのまま出すと、メッセージが台本の抜き書きになって肝心の理由が埋もれる。
func excerptText(text string) string {
	s := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(s) > textExcerptLimit {
		s = string([]rune(s)[:textExcerptLimit]) + "…"
	}
	return s
}

// nopReporter は Reporter が渡されなかったときの受け皿。
//
// 呼ぶ側に nil 判定を撒くより、何もしない実装へ落としたほうが本筋（合成の段取り）が読みやすい。
type nopReporter struct{}

func (nopReporter) Start(int)                         {}
func (nopReporter) LineStart(int, string, string)     {}
func (nopReporter) LineDone(int, bool, time.Duration) {}
func (nopReporter) Done(int, int)                     {}

var (
	_ EngineResolver = Engines(nil)
	_ Reporter       = nopReporter{}
	// 実物のキャッシュがそのまま保管庫として使えること。
	// Store の形を変えたときに、CLI の配線ではなくここで気付けるようにしておく。
	_ Store = (*cache.Store)(nil)
)
