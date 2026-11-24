package persona

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

func packRun(t *testing.T, home string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	full := append([]string{"personas", "pack"}, args...)
	if home != "" {
		full = append(full, "--home", home)
	}
	// 套件設定讀起來沒有照紀錄帶圖的種類（這裡的家沒有衣櫃）：連不到服務的 --no-records 照建（DEV-160 審查 M2）
	cmds := Commands(func(string) string { return sdk.Getenv("EVERGENIE_PERSONAS_PACK_POINTER") }, nil, func(string) ([]string, error) { return nil, nil })
	code := cmdline.Run(context.Background(), full, nil, cmdline.IO{Out: &out, Err: &errOut}, cmds, "用法\n")
	return code, out.String(), errOut.String()
}

func packWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPersonasPackCommands：`personas pack build` 在角色包來源的家（有寫 public_personas）建包：服務沒在跑時不加 --no-records 就不建（PRS-4），
// 加了才建只有圖與卡的包、印 sha256 與總結，`--set-pointer` 寫指標；使用者的家 `sync`（服務沒在跑，指令自己換檔案並說清楚，PRS-5）下載同版號的包（EVERGENIE_PERSONAS_PACK_POINTER 指到那份指標）、
// `status` 看得到版號；指標指到不存在的包時 sync 結束碼 1、不換任何檔，offline doctor 多一條黃字，`status` 看得到待補。
// `cards --repo` 把公開卡寫進 repo（`--set-pointer` 也一起寫，PRS-26）。
// Guards: REQ-283, REQ-131, PRS-26, PRS-4, PRS-5
func TestPersonasPackCommands(t *testing.T) {
	pub := homefixture.New(t).Dir
	data, _ := os.ReadFile(filepath.Join(pub, "home.toml"))
	packWrite(t, filepath.Join(pub, "home.toml"), string(data)+"\n[admin]\npublic_personas = [\"athena\"]\n")
	packWrite(t, filepath.Join(pub, "personas", "athena.md"), "---\nschema: 1\nname: Athena\n---\n公開\n")
	packWrite(t, filepath.Join(pub, "personas", "avatars", "athena.png"), "PNG")
	packWrite(t, filepath.Join(pub, "personas", "avatars", "athena.locked.png"), "SECRET")
	repo, out := t.TempDir(), filepath.Join(t.TempDir(), "pack")
	packWrite(t, filepath.Join(repo, "packages", "official", "personas", "package.toml"), "schema = 1\n")
	// 這個家的服務沒在跑：不加 --no-records 就不建，白話說怎麼辦、什麼都不寫。
	code, stdout, stderr := packRun(t, pub, "build", "--out", out, "--version", "2026.10.29")
	if code != 1 || !strings.Contains(stderr, "連不到這個家的 EverGenie 服務") || !strings.Contains(stderr, "--no-records") || stdout != "" {
		t.Fatalf("服務沒在跑的 build：%d\n%s\n%s", code, stdout, stderr)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("不建包就不寫輸出資料夾")
	}
	code, stdout, stderr = packRun(t, pub, "build", "--out", out, "--version", "2026.10.29", "--set-pointer", repo, "--url-base", "file://"+out, "--no-records")
	if code != 0 || !strings.Contains(stdout, "manifest sha256：") || !strings.Contains(stdout, "沒收：avatars/athena.locked.png") || !strings.Contains(stdout, "已寫入指標") ||
		!strings.Contains(stdout, "角色包 2026.10.29：1 位角色、") || !strings.Contains(stdout, "注意：加了 --no-records") {
		t.Fatalf("build：%d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "跟著程式的公開卡：") || !strings.Contains(stdout, "寫入：athena") {
		t.Fatalf("--set-pointer 要連公開卡一起寫（PRS-26）：\n%s", stdout)
	}
	code, stdout, stderr = packRun(t, pub, "cards", "--repo", repo)
	if code != 0 || !strings.Contains(stdout, "沒變：athena") {
		t.Fatalf("cards：%d\n%s\n%s", code, stdout, stderr)
	}
	if code, _, stderr = packRun(t, pub, "cards", "--repo", t.TempDir()); code != 1 || !strings.Contains(stderr, "不是 EverGenie 的 repo") {
		t.Fatalf("cards 指到不是 repo 的地方：%d %s", code, stderr)
	}
	pointer := filepath.Join(repo, "packages", "official", "personas", "pack.toml")
	t.Setenv("EVERGENIE_PERSONAS_PACK_POINTER", pointer)

	user := homefixture.New(t).Dir
	code, stdout, stderr = packRun(t, user, "sync")
	if code != 0 || !strings.Contains(stdout, "角色包已同步到 2026.10.29") || !strings.Contains(stdout, "這個家的服務沒在跑，這次由指令自己同步") {
		t.Fatalf("sync（服務沒在跑，指令自己換檔案並說清楚）：%d\n%s\n%s", code, stdout, stderr)
	}
	if b, _ := os.ReadFile(filepath.Join(user, "personas", "athena.md")); !strings.Contains(string(b), "公開") {
		t.Fatalf("卡片要在：%q", b)
	}
	if _, err := os.Stat(filepath.Join(user, "personas", "avatars", "athena.locked.png")); err == nil {
		t.Fatal("鎖定版不能出現")
	}
	code, stdout, _ = packRun(t, user, "status", "--json")
	var st struct{ Registered, Latest string }
	if code != 0 || json.Unmarshal([]byte(stdout), &st) != nil || st.Registered != "2026.10.29" || st.Latest != "2026.10.29" {
		t.Fatalf("status：%d %s", code, stdout)
	}
	// 角色包來源的家（有 public_personas）不同步、build 在不是來源的家失敗、用法錯誤結束碼 2。
	if code, stdout, _ = packRun(t, pub, "sync"); code != 0 || !strings.Contains(stdout, "角色包的來源") {
		t.Fatalf("來源的家：%d %s", code, stdout)
	}
	if code, _, _ = packRun(t, user, "build", "--out", filepath.Join(t.TempDir(), "x")); code != 1 {
		t.Fatalf("不是來源的家不能建包：%d", code)
	}
	if code, _, _ = packRun(t, user, "frobnicate"); code != 2 {
		t.Fatalf("不認得的子指令：%d", code)
	}
	if code, _, _ = packRun(t, user); code != 2 {
		t.Fatalf("沒給子指令：%d", code)
	}
	// 新版的包連不上：什麼都不換、結束碼 1、doctor 有黃字。
	packWrite(t, pointer, "version = \"2026.10.30\"\nurl = \"file://"+filepath.Join(t.TempDir(), "gone")+"/m/2026.10.30.json\"\nsha256 = \""+strings.Repeat("b", 64)+"\"\n")
	code, stdout, _ = packRun(t, user, "sync")
	if code != 1 || !strings.Contains(stdout, "沒有同步成功") {
		t.Fatalf("連不上要失敗：%d %s", code, stdout)
	}
	var warned bool
	for _, c := range OfflineChecks(context.Background(), user) {
		if c.ID == "packages.personas_pack" {
			warned = c.Severity == sdk.DoctorSeverityWarn && strings.Contains(c.Detail, "角色包還沒補上")
		}
	}
	if !warned {
		t.Fatal("doctor 要有黃字")
	}
	if _, stdout, _ = packRun(t, user, "status"); !strings.Contains(stdout, "待補") || !strings.Contains(stdout, "登記的版號：2026.10.29") {
		t.Fatalf("status 要顯示待補：%s", stdout)
	}
}

// TestOfficialPersonasPackageHasNoPack：官方套件 personas 的 pack.toml 先放空的 url（＝這一版沒有角色包，等 DEV-158 定下載點）；
// 同步什麼都不做、不是錯誤，白話說「還沒有公開的角色包」、卡已經跟著程式（PRS-25）；status 也這樣說。
// Guards: REQ-283, PRS-25
func TestOfficialPersonasPackageHasNoPack(t *testing.T) {
	t.Setenv("EVERGENIE_PERSONAS_PACK_POINTER", filepath.Join("..", "..", "..", "..", "packages", "official", "personas", "pack.toml"))
	user := homefixture.New(t).Dir
	code, stdout, stderr := packRun(t, user, "sync")
	if code != 0 || !strings.Contains(stdout, "還沒有公開的角色包") || !strings.Contains(stdout, "角色卡已經跟著程式裝好") || strings.Contains(stdout, "pack") {
		t.Fatalf("%d %s %s", code, stdout, stderr)
	}
	if code, stdout, stderr = packRun(t, user, "status"); code != 0 || !strings.Contains(stdout, "這一版的版號：還沒有公開的角色包") {
		t.Fatalf("%d %s %s", code, stdout, stderr)
	}
}

// 指令只在連不上這個家的 socket（服務沒在跑）時才自己同步。服務回的錯誤、連上之後斷掉、指令自己被取消（Ctrl-C）都不算：
// 服務那邊可能還在做（它不跟著這條連線走），指令再同步一次就會兩邊同時換檔。
// Guards: PRS-5
func TestPackSyncFallsBackOnlyWhenSocketUnreachable(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	call := func(ctx context.Context, sock string) error {
		_, err := sdk.NewPersonasClient(sdk.Dial(sock)).PackSync(ctx, &sdk.PersonasPackSyncRequest{By: &sdk.Actor{Name: "owner(cli)"}})
		return err
	}
	if err := call(context.Background(), filepath.Join(dir, "none.sock")); !serviceDown(err) {
		t.Fatalf("沒有 socket＝服務沒在跑：%v", err)
	}
	sock := filepath.Join(dir, "api.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close() // 連上了、什麼都沒回就斷掉
		}
	}()
	if err := call(context.Background(), sock); err == nil || serviceDown(err) {
		t.Fatalf("連上之後斷掉不算沒在跑：%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := call(ctx, filepath.Join(dir, "none.sock")); err == nil || serviceDown(err) {
		t.Fatalf("指令被取消不算沒在跑：%v", err)
	}
	if serviceDown(&sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "套件服務還沒準備好"}) {
		t.Fatal("服務回的錯誤不算沒在跑")
	}
}

// 還不認得 personas.pack_sync 的舊服務（升級途中、指令比服務新）回 not_found：指令自己同步，不當成失敗。
// Guards: PRS-5
func TestPackSyncFallsBackOnOldService(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	sock := platform.RuntimeDir(home, "sockets", "api.sock")
	if err := os.MkdirAll(filepath.Dir(sock), 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"not_found","message":"沒有這個 API"}`))
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	e := &cmdline.Env{Ctx: context.Background(), By: "owner(cli)"}
	res, delegated, err := syncViaService(e, home, "", false)
	if delegated || res != nil || err != nil {
		t.Fatalf("舊服務：要指令自己同步：%v %+v %v", delegated, res, err)
	}
}
