package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckNode(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    Status
		// contains は Detail と Actions を繋いだ文字列に含まれてほしい語。
		contains []string
	}{
		{"要件を満たす", "v22.11.0", StatusOK, []string{"v22.11.0"}},
		{"ちょうど 20", "v20.0.0", StatusOK, []string{"v20.0.0"}},
		{"v が無くても読める", "24.1.0", StatusOK, []string{"24.1.0"}},
		{"古い", "v18.20.4", StatusNG, []string{"v18.20.4", "20 以上", "Remotion"}},
		{"判別できない", "node: command not found?", StatusWarn, []string{"判別できませんでした"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{versions: map[string]string{"node": tt.version}}
			got := checkNode(context.Background(), Options{RunCommand: runner.run}.withDefaults())
			if got.Status != tt.want {
				t.Fatalf("判定が違う: got %v, want %v (%+v)", got.Status, tt.want, got)
			}
			wantContains(t, got.Detail+"\n"+strings.Join(got.Actions, "\n"), tt.contains...)
			if tt.want != StatusOK && len(got.Actions) == 0 {
				t.Error("対処の案内が無い")
			}
		})
	}
}

func TestCheckNode_未導入なら入れ方を案内する(t *testing.T) {
	runner := &fakeRunner{versions: map[string]string{}}
	got := checkNode(context.Background(), Options{RunCommand: runner.run}.withDefaults())

	if got.Status != StatusNG {
		t.Fatalf("未導入なのに NG でない: %+v", got)
	}
	wantContains(t, got.Detail, "見つかりません")
	wantContains(t, strings.Join(got.Actions, "\n"), "nodejs.org", "brew install node", "ターミナルを開き直")
}

func TestCheckNode_実行に失敗したら理由を残す(t *testing.T) {
	opts := Options{RunCommand: func(context.Context, string, ...string) (string, error) {
		return "", errors.New("exec format error")
	}}.withDefaults()

	got := checkNode(context.Background(), opts)
	if got.Status != StatusNG {
		t.Fatalf("失敗したのに NG でない: %+v", got)
	}
	wantContains(t, got.Detail, "exec format error")
}

func TestCheckPnpm(t *testing.T) {
	t.Run("導入済み", func(t *testing.T) {
		runner := &fakeRunner{versions: map[string]string{"pnpm": "11.11.0"}}
		got := checkPnpm(context.Background(), Options{RunCommand: runner.run}.withDefaults())
		if got.Status != StatusOK {
			t.Fatalf("OK でない: %+v", got)
		}
		wantContains(t, got.Detail, "11.11.0")
	})

	t.Run("未導入なら corepack を案内する", func(t *testing.T) {
		runner := &fakeRunner{versions: map[string]string{}}
		got := checkPnpm(context.Background(), Options{RunCommand: runner.run}.withDefaults())
		if got.Status != StatusNG {
			t.Fatalf("未導入なのに NG でない: %+v", got)
		}
		wantContains(t, strings.Join(got.Actions, "\n"), "corepack enable pnpm", "npm install -g pnpm")
	})
}

func TestCheckRendererDeps(t *testing.T) {
	t.Run("導入済み", func(t *testing.T) {
		root := setupWorkDir(t, true)
		got := checkRendererDeps(Options{WorkDir: root}.withDefaults())
		if got.Status != StatusOK {
			t.Fatalf("OK でない: %+v", got)
		}
		wantContains(t, got.Detail, "4.0.508")
	})

	t.Run("node_modules が無ければ pnpm install を案内する", func(t *testing.T) {
		root := setupWorkDir(t, false)
		got := checkRendererDeps(Options{WorkDir: root}.withDefaults())
		if got.Status != StatusNG {
			t.Fatalf("依存が無いのに NG でない: %+v", got)
		}
		wantContains(t, got.Detail, "node_modules")
		wantContains(t, strings.Join(got.Actions, "\n"), "pnpm install --dir ")
	})

	t.Run("remotion が入っていなければ入れ直しを案内する", func(t *testing.T) {
		root := setupWorkDir(t, false)
		// install を途中で止めた木を再現する（node_modules はあるが remotion が無い）。
		writeFile(t, filepath.Join(root, "renderer", "node_modules", ".modules.yaml"), "")
		got := checkRendererDeps(Options{WorkDir: root}.withDefaults())
		if got.Status != StatusNG {
			t.Fatalf("remotion が無いのに NG でない: %+v", got)
		}
		wantContains(t, got.Detail, "remotion が入っていません")
		wantContains(t, strings.Join(got.Actions, "\n"), "pnpm install --dir ")
	})

	t.Run("renderer が見つからなければ実行場所を案内する", func(t *testing.T) {
		got := checkRendererDeps(Options{WorkDir: t.TempDir()}.withDefaults())
		if got.Status != StatusNG {
			t.Fatalf("renderer が無いのに NG でない: %+v", got)
		}
		wantContains(t, got.Detail, "renderer/ が見つかりません")
		wantContains(t, strings.Join(got.Actions, "\n"), "リポジトリの中")
	})

	t.Run("動画ディレクトリから実行しても親の renderer を見つける", func(t *testing.T) {
		root := setupWorkDir(t, true)
		videoDir := filepath.Join(root, "videos", "ep01")
		if err := os.MkdirAll(videoDir, 0o755); err != nil {
			t.Fatalf("ディレクトリを作れなかった: %v", err)
		}
		got := checkRendererDeps(Options{WorkDir: videoDir}.withDefaults())
		if got.Status != StatusOK {
			t.Fatalf("親の renderer を見つけられていない: %+v", got)
		}
	})

	t.Run("RendererDir を指定すればそこだけを見る", func(t *testing.T) {
		root := setupWorkDir(t, true)
		got := checkRendererDeps(Options{WorkDir: t.TempDir(), RendererDir: filepath.Join(root, "renderer")}.withDefaults())
		if got.Status != StatusOK {
			t.Fatalf("指定した renderer を見ていない: %+v", got)
		}
	})
}

func TestCheckWritable(t *testing.T) {
	t.Run("書き込めれば OK", func(t *testing.T) {
		dir := t.TempDir()
		got := checkWritable(Options{WorkDir: dir}.withDefaults())
		if got.Status != StatusOK {
			t.Fatalf("書き込めるのに OK でない: %+v", got)
		}
		// 診断のために作ったファイルを残さないこと
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ディレクトリを読めなかった: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("一時ファイルが残っている: %v", entries)
		}
	})

	t.Run("書き込めなければ NG", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows ではパーミッションで書き込みを止められない")
		}
		if os.Geteuid() == 0 {
			t.Skip("root はパーミッションを無視して書き込めてしまう")
		}
		dir := filepath.Join(t.TempDir(), "readonly")
		if err := os.Mkdir(dir, 0o555); err != nil {
			t.Fatalf("ディレクトリを作れなかった: %v", err)
		}
		// t.TempDir の後片付けが失敗しないよう権限を戻す。
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		got := checkWritable(Options{WorkDir: dir}.withDefaults())
		if got.Status != StatusNG {
			t.Fatalf("書き込めないのに NG でない: %+v", got)
		}
		wantContains(t, strings.Join(got.Actions, "\n"), "権限", ".scenaremo/", "out/")
	})
}

func TestCheckVoicevox_URLが壊れていればその旨を伝える(t *testing.T) {
	report := Run(context.Background(), Options{
		VoicevoxURL: "127.0.0.1:50021", // スキームが無い
		WorkDir:     t.TempDir(),
		RunCommand:  healthyRunner().run,
	})

	check := find(t, report, "VOICEVOX ENGINE")
	if check.Status != StatusNG {
		t.Fatalf("不正な URL なのに NG でない: %+v", check)
	}
	wantContains(t, check.Detail, "127.0.0.1:50021")
	if len(check.Actions) == 0 {
		t.Error("対処の案内が無い")
	}
}

func TestParseNodeMajor(t *testing.T) {
	tests := map[string]struct {
		want int
		ok   bool
	}{
		"v22.11.0":        {22, true},
		"22.11.0":         {22, true},
		"v20.0.0-nightly": {20, true},
		" v24.1.0\n":      {24, true},
		"":                {0, false},
		"vNext":           {0, false},
		"v0.12.0":         {0, false},
	}
	for in, want := range tests {
		got, ok := parseNodeMajor(in)
		if ok != want.ok || got != want.want {
			t.Errorf("parseNodeMajor(%q) = (%d, %v), want (%d, %v)", in, got, ok, want.want, want.ok)
		}
	}
}

func TestExecCommand_存在しないコマンドはErrNotInstalled(t *testing.T) {
	_, err := execCommand(context.Background(), "scenaremo-存在しないコマンド", "--version")
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("ErrNotInstalled でない: %v", err)
	}
}

func TestExecCommand_標準出力を返す(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("echo が使えない")
	}
	out, err := execCommand(context.Background(), "echo", "v22.11.0")
	if err != nil {
		t.Fatalf("実行に失敗した: %v", err)
	}
	if out != "v22.11.0" {
		t.Errorf("出力が違う: %q", out)
	}
}

func TestReadPackageVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package.json")
	writeFile(t, path, `{"name":"remotion","version":"4.0.508"}`)

	got, err := readPackageVersion(path)
	if err != nil {
		t.Fatalf("読めなかった: %v", err)
	}
	if got != "4.0.508" {
		t.Errorf("version が違う: %q", got)
	}

	writeFile(t, path, `{"name":"remotion"}`)
	if _, err := readPackageVersion(path); err == nil {
		t.Error("version が無いのにエラーにならなかった")
	}

	if _, err := readPackageVersion(filepath.Join(dir, "無い.json")); err == nil {
		t.Error("ファイルが無いのにエラーにならなかった")
	}
}

func TestFirstLine(t *testing.T) {
	got := firstLine("  1 行目 \n2 行目\n")
	if got != "1 行目" {
		t.Errorf("1 行目に丸められていない: %q", got)
	}
}
