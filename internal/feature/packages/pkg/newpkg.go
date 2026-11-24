package pkg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 使用者自己的套件（SEC-302、DEV-40、REQ-306～312）：`packages new <id>` 在 packages/mine/<id>/ 建好鷹架。
// 資料夾地圖、保留字清單的說明在 ai_docs/conventions/user-features.md；這裡的 ReservedIDs 是它的程式版，改一邊要改另一邊（TestReservedIDsListedInDocs）。

// newIDPattern 是新職位 id 的格式（U-5）：比 manifest 的 idPattern 多限長度 2～31（新套件的套件 id 同時是職位 id）。
var newIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)

// ReservedIDs 是不能拿來當新職位 id 的字：核心網址的第一段、核心內建的名字、家裡的資料夾名、套件層名稱、秘書名冊裡的職位。
// 官方套件的 id（body、closet、personas、health…）另外由「撞官方名」擋，不必列在這裡。
var ReservedIDs = append(append([]string(nil), CoreRouteSegments...),
	// 核心與家的名字
	"agents", "bus", platform.ProductName, "node", "notify", "runtime", "scheduler", "sdk", "session", "vault",
	// 套件層與套件資料夾
	"installed", "mine", "official", "package", "packages", "persona",
	// 預設的職位（秘書名冊）
	"art", "code", "life", "system", "work",
	// 內建官方套件的名字（就算官方資料夾這台沒有也不給用）
	"body", "closet", "closet-mine", "fitness", "habits", "health", "nutrition", "secretaries",
)

// CoreRouteSegments 是看板核心網址與選單的第一段（internal/feature/inbox/menu 的核心項目與 internal/base/app/board 直接處理的前綴；ReservedIDs 的第一組）。套件頁面的 route 不能用這些字開頭
// （manifest 驗證拒絕，REQ-352），新職位 id 也不能用（REQ-309）。看板網址 "/" 本身另外擋。
var CoreRouteSegments = []string{
	"admin", "api", "assets", "avatars", "chat", "core", "goals", "home", "login", "messages", "overview",
	"p", "personas", "pkg-bundles", "pkg-files", "policy", "reminders", "settings", "share", "static", "tasks", "upload",
}

// shadowsCoreRoute：頁面宣告的 route 會不會蓋住核心網址（"/" 或第一段是 CoreRouteSegments）。
func shadowsCoreRoute(route string) bool {
	r := strings.TrimSpace(route)
	if r == "" {
		return false
	}
	seg, _, _ := strings.Cut(strings.TrimPrefix(r, "/"), "/")
	if seg == "" {
		return true
	}
	for _, c := range CoreRouteSegments {
		if strings.EqualFold(seg, c) {
			return true
		}
	}
	return false
}

func reserved(id string) bool {
	for _, r := range ReservedIDs {
		if r == id {
			return true
		}
	}
	return false
}

// New 建一個空白的使用者套件。全部先寫在 packages/mine/.new-<亂數>/<id>/，ParseManifest 驗過才 rename 成 packages/mine/<id>/；失敗不留任何東西。
// 只寫 mine 層：官方、分享層的資料夾不碰（PKG-N2），官方同名直接拒絕。
func (m *Module) New(ctx context.Context, req *sdk.PackageNewRequest) (*sdk.PackageNewResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if actor(req.By) == "" {
		return nil, platform.Invalid("by", "請提供操作者")
	}
	id := req.ID
	if reserved(id) {
		return nil, platform.Invalid("id", fmt.Sprintf("「%s」是系統保留的名字，換一個（保留字清單：evergenie docs show conventions/user-features.md 的「保留字」）", id))
	}
	if !newIDPattern.MatchString(id) {
		return nil, platform.Invalid("id", fmt.Sprintf("套件的 id「%s」不能用：要用小寫英文開頭，後面是小寫英文、數字或 -，共 2 到 31 個字（例：garden、my-habit）", id))
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = id
	}
	if utf8.RuneCountInString(name) > 40 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return nil, platform.Invalid("name", "顯示名稱要在 40 個字以內，不能有換行或控制字元")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.readyLocked(); err != nil {
		return nil, err
	}
	if err := m.idTakenLocked(id); err != nil {
		return nil, err
	}
	mineRoot := m.tierDir(sdk.TierMine)
	dest := filepath.Join(mineRoot, id)
	if err := os.MkdirAll(mineRoot, 0o755); err != nil {
		return nil, platform.Unavailable("無法建立 packages/mine/")
	}
	tmpRoot := filepath.Join(mineRoot, ".new-"+shortRand()) // 隱藏資料夾：掃描時略過
	tmp := filepath.Join(tmpRoot, id)
	defer os.RemoveAll(tmpRoot)
	files := scaffoldFilesFn(id, name)
	var rels []string
	for rel, body := range files {
		p := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, platform.Unavailable("無法建立套件資料夾")
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return nil, platform.Unavailable("無法寫入套件資料夾")
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	if _, err := ParseManifest(tmp, sdk.TierMine); err != nil {
		return nil, err // 鷹架自己有問題（不該發生）：白話的 INVALID，不留暫存
	}
	if exists(dest) { // 檢查之後才冒出來（別的行程）：不覆蓋
		return nil, platform.Conflict(fmt.Sprintf("packages/mine/%s 已經存在", id))
	}
	if err := os.Rename(tmp, dest); err != nil {
		return nil, platform.Unavailable("無法放進 packages/mine/")
	}
	now := m.d.Clock.Now()
	m.rescanLocked(now)
	m.markChangedLocked(dest)
	if m.log != nil {
		m.log.Info("建立使用者套件", "id", id, "by", actor(req.By))
	}
	p, err := m.kickLocked(id)
	if err != nil {
		return nil, err
	}
	return &sdk.PackageNewResponse{Package: p, Dir: "packages/mine/" + id, Files: rels}, nil
}

// idTakenLocked：id 已經被任何一層的套件、或任何套件帶來的職位（秘書 id）用掉就回 CONFLICT（白話說是誰）。
func (m *Module) idTakenLocked(id string) error {
	for _, t := range []struct {
		tier sdk.Tier
		msg  string
	}{
		{sdk.TierOfficial, "「%s」是官方套件的名字，新套件不能同名（想改官方套件的行為，請用擴充點，不要另建同名套件）"},
		{sdk.TierInstalled, "你已經裝了別人分享的「%s」，新套件不能同名（packages/installed/%[1]s）"},
		{sdk.TierMine, "packages/mine/%s 已經存在，不能蓋掉"},
	} {
		if exists(filepath.Join(m.tierDir(t.tier), id)) {
			return platform.Conflict(fmt.Sprintf(t.msg, id))
		}
	}
	for _, e := range m.entries {
		if e.id == id {
			return platform.Conflict(fmt.Sprintf("已經有一個叫「%s」的套件", id))
		}
		if e.man == nil {
			continue
		}
		for _, a := range e.man.Contributes.Agent {
			if a.ID == id {
				return platform.Conflict(fmt.Sprintf("「%s」已經是套件「%s」帶來的職位名字，新套件不能同名", id, e.id))
			}
		}
	}
	return nil
}

// scaffoldFilesFn 讓測試換掉鷹架內容（模擬驗不過）。
var scaffoldFilesFn = scaffoldFiles

// scaffoldFiles 是新套件的檔案（相對路徑 → 內容）。名字只放進引號字串與純文字，不拼進程式碼結構。
func scaffoldFiles(id, name string) map[string]string {
	q := strconv.Quote(name)
	js, _ := json.Marshal(name)
	manifest := fmt.Sprintf(`# 我的套件「%[1]s」（evergenie packages new 建立）。資料夾說明：evergenie docs show conventions/user-features.md
schema = 1
id = %[2]q
name = %[3]s
version = "0.1.0"
description = %[4]s
kind = "data"
api = "evergenie.v1"

# 擔任這個職位的秘書：職責寫在 agents/%[2]s/agent.md；用 evergenie packages sync %[2]s 預演、主人同意後 --apply 才會真的建出來。
[[contributes.agent]]
id = %[2]q
template = "agents/%[2]s/agent.md"

# 頁面的標準網址是 /p/%[2]s/index；選單放哪裡由使用者在「設定 → 選單」決定。
[[contributes.page]]
id = "index"
title = %[3]s
component = "pages/index.tsx"

[[contributes.menu]]
id = "index"
title = %[3]s
icon = "puzzle"
order = 100
page = "index"
`, name, id, q, strconv.Quote("我的套件："+name))
	agent := fmt.Sprintf(`---
duty: %s
---

# %s 的職責

你擔任「%s」這個職位。把它要幫主人做的事寫在這裡：記什麼、什麼時候提醒、怎麼回答、不做什麼。

- 這份檔案是範本，請依職責改寫。
- 資料用 evergenie data 讀寫；先在 schemas/ 宣告資料種類。
`, strconv.Quote("負責「"+name+"」（請改成一句話的職責）"), id, name)
	page := fmt.Sprintf(`// 「%s」的頁面。標準網址 /p/%s/index；可以 import react、@evergenie/ui、@evergenie/sdk-web，以及這個資料夾裡的檔案。
const TITLE = %s;

export default function Index() {
  return (
    <section className="flex flex-col gap-2 p-4" data-testid="%s-index">
      <h1 className="text-xl font-semibold">{TITLE}</h1>
      <p className="text-sm text-muted">這是新套件的第一頁，請請你的秘書幫你改成想要的樣子。</p>
    </section>
  );
}
`, strings.NewReplacer("\n", " ", "\r", " ").Replace(name), id, js, id)
	readme := fmt.Sprintf(`# %s

這是你自己的套件（id：`+"`%s`"+`），放在 `+"`packages/mine/%s/`"+`。

| 位置 | 是什麼 |
|---|---|
| `+"`package.toml`"+` | 套件的說明書（名字、版本、頁面、選單、資料種類） |
| `+"`agents/%s/agent.md`"+` | 擔任這個職位的秘書的職責 |
| `+"`personas/`"+` | 這個套件自己帶的角色（分享時會一起帶走） |
| `+"`pages/`"+` | 頁面（`+"`index.tsx`"+` 的網址是 /p/%s/index） |
| `+"`schemas/`"+` | 這個套件的資料欄位規格 |

你的資料不在這個資料夾，在家裡的 `+"`vault/pkg/%s/`"+`；分享這個資料夾不會帶走你的資料。
要改這個套件，跟秘書說就好；詳細規則秘書會自己讀（`+"`evergenie docs show conventions/user-features.md`"+`，欄位格式 `+"`evergenie docs show schemas/package.md`"+`）。
`, name, id, id, id, id, id)
	return map[string]string{
		"package.toml":               manifest,
		"agents/" + id + "/agent.md": agent,
		"pages/index.tsx":            page,
		"personas/.gitkeep":          "",
		"schemas/.gitkeep":           "",
		"README.md":                  readme,
	}
}
