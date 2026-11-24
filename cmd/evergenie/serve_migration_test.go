package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/feature/system/update"
)

func upgradeHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "h")
	if _, err := initHome(initOptions{Home: home, Name: "測試", Timezone: "Asia/Taipei", Port: 18999, State: filepath.Join(t.TempDir(), "state")}); err != nil {
		t.Fatal(err)
	}
	return home
}

// serve 啟動前先跑家的遷移，失敗就拒絕啟動（不開任何模組）。
// Guards: REQ-280
func TestServeRefusesWhenMigrationFails(t *testing.T) {
	home := upgradeHome(t)
	old := update.HomeMigrations
	t.Cleanup(func() { update.HomeMigrations = old })
	update.HomeMigrations = []update.HomeMigration{{ID: "boom", Description: "炸掉的整理", Run: func(string) error { return errors.New("炸了") }}}
	var out, errOut bytes.Buffer
	if code := serve([]string{"--home", home}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "serve 拒絕啟動") || !strings.Contains(errOut.String(), "炸掉的整理") {
		t.Fatalf("%d %q", code, errOut.String())
	}
}
