package cache_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/takiren/scenaremo/internal/cache"
)

// TestStore_QueryGetPut は AudioQuery の JSON を wav と同じキーで出し入れできることを確かめる。
//
// モーラ単位の長さは audio_query の応答にしか無い（→ issue #20）。
// wav だけを控えていると、2 回目以降の build はエンジンを呼ばないぶん
// モーラ情報を失う。控えるのは音だけでは足りない。
func TestStore_QueryGetPut(t *testing.T) {
	store := cache.NewStore(t.TempDir())
	key := "testkey123"
	query := []byte(`{"accent_phrases":[],"speedScale":1.0}`)

	if err := store.PutQuery(key, query); err != nil {
		t.Fatalf("PutQueryに失敗しました: %v", err)
	}

	got, err := store.GetQuery(key)
	if err != nil {
		t.Fatalf("GetQueryに失敗しました: %v", err)
	}
	if string(got) != string(query) {
		t.Errorf("GetQueryで得られたデータが異なります: got %q, want %q", got, query)
	}
}

// TestStore_GetQueryNotExist は、控えが無いことを Get と同じ形で伝えることを確かめる。
// 呼び出し側は「無い」と「壊れている」を errors.Is で分けて扱う。
func TestStore_GetQueryNotExist(t *testing.T) {
	store := cache.NewStore(t.TempDir())

	_, err := store.GetQuery("notexist")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("存在しないキーのGetQueryはos.ErrNotExistをラップしたエラーを返す必要があります: %v", err)
	}
}

// TestStore_QueryDoesNotCollideWithWAV は wav と AudioQuery が同じキーでも
// 別のファイルに置かれることを確かめる。片方がもう片方を上書きすると、
// 音が消えるか、モーラが二度と読めなくなる。
func TestStore_QueryDoesNotCollideWithWAV(t *testing.T) {
	dir := t.TempDir()
	store := cache.NewStore(dir)
	key := "samekey"
	wav := []byte("dummy wav data")
	query := []byte(`{"accent_phrases":[]}`)

	if err := store.Put(key, wav); err != nil {
		t.Fatalf("Putに失敗しました: %v", err)
	}
	if err := store.PutQuery(key, query); err != nil {
		t.Fatalf("PutQueryに失敗しました: %v", err)
	}

	gotWAV, err := store.Get(key)
	if err != nil {
		t.Fatalf("Getに失敗しました: %v", err)
	}
	if string(gotWAV) != string(wav) {
		t.Errorf("wavがAudioQueryに上書きされています: got %q, want %q", gotWAV, wav)
	}
	gotQuery, err := store.GetQuery(key)
	if err != nil {
		t.Fatalf("GetQueryに失敗しました: %v", err)
	}
	if string(gotQuery) != string(query) {
		t.Errorf("AudioQueryがwavに上書きされています: got %q, want %q", gotQuery, query)
	}
}

// TestStore_PutQueryCreatesDir は、wav より先に AudioQuery を保存しても
// 置き場所が作られることを確かめる。
func TestStore_PutQueryCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "newdir")
	store := cache.NewStore(dir)

	if err := store.PutQuery("key1", []byte("{}")); err != nil {
		t.Fatalf("PutQueryに失敗しました: %v", err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("PutQueryがディレクトリを作成しませんでした")
	}
}

// TestStore_PutQueryLeavesNoTemp は、書き込みに使った一時ファイルが残らないことを確かめる。
// キャッシュディレクトリは props.json が指す現物の置き場所でもあるので、
// 中途半端なファイルを残さない。
func TestStore_PutQueryLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	store := cache.NewStore(dir)

	if err := store.PutQuery("key1", []byte("{}")); err != nil {
		t.Fatalf("PutQueryに失敗しました: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("キャッシュディレクトリを読めません: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("余計なファイルが残っています: %v", names)
	}
}

// TestStore_PutQuery_同じキーへの同時書き込み は、AudioQuery の控えも wav と同じく
// 同時書き込みで壊れないことを確かめる。
//
// wav と AudioQuery は必ず対で書かれるので、片方だけを競合から守っても意味がない。
// 壊れた控えは JSON として読めなくなり、そのセリフだけ毎回合成し直される。
// 静かに遅くなるだけで表には出ないので、テストが無いと気づけない。
func TestStore_PutQuery_同じキーへの同時書き込み(t *testing.T) {
	dir := t.TempDir()
	store := cache.NewStore(dir)
	key := "concurrentquerykey"

	// 中身の違いが混ざったときに検出できるよう、書き手ごとに別のバイトで埋める。
	// 1 回の write で終わらない大きさにしないと、途中で入れ替わる隙が生まれない。
	const writers = 8
	const size = 1 << 20
	payloads := make([][]byte, writers)
	for i := range payloads {
		payloads[i] = bytes.Repeat([]byte{byte('a' + i)}, size)
	}

	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = store.PutQuery(key, payloads[i])
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("PutQuery[%d] に失敗しました: %v", i, err)
		}
	}

	got, err := store.GetQuery(key)
	if err != nil {
		t.Fatalf("GetQuery に失敗しました: %v", err)
	}
	// 誰が最後に書いたかは決まらないが、**誰か 1 人ぶんがそのまま**入っていなければならない。
	if !slices.ContainsFunc(payloads, func(p []byte) bool { return bytes.Equal(got, p) }) {
		t.Errorf("どの書き手の内容とも一致しません（混ざった中身が残っています）: 長さ %d, 先頭 %q, 末尾 %q",
			len(got), got[:min(8, len(got))], got[max(0, len(got)-8):])
	}
}
