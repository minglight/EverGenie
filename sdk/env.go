package sdk

import (
	"os"
	"strings"
)

// 環境變數（DEV-158）：程式與腳本只寫新名字 EVERGENIE_…；讀一律經 Getenv（引擎套件這種收 getenv 函式的，用 EnvLookup 包一層）。
// 過渡期新名字沒設時讀改名前的舊名（已經開著的 session、舊版服務開的行程還帶著它），舊名只在這個檔出現；
// 等已經裝好的家都搬完就拿掉 legacyEnvPrefix 與它的分支。清環境（拿掉屬於別的家、別的 session 的變數）時新舊兩種一起清：IsEnv、IsProductEnv、EnvPrefixes。

// EnvPrefix 是 EverGenie 環境變數的字首（EVERGENIE_HOME、EVERGENIE_API_SOCKET…）。
const EnvPrefix = "EVERGENIE_"

const legacyEnvPrefix = "HG_" // 讀舊名（legacy）：改名前的字首

// LegacyEnvName 回新名字 name（EVERGENIE_…）的舊名（讀舊名，legacy）；name 不是 EVERGENIE_ 開頭就回空字串。
func LegacyEnvName(name string) string {
	if rest, ok := strings.CutPrefix(name, EnvPrefix); ok && rest != "" {
		return legacyEnvPrefix + rest
	}
	return ""
}

// CurrentEnvName 把舊名（legacy）換成新名字；其他名字原樣回傳。過濾環境、轉給子行程時用，子行程只看到新名字。
func CurrentEnvName(key string) string {
	if rest, ok := strings.CutPrefix(key, legacyEnvPrefix); ok && rest != "" {
		return EnvPrefix + rest
	}
	return key
}

// IsEnv 回環境變數 key 是不是 name 這一個（name 寫新名字；key 是新名字或它的舊名都算）。
func IsEnv(key, name string) bool {
	return key == name || (LegacyEnvName(name) != "" && key == LegacyEnvName(name))
}

// IsProductEnv 回 key 是不是 EverGenie 的環境變數（EVERGENIE_ 開頭，或舊名 legacy）：測試與腳本清環境時用。
func IsProductEnv(key string) bool {
	return strings.HasPrefix(key, EnvPrefix) || strings.HasPrefix(key, legacyEnvPrefix)
}

// EnvPrefixes 回這些「名字=」或字首（寫新名字）加上各自的舊名（legacy），給「拿掉這些變數」的過濾用。
func EnvPrefixes(prefixes ...string) []string {
	out := append([]string(nil), prefixes...)
	for _, p := range prefixes {
		if l := LegacyEnvName(p); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// EnvLookup 把一個 lookup 函式（例 os.LookupEnv）包成讀 EverGenie 環境變數的 getenv：新名字有設就用它（設成空字串也算有設），
// 沒設才讀舊名（legacy）；不是 EVERGENIE_ 開頭的名字照原樣讀。
func EnvLookup(lookup func(string) (string, bool)) func(string) string {
	return func(name string) string {
		if v, ok := lookup(name); ok {
			return v
		}
		if l := LegacyEnvName(name); l != "" {
			v, _ := lookup(l)
			return v
		}
		return ""
	}
}

// Getenv 讀 EverGenie 的環境變數（name 寫新名字 EVERGENIE_…）；新名字沒設時讀舊名（legacy）。
func Getenv(name string) string { return EnvLookup(os.LookupEnv)(name) }
