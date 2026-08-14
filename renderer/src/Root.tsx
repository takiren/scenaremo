import React from 'react';
import {Composition} from 'remotion';
import {Slideshow} from './Slideshow';
import {parseProps, SUPPORTED_VERSION, type Props} from './schema';
import {resolveSceneComponent} from './scenes/registry';

/**
 * props.json が渡されなかったときに使われる値。
 *
 * Remotion は `--props` が無いと defaultProps をそのまま流し込む。ここを
 * 「シーンが 1 つも無い」状態にしておくと calculateMetadata の検証に引っかかり、
 * props.json の指定方法を案内するエラーになる（→ schema.ts の scenes のメッセージ）。
 * 適当な見本を描いてしまうと、props.json が届いていないことに気づけない。
 */
const withoutProps: Props = {
	version: SUPPORTED_VERSION,
	meta: {
		// scenes が空であることだけをエラーにしたいので、他の項目は検証を通る値にしておく。
		// ここが空文字だと無関係な違反が並んで、本当の原因が埋もれる。
		title: '(props.json が指定されていません)',
		aspect: '16:9',
		width: 1920,
		height: 1080,
		fps: 30,
		durationInFrames: 1,
	},
	scenes: [],
	credits: {durationInFrames: 0, entries: []},
};

export const RemotionRoot: React.FC = () => {
	return (
		<Composition
			id="Slideshow"
			component={Slideshow}
			defaultProps={withoutProps}
			/**
			 * 解像度も尺も props.json の meta をそのまま採用する。
			 *
			 * 台本にフレーム数を書かせないための要がここ。動画の尺は音声の実測長で決まり、
			 * それを確定させるのは CLI の仕事なので、renderer 側で秒に戻して計算し直しては
			 * ならない（丸めが変わって音がずれる）。解像度も同じ理由で、
			 * aspect から解決する対応表は renderer 側に持たない。
			 */
			calculateMetadata={({props}) => {
				const data = parseProps(props);

				// 描画が始まる前にコンポーネントの解決を試し、未知の名前があればエラーにする。
				// 返り値はここで捨ててよい。
				data.scenes.forEach((scene, i) => {
					try {
						resolveSceneComponent(scene.component);
					} catch (e) {
						if (e instanceof Error) {
							throw new Error(`シーン ${i + 1} (${scene.image}): ${e.message}`);
						}
						throw e;
					}
				});

				return {
					width: data.meta.width,
					height: data.meta.height,
					fps: data.meta.fps,
					durationInFrames: data.meta.durationInFrames,
					// 検証済みの値を下へ流す。以降のコンポーネントは形を疑わなくてよい。
					props: data,
				};
			}}
		/>
	);
};
