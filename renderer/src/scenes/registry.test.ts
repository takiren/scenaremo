/**
 * シーンコンポーネントレジストリの契約（→ issue #34）。
 *
 * ここで確かめているのは「台本から名前でコンポーネントを指名できること」と、
 * 「間違った名前を書いたときに、次に何をすればよいか分かるエラーが出ること」の 2 つ。
 * レジストリは利用者が自由に増やす層なので、名前の解決に失敗したときの案内が
 * この機構の使い勝手そのものになる。
 *
 * 個々のコンポーネントの見た目は Remotion の描画コンテキストが要るためここでは検査しない。
 * 検査するのは registry の解決規則だけで、これは純粋な関数なので描画なしで確かめられる。
 */
import type {FC} from 'react';
import {describe, expect, it} from 'vitest';
import type {SceneProps} from '../schema';
import {DefaultScene} from './DefaultScene';
import {resolveSceneComponent, sceneRegistry} from './registry';

describe('sceneRegistry', () => {
	it('既定のエントリとして default に DefaultScene を持つ', () => {
		// props.json の scenes[].component は CLI が既定値 "default" を埋めて渡してくる。
		// このキーが欠けると、台本が component を書いていない普通の動画が描けなくなる。
		expect(sceneRegistry.default).toBe(DefaultScene);
	});

	it('レジストリは名前から SceneProps を受け取るコンポーネントへの対応表である', () => {
		// 型の側の契約。利用者がここへ自作コンポーネントを足すときに従うべき形を固定する。
		const registry: Record<string, FC<SceneProps>> = sceneRegistry;
		expect(Object.keys(registry).length).toBeGreaterThan(0);
	});

	it('DefaultScene は SceneProps を受け取る React コンポーネントである', () => {
		const component: FC<SceneProps> = DefaultScene;
		expect(typeof component).toBe('function');
	});
});

describe('resolveSceneComponent', () => {
	it('default を DefaultScene に解決する', () => {
		expect(resolveSceneComponent('default')).toBe(DefaultScene);
	});

	it('登録されているすべての名前を、その名前のコンポーネントに解決する', () => {
		for (const [name, component] of Object.entries(sceneRegistry)) {
			expect(resolveSceneComponent(name)).toBe(component);
		}
	});

	it('未知の名前は default へ暗黙にフォールバックしない', () => {
		// 黙って既定へ倒すと、綴りを間違えた台本が「なぜか演出が効かない動画」になって出てくる。
		// 気づけないまま完成品になるより、その場で止まるほうがよい。
		expect(() => resolveSceneComponent('zoooom')).toThrow();
	});

	it('未知の名前のエラーは、その名前・使える名前の一覧・追加する場所を示す', () => {
		let message = '';
		try {
			resolveSceneComponent('zoooom');
		} catch (error) {
			message = (error as Error).message;
		}

		// 何が間違っていたのか。
		expect(message).toContain('zoooom');
		// 代わりに何が書けるのか（利用可能な名前を列挙する）。
		for (const name of Object.keys(sceneRegistry)) {
			expect(message).toContain(name);
		}
		// 自分で足したい場合にどこを開けばよいのか。
		expect(message).toContain('renderer/src/scenes/registry.ts');
	});

	it('未知の名前のエラーは 1 行に収まっている', () => {
		// Remotion の CLI は例外の 1 行目だけを見出しに出す（実機で確認済み）。
		// 改行で続けると、いちばん伝えたい「使える名前」がそこで切り落とされて利用者に届かない。
		expect(() => resolveSceneComponent('zoooom')).toThrowError(
			expect.objectContaining({message: expect.not.stringContaining('\n')}),
		);
	});
});
