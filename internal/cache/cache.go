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
// 部分的な書き込みを防ぐため、一時ファイルに書き込んでからリネームする（→ put）。
// 同時に呼ばれても安全である。
func (s *Store) Put(key string, wav []byte) error {
	return s.put(key+".wav", wav)
}

// GetQuery はキャッシュから AudioQuery の JSON を取得する。
// 見つからない場合は os.ErrNotExist をラップしたエラーを返す。
func (s *Store) GetQuery(key string) ([]byte, error) {
	path := filepath.Join(s.dir, key+".query.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("キャッシュが見つかりません: %w", os.ErrNotExist)
		}
		return nil, fmt.Errorf("キャッシュの読み込みに失敗しました: %w", err)
	}
	return data, nil
}

// PutQuery は AudioQuery の JSON をキャッシュに保存する。
//
// Put と同じ理由で、同時に呼ばれても安全である。wav と AudioQuery は必ず対で書かれるので、
// 片方だけを競合から守っても意味がない。
func (s *Store) PutQuery(key string, query []byte) error {
	return s.put(key+".query.json", query)
}

// put はキャッシュディレクトリの中の name へ data を原子的に置く。
//
// 一時ファイルへ書いてから rename するのは、部分的な書き込みを防ぐため。
// その一時ファイルの名前を name から決めず os.CreateTemp に任せるのは、
// 同時に呼ばれても壊れないようにするためである。--parallel で合成すると worker が
// 同時に書きに来るうえ、同じ話者が同じ文を二度喋る台本では名前まで同じになる
// （キャッシュはそもそも「同じ入力なら同じファイル」を狙って設計されている）。
// 名前を共有すると、2 つの書き込みが同じ一時ファイルへ混ざったまま本番の名前へ移るか、
// 先に移されたあとの rename が「そんなファイルは無い」で落ちる。
//
// 誰が最後に書いたかは決まらないが、rename は原子的なので、
// 置かれるのは必ず誰か 1 人ぶんの完全な中身になる。
func (s *Store) put(name string, data []byte) error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("キャッシュディレクトリの作成に失敗しました: %w", err)
	}

	path := filepath.Join(s.dir, name)

	temp, err := os.CreateTemp(s.dir, name+".*.tmp")
	if err != nil {
		return fmt.Errorf("キャッシュへの書き込みに失敗しました: %w", err)
	}
	tempPath := temp.Name()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("キャッシュへの書き込みに失敗しました: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("キャッシュへの書き込みに失敗しました: %w", err)
	}
	// CreateTemp は 0600 で作る。以前の実装（0644）と同じ見え方に揃えておく。
	// Remotion を別のユーザやコンテナから走らせる構成でも読めなくならないようにするため。
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
