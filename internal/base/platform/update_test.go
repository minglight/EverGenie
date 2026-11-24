package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func migHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	toml := "[user]\nname = \"測試\"\ntimezone = \"Asia/Taipei\"\n[slots]\ndefault = \"life\"\n[slots.life]\nlabel = \"日常\"\nremind = \"08:30\"\ncheck = \"18:00\"\n[accounts]\ndefault = \"main\"\n[accounts.main]\nengine = \"x\"\n[web]\nport = 18850\n"
	if err := os.WriteFile(filepath.Join(dir, "home.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// [update]：預設值、舊設定的頂層 publisher 被忽略（沒寫 channel 照樣是 release）、寫錯的值回白話錯誤、~ 展開。
// Guards: REQ-280
func TestHomeUpdateSection(t *testing.T) {
	home := migHome(t)
	h, err := LoadHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if h.UpdateChannel() != UpdateChannelRelease || h.Update.CheckAt != "10:00" || h.Update.TonightAt != "03:30" || h.Update.Core != "" {
		t.Fatalf("預設：%+v %s", h.Update, h.UpdateChannel())
	}
	write := func(extra string) error {
		data, _ := os.ReadFile(filepath.Join(home, "home.toml"))
		base := strings.SplitN(string(data), "\n[update]", 2)[0]
		base = strings.SplitN(base, "\npublisher", 2)[0]
		if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte(extra+"\n"+base), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadHome(home)
		return err
	}
	// TOML：頂層 key 要在表格之前，所以把設定放在檔案最前面。
	// 相容：2026-10-31 前的家有頂層 publisher = true（已廢，GOAL-2 W6）；讀得進來、不影響頻道。
	if err := write("publisher = true"); err != nil {
		t.Fatal(err)
	}
	h, _ = LoadHome(home)
	if h.UpdateChannel() != UpdateChannelRelease {
		t.Fatalf("舊的 publisher 不再影響頻道：%s", h.UpdateChannel())
	}
	data, _ := os.ReadFile(filepath.Join(home, "home.toml"))
	if err := os.WriteFile(filepath.Join(home, "home.toml"), append(data, []byte("\n[update]\nchannel = \"safe\"\ncheck_at = \"09:15\"\ncore = \"~/core\"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err = LoadHome(home)
	u, _ := os.UserHomeDir()
	if err != nil || h.UpdateChannel() != UpdateChannelSafe || h.Update.CheckAt != "09:15" || h.Update.Core != filepath.Join(u, "core") {
		t.Fatalf("%+v %v", h.Update, err)
	}
	for _, bad := range []string{"channel = \"nightly\"", "check_at = \"25:00\"", "tonight_at = \"晚上\""} {
		d, _ := os.ReadFile(filepath.Join(home, "home.toml"))
		base := strings.SplitN(string(d), "\n[update]", 2)[0]
		if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte(base+"\n[update]\n"+bad+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadHome(home); err == nil {
			t.Fatalf("%s 該被擋", bad)
		}
	}
}

// [update] source：下載版找新版的版本清單網址。沒寫用編進執行檔的 DefaultUpdateSource（現在是空的＝沒有下載來源）；
// 寫了以它為準；要是 http(s):// 或 file:// 網址，不然回白話錯誤。
func TestHomeUpdateSource(t *testing.T) {
	home := migHome(t)
	h, err := LoadHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if h.Update.Source != "" || h.UpdateSource() != "" {
		t.Fatalf("預設沒有下載來源：%q %q", h.Update.Source, h.UpdateSource())
	}
	old := DefaultUpdateSource
	DefaultUpdateSource = "https://example.invalid/hg/manifest.json"
	t.Cleanup(func() { DefaultUpdateSource = old })
	if h.UpdateSource() != DefaultUpdateSource {
		t.Fatalf("沒寫就用編進去的預設：%q", h.UpdateSource())
	}
	base, _ := os.ReadFile(filepath.Join(home, "home.toml"))
	for _, good := range []string{"https://example.invalid/m.json", "http://127.0.0.1:8000/manifest.json", "file:///tmp/r/manifest.json"} {
		if err := os.WriteFile(filepath.Join(home, "home.toml"), append(append([]byte{}, base...), []byte("\n[update]\nsource = \""+good+"\"\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
		h, err := LoadHome(home)
		if err != nil || h.UpdateSource() != good {
			t.Fatalf("%s：%v %q", good, err, h.UpdateSource())
		}
	}
	// http 只收本機（PLT-25）：別台電腦的 http 不收
	for _, bad := range []string{"/tmp/manifest.json", "ftp://x/m.json", "https://", "下載的地方", "http://example.com/manifest.json", "http://192.0.2.2/m.json"} {
		if err := os.WriteFile(filepath.Join(home, "home.toml"), append(append([]byte{}, base...), []byte("\n[update]\nsource = \""+bad+"\"\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadHome(home)
		if err == nil || !strings.Contains(err.Error(), "https://") || strings.Contains(err.Error(), "http://") {
			t.Fatalf("%s 該被擋、訊息只建議 https：%v", bad, err)
		}
	}
}
