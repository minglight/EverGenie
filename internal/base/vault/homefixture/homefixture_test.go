package homefixture

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
)

// Guards: REQ-118
func TestHomefixtureRefusesRealHome(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "fixture-user")
	for _, path := range []string{filepath.Join(home, "."+platform.ProductName), filepath.Join(home, "."+platform.ProductName, "state", "home")} {
		if safePath(path, home) {
			t.Fatal("real home accepted")
		}
	}
	if !safePath(filepath.Join(home, "temporary"), home) {
		t.Fatal("temporary path rejected")
	}
	h := New(t)
	if err := h.Vault.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-104
func TestFreeWebPortRewritesPort(t *testing.T) {
	h := New(t)
	port := FreeWebPort(t, h.Dir)
	if port == 8850 || port < 1024 {
		t.Fatalf("port = %d", port)
	}
	loaded, err := platform.LoadHome(h.Dir)
	if err != nil || loaded.Web.Port != port {
		t.Fatalf("home.toml port = %d, want %d (%v)", loaded.Web.Port, port, err)
	}
	data, err := os.ReadFile(filepath.Join(h.Dir, "home.toml"))
	if err != nil || strings.Contains(string(data), "8850") || strings.Count(string(data), "port") != 1 {
		t.Fatalf("home.toml: %s %v", data, err)
	}
}
