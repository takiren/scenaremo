import React from 'react';
import {AbsoluteFill, Img, staticFile} from 'remotion';
import type {Scene as SceneData} from './schema';

/**
 * シーン 1 つ分の描画。既定の見た目であり、**あなたが書き換える層**。
 *
 * 画像のパスは動画ディレクトリからの相対で入っている。`--public-dir` に動画ディレクトリを
 * 渡して起動するので、そのまま `staticFile()` に通せる（→ README「設計方針 7」）。
 *
 * シーン名で描画を切り替える registry は issue #34、繋ぎの演出は issue #10、
 * 字幕は issue #21 で入る。ここは今のところ「画像を 1 枚敷く」だけを担う。
 */
export const Scene: React.FC<{scene: SceneData}> = ({scene}) => {
	return (
		<AbsoluteFill style={{backgroundColor: 'black'}}>
			<Img
				src={staticFile(scene.image)}
				style={{width: '100%', height: '100%', objectFit: 'cover'}}
			/>
		</AbsoluteFill>
	);
};
