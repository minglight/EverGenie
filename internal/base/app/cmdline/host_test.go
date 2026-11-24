package cmdline

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func hostEnv() *Env {
	return &Env{Ctx: context.Background(), In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
}

// HostCommand：把本機指令實作包成 Command；結束碼 0 成功、2 用法錯誤、其他 ErrReported；沒實作說不支援；prepare 可以補 Extra。
func TestHostCommandExitCodes(t *testing.T) {
	t.Setenv("EVERGENIE_HOME", " /h ")
	var got HostRequest
	code := 0
	cmd := HostCommand("x", func(_ context.Context, r HostRequest) int { got = r; return code }, func(_ *Env, r *HostRequest) { r.Args = append(r.Args, "prepared") })
	if err := cmd.Run(hostEnv(), []string{"a"}); err != nil || got.Home != "/h" || len(got.Args) != 2 || got.Args[1] != "prepared" {
		t.Fatalf("成功：%v %+v", err, got)
	}
	code = 2
	var ue *UsageError
	if err := cmd.Run(hostEnv(), nil); !errors.As(err, &ue) {
		t.Fatalf("2＝用法錯誤：%v", err)
	}
	code = 1
	if err := cmd.Run(hostEnv(), nil); !errors.Is(err, ErrReported) {
		t.Fatalf("其他＝已回報：%v", err)
	}
	if err := HostCommand("y", nil, nil).Run(hostEnv(), nil); !errors.As(err, &ue) || !strings.Contains(ue.Msg, "不支援 y") {
		t.Fatalf("沒實作：%v", err)
	}
	var sub HostRequest
	if err := SubHost("service", "install", func(_ context.Context, r HostRequest) int { sub = r; return 0 }).Run(hostEnv(), []string{"--exe", "z"}); err != nil || len(sub.Args) != 3 || sub.Args[0] != "install" {
		t.Fatalf("SubHost：%v %+v", err, sub)
	}
}
