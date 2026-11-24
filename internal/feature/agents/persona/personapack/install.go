package personapack

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Install 是一次換檔：把 Src（已經驗過 sha256 的檔）放到 Dst；Exists＝Dst 原本有檔（要先改名留著，失敗才倒得回去）。
// Label 是錯誤訊息裡給人看的名字（邏輯路徑）。角色包同步與角色匯入共用（D-63）。
type Install struct {
	Label  string
	Src    string
	Dst    string
	Exists bool
}

// InstallAll 全部到齊才一次換進來：先把新檔寫成同資料夾的暫存檔（`.<檔名>.hgpack`），再一個一個改名；
// 任何一步失敗就倒回去（新的刪掉、舊的改回來）、回白話的原因。成功時舊檔的暫存名字也清掉。
func InstallAll(items []Install) error {
	staged := make([]swap, 0, len(items))
	for _, it := range items {
		if err := os.MkdirAll(filepath.Dir(it.Dst), 0o755); err != nil {
			discard(staged)
			return fmt.Errorf("無法建立資料夾 %s", filepath.Dir(it.Dst))
		}
		tmp := filepath.Join(filepath.Dir(it.Dst), "."+filepath.Base(it.Dst)+".hgpack")
		if err := CopyFile(it.Src, tmp); err != nil {
			discard(staged)
			return fmt.Errorf("無法寫入「%s」", it.Label)
		}
		staged = append(staged, swap{dst: it.Dst, tmp: tmp})
	}
	var done []swap
	for i, s := range staged {
		if items[i].Exists {
			s.old = s.dst + ".hgpack-old"
			if err := os.Rename(s.dst, s.old); err != nil {
				undo(done)
				discard(staged)
				return fmt.Errorf("無法換掉「%s」", items[i].Label)
			}
		}
		done = append(done, s)
		if err := os.Rename(s.tmp, s.dst); err != nil {
			undo(done)
			discard(staged)
			return fmt.Errorf("無法換進「%s」", items[i].Label)
		}
	}
	for _, s := range done {
		if s.old != "" {
			_ = os.Remove(s.old)
		}
	}
	return nil
}

// swap 是一次換檔：tmp（新內容）→ dst；old 是舊檔暫時改的名字（原本沒有檔案就空）。
type swap struct{ dst, tmp, old string }

// undo 倒回已經換進去的（新的刪掉、舊的改回來）。
func undo(done []swap) {
	for i := len(done) - 1; i >= 0; i-- {
		s := done[i]
		_ = os.Remove(s.dst)
		if s.old != "" {
			_ = os.Rename(s.old, s.dst)
		}
	}
}

// discard 刪掉還沒用到的暫存檔。
func discard(staged []swap) {
	for _, s := range staged {
		_ = os.Remove(s.tmp)
	}
}

// CopyFile 把 src 原子寫到 dst（需要的資料夾先建）：一邊讀一邊寫進同資料夾的暫存檔，寫完 fsync 再改名，大檔不整個讀進記憶體。
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
