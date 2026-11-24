package board

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// WEB-170：下載的捷徑寫的是這個家的 public_url、檔名「跟秘書說」、簽好就快取；簽不成回 503。
func TestVoiceShortcutDownload(t *testing.T) {
	e := newEnv(t, `public_url="https://genie-amy-mini.example.ts.net"`+"\n")
	var signed []string
	old := signShortcut
	t.Cleanup(func() { signShortcut = old })
	signShortcut = func(_ context.Context, in, out string) error {
		b, err := os.ReadFile(in)
		if err != nil {
			return err
		}
		signed = append(signed, string(b))
		return os.WriteFile(out, append([]byte("AEA1"), b...), 0o644)
	}
	get := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, voiceShortcutPath, nil)
		r.Host = "genie-amy-mini.example.ts.net"
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, r)
		return w
	}
	w := get()
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "%E8%B7%9F%E7%A7%98%E6%9B%B8%E8%AA%AA.shortcut") {
		t.Fatalf("code=%d disposition=%q", w.Code, w.Header().Get("Content-Disposition"))
	}
	if len(signed) != 1 || !strings.Contains(signed[0], "<string>https://genie-amy-mini.example.ts.net/api/web/voice_send</string>") ||
		!strings.Contains(signed[0], "is.workflow.actions.notification") || !strings.Contains(signed[0], "is.workflow.actions.speaktext") || !strings.Contains(signed[0], "<string>speak</string>") || strings.Contains(signed[0], "showresult") || !strings.Contains(signed[0], "<string>zh-TW</string>") {
		t.Fatalf("捷徑內容不對：%v", signed)
	}
	if w := get(); w.Code != http.StatusOK || len(signed) != 1 {
		t.Fatalf("第二次要用快取：code=%d 簽了 %d 次", w.Code, len(signed))
	}
}

func TestVoiceShortcutSignFailsAndNoURL(t *testing.T) {
	old := signShortcut
	t.Cleanup(func() { signShortcut = old })
	signShortcut = func(context.Context, string, string) error { return errors.New("沒有 shortcuts") }
	e := newEnv(t, `public_url="https://genie-amy-mini.example.ts.net"`+"\n")
	r := httptest.NewRequest(http.MethodGet, voiceShortcutPath, nil)
	r.Host = "genie-amy-mini.example.ts.net"
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "還沒簽好") {
		t.Fatalf("簽不成要 503：%d %s", w.Code, w.Body.String())
	}
	if m, _ := filepath.Glob(filepath.Join(e.home, "runtime", "voice", "*", "*")); len(m) != 0 {
		t.Fatalf("失敗不能留半個檔：%v", m)
	}
	local := newEnv(t, "")
	r = httptest.NewRequest(http.MethodGet, voiceShortcutPath, nil)
	r.Host = "127.0.0.1"
	w = httptest.NewRecorder()
	local.h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("不知道網址要 409：%d", w.Code)
	}
}

// 產生的 XML 要是 macOS 認得的 plist（有 plutil 才測）。
func TestVoiceShortcutPlistLints(t *testing.T) {
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("沒有 plutil")
	}
	f := filepath.Join(t.TempDir(), "a.plist")
	if err := os.WriteFile(f, []byte(voiceShortcutPlist("https://x.example.ts.net/api/web/voice_send?a=1&b=<2>")), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("plutil", "-lint", f).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
}
