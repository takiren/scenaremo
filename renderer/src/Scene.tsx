import React from 'react';
import {AbsoluteFill, Html5Audio, Img, Sequence, staticFile} from 'remotion';
import type {Scene as SceneData} from './schema';

/**
 * シーン 1 つ分の描画。既定の見た目であり、**あなたが書き換える層**。
 *
 * 画像も音声もパスは動画ディレクトリからの相対で入っている。`--public-dir` に動画ディレクトリを
 * 渡して起動するので、そのまま `staticFile()` に通せる（→ README「設計方針 7」）。
 *
 * シーン名で描画を切り替える registry は issue #34、繋ぎの演出は issue #10、
 * 字幕は issue #21 で入る。ここは今のところ「画像を 1 枚敷き、セリフを鳴らす」ことを担う。
 *
 * 音声に remotion の `Audio` ではなく `Html5Audio` を使うのは、4.0.5xx で `Audio` の名が
 * `@remotion/media` の新しい実装へ移り、こちらが非推奨の別名になったため。
 * 中身は同じもので、依存を増やさずに済む。
 */
export const Scene: React.FC<{scene: SceneData}> = ({scene}) => {
	return (
		<AbsoluteFill style={{backgroundColor: 'black'}}>
			<Img
				src={staticFile(scene.image)}
				style={{width: '100%', height: '100%', objectFit: 'cover'}}
			/>
			{scene.lines.map((line, i) => (
				<Sequence
					// 台本の並び順がセリフの同一性そのものなので、添字を key にしてよい。
					key={i}
					/**
					 * startFrame はシーンの先頭からの相対位置なので、シーンの Sequence の中にいる
					 * ここではそのまま渡せる（→ README「設計方針 6」）。音の位置を fps で割り戻して
					 * 組み直してはならない。CLI が確定させた丸めを再現できず、かえってずれる。
					 */
					from={line.startFrame}
					/**
					 * durationInFrames は実測長を切り上げた値なので、この Sequence が音の末尾を
					 * 切ることはない。逆にここを省くと、次のセリフや次のシーンへ音が食い込む。
					 */
					durationInFrames={line.durationInFrames}
					// 音は目に見えないので、既定の絶対配置 div を挟んでも画像の上に空の層が乗るだけ。
					layout="none"
					name={`${line.speaker}: ${line.text}`}
				>
					<Html5Audio src={staticFile(line.audio)} />
				</Sequence>
			))}
		</AbsoluteFill>
	);
};
