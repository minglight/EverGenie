package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// bundleOf 編譯（或取快取）套件 demo 的一頁：回產物 hash 或給使用者看的白話錯誤。選單怎麼把它放進頁面清單在 internal/feature/inbox/menu 測。
func bundleOf(e *env, page string) (hash, errText string) {
	h, err := e.as.Bundle("demo", page)
	if err != nil {
		return "", err.Error()
	}
	return h, ""
}

// Guards: REQ-114
func TestPackagePageCompiledAndServed(t *testing.T) {
	e := newEnv(t, "")
	withCatalog(t, e, demoPackage("demo", sdk.TierMine))
	hash, errText := bundleOf(e, "home")
	if errText != "" || len(hash) != 16 {
		t.Fatalf("demo.home = %q %q", hash, errText)
	}
	url := "/pkg-bundles/" + hash + ".js"
	w := get(e.h, url)
	if w.Code != 200 {
		t.Fatalf("bundle → %d %s", w.Code, w.Body.String())
	}
	h := w.Header()
	if h.Get("Cache-Control") != "public, max-age=31536000, immutable" || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("bundle 標頭：%v", h)
	}
	body := w.Body.String()
	// react 與外殼的共用模組沒有被打包進來，改成讀 globalThis.__hgShared；套件自己的檔案（lib/util）有被打包。
	if !strings.Contains(body, "__hgShared") || !strings.Contains(body, `"react/jsx-runtime"`) || !strings.Contains(body, `"@evergenie/ui"`) {
		t.Fatalf("bundle 沒有用 __hgShared：%.300s", body)
	}
	if strings.Contains(body, "react.production") || strings.Contains(body, "react.development") || !strings.Contains(body, "demo ") {
		t.Fatalf("bundle 內容不對（不該帶 React，要帶套件自己的 lib）：%.300s", body)
	}
	// 檔名就是內容雜湊，存在家的 runtime/web/bundles/。
	if _, err := os.Stat(filepath.Join(e.home, "runtime", "web", "bundles", hash+".js")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"/pkg-bundles/zzzz.js", "/pkg-bundles/0123456789abcdef.js", "/pkg-bundles/0123456789ABCDEF.js", "/pkg-bundles/../home.toml", "/pkg-bundles/" + hash, "/pkg-bundles/" + hash + ".js/x"} {
		if w := get(e.h, bad); w.Code != 404 {
			t.Fatalf("%s → %d，要 404", bad, w.Code)
		}
	}
	// 沒這個頁面：白話說找不到。
	if _, errText := bundleOf(e, "nope"); !strings.Contains(errText, "找不到這個頁面") {
		t.Fatalf("沒有的頁面：%q", errText)
	}
}

// Guards: REQ-114
func TestPackageBareImportRejected(t *testing.T) {
	e := newEnv(t, "")
	c := withCatalog(t, e, demoPackage("demo", sdk.TierMine))
	_, badErr := bundleOf(e, "bad")
	if !strings.HasPrefix(badErr, "頁面「壞掉的頁面」編不過：") || !strings.Contains(badErr, "套件頁面只能 import 套件裡的檔案，以及 react、@evergenie/ui、@evergenie/sdk-web、@evergenie/host") || !strings.Contains(badErr, "pages/bad.tsx:1") {
		t.Fatalf("demo.bad = %q", badErr)
	}
	// 壞掉的頁面不拖累其他頁面。
	if hash, errText := bundleOf(e, "home"); hash == "" || errText != "" {
		t.Fatalf("demo.home 要照常：%q %q", hash, errText)
	}
	// 讀套件資料夾外面的檔案：同樣編不過。
	if err := os.WriteFile(filepath.Join(filepath.Dir(c.dirs["demo"]), "secret.ts"), []byte("export const secret = 'x';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.pkgs[0].Pages = append(c.pkgs[0].Pages, sdk.PageDecl{ID: "outside", Title: "越界", Component: "pages/outside.tsx"})
	if hash, errText := bundleOf(e, "outside"); hash != "" || !strings.Contains(errText, "套件頁面不能讀套件資料夾外面的檔案") {
		t.Fatalf("越界：%q %q", hash, errText)
	}
	// 符號連結指到資料夾外面也一樣。
	link := filepath.Join(c.dirs["demo"], "lib", "link.ts")
	if err := os.Symlink(filepath.Join(filepath.Dir(c.dirs["demo"]), "secret.ts"), link); err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(c.dirs["demo"], "pages", "card.tsx"), "import { secret } from \"../lib/link\";\nexport default function Card() { return <p>{secret}</p>; }\n", time.Now().Add(time.Minute))
	if hash, errText := bundleOf(e, "card"); hash != "" || !strings.Contains(errText, "套件頁面不能讀套件資料夾外面的檔案") {
		t.Fatalf("符號連結越界：%q %q", hash, errText)
	}
	// 找不到的相對 import：白話說找不到檔案。
	touch(t, filepath.Join(c.dirs["demo"], "pages", "card.tsx"), "import { x } from \"./nope\";\nexport default function Card() { return <p>{x}</p>; }\n", time.Now().Add(2*time.Minute))
	if _, errText := bundleOf(e, "card"); !strings.Contains(errText, "找不到「./nope」這個檔案") {
		t.Fatalf("找不到檔案：%q", errText)
	}
}

// Guards: REQ-114
func TestBundleCacheUntilEdit(t *testing.T) {
	e := newEnv(t, "")
	c := withCatalog(t, e, demoPackage("demo", sdk.TierMine))
	first := map[string][2]string{}
	for _, page := range []string{"home", "card", "bad"} {
		hash, errText := bundleOf(e, page)
		first[page] = [2]string{hash, errText}
	}
	built := e.as.Builds()
	if built != 3 { // home、card、bad 各編一次（壞的也編過了）
		t.Fatalf("第一次編了 %d 次，要 3 次", built)
	}
	// 沒改檔：不重編，hash 不變，連壞頁面的錯誤也是上次的。
	for _, page := range []string{"home", "card", "bad"} {
		hash, errText := bundleOf(e, page)
		if [2]string{hash, errText} != first[page] {
			t.Fatalf("%s 沒改檔結果卻變了：%q %q", page, hash, errText)
		}
	}
	if e.as.Builds() != built {
		t.Fatalf("沒改檔卻重編：builds %d → %d", built, e.as.Builds())
	}
	// 改了檔：重編，內容不同所以 hash 變。
	touch(t, filepath.Join(c.dirs["demo"], "lib", "util.ts"), "export const label = (n: number): string => `changed ${n}`;\n", time.Now().Add(time.Minute))
	third, errText := bundleOf(e, "home")
	if e.as.Builds() == built || third == first["home"][0] || errText != "" {
		t.Fatalf("改了檔要重編：builds %d → %d，%q → %q", built, e.as.Builds(), first["home"][0], third)
	}
	if w := get(e.h, "/pkg-bundles/"+third+".js"); w.Code != 200 || !strings.Contains(w.Body.String(), "changed ") {
		t.Fatalf("新的 bundle：%d", w.Code)
	}
	// 編譯結果的檔案被清掉：下一次要求時補回來。
	if err := os.Remove(filepath.Join(e.home, "runtime", "web", "bundles", third+".js")); err != nil {
		t.Fatal(err)
	}
	if w := get(e.h, "/pkg-bundles/"+third+".js"); w.Code != 404 {
		t.Fatalf("檔案被清掉後 → %d", w.Code)
	}
	if again, _ := bundleOf(e, "home"); again != third {
		t.Fatal("補編後 hash 應該一樣（內容一樣）")
	}
	if w := get(e.h, "/pkg-bundles/"+third+".js"); w.Code != 200 {
		t.Fatalf("補編後 → %d", w.Code)
	}
}
