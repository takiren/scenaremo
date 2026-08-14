/**
 * 字幕の時間軸と色の解決の契約（→ issue #21）。
 *
 * 見た目そのもの（帯の濃さや文字の大きさ）は書き換える前提の層なので試験しない。
 * 固定するのは「いつ出ていつ消えるか」と「どの色になるか」の 2 つで、
 * どちらも props.json の値から機械的に決まる部分である。
 *
 * 描画には Remotion のコンテキストが要るため、時間軸の計算だけを純粋な関数として
 * 切り出してある（→ subtitleSpans）。ここで確かめるのはその関数である。
 */
import {describe, expect, it} from 'vitest';
import type {Scene, Speakers} from './schema';
import {DEFAULT_SUBTITLE_COLOR, subtitleSpans} from './Subtitle';

/** セリフ 2 つのシーン。繋ぎ 12F・セリフ 30F・余白 9F・セリフ 30F・シーン末尾の余白 15F。 */
const scene = (): Scene => ({
	image: 'assets/01.png',
	component: 'default',
	durationInFrames: 96,
	transition: {type: 'fade', durationInFrames: 12},
	lines: [
		{
			speaker: 'zunda',
			text: 'クーベルネティスの話をするのだ',
			caption: 'Kubernetes の話をするのだ',
			audio: '.scenaremo/audio/a.wav',
			startFrame: 12,
			durationInFrames: 30,
		},
		{
			speaker: 'metan',
			text: 'コンテナを束ねる仕組みですね',
			caption: 'コンテナを束ねる仕組みですね',
			audio: '.scenaremo/audio/b.wav',
			startFrame: 51,
			durationInFrames: 30,
		},
	],
});

describe('subtitleSpans', () => {
	it('セリフごとに 1 つの字幕を作る', () => {
		expect(subtitleSpans(scene())).toHaveLength(2);
	});

	it('字幕はセリフの音と同時に出る', () => {
		// 最初のセリフの startFrame は繋ぎの尺と一致する契約なので、
		// フェードで入ってくる間は字幕が出ない（前のシーンの画に次の字幕が乗らない）。
		const spans = subtitleSpans(scene());

		expect(spans[0]?.from).toBe(12);
		expect(spans[1]?.from).toBe(51);
	});

	it('次のセリフが始まるまで出しっぱなしにする', () => {
		// 音の尺 (30F) ではなく次のセリフまで (39F) 出る。セリフの間には余白があり、
		// 音にぴったり合わせると余白のたびに字幕が消えて出る画になる。
		const spans = subtitleSpans(scene());

		expect(spans[0]?.durationInFrames).toBe(39);
	});

	it('最後のセリフはシーンの終わりまで残る', () => {
		// シーンの尻には余白があるので、音の尺で切ると次のシーンへ移る前に一瞬だけ字幕が消える。
		const spans = subtitleSpans(scene());

		expect(spans[1]?.durationInFrames).toBe(96 - 51);
	});

	it('字幕には caption を出す', () => {
		// 読み上げた文字列 (text) は読み仮名でありうる。それを字幕に出すと視聴者は綴りを
		// 受け取れず、言葉を検索できない。字幕を入れる理由そのものが失われる。
		const spans = subtitleSpans(scene());

		expect(spans[0]?.text).toBe('Kubernetes の話をするのだ');
	});

	it('caption が無い props.json では text を出す', () => {
		// この項目より前に作られた props.json を読んだ場合。字幕が消えるより、
		// 読み仮名がそのまま出るほうがまだましである。
		const s = scene();
		delete s.lines[0]?.caption;

		expect(subtitleSpans(s)[0]?.text).toBe('クーベルネティスの話をするのだ');
	});

	it('前後の空白と改行は落とす', () => {
		// YAML のブロックスカラー (`text: |`) は末尾に必ず改行を残す。台本のまま描くと
		// 帯の下に空の行が 1 つ入り、字幕が上へ寄って見える。行の途中の改行は残す。
		const s = scene();
		s.lines[0]!.caption = 'スライドショー形式の\n解説動画を作りますね\n';

		expect(subtitleSpans(s)[0]?.text).toBe('スライドショー形式の\n解説動画を作りますね');
	});

	it('話者の色を字幕の色にする', () => {
		// 立ち絵が無い構成では、話者が切り替わったことが字幕でしか分からない。
		const speakers: Speakers = {zunda: {color: '#69C6A0'}, metan: {color: '#E86B8F'}};
		const spans = subtitleSpans(scene(), speakers);

		expect(spans[0]?.color).toBe('#69C6A0');
		expect(spans[1]?.color).toBe('#E86B8F');
	});

	it('色が無い話者と、speakers 自体が無い props.json は既定色にする', () => {
		// 色は補助であって、字幕が出ること自体は色の指定に依存しない。
		const spans = subtitleSpans(scene(), {zunda: {}});

		expect(spans[0]?.color).toBe(DEFAULT_SUBTITLE_COLOR);
		expect(spans[1]?.color).toBe(DEFAULT_SUBTITLE_COLOR);
		expect(subtitleSpans(scene())[0]?.color).toBe(DEFAULT_SUBTITLE_COLOR);
	});

	it('尺が 0 以下になる並びでも 1 フレームは確保する', () => {
		// Sequence は尺 0 を受け付けない。手で編集した props.json で順序が崩れていても、
		// 字幕の実装が例外で落ちるより、その 1 行が一瞬出るほうが原因を追いやすい。
		const s = scene();
		s.durationInFrames = 51;

		expect(subtitleSpans(s)[1]?.durationInFrames).toBe(1);
	});
});
