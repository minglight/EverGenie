package claude

import "strings"

// withoutFaint 把 `tmux capture-pane -e` 抓到的畫面變回純文字，並拿掉畫成淡色（SGR 2）的字（ECL-55）：
// 平台在空的輸入框裡用淡色放提示或「猜你下一句」的建議，那不是主人打的字。換行照留，行數不變。
func withoutFaint(raw string) string { return unstyle(raw, true) }

// withoutStyle 只拿掉控制碼、所有字都留（找輸入框的框線用：框線就算畫成淡色也要找得到）。
func withoutStyle(raw string) string { return unstyle(raw, false) }

// unstyle 只處理 tmux `capture-pane -e` 會輸出的序列：CSI（SGR 之類）、OSC（超連結），其他 ESC 開頭的跳過兩個字。
func unstyle(raw string, dropFaint bool) string {
	var b strings.Builder
	faint := false
	for i := 0; i < len(raw); {
		c := raw[i]
		if c != 0x1b {
			if !dropFaint || !faint || c == '\n' {
				b.WriteByte(c)
			}
			i++
			continue
		}
		if i+1 >= len(raw) {
			break
		}
		switch raw[i+1] {
		case '[': // CSI：參數到第一個 0x40–0x7e 的字為止；只有 m（樣式）要看
			j := i + 2
			for j < len(raw) && (raw[j] < 0x40 || raw[j] > 0x7e) {
				j++
			}
			// 參數以 < = > ? 開頭的是私有序列（例如 ESC[>4;2m 設鍵盤模式），不是樣式。
			if j < len(raw) && raw[j] == 'm' && (j == i+2 || !strings.ContainsRune("<=>?", rune(raw[i+2]))) {
				faint = sgrFaint(raw[i+2:j], faint)
			}
			i = j + 1
		case ']': // OSC（例如超連結）：到 BEL 或 ESC \ 為止
			j := i + 2
			for j < len(raw) && raw[j] != 0x07 && (raw[j] != 0x1b || j+1 >= len(raw) || raw[j+1] != '\\') {
				j++
			}
			if j < len(raw) && raw[j] == 0x07 {
				i = j + 1
			} else {
				i = j + 2
			}
		default:
			i += 2
		}
	}
	return b.String()
}

// sgrFaint 照一段 SGR 參數算出之後是不是淡色：0（或空）全部重設、2 淡色、22 回正常；
// 38／48／58 後面跟的是顏色（5;n 或 2;r;g;b），裡面的 2 不是淡色。
func sgrFaint(params string, faint bool) bool {
	if params == "" {
		return false
	}
	ps := strings.Split(params, ";")
	for k := 0; k < len(ps); k++ {
		switch ps[k] {
		case "", "0":
			faint = false
		case "2":
			faint = true
		case "22":
			faint = false
		case "38", "48", "58":
			if k+1 < len(ps) {
				switch ps[k+1] {
				case "5":
					k += 2
				case "2":
					k += 4
				}
			}
		}
	}
	return faint
}
