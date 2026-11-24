package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// DEV-88：資料指令的 --home 從參數裡拿掉並換成絕對路徑；serve／init／service／tailscale 自己處理，不動。
func TestExtractHomeFlag(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "h")
	cases := []struct {
		args []string
		home string
		rest []string
		bad  bool
	}{
		{[]string{"roster"}, "", []string{"roster"}, false},
		{[]string{"roster", "--home", abs}, abs, []string{"roster"}, false},
		{[]string{"--home=" + abs, "tasks", "list", "--status", "open"}, abs, []string{"tasks", "list", "--status", "open"}, false},
		{[]string{"tasks", "add", "--home", abs, "標題"}, abs, []string{"tasks", "add", "標題"}, false},
		{[]string{"init", "--home", abs}, "", []string{"init", "--home", abs}, false},
		{[]string{"serve", "--home", abs}, "", []string{"serve", "--home", abs}, false},
		{[]string{"service", "status", "--home", abs}, "", []string{"service", "status", "--home", abs}, false},
		{[]string{"roster", "--home"}, "", nil, true},
		{[]string{"roster", "--home", "--json"}, "", nil, true},
		{[]string{"roster", "--home="}, "", nil, true},
	}
	for _, c := range cases {
		home, rest, err := extractHomeFlag(c.args)
		if (err != nil) != c.bad {
			t.Fatalf("%v：err=%v", c.args, err)
		}
		if c.bad {
			continue
		}
		if home != c.home || !reflect.DeepEqual(rest, c.rest) {
			t.Fatalf("%v：home=%q rest=%v，想要 %q %v", c.args, home, rest, c.home, c.rest)
		}
	}
}

func TestUseHomeFlagSetsEnv(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "h")
	got := ""
	rest, err := useHomeFlag([]string{"roster", "--home", abs}, func(k, v string) error {
		if k != "EVERGENIE_HOME" {
			t.Fatal(k)
		}
		got = v
		return nil
	})
	if err != nil || got != abs || !reflect.DeepEqual(rest, []string{"roster"}) {
		t.Fatal(got, rest, err)
	}
}

func TestOtherHomeNoteHasLabelOrPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "沒有這個家")
	if got := otherHomeNote(missing); !strings.Contains(got, missing) || !strings.Contains(got, "另一個家") {
		t.Fatal(got)
	}
}
