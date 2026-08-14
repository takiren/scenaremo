import React from 'react';
import {AbsoluteFill, Img, staticFile} from 'remotion';
import type {SceneProps} from '../schema';

/**
 * シーン 1 つ分の描画。既定の見た目であり、**あなたが書き換える層**。
 * registry へ自作コンポーネントを足すときの雛形でもある（→ docs/custom-components.md）。
 *
 * 画像のパスは動画ディレクトリからの相対で入っている。`--public-dir` に動画ディレクトリを
 * 渡して起動するので、そのまま `staticFile()` に通せる（→ README「設計方針 7」）。
 *
 * セリフ音声も字幕もここでは描かない。シーンコンポーネントの中でやると、利用者が自作の
 * コンポーネントを 1 つ書いた瞬間にそのシーンだけ無音になったり字幕が消えたりして、
 * 「演出だけ書けばよい」という逃げ道が成り立たなくなる。どちらも共通レイヤー
 * (SceneAudio.tsx / Subtitle.tsx) の担当である（→ issue #34 / issue #21）。
 *
 * 繋ぎの演出は issue #10 で入る。ここは今のところ「画像を 1 枚敷く」ことだけを担う。
 */
export const DefaultScene: React.FC<SceneProps> = ({scene}) => {
	return (
		<AbsoluteFill style={{backgroundColor: 'black'}}>
			<Img
				src={staticFile(scene.image)}
				style={{width: '100%', height: '100%', objectFit: 'cover'}}
			/>
		</AbsoluteFill>
	);
};
