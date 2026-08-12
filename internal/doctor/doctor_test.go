package doctor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRunner は「入っているコマンド」の一覧から CommandRunner を作る。
// 一覧に無いコマンドは ErrNotInstalled を返し、未導入の環境を再現する。
type fakeRunner struct {
	versions map[string]string

	mu    sync.Mutex
	calls []string
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	f.mu.Unlock()

	out, ok := f.versions[name]
	if !ok {
		return "", fmt.Errorf("%s: %w", name, ErrNotInstalled)
	}
	return out, nil
}

// healthyRunner は node も pnpm も要件を満たしている環境。
func healthyRunner() *fakeRunner {
	return &fakeRunner{versions: map[string]string{
		"node": "v22.11.0",
		"pnpm": "11.11.0",
	}}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("ディレクトリを作れなかった: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("ファイルを書けなかった: %v", err)
	}
}

// setupWorkDir は renderer/ を持つ作業ディレクトリを作る。deps が true なら依存も入れる。
func setupWorkDir(t *testing.T, deps bool) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "renderer", "package.json"), `{"name":"scenaremo-renderer"}`)
	if deps {
		writeFile(t, filepath.Join(root, "renderer", "node_modules", "remotion", "package.json"),
			`{"name":"remotion","version":"4.0.508"}`)
	}
	return root
}

// startEngine は VOICEVOX ENGINE の代わりに立てるサーバの URL を返す。
func startEngine(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

// deadEngineURL は誰も待ち受けていない URL を返す。エンジン未起動の再現に使う。
func deadEngineURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	return url
}

func versionHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.Error(w, "未定義のパス: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "%q", version)
	}
}

// find は名前で診断項目を引く。
func find(t *testing.T, r Report, name string) Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("%q の診断結果が無い: %+v", name, r.Checks)
	return Check{}
}

// wantContains は文字列に欲しい語がすべて含まれることを確かめる。
func wantContains(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%q が含まれない:\n%s", w, got)
		}
	}
}

func TestRun_すべて揃っていれば全項目OK(t *testing.T) {
	root := setupWorkDir(t, true)
	runner := healthyRunner()

	report := Run(context.Background(), Options{
		VoicevoxURL: startEngine(t, versionHandler("0.14.4")),
		WorkDir:     root,
		RunCommand:  runner.run,
	})

	if !report.OK() {
		t.Fatalf("すべて揃っているのに要対応がある: %+v", report.Failures())
	}
	for _, c := range report.Checks {
		if c.Status != StatusOK {
			t.Errorf("%s が OK でない: %+v", c.Name, c)
		}
	}

	// バージョンは実際に問い合わせて得た値を出すこと（利用者が食い違いに気づけるようにするため）
	wantContains(t, find(t, report, "Node.js").Detail, "v22.11.0")
	wantContains(t, find(t, report, "pnpm").Detail, "11.11.0")
	wantContains(t, find(t, report, "VOICEVOX ENGINE").Detail, "0.14.4")
	wantContains(t, find(t, report, "renderer の依存").Detail, "4.0.508")

	if want := []string{"node --version", "pnpm --version"}; !sameStrings(runner.calls, want) {
		t.Errorf("外部コマンドの呼び方が違う: %v", runner.calls)
	}
}

func TestRun_VOICEVOXが未起動なら起動手順を案内する(t *testing.T) {
	root := setupWorkDir(t, true)
	url := deadEngineURL(t)

	report := Run(context.Background(), Options{
		VoicevoxURL: url,
		WorkDir:     root,
		RunCommand:  healthyRunner().run,
	})

	check := find(t, report, "VOICEVOX ENGINE")
	if check.Status != StatusNG {
		t.Fatalf("未起動なのに NG でない: %+v", check)
	}
	wantContains(t, check.Detail, url, "接続できませんでした")

	// 最も頻度の高い失敗なので、起動の仕方を環境ごとに案内できていること
	actions := strings.Join(check.Actions, "\n")
	wantContains(t, actions,
		"VOICEVOX アプリを起動",
		"run.exe",
		"docker run",
		url+"/docs",
		"--voicevox-url",
	)

	if report.OK() {
		t.Error("要対応があるのに OK になっている")
	}
	// 1 項目の失敗で診断を打ち切らないこと
	if n := len(report.Checks); n != 5 {
		t.Errorf("診断項目が途中で止まっている: %d 件", n)
	}
	for _, name := range []string{"Node.js", "pnpm", "renderer の依存", "書き込み権限"} {
		if c := find(t, report, name); c.Status != StatusOK {
			t.Errorf("VOICEVOX の失敗が %s の診断へ波及している: %+v", name, c)
		}
	}
}

func TestRun_VOICEVOXが200以外を返すなら別の原因として案内する(t *testing.T) {
	root := setupWorkDir(t, true)
	url := startEngine(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	report := Run(context.Background(), Options{VoicevoxURL: url, WorkDir: root, RunCommand: healthyRunner().run})

	check := find(t, report, "VOICEVOX ENGINE")
	if check.Status != StatusNG {
		t.Fatalf("404 なのに NG でない: %+v", check)
	}
	wantContains(t, check.Detail, "/version", "404")
	// 繋がってはいるので「起動してください」ではなく、別の原因を案内すること
	actions := strings.Join(check.Actions, "\n")
	wantContains(t, actions, "VOICEVOX 以外のアプリ", "最新版")
	if strings.Contains(actions, "docker run") {
		t.Errorf("繋がっているのに起動手順を案内している: %v", check.Actions)
	}
}

func TestRun_VOICEVOXが応答しなければ待ち時間を伝える(t *testing.T) {
	root := setupWorkDir(t, true)
	block := make(chan struct{})
	defer close(block)

	url := startEngine(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	})

	report := Run(context.Background(), Options{
		VoicevoxURL: url,
		WorkDir:     root,
		RunCommand:  healthyRunner().run,
		Timeout:     50 * time.Millisecond,
	})

	check := find(t, report, "VOICEVOX ENGINE")
	if check.Status != StatusNG {
		t.Fatalf("応答が無いのに NG でない: %+v", check)
	}
	wantContains(t, check.Detail, "応答しませんでした")
	wantContains(t, strings.Join(check.Actions, "\n"), "少し待って")
}

func TestRun_何も揃っていなくても全項目を診断して対処を示す(t *testing.T) {
	// renderer/ が無く、node も pnpm も入っていない環境。
	root := t.TempDir()
	report := Run(context.Background(), Options{
		VoicevoxURL: deadEngineURL(t),
		WorkDir:     root,
		RunCommand:  (&fakeRunner{versions: map[string]string{}}).run,
	})

	if report.OK() {
		t.Fatal("何も揃っていないのに OK になっている")
	}
	if n := len(report.Failures()); n != 4 {
		t.Errorf("要対応の件数が違う: %d 件 %+v", n, report.Failures())
	}
	// 書き込みだけはできる（一時ディレクトリなので）
	if c := find(t, report, "書き込み権限"); c.Status != StatusOK {
		t.Errorf("書き込めるはずなのに NG: %+v", c)
	}

	// 失敗した項目には必ず次の一手が付くこと。これが無い診断は「NG」と言うだけで役に立たない。
	for _, c := range report.Failures() {
		if len(c.Actions) == 0 {
			t.Errorf("%s に対処の案内が無い: %+v", c.Name, c)
		}
		if c.Detail == "" {
			t.Errorf("%s に理由が書かれていない: %+v", c.Name, c)
		}
	}
}

func TestRun_既定値でも診断できる(t *testing.T) {
	// Options のゼロ値でも実環境を対象に動くこと。外部コマンドと接続先は実物になるため、
	// ここでは結果の中身ではなく「落ちずに全項目返ること」だけを確かめる。
	report := Run(context.Background(), Options{Timeout: 200 * time.Millisecond})
	if n := len(report.Checks); n != 5 {
		t.Fatalf("診断項目が足りない: %d 件", n)
	}
	for _, c := range report.Checks {
		if c.Name == "" || c.Detail == "" {
			t.Errorf("項目名か理由が空: %+v", c)
		}
	}
}

// sameStrings は文字列スライスが同じ並びかを返す。
func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
