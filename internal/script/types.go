// Package script は台本（YAML / JSON）の型定義を提供する。
//
// スキーマの唯一の正は docs/schema.json であり、この型定義はそれに追従する。
// フィールドを足し引きするときは、必ず先に docs/schema.json を直すこと。
package script

// Aspect はアスペクト比を表す。
type Aspect string

const (
	// Aspect16x9 は横型。既定値。
	Aspect16x9 Aspect = "16:9"
	// Aspect9x16 は縦型（ショート動画向け）。
	Aspect9x16 Aspect = "9:16"
)

// Transition はシーンの繋ぎ方を表す。
type Transition string

const (
	// TransitionFade はフェードで繋ぐ。
	TransitionFade Transition = "fade"
	// TransitionNone は繋ぎの演出を入れない。
	TransitionNone Transition = "none"
)

// Engine は音声合成エンジンを表す。
type Engine string

// EngineVoicevox は VOICEVOX。MVP 時点で唯一のエンジン。
const EngineVoicevox Engine = "voicevox"

// 省略されたフィールドに適用される既定値。docs/schema.json の default と一致させること。
const (
	// DefaultAspect は meta.aspect の既定値。
	DefaultAspect = Aspect16x9
	// DefaultFPS は meta.fps の既定値。
	DefaultFPS = 30
	// DefaultEngine は speakers[].engine の既定値。
	DefaultEngine = EngineVoicevox
	// DefaultTransition は transition の既定値。
	DefaultTransition = TransitionFade
	// DefaultGapMs は defaults.gapMs の既定値（ミリ秒）。
	DefaultGapMs = 300
	// DefaultSceneGapMs は defaults.sceneGapMs の既定値（ミリ秒）。
	//
	// セリフ間 (DefaultGapMs) より長いのは、シーンの切り替わりが話題の切れ目にあたるため。
	// ここが短いと、文の切れ目より話題の切れ目のほうが浅い間になって落ち着かない。
	// DefaultTransitionMs (400) より長いのも意図的で、こうしておくとフェードが
	// 無音の中で完結し、前のシーンの語尾に被らない（→ issue #44）。
	DefaultSceneGapMs = 500
	// DefaultComponent は scenes[].component の既定値。renderer 側 registry のキー。
	DefaultComponent = "default"
)

// Script は台本1本ぶんの内容を表す。人間が書く唯一の入力。
type Script struct {
	// Schema は JSON で台本を書く場合のスキーマ参照。
	// YAML では代わりに先頭へ `# yaml-language-server: $schema=...` コメントを書くため、
	// 通常は空になる。
	Schema string `yaml:"$schema,omitempty" json:"$schema,omitempty"`

	// Meta は動画全体の設定。
	Meta Meta `yaml:"meta" json:"meta"`

	// Speakers は話者エイリアスから音声エンジン設定への対応表。キーが台本中で使う名前。
	Speakers map[string]Speaker `yaml:"speakers" json:"speakers"`

	// Defaults は省略されたフィールドに使う既定値。未指定なら nil。
	Defaults *Defaults `yaml:"defaults,omitempty" json:"defaults,omitempty"`

	// Scenes はシーンの並び。動画はこの順に再生される。
	Scenes []Scene `yaml:"scenes" json:"scenes"`
}

// Meta は動画全体の設定を表す。
// 尺は音声の実測長から決まるため、フレーム数や秒数は持たない。
type Meta struct {
	// Title は動画のタイトル。必須。
	Title string `yaml:"title" json:"title"`

	// Aspect はアスペクト比。空なら DefaultAspect。
	Aspect Aspect `yaml:"aspect,omitempty" json:"aspect,omitempty"`

	// FPS はフレームレート。0（未指定）なら DefaultFPS。
	FPS int `yaml:"fps,omitempty" json:"fps,omitempty"`
}

// Speaker は話者エイリアス1件の定義を表す。
// 音声パラメータはポインタで持つ。未指定と「0 を明示した」を区別する必要があるため。
type Speaker struct {
	// Engine は音声合成エンジン。空なら DefaultEngine。
	Engine Engine `yaml:"engine,omitempty" json:"engine,omitempty"`

	// StyleID はエンジンのスタイル ID。必須。
	StyleID int `yaml:"styleId" json:"styleId"`

	// SpeedScale は話速。既定は 1.0。
	SpeedScale *float64 `yaml:"speedScale,omitempty" json:"speedScale,omitempty"`

	// PitchScale は音高。既定は 0。
	PitchScale *float64 `yaml:"pitchScale,omitempty" json:"pitchScale,omitempty"`

	// IntonationScale は抑揚。既定は 1.0。
	IntonationScale *float64 `yaml:"intonationScale,omitempty" json:"intonationScale,omitempty"`

	// VolumeScale は音量。既定は 1.0。
	VolumeScale *float64 `yaml:"volumeScale,omitempty" json:"volumeScale,omitempty"`
}

// Defaults は省略されたフィールドに適用される既定値を表す。
type Defaults struct {
	// Speaker は lines[].speaker を省略したときに使う話者エイリアス。
	Speaker string `yaml:"speaker,omitempty" json:"speaker,omitempty"`

	// Transition は scenes[].transition を省略したときに使うトランジション。
	// 空なら DefaultTransition。
	Transition Transition `yaml:"transition,omitempty" json:"transition,omitempty"`

	// GapMs は同じシーンの中でセリフ間に入れる余白（ミリ秒）。
	// 0 を明示する（余白なし）ことと未指定を区別するためポインタで持つ。nil なら DefaultGapMs。
	GapMs *int `yaml:"gapMs,omitempty" json:"gapMs,omitempty"`

	// SceneGapMs はシーンの末尾に入れる余白（ミリ秒）。
	// シーンとシーンの間の間（ま）と、動画末尾の余韻の両方がこの値で決まる。
	// GapMs と同じ理由でポインタで持つ。nil なら DefaultSceneGapMs。
	SceneGapMs *int `yaml:"sceneGapMs,omitempty" json:"sceneGapMs,omitempty"`
}

// Scene は画像1枚と、その間に喋るセリフの集まりを表す。
type Scene struct {
	// Image は表示する画像のパス。台本ファイルからの相対パス。必須。
	Image string `yaml:"image" json:"image"`

	// Transition は前のシーンからの繋ぎ方。空なら Defaults.Transition。
	Transition Transition `yaml:"transition,omitempty" json:"transition,omitempty"`

	// Component はこのシーンの描画に使う React コンポーネント名。
	// renderer 側の registry のキーで、空なら DefaultComponent。
	//
	// 拡張の逃げ道（issue #34）のための予約フィールド。MVP では registry に default しか無いが、
	// 後から props.json の契約に足すと影響が大きいため最初から確保しておく。
	Component string `yaml:"component,omitempty" json:"component,omitempty"`

	// Props は Component に渡す任意のプロパティ。
	// 中身はコンポーネント側の責務であり、CLI は検証せず props.json へそのまま透過させる。
	//
	// Component と同じく issue #34 のための予約フィールド。
	Props map[string]any `yaml:"props,omitempty" json:"props,omitempty"`

	// Lines はこのシーンで喋るセリフの並び。必須。
	Lines []Line `yaml:"lines" json:"lines"`
}

// Line はセリフ1つを表す。
type Line struct {
	// Speaker は話者エイリアス。空なら Defaults.Speaker。
	Speaker string `yaml:"speaker,omitempty" json:"speaker,omitempty"`

	// Text は読み上げる文章。必須。改行を含められる。
	Text string `yaml:"text" json:"text"`
}
