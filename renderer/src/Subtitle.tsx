import React from 'react';
import {AbsoluteFill, Sequence, useVideoConfig} from 'remotion';
import {captionOf, type Scene, type Speakers} from './schema';

/**
 * 話者に色が指定されていないときの字幕の色。
 *
 * 既定色を CLI ではなくここに置いているのは、見た目の既定が renderer の持ち物だからである。
 * 台本の speakers[].color は「この既定を上書きするノブ」であって、色の決定そのものではない。
 */
export const DEFAULT_SUBTITLE_COLOR = '#FFFFFF';

/** 1 行ぶんの字幕が画面に出ている区間。位置はシーンの先頭からの相対フレーム。 */
export type SubtitleSpan = {
	/** シーンの先頭から数えて、この字幕が出るフレーム。 */
	from: number;
	/** 出ているフレーム数。 */
	durationInFrames: number;
	/** 表示する文章（props.json の caption。無い版なら text へ倒したもの）。 */
	text: string;
	/** 文字色。話者に色が無ければ既定色。 */
	color: string;
	/** 話者エイリアス。Studio のタイムラインに出す名前に使う。 */
	speaker: string;
};

/**
 * シーンのセリフを、字幕を出す区間の並びへ直す。
 *
 * **セリフが鳴っている間だけ出すのではなく、次のセリフが始まるまで出しっぱなしにする。**
 * セリフの間には余白 (defaults.gapMs) があり、シーンの尻にも余白 (defaults.sceneGapMs) が付くので、
 * 音の尺にぴったり合わせると 0.3 秒ごとに字幕が消えて出る画になる。読んでいる途中で消えるほうが、
 * 少し長く残るより読み手には辛い。
 *
 * 最初のセリフの startFrame は繋ぎの尺と一致するので、フェードで入ってくる間は字幕が出ない。
 * 前のシーンの画に次のシーンの字幕が乗るのを避けるための位置取りで、これは CLI 側の契約で決まっている。
 *
 * 描画から切り離してあるのは、ここが字幕の唯一の時間軸だからである。
 * Remotion の描画コンテキスト無しで確かめられる形にしておくと、タイミングの規則を試験で固定できる。
 */
export const subtitleSpans = (scene: Scene, speakers?: Speakers): SubtitleSpan[] => {
	return scene.lines.map((line, i) => {
		const next = scene.lines[i + 1];
		// 最後のセリフはシーンの終わりまで残す。次のシーンが繋ぎで重なってくるので、
		// フェードの間も字幕は前のシーンの一部として一緒に消えていく。
		const end = next ? next.startFrame : scene.durationInFrames;
		return {
			from: line.startFrame,
			// 手書きの props.json で順序が崩れていても Sequence を壊さないよう、最低 1 フレームは確保する。
			durationInFrames: Math.max(1, end - line.startFrame),
			// 前後の空白と改行は落とす。YAML のブロックスカラー (`text: |`) は末尾に必ず改行を残すため、
			// そのまま pre-wrap で描くと帯の下に空の行が 1 つ入り、字幕が上へ寄って見える。
			// props.json 側は台本のままにしておく（落とすのは見せ方の都合なので renderer の仕事）。
			text: captionOf(line).trim(),
			color: speakers?.[line.speaker]?.color ?? DEFAULT_SUBTITLE_COLOR,
			speaker: line.speaker,
		};
	});
};

/**
 * 字幕の共通レイヤーであり、**あなたが書き換える層**（→ README「設計方針 1」）。
 *
 * 画面の一等地は字幕に渡す。解説動画で扱うのは視聴者が聞いたことのない言葉であり、
 * 知らない言葉は音だけでは聞き取れないためである（→ issue #21）。
 *
 * SceneAudio と同じくシーンコンポーネントの外に置いてある。中で描くことにすると、
 * 利用者が自作コンポーネントを 1 つ書いた瞬間にそのシーンだけ字幕が消え、
 * 「演出だけ書けばよい」という逃げ道が成り立たなくなる。
 *
 * 話者は色で区別する。名前ラベル（「ずんだもん:」）は横幅を食うためで、色は props.json の
 * speakers から引く。**色だけに意味を持たせないこと。** 色覚特性によっては区別できない人がいるので、
 * 誰が喋っているかがその場面で本質的な情報なら、そのことは台本の文面でも示すこと。
 *
 * 動きは付けない。読む対象が動くと視線がそれを追ってしまい、画のほうを見られなくなる。
 * モーラ単位のカラオケ表示 (#20) を入れないのも同じ理由である。
 */
export const Subtitle: React.FC<{scene: Scene; speakers?: Speakers}> = ({scene, speakers}) => {
	const {width, height} = useVideoConfig();

	/*
	 * 文字の大きさは短辺から決める。16:9 と 9:16 で同じ見え方にするためで、
	 * 高さだけを基準にすると縦型で文字が画面幅からはみ出す（Credits.tsx と同じ決め方）。
	 */
	const fontSize = Math.min(width, height) * 0.055;
	/*
	 * 下端に貼り付けない。プレイヤーのコントロールバーが重なる帯があり、
	 * 完全な最下部に置くと再生中だけ字幕が隠れる。
	 */
	const bottom = height * 0.08;
	// 縁取りの太さも文字の大きさに比例させる。解像度が変わっても輪郭の見え方が変わらない。
	const outline = Math.max(1, Math.round(fontSize * 0.06));

	return (
		<>
			{subtitleSpans(scene, speakers).map((span, i) => (
				<Sequence
					// 台本の並び順がセリフの同一性そのものなので、添字を key にしてよい。
					key={i}
					from={span.from}
					durationInFrames={span.durationInFrames}
					// 位置は中の AbsoluteFill が持つので、Sequence 側の既定の層は要らない。
					layout="none"
					name={`字幕 ${span.speaker}: ${span.text}`}
				>
					<AbsoluteFill
						style={{
							justifyContent: 'flex-end',
							alignItems: 'center',
							paddingBottom: bottom,
						}}
					>
						<div
							style={{
								/*
								 * 背景素材がどんな色でも読めるように、帯と縁取りの両方を掛ける。
								 * 帯だけでは明るい画像の上で沈み、縁取りだけでは細かい模様の上で溶ける。
								 */
								backgroundColor: 'rgba(0, 0, 0, 0.6)',
								borderRadius: fontSize * 0.25,
								padding: `${fontSize * 0.3}px ${fontSize * 0.7}px`,
								// 画面いっぱいには広げない。端まで届く行は視線の移動が長くなって読みにくい。
								maxWidth: '84%',
								color: span.color,
								fontFamily: 'sans-serif',
								fontSize,
								fontWeight: 700,
								lineHeight: 1.4,
								textAlign: 'center',
								/*
								 * 台本に書かれた改行はそこで折り、長い行は表示幅で自動的に折り返す。
								 * 長すぎる行をこちらで切り詰めることはしない。行の途中で切るには
								 * 行内のタイミングが要り、それは台本側で「1 行を短く書く」のが正しい直し方である。
								 */
								whiteSpace: 'pre-wrap',
								overflowWrap: 'break-word',
								textShadow: [
									`${outline}px ${outline}px 0 rgba(0, 0, 0, 0.9)`,
									`${-outline}px ${outline}px 0 rgba(0, 0, 0, 0.9)`,
									`${outline}px ${-outline}px 0 rgba(0, 0, 0, 0.9)`,
									`${-outline}px ${-outline}px 0 rgba(0, 0, 0, 0.9)`,
								].join(', '),
							}}
						>
							{span.text}
						</div>
					</AbsoluteFill>
				</Sequence>
			))}
		</>
	);
};
