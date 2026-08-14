package props

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"time"

	"github.com/takiren/scenaremo/internal/script"
	"github.com/takiren/scenaremo/internal/timeline"
)

// DefaultTransitionMs はシーンを繋ぐのに掛ける時間の既定値（ミリ秒）。
//
// 台本にはまだこれを変えるノブが無い。長さを ms で指定する `transitionMs` を台本へ足すのは
// トランジションの実装（issue #10）と一緒に行う。フレームではなく ms なのは、
// フレームで書かせると fps を変えた瞬間に体感の速さが変わってしまうためで、
// ms → フレームの変換は CLI の仕事という層の分け方とも一致する。
//
// この値は script.DefaultSceneGapMs より短くしておくこと。繋ぎは前のシーンの末尾に重なるので、
// シーン末尾の余白より長いとフェードが前のシーンの語尾に被る（→ issue #44）。
const DefaultTransitionMs = 400

// CreditsBaseMs はクレジットシーンの尺のうち、表記の件数に依らない部分（ミリ秒）。
//
// 画が切り替わったことに気づいてから 1 行目を読み始めるまでの時間にあたる。
// フレームではなく ms なのは DefaultTransitionMs と同じ理由で、
// フレームで持つと fps を変えた瞬間に体感の速さが変わってしまうためである。
const CreditsBaseMs = 2000

// CreditsPerEntryMs はクレジット表記 1 件につき足す時間（ミリ秒）。
//
// 尺を「基本 + 件数 × これ」にしたのは、表記が 1 件 1 行で並ぶ以上、
// 読み切るのに要る時間が件数に比例するからである。固定の尺にすると、話者を 5 人使った台本で
// 読み終わる前にクレジットが消える。規約を満たすために出しているものが読めないのでは
// 出す意味が無いので、件数の側に合わせるほうを採った。
//
// 上限は設けていない。頭打ちにすると「表記はあるが読めない」という、
// いちばん避けたい状態を機械的に作り出してしまう。長すぎると感じる場合は
// 台本で meta.creditsScene: false にして、自分で置く道が残してある。
const CreditsPerEntryMs = 1000

// resolutions はアスペクト比から解像度への対応表。
//
// この表を CLI 側に置くことで、renderer は props.json の width / height を読むだけで済む。
// 解像度の決め方が 2 箇所にあると、どちらが正なのか分からなくなる。
var resolutions = map[script.Aspect]struct{ width, height int }{
	script.Aspect16x9: {1920, 1080},
	script.Aspect9x16: {1080, 1920},
}

// creditEngineNames はクレジット表記に使うエンジンの名前。
//
// tts.DisplayName とは別に持つ。あちらはエラーメッセージ用で "VOICEVOX ENGINE" のように
// ソフトウェア名を返すが、クレジットに書くべきなのは規約が定める "VOICEVOX" のほうであるため。
var creditEngineNames = map[script.Engine]string{
	script.EngineVoicevox: "VOICEVOX",
}

// MoraTiming は合成結果から受け取るモーラ 1 つ分の実時間。
// tts パッケージの同名型と重複するが、これはエンジン実装 (tts) へ依存させずに契約を保つため。
type MoraTiming struct {
	// Text はカナ表記。
	Text string
	// Vowel は母音の音素。
	Vowel string
	// Offset はそのセリフの音声の先頭（無音の prePhonemeLength を含む）からこのモーラが鳴り始めるまでの時間。
	Offset time.Duration
	// Duration はこのモーラの発話長。
	Duration time.Duration
}

// LineAudio はセリフ1つ分の合成結果。
type LineAudio struct {
	// Path は props.json に載せる wav のパス。動画ディレクトリからの相対で / 区切り。
	Path string
	// Duration は wav の実測長。フレーム数はここから決まる。
	Duration time.Duration
	// Moras はモーラごとのタイミング情報。エンジンが返さなければ空。
	Moras []MoraTiming
}

// SpeakerCredit は話者エイリアス1件のクレジット情報。
//
// スタイル ID から話者名を引くにはエンジンへの問い合わせが要るため、Build の入力として受け取る。
// ここで自分から問い合わせてしまうと、props.json の組み立てがネットワークに依存し、
// エンジンを起動せずにテストできなくなる。
type SpeakerCredit struct {
	// Name は話者（キャラクター）の表示名。エンジンの /speakers が返す名前。
	Name string
	// UUID はエンジンが返す話者の UUID。空でもよいが、あれば同名の別話者を取り違えずに済む。
	UUID string
}

// Input は props.json を組み立てるための材料。
type Input struct {
	// Script は検証を終えた台本。既定値は Build が埋めるので、埋まっていなくてもよい。
	Script *script.Script

	// Audio は各セリフの合成結果。Script.Scenes と同じ形（シーンの数、各シーンのセリフの数）であること。
	Audio [][]LineAudio

	// Credits は話者エイリアスごとのクレジット情報。台本で使われるすべての話者について必要。
	Credits map[string]SpeakerCredit

	// GeneratedBy は生成した scenaremo のバージョン。
	GeneratedBy string
}

// Build は台本と合成結果から props.json の内容を組み立てる。
//
// 音声合成そのものからは切り離してある。エンジンを起動しなくても、実測長を並べれば
// タイムラインとクレジットの組み立てだけを確かめられる。
func Build(in Input) (*Props, error) {
	s := in.Script
	if s == nil {
		return nil, errors.New("台本がありません")
	}
	if len(s.Scenes) == 0 {
		return nil, errors.New("シーンが1つもありません")
	}
	// この先は「省略されているかもしれない」を考えなくてよい状態にする。
	// Parse を通っていれば既に埋まっているが、冪等なので二度呼んでも変わらない。
	script.ApplyDefaults(s)

	size, ok := resolutions[s.Meta.Aspect]
	if !ok {
		return nil, fmt.Errorf("解像度を決められないアスペクト比です: %q", s.Meta.Aspect)
	}
	if len(in.Audio) != len(s.Scenes) {
		return nil, fmt.Errorf("音声の数がシーンの数と合いません: シーン %d 個に対して音声 %d 個",
			len(s.Scenes), len(in.Audio))
	}

	tlIn, err := timelineInput(s, in.Audio)
	if err != nil {
		return nil, err
	}
	tl := timeline.Calculate(tlIn)

	scenes, err := buildScenes(s, in.Audio, tl)
	if err != nil {
		return nil, err
	}

	credits, err := BuildCredits(s, in.Credits)
	if err != nil {
		return nil, err
	}

	return &Props{
		Version:     Version,
		GeneratedBy: in.GeneratedBy,
		Note:        Note,
		Meta: Meta{
			Title:  s.Meta.Title,
			Aspect: string(s.Meta.Aspect),
			Width:  size.width,
			Height: size.height,
			FPS:    s.Meta.FPS,
			// クレジットシーンの分まで含めた最終的な尺（→ issue #17）。
			// クレジットは最後のシーンの直後に置くと決まっていて繋ぎも持たないので、
			// 総尺はこの足し算だけで出る。表示しない場合は credits.DurationInFrames が 0 になり、
			// 式を分岐させずにそのまま伸びない値になる。
			DurationInFrames: tl.TotalFrames + credits.DurationInFrames,
		},
		Scenes:  scenes,
		Credits: credits,
	}, nil
}

// timelineInput は台本と合成結果をタイムライン計算の入力へ直す。
// 音声の数と形がここで台本と合っているかを確かめる。
func timelineInput(s *script.Script, audio [][]LineAudio) (timeline.Input, error) {
	in := timeline.Input{
		FPS:        s.Meta.FPS,
		GapMs:      *s.Defaults.GapMs,
		SceneGapMs: *s.Defaults.SceneGapMs,
		Scenes:     make([]timeline.SceneInput, 0, len(s.Scenes)),
	}

	for i, scene := range s.Scenes {
		// 喋らないシーンは尺が 0 になり、契約 (scenes[].durationInFrames >= 1) を満たせない。
		// 台本のスキーマも minItems: 1 で弾いているが、ここを通す道があると壊れた props.json が出る。
		if len(scene.Lines) == 0 {
			return timeline.Input{}, fmt.Errorf("scenes[%d]: セリフが1つもありません。シーンの尺は音声で決まるため、喋らないシーンは置けません", i)
		}
		if len(audio[i]) != len(scene.Lines) {
			return timeline.Input{}, fmt.Errorf("scenes[%d]: 音声の数がセリフの数と合いません: セリフ %d 個に対して音声 %d 個",
				i, len(scene.Lines), len(audio[i]))
		}

		lines := make([]timeline.LineInput, 0, len(audio[i]))
		for j, a := range audio[i] {
			if a.Duration <= 0 {
				return timeline.Input{}, fmt.Errorf("scenes[%d].lines[%d]: 音声の長さが 0 です。合成に失敗した wav ではありませんか", i, j)
			}
			lines = append(lines, timeline.LineInput{AudioDuration: a.Duration})
		}

		in.Scenes = append(in.Scenes, timeline.SceneInput{
			TransitionMs: transitionMs(scene.Transition),
			Lines:        lines,
		})
	}
	return in, nil
}

// transitionMs は台本のトランジション指定を時間へ直す。
func transitionMs(t script.Transition) int {
	if t == script.TransitionNone {
		return 0
	}
	return DefaultTransitionMs
}

// frameMoras は実時間のモーラ情報をフレーム番号へ直す。
//
// 1. 境界での計算:
// 個々の長さをフレームへ丸めて足し込むのではなく、実時間のまま累積した「開始位置」と「終了位置」を
// それぞれフレームへ直してから、その差を長さとする。こうすることで隣り合うモーラが隙間なく繋がる。
//
// 2. 整数のナノ秒での切り上げ:
// 0.3秒 × 30fps のような値を float64 の秒で計算すると 9.000000000000002 になり、
// これを math.Ceil すると 10 に切り上がってしまう事故が起きる。
// そのため time.Duration (int64) のまま整数演算で切り上げる。
//
// 3. 尺のクランプ:
// wav の実測長はエンジンの出力結果であり、クエリの予測時間とはわずかにずれうる。
// props.json は Sequence に載せる以上「セリフの尺の中に収まっている」必要があるため、はみ出る分は詰める。
func frameMoras(timings []MoraTiming, fps int, lineDurFrames int) []Mora {
	if len(timings) == 0 {
		return nil
	}
	var moras []Mora
	for _, m := range timings {
		start := (int64(m.Offset)*int64(fps) + int64(time.Second) - 1) / int64(time.Second)
		end := (int64(m.Offset+m.Duration)*int64(fps) + int64(time.Second) - 1) / int64(time.Second)
		startFrame := int(start)
		durationInFrames := int(end - start)

		if startFrame >= lineDurFrames {
			continue
		}
		if startFrame+durationInFrames > lineDurFrames {
			durationInFrames = lineDurFrames - startFrame
		}
		moras = append(moras, Mora{
			Text:             m.Text,
			Vowel:            m.Vowel,
			StartFrame:       startFrame,
			DurationInFrames: durationInFrames,
		})
	}
	return moras
}

// buildScenes は台本・合成結果・タイムラインを突き合わせて scenes を組み立てる。
func buildScenes(s *script.Script, audio [][]LineAudio, tl timeline.Timeline) ([]Scene, error) {
	scenes := make([]Scene, 0, len(s.Scenes))

	for i, scene := range s.Scenes {
		image, err := assetPath(scene.Image)
		if err != nil {
			return nil, fmt.Errorf("scenes[%d].image: %w", i, err)
		}

		lines := make([]Line, 0, len(scene.Lines))
		for j, line := range scene.Lines {
			audioPath, err := assetPath(audio[i][j].Path)
			if err != nil {
				return nil, fmt.Errorf("scenes[%d].lines[%d].audio: %w", i, j, err)
			}
			lineDurFrames := tl.Scenes[i].Lines[j].DurationFrames
			moras := frameMoras(audio[i][j].Moras, s.Meta.FPS, lineDurFrames)

			lines = append(lines, Line{
				Speaker:          line.Speaker,
				Text:             line.Text,
				Audio:            audioPath,
				StartFrame:       tl.Scenes[i].Lines[j].StartFrame,
				DurationInFrames: lineDurFrames,
				Moras:            moras,
			})
		}

		scenes = append(scenes, Scene{
			Image:     image,
			Component: scene.Component,
			// Props は中身を見ずにそのまま渡す。何が正しいかを知っているのは
			// コンポーネント側だけなので、CLI が検証すると逃げ道を塞ぐことになる（→ issue #34）。
			Props:            scene.Props,
			DurationInFrames: tl.Scenes[i].DurationFrames,
			Transition: Transition{
				Type:             string(scene.Transition),
				DurationInFrames: tl.Scenes[i].TransitionFrames,
			},
			Lines: lines,
		})
	}
	return scenes, nil
}

// assetPath は台本や合成結果のパスを props.json に載せる形へ直す。
//
// 区切りを / に揃えるのは、Windows で作った props.json を Linux でレンダリングできるようにするため。
// 絶対パスを弾くのは、それが別のマシンでも CI でも eject 後のプロジェクトでも成り立たないため。
// 動画ディレクトリからの相対でありさえすれば、renderer 側がどうアセットを解決することにしても
// （--public-dir を差し替えても、public へコピーしても）そのまま通る（→ issue #12）。
func assetPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("パスが空です")
	}
	slashed := filepath.ToSlash(p)
	if path.IsAbs(slashed) || filepath.IsAbs(p) {
		return "", fmt.Errorf("パスが絶対パスです: %s (動画ディレクトリからの相対パスで書いてください。"+
			"props.json は別のマシンでも読めるように絶対パスを持ちません)", p)
	}
	return slashed, nil
}

// BuildCredits は台本で使われた話者からクレジットを集計する。
//
// 集計の単位はスタイルではなくキャラクターにする。規約が求めているのは音声ライブラリ単位の表記なので、
// 同じ話者のノーマルとあまあまを使い分けても、書くべきクレジットは1つだからである。
//
// Build の一部でありながら公開しているのは、`scenaremo credits`（→ issue #16）が
// 音声を合成せずに同じクレジットを出す必要があるためである。あちらへ集計をもう1つ書くと、
// props.json に載るクレジットと `scenaremo credits` の出力が静かに食い違い、
// 表記漏れを機械的に防ぐという目的そのものが崩れる。
func BuildCredits(s *script.Script, resolved map[string]SpeakerCredit) (Credits, error) {
	// 同名の別話者を1件にまとめてしまわないよう、UUID が分かるならそちらを優先して数える。
	type key struct{ engine, identity string }

	var order []key
	entries := make(map[key]*Entry)

	for i, scene := range s.Scenes {
		for j, line := range scene.Lines {
			speaker, ok := s.Speakers[line.Speaker]
			if !ok {
				return Credits{}, fmt.Errorf("scenes[%d].lines[%d]: 話者 %q が speakers に定義されていません",
					i, j, line.Speaker)
			}
			credit, ok := resolved[line.Speaker]
			if !ok || credit.Name == "" {
				// 黙って飛ばすとクレジットが1件足りない props.json ができてしまう。
				// 表記漏れは利用者の事故に直結するので、揃っていなければ生成しない。
				return Credits{}, fmt.Errorf("話者 %q のクレジット情報がありません。"+
					"クレジットの表記漏れは利用者の事故に直結するため、"+
					"使用したすべての話者の名前が揃うまで props.json は生成しません", line.Speaker)
			}

			k := key{engine: string(speaker.Engine), identity: credit.UUID}
			if k.identity == "" {
				k.identity = credit.Name
			}

			entry, seen := entries[k]
			if !seen {
				entry = &Entry{
					Engine:      string(speaker.Engine),
					SpeakerName: credit.Name,
					SpeakerUUID: credit.UUID,
					Text:        creditText(speaker.Engine, credit.Name),
				}
				entries[k] = entry
				order = append(order, k)
			}
			if !slices.Contains(entry.StyleIDs, speaker.StyleID) {
				entry.StyleIDs = append(entry.StyleIDs, speaker.StyleID)
			}
		}
	}

	// 並びは台本での登場順。map をそのまま回すと build のたびに順序が変わり、
	// 同じ台本から違う props.json が出てしまう。
	out := make([]Entry, 0, len(order))
	for _, k := range order {
		entry := entries[k]
		slices.Sort(entry.StyleIDs)
		out = append(out, *entry)
	}

	// クレジットシーンの尺を決める（→ issue #17）。0 は「表示しない」を表し、
	// meta.durationInFrames もその分だけ伸びない。
	//
	// 台本が meta.creditsScene: false で切っているときのほか、載せる表記が 1 件も無いときも 0 にする。
	// 後者を通してしまうと、何も書かれていない画が末尾に数秒残ることになる。
	// entries のほうは切っていても返す。renderer が独自に表示できるという契約のためで、
	// 「表示しない」のはこのシーンだけであって集計そのものではない。
	//
	// CreditsScene は ApplyDefaults を通っていれば nil にならないが、nil は既定と同じ「有効」として読む。
	// この関数は Build 以外からも呼ばれうるので、埋め忘れで落ちるより安全側の既定に倒すほうがよい。
	enabled := s.Meta.CreditsScene == nil || *s.Meta.CreditsScene
	frames := 0
	if enabled && len(out) > 0 {
		// ms → フレームの変換は CLI の仕事（renderer 側でやり直させない）。
		// 丸めは timeline と同じ切り上げに揃える。規則が 1 つで説明できるほうが契約として強い。
		ms := CreditsBaseMs + CreditsPerEntryMs*len(out)
		frames = (ms*s.Meta.FPS + 999) / 1000
	}

	return Credits{
		DurationInFrames: frames,
		Entries:          out,
	}, nil
}

// creditText はそのまま表示できるクレジット表記を作る。
func creditText(engine script.Engine, name string) string {
	label, ok := creditEngineNames[engine]
	if !ok {
		label = string(engine)
	}
	return label + ":" + name
}
