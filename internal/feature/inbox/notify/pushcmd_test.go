package notify

import (
	"os"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
)

func TestPushInitOfflineAndPreservesExistingKeys(t *testing.T) {
	h := homefixture.New(t)
	code, out, errs := run(t, nil, "", "push", "init", "--home", h.Dir)
	if code != 0 {
		t.Fatalf("%d %s", code, errs)
	}
	config, err := platform.LoadHome(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	path := config.SecretsPath()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("秘密檔權限: %v %v", info, err)
	}
	if strings.Contains(out, "vapid_private_key") {
		t.Fatal("不應印秘密")
	}
	code, _, errs = run(t, nil, "", "push", "init", "--home", h.Dir)
	after, err := os.ReadFile(path)
	if code != 0 || err != nil || string(after) != string(before) {
		t.Fatalf("existing keys changed: %d %s %v", code, errs, err)
	}
	if code, _, _ := run(t, nil, "", "push", "init", "--home", h.Dir, "--subject", "invalid"); code != 2 {
		t.Fatalf("invalid subject: %d", code)
	}
}
