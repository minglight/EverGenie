package backupsweep

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記 sdk system 服務的 backup_sweep（system 其他方法由同塊別的子模組登記）。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "system", Impl: &service{m: m}, Methods: []string{"backup_sweep"}}}
}

type service struct{ m *Module }

var _ interface {
	BackupSweep(context.Context, *sdk.SystemBackupSweepRequest) (*sdk.SystemBackupSweepResponse, error)
} = (*service)(nil)

func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if out.Code == sdk.ErrorCodeInternal && !errors.Is(err, context.Canceled) {
		s.m.logger().Error("request failed", "error", err)
	}
	return out
}

// BackupSweep：run=false 回上一次的報告（誰都可以）；run=true 現在掃一次（誰都可以叫，要有身分；沙盒家也掃，是有人自己按的）。
// 主人叫的才順便照保留規則清 runtime/backups/（跟 `backup prune --apply` 一樣只有主人能刪）；排程叫的由 system 清（BSW-5）。
func (s *service) BackupSweep(ctx context.Context, req *sdk.SystemBackupSweepRequest) (*sdk.SystemBackupSweepResponse, error) {
	home := s.m.homeDir()
	if home == "" {
		return nil, s.fail(platform.Unavailable("還不知道家在哪裡"))
	}
	if req == nil || !req.Run {
		rep, err := loadReport(home)
		if err != nil {
			return nil, s.fail(err)
		}
		return &sdk.SystemBackupSweepResponse{Report: rep, Running: s.m.running.Load()}, nil
	}
	who, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	if !s.m.running.CompareAndSwap(false, true) {
		rep, err := loadReport(home)
		if err != nil {
			return nil, s.fail(err)
		}
		return &sdk.SystemBackupSweepResponse{Report: rep, Running: true}, nil
	}
	defer s.m.running.Store(false)
	rep, ok := s.m.manual(ctx, who, platform.IsOwner(who))
	if !ok {
		return nil, s.fail(platform.Unavailable("服務正在收工，等它重開再掃"))
	}
	return &sdk.SystemBackupSweepResponse{Report: rep}, nil
}
