package host

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 安裝前檢查（Q4，`evergenie preflight`）：這台電腦夠不夠格跑這個家。這裡只檢查「跟平台無關」的（系統、Homebrew、必備工具、要的話 Tailscale）；
// 必備工具的名單由命令列組裝時給（Tools，引擎要用的工具也在裡面）；各帳號的平台專屬項目（程式、登入）由各引擎回報（system.preflight），CLI 合併後一起印。

// Level 是一項檢查的結果等級。
type Level string

const (
	LevelOK   Level = "ok"
	LevelWarn Level = "warn" // 沒有也能跑，但有些事做不了（例如沒有 Tailscale 手機連不到看板）
	LevelBad  Level = "bad"  // 一定要先處理
)

// Check 是一項檢查。
type Check struct {
	ID      string
	Name    string
	Level   Level
	Detail  string
	Fix     string
	Install []string // 不需要密碼的安裝指令；只接受 brew install …
}

// PreflightOptions 是檢查的選項。Need 是額外要求的項目（目前只有 "tailscale"）。
type PreflightOptions struct {
	Need  map[string]bool
	Tools []string // 必備的工具（程式名稱）；每個缺了就是 ✘，Homebrew 有的話給 brew install 的建議
	OS    string   // 空＝runtime.GOOS（測試用）
}

// Preflight 做跟平台無關的檢查。
func Preflight(r Runner, o PreflightOptions) []Check {
	goos := o.OS
	if goos == "" {
		goos = runtime.GOOS
	}
	var out []Check
	if goos == "darwin" {
		out = append(out, Check{ID: "os", Name: "系統", Level: LevelOK, Detail: "macOS"})
	} else {
		out = append(out, Check{ID: "os", Name: "系統", Level: LevelBad, Detail: "這一版只支援 macOS（launchd、macOS 通知）：" + goos})
	}
	brew := r.LookPath("brew")
	if brew != "" {
		out = append(out, Check{ID: "brew", Name: "Homebrew", Level: LevelOK, Detail: brew})
	} else {
		out = append(out, Check{ID: "brew", Name: "Homebrew", Level: LevelWarn, Detail: "沒有 Homebrew（缺的工具要自己裝）", Fix: "https://brew.sh"})
	}
	for _, t := range o.Tools {
		out = append(out, tool(r, t, t, LevelBad, brew != ""))
	}
	// Tailscale：跟 `evergenie tailscale setup`、看板的手機連線同一套找法（TailscaleBin：先 App 內附那支，DEV-153）；
	// 沒裝請主人到官網下載 App，不建議 brew（那支常比 App 舊，--fix 也不替他裝）。
	tsFix := "到 " + TailscaleDownload + " 下載 Tailscale App，裝好並登入（之後在看板「設定 → 手機連線」照步驟做）"
	if bin := TailscaleBin(r); bin != "" {
		out = append(out, Check{ID: "tailscale", Name: "Tailscale", Level: LevelOK, Detail: bin})
	} else if o.Need["tailscale"] {
		out = append(out, Check{ID: "tailscale", Name: "Tailscale", Level: LevelBad, Detail: "Tailscale 沒裝", Fix: tsFix})
	} else {
		out = append(out, Check{ID: "tailscale", Name: "Tailscale", Level: LevelWarn, Detail: "沒裝（要讓手機連看板才需要）", Fix: tsFix})
	}
	return out
}

func brewInstall(haveBrew bool, pkg string) []string {
	if !haveBrew {
		return nil
	}
	return []string{"brew", "install", pkg}
}

func tool(r Runner, id, name string, missing Level, haveBrew bool) Check {
	if p := r.LookPath(id); p != "" {
		return Check{ID: id, Name: name, Level: LevelOK, Detail: p}
	}
	return Check{ID: id, Name: name, Level: missing, Detail: name + " 沒裝", Fix: "brew install " + id, Install: brewInstall(haveBrew, id)}
}

// FromEngine 把引擎回的檢查換成同樣的形狀；id 已經有的（例如必備工具）以前面的為準。
func FromEngine(checks []sdk.PreflightCheck, have []Check) []Check {
	seen := map[string]bool{}
	for _, c := range have {
		seen[c.ID] = true
	}
	var out []Check
	for _, c := range checks {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		lvl := LevelOK
		if !c.Ok {
			lvl = LevelBad
		}
		out = append(out, Check{ID: c.ID, Name: c.Name, Level: lvl, Detail: c.Detail, Fix: c.Fix, Install: c.Install})
	}
	return out
}

// Print 印出每一項（✔／⚠／✘），有問題的下一行寫修法；最後一行摘要。回傳 ✘ 的數量。
func Print(w io.Writer, checks []Check) int {
	mark := map[Level]string{LevelOK: "✔", LevelWarn: "⚠", LevelBad: "✘"}
	bad := 0
	for _, c := range checks {
		line := c.Name
		if c.Detail != "" {
			line += "（" + c.Detail + "）"
		}
		fmt.Fprintf(w, "  %s %s\n", mark[c.Level], line)
		if c.Level != LevelOK && c.Fix != "" {
			fmt.Fprintf(w, "      → %s\n", c.Fix)
		}
		if c.Level == LevelBad {
			bad++
		}
	}
	if bad == 0 {
		fmt.Fprintln(w, "\n都齊了，可以往下一步。")
	} else {
		fmt.Fprintf(w, "\n還有 %d 項要先處理（上面的 ✘），修好再跑一次 evergenie preflight。\n", bad)
	}
	return bad
}

// Fix 幫忙裝缺的：只跑 `brew install <套件>`（不需要密碼），而且只裝 ✘ 的，或要求過的 Tailscale。
// 回傳實際跑的指令（沒有可裝的是空字串）；失敗回錯誤。
func Fix(ctx context.Context, r Runner, w io.Writer, checks []Check, need map[string]bool) (string, error) {
	pkgs := map[string]bool{}
	for _, c := range checks {
		if c.Level == LevelOK || len(c.Install) != 3 || c.Install[0] != "brew" || c.Install[1] != "install" {
			continue
		}
		if c.Level == LevelBad || need[c.ID] {
			pkgs[c.Install[2]] = true
		}
	}
	if len(pkgs) == 0 {
		for _, c := range checks {
			if c.Level == LevelBad && r.LookPath("brew") == "" {
				return "", platform.Unavailable("沒有 Homebrew，沒辦法幫你裝：https://brew.sh")
			}
		}
		fmt.Fprintln(w, "沒有可以自動裝的項目。")
		return "", nil
	}
	list := make([]string, 0, len(pkgs))
	for p := range pkgs {
		list = append(list, p)
	}
	sort.Strings(list)
	brew := r.LookPath("brew")
	if brew == "" {
		return "", platform.Unavailable("沒有 Homebrew，沒辦法幫你裝：https://brew.sh")
	}
	cmd := "brew install " + strings.Join(list, " ")
	fmt.Fprintf(w, "執行 %s …\n", cmd)
	if out, code := r.Run(ctx, brew, append([]string{"install"}, list...)...); code != 0 {
		return cmd, platform.Unavailable(fmt.Sprintf("安裝失敗（結束碼 %d）：%s", code, oneLine(out)))
	}
	return cmd, nil
}
