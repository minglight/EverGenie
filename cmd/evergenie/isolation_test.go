package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/sdk"
)

// TestUserFeatureIsolation（SEC-302 S6，真 serve＋fixture 家、假引擎，不碰正式家）：使用者自己的套件不拖累別人。
//
//	① manifest 壞掉的套件（kind 寫錯、宣告別人的資料種類）只有它 rejected，官方套件與別的套件照常，doctor 只黃字
//	② 附程式的套件一直崩潰：發 system.alert，核心、API 與別的套件照常
//	③ 核心升級（migrate-home --apply、換新的執行檔重開 serve）後 packages/mine/、personas/mine/、vault/pkg/<id>/ 位元組不變
//	④ 移除套件：它的角色從清單消失，坐那位角色的秘書還在、指令組得出來，doctor 黃字而不是紅字（紅字會擋住部署）
//	⑤ 套件經 data 指令寫自己的種類，只落在 vault/pkg/<自己>/，別的套件的資料夾不動（D-19 的能力是宣告用的，這裡不承諾沙盒）
//
// Guards: REQ-359, REQ-360
func TestUserFeatureIsolation(t *testing.T) {
	dir := doctorHome(t)
	copyOfficialPackage(t, dir, "nutrition")
	writeTree(t, filepath.Join(dir, "packages", "mine"), map[string]string{
		// ① 壞掉的：kind 寫錯；偷宣告別人的資料種類（種類名稱不能帶點，核心自己加 <套件>. 前綴）。
		"broken/package.toml": "schema = 1\nid = \"broken\"\nname = \"壞掉的\"\nversion = \"0.1.0\"\ndescription = \"測試用\"\nkind = \"nonsense\"\napi = \"evergenie.v1\"\n",
		"sneaky/package.toml": "schema = 1\nid = \"sneaky\"\nname = \"偷寫的\"\nversion = \"0.1.0\"\ndescription = \"測試用\"\nkind = \"data\"\napi = \"evergenie.v1\"\n\n" +
			"[[contributes.data]]\nkind = \"nutrition.meal\"\nschema = \"schemas/meal.toml\"\nids = \"seq\"\nprefix = \"M-\"\n",
		"sneaky/schemas/meal.toml": "schema = 1\ntitle = \"餐\"\n\n[[field]]\nname = \"text\"\ntype = \"string\"\n",
		// ② 附程式、一啟動就結束（不報到）。
		"crashy/package.toml":       "schema = 1\nid = \"crashy\"\nname = \"會當機\"\nversion = \"0.1.0\"\ndescription = \"測試用\"\nkind = \"program\"\napi = \"evergenie.v1\"\n\n[program]\nentry = \"cmd/crashy\"\nrestart = \"always\"\n",
		"crashy/go.mod":             "module crashy\n\ngo 1.21\n",
		"crashy/cmd/crashy/main.go": "package main\n\nimport \"os\"\n\nfunc main() { os.Exit(3) }\n",
		// 好好的套件：一種資料、一位角色、一位秘書的樣板。
		"garden/package.toml": "schema = 1\nid = \"garden\"\nname = \"花園\"\nversion = \"0.1.0\"\ndescription = \"測試用\"\nkind = \"data\"\napi = \"evergenie.v1\"\n\n" +
			"[[contributes.agent]]\nid = \"garden\"\ntemplate = \"agents/garden/agent.md\"\n\n" +
			"[[contributes.persona]]\nslug = \"fairy\"\npath = \"personas/fairy.md\"\n\n" +
			"[[contributes.data]]\nkind = \"note\"\nschema = \"schemas/note.toml\"\nids = \"seq\"\nprefix = \"N-\"\n",
		"garden/agents/garden/agent.md": "---\nduty: 照顧花園\n---\n\n# 花園的職責\n\n記澆水。\n",
		"garden/personas/fairy.md":      "---\nschema: 1\nname: 花仙子\n---\n我是花園裡的小仙子。\n",
		"garden/schemas/note.toml":      "schema = 1\ntitle = \"花園筆記\"\n\n[[field]]\nname = \"text\"\ntype = \"string\"\nrequired = true\n",
	})
	_, sock, stop := serveIsolation(t, dir)
	t.Setenv("EVERGENIE_HOME", dir)
	pkgs := sdk.NewPackageClient(sdk.Dial(sock))
	state := func(id string) sdk.PackageState {
		t.Helper()
		r, err := pkgs.Get(context.Background(), &sdk.PackageGetRequest{ID: id})
		if err != nil {
			return ""
		}
		if r.Package.Error != "" {
			t.Logf("%s：%s %s", id, r.Package.State, r.Package.Error)
		}
		return r.Package.State
	}

	// ①
	for _, id := range []string{"broken", "sneaky"} {
		if s := state(id); s != sdk.PackageStateRejected {
			t.Fatalf("%s 要被拒：%s", id, s)
		}
	}
	for _, id := range []string{"nutrition", "garden"} {
		if s := state(id); s != sdk.PackageStateRunning {
			t.Fatalf("別人的套件壞了，%s 要照常：%s", id, s)
		}
	}

	// ② 編好、啟動、馬上結束 → system.alert；別的照常。
	waitUntil(t, "crashy 掛了的 system.alert", 90*time.Second, func() bool { return hasNotice(t, sock, "會當機") })
	if s := state("crashy"); s != sdk.PackageStateFailed && s != sdk.PackageStateBuilding {
		t.Fatalf("crashy 要是 failed（或正在重啟）：%s", s)
	}
	for _, id := range []string{"nutrition", "garden"} {
		if s := state(id); s != sdk.PackageStateRunning {
			t.Fatalf("crashy 一直掛，%s 要照常：%s", id, s)
		}
	}

	// 角色指派、我的角色、兩邊各寫一筆資料。
	hgOK(t, sock, "packages", "sync", "garden", "--persona", "garden.fairy", "--apply")
	if f, ok := agentFields(t, sock, "garden"); !ok || f["persona"] != "garden.fairy" {
		t.Fatalf("garden 秘書坐 garden.fairy：%v", f)
	}
	hgOK(t, sock, "agents", "set", "garden", "enabled", "true") // 停用的秘書 doctor 不看；要上班的才會被套件不見影響
	hgOK(t, sock, "personas", "new", "momo", "--name", "桃桃")
	t.Setenv("EVERGENIE_AGENT", "garden")
	hgOK(t, sock, "data", "add", "garden.note", "--json", `{"text":"玫瑰澆水"}`)
	t.Setenv("EVERGENIE_AGENT", "health")
	hgOK(t, sock, "data", "add", "nutrition.goal", "--id", "main", "--json", `{"mode":"維持","kcal":2000,"protein":100,"carb":0,"fat":0}`)
	t.Setenv("EVERGENIE_AGENT", "")

	// ⑤ garden 的資料只在 vault/pkg/garden/；別的套件的資料夾不因 garden 寫入而變。
	nutritionBefore := treeHash(t, filepath.Join(dir, "vault", "pkg", "nutrition"))
	t.Setenv("EVERGENIE_AGENT", "garden")
	hgOK(t, sock, "data", "add", "garden.note", "--json", `{"text":"番茄施肥"}`)
	t.Setenv("EVERGENIE_AGENT", "")
	if got := treeHash(t, filepath.Join(dir, "vault", "pkg", "nutrition")); got != nutritionBefore {
		t.Fatal("garden 寫資料不能動到 vault/pkg/nutrition/")
	}
	if n := countFiles(t, filepath.Join(dir, "vault", "pkg", "garden")); n != 2 {
		t.Fatalf("garden 的兩筆要在 vault/pkg/garden/：%d", n)
	}
	for _, id := range []string{"broken", "sneaky", "crashy"} {
		if _, err := os.Stat(filepath.Join(dir, "vault", "pkg", id)); err == nil {
			t.Fatalf("沒寫資料的套件不會有 vault/pkg/%s/", id)
		}
	}

	// ③ 核心升級：記下使用者的東西 → 停 serve → migrate-home --apply → 用新的模組重開 → 重新載入 → 一個位元組都沒變。
	keep := []string{"packages/mine", "personas/mine", "vault/pkg/garden"}
	before := map[string]string{}
	for _, rel := range keep {
		before[rel] = treeHash(t, filepath.Join(dir, rel))
	}
	stop()
	if code, out, errOut := hg(t, sock, "", "migrate-home", "--apply", "--home", dir); code != 0 {
		t.Fatalf("migrate-home：%d %s %s", code, out, errOut)
	}
	m, sock, _ := serveIsolation(t, dir)
	pkgs = sdk.NewPackageClient(sdk.Dial(sock))
	hgOK(t, sock, "packages", "reload")
	waitUntil(t, "重開後 garden 照常", 30*time.Second, func() bool { return state("garden") == sdk.PackageStateRunning })
	for _, rel := range keep {
		if got := treeHash(t, filepath.Join(dir, rel)); got != before[rel] {
			t.Fatalf("升級後 %s 被動到了", rel)
		}
	}

	// ④ 移除 garden：角色從清單消失，秘書還在、組得出指令，doctor 只黃字。
	if err := os.RemoveAll(filepath.Join(dir, "packages", "mine", "garden")); err != nil {
		t.Fatal(err)
	}
	hgOK(t, sock, "packages", "reload")
	waitUntil(t, "garden 從套件清單消失", 30*time.Second, func() bool { return state("garden") == "" })
	if out := hgOK(t, sock, "personas", "list", "--json"); strings.Contains(out, `"garden.fairy"`) {
		t.Fatalf("移除後角色要從清單消失：%s", out)
	}
	if f, ok := agentFields(t, sock, "garden"); !ok || f["persona"] != "garden.fairy" {
		t.Fatalf("秘書還在、原值不動：%v", f)
	}
	if _, err := m.agent.Instructions(context.Background(), "garden", "garden-iso"); err != nil {
		t.Fatalf("角色的套件不見了，秘書也要起得來：%v", err)
	}
	if s := state("nutrition"); s != sdk.PackageStateRunning {
		t.Fatalf("移除 garden 不影響 nutrition：%s", s)
	}
	_, checks := doctorJSON(t, sock, dir)
	var warned []string
	for _, c := range checks {
		if c.Severity == sdk.DoctorSeverityError {
			t.Errorf("使用者的套件壞了或不見了，doctor 不能紅字（會擋部署）：%+v", c)
		}
		if c.Severity == sdk.DoctorSeverityWarn {
			warned = append(warned, c.ID+"："+c.Detail)
		}
	}
	joined := strings.Join(warned, "\n")
	// crashy 在重開後依退避重啟，doctor 那一刻可能是 failed 或正在編、啟動，所以不在這裡要求。
	for _, want := range []string{"config.agent_feature：秘書 ", "garden.fairy 來自的套件不在了", "packages.feature_broken：你的套件「broken」", "你的套件「sneaky」"} {
		if !strings.Contains(joined, want) {
			t.Errorf("doctor 黃字少了「%s」：\n%s", want, joined)
		}
	}
}

// serveIsolation 起全部模組並接上假引擎；回模組、socket 與停止的函式（停過之後 Cleanup 不再停一次）。
func serveIsolation(t *testing.T, dir string) (*modules, string, func()) {
	t.Helper()
	m, err := buildModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	stop := func() {
		if err := m.app.Stop(ctx); err != nil { // 停過再停什麼都不做
			t.Error(err)
		}
	}
	t.Cleanup(stop)
	if err := m.app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.engines.Attach("fake", fake.New(nil)); err != nil {
		t.Fatal(err)
	}
	return m, filepath.Join(dir, "runtime/sockets/api.sock"), stop
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// treeHash 是整個資料夾（路徑、權限、內容）的指紋；資料夾不存在回空字串。
func treeHash(t *testing.T, root string) string {
	t.Helper()
	if _, err := os.Stat(root); err != nil {
		return ""
	}
	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		info, err := d.Info()
		if err != nil {
			return err
		}
		line := rel + " " + info.Mode().String()
		if d.Type().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			line += " " + hex.EncodeToString(sum[:])
		}
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

func countFiles(t *testing.T, root string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && strings.HasSuffix(d.Name(), ".md") {
			n++
		}
		return nil
	})
	return n
}

func hasNotice(t *testing.T, sock, title string) bool {
	t.Helper()
	r, err := sdk.NewNotifyClient(sdk.Dial(sock)).List(context.Background(), &sdk.NotifyListRequest{})
	if err != nil {
		return false
	}
	for _, n := range r.Notifications {
		if n.Source == "system.alert" && strings.Contains(n.Title, title) {
			return true
		}
	}
	return false
}
