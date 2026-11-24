package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const manifest = "schema = 1\nid = \"health\"\nname = \"健康\"\nversion = \"1.0.0\"\ndescription = \"測試\"\nkind = \"data\"\napi = \"evergenie.v1\"\n" +
	"[[contributes.data]]\nkind = \"body\"\nschema = \"schemas/body.toml\"\nids = \"seq\"\nprefix = \"B-\"\n"

const schemaV1 = "schema = 1\ntitle = \"身體\"\n[[field]]\nname = \"weight\"\ntype = \"number\"\n" +
	"[[field]]\nname = \"mood\"\ntype = \"enum\"\nenum = [\"good\", \"ok\", \"bad\"]\n[[field]]\nname = \"note\"\ntype = \"string\"\n"

func put(t *testing.T, root, extra, schema string) {
	t.Helper()
	dir := filepath.Join(root, "health", "schemas")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "health", "package.toml"), []byte(manifest+extra), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "body.toml"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 只加不必填的欄位：過。拿掉欄位、換型別、拿掉選項、變必填而沒附轉換：每一項都列出來；附了轉換或登記例外就過。
func TestSchemaCheck(t *testing.T) {
	old, cur := t.TempDir(), t.TempDir()
	put(t, old, "", schemaV1)
	run := func() (int, string) {
		var b bytes.Buffer
		code := check(old, cur, &b)
		return code, b.String()
	}
	put(t, cur, "", schemaV1+"[[field]]\nname = \"sleep\"\ntype = \"number\"\n")
	if code, out := run(); code != 0 || !strings.Contains(out, "1 個") {
		t.Fatalf("只加欄位要過：%d %s", code, out)
	}
	v2 := "schema = 1\ntitle = \"身體\"\n[[field]]\nname = \"weight_kg\"\ntype = \"number\"\n" +
		"[[field]]\nname = \"mood\"\ntype = \"enum\"\nenum = [\"good\", \"bad\"]\n[[field]]\nname = \"note\"\ntype = \"int\"\n[[field]]\nname = \"unit\"\ntype = \"string\"\nrequired = true\n"
	put(t, cur, "", v2)
	code, out := run()
	if code != 1 {
		t.Fatalf("沒附轉換要擋：%d %s", code, out)
	}
	for _, want := range []string{"health.body.weight 欄位不見了", "health.body.mood 的選項「ok」不見了", "health.body.note 型別從 string 換成 int", "health.body.unit 變成必填"} {
		if !strings.Contains(out, want) {
			t.Errorf("少了 %q：\n%s", want, out)
		}
	}
	mig := "[[migration]]\nid = \"v2\"\ndescription = \"第二版\"\nkind = \"body\"\nremove = [\"note\"]\n[migration.rename]\nweight = \"weight_kg\"\n" +
		"[migration.values.mood]\nok = \"good\"\n[migration.defaults]\nunit = \"kg\"\n"
	put(t, cur, mig, v2)
	if code, out := run(); code != 0 {
		t.Fatalf("附了轉換要過：%d %s", code, out)
	}
	// 沒附轉換但登記了例外（要寫原因）。
	put(t, cur, "", schemaV1[:strings.Index(schemaV1, "[[field]]\nname = \"note\"")])
	if code, out := run(); code != 1 || !strings.Contains(out, "health.body.note 欄位不見了") {
		t.Fatalf("拿掉 note 要擋：%d %s", code, out)
	}
	if err := os.WriteFile(filepath.Join(cur, "schema.removed"), []byte("# 例外\nhealth.body.note\t從來沒人用過\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := run(); code != 0 {
		t.Fatalf("登記例外要過：%d %s", code, out)
	}
	if err := os.WriteFile(filepath.Join(cur, "schema.removed"), []byte("health.body.note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := run(); code != 1 || !strings.Contains(out, "沒有寫原因") {
		t.Fatalf("例外沒寫原因要擋：%d %s", code, out)
	}
	// 整個套件不見。
	if err := os.RemoveAll(filepath.Join(cur, "health")); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(cur, "schema.removed"))
	if code, out := run(); code != 1 || !strings.Contains(out, "health.body 整個套件不見了") {
		t.Fatalf("套件不見要擋：%d %s", code, out)
	}
}

// 審查補的洞（DEV-154）：編號方式變了、型別換了卻只附 values、上一版的轉換被拿掉、上一版 manifest 讀不起來，都要列出來。
func TestSchemaCheckHoles(t *testing.T) {
	old, cur := t.TempDir(), t.TempDir()
	oldMig := "[[migration]]\nid = \"v1\"\ndescription = \"第一版\"\nkind = \"body\"\nremove = [\"legacy\"]\n"
	put(t, old, oldMig, schemaV1)
	run := func() (int, string) {
		var b bytes.Buffer
		code := check(old, cur, &b)
		return code, b.String()
	}
	put(t, cur, oldMig, schemaV1)
	if code, out := run(); code != 0 {
		t.Fatalf("沒改要過：%d %s", code, out)
	}
	// 上一版的轉換被拿掉。
	put(t, cur, "", schemaV1)
	if code, out := run(); code != 1 || !strings.Contains(out, "上一版的資料轉換「v1」被拿掉了") {
		t.Fatalf("轉換被拿掉要擋：%d %s", code, out)
	}
	// 型別換了，只附 values 不算處理到。
	typed := strings.Replace(schemaV1, "name = \"note\"\ntype = \"string\"", "name = \"note\"\ntype = \"int\"", 1)
	put(t, cur, oldMig+"[[migration]]\nid = \"v2\"\ndescription = \"第二版\"\nkind = \"body\"\n[migration.values.note]\na = \"1\"\n", typed)
	if code, out := run(); code != 1 || !strings.Contains(out, "health.body.note 型別從 string 換成 int") {
		t.Fatalf("換型別只附 values 要擋：%d %s", code, out)
	}
	// 編號方式變了。
	if err := os.WriteFile(filepath.Join(cur, "health", "package.toml"), []byte(strings.Replace(manifest, "prefix = \"B-\"", "prefix = \"BD-\"", 1)+oldMig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cur, "health", "schemas", "body.toml"), []byte(schemaV1), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := run(); code != 1 || !strings.Contains(out, "health.body 編號方式變了") {
		t.Fatalf("編號方式變了要擋：%d %s", code, out)
	}
	// 上一版的 manifest 讀不起來：不能當沒事。
	put(t, cur, oldMig, schemaV1)
	if err := os.WriteFile(filepath.Join(old, "health", "package.toml"), []byte("這不是 toml = ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := run(); code != 1 || !strings.Contains(out, "上一版的 package.toml 用現在的規則讀不起來") {
		t.Fatalf("上一版讀不起來要列出來：%d %s", code, out)
	}
}
