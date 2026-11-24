package pkg

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 角色改代號時紀錄怎麼跟著走的宣告（PKG-64）：persona_field 可以單獨寫；persona_id 只給 given、只能 exact／prefix；
// persona_prefixed、persona_source 要是規格裡型別對的欄位；不合格讓套件 rejected、field 是 contributes.data。
func TestDataPersonaDeclValidated(t *testing.T) {
	h := newHarness(t)
	const schema = "schema = 1\ntitle = \"看法\"\n[[field]]\nname = \"persona\"\ntype = \"string\"\n[[field]]\nname = \"record\"\ntype = \"string\"\n" +
		"[[field]]\nname = \"source\"\ntype = \"enum\"\nenum = [\"official\", \"mine\"]\n[[field]]\nname = \"kindof\"\ntype = \"enum\"\nenum = [\"a\", \"b\"]\n[[field]]\nname = \"n\"\ntype = \"int\"\n"
	kind := func(name, ids, extra string) string {
		head := "\n[[contributes.data]]\nkind = \"" + name + "\"\nschema = \"schemas/look.toml\"\n"
		if ids == "seq" {
			return head + "ids = \"seq\"\nprefix = \"L-\"\n" + extra
		}
		return head + "ids = \"given\"\n" + extra
	}
	good := h.write(sdk.TierMine, "look", dataPkg("look", "1.0.0",
		kind("alone", "seq", "persona_field = \"persona\"\npersona_prefixed = [\"record\"]\n")+
			kind("byid", "given", "persona_id = \"prefix\"\npersona_source = \"source\"\n")+
			kind("both", "given", "export = \"by_persona\"\npersona_id = \"exact\"\n")+
			kind("plain", "given", "")),
		map[string]string{"schemas/look.toml": schema})
	m, err := ParseManifest(good, sdk.TierMine)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]sdk.DataKindDecl{}
	for _, d := range m.Public(sdk.TierMine).Data {
		d.Fields, d.Title = nil, ""
		got[d.Kind] = d
	}
	want := map[string]sdk.DataKindDecl{
		"alone": {Kind: "alone", IDs: "seq", Prefix: "L-", PersonaField: "persona", PersonaPrefixed: []string{"record"}},
		"byid":  {Kind: "byid", IDs: "given", PersonaID: "prefix", PersonaSource: "source", PersonaPrefixed: []string{}},
		"both":  {Kind: "both", IDs: "given", Export: "by_persona", PersonaField: "persona", PersonaID: "exact", PersonaPrefixed: []string{}},
		"plain": {Kind: "plain", IDs: "given", PersonaPrefixed: []string{}},
	}
	for k, w := range want {
		g := got[k]
		if len(g.PersonaPrefixed) == 0 {
			g.PersonaPrefixed = []string{}
		}
		g.ExportOmit = nil
		if !reflect.DeepEqual(g, w) {
			t.Errorf("%s：\n got %+v\nwant %+v", k, g, w)
		}
	}

	cases := []struct{ name, decl, want string }{
		{"unknown persona_id", kind("x", "given", "persona_id = \"whole\"\n"), "persona_id 要是"},
		{"persona_id on seq", kind("x", "seq", "persona_id = \"exact\"\n"), "只給 ids"},
		{"persona_field not in schema", kind("x", "given", "persona_field = \"nobody\"\n"), "nobody"},
		{"persona_field not string", kind("x", "given", "persona_field = \"n\"\n"), "string"},
		{"prefixed not in schema", kind("x", "given", "persona_prefixed = [\"nobody\"]\n"), "nobody"},
		{"prefixed not string", kind("x", "given", "persona_prefixed = [\"n\"]\n"), "string"},
		{"prefixed duplicate", kind("x", "given", "persona_prefixed = [\"record\", \"record\"]\n"), "重複"},
		{"prefixed is persona_field", kind("x", "given", "persona_field = \"persona\"\npersona_prefixed = [\"persona\"]\n"), "不能同時"},
		{"source alone", kind("x", "given", "persona_source = \"source\"\n"), "要配"},
		{"source wrong enum", kind("x", "given", "persona_id = \"exact\"\npersona_source = \"kindof\"\n"), "official 與 mine"},
		{"source not in schema", kind("x", "given", "persona_id = \"exact\"\npersona_source = \"nobody\"\n"), "nobody"},
		{"reserved prefixed", kind("x", "given", "persona_prefixed = [\"id\"]\n"), "名稱不對"},
	}
	for _, c := range cases {
		dir := h.write(sdk.TierMine, "bad", dataPkg("bad", "1.0.0", c.decl), map[string]string{"schemas/look.toml": schema})
		_, err := ParseManifest(dir, sdk.TierMine)
		var pe *platform.Err
		if err == nil || !asPlatformErr(err, &pe) || pe.Field != "contributes.data" || !strings.Contains(pe.Message, c.want) {
			t.Errorf("%s：要 contributes.data 加「%s」，實際 %v", c.name, c.want, err)
		}
	}
}

// 官方衣櫃與上傳衣服的宣告（PKG-64）：改角色代號時衣服目錄、兌換、輪播、檢視、意見、兌換請求、上傳請求都跟著走；
// 衣服目錄的 source 是 persona_source。這是 home tidy、personas promote 唯一知道衣櫃紀錄長相的地方。
func TestClosetDeclaresPersonaKeys(t *testing.T) {
	want := map[string]string{
		"closet.outfit":       "persona|prefix|from|source",
		"closet.unlock":       "persona|prefix||",
		"closet.rotation":     "|exact||",
		"closet.request":      "persona||unlock|",
		"closet.person":       "persona|exact||",
		"closet.note":         "persona||record,photo|",
		"closet.photo":        "persona|prefix|outfit,of|source",
		"closet.state":        "||| ",
		"closet.variant":      "||| ",
		"closet-mine.request": "persona||result,photo_id|", // photo_id：照片紀錄的編號（DEV-160 S3）
	}
	got := map[string]string{}
	for _, id := range []string{"closet", "closet-mine"} {
		root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "packages", "official", id))
		if err != nil {
			t.Fatal(err)
		}
		m, err := ParseManifest(root, sdk.TierOfficial)
		if err != nil {
			t.Fatalf("%s：%v", id, err)
		}
		for _, d := range m.Public(sdk.TierOfficial).Data {
			v := d.PersonaField + "|" + d.PersonaID + "|" + strings.Join(d.PersonaPrefixed, ",") + "|" + d.PersonaSource
			if d.PersonaField+d.PersonaID+d.PersonaSource == "" && len(d.PersonaPrefixed) == 0 {
				v = "||| "
			}
			got[id+"."+d.Kind] = v
		}
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s：%q，要 %q", k, got[k], w)
		}
	}
}
