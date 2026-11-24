package platform

import (
	"path/filepath"
	"testing"
)

// Guards: REQ-10

// Guards: REQ-254
func TestLoadHomeExpandsTilde(t *testing.T) {
	user := t.TempDir()
	t.Setenv("HOME", user)
	h, err := LoadHome(configDir(t, homeConfig+"[sessions]\nworkdir = \"~\"\n[backup]\nsnapshot_dir = \"~/bk\"\n[secrets]\nfile = \"~/.secret.sh\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string][2]string{
		"workdir": {h.Sessions.Workdir, user},
		"secrets": {h.SecretsPath(), filepath.Join(user, ".secret.sh")},
	} {
		if tc[0] != tc[1] {
			t.Errorf("%s：%q，應該是 %q", name, tc[0], tc[1])
		}
	}
	// 不是 ~ 開頭的照舊（相對路徑仍然接在家底下）。
	for in, want := range map[string]string{"/abs/x": "/abs/x", "rel/x": "rel/x", "~user/x": "~user/x", "": ""} {
		if got := ExpandHome(in); got != want {
			t.Errorf("ExpandHome(%q) = %q，應該是 %q", in, got, want)
		}
	}
}

func TestLegacyBackupAndServersSettingsAreIgnored(t *testing.T) {
	dir := configDir(t, homeConfig+"\n[backup]\nto = 42\nurl = false\nsnapshot_dir = []\n[servers]\nhub = false\n[incidents]\nowner = \"code\"\n")
	h, err := LoadHome(dir)
	if err != nil {
		t.Fatal(err)
	}
	if h.Incidents.Owner != "code" {
		t.Fatal(h.Incidents.Owner)
	}
}
