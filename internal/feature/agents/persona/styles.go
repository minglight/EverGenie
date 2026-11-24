package persona

// 說話層強度（P6-5a、D-31、REQ-130）：a–d 是核心內建的四份文字（styles/*.md），custom 讀家裡的 personas/_style.md。
// 設定存 home.toml 的 [persona] style；沒設定時 _style.md 存在＝custom，不存在＝c。

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

//go:embed styles/*.md
var styleFS embed.FS

// StyleLevels 是五個選項，順序就是看板上的順序。
var StyleLevels = []string{"a", "b", "c", "d", "custom"}

func presetStyle(level string) string {
	data, err := styleFS.ReadFile("styles/" + level + ".md")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (m *Module) customStylePath() string { return filepath.Join(m.home, "personas", "_style.md") }

// readCustomStyle：personas/_style.md 的內容；不存在回 exists=false（不算錯）。
func (m *Module) readCustomStyle() (text string, exists bool, err error) {
	data, err := os.ReadFile(m.customStylePath())
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, platform.Unavailable("無法讀取說話層")
	}
	return strings.TrimSpace(string(data)), true, nil
}

// styleSetting 是 home.toml 設定的強度（沒設定回空字串）。
func (m *Module) styleSetting() (string, error) {
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return "", err
	}
	return h.Persona.Style, nil
}

// effectiveStyle 算出現在生效的強度與全文。設了 custom 但 _style.md 不在：退回 c（exists=false，doctor 會報）。
func (m *Module) effectiveStyle() (level, text string, customExists bool, err error) {
	set, err := m.styleSetting()
	if err != nil {
		return "", "", false, err
	}
	custom, customExists, err := m.readCustomStyle()
	if err != nil {
		return "", "", false, err
	}
	level = set
	if level == "" {
		level = "c"
		if customExists {
			level = "custom"
		}
	}
	if level == "custom" {
		if customExists {
			return level, custom, true, nil
		}
		return level, presetStyle("c"), false, nil
	}
	return level, presetStyle(level), customExists, nil
}

// StyleInfo 回現在生效的強度與全文，以及五個選項各自的全文。
func (m *Module) StyleInfo(ctx context.Context) (level, text string, customExists bool, levels []sdk.PersonaStyleLevel, err error) {
	level, text, customExists, err = m.effectiveStyle()
	if err != nil {
		return "", "", false, nil, err
	}
	custom, _, err := m.readCustomStyle()
	if err != nil {
		return "", "", false, nil, err
	}
	for _, l := range StyleLevels {
		if l == "custom" {
			levels = append(levels, sdk.PersonaStyleLevel{Level: l, Text: custom, Exists: customExists})
			continue
		}
		levels = append(levels, sdk.PersonaStyleLevel{Level: l, Text: presetStyle(l), Exists: true})
	}
	return level, text, customExists, levels, nil
}

// SetStyle 設說話層強度；只有主人。custom 要 personas/_style.md 在。回備份的位置（沒有舊檔時空字串）。
func (m *Module) SetStyle(ctx context.Context, level, by string) (string, error) {
	if err := requireOwner(by); err != nil {
		return "", err
	}
	level = strings.ToLower(strings.TrimSpace(level))
	if !contains(StyleLevels, level) {
		return "", platform.Invalid("level", "說話層強度只能是 a、b、c、d、custom")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if level == "custom" {
		if _, exists, err := m.readCustomStyle(); err != nil {
			return "", err
		} else if !exists {
			return "", platform.Invalid("level", "自己的說話層還沒有：先建立 personas/_style.md")
		}
	}
	backup, _, err := platform.SetHomeValue(m.home, "persona.style", `"`+level+`"`, m.d.Clock.Now())
	return backup, err
}

// Style 是名冊讀說話層的入口（agent.PersonaSource）：現在生效的強度、全文、自己的說話層在不在。
func (m *Module) Style() (level, text string, customExists bool, err error) {
	return m.effectiveStyle()
}
