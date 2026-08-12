// Package audio は WAV ファイルの再生時間を実測する。
//
// 動画の尺は合成した音声の実測長で決まるため、ここでの計測結果がタイムライン計算の入力になる。
// 計測に ffmpeg / ffprobe は使わない。利用者に外部バイナリのインストールを要求しないためで、
// 対象は VOICEVOX が返す WAV だけなので go-audio/wav で足りる。
package audio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/go-audio/wav"
)

// ErrInvalidWAV は WAV として読み取れないファイルを表す。
// 呼び出し側は errors.Is(err, audio.ErrInvalidWAV) で「ファイルが壊れている」ことを判別できる。
// ファイルが存在しない等の I/O エラーはラップせずに返すため、errors.Is(err, fs.ErrNotExist) も使える。
var ErrInvalidWAV = errors.New("WAV ファイルとして読み取れません")

// Info は WAV の書式と実測長をまとめたもの。
type Info struct {
	// Duration は data チャンクの実サイズから求めた再生時間。
	Duration time.Duration
	// SampleRate はサンプリング周波数 (Hz)。VOICEVOX の既定は 24000。
	SampleRate int
	// NumChannels はチャンネル数。VOICEVOX の既定は 1 (モノラル)。
	NumChannels int
	// BitDepth は 1 サンプルあたりのビット数。VOICEVOX の既定は 16。
	BitDepth int
	// PCMBytes は data チャンクのバイト数。
	PCMBytes int64
}

// Duration は path の WAV の再生時間を返す。
//
// タイムライン計算はこの値を起点にフレーム数を決める。書式まで必要な場合は Measure を使う。
// 既にメモリ上にある wav を測るなら MeasureBytes のほうが素直（ディスクへの往復が要らない）。
func Duration(path string) (time.Duration, error) {
	info, err := Measure(path)
	if err != nil {
		return 0, err
	}
	return info.Duration, nil
}

// Measure は path の WAV を読み取り、書式と再生時間を返す。
// PCM データそのものはメモリに読み込まないため、ファイルサイズに関係なく一定のコストで済む。
func Measure(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		// os.Open のエラーはパスを含む。%w で包むので errors.Is(err, fs.ErrNotExist) もそのまま効く。
		return Info{}, fmt.Errorf("音声ファイルを開けません: %w", err)
	}
	defer f.Close()

	info, err := MeasureReader(f)
	if err != nil {
		// どのファイルが壊れているのかを分かるようにパスを添える。
		return Info{}, fmt.Errorf("%s: %w", path, err)
	}
	return info, nil
}

// MeasureBytes はメモリ上の WAV バイト列から書式と再生時間を返す。
//
// 音声合成は wav をバイト列で返すため、合成直後の計測はこれを使う。
// 一度ディスクへ書いて読み直す往復が要らず、キャッシュへの書き出しとも独立に測れる。
func MeasureBytes(b []byte) (Info, error) {
	return MeasureReader(bytes.NewReader(b))
}

// MeasureReader は r から WAV を読み取り、書式と再生時間を返す。
// r は WAV の先頭を指している必要がある。
//
// 計測の本体はこの関数で、Duration / Measure / MeasureBytes はいずれもここへ委譲する。
// io.Reader ではなく io.ReadSeeker を要求するのは、先頭のコンテナ検査で読み位置を戻し、
// data チャンクの申告値と実データ量を突き合わせるために末尾へ飛ぶため。
// io.Reader にすると全体をバッファリングする必要が生じ、PCM 本体を読まずに済む利点が失われる。
func MeasureReader(r io.ReadSeeker) (Info, error) {
	if err := checkContainer(r); err != nil {
		return Info{}, err
	}

	d := wav.NewDecoder(r)

	d.ReadInfo()
	if err := d.Err(); err != nil {
		return Info{}, fmt.Errorf("%w: ヘッダを解析できません: %v", ErrInvalidWAV, err)
	}

	// FwdToPCM() は data チャンクの先頭まで読み進め、その過程で PCMSize を設定する。
	// ReadInfo() は fmt チャンクで読み取りを止めるので、これを呼ばないと PCMLen() は常に 0 を返す。
	if err := d.FwdToPCM(); err != nil {
		return Info{}, fmt.Errorf("%w: data チャンクが見つかりません: %v", ErrInvalidWAV, err)
	}
	// FwdToPCM() はヘッダの解析に失敗しても nil を返す実装 (decoder.go: `if d.err != nil { return nil }`)
	// なので、戻り値だけでは失敗を検知できない。Err() を必ず確認する。
	if err := d.Err(); err != nil {
		return Info{}, fmt.Errorf("%w: data チャンクを読み取れません: %v", ErrInvalidWAV, err)
	}

	// 空ファイルや途中で切れたヘッダは io.EOF になるが、Decoder.Err() は io.EOF を nil に潰して返すため、
	// ここまでのエラーチェックでは検知できない。fmt チャンクを読めたか (NumChans != 0) で判定する。
	if d.NumChans == 0 {
		return Info{}, fmt.Errorf("%w: fmt チャンクが見つかりません", ErrInvalidWAV)
	}
	if d.BitDepth == 0 || d.BitDepth%8 != 0 {
		return Info{}, fmt.Errorf("%w: 量子化ビット数が不正です (%d bit)", ErrInvalidWAV, d.BitDepth)
	}
	// 1 秒あたりのバイト数。ヘッダの AvgBytesPerSec は書き手によっては信用できないため自分で計算する。
	byteRate := int64(d.SampleRate) * int64(d.NumChans) * int64(d.BitDepth) / 8
	if byteRate <= 0 {
		return Info{}, fmt.Errorf("%w: 書式が不正です (%d Hz, %d ch, %d bit)",
			ErrInvalidWAV, d.SampleRate, d.NumChans, d.BitDepth)
	}

	pcmBytes := d.PCMLen()
	if pcmBytes <= 0 {
		return Info{}, fmt.Errorf("%w: PCM データが空です", ErrInvalidWAV)
	}
	// PCMLen() は data チャンクヘッダの申告値であって実データ量ではない。
	// 合成やダウンロードが途中で中断された wav を正しい長さとして扱わないよう、実ファイルの残りと突き合わせる。
	// RIFF はチャンクを 2 バイト境界に揃えるため申告値が実データより 1 バイト大きいことがあり、その分は許容する。
	if remaining, err := remainingBytes(r); err == nil && remaining < pcmBytes-1 {
		return Info{}, fmt.Errorf("%w: ファイルが途中で切れています (data チャンクの宣言は %d バイトだが実データは %d バイト)",
			ErrInvalidWAV, pcmBytes, remaining)
	}

	// wav.Decoder.Duration() は使わないこと。
	// 内部 (go-audio/riff/parser.go の wavDuration) が data チャンクではなく RIFF チャンクのサイズを使っており、
	//     duration := time.Duration((float64(p.Size) / float64(p.AvgBytesPerSec)) * float64(time.Second))
	// RIFF サイズはヘッダ 36 バイトを含むため、常に 36 バイト分だけ過大な値を返す
	// (24000Hz / 1ch / 16bit で約 750µs、44100Hz / 2ch / 16bit で約 204µs)。
	// 「1 行で書ける」からと Duration() に戻さないこと。data チャンクのサイズから計算するのが正しい。
	duration := time.Duration(float64(pcmBytes) / float64(byteRate) * float64(time.Second))

	return Info{
		Duration:    duration,
		SampleRate:  int(d.SampleRate),
		NumChannels: int(d.NumChans),
		BitDepth:    int(d.BitDepth),
		PCMBytes:    pcmBytes,
	}, nil
}

// checkContainer は先頭 12 バイトが RIFF/WAVE コンテナであることだけを確認し、読み取り位置を元に戻す。
//
// go-audio/wav も同じ検査をするが、エラーメッセージに先頭 4 バイトを生のまま埋め込むため、
// wav でないファイルを渡すと文字化けした文言になり、何が悪いのか伝わらない。
// チャンクの解析はライブラリに任せ、ここでは診断のためだけにマジックナンバーを見る。
func checkContainer(r io.ReadSeeker) error {
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return fmt.Errorf("%w: ファイルが小さすぎます (WAV は最低でも 44 バイト必要)", ErrInvalidWAV)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return fmt.Errorf("%w: RIFF/WAVE 形式ではありません", ErrInvalidWAV)
	}
	if _, err := r.Seek(-int64(len(header)), io.SeekCurrent); err != nil {
		return fmt.Errorf("%w: 先頭に戻れません: %v", ErrInvalidWAV, err)
	}
	return nil
}

// remainingBytes は r の現在位置から末尾までのバイト数を返し、位置を元に戻す。
func remainingBytes(r io.ReadSeeker) (int64, error) {
	cur, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	if _, err := r.Seek(cur, io.SeekStart); err != nil {
		return 0, err
	}
	return end - cur, nil
}
