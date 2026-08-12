package script

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// ImagePath は台本ファイルの位置を基準に scenes[].image を実際のファイルパスへ解決する。
// 台本に書くパスは台本ファイルからの相対で、絶対パスならそのまま使う。
func ImagePath(scriptPath, image string) string {
	return resolveImage(filepath.Dir(scriptPath), image)
}

// resolveImage は基準ディレクトリから画像パスを解決する。
func resolveImage(baseDir, image string) string {
	// 台本には OS に依らず / 区切りで書くため、ここで OS の区切りへ直す。
	p := filepath.FromSlash(image)
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(baseDir, p)
}

// checkSpeakers は話者エイリアスの参照が解決できることを確かめる。
//
// 既定値を埋める前に呼ぶこと。どのセリフが speaker を省略していたかが分からなくなると、
// 「defaults.speaker が悪いのか、その行が悪いのか」を示し分けられなくなるため。
func checkSpeakers(s *Script, loc *locator) []Issue {
	var issues []Issue
	available := speakerNames(s)

	// defaults.speaker の誤りは、それを使うセリフの数だけ繰り返さず1件にまとめる。
	fallback := ""
	if s.Defaults != nil && s.Defaults.Speaker != "" {
		fallback = s.Defaults.Speaker
		if !hasSpeaker(s, fallback) {
			p := loc.resolve([]step{key("defaults"), key("speaker")}, false)
			issues = append(issues, issueAt(p,
				fmt.Sprintf("既定の話者 %s は speakers に定義されていません", strconv.Quote(fallback)),
				speakerHint(available)))
		}
	}

	for i, scene := range s.Scenes {
		for j, line := range scene.Lines {
			at := func(extra ...step) []step {
				return append([]step{key("scenes"), index(i), key("lines"), index(j)}, extra...)
			}

			if line.Speaker == "" {
				if fallback == "" {
					p := loc.resolve(at(), true)
					issues = append(issues, issueAt(p,
						fmt.Sprintf("%sの話者が決まりません", p.subject()),
						"このセリフに speaker を書くか、defaults.speaker を設定してください"))
				}
				// 省略した場合の誤りは defaults.speaker 側で報告済み。
				continue
			}
			if !hasSpeaker(s, line.Speaker) {
				p := loc.resolve(at(key("speaker")), false)
				issues = append(issues, issueAt(p,
					fmt.Sprintf("話者 %s は speakers に定義されていません", strconv.Quote(line.Speaker)),
					speakerHint(available)))
			}
		}
	}

	return issues
}

// checkImages は scenes[].image が実際に存在することを確かめる。
func checkImages(s *Script, baseDir string, loc *locator) []Issue {
	var issues []Issue
	for i, scene := range s.Scenes {
		if scene.Image == "" {
			continue // 空はスキーマ検証で弾かれている。
		}
		path := resolveImage(baseDir, scene.Image)
		info, err := os.Stat(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			p := loc.resolve([]step{key("scenes"), index(i), key("image")}, false)
			issues = append(issues, issueAt(p,
				fmt.Sprintf("画像が見つかりません: %s", path),
				"パスは台本ファイルからの相対で書きます"))
		case err != nil:
			p := loc.resolve([]step{key("scenes"), index(i), key("image")}, false)
			issues = append(issues, issueAt(p,
				fmt.Sprintf("画像を確認できません: %v", err), ""))
		case info.IsDir():
			p := loc.resolve([]step{key("scenes"), index(i), key("image")}, false)
			issues = append(issues, issueAt(p,
				fmt.Sprintf("%s はディレクトリです。画像ファイルを指定してください", path), ""))
		}
	}
	return issues
}

// speakerNames は定義済みの話者エイリアスを並べて返す。
func speakerNames(s *Script) []string {
	names := make([]string, 0, len(s.Speakers))
	for name := range s.Speakers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// hasSpeaker は話者エイリアスが定義済みかを返す。
func hasSpeaker(s *Script, name string) bool {
	_, ok := s.Speakers[name]
	return ok
}

// speakerHint は使える話者を並べた案内文を作る。
// 打ち間違いのときに、正しい綴りをその場で示せるようにするため。
func speakerHint(available []string) string {
	if len(available) == 0 {
		return "speakers に話者を定義してください"
	}
	return "使える話者: " + strings.Join(available, " / ")
}
