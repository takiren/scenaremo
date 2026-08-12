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
func (s *Store) Put(key string, wav []byte) error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("キャッシュディレクトリの作成に失敗しました: %w", err)
	}

	path := filepath.Join(s.dir, key+".wav")
	tempPath := path + ".tmp"

	if err := os.WriteFile(tempPath, wav, 0644); err != nil {
		return fmt.Errorf("キャッシュへの書き込みに失敗しました: %w", err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
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
