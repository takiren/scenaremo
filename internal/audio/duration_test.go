package audio_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	goaudio "github.com/go-audio/audio"
	"github.com/go-audio/wav"

	"github.com/takiren/scenaremo/internal/audio"
)

func TestDuration(t *testing.T) {
	tests := []struct {
		name        string
		sampleRate  int
		bitDepth    int
		numChans    int
		frames      int
		want        time.Duration
		wantPCMSize int64
	}{
		{
			// VOICEVOX の既定書式。実運用で最も多く通る経路。
			name:        "VOICEVOX既定 24000Hz/1ch/16bit 1.5秒",
			sampleRate:  24000,
			bitDepth:    16,
			numChans:    1,
			frames:      36000,
			want:        1500 * time.Millisecond,
			wantPCMSize: 72000,
		},
		{
			name:        "44100Hz/2ch/16bit 1秒",
			sampleRate:  44100,
			bitDepth:    16,
			numChans:    2,
			frames:      44100,
			want:        time.Second,
			wantPCMSize: 176400,
		},
		{
			name:        "44100Hz/2ch/16bit 3.5秒",
			sampleRate:  44100,
			bitDepth:    16,
			numChans:    2,
			frames:      154350,
			want:        3500 * time.Millisecond,
			wantPCMSize: 617400,
		},
		{
			name:        "48000Hz/1ch/24bit 17ms",
			sampleRate:  48000,
			bitDepth:    24,
			numChans:    1,
			frames:      816,
			want:        17 * time.Millisecond,
			wantPCMSize: 2448,
		},
		{
			// 「はい」程度の短いセリフ。切り捨てや 0 除算で壊れないこと。
			name:        "極端に短い 24000Hz/1ch/16bit 30ms",
			sampleRate:  24000,
			bitDepth:    16,
			numChans:    1,
			frames:      720,
			want:        30 * time.Millisecond,
			wantPCMSize: 1440,
		},
		{
			name:        "極端に短い 24000Hz/1ch/16bit 5ms",
			sampleRate:  24000,
			bitDepth:    16,
			numChans:    1,
			frames:      120,
			want:        5 * time.Millisecond,
			wantPCMSize: 240,
		},
		{
			name:        "1フレームだけ 24000Hz/1ch/16bit",
			sampleRate:  24000,
			bitDepth:    16,
			numChans:    1,
			frames:      1,
			want:        time.Second / 24000,
			wantPCMSize: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeWAV(t, tt.sampleRate, tt.bitDepth, tt.numChans, tt.frames)

			got, err := audio.Duration(path)
			if err != nil {
				t.Fatalf("Duration() が失敗した: %v", err)
			}
			if diff := absDuration(got - tt.want); diff >= time.Millisecond {
				t.Errorf("Duration() = %v, 期待値 %v (誤差 %v は 1ms 以上)", got, tt.want, diff)
			}

			info, err := audio.Measure(path)
			if err != nil {
				t.Fatalf("Measure() が失敗した: %v", err)
			}
			if info.Duration != got {
				t.Errorf("Measure().Duration = %v, Duration() = %v (一致すべき)", info.Duration, got)
			}
			if info.SampleRate != tt.sampleRate || info.NumChannels != tt.numChans || info.BitDepth != tt.bitDepth {
				t.Errorf("Measure() の書式 = %dHz/%dch/%dbit, 期待値 %dHz/%dch/%dbit",
					info.SampleRate, info.NumChannels, info.BitDepth, tt.sampleRate, tt.numChans, tt.bitDepth)
			}
			if info.PCMBytes != tt.wantPCMSize {
				t.Errorf("Measure().PCMBytes = %d, 期待値 %d", info.PCMBytes, tt.wantPCMSize)
			}
		})
	}
}

// 秒単位で端数が出る長さでも、data チャンクのバイト数から求めた理論値と一致すること。
func TestDuration割り切れない長さ(t *testing.T) {
	sampleRate, frames := 24000, 33457 // 1.394041666...秒
	path := writeWAV(t, sampleRate, 16, 1, frames)

	got, err := audio.Duration(path)
	if err != nil {
		t.Fatalf("Duration() が失敗した: %v", err)
	}
	want := time.Duration(float64(frames) / float64(sampleRate) * float64(time.Second))
	if diff := absDuration(got - want); diff > time.Microsecond {
		t.Errorf("Duration() = %v, 期待値 %v (誤差 %v)", got, want, diff)
	}
}

// 罠1 の回帰テスト。
// wav.Decoder.Duration() は data チャンクではなく RIFF チャンクのサイズを使うため、
// 常にヘッダ 36 バイト分だけ過大な値を返す。実装が Duration() に差し戻されたらここで気付ける。
func TestDecoderのDurationは36バイト分過大(t *testing.T) {
	const (
		sampleRate = 24000
		bitDepth   = 16
		numChans   = 1
		frames     = 36000
	)
	path := writeWAV(t, sampleRate, bitDepth, numChans, frames)

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("テスト用 wav を開けない: %v", err)
	}
	defer func() {
		_ = f.Close()
	}()

	d := wav.NewDecoder(f)
	d.ReadInfo()
	libDuration, err := d.Duration()
	if err != nil {
		t.Fatalf("wav.Decoder.Duration() が失敗した: %v", err)
	}

	measured, err := audio.Duration(path)
	if err != nil {
		t.Fatalf("Duration() が失敗した: %v", err)
	}
	if measured != 1500*time.Millisecond {
		t.Fatalf("Duration() = %v, 期待値 1.5s", measured)
	}

	byteRate := sampleRate * numChans * bitDepth / 8
	wantExcess := time.Duration(float64(36) / float64(byteRate) * float64(time.Second)) // 24kHz/16bit/1ch で約 750µs
	if excess := libDuration - measured; absDuration(excess-wantExcess) > time.Microsecond {
		t.Errorf("wav.Decoder.Duration() との差 = %v, 期待値 %v (36 バイト分の過大)", excess, wantExcess)
	}
}

// 罠2 の回帰テスト。FwdToPCM() を呼ばないと PCMLen() は 0 のままになる。
func TestFwdToPCMを呼ばないとPCMLenは0(t *testing.T) {
	path := writeWAV(t, 24000, 16, 1, 36000)

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("テスト用 wav を開けない: %v", err)
	}
	defer func() {
		_ = f.Close()
	}()

	d := wav.NewDecoder(f)
	d.ReadInfo()
	if got := d.PCMLen(); got != 0 {
		t.Fatalf("ReadInfo() だけで PCMLen() = %d になった。前提が変わったので実装を見直すこと", got)
	}
	if err := d.FwdToPCM(); err != nil {
		t.Fatalf("FwdToPCM() が失敗した: %v", err)
	}
	if got := d.PCMLen(); got != 72000 {
		t.Errorf("FwdToPCM() 後の PCMLen() = %d, 期待値 72000", got)
	}
}

func TestDurationのエラー(t *testing.T) {
	dir := t.TempDir()

	validPath := writeWAV(t, 24000, 16, 1, 24000)
	valid, err := os.ReadFile(validPath)
	if err != nil {
		t.Fatalf("テスト用 wav を読めない: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		wantIs   error
		wantText string
	}{
		{
			name:     "存在しないファイル",
			path:     filepath.Join(dir, "居ない.wav"),
			wantIs:   fs.ErrNotExist,
			wantText: "居ない.wav",
		},
		{
			name:     "空ファイル",
			path:     writeBytes(t, dir, "空.wav", nil),
			wantIs:   audio.ErrInvalidWAV,
			wantText: "ファイルが小さすぎます",
		},
		{
			name:     "WAVではないファイル",
			path:     writeBytes(t, dir, "ただのテキスト.wav", []byte("これは wav ではありません。台本の書き間違いかもしれません。")),
			wantIs:   audio.ErrInvalidWAV,
			wantText: "RIFF/WAVE 形式ではありません",
		},
		{
			name:     "dataチャンクが無い",
			path:     writeBytes(t, dir, "data無し.wav", riffContainer(fmtChunk(24000, 16, 1))),
			wantIs:   audio.ErrInvalidWAV,
			wantText: "data チャンクが見つかりません",
		},
		{
			name:     "dataチャンクが空",
			path:     writeBytes(t, dir, "data空.wav", riffContainer(append(fmtChunk(24000, 16, 1), dataChunkHeader(0)...))),
			wantIs:   audio.ErrInvalidWAV,
			wantText: "PCM データが空です",
		},
		{
			name:     "PCMデータが途中で切れている",
			path:     writeBytes(t, dir, "途中で切れた.wav", valid[:len(valid)/2]),
			wantIs:   audio.ErrInvalidWAV,
			wantText: "途中で切れています",
		},
		{
			name:     "fmtチャンクが途中で切れている",
			path:     writeBytes(t, dir, "ヘッダ欠損.wav", valid[:30]),
			wantIs:   audio.ErrInvalidWAV,
			wantText: "ヘッダ欠損.wav",
		},
		{
			name:     "RIFFだがWAVEではない",
			path:     writeBytes(t, dir, "avi.wav", []byte("RIFF\x24\x00\x00\x00AVI LIST")),
			wantIs:   audio.ErrInvalidWAV,
			wantText: "RIFF/WAVE 形式ではありません",
		},
		{
			// エラーメッセージに生のバイト列が混ざって文字化けしないこと。
			name:     "先頭が非UTF8バイト",
			path:     writeBytes(t, dir, "バイナリ.wav", []byte{0xff, 0xfe, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a}),
			wantIs:   audio.ErrInvalidWAV,
			wantText: "RIFF/WAVE 形式ではありません",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := audio.Duration(tt.path)
			if err == nil {
				t.Fatalf("エラーになるべきだが nil が返った")
			}
			if !errors.Is(err, tt.wantIs) {
				t.Errorf("errors.Is(err, %v) が false。err = %v", tt.wantIs, err)
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("エラーメッセージに %q が含まれない: %v", tt.wantText, err)
			}
			// どのファイルが問題なのか分かること。
			if !strings.Contains(err.Error(), filepath.Base(tt.path)) {
				t.Errorf("エラーメッセージにファイル名が含まれない: %v", err)
			}
			// 生のバイト列が混ざって文字化けしていないこと。
			if !utf8.ValidString(err.Error()) {
				t.Errorf("エラーメッセージが不正な UTF-8 になっている: %q", err.Error())
			}
		})
	}
}

func TestMeasureReader(t *testing.T) {
	path := writeWAV(t, 24000, 16, 1, 12000)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("テスト用 wav を読めない: %v", err)
	}

	info, err := audio.MeasureReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("MeasureReader() が失敗した: %v", err)
	}
	if info.Duration != 500*time.Millisecond {
		t.Errorf("MeasureReader().Duration = %v, 期待値 500ms", info.Duration)
	}
}

// 合成直後の計測はディスクを経由しないため、バイト列から測れることを保証しておく。
func TestMeasureBytes(t *testing.T) {
	path := writeWAV(t, 24000, 16, 1, 12000)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("テスト用 wav を読めない: %v", err)
	}

	info, err := audio.MeasureBytes(data)
	if err != nil {
		t.Fatalf("MeasureBytes() が失敗した: %v", err)
	}
	if info.Duration != 500*time.Millisecond {
		t.Errorf("MeasureBytes().Duration = %v, 期待値 500ms", info.Duration)
	}

	// パス経由と同じ結果になること（委譲先が同じであることの確認）。
	fromPath, err := audio.Measure(path)
	if err != nil {
		t.Fatalf("Measure() が失敗した: %v", err)
	}
	if info != fromPath {
		t.Errorf("MeasureBytes() = %+v, Measure() = %+v (一致すべき)", info, fromPath)
	}

	if _, err := audio.MeasureBytes(nil); err == nil {
		t.Error("空のバイト列がエラーにならなかった")
	}
}

// writeWAV は既知の長さの wav を一時ディレクトリに書き出し、そのパスを返す。
func writeWAV(t *testing.T, sampleRate, bitDepth, numChans, frames int) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.wav")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("テスト用 wav を作れない: %v", err)
	}
	defer func() {
		_ = f.Close()
	}()

	e := wav.NewEncoder(f, sampleRate, bitDepth, numChans, 1)
	buf := &goaudio.IntBuffer{
		Format:         &goaudio.Format{NumChannels: numChans, SampleRate: sampleRate},
		Data:           make([]int, frames*numChans),
		SourceBitDepth: bitDepth,
	}
	for i := range buf.Data {
		buf.Data[i] = i % 100 // 無音でなくても長さには影響しないが、念のため値を入れておく
	}
	if err := e.Write(buf); err != nil {
		t.Fatalf("テスト用 wav を書けない: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("テスト用 wav を閉じられない: %v", err)
	}
	return path
}

func writeBytes(t *testing.T, dir, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("テスト用ファイルを作れない: %v", err)
	}
	return path
}

func writeBinPanic(w io.Writer, data any) {
	if err := binary.Write(w, binary.LittleEndian, data); err != nil {
		panic(fmt.Sprintf("binary.Write failed: %v", err))
	}
}

// riffContainer は payload を RIFF/WAVE コンテナで包む。
func riffContainer(payload []byte) []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	writeBinPanic(&b, uint32(4+len(payload)))
	b.WriteString("WAVE")
	b.Write(payload)
	return b.Bytes()
}

func fmtChunk(sampleRate, bitDepth, numChans int) []byte {
	var b bytes.Buffer
	b.WriteString("fmt ")
	writeBinPanic(&b, uint32(16))
	writeBinPanic(&b, uint16(1)) // PCM
	writeBinPanic(&b, uint16(numChans))
	writeBinPanic(&b, uint32(sampleRate))
	writeBinPanic(&b, uint32(sampleRate*numChans*bitDepth/8))
	writeBinPanic(&b, uint16(numChans*bitDepth/8))
	writeBinPanic(&b, uint16(bitDepth))
	return b.Bytes()
}

func dataChunkHeader(size int) []byte {
	var b bytes.Buffer
	b.WriteString("data")
	writeBinPanic(&b, uint32(size))
	return b.Bytes()
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
