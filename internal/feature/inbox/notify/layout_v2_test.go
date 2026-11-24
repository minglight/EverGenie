package notify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// useLayoutV2 把測試的家改成佈局第 2 版（DEV-145），回傳狀態資料夾。
func useLayoutV2(t *testing.T, home string) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	f, err := os.OpenFile(filepath.Join(home, "home.toml"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "\n[layout]\nversion = 2\nstate = %q\n", state); err != nil {
		t.Fatal(err)
	}
	return state
}

// TestLayoutV2ShareInMedia：第 2 版的家，分享的附件寫在家裡的 media/share/（不在 runtime/）；
// 狀態資料夾的 secrets/ 裡的檔不能分享。
func TestLayoutV2ShareInMedia(t *testing.T) {
	m, h, _, _ := fixture(t)
	state := useLayoutV2(t, h.Dir)
	path := filepath.Join(t.TempDir(), "fixture.txt")
	write(t, path, "shared bytes\n")
	out, err := m.Share(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimPrefix(out.URL, "/share/")
	data, err := os.ReadFile(filepath.Join(h.Dir, "media", "share", name))
	if err != nil || string(data) != "shared bytes\n" {
		t.Fatalf("附件要在 media/share：%v", err)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "runtime", "share", name)); err == nil {
		t.Fatal("第 2 版不該寫到 runtime/share")
	}
	secret := filepath.Join(state, "secrets", "other.toml")
	write(t, secret, "TOKEN=x\n")
	if _, err := m.Share(ctx, secret); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("狀態資料夾的 secrets/ 不能分享：%v", err)
	}
}
