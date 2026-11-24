package vault

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// vaultRoots 是 vault/ 底下核心自己管的最上層名字；其他的都是主人自己放的東西。
// 註冊紀錄種類時自己指定路徑的（Spec.Path，例如規矩的版本放 vault/rules/）也要在這裡：RegisterSchema 會檢查，
// 用到清單外的資料夾直接 panic（啟動與測試馬上看得到），免得那個資料夾被當成主人的東西略過、重開後紀錄全不見。
// 不做成「註冊時自動加」：有的模組在資料庫打開、掃描完之後才註冊，那時候加已經來不及。
var vaultRoots = map[string]bool{
	"agents": true, "goals": true, "messages": true, "tasks": true, "reminders": true,
	"jobs": true, "policy": true, "rules": true, "pkg": true, "profile.md": true,
	"notes": true, // 全家共用的主題筆記（DEV-161，schemas/note.md）
}

// IndexFileName 是核心產生的索引檔名（總索引 vault/notes/索引.md、職位索引 vault/agents/<職位>/索引.md，DEV-161）。
// 它們是照主題筆記的檔頭算出來的，不是紀錄：不進索引、不監看、不算壞檔（notOurs），由名冊整份重產。
const IndexFileName = "索引.md"

// generatedIndex：rel（相對於 vault/）是不是核心產生的索引檔。
func generatedIndex(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	switch {
	case len(parts) == 2 && parts[0] == "notes" && parts[1] == IndexFileName:
		return true
	case len(parts) == 3 && parts[0] == "agents" && parts[2] == IndexFileName:
		return true
	}
	return false
}

// OwnerNoteFile：vault/notes/ 底下的這個檔（rel 相對於 vault/，r 與 decodeErr 是 Decode 的結果）是不是主人自己的東西
// （DEV-154 原則「不認得的不掃、不碰、不擋」，DEV-161）：檔頭讀不懂、或沒有自稱是主題筆記（有 name 欄位）或分類說明
// （kind: category）的，一律當主人自己的筆記，不進索引、不算壞檔。自稱是主題、欄位卻壞掉的照舊算壞檔讓 doctor 報。
func OwnerNoteFile(rel string, r Record, decodeErr error) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 || parts[0] != "notes" {
		return false
	}
	if decodeErr != nil {
		return true
	}
	if _, ok := r.Fields["name"]; ok {
		return false
	}
	return r.Fields["kind"] != "category"
}

// specRoot 回一個紀錄種類存放路徑的最上層名字（vault/<這個>/…）；沒有自訂路徑或路徑不在 vault/ 底下回空字串。
func specRoot(sc Spec) string {
	if sc.Path == nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(sc.Path("x"))), "/")
	if len(parts) < 2 || parts[0] != "vault" {
		return ""
	}
	return parts[1]
}

// notOurs：這個路徑（相對於 vault/）不是核心管的，不進索引、不監看、也不算壞檔（DEV-154 原則：
// 家裡都是主人個人的，系統不認得的東西不掃、不碰、不擋升級）。三種：
//   - 最上層不是核心的資料夾（主人自己的筆記、整份備份 pkg.bak-…）；
//   - 任何一層是隱藏的（`.` 開頭，例 .obsidian、.trash）；
//   - 任何一層是備份（名字含 `.bak-`，備份規則 <原名>.bak-<日期>-<原因>）；
//   - 核心產生的索引檔（generatedIndex：不是紀錄，照主題筆記重產）。
//
// 核心資料夾裡面格式不對的檔案照舊標成壞檔讓 doctor 報告（VFS-8），不在這裡略過。
func notOurs(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 || parts[0] == "." || parts[0] == "" {
		return false
	}
	if !vaultRoots[parts[0]] || generatedIndex(rel) {
		return true
	}
	for _, p := range parts {
		if strings.HasPrefix(p, ".") || strings.Contains(p, ".bak-") {
			return true
		}
	}
	return false
}

// headerlessOwnNote：vault/agents/<職位>/notes/<名字>.md（rel 相對於 vault/，含封存的 archive/<月>/）要是沒有檔頭
// （檔案第一行不是 ---，例：秘書或主人直接用編輯器寫的），不擋、不算壞檔：整份當內文，欄位照檔案時間補——
// updated_at、created_at 是檔案修改時間，by 是 external，agent 是職位——讀得到、搜得到；下一次經核心寫回（evergenie memory note write）
// 才補上真正的檔頭（DEV-161）。檔頭有但壞掉的不在這裡處理，照舊是壞檔。
func headerlessOwnNote(rel string, data []byte, mtime time.Time) (Record, bool) {
	agent, name, archived, ok := ownNoteRel(rel)
	if !ok || hasFrontmatter(data) {
		return Record{}, false
	}
	at := mtime.Format(time.RFC3339Nano)
	return Record{Kind: "note", ID: agent + "/" + name, Archived: archived,
		Fields:   map[string]any{"schema": 1, "updated_at": at, "created_at": at, "by": "external", "agent": agent},
		Body:     normalizedBody(string(data)),
		Revision: revision(data), UpdatedAt: mtime}, true
}

// ownNoteRel：rel（相對於 vault/）是不是秘書自己的做事筆記檔 agents/<職位>/notes/<名字>.md（含 notes/archive/<月>/ 的封存）。
func ownNoteRel(rel string) (agent, name string, archived, ok bool) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 4 || parts[0] != "agents" || parts[2] != "notes" || !strings.HasSuffix(parts[len(parts)-1], ".md") {
		return "", "", false, false
	}
	// 子資料夾照舊認（例：life 的 briefings/、reflections/，id 是 <職位>/<子資料夾>/<名字>，DEV-161 前就這樣）；archive/<月>/ 是封存。
	rest := parts[3:]
	if len(rest) == 3 && rest[0] == "archive" {
		return parts[1], strings.TrimSuffix(rest[2], ".md"), true, true
	}
	return parts[1], strings.TrimSuffix(strings.Join(rest, "/"), ".md"), false, true
}

// hasFrontmatter：檔案第一行是 ---（有開檔頭；有沒有寫對由 Decode 判斷）。
func hasFrontmatter(data []byte) bool {
	first, _, _ := strings.Cut(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	return first == "---"
}

// HeaderlessOwnNote 給健檢用：這個檔（rel 相對於 vault/）是不是沒有檔頭的做事筆記。
func HeaderlessOwnNote(rel string, data []byte) bool {
	_, _, _, ok := ownNoteRel(rel)
	return ok && !hasFrontmatter(data)
}

// decodeIndexed 是索引與監看讀檔的方式：一般檔照 Decode；沒有檔頭的做事筆記補欄位當內文（headerlessOwnNote）。
func decodeIndexed(rel, path string, data []byte) (Record, []string, error) {
	if _, _, _, ok := ownNoteRel(rel); ok && !hasFrontmatter(data) {
		if info, err := os.Stat(path); err == nil {
			if r, ok := headerlessOwnNote(rel, data, info.ModTime()); ok {
				return r, nil, nil
			}
		}
	}
	return Decode(data)
}

// notOursAbs 同 notOurs，吃絕對路徑。
func (m *Module) notOursAbs(path string) bool {
	rel, err := filepath.Rel(filepath.Join(m.home, "vault"), path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	return notOurs(rel)
}

// CorePath：相對於家的路徑 rel（斜線分隔）是不是 vault/ 裡核心管的東西（核心資料夾底下、不是隱藏的也不是備份；
// 加上編號檔 vault/.counters.toml）。升級退回時「資料回到升級前」只動這些（update 模組用）：主人自己放的東西不碰。
func CorePath(rel string) bool {
	rest, ok := strings.CutPrefix(filepath.ToSlash(rel), "vault/")
	if !ok || rest == "" {
		return false
	}
	return rest == ".counters.toml" || !notOurs(rest)
}
