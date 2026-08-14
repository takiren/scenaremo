// Package cache は音声合成の結果をファイルシステムにキャッシュする機能を提供する。
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/takiren/scenaremo/internal/tts"
)

// Store は合成された音声をファイルシステムにキャッシュする。
type Store struct {
	dir string
}

// NewStore は新しい Store を作成する。
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Get はキャッシュから音声データを取得する。
// 見つからない場合は os.ErrNotExist をラップしたエラーを返す。
func (s *Store) Get(key string) ([]byte, error) {
	path := filepath.Join(s.dir, key+".wav")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("キャッシュが見つかりません: %w", os.ErrNotExist)
		}
		return nil, fmt.Errorf("キャッシュの読み込みに失敗しました: %w", err)
	}
	return data, nil
}

// Put は音声データをキャッシュに保存する。
// 部分的な書き込みを防ぐため、一時ファイルに書き込んでからリネームする。
//
// 同時に呼ばれても安全である。--parallel で合成すると worker が同時に Put を呼び、
// 同じ話者が同じ文を二度喋る台本ではキーまで同じになる（キャッシュはそもそも
// 「同じ入力なら同じファイル」を狙って設計されている）。
// そのため一時ファイルの名前はキーだけで決めず、呼び出しごとに別の名前にする。
// 名前を共有すると、2 つの Put が同じ一時ファイルへ同時に書いて混ざった中身が
// 本番の名前へ移るか、先に移されたあとの rename が「そんなファイルは無い」で落ちる。
func (s *Store) Put(key string, wav []byte) error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("キャッシュディレクトリの作成に失敗しました: %w", err)
	}

	path := filepath.Join(s.dir, key+".wav")

	temp, err := os.CreateTemp(s.dir, key+".*.wav.tmp")
	if err != nil {
		return fmt.Errorf("キャッシュへの書き込みに失敗しました: %w", err)
	}
	tempPath := temp.Name()

	if _, err := temp.Write(wav); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("キャッシュへの書き込みに失敗しました: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("キャッシュへの書き込みに失敗しました: %w", err)
	}
	// CreateTemp は 0600 で作る。以前の実装（0644）と同じ見え方に揃えておく。
	// Remotion を別のユーザやコンテナから走らせる構成でも wav が読めなくならないようにするため。
	if err := os.Chmod(tempPath, 0644); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("キャッシュへの書き込みに失敗しました: %w", err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("キャッシュの保存に失敗しました: %w", err)
	}
	return nil
}

func formatFloatPtr(v *float64) string {
	if v == nil {
		return "<nil>"
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// Key は合成パラメータから決定論的なキャッシュキー（SHA-256の16進数文字列表現）を生成する。
func Key(engine tts.EngineKind, req tts.SynthesizeRequest) string {
	str := fmt.Sprintf("%s|%d|%s|%s|%s|%s|%s",
		engine,
		req.StyleID,
		req.Text,
		formatFloatPtr(req.Params.SpeedScale),
		formatFloatPtr(req.Params.PitchScale),
		formatFloatPtr(req.Params.IntonationScale),
		formatFloatPtr(req.Params.VolumeScale),
	)
	hash := sha256.Sum256([]byte(str))
	return hex.EncodeToString(hash[:])
}
