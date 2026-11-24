package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 衣服套件的替身：只有資料（不用建衣櫃的程式），schema 直接用官方衣櫃的 outfit／variant，宣告照官方衣櫃。
// 名字故意不叫 closet：核心只看宣告，不認得任何套件（D-63）。
const dressManifest = `schema = 1
id = "dress"
name = "衣服"
version = "1.0.0"
description = "匯出測試用"
kind = "data"
api = "evergenie.v1"
[[contributes.data]]
kind = "outfit"
schema = "schemas/outfit.toml"
ids = "given"
export = "by_persona"
persona_id = "prefix"
pack_only = { source = ["official"], state = ["active", "retired"] }
pack_only_note = "還沒升成官方或還在設計中的衣服"
[[contributes.data]]
kind = "variant"
schema = "schemas/variant.toml"
ids = "given"
export = "by_folder"
folder_field = "dir"
`

func dressPackage(t *testing.T, home string) {
	t.Helper()
	files := map[string]string{}
	for _, k := range []string{"outfit", "variant"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "packages", "official", "closet", "schemas", k+".toml"))
		if err != nil {
			t.Fatal(err)
		}
		files["schemas/"+k+".toml"] = string(b)
	}
	writePackage(t, home, "dress", dressManifest, files)
}

func putFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 經真的核心（vault、套件宣告）在兩個家之間匯出、匯入：helper 的卡、圖、衣服紀錄、有她圖的隱藏版到 B，
// 紀錄經 vault 寫入（有檔頭）；builder 的圖不跟；沒裝衣服套件的家，紀錄略過並列出；--dry-run 不寫（PRS-8～10）。
func TestPersonasExportImportBetweenHomes(t *testing.T) {
	homeA, sockA := startFixtureAPIWith(t, func(dir string) {
		dressPackage(t, dir)
		for _, f := range []string{"avatars/helper.png", "avatars/helper@sea.png", "avatars/full/helper.png", "avatars/builder.png", "avatars/hidden/helper@sea.png", "avatars/hidden/builder.png"} {
			putFile(t, filepath.Join(dir, "personas", filepath.FromSlash(f)), "圖 "+f)
		}
	})
	hgOK(t, sockA, "data", "add", "dress.outfit", "--id", "helper-sea", "--json", `{"persona":"helper","outfit":"sea","name":"海邊","cost":1,"state":"active","source":"official"}`)
	// 主人自己的、還在設計中的衣服：套件的 pack_only 只管官方角色包（PKG-68），主人點名的匯出照舊帶。
	hgOK(t, sockA, "data", "add", "dress.outfit", "--id", "helper-my-7", "--json", `{"persona":"helper","outfit":"my-7","name":"私服","cost":0,"state":"draft","source":"mine"}`)
	hgOK(t, sockA, "data", "add", "dress.outfit", "--id", "builder-base", "--json", `{"persona":"builder","outfit":"base","name":"基本","cost":0,"state":"active","source":"official"}`)
	hgOK(t, sockA, "data", "add", "dress.variant", "--id", "hidden", "--json", `{"name":"隱藏版","dir":"avatars/hidden","button":true}`)

	out := filepath.Join(t.TempDir(), "helper")
	exp := hgOK(t, sockA, "personas", "export", "--persona", "helper", "--out", out, "--home", homeA)
	if !strings.Contains(exp, "匯出了 helper：5 個檔案、3 筆紀錄（dress.outfit、dress.variant）") || !strings.Contains(exp, out+".hgpersona") {
		t.Fatalf("export：%s", exp)
	}
	file := out + ".hgpersona"

	homeB, sockB := startFixtureAPIWith(t, func(dir string) { dressPackage(t, dir) })
	_ = os.Remove(filepath.Join(homeB, "personas", "helper.md"))
	dry := hgOK(t, sockB, "personas", "import", file, "--dry-run", "--home", homeB)
	if !strings.Contains(dry, "預覽") || fileExists(filepath.Join(homeB, "personas", "helper.md")) {
		t.Fatalf("dry-run 不能寫：%s", dry)
	}
	imp := hgOK(t, sockB, "personas", "import", file, "--home", homeB)
	for _, want := range []string{"新增 5 個檔", "紀錄新增 3 筆", "新增：personas/avatars/hidden/helper@sea.png", "新增紀錄：dress.outfit/helper-sea", "新增紀錄：dress.outfit/helper-my-7"} {
		if !strings.Contains(imp, want) {
			t.Errorf("import 少了「%s」：\n%s", want, imp)
		}
	}
	for _, f := range []string{"avatars/builder.png", "avatars/hidden/builder.png"} {
		if fileExists(filepath.Join(homeB, "personas", filepath.FromSlash(f))) {
			t.Errorf("別的角色的圖不跟：%s", f)
		}
	}
	rec := jsonLine(t, hgOK(t, sockB, "data", "get", "dress.outfit", "helper-sea", "--json"))
	if f, _ := rec["fields"].(map[string]any); f["name"] != "海邊" || f["created_at"] == nil {
		t.Fatalf("紀錄要經 vault 寫入（有檔頭）：%v", rec)
	}
	if code, _, _ := hg(t, sockB, "", "data", "get", "dress.outfit", "builder-base"); code == 0 {
		t.Error("別的角色的衣服不跟")
	}

	// B 改了一張圖：再匯入不蓋；--force 蓋並備份。
	putFile(t, filepath.Join(homeB, "personas", "avatars", "helper.png"), "B 換的")
	again := hgOK(t, sockB, "personas", "import", file, "--home", homeB)
	if !strings.Contains(again, "你改過的，沒換：personas/avatars/helper.png") {
		t.Fatalf("改過的不蓋：%s", again)
	}
	forced := hgOK(t, sockB, "personas", "import", file, "--force", "--home", homeB)
	if !strings.Contains(forced, "蓋掉的東西備份在：runtime/backups/persona-import/") {
		t.Fatalf("--force 要備份：%s", forced)
	}

	// 沒裝衣服套件的家：檔案照樣進來，紀錄略過並列出。
	homeC, sockC := startFixtureAPIWith(t, nil)
	noPkg := hgOK(t, sockC, "personas", "import", file, "--home", homeC)
	if !strings.Contains(noPkg, "略過的紀錄：dress.outfit/helper-sea：dress 套件沒裝或停用") || !fileExists(filepath.Join(homeC, "personas", "avatars", "helper@sea.png")) {
		t.Fatalf("沒裝套件：%s", noPkg)
	}
}

// 指令的匯出、匯入接上跟著程式的公開卡（registry 給 agents.Commands 的 pkg.PersonasBundledCards，PRS-25）：
// 官方資料夾沒有、只有程式帶的卡也匯得出來；匯入到同一版程式的家，跟程式帶的一樣，不抄一份到官方位置。
// Guards: PRS-25
func TestPersonasExportImportBundledCard(t *testing.T) {
	manifest, err := os.ReadFile(filepath.Join("..", "..", "packages", "official", "personas", "package.toml"))
	if err != nil {
		t.Fatal(err)
	}
	official := t.TempDir()
	putFile(t, filepath.Join(official, "personas", "package.toml"), string(manifest))
	putFile(t, filepath.Join(official, "personas", "cards", "kaguya.md"), "+++\nname = \"輝夜\"\n+++\n程式帶的\n")
	t.Setenv("EVERGENIE_OFFICIAL_PACKAGES_DIR", official)

	homeA, sockA := startFixtureAPIWith(t, nil)
	out := filepath.Join(t.TempDir(), "kaguya")
	if exp := hgOK(t, sockA, "personas", "export", "--persona", "kaguya", "--out", out, "--home", homeA); !strings.Contains(exp, "匯出了 kaguya") {
		t.Fatalf("export：%s", exp)
	}
	homeB, sockB := startFixtureAPIWith(t, nil)
	imp := hgOK(t, sockB, "personas", "import", out+".hgpersona", "--home", homeB)
	if strings.Contains(imp, "personas/kaguya.md") || fileExists(filepath.Join(homeB, "personas", "kaguya.md")) {
		t.Fatalf("跟程式帶的一樣，不寫進官方位置：%s", imp)
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// doctor 接上「我的卡跟官方角色同代號」的離線檢查（registry 給 system.Commands 的 persona.SlugClashChecks(pkg.PersonasBundledCards)，PRS-27）：
// 沒撞名時沒有這條；我的卡跟這一版程式帶的公開卡同代號時多一條注意（serve 沒在跑也照樣檢查，文字輸出也有）。
func TestDoctorWarnsMineSlugSharingBundledCard(t *testing.T) {
	official := t.TempDir()
	putFile(t, filepath.Join(official, "personas", "cards", "venus.md"), "+++\nname = \"維納斯\"\n+++\n")
	t.Setenv("EVERGENIE_OFFICIAL_PACKAGES_DIR", official)
	dir := doctorHome(t)
	sock := filepath.Join(dir, "runtime", "sockets", "api.sock")
	clash := func() []sdk.DoctorCheck {
		_, checks := doctorJSON(t, sock, dir)
		var out []sdk.DoctorCheck
		for _, c := range checks {
			if c.ID == "personas.slug_clash" {
				out = append(out, c)
			}
		}
		return out
	}
	if got := clash(); len(got) != 0 {
		t.Fatalf("沒撞名不該有這條：%+v", got)
	}
	putFile(t, filepath.Join(dir, "personas", "mine", "venus.md"), "---\nname: 我的維納斯\n---\n")
	got := clash()
	if len(got) != 1 || got[0].Severity != sdk.DoctorSeverityWarn || !strings.Contains(got[0].Detail, "personas/mine/venus.md") || !strings.Contains(got[0].Fix, "my_venus") {
		t.Fatalf("跟程式帶的公開卡同代號要有一條注意：%+v", got)
	}
	if _, out, _ := hg(t, sock, "", "doctor", "--home", dir); !strings.Contains(out, "! 你自己的角色 venus") {
		t.Fatalf("文字輸出要有這條：\n%s", out)
	}
}
