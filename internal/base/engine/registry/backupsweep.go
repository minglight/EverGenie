package registry

// 每週的備份掃描（system/backupsweep，DEV-163）要的平台相關資料：核心其他地方不寫平台名稱（MOD-N7），集中在這裡。
// 只有資料，沒有行為分支。

// SweepSkip 是一條「不算備份」的例外：Path 用 ~/ 開頭、可以有 * 萬用字元。
type SweepSkip struct {
	Path   string
	Reason string
}

// SweepAccountDirs 是各引擎的帳號資料夾（規格的掃描範圍之一）。
func SweepAccountDirs() []string {
	return []string{"~/.claude*", "~/.codex"}
}

// SweepSkips 是平台工具自己輪替的備份，不算主人留的備份。
func SweepSkips() []SweepSkip {
	return []SweepSkip{{Path: "~/.claude*/backups", Reason: "AI 程式自己輪替的設定備份（只留最近幾份）"}}
}

// SweepTerminalCheck 是「在用」檢查的終端機工具那一種：程式內部的名稱與指令（給主人看的字是 backupsweep 的「終端機視窗」）。
// 查不了時 stderr 的原因會進報告與通知，所以只寫白話、不寫工具名稱。
// 指令輸出每一行是一個視窗的目前路徑與啟動指令；沒有開著的 session 輸出空的；查不了（沒裝、版本不合）就結束碼非 0、原因寫 stderr。
func SweepTerminalCheck() (string, []string) {
	return "tmux", []string{"/bin/sh", "-c", `
PATH="$PATH:/opt/homebrew/bin:/usr/local/bin"
dir="${TMUX_TMPDIR:-/tmp}/tmux-$(id -u)"
[ -d "$dir" ] || exit 0
found=""
for s in "$dir"/*; do [ -S "$s" ] && found=1; done
[ -n "$found" ] || exit 0
command -v tmux >/dev/null 2>&1 || { echo "找不到終端機工具，查不了開著的終端機視窗" >&2; exit 1; }
for s in "$dir"/*; do
  [ -S "$s" ] || continue
  if out=$(tmux -S "$s" list-panes -a -F '#{pane_current_path} #{pane_start_command}' 2>&1); then
    printf '%s\n' "$out"
    continue
  fi
  case "$out" in
    *"no server running"*|*"error connecting"*|*"onnection refused"*|*"No such file or directory"*) continue ;;
  esac
  echo "有一個終端機視窗查不了：$out" >&2
  exit 1
done
`}
}
