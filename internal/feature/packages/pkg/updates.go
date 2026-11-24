package pkg

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// ListUpdates 比較 fork 的基準內容（.fork-base/）跟現在的官方套件，逐檔列出官方改了什麼，
// 並寫一份給 AI 讀的差異清單檔。核心不呼叫 AI、也不合併（PKG-N1）：整理成白話、逐項問使用者是秘書的事。
func (m *Module) ListUpdates(ctx context.Context, req *sdk.PackageListUpdatesRequest) (*sdk.PackageListUpdatesResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.readyLocked(); err != nil {
		return nil, err
	}
	e := m.entries[req.ID]
	if e == nil || e.tier != sdk.TierMine || e.man == nil || e.man.Fork == nil {
		return nil, platform.NotFound(fmt.Sprintf("「%s」不是 fork 的套件", req.ID))
	}
	base := filepath.Join(e.dir, forkBaseDir)
	if !isDir(base) {
		return nil, platform.Conflict(fmt.Sprintf("fork 的基準內容不見了（packages/mine/%s/%s），沒辦法比較", req.ID, forkBaseDir))
	}
	resp := &sdk.PackageListUpdatesResponse{BaseVersion: e.man.Fork.BaseVersion}
	latestDir := filepath.Join(m.officialDir(), e.man.Fork.Of)
	latest, err := ParseManifest(latestDir, sdk.TierOfficial)
	if err != nil {
		return resp, nil // 官方套件不在了（或壞了）：fork 照常可用，只是不再有更新
	}
	resp.LatestVersion = latest.Version
	items, err := compareTrees(base, latestDir, e.dir)
	if err != nil {
		return nil, platform.Unavailable("讀不了套件的檔案")
	}
	resp.Items = items
	if len(items) == 0 {
		return resp, nil
	}
	full := platform.RuntimeDir(m.home, "packages", req.ID, "updates", fmt.Sprintf("%s-to-%s.md", resp.BaseVersion, resp.LatestVersion))
	rel := full // 第 2 版暫存在家外面：給完整路徑
	if r, err := filepath.Rel(m.home, full); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		rel = r
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, platform.Unavailable("無法寫入差異清單檔")
	}
	if err := platform.AtomicWrite(full, []byte(renderReport(e, resp)), 0o644); err != nil {
		return nil, platform.Unavailable("無法寫入差異清單檔")
	}
	resp.ReportPath = filepath.ToSlash(rel)
	return resp, nil
}

// readTree 讀資料夾裡每個一般檔案（相對路徑 → 內容）；略過 .git、fork 基準、符號連結與 .DS_Store。
func readTree(root string) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (d.Name() == ".git" || d.Name() == forkBaseDir) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || d.Name() == ".DS_Store" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = b
		return nil
	})
	if os.IsNotExist(err) {
		return out, nil
	}
	return out, err
}

// compareTrees：base＝fork 當時的官方、latest＝現在的官方、fork＝使用者的 fork。一個檔一項。
func compareTrees(baseDir, latestDir, forkDir string) ([]sdk.UpdateItem, error) {
	base, err := readTree(baseDir)
	if err != nil {
		return nil, err
	}
	latest, err := readTree(latestDir)
	if err != nil {
		return nil, err
	}
	fork, err := readTree(forkDir)
	if err != nil {
		return nil, err
	}
	if b, ok := fork["package.toml"]; ok {
		fork["package.toml"] = stripFork(b)
	}
	paths := map[string]bool{}
	for p := range base {
		paths[p] = true
	}
	for p := range latest {
		paths[p] = true
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	var items []sdk.UpdateItem
	for _, p := range sorted {
		b, inBase := base[p]
		n, inLatest := latest[p]
		if inBase && inLatest && sameText(b, n) {
			continue
		}
		f, inFork := fork[p]
		it := sdk.UpdateItem{Path: p}
		switch {
		case !inBase:
			it.Summary = fmt.Sprintf("官方新增了 %s", p)
			it.ConflictsWithFork = inFork && !sameText(f, n)
		case !inLatest:
			it.Summary = fmt.Sprintf("官方刪掉了 %s", p)
			it.ConflictsWithFork = !inFork || !sameText(f, b)
		default:
			it.Summary = fmt.Sprintf("官方改了 %s", p)
			if p == "package.toml" {
				if s := manifestChanges(b, n); s != "" {
					it.Summary += "：" + s
				}
			}
			it.ConflictsWithFork = !inFork || !sameText(f, b)
		}
		it.Diff = lineDiff(b, n)
		items = append(items, it)
	}
	return items, nil
}

func sameText(a, b []byte) bool { return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b)) }

// stripFork 拿掉 package.toml 的 [fork] 區段（fork 工具自己加的，不算使用者的改動）。
func stripFork(b []byte) []byte {
	var out []string
	skip := false
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			skip = t == "[fork]"
		}
		if !skip {
			out = append(out, line)
		}
	}
	return []byte(strings.Join(out, "\n"))
}

// manifestChanges 用白話寫 package.toml 的版本與擴充點增減。
func manifestChanges(oldB, newB []byte) string {
	var a, b Manifest
	if _, err := toml.Decode(string(oldB), &a); err != nil {
		return ""
	}
	if _, err := toml.Decode(string(newB), &b); err != nil {
		return ""
	}
	var parts []string
	if a.Version != b.Version {
		parts = append(parts, fmt.Sprintf("版本 %s → %s", a.Version, b.Version))
	}
	had, has := map[string]bool{}, map[string]bool{}
	for _, x := range a.Extension {
		had[x.ID] = true
	}
	for _, x := range b.Extension {
		has[x.ID] = true
		if !had[x.ID] {
			parts = append(parts, "新增擴充點 "+x.ID)
		}
	}
	for _, x := range a.Extension {
		if !has[x.ID] {
			parts = append(parts, "拿掉擴充點 "+x.ID+"（你設定過的值會保留，但沒有作用）")
		}
	}
	return strings.Join(parts, "；")
}

const (
	diffContext  = 2
	diffMaxLines = 120
	diffMaxCells = 4_000_000 // 兩個檔的行數相乘超過這個就不算逐行差異
)

// lineDiff 是逐行差異（前後各 2 行，太長截斷）；二進位檔只寫「內容不同」。
func lineDiff(a, b []byte) string {
	if isBinary(a) || isBinary(b) {
		return "（二進位檔，內容不同）"
	}
	x, y := splitLines(a), splitLines(b)
	if len(x)*len(y) > diffMaxCells {
		return "（檔案太大，不列逐行差異）"
	}
	// LCS 表：lcs[i][j]＝x[i:] 與 y[j:] 的最長共同子序列長度。
	lcs := make([][]int32, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type op struct {
		kind byte // ' '、'-'、'+'
		text string
	}
	var ops []op
	i, j := 0, 0
	for i < len(x) || j < len(y) {
		switch {
		case i < len(x) && j < len(y) && x[i] == y[j]:
			ops = append(ops, op{' ', x[i]})
			i, j = i+1, j+1
		case j < len(y) && (i == len(x) || lcs[i][j+1] >= lcs[i+1][j]):
			ops = append(ops, op{'+', y[j]})
			j++
		default:
			ops = append(ops, op{'-', x[i]})
			i++
		}
	}
	keep := make([]bool, len(ops))
	for k, o := range ops {
		if o.kind == ' ' {
			continue
		}
		for c := max(0, k-diffContext); c <= min(len(ops)-1, k+diffContext); c++ {
			keep[c] = true
		}
	}
	var lines []string
	gap := false
	for k, o := range ops {
		if !keep[k] {
			gap = len(lines) > 0
			continue
		}
		if gap {
			lines = append(lines, "…")
			gap = false
		}
		lines = append(lines, string(o.kind)+" "+o.text)
	}
	if len(lines) > diffMaxLines {
		rest := len(lines) - diffMaxLines
		lines = append(lines[:diffMaxLines], fmt.Sprintf("…（後面還有 %d 行，沒有列出）", rest))
	}
	return strings.Join(lines, "\n")
}

func splitLines(b []byte) []string {
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func isBinary(b []byte) bool { return bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) }

// renderReport 是寫給 AI（秘書）讀的差異清單：開頭說明怎麼用，接著一項一項。
func renderReport(e *entry, r *sdk.PackageListUpdatesResponse) string {
	var b strings.Builder
	conflicts := 0
	for _, it := range r.Items {
		if it.ConflictsWithFork {
			conflicts++
		}
	}
	fmt.Fprintf(&b, "# 套件「%s」官方更新：%s → %s\n\n", e.man.Name, r.BaseVersion, r.LatestVersion)
	fmt.Fprintf(&b, "> 給 AI：這份是核心產生的差異清單，核心不會自己合併。請把每一項整理成白話（官方改了什麼、對使用者有什麼影響、跟他自己改過的地方有沒有衝突），一項一項問使用者要不要合進他的 fork（`packages/mine/%s/`）。使用者說要，才改 `packages/mine/%s/` 裡的檔案；不要改官方資料夾，也不要改 `[fork]` 與 `%s/`。\n\n", e.id, e.id, forkBaseDir)
	fmt.Fprintf(&b, "- 套件：`%s`（fork 自官方 `%s`）\n- 基準版本：%s\n- 官方新版：%s\n- 項目：%d 項（其中 %d 項使用者在 fork 裡也改過）\n", e.id, e.man.Fork.Of, r.BaseVersion, r.LatestVersion, len(r.Items), conflicts)
	for i, it := range r.Items {
		fmt.Fprintf(&b, "\n## %d. %s\n\n- 檔案：`%s`\n", i+1, it.Summary, it.Path)
		if it.ConflictsWithFork {
			b.WriteString("- 使用者在 fork 裡也改過這個檔：合併前先給他看兩邊的差別\n")
		} else {
			b.WriteString("- 使用者沒有改過這個檔\n")
		}
		if it.Diff != "" {
			fmt.Fprintf(&b, "\n~~~~diff\n%s\n~~~~\n", it.Diff)
		}
	}
	return b.String()
}
