package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-84
func TestTrustActionsValidation(t *testing.T) {
	for _, section := range []string{"approve", "deny"} {
		for _, value := range []string{"'not-array'", "true", "1", "['read', 1]"} {
			t.Run(section+value, func(t *testing.T) {
				dir := configDir(t, homeConfig)
				if err := os.MkdirAll(filepath.Join(dir, "settings"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "settings/trust.toml"), []byte("["+section+"]\nactions="+value+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := LoadHome(dir); CodeOf(err) != sdk.ErrorCodeInvalid {
					t.Fatal(err)
				}
			})
		}
	}
	for _, body := range []string{"schema=1\n", "[approve]\nactions=[]\n[deny]\nactions=['delete']\n"} {
		dir := configDir(t, homeConfig)
		if err := os.MkdirAll(filepath.Join(dir, "settings"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "settings/trust.toml"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadHome(dir); err != nil {
			t.Fatal(err)
		}
	}
}

// Guards: REQ-84, REQ-120
func TestTrustUnknownFieldsWarnWithoutValues(t *testing.T) {
	dir := configDir(t, homeConfig)
	if err := os.MkdirAll(filepath.Join(dir, "settings"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings/trust.toml"), []byte("[approve]\nunknown_a='do-not-log-value'\n[deny]\nunknown_b='do-not-log-value'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHome(dir); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "runtime/logs/*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Count(s, "WARN") != 1 || strings.Contains(s, "do-not-log-value") || !strings.Contains(s, "approve.unknown_a") || !strings.Contains(s, "deny.unknown_b") {
		t.Fatal("unexpected warning contents")
	}
}

func writeTrust(t *testing.T, body string) string {
	t.Helper()
	dir := configDir(t, homeConfig)
	if err := os.MkdirAll(filepath.Join(dir, "settings"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings/trust.toml"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Guards: REQ-84
func TestTrustLevelValidation(t *testing.T) {
	for _, body := range []string{
		"default_level='root'\n",
		"[agent.code]\nlevel='admin'\n",
		"[agent.code]\n",
		"[agent.' ']\nlevel='full'\n",
	} {
		if _, err := LoadHome(writeTrust(t, body)); CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Errorf("%q 要回 Invalid：%v", body, err)
		}
	}
}
