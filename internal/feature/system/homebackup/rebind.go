package homebackup

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 還原時改寫跟機器綁死的設定（HBK-7）。原則：**綁機器的設定用「要還原進去的這個家」現在的值，其他全部用備份的**。
// 從備份建新家也是同一條路：先照一般方式建一個空的新家（看板的埠已經挑過沒被占用的、佈局是這台的），再把備份還原進去。

// machineKeys 是 home.toml 裡綁機器的設定（點分隔的路徑）：
//   - layout：佈局與狀態資料夾（絕對路徑、跟使用者名稱有關）
//   - secrets.file：秘密檔的位置
//   - web.port：看板的埠（新電腦上可能被占用）
//   - web.public_url、web.service：這台電腦的 Tailscale 網址與服務名（跟著機器；新電腦要重新設定手機連回家）
//   - runtime：整段都是「這個家在這台電腦上的身分」（電腦代號、開機服務名、看板的埠、正式家或開發家、沙盒，
//     以及核心不解讀、原樣交給引擎的欄位，例如引擎跟別的家分開用的 socket 名）。整段用這個家現在的，
//     不然把別的家的備份還原進來，會跟那個家搶同一個服務名與對話。
//   - update.core：程式放在哪
var machineKeys = []string{
	"layout", "secrets.file", "web.port", "web.public_url", "web.service", "runtime", "update.core",
}

// rebindPaths：做備份那台與這台的家、使用者資料夾（絕對路徑）。Old 開頭的字串換成 New 開頭。
type rebindPaths struct {
	OldHome, NewHome string
	OldUser, NewUser string
	// DefaultSecrets：備份帶著推播金鑰、但這個家還沒設秘密檔的位置時，補上這個值（secrets/push.toml，兩種佈局都對得到）。
	DefaultSecrets string
}

// rebindHomeToml 回還原後要用的 home.toml：備份的內容，綁機器的設定換成 dest（這個家現在的 home.toml）的值（dest 沒有的就拿掉），
// 其他字串裡寫死的舊家、舊使用者資料夾路徑換成這台的。changed 是白話的改動清單（給日誌與結果）。
func rebindHomeToml(backup, dest []byte, p rebindPaths) (out []byte, changed []string, err error) {
	src := map[string]any{}
	if _, err := toml.Decode(string(backup), &src); err != nil {
		return nil, nil, platform.Invalid("backup", "備份裡的家設定讀不懂，不能還原")
	}
	cur := map[string]any{}
	if _, err := toml.Decode(string(dest), &cur); err != nil {
		return nil, nil, platform.Conflict("這個家現在的設定檔（home.toml）讀不懂，沒辦法自動還原；跟任何一位秘書說「家的設定檔讀不懂，幫我看」")
	}
	if n := rewritePaths(src, p); n > 0 {
		changed = append(changed, "設定裡寫死的資料夾位置改成這台電腦的")
	}
	for _, key := range machineKeys {
		parts := strings.Split(key, ".")
		old, had := lookup(src, parts)
		now, has := lookup(cur, parts)
		switch {
		case has:
			set(src, parts, now)
		case had:
			remove(src, parts)
		}
		if had != has || (had && !sameValue(old, now)) {
			changed = append(changed, machineKeyLabel(key))
		}
	}
	if _, has := lookup(src, []string{"secrets", "file"}); !has && p.DefaultSecrets != "" {
		set(src, []string{"secrets", "file"}, p.DefaultSecrets)
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(src); err != nil {
		return nil, nil, platform.Internal("寫不出還原後的家設定")
	}
	return buf.Bytes(), changed, nil
}

func machineKeyLabel(key string) string {
	switch key {
	case "layout":
		return "狀態資料夾的位置用這台電腦的"
	case "secrets.file":
		return "秘密檔的位置用這台電腦的"
	case "web.port":
		return "看板的埠用這台電腦的"
	case "web.public_url", "web.service":
		return "手機連回家的網址用這台電腦的（沒設過就要重新設定）"
	case "update.core":
		return "程式的位置用這台電腦的"
	}
	return "這個家在這台電腦上的身分（電腦代號、開機服務名、看板的埠）用這台電腦的"
}

func lookup(t map[string]any, parts []string) (any, bool) {
	var cur any = t
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[p]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func set(t map[string]any, parts []string, v any) {
	for _, p := range parts[:len(parts)-1] {
		next, ok := t[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			t[p] = next
		}
		t = next
	}
	t[parts[len(parts)-1]] = v
}

// remove 拿掉一個設定；它所在的表格因此空了就一起拿掉。
func remove(t map[string]any, parts []string) {
	if len(parts) == 1 {
		delete(t, parts[0])
		return
	}
	next, ok := t[parts[0]].(map[string]any)
	if !ok {
		return
	}
	remove(next, parts[1:])
	if len(next) == 0 {
		delete(t, parts[0])
	}
}

func sameValue(a, b any) bool {
	x, err1 := platform.RenderValue(a)
	y, err2 := platform.RenderValue(b)
	return err1 == nil && err2 == nil && x == y
}

// rewritePaths 把表格裡所有字串開頭的舊路徑換成新的（先比家、再比使用者資料夾：家通常在使用者資料夾底下），回改了幾處。
func rewritePaths(v any, p rebindPaths) int {
	n := 0
	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			if y, ok := rewritePath(x, p); ok {
				n++
				return y
			}
		case map[string]any:
			for k, e := range x {
				x[k] = walk(e)
			}
		case []any:
			for i, e := range x {
				x[i] = walk(e)
			}
		case []map[string]any:
			for _, e := range x {
				walk(e)
			}
		}
		return v
	}
	walk(v)
	return n
}

func rewritePath(s string, p rebindPaths) (string, bool) {
	for _, pair := range [][2]string{{p.OldHome, p.NewHome}, {p.OldUser, p.NewUser}} {
		old, now := filepath.Clean(pair[0]), filepath.Clean(pair[1])
		if pair[0] == "" || pair[1] == "" || old == now || old == "/" {
			continue
		}
		if s == old {
			return now, true
		}
		if strings.HasPrefix(s, old+"/") {
			return now + s[len(old):], true
		}
	}
	return s, false
}
