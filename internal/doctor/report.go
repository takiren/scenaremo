package doctor

import (
	"fmt"
	"strings"
)

// actionIndent は対処の手順を項目名の下へぶら下げるための字下げ。
// ステータス欄 "[ OK ] " と同じ幅にしてあり、どの項目に対する手順なのかが目で追えるようにしている。
const actionIndent = "       "

// Text は人が読む診断結果を組み立てる。
//
// io.Writer へ直接書かずに文字列を返すのは、出力の形そのものがこのパッケージの成果物であり、
// テストで丸ごと突き合わせられるようにしておきたいため。書き出しは呼び出し側の 1 行で済む。
func (r Report) Text() string {
	var b strings.Builder

	b.WriteString("scenaremo doctor: 動かすために必要なものが揃っているか確認します\n\n")

	for _, c := range r.Checks {
		fmt.Fprintf(&b, "[ %s ] %s: %s\n", c.Status.Label(), c.Name, c.Detail)
		for _, action := range c.Actions {
			fmt.Fprintf(&b, "%s→ %s\n", actionIndent, action)
		}
	}

	b.WriteString("\n")
	b.WriteString(r.summary())
	return b.String()
}

// summary は末尾の 1 行。
//
// 失敗があるときに「次に何をするか」をもう一度言うのは、項目が多いと上へ戻る手間が要るためで、
// ここだけ読んでも動けるようにしている。
func (r Report) summary() string {
	failures := r.Failures()
	if len(failures) == 0 {
		return fmt.Sprintf("%d 件すべて問題ありません。scenaremo init で動画を作り始められます。\n", len(r.Checks))
	}

	names := make([]string, 0, len(failures))
	for _, c := range failures {
		names = append(names, c.Name)
	}
	return fmt.Sprintf(
		"%d 件中 %d 件が要対応です（%s）。上の → の手順を実行してから、もう一度 scenaremo doctor を実行してください。\n",
		len(r.Checks), len(failures), strings.Join(names, ", "),
	)
}
