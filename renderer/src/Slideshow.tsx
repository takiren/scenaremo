import React from 'react';
import {AbsoluteFill, Sequence} from 'remotion';
import {Scene} from './Scene';
import type {Props, Scene as SceneData} from './schema';

/**
 * シーンを動画の先頭からの絶対位置に直す。
 *
 * props.json のシーンは「尺」しか持たない。開始位置を持たないのは、シーンを
 * TransitionSeries で並べる前提だからで、TransitionSeries は隣り合うシーケンスを
 * 繋ぎのぶん重ねて前へ詰めるため、絶対位置を渡しても意味を成さないためである。
 *
 * その詰め方をここで再現する。シーン i は、それより前のシーンの尺の合計から、
 * **自分の繋ぎまでを含めた**繋ぎの合計を引いた位置から始まる。
 * 自分の繋ぎを含めるのは、繋ぎのぶんだけ自分が前へ食い込むのが TransitionSeries の挙動だから。
 *
 * 結果として最後のシーンの終端は meta.durationInFrames と一致し、
 * あるシーンの最後のセリフが終わった瞬間に次のシーンの最初のセリフが始まる。
 *
 * issue #10 で TransitionSeries に置き換えると、この計算は TransitionSeries 側が持つ。
 */
export const sceneOffsets = (scenes: SceneData[]): number[] => {
	const offsets: number[] = [];
	let durations = 0;
	let transitions = 0;
	for (const scene of scenes) {
		transitions += scene.transition.durationInFrames;
		offsets.push(durations - transitions);
		durations += scene.durationInFrames;
	}
	return offsets;
};

/**
 * メインコンポジション。props.json の scenes をそのまま時間軸に並べる。
 *
 * 尺と解像度は Root.tsx の calculateMetadata が props.json から決めるので、
 * ここでフレーム数を計算し直してはならない（丸めが変わって音がずれる）。
 */
export const Slideshow: React.FC<Props> = ({scenes}) => {
	const offsets = sceneOffsets(scenes);

	return (
		<AbsoluteFill style={{backgroundColor: 'black'}}>
			{scenes.map((scene, i) => (
				<Sequence
					// 台本の並び順がシーンの同一性そのものなので、添字を key にしてよい。
					key={i}
					from={offsets[i]}
					durationInFrames={scene.durationInFrames}
					name={scene.image}
				>
					<Scene scene={scene} />
				</Sequence>
			))}
		</AbsoluteFill>
	);
};
