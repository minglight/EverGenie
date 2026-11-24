package run

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 開發家的 api.sock 照它自己 home.toml 的 [layout] 找（DEV-145）：第 1 版 <家>/runtime/，第 2 版在狀態資料夾；
// 這個家的 EVERGENIE_STATE 不能影響別的家。
func TestDevHomeSocketFollowsItsLayout(t *testing.T) {
	t.Setenv(sdk.EnvState, t.TempDir()) // 這個家的狀態資料夾：不能拿來找開發家
	cases := map[string]struct {
		toml string
		want func(dir string) string
	}{
		"第 1 版":         {"[web]\nport = 18870\n", func(d string) string { return filepath.Join(d, "runtime", "sockets", "api.sock") }},
		"第 2 版相對 state": {"[layout]\nversion = 2\nstate = \"../st\"\n", func(d string) string { return filepath.Join(d, "..", "st", "runtime", "sockets", "api.sock") }},
		"第 2 版絕對 state": {"[layout]\nversion = 2\nstate = \"/abs/state\"\n", func(string) string { return "/abs/state/runtime/sockets/api.sock" }},
		"第 2 版沒寫 state": {"[layout]\nversion = 2\n", func(d string) string {
			return filepath.Join(expandHome("~/.evergenie/state"), filepath.Base(d), "runtime", "sockets", "api.sock")
		}},
	}
	for name, c := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "home.toml"), []byte(c.toml), 0o644); err != nil {
			t.Fatal(err)
		}
		var got string
		doctor := func(_ context.Context, sock string) (*sdk.SystemDoctorResponse, error) {
			got = sock
			return &sdk.SystemDoctorResponse{}, nil
		}
		ProbeDevHome(context.Background(), dir, doctor, time.Second)
		if want := filepath.Clean(c.want(dir)); got != want {
			t.Errorf("%s：sock = %s，要 %s", name, got, want)
		}
	}
}
