import type React from 'react';
import type {SceneProps} from '../schema';
import {DefaultScene} from './DefaultScene';

/**
 * シーンコンポーネントのレジストリ。**利用者が自由に増やす層**。
 *
 * props.json の scenes[].component で指名された名前を、実際の React コンポーネントへ解決する。
 * 台本のスキーマを拡張し続けると YAML が第二のプログラミング言語になって破綻するので、
 * 表現力の限界にはスキーマではなく React 側で答える。その入口がここである
 * （→ README「設計方針 6」／ issue #34）。
 *
 * 自作コンポーネントを足すときは、このオブジェクトへ 1 行加えるだけでよい。
 * 生成されたコードと違って全動画から名前で指名できるので、足したものは資産として残る。
 * MVP 時点のエントリは default のみ（→ docs/custom-components.md）。
 */
export const sceneRegistry: Record<string, React.FC<SceneProps>> = {
	default: DefaultScene,
};

/**
 * 名前からコンポーネントを解決する。
 *
 * 未知の名前は既定へ倒さずに投げる。黙って default を描いてしまうと、綴りを間違えた台本が
 * 「なぜか演出が効かない動画」として出てきて、完成するまで気づけないためである。
 *
 * 名前を並べて示すのは、レジストリの中身が利用者ごとに違うからで、
 * 何が使えるのかはコードを開かないと分からない。並びはソートして安定させてある。
 *
 * メッセージは 1 行に収めること。Remotion の CLI は例外の 1 行目しか見出しに出さないため、
 * 改行で続けると肝心の「使える名前」が利用者に届かない（実機で確認済み）。
 */
export const resolveSceneComponent = (name: string): React.FC<SceneProps> => {
	const Component = sceneRegistry[name];
	if (Component === undefined) {
		const availableNames = Object.keys(sceneRegistry).sort().join(', ');
		throw new Error(
			`未知のシーンコンポーネント "${name}" が指定されました。使える名前: ${availableNames}` +
				`（自分で足す場合は renderer/src/scenes/registry.ts に登録してください）`,
		);
	}
	return Component;
};
