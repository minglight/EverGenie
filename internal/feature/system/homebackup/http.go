package homebackup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記 sdk 的 backup 服務（整個由這裡實作）。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "backup", Impl: &service{m: m}}}
}

type service struct{ m *Module }

var _ sdk.BackupAPI = (*service)(nil)

func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if out.Code == sdk.ErrorCodeInternal && !errors.Is(err, context.Canceled) {
		s.m.logger().Error("request failed", "error", err)
	}
	return out
}

func (s *service) home() (string, error) {
	home := s.m.homeDir()
	if home == "" {
		return "", platform.Unavailable("還不知道家在哪裡")
	}
	return home, nil
}

// owner：只有主人能做的事（還原）。
func owner(by *sdk.Actor, what string) error {
	who, err := platform.ActorName(by)
	if err != nil {
		return err
	}
	if !platform.IsOwner(who) {
		return platform.Forbidden(what + "只有主人能做；請主人在看板「設定 → 電腦 → 備份」操作")
	}
	return nil
}

func (s *service) GetStatus(ctx context.Context, req *sdk.BackupGetStatusRequest) (*sdk.BackupGetStatusResponse, error) {
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	res := &sdk.BackupGetStatusResponse{
		Files: List(home), Restoring: HasPending(home), KeepCount: KeepCount, KeepDays: KeepDays,
		Local: req != nil && req.Local,
	}
	if free, ok := freeBytes(platform.HomeBackupDir(home, sdk.HomeBackupKindManual)); ok {
		res.FreeBytes = free
	}
	for _, f := range res.Files {
		if f.Kind != sdk.HomeBackupKindBeforeRestore {
			res.LastAt, res.LastKind = f.CreatedAt, f.Kind
			break
		}
	}
	if r := readResult(home); r != nil {
		res.LastRestore = &r.HomeRestoreResult
	}
	return res, nil
}

func (s *service) Create(ctx context.Context, _ *sdk.BackupCreateRequest) (*sdk.BackupCreateResponse, error) {
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	now := s.m.now()
	res, err := s.m.makeBackup(home, sdk.HomeBackupKindManual, now)
	if err != nil {
		return nil, s.fail(err)
	}
	c := res.created
	return &sdk.BackupCreateResponse{
		File: sdk.HomeBackupFile{
			Name: c.Name, Kind: sdk.HomeBackupKindManual, Size: c.Size, CreatedAt: c.Manifest.CreatedAt,
			Download: "/home-backup/" + sdk.HomeBackupKindManual + "/" + c.Name,
		},
		Sections: c.Manifest.sdkSections(), Skipped: c.Manifest.skippedPaths(),
	}, nil
}

// locate 找出請求指的是哪個檔：看板上傳的、這台現有的一份、或這台電腦上的路徑。
func locate(home, upload, kind, name, path string) (string, error) {
	switch {
	case upload != "":
		if !platform.IsHomeBackupUploadName(upload) {
			return "", platform.Invalid("upload", "上傳的檔案名稱不對，請重新上傳")
		}
		return filepath.Join(platform.HomeBackupUploadDir(home), upload), nil
	case name != "":
		if !platform.IsHomeBackupKind(kind) || !platform.IsHomeBackupName(name) {
			return "", platform.Invalid("name", "找不到這份備份")
		}
		return filepath.Join(platform.HomeBackupDir(home, kind), name), nil
	case path != "":
		p := platform.ExpandHome(strings.TrimSpace(path))
		if !filepath.IsAbs(p) {
			return "", platform.Invalid("path", "請給備份檔的完整位置")
		}
		return filepath.Clean(p), nil
	}
	return "", platform.Invalid("name", "要說是哪一份備份")
}

func (s *service) Preview(ctx context.Context, req *sdk.BackupPreviewRequest) (*sdk.BackupPreviewResponse, error) {
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	archive, err := locate(home, req.Upload, req.Kind, req.Name, req.Path)
	if err != nil {
		return nil, s.fail(err)
	}
	info, err := os.Stat(archive)
	if err != nil || !info.Mode().IsRegular() {
		return nil, s.fail(platform.NotFound("找不到這份備份檔"))
	}
	m, err := Inspect(archive)
	if err != nil {
		return nil, s.fail(err)
	}
	res := &sdk.BackupPreviewResponse{
		CreatedAt: m.CreatedAt, HomeName: m.HomeName, Owner: m.Owner, Machine: m.Machine, AppVersion: m.AppVersion, Kind: m.Kind,
		Size: info.Size(), Files: int32(m.TotalFiles()), Bytes: m.TotalBytes(), Sections: m.sdkSections(), Skipped: m.skippedPaths(),
		Problem: Problem(m, s.m.known(home)), Installed: m.Installed,
		OtherMachine: filepath.Clean(m.HomeDir) != filepath.Clean(home), LostLinks: LostLinks(home),
	}
	if h, err := platform.LoadHome(home); err == nil && m.Machine != "" && m.Machine != h.Runtime.Machine {
		res.OtherMachine = true
	}
	return res, nil
}

func (s *service) Restore(ctx context.Context, req *sdk.BackupRestoreRequest) (*sdk.BackupRestoreResponse, error) {
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	if err := owner(req.By, "還原"); err != nil {
		return nil, s.fail(err)
	}
	archive, err := locate(home, req.Upload, req.Kind, req.Name, req.Path)
	if err != nil {
		return nil, s.fail(err)
	}
	s.m.work.Lock()
	p, err := Stage(StageOptions{Home: home, Archive: archive, Now: s.m.now(), KnownMigrations: s.m.known(home)})
	s.m.work.Unlock()
	if err != nil {
		return nil, s.fail(err)
	}
	s.m.logger().Info("restore staged; restarting", "backup_created_at", p.BackupCreatedAt, "safety", p.Safety)
	s.m.closeSessions(ctx)
	s.m.restart()
	return &sdk.BackupRestoreResponse{Restarting: true, Safety: p.Safety}, nil
}
