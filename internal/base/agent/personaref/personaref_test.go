package personaref

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 套件角色的有效代號是 <套件id>.<slug>：拆得開才算，官方與我的代號（不含點）不是。
func TestSplitPackageCode(t *testing.T) {
	pkg, slug, ok := SplitPackageCode(PackageCode("garden-2", "fairy"))
	if !ok || pkg != "garden-2" || slug != "fairy" {
		t.Fatalf("拆不回來：%q %q %v", pkg, slug, ok)
	}
	for _, bad := range []string{"fairy", "garden.", ".fairy", "Garden.fairy", "garden.Fairy", "garden.fairy.x", "garden-.fairy"} {
		if IsPackageCode(bad) {
			t.Errorf("%q 不該是套件代號", bad)
		}
	}
}

// TestIsAdultPathSharedCases：自己的隱藏版判斷照共用案例（testdata/adult_paths.txt；衣櫃轉換器 photomigrate 的測試讀同一份）。
func TestIsAdultPathSharedCases(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "adult_paths.txt"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		want, rel, _ := strings.Cut(line, " ")
		if got := IsAdultPath(rel); got != (want == "true") {
			t.Errorf("IsAdultPath(%q) = %v，要 %s", rel, got, want)
		}
		n++
	}
	if n < 10 {
		t.Fatalf("案例太少：%d", n)
	}
}
