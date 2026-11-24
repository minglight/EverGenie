package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/engine/registry"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/host"
)

// 安裝程式 scripts/install.sh --from-source 的整套情境（scripts/install-source-test.sh；`make install-test` 也跑）：暫存 bare origin（file://）＋假的 Makefile／evergenie／go、假 HOME，
// 不編譯、不碰真的服務：預設裝最大的正式版、--ref、沒有正式版裝 main、找不到版本、編譯／init／服務失敗不留家、家已經有東西不動、沒有 Go 白話說明，各自的結束碼。
// 下載版（預設的裝法）要真的編一包，太慢、又要先編 tmux，不在 make ci：make install-test（scripts/install-test.sh）。兩支腳本的語法這裡都檢查。
// Guards: REQ-279
func TestInstallScript(t *testing.T) {
	for _, tool := range []string{"git", "python3", "make"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("沒有 " + tool)
		}
	}
	for _, name := range []string{"install.sh", "install-test.sh", "install-source-test.sh"} {
		script, _ := filepath.Abs(filepath.Join("..", "..", "scripts", name))
		if out, err := exec.Command("bash", "-n", script).CombinedOutput(); err != nil {
			t.Fatalf("%s 語法不對：%v\n%s", name, err, out)
		}
	}
	script, _ := filepath.Abs("../../scripts/install-source-test.sh")
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "失敗 0") {
		t.Fatalf("install-source-test.sh 沒有全過：%v\n%s", err, out)
	}
}

// 安裝程式「名稱與網址」那一段跟程式對得上（DEV-155 S3）：服務名稱＝host.DefaultLabel（裝之前靠它判斷這台已經有一個家）；
// DEFAULT_SOURCE 剛好一行、格式固定（scripts/dist.sh 沒給 --source 時讀它編進程式）。
func TestInstallScriptNames(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	product := regexp.MustCompile(`(?m)^PRODUCT="([a-z0-9-]+)"$`).FindStringSubmatch(s)
	label := regexp.MustCompile(`(?m)^SERVICE_LABEL="([^"]+)"`).FindStringSubmatch(s)
	if product == nil || label == nil {
		t.Fatalf("install.sh 少了 PRODUCT 或 SERVICE_LABEL")
	}
	if got := strings.ReplaceAll(label[1], "$PRODUCT", product[1]); got != host.DefaultLabel {
		t.Fatalf("install.sh 的服務名稱 %q 跟 host.DefaultLabel %q 不一樣", got, host.DefaultLabel)
	}
	if n := len(regexp.MustCompile(`(?m)^DEFAULT_SOURCE=`).FindAllString(s, -1)); n != 1 {
		t.Fatalf("DEFAULT_SOURCE 要剛好一行，有 %d 行", n)
	}
	src := regexp.MustCompile(`(?m)^DEFAULT_SOURCE="([^"]+)"$`).FindStringSubmatch(s)
	if src == nil {
		t.Fatalf(`DEFAULT_SOURCE 要寫成 DEFAULT_SOURCE="<網址>"（scripts/dist.sh 照這個格式讀）`)
	}
	// 預設的下載點要合下載來源的規則（PLT-25），不然 dist.sh 編不進去、install.sh 沒給 --source 就裝不了
	if !platform.DownloadURLAllowed(src[1]) {
		t.Fatalf("DEFAULT_SOURCE %q 不合下載來源的規則（platform.DownloadURLPattern）", src[1])
	}
	if !regexp.MustCompile(`(?m)^DEFAULT_REPO="https://[^"]+\.git"$`).MatchString(s) {
		t.Fatalf(`DEFAULT_REPO 要寫成 DEFAULT_REPO="https://….git"（公開 repo，不用 SSH 別名）`)
	}
}

// 產品名只有一個來源（DEV-155 審查，改名 DEV-158 用；platform 卡 PLT-26）：Go 的 platform.ProductName（＝sdk.ProductName）跟
// scripts/install.sh 的 PRODUCT 一樣；從它推出來的每一處都對得上——核心執行檔 bin/<名> 與 cmd/<名>、Makefile 與 dist.sh 編的執行檔都叫
// <名> 或 <名>-<套件>（兩邊同一份清單）、引擎執行檔（registry）在清單裡、指標檔 <名>.local.toml（install.sh 寫的也是）、
// 預設的狀態資料夾 ~/.<名>/state（install.sh 的 APP_ROOT 也是 ~/.<名>）、服務名稱（TestInstallScriptNames）、INSTALL.md 表上的產品名。
// 改名的人照 install.sh「名稱與網址」那段的清單改完，這個測試綠了就是對的。
func TestProductNameSameEverywhere(t *testing.T) {
	read := func(p string) string {
		data, err := os.ReadFile(filepath.Join("..", "..", p))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	name := platform.ProductName
	install, makefile, dist, doc := read("scripts/install.sh"), read("Makefile"), read("scripts/dist.sh"), read("INSTALL.md")
	if m := regexp.MustCompile(`(?m)^PRODUCT="([^"]*)"$`).FindStringSubmatch(install); m == nil || m[1] != name {
		t.Fatalf("install.sh 的 PRODUCT %v 跟 platform.ProductName %q 不一樣", m, name)
	}
	if !regexp.MustCompile(`(?m)^APP_ROOT="\$HOME/\.\$PRODUCT"`).MatchString(install) {
		t.Fatal(`install.sh 的 APP_ROOT 要是 "$HOME/.$PRODUCT"（跟程式的預設狀態資料夾 ~/.<名>/state 同一個上層）`)
	}
	if !strings.Contains(install, `"$DIR/$PRODUCT.local.toml"`) || platform.PointerFile != name+".local.toml" {
		t.Fatalf("指標檔名對不上：程式是 %q，install.sh 要寫 $DIR/$PRODUCT.local.toml", platform.PointerFile)
	}
	if want := filepath.Join(platform.ExpandHome("~/."+name+"/state"), "家"); platform.DefaultStateDir("/x/家") != want {
		t.Fatalf("預設的狀態資料夾 %q，要是 %q", platform.DefaultStateDir("/x/家"), want)
	}
	if wd, _ := os.Getwd(); filepath.Base(wd) != name {
		t.Fatalf("核心的指令資料夾要叫 cmd/%s（現在 %s）", name, wd)
	}
	if !regexp.MustCompile("(?m)^\\| 產品名、指令名 \\| `" + regexp.QuoteMeta(name) + "` \\|$").MatchString(doc) {
		t.Fatalf("INSTALL.md「名稱與網址」表上的產品名要是 %s", name)
	}
	ok := func(bin string) bool { return bin == name || strings.HasPrefix(bin, name+"-") }
	// Makefile build 那段編的執行檔（bin/<檔名>）
	recipe := makefile[strings.Index(makefile, "\nbuild:"):]
	recipe = recipe[:strings.Index(recipe[1:], "\n\n")+1]
	var made []string
	for _, m := range regexp.MustCompile(`go-build-bin\.sh \S+ bin/(\S+) `).FindAllStringSubmatch(recipe, -1) {
		made = append(made, m[1])
	}
	var shipped []string
	for _, m := range regexp.MustCompile(`(?m)^  "[^|"]+\|([^|"]+)\|[^"]+"$`).FindAllStringSubmatch(dist, -1) {
		shipped = append(shipped, m[1])
	}
	if len(made) == 0 || strings.Join(made, " ") != strings.Join(shipped, " ") || made[0] != name {
		t.Fatalf("Makefile build 編的 %v 跟 dist.sh 的 BINS %v 要同一份、第一支是 %s", made, shipped, name)
	}
	for _, b := range made {
		if !ok(b) {
			t.Fatalf("執行檔 %q 要叫 %s 或 %s-<套件>", b, name, name)
		}
	}
	for id, exe := range registry.Executables() {
		if !ok(exe) || !strings.Contains(makefile, "bin/"+exe+" ") {
			t.Fatalf("引擎 %s 的執行檔 %q 要叫 %s-<套件>，而且 Makefile 有編它", id, exe, name)
		}
	}
}

// 程式碼裡不另寫產品名（DEV-155 審查；PLT-26）：Go 的非測試檔裡，整個字串就是由產品名組成的檔名、路徑或名稱
// （"<名>"、"<名>-<套件>"、"~/.<名>/…"、"<名>.local.toml"、"com.<名>.serve"…）一律改用 platform.ProductName／CoreExe（套件用 sdk.ProductName）。
// 例外只有改了舊東西會讀不到的格式識別字，列在下面並寫原因；指令用法與訊息裡的字照 install.sh 改名清單的第 4 項另外換。
func TestNoHardCodedProductName(t *testing.T) {
	name := regexp.QuoteMeta(platform.ProductName)
	lit := regexp.MustCompile(`"(~/\.|\.|com\.)?` + name + `(-[a-z-]+|\.local\.toml|\.serve|/[^"\s]*)?"`)
	allowed := map[string]string{ // 檔案 → 那一行要含的字：原因
		"sdk/product.go": "const ProductName", // 唯一的來源
		"internal/base/app/board/persona_transfer.go":    "X-Requested-With", // 看板請求的格式識別字（網頁那邊寫死同一個字）
		"internal/base/app/board/upload.go":              "X-Requested-With",
		"internal/base/app/board/home_backup.go":         "X-Requested-With",
		"packages/official/admin/internal/run/config.go": "defaultReleaseState", // 發版工具在開發機上的位置，跟產品改名無關
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata", "dist", "bin", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") || !lit.MatchString(line) {
				continue
			}
			if want, ok := allowed[rel]; ok && strings.Contains(line, want) {
				continue
			}
			t.Errorf("%s:%d 寫死了產品名，改用 platform.ProductName／platform.CoreExe（套件用 sdk.ProductName）：%s", rel, i+1, strings.TrimSpace(line))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// 結束碼只有一套：install.sh 真的會用的（die N，加上 0 成功、1 被中斷、2 用法不對）＝ install.sh 開頭的表 ＝ INSTALL.md 的表。
func TestInstallScriptExitCodes(t *testing.T) {
	read := func(p string) string {
		data, err := os.ReadFile(filepath.Join("..", "..", p))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	script, doc := read("scripts/install.sh"), read("INSTALL.md")
	set := func(codes ...string) string {
		seen := map[int]bool{}
		for _, c := range codes {
			n, err := strconv.Atoi(c)
			if err != nil {
				t.Fatalf("結束碼 %q 不是數字", c)
			}
			seen[n] = true
		}
		var out []int
		for n := range seen {
			out = append(out, n)
		}
		sort.Ints(out)
		return fmt.Sprint(out)
	}
	used := []string{"0", "1", "2"}
	for _, m := range regexp.MustCompile(`\bdie ([0-9]+) `).FindAllStringSubmatch(script, -1) {
		used = append(used, m[1])
	}
	var header []string
	if i := strings.Index(script, "\n# 結束碼"); i >= 0 {
		for _, line := range strings.Split(script[i+1:], "\n")[1:] {
			if m := regexp.MustCompile(`^#\s+([0-9]+)\s`).FindStringSubmatch(line); m != nil {
				header = append(header, m[1])
			} else if !strings.HasPrefix(line, "#   ") { // 「#   …」是上一行的接續（說明或某個結束碼寫成兩行）；空的「#」是表的結尾
				break
			}
		}
	}
	var table []string
	for _, m := range regexp.MustCompile(`(?m)^\| ([0-9]+(?:、[0-9]+)*) \|`).FindAllStringSubmatch(doc, -1) {
		table = append(table, strings.Split(m[1], "、")...)
	}
	u, h, d := set(used...), set(header...), set(table...)
	if u != h || h != d {
		t.Fatalf("結束碼對不上：install.sh 用的 %s、install.sh 開頭的表 %s、INSTALL.md 的表 %s", u, h, d)
	}
}

// 下載來源的規則（PLT-25）在程式（platform.DownloadURLPattern：家的 [update] source、升級、角色包）、scripts/install.sh 的 --source
// 與清單裡的包、scripts/dist.sh 的 --source 是同一條：兩支腳本的 DOWNLOAD_URL_RE 跟程式一字不差，同一批網址 bash 與 Go 判得一樣
// （https 要有主機；http 只收本機；file 要絕對路徑；不收空白、帳號密碼）。
func TestDownloadSourceRuleSameEverywhere(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("沒有 bash")
	}
	line := regexp.MustCompile(`(?m)^DOWNLOAD_URL_RE='([^']*)'$`)
	var defs []string
	for _, name := range []string{"install.sh", "dist.sh"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		m := line.FindAllStringSubmatch(string(data), -1)
		if len(m) != 1 || m[0][1] != platform.DownloadURLPattern {
			t.Fatalf("%s 的 DOWNLOAD_URL_RE 要剛好一行、跟 platform.DownloadURLPattern 一字不差：%q", name, m)
		}
		fn := regexp.MustCompile(`(?ms)^valid_source\(\) \{\n  local LC_ALL=C\n  \[\[ "\$1" =~ \$DOWNLOAD_URL_RE \]\]\n\}$`)
		if !fn.MatchString(string(data)) {
			t.Fatalf("%s 的 valid_source 要直接用 DOWNLOAD_URL_RE", name)
		}
		defs = append(defs, m[0][0])
	}
	urls := []string{
		"https://example.com/m.json", "https://example.com", "https://example.com:8443/a/b.json?x=1&y=2",
		"http://127.0.0.1:8000/manifest.json", "http://localhost/m.json", "http://[::1]:9/m.json", "file:///tmp/r/manifest.json",
		"file:///Users/小明/測試/manifest.json",
		"http://example.com/m.json", "http://192.0.2.5/m.json", "http://127.0.0.2/m.json", "http://localhost.example.com/m.json",
		"http://localhost@example.com/m.json", "https://user:pw@example.com/m.json", "https://", "HTTPS://example.com/m.json",
		"ftp://example.com/m.json", "file://host/m.json", "file:///", "/tmp/m.json", "https://例子.com/m.json",
		"https://example.com/有 空白.json", "https://example.com/a\tb", "",
		// 公開的下載點（GitHub Releases）：清單、包，與 GitHub 轉過去的下載網址（帶簽名的查詢字串）
		"https://github.com/minglight/EverGenie/releases/latest/download/manifest.json",
		"https://github.com/minglight/EverGenie/releases/download/v0.3.0/evergenie-0.3.0-darwin-arm64.tar.gz",
		"https://release-assets.githubusercontent.com/github-production-release-asset/1/ab-c?sp=r&se=2026-11-24T10%3A00%3A00Z&rscd=attachment%3B+filename%3Dmanifest.json&sig=x%2By%3D",
	}
	script := defs[0] + "\nvalid_source() {\n  local LC_ALL=C\n  [[ \"$1\" =~ $DOWNLOAD_URL_RE ]]\n}\n" +
		"while IFS= read -r u; do if valid_source \"$u\"; then echo 1; else echo 0; fi; done\n"
	// install.sh 在主人的 Mac 上是 macOS 內建的 /bin/bash（3.2）跑的：比對要用同一支，不是開發機 PATH 上的新版 bash
	bash := "bash"
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat("/bin/bash"); err == nil {
			bash = "/bin/bash"
		}
	}
	cmd := exec.Command(bash, "-c", script)
	cmd.Stdin = strings.NewReader(strings.Join(urls, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(out))
	if len(got) != len(urls) {
		t.Fatalf("bash 的結果數不對：%q", got)
	}
	for i, u := range urls {
		if (got[i] == "1") != platform.DownloadURLAllowed(u) {
			t.Errorf("%q：bash %s、Go %v", u, got[i], platform.DownloadURLAllowed(u))
		}
	}
}
