// Package attach 是任務單與待辦共用的附件規則（TSK-28、RMD-23、D-61，說明卡 ai_docs/modules/tasks.md）：
// 單上只記 `/share/<名字>` 連結，檔案留在 <家>/runtime/share/（看板上傳、`evergenie share` 都放那裡，看板由 /share/ 伺服、
// 秘書 session 讀得到）；這裡不搬也不刪檔。
package attach

import (
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
)

// Field 是單上存附件的欄位名（任務單、待辦都一樣）。
const Field = "attachments"

// shareHint 是附件連結不對時給的白話：怎麼把檔案放進分享資料夾。
const shareHint = "附件要是這台分享資料夾裡的 /share/<名字> 連結：看板上傳，或先用 evergenie share <完整路徑> 放進去，再用它回的網址"

// Check 驗一串附件連結（field 是錯誤要指的欄位）：只收 `/share/<名字>`（或路徑是它的 http(s) 網址，存成相對連結），名字跟 /share 的伺服規則一樣
// （不含 / \ NUL、..，不以 . 開頭），而且分享資料夾（第 1 版 <家>/runtime/share/、第 2 版 <家>/media/share/）裡的 <名字> 是一般檔案（不是符號連結、資料夾）。
// 回傳去掉前後空白、去掉重複的連結。
func Check(home, field string, refs []string) ([]string, error) {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = normalize(ref)
		name, ok := strings.CutPrefix(ref, "/share/")
		if !ok || name == "" || strings.ContainsAny(name, "/\\\x00") || strings.Contains(name, "..") || strings.HasPrefix(name, ".") {
			return nil, platform.Invalid(field, shareHint)
		}
		info, err := os.Lstat(filepath.Join(platform.SharesDir(home), name))
		if err != nil || !info.Mode().IsRegular() {
			return nil, platform.Invalid(field, "找不到附件 "+ref+"。"+shareHint)
		}
		if !slices.Contains(out, ref) {
			out = append(out, ref)
		}
	}
	return out, nil
}

// normalize 去掉前後空白；`evergenie share` 有設對外網址時印的是完整網址（https://…/share/<名字>）：收成這台的相對連結，看板照台補網址。
func normalize(ref string) string {
	ref = strings.TrimSpace(ref)
	if u, err := url.Parse(ref); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.RawQuery == "" && u.Fragment == "" {
		return u.Path
	}
	return ref
}

// StringList 讀紀錄上的字串清單欄位（vault 讀回來是 []any，剛寫的是 []string）。
func StringList(r vault.Record, key string) []string {
	var all []string
	switch v := r.Fields[key].(type) {
	case []string:
		all = append(all, v...)
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				all = append(all, s)
			}
		}
	}
	return all
}

// Set 照順序把 add 接在紀錄的附件後面（已經有的略過），再拿掉 remove（完整網址也認）；最後是空的就不留這個欄位。
func Set(r *vault.Record, add, remove []string) {
	if len(add) == 0 && len(remove) == 0 {
		return
	}
	all := StringList(*r, Field)
	for _, a := range add {
		if !slices.Contains(all, a) {
			all = append(all, a)
		}
	}
	drop := make([]string, len(remove))
	for i, x := range remove {
		drop[i] = normalize(x)
	}
	all = slices.DeleteFunc(all, func(a string) bool { return slices.Contains(drop, a) })
	if len(all) == 0 {
		delete(r.Fields, Field)
		return
	}
	r.Fields[Field] = all
}

// Lines 是交給秘書的文字裡的附件段（任務單的第一句、session 的 readWork、待辦提醒用同一個格式）；沒有附件是空字串。
func Lines(refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	return "\n附件：\n- " + strings.Join(refs, "\n- ")
}
