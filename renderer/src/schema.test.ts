/**
 * props.json の検証 (parseProps) の契約。
 *
 * ここで検査するのは形そのものではなく、**形が合わなかったときに何が伝わるか**である。
 * props.json は CLI が生成する中間生成物なので、これが弾かれるのは版の食い違いか生成の不具合であり、
 * どちらも利用者には直接手の出せない場所で起きる。そのとき「次に何をすればよいか」が
 * 文面に入っていないと、Studio の赤い画面を見せられて終わりになる。
 */
import {describe, expect, it} from 'vitest';
import {parseProps, SUPPORTED_VERSION} from './schema';

/** 検証を通る最小の props.json。各テストはここから 1 箇所だけ崩す。 */
const validProps = () => ({
	version: SUPPORTED_VERSION,
	$generatedBy: 'scenaremo test',
	meta: {
		title: 'テスト動画',
		aspect: '16:9',
		width: 1920,
		height: 1080,
		fps: 30,
		durationInFrames: 120,
	},
	scenes: [
		{
			image: 'assets/01.png',
			component: 'default',
			durationInFrames: 120,
			transition: {type: 'none', durationInFrames: 0},
			lines: [
				{
					speaker: 'zunda',
					text: 'こんにちは',
					audio: '.scenaremo/audio/abc.wav',
					startFrame: 0,
					durationInFrames: 90,
				},
			],
		},
	],
	credits: {durationInFrames: 60, entries: []},
});

describe('parseProps', () => {
	it('契約に合う props.json を型の付いた値にして返す', () => {
		const parsed = parseProps(validProps());

		expect(parsed.meta.durationInFrames).toBe(120);
		expect(parsed.scenes[0]?.component).toBe('default');
		expect(parsed.scenes[0]?.lines[0]?.startFrame).toBe(0);
	});

	it('scenes が空なら props.json の指定方法を案内する', () => {
		// 実際にここへ来るのは、--props を渡し忘れて Root.tsx の既定値が流れ込んだときである。
		// 「配列が短い」とだけ言われても、起動コマンドを直せばよいとは気づけない。
		const props = {...validProps(), scenes: []};

		expect(() => parseProps(props)).toThrowError(/--props=/);
	});

	it('形が合わない項目は、その場所が分かる形で報告する', () => {
		const props = validProps();
		props.meta.fps = 0;

		expect(() => parseProps(props)).toThrowError(/meta\.fps/);
	});

	it('読めない版は、renderer と scenaremo のどちらを直すのかまで言う', () => {
		// 版の検査は形の検査を通ったあとに行われる。両方を混ぜて報告すると、
		// どちらを直せばよいのか分からなくなるためである。
		const props = {...validProps(), version: SUPPORTED_VERSION + 1};

		expect(() => parseProps(props)).toThrowError(/renderer\/ を新しい scenaremo のものへ更新/);
	});
});
