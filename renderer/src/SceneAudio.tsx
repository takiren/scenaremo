import React from 'react';
import {Html5Audio, Sequence, staticFile} from 'remotion';
import type {SceneProps} from './schema';

/**
 * セリフ音声を鳴らす共通レイヤー。
 * シーンコンポーネントから分離されているため、利用者が自作コンポーネントを書いても
 * 音声再生の処理を気にせずに済む。
 *
 * 音声に remotion の `Audio` ではなく `Html5Audio` を使うのは、4.0.5xx で `Audio` の名が
 * `@remotion/media` の新しい実装へ移り、こちらが非推奨の別名になったため。
 * 中身は同じもので、依存を増やさずに済む。
 */
export const SceneAudio: React.FC<SceneProps> = ({scene}) => {
	return (
		<>
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
		</>
	);
};
