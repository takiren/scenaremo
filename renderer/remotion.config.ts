/**
 * Remotion CLI の設定。
 *
 * ここに書けるのは全動画に共通する設定だけ。動画ごとに変わるもの（public dir、props、
 * 出力先）は CLI が起動時に渡すので、このファイルには置かない。
 * 解像度と尺は props.json から calculateMetadata が決めるため、ここでは触らない。
 */
import {Config} from '@remotion/cli/config';

// 出力先が既にある場合に上書きする。同じ動画を何度も作り直すため。
Config.setOverwriteOutput(true);
