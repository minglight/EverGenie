package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/engine"
	"github.com/minglight/evergenie/internal/base/engine/registry"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// OfflineChecks 是 `evergenie doctor` 不需要 serve 的那幾項（DCT-6；launchd 那一項由 registry 傳進 Commands）：直接讀家，不連 API。
// 不讀秘密檔內容（CLI-N4）：settings/*.toml 只判斷解析得了，錯誤只報檔名與行號，不帶內容。
func OfflineChecks(ctx context.Context, home string) []sdk.DoctorCheck {
	config := func(id, detail, fix string, sev sdk.DoctorSeverity) sdk.DoctorCheck {
		return sdk.DoctorCheck{ID: id, Group: sdk.DoctorGroupConfig, Ok: sev == sdk.DoctorSeverityOk, Severity: sev, Detail: detail, Fix: fix}
	}
	if home == "" {
		return []sdk.DoctorCheck{
			config("config.home", "不知道家在哪裡（沒有給 --home，也沒有設定 EVERGENIE_HOME）", "用 `evergenie doctor --home <家的資料夾>`，或設定環境變數 EVERGENIE_HOME", sdk.DoctorSeverityError),
		}
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		var pe *platform.Err
		msg := "家的設定讀不起來"
		if errors.As(err, &pe) && pe.Message != "" {
			msg += "：" + pe.Message
		}
		return []sdk.DoctorCheck{
			config("config.home", fmt.Sprintf("%s（%s）", msg, filepath.Join(home, "home.toml")), "建立或修正 home.toml（以及 settings/trust.toml）後再執行一次", sdk.DoctorSeverityError),
		}
	}
	out := []sdk.DoctorCheck{config("config.home", "home.toml 讀得起來："+home, "", sdk.DoctorSeverityOk)}

	// settings/*.toml 都要解析得了。
	files, _ := filepath.Glob(filepath.Join(home, "settings", "*.toml"))
	sort.Strings(files)
	bad := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err == nil {
			var v map[string]any
			_, err = toml.Decode(string(data), &v)
		}
		if err == nil {
			continue
		}
		bad++
		where := ""
		var perr toml.ParseError
		if errors.As(err, &perr) && perr.Position.Line > 0 {
			where = fmt.Sprintf("（第 %d 行）", perr.Position.Line)
		}
		out = append(out, config("config.settings", fmt.Sprintf("settings/%s 格式不對%s", filepath.Base(f), where), "修正那個檔案；設定頁改過的檔案也可以刪掉重設", sdk.DoctorSeverityError))
	}
	switch {
	case bad > 0:
	case len(files) == 0:
		out = append(out, config("config.settings", "settings 資料夾裡還沒有設定檔（用預設值）", "", sdk.DoctorSeverityOk))
	default:
		out = append(out, config("config.settings", fmt.Sprintf("settings 底下 %d 個設定檔都讀得起來", len(files)), "", sdk.DoctorSeverityOk))
	}

	// 帳號用到的引擎：內建、而且執行檔找得到。
	binaries := registry.Executables()
	used := map[string][]string{}
	for name, v := range h.Accounts {
		if table, ok := v.(map[string]any); ok && name != "default" {
			id, _ := table["engine"].(string)
			used[id] = append(used[id], name)
		}
	}
	ids := make([]string, 0, len(used))
	for id := range used {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	pkg := func(detail, fix string, sev sdk.DoctorSeverity) sdk.DoctorCheck {
		return sdk.DoctorCheck{ID: "packages.engine_bin", Group: sdk.DoctorGroupPackages, Ok: sev == sdk.DoctorSeverityOk, Severity: sev, Detail: detail, Fix: fix}
	}
	missing := 0
	for _, id := range ids {
		sort.Strings(used[id])
		binary, known := binaries[id]
		switch {
		case !known:
			missing++
			out = append(out, pkg(fmt.Sprintf("帳號 %v 指定的引擎「%s」這個版本沒有內建", used[id], id), "把 home.toml 該帳號的 engine 改成可用的引擎", sdk.DoctorSeverityError))
		case engine.FindBinary(binary) == "":
			missing++
			out = append(out, pkg(fmt.Sprintf("找不到引擎「%s」的執行檔（%s）", id, binary), "把它放在 EverGenie 旁邊，或用 EVERGENIE_ENGINE_BIN_DIR 指到放它的資料夾", sdk.DoctorSeverityError))
		}
	}
	if missing == 0 {
		out = append(out, pkg(fmt.Sprintf("引擎執行檔都找得到（%d 個引擎）", len(ids)), "", sdk.DoctorSeverityOk))
	}
	return out
}
