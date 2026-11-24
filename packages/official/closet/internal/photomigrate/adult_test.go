package photomigrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIsAdultDirSharedCases：跟核心 personaref.IsAdultPath 讀同一組案例（核心沒有照片紀錄可讀時的保底，跟轉換器同一份規則）。
// 套件被複製到 repo 外面時讀不到核心的案例檔，略過。
func TestIsAdultDirSharedCases(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "internal", "base", "agent", "personaref", "testdata", "adult_paths.txt"))
	if err != nil {
		t.Skipf("讀不到核心的共用案例（不在 EverGenie repo 裡）：%v", err)
	}
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		want, rel, _ := strings.Cut(line, " ")
		if got := IsAdultDir(rel); got != (want == "true") {
			t.Errorf("IsAdultDir(%q) = %v，要 %s（跟核心不一致）", rel, got, want)
		}
		n++
	}
	if n < 10 {
		t.Fatalf("案例太少：%d", n)
	}
}
