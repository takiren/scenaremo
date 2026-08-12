package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRun_引数が無ければ使い方を出して失敗する(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), nil, &stdout, &stderr)

	if code == exitSuccess {
		t.Error("引数が無いのに成功として終了した")
	}
	if !strings.Contains(stderr.String(), "使い方:") {
		t.Errorf("使い方が出ていない: %s", stderr.String())
	}
	// 使えるコマンドが分かること
	if !strings.Contains(stderr.String(), "doctor") {
		t.Errorf("コマンド一覧が出ていない: %s", stderr.String())
	}
}

func TestRun_知らないコマンドは名前を挙げて失敗する(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"buiId"}, &stdout, &stderr)

	if code != exitFailure {
		t.Errorf("終了コードが違う: %d", code)
	}
	msg := stderr.String()
	for _, want := range []string{"知らないコマンド", "buiId", "doctor"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q が含まれない: %s", want, msg)
		}
	}
	if stdout.Len() != 0 {
		t.Errorf("失敗なのに標準出力へ書いている: %s", stdout.String())
	}
}

func TestRun_helpは標準出力へ出して成功する(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(context.Background(), []string{arg}, &stdout, &stderr)

			if code != exitSuccess {
				t.Errorf("終了コードが違う: %d", code)
			}
			if !strings.Contains(stdout.String(), "doctor") {
				t.Errorf("コマンド一覧が標準出力に出ていない: %s", stdout.String())
			}
		})
	}
}
