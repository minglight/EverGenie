package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// TestSharePackageBetweenHomes（SEC-302 S5）：兩個 fixture 家、同一個行程裡各起全部模組（真的 api 與看板 handler，不起 launchd、不碰正式家）。
// A 家 packages new garden → 手寫角色 personas/fairy.md＋[[contributes.persona]] → 寫一筆自己的資料 → packages export --git；
// 匯出的資料夾 git init＋tag 之後，B 家從 file:// 安裝：沒有 --yes 只預覽（列出帶來的角色）不裝，--yes 才裝；
// 裝好 personas list 有 garden.fairy、看板頁面清單有 /p/garden/index（編得過、有 bundle）、B 家沒有 vault/pkg/garden/。
// Guards: REQ-337, REQ-339, REQ-340
func TestSharePackageBetweenHomes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("這台沒有 git")
	}
	t.Setenv("EVERGENIE_AGENT", "")
	homeA, sockA := startFixtureAPI(t)
	homeB, sockB, boardB := startFixtureWebWith(t, nil)

	// A 家：建套件、加角色、寫一筆自己的資料。
	hgOK(t, sockA, "packages", "new", "garden", "--name", "花園")
	dir := filepath.Join(homeA, "packages", "mine", "garden")
	man, err := os.ReadFile(filepath.Join(dir, "package.toml"))
	if err != nil {
		t.Fatal(err)
	}
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "personas", "fairy.md"), "---\nschema: 1\nname: 花仙子\n---\n我是花園裡的小仙子，說話輕輕的。\n")
	write(filepath.Join(dir, "package.toml"), string(man)+"\n[[contributes.persona]]\nslug = \"fairy\"\npath = \"personas/fairy.md\"\n")
	write(filepath.Join(homeA, "vault", "pkg", "garden", "plant", "p-1.md"), "---\nid: p-1\n---\nA 家自己的澆水紀錄\n")
	hgOK(t, sockA, "packages", "reload")
	if list := hgOK(t, sockA, "personas", "list"); !strings.Contains(list, "garden.fairy") {
		t.Fatalf("A 家的角色庫要有 garden.fairy：%s", list)
	}

	out := filepath.Join(t.TempDir(), "garden")
	exp := hgOK(t, sockA, "packages", "export", "garden", "--out", out, "--git")
	if !strings.Contains(exp, "已匯出「garden」") || !strings.Contains(exp, "git init") {
		t.Fatalf("export：%s", exp)
	}
	if _, err := os.Stat(filepath.Join(out, ".gitignore")); err != nil {
		t.Fatalf("--git 要有 .gitignore：%v", err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
		cmd.Dir = out
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v：%v\n%s", args, err, b)
		}
	}
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-q", "-m", "garden 0.1.0")
	git("tag", "v0.1.0")
	url := "file://" + out

	// B 家：沒有 --yes 只預覽，不裝。
	pre := hgOK(t, sockB, "packages", "install", url)
	if !strings.Contains(pre, "帶來的角色：花仙子（garden.fairy）") || !strings.Contains(pre, "--yes") {
		t.Fatalf("預覽要列出帶來的角色並提醒 --yes：%s", pre)
	}
	if list := hgOK(t, sockB, "packages", "list"); strings.Contains(list, `"id":"garden"`) {
		t.Fatalf("沒有 --yes 不能裝：%s", list)
	}
	if _, err := os.Stat(filepath.Join(homeB, "packages", "installed", "garden")); !os.IsNotExist(err) {
		t.Fatal("沒有 --yes 不能有 packages/installed/garden")
	}
	if got := hgOK(t, sockB, "packages", "install", url, "--yes"); !strings.Contains(got, "已安裝") {
		t.Fatalf("--yes 安裝：%s", got)
	}

	// 角色庫馬上有 garden.fairy。
	var cards []sdk.PersonaCard
	for _, line := range strings.Split(strings.TrimSpace(hgOK(t, sockB, "personas", "list", "--json")), "\n") {
		var c sdk.PersonaCard
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("personas list --json：%q %v", line, err)
		}
		cards = append(cards, c)
	}
	found := false
	for _, c := range cards {
		if c.Slug == "garden.fairy" {
			found = c.Package == "garden" && c.Unassignable == ""
		}
	}
	if !found {
		t.Fatalf("B 家的角色庫要有可指派的 garden.fairy：%+v", cards)
	}

	// 看板：頁面 /p/garden/index 可解析、編得過。
	req := httptest.NewRequest(http.MethodPost, "/api/web/get_pages", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	boardB.ServeHTTP(w, req)
	var pages sdk.WebGetPagesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &pages); w.Code != http.StatusOK || err != nil {
		t.Fatalf("get_pages：%d %v %s", w.Code, err, w.Body.String())
	}
	ok := false
	for _, p := range pages.Pages {
		if p.ID == "garden.index" {
			ok = p.Route == "/p/garden/index" && p.Error == "" && p.BundleURL != ""
			if !ok {
				t.Fatalf("garden.index：%+v", p)
			}
		}
	}
	if !ok {
		t.Fatalf("B 家的看板要有 garden.index：%+v", pages.Pages)
	}
	w = httptest.NewRecorder()
	boardB.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/p/garden/index", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /p/garden/index → %d", w.Code)
	}

	// 資料不跟著走。
	if ents, err := os.ReadDir(filepath.Join(homeB, "vault", "pkg", "garden")); err == nil && len(ents) > 0 {
		t.Fatalf("B 家的 vault/pkg/garden 要不存在或是空的：%v", ents)
	}
	_ = filepath.WalkDir(filepath.Join(homeB, "packages", "installed", "garden"), func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), "澆水紀錄") {
				t.Errorf("A 家的資料跑到 B 家：%s", p)
			}
		}
		return nil
	})
}
