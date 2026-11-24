package pkg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// forkBaseDir 是 fork 時存下的「官方原樣」（基準內容），放在 packages/mine/<id>/ 裡；更新差異以它為準（D-3、REQ-95）。
const forkBaseDir = ".fork-base"

// Fork 把一個現在有效的官方套件整包複製進 packages/mine/<id>/：留一份基準內容、在 package.toml 尾端寫 [fork]。
// 全部先寫在暫存資料夾再 rename；之後 mine 蓋掉官方，舊行程收掉、新的重編（官方資料夾一律不動，PKG-N2）。
func (m *Module) Fork(ctx context.Context, req *sdk.PackageForkRequest) (*sdk.PackageForkResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.readyLocked(); err != nil {
		return nil, err
	}
	e := m.entries[req.ID]
	src := e
	if e != nil && e.tier == sdk.TierMine {
		return nil, platform.Conflict(fmt.Sprintf("「%s」已經是你自己的套件（packages/mine/%s），不用再 fork", req.ID, req.ID))
	}
	if src == nil || src.tier != sdk.TierOfficial || src.man == nil || src.err != nil {
		return nil, platform.NotFound(fmt.Sprintf("沒有可以 fork 的官方套件「%s」", req.ID))
	}
	mineRoot := m.tierDir(sdk.TierMine)
	dest := filepath.Join(mineRoot, req.ID)
	if exists(dest) {
		return nil, platform.Conflict(fmt.Sprintf("packages/mine/%s 已經存在", req.ID))
	}
	tmpRoot := filepath.Join(mineRoot, ".fork-"+shortRand()) // 隱藏資料夾：掃描時略過
	tmp := filepath.Join(tmpRoot, req.ID)
	defer os.RemoveAll(tmpRoot)
	fail := func(err error) (*sdk.PackageForkResponse, error) { return nil, err }
	if err := copyTree(src.dir, tmp); err != nil {
		return fail(platform.Unavailable("無法複製官方套件"))
	}
	if err := copyTree(src.dir, filepath.Join(tmp, forkBaseDir)); err != nil {
		return fail(platform.Unavailable("無法保存 fork 的基準內容"))
	}
	now := m.d.Clock.Now()
	block := fmt.Sprintf("\n[fork]\nof = %q\nbase_version = %q\nforked_at = %q\n", src.id, src.man.Version, now.UTC().Format(time.RFC3339))
	f, err := os.OpenFile(filepath.Join(tmp, "package.toml"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fail(platform.Unavailable("無法寫入 fork 的 package.toml"))
	}
	_, werr := f.WriteString(block)
	if cerr := f.Close(); werr != nil || cerr != nil {
		return fail(platform.Unavailable("無法寫入 fork 的 package.toml"))
	}
	if _, err := ParseManifest(tmp, sdk.TierMine); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return fail(platform.Unavailable("無法放進 packages/mine/"))
	}
	m.rescanLocked(now)
	m.markChangedLocked(dest)
	if m.log != nil {
		m.log.Info("fork 官方套件", "id", req.ID, "base_version", src.man.Version)
	}
	p, err := m.kickLocked(req.ID)
	if err != nil {
		return nil, err
	}
	return &sdk.PackageForkResponse{Package: p}, nil
}

// markChangedLocked：dir 的內容剛被核心自己改過（fork、安裝）；記下指紋，並讓那個套件重編重啟。
func (m *Module) markChangedLocked(dir string) {
	m.seen[dir] = fingerprint(dir)
	delete(m.pend, dir)
	for _, e := range m.entries {
		if e.dir == dir {
			e.rebuild, e.gaveUp, e.wantRun, e.restarts = true, false, true, 0
		}
	}
}

// copyTree 整包複製 src 到 dst（dst 不能已存在）；略過 .git、fork 基準與符號連結。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.IsDir() && rel != "." && (d.Name() == ".git" || d.Name() == forkBaseDir) {
			return filepath.SkipDir
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(p, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func shortRand() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
