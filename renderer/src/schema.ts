/**
 * props.json の zod スキーマ。
 *
 * スキーマの唯一の正は `docs/props.schema.json` であり、この定義はそれに追従する。
 * 項目を足し引きするときは、必ず先に `docs/props.schema.json` を直すこと。
 * Go 側の型 (`internal/props`) も同じファイルに従うため、ここだけ直すと静かに乖離する
 * （乖離を CI で検出する仕組みは issue #25）。
 *
 * ここで検証するのは「CLI が生成した中間生成物」であって人間の入力ではない。
 * それでも検証するのは、版の食い違いや生成の不具合を、意味不明な描画結果ではなく
 * 読めるエラーとして表に出すためである。
 */
import {z} from 'zod';

/** この renderer が読める props.json の版。 */
export const SUPPORTED_VERSION = 1;

export const transitionSchema = z.object({
	/** 繋ぎ方。none は繋ぎの演出を入れない。 */
	type: z.enum(['fade', 'none']),
	/**
	 * 繋ぎに掛けるフレーム数。0 は繋ぎ無しで、先頭のシーンは繋ぐ相手がいないので必ず 0 になる。
	 *
	 * この値をそのまま使う timing を選ぶこと（`linearTiming({durationInFrames})` など）。
	 * `springTiming` のように設定から尺が決まるものを使うと、CLI の申告と食い違って音がずれる。
	 */
	durationInFrames: z.number().int().min(0),
});

export const lineSchema = z.object({
	/** 話者エイリアス。台本で省略されていた場合は既定値が解決済みで入っている。 */
	speaker: z.string().min(1),
	/** 読み上げた文章。改行も台本のまま保持されている。字幕 (issue #21) はこれを使う。 */
	text: z.string().min(1),
	/** 合成された wav のパス。動画ディレクトリからの相対で、`staticFile()` にそのまま渡せる。 */
	audio: z.string().min(1),
	/**
	 * 音声が鳴り始めるフレーム。**シーンの先頭からの相対位置**で、動画の先頭からではない。
	 * シーンの Sequence の中にそのまま置ける。
	 */
	startFrame: z.number().int().min(0),
	/** このセリフに与えられたフレーム数。音声の実測長を切り上げた値。 */
	durationInFrames: z.number().int().min(1),
});

export const sceneSchema = z.object({
	/** 表示する画像のパス。動画ディレクトリからの相対で、`staticFile()` にそのまま渡せる。 */
	image: z.string().min(1),
	/** 描画に使うコンポーネント名。registry のキーで、既定は default（registry 本体は issue #34）。 */
	component: z.string().min(1),
	/** component に渡す任意のプロパティ。CLI は中身を検証せず素通しする。 */
	props: z.record(z.string(), z.unknown()).optional(),
	/**
	 * TransitionSeries.Sequence へ渡す尺。喋りの尺そのものではなく、
	 * そこへ transition.durationInFrames を足した値になっている。
	 */
	durationInFrames: z.number().int().min(1),
	transition: transitionSchema,
	lines: z.array(lineSchema).min(1),
});

export const creditEntrySchema = z.object({
	engine: z.string().min(1),
	/** 話者（キャラクター）の名前。台本の話者エイリアスではない。 */
	speakerName: z.string().min(1),
	speakerUuid: z.string().optional(),
	styleIds: z.array(z.number().int().min(0)),
	/** そのまま表示できるクレジット表記（例: `VOICEVOX:ずんだもん`）。 */
	text: z.string().min(1),
});

export const creditsSchema = z.object({
	/**
	 * クレジットシーンの尺。0 は表示しないことを表す（台本の `meta.creditsScene: false`、
	 * または載せる表記が 1 件も無い場合）。置く位置は最後のシーンの直後で固定なので開始位置は持たない。
	 *
	 * 0 でも entries は入っている。「表示しない」のは既定のクレジットシーンだけであって、
	 * renderer が独自に表示する道は塞がない。
	 */
	durationInFrames: z.number().int().min(0),
	entries: z.array(creditEntrySchema),
});

export const metaSchema = z.object({
	title: z.string().min(1),
	aspect: z.enum(['16:9', '9:16']),
	/** 解像度は CLI が aspect から解決済み。renderer は対応表を持たない。 */
	width: z.number().int().min(1),
	height: z.number().int().min(1),
	fps: z.number().int().min(1).max(240),
	/** 動画全体の総フレーム数。クレジットの分も含む。calculateMetadata はこれをそのまま採用する。 */
	durationInFrames: z.number().int().min(1),
});

export const propsSchema = z.object({
	version: z.number().int().min(1),
	$generatedBy: z.string().optional(),
	$note: z.string().optional(),
	meta: metaSchema,
	scenes: z
		.array(sceneSchema)
		// props.json が渡されていないときは既定値の空配列がここへ来る。
		// 実際に起きるのはほぼその場合なので、指定方法をそのままエラー文にしている。
		.min(1, 'シーンが 1 つもありません。--props=videos/<id>/.scenaremo/props.json を指定して起動してください'),
	credits: creditsSchema,
});

export type Transition = z.infer<typeof transitionSchema>;
export type Line = z.infer<typeof lineSchema>;
export type Scene = z.infer<typeof sceneSchema>;
export type CreditEntry = z.infer<typeof creditEntrySchema>;
export type Credits = z.infer<typeof creditsSchema>;
export type Meta = z.infer<typeof metaSchema>;
export type Props = z.infer<typeof propsSchema>;

/**
 * props.json を検証して型の付いた値にする。
 *
 * 失敗したら投げる。Remotion は calculateMetadata が投げた例外をそのまま画面と
 * 終了コードに出すため、ここで人間に読める文言にしておくと、
 * Studio でもコマンドラインでも同じ説明が届く。
 */
export const parseProps = (input: unknown): Props => {
	const parsed = propsSchema.safeParse(input);
	if (!parsed.success) {
		const detail = parsed.error.issues
			.map((issue) => `  - ${issue.path.join('.') || '(root)'}: ${issue.message}`)
			.join('\n');
		throw new Error(`props.json の形が契約と合っていません:\n${detail}`);
	}

	// 版の検査は形の検査を通ったあとに行う。
	// 「版が違う」と「形が壊れている」を混ぜて報告しても、どちらを直せばよいのか分からないため。
	const {version} = parsed.data;
	if (version !== SUPPORTED_VERSION) {
		throw new Error(
			version > SUPPORTED_VERSION
				? `props.json の版 ${version} は、この renderer (対応する版: ${SUPPORTED_VERSION}) では読めません。renderer/ を新しい scenaremo のものへ更新してください。`
				: `props.json の版 ${version} は、この renderer (対応する版: ${SUPPORTED_VERSION}) では読めません。scenaremo build で生成し直してください。`,
		);
	}

	return parsed.data;
};
