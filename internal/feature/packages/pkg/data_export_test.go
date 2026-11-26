package pkg

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 匯出角色時跟著走的宣告（PKG-57）：export 只能是 by_persona（persona_field 預設 persona）或 by_folder（folder_field 必填），
// 只給 given 的種類，欄位要在規格裡而且是 string；不合格讓套件 rejected、field 是 contributes.data；合格的帶在 Package.Data。
func TestDataExportDeclValidated(t *testing.T) {
	h := newHarness(t)
	const lookSchema = "schema = 1\ntitle = \"衣服\"\n[[field]]\nname = \"persona\"\ntype = \"string\"\n[[field]]\nname = \"who\"\ntype = \"string\"\n[[field]]\nname = \"dir\"\ntype = \"string\"\n[[field]]\nname = \"n\"\ntype = \"int\"\n"
	decl := func(extra string) string {
		return "\n[[contributes.data]]\nkind = \"look\"\nschema = \"schemas/look.toml\"\nids = \"given\"\n" + extra
	}
	good := h.write(sdk.TierMine, "wear", dataPkg("wear", "1.0.0", decl("export = \"by_persona\"\n")+
		"\n[[contributes.data]]\nkind = \"alt\"\nschema = \"schemas/look.toml\"\nids = \"given\"\nexport = \"by_folder\"\nfolder_field = \"dir\"\nexport_omit = [\"n\"]\n"+
		"\n[[contributes.data]]\nkind = \"own\"\nschema = \"schemas/look.toml\"\nids = \"given\"\nexport = \"by_persona\"\npersona_field = \"who\"\npack_only = { persona = [\"a\", \"b\"] }\npack_only_note = \"還沒公開的\"\npack_except = { dir = [\"secret\"] }\npack_except_note = \"主人標了不公開的\"\n"+
		"\n[[contributes.data]]\nkind = \"plain\"\nschema = \"schemas/look.toml\"\nids = \"given\"\n"),
		map[string]string{"schemas/look.toml": lookSchema})
	m, err := ParseManifest(good, sdk.TierMine)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]sdk.DataKindDecl{}
	for _, d := range m.Public(sdk.TierMine).Data {
		got[d.Kind] = d
	}
	if d := got["look"]; d.Export != "by_persona" || d.PersonaField != "persona" || d.FolderField != "" {
		t.Errorf("by_persona 預設欄位 persona：%+v", d)
	}
	if d := got["alt"]; d.Export != "by_folder" || d.FolderField != "dir" || d.PersonaField != "" {
		t.Errorf("by_folder：%+v", d)
	}
	if d := got["own"]; d.Export != "by_persona" || d.PersonaField != "who" {
		t.Errorf("persona_field：%+v", d)
	}
	if d := got["plain"]; d.Export != "" || d.PersonaField != "" || d.FolderField != "" {
		t.Errorf("沒宣告就不匯出：%+v", d)
	}
	if d := got["alt"]; len(d.ExportOmit) != 1 || d.ExportOmit[0] != "n" {
		t.Errorf("export_omit 帶在宣告裡：%+v", d)
	}
	// pack_only（PKG-68）帶在宣告裡；沒寫的種類是空的（官方角色包照舊每一筆都帶）。
	if d := got["own"]; len(d.PackOnly) != 1 || strings.Join(d.PackOnly["persona"], ",") != "a,b" || d.PackOnlyNote != "還沒公開的" {
		t.Errorf("pack_only 帶在宣告裡：%+v", d)
	}
	if d := got["look"]; d.PackOnly != nil || d.PackOnlyNote != "" {
		t.Errorf("沒寫 pack_only 就是空的：%+v", d)
	}
	// pack_except（PKG-71）也帶在宣告裡；沒寫的種類是空的。
	if d := got["own"]; len(d.PackExcept) != 1 || strings.Join(d.PackExcept["dir"], ",") != "secret" || d.PackExceptNote != "主人標了不公開的" {
		t.Errorf("pack_except 帶在宣告裡：%+v", d)
	}
	if d := got["look"]; d.PackExcept != nil || d.PackExceptNote != "" {
		t.Errorf("沒寫 pack_except 就是空的：%+v", d)
	}

	cases := []struct{ name, decl, want string }{
		{"unknown export", decl("export = \"always\"\n"), "export 要是"},
		{"by_folder without folder_field", decl("export = \"by_folder\"\n"), "folder_field"},
		{"by_persona with folder_field", decl("export = \"by_persona\"\nfolder_field = \"dir\"\n"), "folder_field"},
		{"by_folder with persona_field", decl("export = \"by_folder\"\nfolder_field = \"dir\"\npersona_field = \"who\"\n"), "persona_field"},
		{"folder_field without export", decl("folder_field = \"dir\"\n"), "只能跟 export 一起寫"},
		{"seq kind", "\n[[contributes.data]]\nkind = \"look\"\nschema = \"schemas/look.toml\"\nids = \"seq\"\nprefix = \"L-\"\nexport = \"by_persona\"\n", "given"},
		{"field not in schema", decl("export = \"by_persona\"\npersona_field = \"nobody\"\n"), "nobody"},
		{"field not string", decl("export = \"by_folder\"\nfolder_field = \"n\"\n"), "string"},
		{"reserved field", decl("export = \"by_persona\"\npersona_field = \"id\"\n"), "名稱不對"},
		// export_omit（PKG-58）：只能配 export、要是規格裡的欄位、不能是匯出用的欄位或必填、不能重複。
		{"omit without export", decl("export_omit = [\"n\"]\n"), "只能跟 export 一起寫"},
		{"omit export field", decl("export = \"by_persona\"\nexport_omit = [\"persona\"]\n"), "匯出用的欄位"},
		{"omit not in schema", decl("export = \"by_persona\"\nexport_omit = [\"nobody\"]\n"), "nobody"},
		{"omit required", decl("export = \"by_persona\"\nexport_omit = [\"must\"]\n"), "必填"},
		{"omit duplicate", decl("export = \"by_persona\"\nexport_omit = [\"n\", \"n\"]\n"), "重複"},
		{"omit reserved", decl("export = \"by_persona\"\nexport_omit = [\"by\"]\n"), "名稱不對"},
		// pack_only（PKG-68）：只能配 by_persona、欄位要在規格裡而且是 string 或 enum、不能在 export_omit 裡、值不能空或重複、enum 的值要是選項。
		{"pack_only without export", decl("pack_only = { who = [\"a\"] }\n"), "pack_only"},
		{"pack_only with by_folder", decl("export = \"by_folder\"\nfolder_field = \"dir\"\npack_only = { who = [\"a\"] }\n"), "by_persona"},
		{"pack_only not in schema", decl("export = \"by_persona\"\npack_only = { nobody = [\"a\"] }\n"), "nobody"},
		{"pack_only not string", decl("export = \"by_persona\"\npack_only = { n = [\"1\"] }\n"), "string"},
		{"pack_only omitted field", decl("export = \"by_persona\"\nexport_omit = [\"who\"]\npack_only = { who = [\"a\"] }\n"), "export_omit"},
		{"pack_only no values", decl("export = \"by_persona\"\npack_only = { who = [] }\n"), "至少要寫一個"},
		{"pack_only empty table", decl("export = \"by_persona\"\npack_only = {}\n"), "至少要寫一個欄位"},
		{"pack_only duplicate value", decl("export = \"by_persona\"\npack_only = { who = [\"a\", \"a\"] }\n"), "重複"},
		{"pack_only blank value", decl("export = \"by_persona\"\npack_only = { who = [\" \"] }\n"), "空白"},
		{"pack_only persona field", decl("export = \"by_persona\"\npack_only = { persona = [\"a\"] }\n"), "角色代號的欄位"},
		{"pack_only reserved field", decl("export = \"by_persona\"\npack_only = { by = [\"a\"] }\n"), "名稱不對"},
		{"pack_only value not in enum", decl("export = \"by_persona\"\npack_only = { tone = [\"loud\"] }\n"), "不是這個欄位的選項"},
		{"pack_only bool bad value", decl("export = \"by_persona\"\npack_only = { flag = [\"yes\"] }\n"), "true"},
		{"note without pack_only", decl("export = \"by_persona\"\npack_only_note = \"還沒公開的\"\n"), "pack_only_note 要配 pack_only"},
		// pack_except（PKG-71）：宣告規則同 pack_only。
		{"pack_except without export", decl("pack_except = { who = [\"a\"] }\n"), "pack_except"},
		{"pack_except with by_folder", decl("export = \"by_folder\"\nfolder_field = \"dir\"\npack_except = { who = [\"a\"] }\n"), "by_persona"},
		{"pack_except not in schema", decl("export = \"by_persona\"\npack_except = { nobody = [\"a\"] }\n"), "nobody"},
		{"pack_except not string", decl("export = \"by_persona\"\npack_except = { n = [\"1\"] }\n"), "string"},
		{"pack_except omitted field", decl("export = \"by_persona\"\nexport_omit = [\"who\"]\npack_except = { who = [\"a\"] }\n"), "export_omit"},
		{"pack_except no values", decl("export = \"by_persona\"\npack_except = { who = [] }\n"), "至少要寫一個"},
		{"pack_except empty table", decl("export = \"by_persona\"\npack_except = {}\n"), "至少要寫一個欄位"},
		{"pack_except duplicate value", decl("export = \"by_persona\"\npack_except = { flag = [\"true\", \"true\"] }\n"), "重複"},
		{"pack_except persona field", decl("export = \"by_persona\"\npack_except = { persona = [\"a\"] }\n"), "角色代號的欄位"},
		{"pack_except value not in enum", decl("export = \"by_persona\"\npack_except = { tone = [\"loud\"] }\n"), "不是這個欄位的選項"},
		{"pack_except bool bad value", decl("export = \"by_persona\"\npack_except = { flag = [\"yes\"] }\n"), "true"},
		{"except note without pack_except", decl("export = \"by_persona\"\npack_except_note = \"不公開的\"\n"), "pack_except_note 要配 pack_except"},
		{"except note multi-line", decl("export = \"by_persona\"\npack_except = { flag = [\"true\"] }\npack_except_note = \"一\\n二\"\n"), "一行"},
		{"note multi-line", decl("export = \"by_persona\"\npack_only = { who = [\"a\"] }\npack_only_note = \"一\\n二\"\n"), "一行"},
		// file_field、export_only、pack_parent（DEV-160）：只配 by_persona；欄位要在規格裡、是 string（export_only 同 pack_only 的規則）；
		// file_field 不能是角色代號的欄位或在 export_omit 裡；pack_parent 要指到同一個套件另一種 by_persona 的資料。
		{"file_field without export", decl("file_field = \"dir\"\n"), "只能跟 export = \"by_persona\" 一起寫"},
		{"file_field with by_folder", decl("export = \"by_folder\"\nfolder_field = \"dir\"\nfile_field = \"who\"\n"), "by_persona"},
		{"file_field not in schema", decl("export = \"by_persona\"\nfile_field = \"nobody\"\n"), "nobody"},
		{"file_field not string", decl("export = \"by_persona\"\nfile_field = \"n\"\n"), "type = \"string\""},
		{"file_field persona field", decl("export = \"by_persona\"\nfile_field = \"persona\"\n"), "角色代號"},
		{"file_field omitted", decl("export = \"by_persona\"\nexport_omit = [\"dir\"]\nfile_field = \"dir\"\n"), "export_omit"},
		{"export_only without export", decl("export_only = { who = [\"a\"] }\n"), "by_persona"},
		{"export_only not in schema", decl("export = \"by_persona\"\nexport_only = { nobody = [\"a\"] }\n"), "nobody"},
		{"export_only bool bad value", decl("export = \"by_persona\"\nexport_only = { flag = [\"yes\"] }\n"), "true"},
		{"pack_parent unknown kind", decl("export = \"by_persona\"\npack_parent = { kind = \"nobody\", field = \"who\" }\n"), "pack_parent"},
		{"pack_parent itself", decl("export = \"by_persona\"\npack_parent = { kind = \"look\", field = \"who\" }\n"), "pack_parent"},
		{"pack_parent field not string", decl("export = \"by_persona\"\npack_parent = { kind = \"top\", field = \"n\" }\n") +
			"\n[[contributes.data]]\nkind = \"top\"\nschema = \"schemas/look.toml\"\nids = \"given\"\nexport = \"by_persona\"\n", "type = \"string\""},
		{"pack_parent to by_folder kind", decl("export = \"by_persona\"\npack_parent = { kind = \"top\", field = \"who\" }\n") +
			"\n[[contributes.data]]\nkind = \"top\"\nschema = \"schemas/look.toml\"\nids = \"given\"\nexport = \"by_folder\"\nfolder_field = \"dir\"\n", "不是"},
	}
	for _, c := range cases {
		dir := h.write(sdk.TierMine, "bad", dataPkg("bad", "1.0.0", c.decl), map[string]string{"schemas/look.toml": lookSchema + "[[field]]\nname = \"must\"\ntype = \"string\"\nrequired = true\n[[field]]\nname = \"tone\"\ntype = \"enum\"\nenum = [\"soft\", \"calm\"]\n[[field]]\nname = \"flag\"\ntype = \"bool\"\n"})
		_, err := ParseManifest(dir, sdk.TierMine)
		var pe *platform.Err
		if err == nil || !asPlatformErr(err, &pe) || pe.Field != "contributes.data" || !strings.Contains(pe.Message, c.want) {
			t.Errorf("%s：要 contributes.data 加「%s」，實際 %v", c.name, c.want, err)
		}
	}
	// 整個流程：壞的宣告讓套件 rejected，好的照常。
	h.write(sdk.TierMine, "bad", dataPkg("bad", "1.0.0", decl("export = \"by_folder\"\n")), map[string]string{"schemas/look.toml": lookSchema})
	h.start()
	if got := h.get("bad"); got.State != sdk.PackageStateRejected || !strings.Contains(got.Error, "folder_field") {
		t.Fatalf("壞宣告要 rejected：%+v", got)
	}
	if got := h.get("wear"); got.State == sdk.PackageStateRejected {
		t.Fatalf("好的套件不受影響：%+v", got)
	}
}

// 官方衣櫃的衣服目錄跟著角色走、隱藏版跟著資料夾裡的角色圖走；兌換、檢視、點數、輪播、請求不跟（D-63）。
func TestClosetDeclaresExport(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "packages", "official", "closet"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(root, sdk.TierOfficial)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range m.Public(sdk.TierOfficial).Data {
		// persona_field 沒有 export 也能單獨寫（PKG-64），這裡只看匯出用的欄位。
		field := d.FolderField
		if d.Export == sdk.DataExportByPersona {
			field = d.PersonaField
		}
		got[d.Kind] = d.Export + "/" + field
	}
	want := map[string]string{"outfit": "by_persona/persona", "photo": "by_persona/persona", "variant": "by_folder/dir", "unlock": "/", "person": "/", "state": "/", "rotation": "/", "request": "/", "note": "/"}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("closet.%s：%q，要 %q", k, got[k], w)
		}
	}
	// 隱藏版的開關 on 是這個家當下的狀態，不跟著走（PKG-58）；時段 windows 照帶。
	for _, d := range m.Public(sdk.TierOfficial).Data {
		if d.Kind == "variant" && (len(d.ExportOmit) != 1 || d.ExportOmit[0] != "on") {
			t.Errorf("closet.variant 的 export_omit 要是 [on]：%v", d.ExportOmit)
		}
		// from（升成官方前的編號，DEV-141）只對這個家有意義：帶到別的家會被當成「那邊的舊編號兌換過就擁有」，所以不跟著走。
		// adopted（設計中單張採用的進度，SEC-10）也是這個家當下的審核狀態，不跟著走。
		// 官方角色包只帶官方來源、已採用或下架的衣服（PKG-68，作者 2026-11-14）：主人自己的、設計中的不帶。
		if d.Kind == "outfit" && (len(d.PackOnly) != 2 || strings.Join(d.PackOnly["source"], ",") != "official" || strings.Join(d.PackOnly["state"], ",") != "active,retired" || d.PackOnlyNote == "") {
			t.Errorf("closet.outfit 的 pack_only 要是 source = [official]、state = [active retired]，而且有白話說明：%v %q", d.PackOnly, d.PackOnlyNote)
		}
		// 照片（DEV-160）：官方角色包只帶官方位置、一般版、已採用的；主人自己的（personas/mine/）、隱藏版、還沒採用的不帶。採用狀態跟著走（不 omit）。
		if d.Kind == "photo" && (len(d.PackOnly) != 3 || strings.Join(d.PackOnly["source"], ",") != "official" || strings.Join(d.PackOnly["hidden"], ",") != "false" || strings.Join(d.PackOnly["adopted"], ",") != "true" || d.PackOnlyNote == "" || len(d.ExportOmit) != 0) {
			t.Errorf("closet.photo 的 pack_only 要是 source = [official]、hidden = [false]、adopted = [true]，不拿掉欄位：%v %q %v", d.PackOnly, d.PackOnlyNote, d.ExportOmit)
		}
		// 照片紀錄指著檔（DEV-160）：建包、匯出照 file 帶檔；隱藏版不跟著走；衣服沒進官方角色包的照片也不進。
		if d.Kind == "photo" && (d.FileField != "file" || strings.Join(d.ExportOnly["hidden"], ",") != "false" || len(d.ExportOnly) != 1 ||
			d.PackParent == nil || d.PackParent.Kind != "outfit" || d.PackParent.Field != "outfit") {
			t.Errorf("closet.photo 要宣告 file_field = file、export_only = { hidden = [false] }、pack_parent = outfit/outfit：%q %v %+v", d.FileField, d.ExportOnly, d.PackParent)
		}
		if d.Kind != "photo" && (d.FileField != "" || d.ExportOnly != nil || d.PackParent != nil) {
			t.Errorf("closet.%s 不該有 file_field、export_only、pack_parent", d.Kind)
		}
		// 主人標了 private = true 的衣服不進官方角色包（PKG-71，作者 2026-11-26）：這個家照常有、照常穿。
		if d.Kind == "outfit" && (len(d.PackExcept) != 1 || strings.Join(d.PackExcept["private"], ",") != "true" || d.PackExceptNote == "") {
			t.Errorf("closet.outfit 的 pack_except 要是 private = [true]，而且有白話說明：%v %q", d.PackExcept, d.PackExceptNote)
		}
		if d.Kind != "outfit" && d.PackExcept != nil {
			t.Errorf("closet.%s 不該有 pack_except：%v", d.Kind, d.PackExcept)
		}
		if d.Kind != "outfit" && d.Kind != "photo" && d.PackOnly != nil {
			t.Errorf("closet.%s 不該有 pack_only：%v", d.Kind, d.PackOnly)
		}
		if d.Kind == "outfit" && (len(d.ExportOmit) != 2 || d.ExportOmit[0] != "from" || d.ExportOmit[1] != "adopted") {
			t.Errorf("closet.outfit 的 export_omit 要是 [from adopted]：%v", d.ExportOmit)
		}
	}
}
