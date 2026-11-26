package pkg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// data：套件的資料（D-28、REQ-173）。套件在 manifest 宣告資料種類與欄位規格，核心註冊成 vault 的種類 `<套件 id>.<種類>`；
// 這組指令是秘書讀寫它們的通用入口，全部經 vault API（欄位不符核心會拒絕並回白話原因）。
// 只管套件的資料：任務、待辦等核心種類請用各自的指令。

const dataUsage = `用法：evergenie data <子指令>   套件的資料（套件宣告的種類，名稱是 <套件>.<種類>，例 nutrition.meal）
  kinds [--json]                                     有哪些種類、每個欄位的名稱、型別、必填、說明
  list <種類> [--since YYYY-MM-DD] [--until YYYY-MM-DD] [--limit N] [--json]
                                                     新到舊；日期依種類的 date_field（沒有就用建立日）
  get <種類> <編號> [--json]
  add <種類> [--id <編號>] --json '<物件>' [--body-file <檔>]   新增一筆（--json - 從標準輸入讀）；編號由核心發的種類不能給 --id，要自己給的一定要給；印出編號
  set <種類> <編號> --json '<物件>'                     改指定的欄位（值是 null 的欄位拿掉），其他欄位與內文不動
  rm <種類> <編號>                                    封存這一筆（不刪檔）
欄位不符（缺必填、型別不對、不在規格裡）會被拒絕並說明；先用 kinds 看欄位。
`

func dataHelp(e *cmdline.Env, a []string) error {
	for _, x := range a {
		if x != "--help" && x != "-h" && x != "help" {
			return cmdline.Usagef("不認得的子指令：data %s（evergenie data --help 看用法）", x)
		}
	}
	_, err := fmt.Fprint(e.Out, dataUsage)
	return err
}

// dataKindInfo 是 `data kinds --json` 的一行。
type dataKindInfo struct {
	Package string `json:"package"`
	Kind    string `json:"kind"` // 完整名稱 <套件>.<種類>
	sdk.DataKindDecl
}

// dataKinds 列出目前註冊中的套件資料種類（沒被拒、沒停用的套件）。
func dataKinds(e *cmdline.Env) ([]dataKindInfo, error) {
	r, err := api(e).List(e.Ctx, &sdk.PackageListRequest{})
	if err != nil {
		return nil, err
	}
	var out []dataKindInfo
	for _, p := range r.Packages {
		if p.State == sdk.PackageStateRejected || !p.Enabled {
			continue
		}
		for _, d := range p.Data {
			info := dataKindInfo{Package: p.ID, DataKindDecl: d}
			info.Kind = p.ID + "." + d.Kind
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out, nil
}

// findKind 找一個完整種類名稱；核心種類（沒有 `.`）是用法錯誤，不認得的種類回白話說明。
func findKind(e *cmdline.Env, kind string) (dataKindInfo, error) {
	if !strings.Contains(kind, ".") {
		return dataKindInfo{}, cmdline.Usagef("data 只管套件的資料；任務、待辦請用各自的指令")
	}
	kinds, err := dataKinds(e)
	if err != nil {
		return dataKindInfo{}, err
	}
	var names []string
	for _, k := range kinds {
		if k.Kind == kind {
			return k, nil
		}
		names = append(names, k.Kind)
	}
	hint := "目前沒有任何套件宣告資料種類"
	if len(names) > 0 {
		hint = "有：" + strings.Join(names, "、")
	}
	return dataKindInfo{}, fmt.Errorf("沒有「%s」這種資料（套件停用或沒宣告；evergenie data kinds 看有哪些）。%s", kind, hint)
}

func fieldLine(f sdk.DataFieldDecl) string {
	typ := f.Type
	if f.Type == "enum" {
		typ = "enum(" + strings.Join(f.Enum, "|") + ")"
	}
	need := "選填"
	if f.Required {
		need = "必填"
	}
	line := fmt.Sprintf("  %-14s %-18s %s", f.Name, typ, need)
	if f.Description != "" {
		line += "　" + f.Description
	}
	return line
}

// data kinds [--json]
func dataKindsCmd(e *cmdline.Env, args []string) error {
	o, err := parseOpts(args, nil, []string{"json"}, "evergenie data kinds [--json]")
	if err != nil || len(o.pos) > 0 {
		return cmdline.Usagef("用法：evergenie data kinds [--json]")
	}
	kinds, err := dataKinds(e)
	if err != nil {
		return err
	}
	if o.flags["json"] {
		for _, k := range kinds {
			if err := e.Line(k); err != nil {
				return err
			}
		}
		return nil
	}
	if len(kinds) == 0 {
		_, err := fmt.Fprintln(e.Out, "目前沒有任何套件宣告資料種類")
		return err
	}
	var b strings.Builder
	for _, k := range kinds {
		ids := "編號要自己給（--id）"
		if k.IDs == "seq" {
			ids = "編號 " + k.Prefix + "<數字>，核心自動發"
		}
		fmt.Fprintf(&b, "%s　%s　%s\n", k.Kind, k.Title, ids)
		if k.DateField != "" {
			fmt.Fprintf(&b, "  日期欄位：%s\n", k.DateField)
		}
		switch k.Export {
		case ExportByPersona:
			fmt.Fprintf(&b, "  匯出角色時：跟著角色走（欄位 %s 是被匯出的角色）\n", k.PersonaField)
		case ExportByFolder:
			fmt.Fprintf(&b, "  匯出角色時：資料夾（欄位 %s）裡有被匯出角色的圖就跟著走，只帶她的圖\n", k.FolderField)
		}
		if len(k.ExportOmit) > 0 {
			fmt.Fprintf(&b, "  匯出時不帶的欄位：%s（匯入時已有這筆就保留本機的值）\n", strings.Join(k.ExportOmit, "、"))
		}
		if len(k.PackOnly) > 0 {
			var parts []string
			for _, name := range sortedKeys(k.PackOnly) {
				parts = append(parts, name+" 是 "+strings.Join(k.PackOnly[name], "／"))
			}
			fmt.Fprintf(&b, "  官方角色包只帶：%s 的（其他的不帶；主人點名的角色匯出照帶）\n", strings.Join(parts, "、"))
		}
		if len(k.PackExcept) > 0 {
			var parts []string
			for _, name := range sortedKeys(k.PackExcept) {
				parts = append(parts, name+" 是 "+strings.Join(k.PackExcept[name], "／"))
			}
			fmt.Fprintf(&b, "  官方角色包不帶：%s 的（沒寫這個欄位的照帶；主人點名的角色匯出照帶）\n", strings.Join(parts, "、"))
		}
		if len(k.ExportOnly) > 0 {
			var parts []string
			for _, name := range sortedKeys(k.ExportOnly) {
				parts = append(parts, name+" 是 "+strings.Join(k.ExportOnly[name], "／"))
			}
			fmt.Fprintf(&b, "  跟著角色走時只帶：%s 的（匯出與官方角色包都一樣，不帶的連檔也不帶）\n", strings.Join(parts, "、"))
		}
		if k.FileField != "" {
			fmt.Fprintf(&b, "  欄位 %s 是這筆的檔：匯出與官方角色包照它帶檔\n", k.FileField)
		}
		if k.PackParent != nil {
			fmt.Fprintf(&b, "  上層是 %s（欄位 %s）：上層沒進官方角色包時這筆連檔也不進\n", k.PackParent.Kind, k.PackParent.Field)
		}
		if line := personaDeclLine(k.DataKindDecl); line != "" {
			fmt.Fprintf(&b, "  角色改代號時跟著改：%s\n", line)
		}
		for _, f := range k.Fields {
			b.WriteString(fieldLine(f) + "\n")
		}
	}
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

// recordDate 是一筆資料的日期（YYYY-MM-DD）：種類的 date_field；種類沒有 date_field 或這一筆沒填，就用建立日。
func recordDate(k dataKindInfo, r sdk.Record) string {
	if k.DateField != "" {
		if s, ok := r.Fields[k.DateField].(string); ok && len(s) >= 10 {
			return s[:10]
		}
		// 這一筆沒填日期欄位：用建立日。
	}
	if s, ok := r.Fields["created_at"].(string); ok && len(s) >= 10 {
		return s[:10]
	}
	return ""
}

func createdAt(r sdk.Record) time.Time {
	s, _ := r.Fields["created_at"].(string)
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func dateFlag(o opts, name string) (string, error) {
	v := o.vals[name]
	if v == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return "", cmdline.Usagef("--%s 要寫成 YYYY-MM-DD（例：2026-10-25）", name)
	}
	return v, nil
}

func valueText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// data list <種類> [--since] [--until] [--limit] [--json]
func dataList(e *cmdline.Env, args []string) error {
	const usage = "evergenie data list <種類> [--since YYYY-MM-DD] [--until YYYY-MM-DD] [--limit N] [--json]"
	o, err := parseOpts(args, []string{"since", "until", "limit"}, []string{"json"}, usage)
	if err != nil || len(o.pos) != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	since, err := dateFlag(o, "since")
	if err != nil {
		return err
	}
	until, err := dateFlag(o, "until")
	if err != nil {
		return err
	}
	limit := 0
	if v := o.vals["limit"]; v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 {
			return cmdline.Usagef("--limit 要是 1 以上的整數")
		}
	}
	k, err := findKind(e, o.pos[0])
	if err != nil {
		return err
	}
	r, err := sdk.NewVaultClient(e.Client).List(e.Ctx, &sdk.VaultListRequest{Kind: k.Kind})
	if err != nil {
		return err
	}
	rows := make([]sdk.Record, 0, len(r.Records))
	for _, rec := range r.Records {
		d := recordDate(k, rec)
		if (since != "" || until != "") && d == "" {
			continue
		}
		if (since != "" && d < since) || (until != "" && d > until) {
			continue
		}
		rows = append(rows, rec)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		di, dj := recordDate(k, rows[i]), recordDate(k, rows[j])
		if di != dj {
			return di > dj
		}
		if ci, cj := createdAt(rows[i]), createdAt(rows[j]); !ci.Equal(cj) {
			return ci.After(cj)
		}
		return rows[i].ID > rows[j].ID
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	var b strings.Builder
	for _, rec := range rows {
		if o.flags["json"] {
			if err := e.Line(rec); err != nil {
				return err
			}
			continue
		}
		fmt.Fprintf(&b, "%s  %s  %s\n", rec.ID, orDash(recordDate(k, rec)), summary(k, rec))
	}
	if o.flags["json"] {
		return nil
	}
	if len(rows) == 0 {
		b.WriteString("沒有符合的資料\n")
	}
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// summary 是一行摘要：規格裡前三個欄位（有填的才列），`名稱=值`。
func summary(k dataKindInfo, r sdk.Record) string {
	var parts []string
	for i, f := range k.Fields {
		if i >= 3 {
			break
		}
		if v, ok := r.Fields[f.Name]; ok && v != nil && v != "" {
			parts = append(parts, f.Name+"="+valueText(v))
		}
	}
	return strings.Join(parts, "  ")
}

// data get <種類> <編號> [--json]
func dataGet(e *cmdline.Env, args []string) error {
	const usage = "evergenie data get <種類> <編號> [--json]"
	o, err := parseOpts(args, nil, []string{"json"}, usage)
	if err != nil || len(o.pos) != 2 {
		return cmdline.Usagef("用法：%s", usage)
	}
	k, err := findKind(e, o.pos[0])
	if err != nil {
		return err
	}
	r, err := sdk.NewVaultClient(e.Client).Get(e.Ctx, &sdk.VaultGetRequest{Kind: k.Kind, ID: o.pos[1]})
	if err != nil {
		return err
	}
	if o.flags["json"] {
		return e.Line(r.Record)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s（%s）\n", r.Record.ID, k.Title)
	for _, f := range k.Fields {
		if v, ok := r.Record.Fields[f.Name]; ok && v != nil {
			fmt.Fprintf(&b, "  %s: %s\n", f.Name, valueText(v))
		}
	}
	if c, ok := r.Record.Fields["created_at"].(string); ok {
		fmt.Fprintf(&b, "  建立：%s\n", c)
	}
	if r.Record.Archived {
		b.WriteString("  （已封存）\n")
	}
	if strings.TrimSpace(r.Record.Body) != "" {
		b.WriteString("\n" + r.Record.Body + "\n")
	}
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

// readObject 讀 --json 的值（"-" 從標準輸入讀）並解成物件。
func readObject(e *cmdline.Env, raw string) (map[string]any, error) {
	if raw == "-" {
		b, err := io.ReadAll(e.In)
		if err != nil {
			return nil, fmt.Errorf("讀不了標準輸入")
		}
		raw = string(b)
	}
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, cmdline.Usagef("--json 要是一個 JSON 物件，例如 '{\"text\":\"...\"}'")
	}
	if dec.More() {
		return nil, cmdline.Usagef("--json 只能有一個 JSON 物件")
	}
	return obj, nil
}

// fieldError 讓欄位不符的錯誤說出是哪個欄位（API 的錯誤把欄位名放在 field，訊息本身只說原因）。
func fieldError(err error) error {
	var se *sdk.Error
	if errors.As(err, &se) && se.Field != "" && se.Code == sdk.ErrorCodeInvalid && !strings.Contains(se.Message, se.Field) {
		return fmt.Errorf("欄位「%s」不符：%s", se.Field, se.Message)
	}
	return err
}

// checkKnownFields 擋掉規格裡沒有的欄位（打錯字的欄位名不會悄悄存進去）。
func checkKnownFields(k dataKindInfo, obj map[string]any) error {
	known := map[string]bool{}
	names := make([]string, 0, len(k.Fields))
	for _, f := range k.Fields {
		known[f.Name] = true
		names = append(names, f.Name)
	}
	var unknown []string
	for name := range obj {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("欄位「%s」不在「%s」的規格裡（可用的欄位：%s；evergenie data kinds 看說明）", strings.Join(unknown, "、"), k.Kind, strings.Join(names, "、"))
}

// data add <種類> [--id <編號>] --json '<物件>' [--body-file <檔>]
func dataAdd(e *cmdline.Env, args []string) error {
	const usage = "evergenie data add <種類> [--id <編號>] --json '<物件>' [--body-file <檔>]"
	o, err := parseOpts(args, []string{"id", "json", "body-file"}, nil, usage)
	if err != nil || len(o.pos) != 1 || o.vals["json"] == "" {
		return cmdline.Usagef("用法：%s", usage)
	}
	k, err := findKind(e, o.pos[0])
	if err != nil {
		return err
	}
	id := o.vals["id"]
	switch {
	case k.IDs == "seq" && id != "":
		return cmdline.Usagef("「%s」的編號由核心發（%s<數字>），不能給 --id", k.Kind, k.Prefix)
	case k.IDs == "given" && id == "":
		return cmdline.Usagef("「%s」的編號要自己給，請加 --id（例：--id 2026-10-25）", k.Kind)
	}
	obj, err := readObject(e, o.vals["json"])
	if err != nil {
		return err
	}
	if err := checkKnownFields(k, obj); err != nil {
		return err
	}
	// 缺必填欄位先擋下（不要先領掉一個編號才發現不能存）；型別對不對由核心檢查。
	for _, f := range k.Fields {
		if v, ok := obj[f.Name]; f.Required && (!ok || v == nil || v == "") {
			return fmt.Errorf("欄位「%s」是必填（%s）", f.Name, orDash(f.Description))
		}
	}
	body := ""
	if p := o.vals["body-file"]; p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("讀不了 --body-file 指的檔案：%s", p)
		}
		body = string(b)
	}
	if k.IDs == "seq" {
		n, err := sdk.NewVaultClient(e.Client).NextId(e.Ctx, &sdk.VaultNextIdRequest{Kind: k.Kind})
		if err != nil {
			return err
		}
		id = n.ID
	}
	if _, err := sdk.NewVaultClient(e.Client).Put(e.Ctx, &sdk.VaultPutRequest{Record: &sdk.Record{Kind: k.Kind, ID: id, Fields: obj, Body: body}, By: &sdk.Actor{Name: e.By}}); err != nil {
		return fieldError(err)
	}
	_, err = fmt.Fprintln(e.Out, id)
	return err
}

// data set <種類> <編號> --json '<物件>'
func dataSet(e *cmdline.Env, args []string) error {
	const usage = "evergenie data set <種類> <編號> --json '<物件>'"
	o, err := parseOpts(args, []string{"json"}, nil, usage)
	if err != nil || len(o.pos) != 2 || o.vals["json"] == "" {
		return cmdline.Usagef("用法：%s", usage)
	}
	k, err := findKind(e, o.pos[0])
	if err != nil {
		return err
	}
	obj, err := readObject(e, o.vals["json"])
	if err != nil {
		return err
	}
	if err := checkKnownFields(k, obj); err != nil {
		return err
	}
	set, unset := map[string]any{}, []string(nil)
	for name, v := range obj {
		if v == nil {
			unset = append(unset, name)
		} else {
			set[name] = v
		}
	}
	sort.Strings(unset)
	if len(set)+len(unset) == 0 {
		return cmdline.Usagef("--json 沒有要改的欄位")
	}
	if _, err := sdk.NewVaultClient(e.Client).Patch(e.Ctx, &sdk.VaultPatchRequest{Kind: k.Kind, ID: o.pos[1], Set: set, Unset: unset, By: &sdk.Actor{Name: e.By}}); err != nil {
		return fieldError(err)
	}
	_, err = fmt.Fprintln(e.Out, o.pos[1])
	return err
}

// data rm <種類> <編號>：封存，不刪檔。
func dataRm(e *cmdline.Env, args []string) error {
	const usage = "evergenie data rm <種類> <編號>"
	o, err := parseOpts(args, nil, nil, usage)
	if err != nil || len(o.pos) != 2 {
		return cmdline.Usagef("用法：%s", usage)
	}
	k, err := findKind(e, o.pos[0])
	if err != nil {
		return err
	}
	if _, err := sdk.NewVaultClient(e.Client).Archive(e.Ctx, &sdk.VaultArchiveRequest{Kind: k.Kind, ID: o.pos[1], By: &sdk.Actor{Name: e.By}}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已封存 %s（沒有刪檔，資料還在 vault 的 archive/ 裡）\n", o.pos[1])
	return err
}

// personaDeclLine 是「角色改代號時怎麼跟著走」（PKG-64）的白話；沒宣告回空字串。
func personaDeclLine(k sdk.DataKindDecl) string {
	var parts []string
	switch k.PersonaID {
	case PersonaIDExact:
		parts = append(parts, "編號就是角色代號")
	case PersonaIDPrefix:
		parts = append(parts, "編號是 <角色代號>-<其他>")
	}
	if k.PersonaField != "" {
		parts = append(parts, "欄位 "+k.PersonaField+" 是角色代號")
	}
	if len(k.PersonaPrefixed) > 0 {
		parts = append(parts, "欄位 "+strings.Join(k.PersonaPrefixed, "、")+" 是 <角色代號>-<其他>")
	}
	if k.PersonaSource != "" {
		parts = append(parts, "欄位 "+k.PersonaSource+" 說圖在官方位置或 mine")
	}
	return strings.Join(parts, "；")
}
