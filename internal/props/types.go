// Package props は props.json の型と生成を提供する。
//
// props.json は CLI と Remotion の間の唯一のインターフェースであり、ここが固まっている限り
// 両者を独立に開発できる（→ README「設計方針 6」）。CLI は「音を作ってタイムラインを組む」ところまでで、
// 見た目の責務は一切持たない。その境界を形にしたのがこのパッケージである。
//
// スキーマの唯一の正は docs/props.schema.json であり、この型定義はそれに追従する。
// フィールドを足し引きするときは、必ず先に docs/props.schema.json を直すこと。
// renderer 側の zod も同じファイルに従うため、ここだけ直すと静かに乖離する。
package props

// Version は props.json の契約の版。
//
// 互換性を壊す変更をしたときだけ上げる。項目を足すだけなら上げない
// （読み手は知らない項目を無視すればよく、古い renderer でも壊れないため）。
const Version = 1

// Note は props.json に埋め込む人間向けの注意書き。
//
// JSON にはコメントが書けないため、「手で編集しない」という契約をファイル自身に持たせている。
// .scenaremo/ は gitignore 済みで毎回作り直されるので、ここを直しても次の build で消える。
const Note = "このファイルは scenaremo が生成します。手で編集しないでください（変更は script.yaml か renderer/src/*.tsx で行います）。"

// Props は props.json 全体。
//
// JSON のキーの並びは構造体の並びで決まる。人が読む場面（不具合の報告や差分の確認）を考えて、
// メタ情報を先に、中身を後に置いている。
//
// # 座標系
//
// 動画の先頭からの絶対位置はどこにも現れない。シーンは「尺」を、セリフは「シーンの先頭からの相対位置」を持つ。
// renderer が TransitionSeries でシーンを並べると子シーケンスが前へ詰められるため、
// 絶対位置を渡しても意味を成さないからである。相対に振り切ることで、
// どの値もそれが置かれる Sequence の中でそのまま使える。
type Props struct {
	// Version は契約の版。
	Version int `json:"version"`

	// GeneratedBy は生成した scenaremo のバージョン。
	// 不具合の報告を受けたときに、どの版が吐いた props.json なのかを特定するために使う。
	GeneratedBy string `json:"$generatedBy"`

	// Note は人間向けの注意書き。機械はこの値を読まない。
	Note string `json:"$note"`

	// Meta は動画全体の設定。
	Meta Meta `json:"meta"`

	// Scenes はシーンの並び。台本の scenes と 1 対 1 で対応する。
	Scenes []Scene `json:"scenes"`

	// Credits は使用した音声ライブラリのクレジット。
	Credits Credits `json:"credits"`
}

// Meta は動画全体の設定。renderer の calculateMetadata はここから解像度と尺を決める。
type Meta struct {
	// Title は動画のタイトル。
	Title string `json:"title"`

	// Aspect はアスペクト比。解像度は Width / Height で確定しているので描画には要らないが、
	// どの指定から解決された値なのかが分かるように残している。
	Aspect string `json:"aspect"`

	// Width は解像度の幅 (px)。
	// 台本の aspect から CLI が解決する。renderer 側に解像度の対応表を持たせないため、
	// ここには必ず具体的な数値が入る。
	Width int `json:"width"`

	// Height は解像度の高さ (px)。
	Height int `json:"height"`

	// FPS はフレームレート。props.json 中のすべてのフレーム数はこの値を前提に計算されている。
	FPS int `json:"fps"`

	// DurationInFrames は動画全体の総フレーム数。Credits の分も含む。
	// calculateMetadata はこの値をそのまま採用する。台本にフレーム数を書かせないための要。
	//
	// TransitionSeries の尺の式（各シーケンスの尺の合計 − トランジションの尺の合計）と一致する。
	DurationInFrames int `json:"durationInFrames"`
}

// Scene は画像1枚と、その間に喋るセリフの集まり。
type Scene struct {
	// Image は表示する画像のパス。動画ディレクトリからの相対で、区切りは OS に依らず / 。
	//
	// この形なのは、renderer が --public-dir に動画ディレクトリを渡して起動し、
	// この値をそのまま staticFile() へ通すためである（→ README「設計方針 7」）。
	// 絶対パスにしてはならない。staticFile() が TypeError で弾く。
	Image string `json:"image"`

	// Component はこのシーンの描画に使う React コンポーネント名。
	// renderer 側 registry のキーで、既定は default（→ issue #34）。
	Component string `json:"component"`

	// Props は Component に渡す任意のプロパティ。
	// 台本に書かれた内容を CLI が検証せずそのまま透過させたもの。
	Props map[string]any `json:"props,omitempty"`

	// DurationInFrames は TransitionSeries.Sequence へ渡す尺。
	//
	// 喋りの尺そのものではなく、そこへシーン末尾の余白（台本の defaults.sceneGapMs）と
	// Transition.DurationInFrames を足した値になる。
	// TransitionSeries は隣り合うシーケンスを繋ぎのぶん重ねて詰めるので、
	// 重なる分をあらかじめ申告しておかないと、シーンが繋ぎのぶんだけ前へずれてしまう。
	DurationInFrames int `json:"durationInFrames"`

	// Transition は前のシーンからこのシーンへの繋ぎ。
	Transition Transition `json:"transition"`

	// Lines はこのシーンで喋るセリフの並び。
	Lines []Line `json:"lines"`
}

// Transition は前のシーンからの繋ぎ。TransitionSeries.Transition に対応する。
//
// 繋ぎはシーンの先頭 DurationInFrames フレームで行われ、
// ちょうど終わったところで最初のセリフが鳴り始める（Lines[0].StartFrame と一致する）。
// 次の声が鳴り始めた時点で新しい画像が出揃っている状態にするための置き方である。
type Transition struct {
	// Type は繋ぎ方。none なら繋ぎの演出を入れない。
	Type string `json:"type"`

	// DurationInFrames は繋ぎに掛けるフレーム数。0 なら繋ぎ無し。
	//
	// renderer はこの値をそのまま使う timing を選ぶこと（linearTiming({durationInFrames}) など）。
	// springTiming のように設定から尺が決まるものを使うと、CLI の申告と食い違って音がずれる。
	DurationInFrames int `json:"durationInFrames"`
}

// Line はセリフ1つ。
type Line struct {
	// Speaker は話者エイリアス。台本で省略されていた場合は既定値が解決済みで入っている。
	Speaker string `json:"speaker"`

	// Text は読み上げた文章。台本に書かれたまま（改行も保持する）で、字幕はこれを使う。
	Text string `json:"text"`

	// Audio は合成された wav のパス。Image と同じく動画ディレクトリからの相対で / 区切り。
	// .scenaremo/ 以下を指すが、ドット始まりのディレクトリも public dir として配信されることは確認済み。
	Audio string `json:"audio"`

	// StartFrame は音声が鳴り始めるフレーム。**シーンの先頭からの相対位置**で、動画の先頭からではない。
	// シーンの Sequence の中にそのまま置ける値になっている。
	StartFrame int `json:"startFrame"`

	// DurationInFrames はこのセリフに与えられたフレーム数。音声の実測長を切り上げた値。
	DurationInFrames int `json:"durationInFrames"`
}

// Credits は使用した音声ライブラリのクレジット表記。
//
// VOICEVOX は音声ライブラリごとにクレジット表記が必要で、表記漏れは利用者の事故に直結する。
// 台本から機械的に集計することでその事故を防ぐのは、この CLI の重要な役割と位置づけている。
type Credits struct {
	// DurationInFrames はクレジットシーンの尺。0 は表示しないことを表す（→ issue #17）。
	// 置く位置は最後のシーンの直後と決まっているので、開始位置は持たない。
	// Entries は 0 でも入っているので、renderer が独自に表示することはできる。
	//
	// 0 になるのは台本が meta.creditsScene: false で切っている場合と、載せる表記が 1 件も無い場合。
	// それ以外では表記の件数から CLI が決める（→ CreditsBaseMs / CreditsPerEntryMs）。
	// クレジットシーンは繋ぎを持たない。持たせると尺の式に引き算が増え、
	// Meta.DurationInFrames との関係が足し算のままでなくなるためである。
	DurationInFrames int `json:"durationInFrames"`

	// Entries はクレジット表記の並び。台本での登場順で、重複は取り除いてある。
	Entries []Entry `json:"entries"`
}

// Entry は音声ライブラリ1つ分のクレジット。
//
// 規約が求めるのはキャラクター単位の表記なので、同じ話者の別スタイルを使っていても1件にまとまる。
type Entry struct {
	// Engine は音声合成エンジン。台本の speakers[].engine と同じ値。
	Engine string `json:"engine"`

	// SpeakerName は話者（キャラクター）の名前。
	// エンジンの /speakers から取得した表示名で、台本の話者エイリアスではない。
	SpeakerName string `json:"speakerName"`

	// SpeakerUUID はエンジンが返す話者の UUID。同名の別話者を取り違えないための識別子。
	SpeakerUUID string `json:"speakerUuid,omitempty"`

	// StyleIDs はこの話者について実際に使われたスタイル ID を昇順に並べたもの。
	// 表記には要らないが、意図した声が使われたかを確かめるために残している。
	StyleIDs []int `json:"styleIds"`

	// Text はそのまま表示できるクレジット表記（例: VOICEVOX:ずんだもん）。
	Text string `json:"text"`
}
