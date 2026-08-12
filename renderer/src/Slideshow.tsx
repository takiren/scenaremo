import {linearTiming, TransitionSeries} from '@remotion/transitions';
import {fade} from '@remotion/transitions/fade';
import {none} from '@remotion/transitions/none';
import React from 'react';
import {AbsoluteFill} from 'remotion';
import {Scene} from './Scene';
import type {Props, Transition as TransitionData} from './schema';

/**
 * 繋ぎの見せ方を選ぶ。
 *
 * fade は既定のまま（`shouldFadeOutExitingScene` は指定しない）で使う。前のシーンを
 * 出したまま新しいシーンを重ねて濃くしていくので、繋ぎの間ずっと画面は前後どちらかの色で
 * 埋まっている。両方を同時に薄くすると中間だけ下地の黒が透けて、暗く沈む一瞬が生まれる。
 *
 * none は「演出を入れない」ための presentation であって、繋ぎの尺そのものを省くものではない。
 * 総尺の式（Σ シーンの尺 − Σ 繋ぎの尺）は type に依らず成り立っているので、
 * none でも申告されたフレーム数は詰めないと総尺が meta.durationInFrames と食い違い、
 * 末尾が composition の尺で切り落とされる。画は繋ぎの先頭で切り替わる（前のシーンの上に
 * 新しいシーンがそのまま乗るため）。次の声が鳴り始める頃には既に切り替わっている側なので、
 * 「新しい声に古い画が残る」という同期ずれには倒れない。
 *
 * なお現状の CLI は type が none なら尺も 0 にするため、この分岐が効くのは
 * 手書きや将来の props.json で none に尺が付いてきた場合だけである。
 */
const presentationFor = (type: TransitionData['type']) => (type === 'fade' ? fade() : none());

/**
 * メインコンポジション。props.json の scenes を TransitionSeries で並べる。
 *
 * シーンの開始位置を計算しないのは、TransitionSeries が繋ぎのぶん重ねながら
 * 子シーケンスを前へ詰めてくれるからである。props.json が絶対位置を持たないのも同じ理由で、
 * 詰め方を両側で二重に持つと必ずどちらかがずれる（→ README「設計方針 6」）。
 *
 * 尺と解像度は Root.tsx の calculateMetadata が props.json から決めるので、
 * ここでフレーム数を計算し直してはならない（丸めが変わって音がずれる）。
 * scene.durationInFrames は繋ぎのぶんを含んだ値で届くため、そのまま渡すのが正しい。
 */
export const Slideshow: React.FC<Props> = ({scenes}) => {
	return (
		<AbsoluteFill style={{backgroundColor: 'black'}}>
			<TransitionSeries>
				{scenes.map((scene, i) => (
					// TransitionSeries は Transition を Sequence の間に挟む API なので、
					// シーン 1 つを「繋ぎ + シーケンス」の組で表す。Fragment は TransitionSeries 側で
					// 平らに均されるため、子の並びとしては両者が交互に現れる形になる。
					// 台本の並び順がシーンの同一性そのものなので、添字を key にしてよい。
					<React.Fragment key={i}>
						{/*
						 * 繋ぎが 0 フレームなら Transition を置かない。先頭のシーンは繋ぐ相手が
						 * いないので必ず 0 で届き、TransitionSeries も先頭に Transition を置けないため、
						 * この 1 つの条件で「繋ぎ無し」と「先頭は繋げない」の両方を満たせる。
						 */}
						{scene.transition.durationInFrames > 0 ? (
							<TransitionSeries.Transition
								presentation={presentationFor(scene.transition.type)}
								// 申告されたフレーム数をそのまま尺にする timing でなければならない。
								// springTiming のように設定から尺が決まるものを使うと、
								// CLI が確定させた総尺と食い違って音がずれる。
								timing={linearTiming({
									durationInFrames: scene.transition.durationInFrames,
								})}
							/>
						) : null}
						<TransitionSeries.Sequence
							durationInFrames={scene.durationInFrames}
							name={scene.image}
						>
							<Scene scene={scene} />
						</TransitionSeries.Sequence>
					</React.Fragment>
				))}
			</TransitionSeries>
		</AbsoluteFill>
	);
};
