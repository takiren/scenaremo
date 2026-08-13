import React from 'react';
import {AbsoluteFill, interpolate, useCurrentFrame, useVideoConfig} from 'remotion';
import type {Credits as CreditsData} from './schema';

/**
 * クレジットシーンの既定の見た目であり、**あなたが書き換える層**（→ README「設計方針 1」）。
 *
 * VOICEVOX は音声ライブラリごとのクレジット表記を求めており、表記漏れは利用者の事故に直結する。
 * そこで CLI が台本から機械的に集計し、既定でこのシーンを動画末尾へ入れる。
 * 台本側で切りたい場合は `meta.creditsScene: false`（→ issue #17）。
 *
 * 差し替えは Scene.tsx と同じく「このファイルを書き換える」形にした。台本から名前で指名する形
 * （`scenes[].component` と renderer 側 registry、→ issue #34）に合わせなかったのは、
 * その registry がまだ無く、指名先の無いキーを props.json の契約へ先に足すと、
 * 読み手はそれが効くものと誤解するためである。registry が入ったらそちらへ寄せればよく、
 * そのときも「クレジットは最後のシーンの直後」という props.json の契約は変わらない。
 *
 * 尺はここでは決めない。props.json の credits.durationInFrames が唯一の正で、
 * このコンポーネントはその Sequence の中に置かれるだけである（→ README「設計方針 2」）。
 *
 * 見出し（「クレジット」など）を置いていないのは、規約が求めているのは表記そのものであり、
 * 見出しの文言は動画の言語や作風で変わるためである。要るなら足せばよい層に置いてある。
 */
export const Credits: React.FC<{credits: CreditsData}> = ({credits}) => {
	const frame = useCurrentFrame();
	const {fps, width, height} = useVideoConfig();

	/*
	 * 文字の大きさは短辺から決める。16:9 と 9:16 で同じ見え方にするためで、
	 * 高さだけを基準にすると縦型で文字が画面幅からはみ出す。
	 * 解像度そのものは props.json の meta が持っており、ここで aspect から解決し直してはいない。
	 */
	const fontSize = Math.min(width, height) * 0.05;

	/*
	 * 前のシーンからは繋ぎ無しで切り替わるので、文字だけを短く浮かび上がらせる。
	 * これは Sequence の中の見た目の変化であって尺ではない。総尺は props.json が決めたままで、
	 * ここで何フレーム使おうとクレジットシーンの長さは 1 フレームも変わらない。
	 * 400ms は既定のトランジションと同じ長さに合わせてある。
	 */
	const opacity = interpolate(frame, [0, Math.round(fps * 0.4)], [0, 1], {
		extrapolateLeft: 'clamp',
		extrapolateRight: 'clamp',
	});

	return (
		<AbsoluteFill
			style={{
				backgroundColor: 'black',
				alignItems: 'center',
				justifyContent: 'center',
				opacity,
			}}
		>
			<div
				style={{
					display: 'flex',
					flexDirection: 'column',
					alignItems: 'center',
					gap: fontSize * 0.6,
					padding: fontSize * 2,
					color: 'white',
					fontFamily: 'sans-serif',
					fontSize,
					fontWeight: 600,
					lineHeight: 1.4,
					textAlign: 'center',
				}}
			>
				{credits.entries.map((entry, i) => (
					// 並びは台本での登場順で CLI が確定させている。順序がそのまま同一性なので添字を key にしてよい。
					// text は「そのまま表示できる表記」として届くため、ここで engine と名前を繋ぎ直さない
					// （繋ぎ方を 2 箇所に持つと、規約側の表記が変わったときに片方だけ古くなる）。
					<div key={i}>{entry.text}</div>
				))}
			</div>
		</AbsoluteFill>
	);
};
