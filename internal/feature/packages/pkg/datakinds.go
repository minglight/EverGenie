package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 套件的資料種類（D-28、REQ-173）：manifest 的 [[contributes.data]] 指到 schemas/<種類>.toml 的欄位規格，
// 核心把它註冊成 vault 的種類 `<套件 id>.<種類>`。這份檔案管規格檔的解析與驗證、轉成 vault 的 Spec 與 SDK 的宣告。

var (
	dataKindPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	dataFieldPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	dataPrefixPattern = regexp.MustCompile(`^[A-Z]+-$`)
)

// reservedDataFields 是核心自己會寫的欄位名稱，套件的欄位不能用。
var reservedDataFields = map[string]bool{"schema": true, "id": true, "created_at": true, "updated_at": true, "by": true, "kind": true, "archived_at": true}

// dataFieldTypes 是欄位型別對到 vault 的型別。
var dataFieldTypes = map[string]vault.FieldType{
	"string": vault.String, "int": vault.Int, "number": vault.Number, "bool": vault.Bool, "date": vault.Date,
	"datetime": vault.DateTime, "enum": vault.Enum, "list": vault.List, "map": vault.Map,
}

// ContribData 是 manifest 的 [[contributes.data]]。
type ContribData struct {
	Kind   string `toml:"kind"`
	Schema string `toml:"schema"`
	IDs    string `toml:"ids"`
	Prefix string `toml:"prefix"`
	// 匯出角色時要不要跟著走（PKG-57、D-63）：by_persona（persona_field，預設 persona）或 by_folder（folder_field 必填）；只給 ids = "given" 的種類。
	Export       string `toml:"export"`
	PersonaField string `toml:"persona_field"`
	FolderField  string `toml:"folder_field"`
	// ExportOmit 是匯出（與角色包）時拿掉的欄位；匯入時本機已有這筆就保留本機的值（PKG-58）。只能配 export，要是規格裡、不是必填、不是 export 用的欄位。
	ExportOmit []string `toml:"export_omit"`
	// PackOnly 是「官方角色包只帶哪些」（PKG-68）：欄位名 → 可以公開的值，每個列出的欄位的值都在清單裡才進官方角色包（建包不收、同步不寫）；
	// 只能配 export = "by_persona"，欄位要是規格裡的 string、enum 或 bool（值寫 "true"／"false"，DEV-160）、不在 export_omit 裡。PackOnlyNote 是被擋下的紀錄的白話名稱（建包的輸出用）。
	PackOnly     map[string][]string `toml:"pack_only"`
	PackOnlyNote string              `toml:"pack_only_note"`
	// PackExcept 是「官方角色包不帶哪些」（PKG-71）：欄位名 → 不公開的值，任何一個列出的欄位的值在清單裡就不進官方角色包（建包不收、同步不寫）；
	// 欄位沒填、值不在清單裡＝不擋。宣告規則同 pack_only。PackExceptNote 是被它擋下的紀錄的白話名稱（建包的輸出用）。
	PackExcept     map[string][]string `toml:"pack_except"`
	PackExceptNote string              `toml:"pack_except_note"`
	// 角色改代號時紀錄怎麼跟著走（PKG-64）：persona_field 也可以單獨寫（值是角色代號）；persona_id 是 exact／prefix（只給 given）；
	// persona_prefixed 是值為 <角色代號>-<其他> 的欄位；persona_source 是值為 official／mine 的欄位（這筆的圖放哪）。
	PersonaID       string   `toml:"persona_id"`
	PersonaPrefixed []string `toml:"persona_prefixed"`
	PersonaSource   string   `toml:"persona_source"`
	// 紀錄指著檔的種類（DEV-160，只配 export = "by_persona"）：file_field 是值為這筆的檔的 string 欄位（相對於擁有者的位置：
	// persona_source 是 mine 時是 personas/mine/，其他是官方角色資料夾），匯出與官方角色包照它帶檔；
	// export_only 是跟著角色走時只帶哪些（格式同 pack_only，匯出與官方角色包都看）；
	// pack_parent 是上層紀錄（kind＝同一個套件的種類、field＝放上層編號的 string 欄位），上層沒進官方角色包時這筆也不進。
	FileField  string              `toml:"file_field"`
	ExportOnly map[string][]string `toml:"export_only"`
	PackParent *contribParent      `toml:"pack_parent"`
}

// contribParent 是 contributes.data 的 pack_parent。
type contribParent struct {
	Kind  string `toml:"kind"`
	Field string `toml:"field"`
}

// 匯出方式（contributes.data 的 export）。
const (
	ExportByPersona = sdk.DataExportByPersona
	ExportByFolder  = sdk.DataExportByFolder
)

type dataFieldFile struct {
	Name        string   `toml:"name"`
	Type        string   `toml:"type"`
	Required    bool     `toml:"required"`
	Enum        []string `toml:"enum"`
	Description string   `toml:"description"`
}

// dataSchemaFile 是 schemas/<種類>.toml。
type dataSchemaFile struct {
	Schema    int             `toml:"schema"`
	Title     string          `toml:"title"`
	DateField string          `toml:"date_field"`
	Field     []dataFieldFile `toml:"field"`
}

// DataKind 是解析好的一種資料。
type DataKind struct {
	Kind      string
	Title     string
	IDs       string // seq｜given
	Prefix    string
	DateField string
	Fields    []dataFieldFile
	// 匯出角色時跟不跟著走：Export 空＝不跟；ExportField 是 by_persona 的角色欄位或 by_folder 的資料夾欄位。
	Export      string
	ExportField string
	ExportOmit  []string
	// 官方角色包只帶哪些（PKG-68）：欄位名 → 可以公開的值；PackOnlyNote 是被擋下的紀錄的白話名稱。
	PackOnly     map[string][]string
	PackOnlyNote string
	// 官方角色包不帶哪些（PKG-71）：欄位名 → 不公開的值；PackExceptNote 是被它擋下的紀錄的白話名稱。
	PackExcept     map[string][]string
	PackExceptNote string
	// 角色改代號時怎麼跟著走（PKG-64）：PersonaField 是值為角色代號的欄位（export = by_persona 時就是 ExportField）。
	PersonaField    string
	PersonaID       string
	PersonaPrefixed []string
	PersonaSource   string
	// 紀錄指著檔（DEV-160）：FileField 是放檔的欄位；ExportOnly 是跟著角色走時只帶哪些；PackParent 是上層紀錄（nil＝沒有）。
	FileField  string
	ExportOnly map[string][]string
	PackParent *sdk.DataParentDecl
}

// parseDataKinds 讀 manifest 宣告的每一種資料的規格檔並驗證；錯誤都寫是哪個種類、哪個檔、哪一欄。
// dir 是空的（只驗 manifest 本身，例如安裝前的檢查）時不讀檔案。
func parseDataKinds(decls []ContribData, dir string) ([]DataKind, error) {
	bad := func(format string, a ...any) error {
		return platform.Invalid("contributes.data", fmt.Sprintf(format, a...))
	}
	seen := map[string]bool{}
	var out []DataKind
	for _, d := range decls {
		if !dataKindPattern.MatchString(d.Kind) {
			return nil, bad("資料種類 kind「%s」要是小寫英文、數字與 -，英文開頭（例：meal）", d.Kind)
		}
		if seen[d.Kind] {
			return nil, bad("資料種類「%s」重複宣告", d.Kind)
		}
		seen[d.Kind] = true
		switch d.IDs {
		case "seq":
			if !dataPrefixPattern.MatchString(d.Prefix) {
				return nil, bad("資料種類「%s」的 ids = \"seq\" 要寫 prefix（大寫英文加 -，例：M-）", d.Kind)
			}
		case "given":
			if d.Prefix != "" {
				return nil, bad("資料種類「%s」的 ids = \"given\"（編號由呼叫者給）不能有 prefix", d.Kind)
			}
		default:
			return nil, bad("資料種類「%s」的 ids 要是 seq（核心發編號）或 given（呼叫者給編號）", d.Kind)
		}
		if !safeRel(d.Schema) {
			return nil, bad("資料種類「%s」的 schema 必須是套件資料夾裡面的相對路徑", d.Kind)
		}
		k := DataKind{Kind: d.Kind, IDs: d.IDs, Prefix: d.Prefix}
		if err := k.exportDecl(d); err != nil {
			return nil, err
		}
		if err := k.personaDecl(d); err != nil {
			return nil, err
		}
		if err := k.fileDecl(d); err != nil {
			return nil, err
		}
		if dir != "" {
			if err := k.load(filepath.Join(dir, filepath.FromSlash(d.Schema)), d.Schema); err != nil {
				return nil, err
			}
		}
		out = append(out, k)
	}
	// pack_parent 指的種類要是同一個套件宣告的、跟著角色走的（by_persona）種類。
	byKind := map[string]DataKind{}
	for _, k := range out {
		byKind[k.Kind] = k
	}
	for _, k := range out {
		if k.PackParent == nil {
			continue
		}
		if p, ok := byKind[k.PackParent.Kind]; !ok || p.Export != ExportByPersona || p.Kind == k.Kind {
			return nil, bad("資料種類「%s」的 pack_parent 要指到這個套件另一種 export = \"by_persona\" 的資料（kind = \"%s\" 不是）", k.Kind, k.PackParent.Kind)
		}
	}
	return out, nil
}

// exportDecl 驗證並記下 export、persona_field、folder_field（PKG-57）：只有 given 的種類能跟著角色匯出（編號由核心發的種類，
// 在對方的家會撞號）；by_persona 的欄位沒寫就是 persona，by_folder 一定要寫 folder_field；沒寫 export 就不能寫這兩個欄位。
// 欄位要真的在規格檔裡、型別是 string，在 load 查。
func (k *DataKind) exportDecl(d ContribData) error {
	bad := func(format string, a ...any) error {
		return platform.Invalid("contributes.data", fmt.Sprintf("資料種類「%s」的 ", d.Kind)+fmt.Sprintf(format, a...))
	}
	switch d.Export {
	case "":
		// persona_field 可以單獨寫（PKG-64）：只表示這個欄位的值是角色代號，在 personaDecl 驗。
		if d.FolderField != "" || len(d.ExportOmit) > 0 {
			return bad("folder_field、export_omit 只能跟 export 一起寫")
		}
		if d.FileField != "" || d.ExportOnly != nil || d.PackParent != nil {
			return bad("file_field、export_only、pack_parent 只能跟 export = \"by_persona\" 一起寫")
		}
		if d.PackOnly != nil || d.PackOnlyNote != "" || d.PackExcept != nil || d.PackExceptNote != "" {
			return bad("pack_only、pack_only_note、pack_except、pack_except_note 只能跟 export = \"by_persona\" 一起寫（只有跟著角色走的紀錄會進官方角色包）")
		}
		return nil
	case ExportByPersona:
		if d.FolderField != "" {
			return bad("export = \"by_persona\" 不能寫 folder_field（要寫的是 persona_field，沒寫就是 persona）")
		}
		k.ExportField = d.PersonaField
		if k.ExportField == "" {
			k.ExportField = "persona"
		}
	case ExportByFolder:
		if d.PersonaField != "" {
			return bad("export = \"by_folder\" 不能寫 persona_field")
		}
		if d.FolderField == "" {
			return bad("export = \"by_folder\" 要寫 folder_field（哪個欄位是 personas/ 底下的資料夾）")
		}
		k.ExportField = d.FolderField
	default:
		return bad("export 要是 by_persona（跟著角色欄位）或 by_folder（跟著資料夾裡的角色圖）")
	}
	if !dataFieldPattern.MatchString(k.ExportField) || reservedDataFields[k.ExportField] {
		return bad("匯出用的欄位「%s」名稱不對", k.ExportField)
	}
	if d.IDs != "given" {
		return bad("export 只給 ids = \"given\" 的種類（核心發編號的種類到別的家會撞號）")
	}
	seen := map[string]bool{}
	for _, name := range d.ExportOmit {
		switch {
		case !dataFieldPattern.MatchString(name) || reservedDataFields[name]:
			return bad("export_omit 的欄位「%s」名稱不對", name)
		case name == k.ExportField:
			return bad("export_omit 不能拿掉匯出用的欄位「%s」", name)
		case seen[name]:
			return bad("export_omit 的欄位「%s」重複", name)
		}
		seen[name] = true
	}
	k.Export, k.ExportOmit = d.Export, append([]string(nil), d.ExportOmit...)
	if err := k.packOnlyDecl(d, seen, bad); err != nil {
		return err
	}
	if err := k.packExceptDecl(d, seen, bad); err != nil {
		return err
	}
	if d.ExportOnly == nil {
		return nil
	}
	if d.Export != ExportByPersona {
		return bad("export_only 只能跟 export = \"by_persona\" 一起寫")
	}
	only, err := onlyValues("export_only", d.ExportOnly, k.ExportField, seen, bad)
	if err != nil {
		return err
	}
	k.ExportOnly = only
	return nil
}

// fileDecl 驗證並記下 file_field、pack_parent（DEV-160）：只配 export = "by_persona"；欄位名稱要對、不能是角色代號或 persona_source 的欄位。
// 欄位在規格裡、型別是 string，在 load 查；pack_parent 指的種類在 parseDataKinds 最後查。
func (k *DataKind) fileDecl(d ContribData) error {
	bad := func(format string, a ...any) error {
		return platform.Invalid("contributes.data", fmt.Sprintf("資料種類「%s」的 ", d.Kind)+fmt.Sprintf(format, a...))
	}
	if d.FileField == "" && d.PackParent == nil {
		return nil
	}
	if d.Export != ExportByPersona {
		return bad("file_field、pack_parent 只能跟 export = \"by_persona\" 一起寫")
	}
	if d.FileField != "" {
		switch {
		case !dataFieldPattern.MatchString(d.FileField) || reservedDataFields[d.FileField]:
			return bad("file_field 的欄位「%s」名稱不對", d.FileField)
		case d.FileField == k.ExportField || d.FileField == k.PersonaSource || slices.Contains(k.PersonaPrefixed, d.FileField):
			return bad("file_field 的欄位「%s」不能同時是角色代號或 persona_source 的欄位", d.FileField)
		case slices.Contains(k.ExportOmit, d.FileField):
			return bad("file_field 的欄位「%s」不能同時在 export_omit 裡（拿掉之後對方的家不知道檔在哪）", d.FileField)
		}
		k.FileField = d.FileField
	}
	if p := d.PackParent; p != nil {
		switch {
		case !dataKindPattern.MatchString(p.Kind):
			return bad("pack_parent 的 kind「%s」要是這個套件的資料種類（不含套件 id）", p.Kind)
		case !dataFieldPattern.MatchString(p.Field) || reservedDataFields[p.Field] || p.Field == k.ExportField:
			return bad("pack_parent 的欄位「%s」名稱不對", p.Field)
		}
		k.PackParent = &sdk.DataParentDecl{Kind: p.Kind, Field: p.Field}
	}
	return nil
}

// packOnlyDecl 驗證並記下 pack_only、pack_only_note（PKG-68）：只配 export = "by_persona"（by_folder 的本來就不進官方角色包）；
// 欄位名稱要對、不能是 export_omit 的欄位（拿掉了對方就看不到值，同步端會整種都不收）；每個欄位至少一個值、值不能空白或重複；
// 說明要配 pack_only、一行、不超過 60 個字。欄位在規格裡、型別是 string、enum 或 bool、值是 enum 的選項（bool 寫 "true"／"false"），在 load 查。
func (k *DataKind) packOnlyDecl(d ContribData, omitted map[string]bool, bad func(string, ...any) error) error {
	note := strings.TrimSpace(d.PackOnlyNote)
	if len(d.PackOnly) == 0 {
		if note != "" || d.PackOnly != nil {
			return bad("pack_only 至少要寫一個欄位（pack_only_note 要配 pack_only）")
		}
		return nil
	}
	if d.Export != ExportByPersona {
		return bad("pack_only 只能跟 export = \"by_persona\" 一起寫（跟著資料夾走的紀錄本來就不進官方角色包）")
	}
	if strings.ContainsAny(note, "\r\n") || len([]rune(note)) > 60 {
		return bad("pack_only_note 要是一行、60 個字以內的白話說明")
	}
	only, err := onlyValues("pack_only", d.PackOnly, k.ExportField, omitted, bad)
	if err != nil {
		return err
	}
	k.PackOnly, k.PackOnlyNote = only, note
	return nil
}

// packExceptDecl 驗證並記下 pack_except、pack_except_note（PKG-71）：規則同 pack_only（packOnlyDecl），只是意思反過來——
// 列出的欄位的值在清單裡就不進官方角色包。欄位型別、enum 選項、bool 的寫法一樣在 load 查。
func (k *DataKind) packExceptDecl(d ContribData, omitted map[string]bool, bad func(string, ...any) error) error {
	note := strings.TrimSpace(d.PackExceptNote)
	if len(d.PackExcept) == 0 {
		if note != "" || d.PackExcept != nil {
			return bad("pack_except 至少要寫一個欄位（pack_except_note 要配 pack_except）")
		}
		return nil
	}
	if d.Export != ExportByPersona {
		return bad("pack_except 只能跟 export = \"by_persona\" 一起寫（跟著資料夾走的紀錄本來就不進官方角色包）")
	}
	if strings.ContainsAny(note, "\r\n") || len([]rune(note)) > 60 {
		return bad("pack_except_note 要是一行、60 個字以內的白話說明")
	}
	except, err := onlyValues("pack_except", d.PackExcept, k.ExportField, omitted, bad)
	if err != nil {
		return err
	}
	k.PackExcept, k.PackExceptNote = except, note
	return nil
}

// onlyValues 驗 pack_only／pack_except／export_only 的「欄位名 → 值」（label 是錯誤訊息用的名字）：欄位名稱要對、不是角色代號的欄位、不在 export_omit 裡；
// 每個欄位至少一個值、值不能空白或重複。回一份複製。
func onlyValues(label string, m map[string][]string, personaField string, omitted map[string]bool, bad func(string, ...any) error) (map[string][]string, error) {
	if len(m) == 0 {
		return nil, bad("%s 至少要寫一個欄位", label)
	}
	only := map[string][]string{}
	for _, name := range sortedKeys(m) {
		values := m[name]
		switch {
		case !dataFieldPattern.MatchString(name) || reservedDataFields[name]:
			return nil, bad("%s 的欄位「%s」名稱不對", label, name)
		case name == personaField:
			return nil, bad("%s 不能用角色代號的欄位「%s」（哪幾位角色公開看 home.toml 的 public_personas）", label, name)
		case omitted[name]:
			return nil, bad("%s 的欄位「%s」不能同時在 export_omit 裡（拿掉之後對方的家看不到這個值）", label, name)
		case len(values) == 0:
			return nil, bad("%s 的欄位「%s」至少要寫一個值", label, name)
		}
		seen := map[string]bool{}
		for _, v := range values {
			if strings.TrimSpace(v) == "" || seen[v] {
				return nil, bad("%s 的欄位「%s」的值不能空白或重複", label, name)
			}
			seen[v] = true
		}
		only[name] = append([]string(nil), values...)
	}
	return only, nil
}

// 角色代號放在編號裡的方式（persona_id，PKG-64）。
const (
	PersonaIDExact  = sdk.DataPersonaIDExact
	PersonaIDPrefix = sdk.DataPersonaIDPrefix
)

// personaDecl 驗證並記下「角色改代號時紀錄怎麼跟著走」的宣告（PKG-64）：persona_field（沒寫 export 時單獨寫；
// export = by_persona 時就是匯出用的那個欄位）、persona_id（exact／prefix，只給 given）、persona_prefixed、persona_source。
// 欄位要真的在規格檔裡、型別對，在 load 查。核心改代號的工具只照這些宣告做，不認得個別套件（法律第 3 條）。
func (k *DataKind) personaDecl(d ContribData) error {
	bad := func(format string, a ...any) error {
		return platform.Invalid("contributes.data", fmt.Sprintf("資料種類「%s」的 ", d.Kind)+fmt.Sprintf(format, a...))
	}
	k.PersonaField = d.PersonaField
	if d.Export == ExportByPersona {
		k.PersonaField = k.ExportField
	}
	switch d.PersonaID {
	case "", PersonaIDExact, PersonaIDPrefix:
	default:
		return bad("persona_id 要是 exact（編號就是角色代號）或 prefix（編號是 <角色代號>-<其他>）")
	}
	if d.PersonaID != "" && d.IDs != "given" {
		return bad("persona_id 只給 ids = \"given\" 的種類（核心發的流水號裡沒有角色代號）")
	}
	names := []string{}
	if k.PersonaField != "" {
		names = append(names, k.PersonaField)
	}
	seen := map[string]bool{}
	for _, name := range d.PersonaPrefixed {
		switch {
		case !dataFieldPattern.MatchString(name) || reservedDataFields[name]:
			return bad("persona_prefixed 的欄位「%s」名稱不對", name)
		case seen[name]:
			return bad("persona_prefixed 的欄位「%s」重複", name)
		case name == k.PersonaField:
			return bad("欄位「%s」不能同時是 persona_field 與 persona_prefixed", name)
		}
		seen[name] = true
	}
	if d.PersonaSource != "" {
		switch {
		case !dataFieldPattern.MatchString(d.PersonaSource) || reservedDataFields[d.PersonaSource]:
			return bad("persona_source 的欄位「%s」名稱不對", d.PersonaSource)
		case d.PersonaSource == k.PersonaField || seen[d.PersonaSource]:
			return bad("persona_source 的欄位「%s」不能同時是角色代號的欄位", d.PersonaSource)
		case k.PersonaField == "" && d.PersonaID == "":
			return bad("persona_source 要配 persona_field 或 persona_id（才知道這筆是哪位角色的）")
		}
	}
	for _, name := range append(names, d.PersonaPrefixed...) {
		if !dataFieldPattern.MatchString(name) || reservedDataFields[name] {
			return bad("persona_field 的欄位「%s」名稱不對", name)
		}
	}
	k.PersonaID, k.PersonaPrefixed, k.PersonaSource = d.PersonaID, append([]string(nil), d.PersonaPrefixed...), d.PersonaSource
	return nil
}

// load 讀並驗證一個規格檔；rel 是它在套件裡的相對路徑（錯誤訊息用）。
func (k *DataKind) load(path, rel string) error {
	bad := func(format string, a ...any) error {
		return platform.Invalid("contributes.data", fmt.Sprintf("資料種類「%s」的欄位規格 %s：", k.Kind, rel)+fmt.Sprintf(format, a...))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return bad("找不到這個檔案")
	}
	var f dataSchemaFile
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		where := ""
		if pe, ok := err.(toml.ParseError); ok && pe.Position.Line > 0 {
			where = fmt.Sprintf("（第 %d 行）", pe.Position.Line)
		}
		return bad("格式不對%s", where)
	}
	if undec := md.Undecoded(); len(undec) > 0 {
		keys := make([]string, 0, len(undec))
		for _, u := range undec {
			keys = append(keys, u.String())
		}
		return bad("有不認得的欄位：%s", strings.Join(keys, "、"))
	}
	if f.Schema != 1 {
		return bad("schema 必須是 1")
	}
	if strings.TrimSpace(f.Title) == "" {
		return bad("要寫白話名稱 title")
	}
	k.Title = f.Title
	names := map[string]string{}
	for i, fd := range f.Field {
		if !dataFieldPattern.MatchString(fd.Name) {
			return bad("第 %d 個欄位的 name「%s」要是小寫英文、數字與 _，英文開頭", i+1, fd.Name)
		}
		if reservedDataFields[fd.Name] {
			return bad("欄位 %s 是核心保留的名稱，請換一個", fd.Name)
		}
		if _, dup := names[fd.Name]; dup {
			return bad("欄位 %s 重複", fd.Name)
		}
		if _, ok := dataFieldTypes[fd.Type]; !ok {
			return bad("欄位 %s 的 type「%s」不認得（可用 string、int、number、bool、date、datetime、enum、list、map）", fd.Name, fd.Type)
		}
		switch {
		case fd.Type == "enum" && len(fd.Enum) == 0:
			return bad("欄位 %s 是 enum，要寫 enum 的選項", fd.Name)
		case fd.Type != "enum" && len(fd.Enum) > 0:
			return bad("欄位 %s 不是 enum，不能寫 enum 的選項", fd.Name)
		}
		for _, v := range fd.Enum {
			if strings.TrimSpace(v) == "" {
				return bad("欄位 %s 的 enum 選項不能是空白", fd.Name)
			}
		}
		names[fd.Name] = fd.Type
	}
	if f.DateField != "" {
		if typ, ok := names[f.DateField]; !ok || typ != "date" {
			return bad("date_field「%s」要指到一個 type = \"date\" 的欄位", f.DateField)
		}
	}
	if k.Export != "" {
		if typ, ok := names[k.ExportField]; !ok || typ != "string" {
			return bad("export 用的欄位「%s」要是這個規格裡 type = \"string\" 的欄位", k.ExportField)
		}
		required := map[string]bool{}
		for _, fd := range f.Field {
			required[fd.Name] = fd.Required
		}
		for _, name := range k.ExportOmit {
			if _, ok := names[name]; !ok {
				return bad("export_omit 的欄位「%s」不在這個規格裡", name)
			}
			if required[name] {
				return bad("export_omit 的欄位「%s」是必填的，拿掉之後對方的家寫不進去", name)
			}
		}
	}
	for _, only := range []struct {
		label string
		m     map[string][]string
	}{{"pack_only", k.PackOnly}, {"pack_except", k.PackExcept}, {"export_only", k.ExportOnly}} {
		for _, name := range sortedKeys(only.m) {
			var fd *dataFieldFile
			for i := range f.Field {
				if f.Field[i].Name == name {
					fd = &f.Field[i]
				}
			}
			switch {
			case fd == nil:
				return bad("%s 的欄位「%s」不在這個規格裡", only.label, name)
			case fd.Type != "string" && fd.Type != "enum" && fd.Type != "bool":
				return bad("%s 的欄位「%s」要是 type = \"string\"、\"enum\" 或 \"bool\" 的欄位", only.label, name)
			}
			if fd.Type == "bool" {
				for _, v := range only.m[name] {
					if v != "true" && v != "false" {
						return bad("%s 的欄位「%s」是 bool，值只能寫 \"true\" 或 \"false\"", only.label, name)
					}
				}
			}
			if fd.Type == "enum" {
				for _, v := range only.m[name] {
					if !hasAll(fd.Enum, v) {
						return bad("%s 的欄位「%s」的值「%s」不是這個欄位的選項", only.label, name, v)
					}
				}
			}
		}
	}
	for _, name := range []string{k.FileField, func() string {
		if k.PackParent != nil {
			return k.PackParent.Field
		}
		return ""
	}()} {
		if name == "" {
			continue
		}
		var fd *dataFieldFile
		for i := range f.Field {
			if f.Field[i].Name == name {
				fd = &f.Field[i]
			}
		}
		if fd == nil || fd.Type != "string" {
			return bad("file_field／pack_parent 的欄位「%s」要是這個規格裡 type = \"string\" 的欄位", name)
		}
	}
	if err := k.checkPersonaFields(f.Field, bad); err != nil {
		return err
	}
	k.DateField = f.DateField
	k.Fields = f.Field
	return nil
}

// checkPersonaFields：角色代號的宣告（PKG-64）指到的欄位要在規格裡：persona_field、persona_prefixed 是 string；
// persona_source 是 string，或選項有 official 與 mine 的 enum。
func (k *DataKind) checkPersonaFields(fields []dataFieldFile, bad func(string, ...any) error) error {
	byName := map[string]dataFieldFile{}
	for _, fd := range fields {
		byName[fd.Name] = fd
	}
	if k.PersonaField != "" && k.Export != ExportByPersona {
		if fd, ok := byName[k.PersonaField]; !ok || fd.Type != "string" {
			return bad("persona_field「%s」要是這個規格裡 type = \"string\" 的欄位", k.PersonaField)
		}
	}
	for _, name := range k.PersonaPrefixed {
		if fd, ok := byName[name]; !ok || fd.Type != "string" {
			return bad("persona_prefixed 的欄位「%s」要是這個規格裡 type = \"string\" 的欄位", name)
		}
	}
	if k.PersonaSource != "" {
		fd, ok := byName[k.PersonaSource]
		switch {
		case !ok:
			return bad("persona_source「%s」不在這個規格裡", k.PersonaSource)
		case fd.Type == "string":
		case fd.Type == "enum" && hasAll(fd.Enum, "official", "mine"):
		default:
			return bad("persona_source「%s」要是 string，或選項有 official 與 mine 的 enum", k.PersonaSource)
		}
	}
	return nil
}

func hasAll(list []string, want ...string) bool {
	have := map[string]bool{}
	for _, s := range list {
		have[s] = true
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}

// decl 轉成 SDK 的宣告（List／Get 回傳）。
func (k DataKind) decl() sdk.DataKindDecl {
	d := sdk.DataKindDecl{Kind: k.Kind, Title: k.Title, IDs: k.IDs, Prefix: k.Prefix, DateField: k.DateField, Export: k.Export, ExportOmit: append([]string(nil), k.ExportOmit...)}
	switch k.Export {
	case ExportByFolder:
		d.FolderField = k.ExportField
	}
	if len(k.PackOnly) > 0 {
		d.PackOnly, d.PackOnlyNote = map[string][]string{}, k.PackOnlyNote
		for name, values := range k.PackOnly {
			d.PackOnly[name] = append([]string(nil), values...)
		}
	}
	if len(k.PackExcept) > 0 {
		d.PackExcept, d.PackExceptNote = map[string][]string{}, k.PackExceptNote
		for name, values := range k.PackExcept {
			d.PackExcept[name] = append([]string(nil), values...)
		}
	}
	d.PersonaField = k.PersonaField
	d.PersonaID, d.PersonaSource = k.PersonaID, k.PersonaSource
	d.PersonaPrefixed = append([]string(nil), k.PersonaPrefixed...)
	d.FileField = k.FileField
	if len(k.ExportOnly) > 0 {
		d.ExportOnly = map[string][]string{}
		for name, values := range k.ExportOnly {
			d.ExportOnly[name] = append([]string(nil), values...)
		}
	}
	if k.PackParent != nil {
		p := *k.PackParent
		d.PackParent = &p
	}
	for _, f := range k.Fields {
		d.Fields = append(d.Fields, sdk.DataFieldDecl{Name: f.Name, Type: f.Type, Required: f.Required, Enum: append([]string(nil), f.Enum...), Description: f.Description})
	}
	return d
}

// spec 轉成 vault 的種類（完整名稱 <套件 id>.<種類>）。核心自己寫的欄位排在最前面、都不必填（使用者在外面手改檔案也不會因此壞掉）。
func (k DataKind) spec(pkgID string) vault.Spec {
	fields := []vault.Field{
		{Name: "schema", Type: vault.Int}, {Name: "id", Type: vault.String}, {Name: "created_at", Type: vault.DateTime},
		{Name: "updated_at", Type: vault.DateTime}, {Name: "by", Type: vault.String},
	}
	for _, f := range k.Fields {
		fields = append(fields, vault.Field{Name: f.Name, Type: dataFieldTypes[f.Type], Required: f.Required, Enum: append([]string(nil), f.Enum...)})
	}
	kind := k.Kind
	return vault.Spec{
		Kind:     vault.PackageKind(pkgID, kind),
		Fields:   fields,
		IDMode:   k.IDs,
		IDPrefix: k.Prefix,
		Path:     func(id string) string { return vault.PackagePath(pkgID, kind, id) },
	}
}
