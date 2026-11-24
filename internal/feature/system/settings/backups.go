package settings

import (
	"context"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Preflight：各帳號的引擎回報「跑起來需要的東西」齊不齊（evergenie preflight 的平台那一段）。只檢查，不安裝。
func (s *service) Preflight(ctx context.Context, req *sdk.SystemPreflightRequest) (*sdk.SystemPreflightResponse, error) {
	if s.m.d.Engine == nil {
		return nil, s.fail(platform.Unavailable("引擎服務還沒接上"))
	}
	rows, err := s.m.d.Engine.Preflight(ctx)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.SystemPreflightResponse{Checks: rows}, nil
}

// Backups：列出備份；prune 依保留規則標出要刪的（預演）；prune＋apply 真的刪，只有主人能做。
func (s *service) Backups(ctx context.Context, req *sdk.SystemBackupsRequest) (*sdk.SystemBackupsResponse, error) {
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	if req.Apply {
		if !req.Prune {
			return nil, s.fail(platform.Invalid("apply", "apply 要搭配 prune"))
		}
		who, err := platform.ActorName(req.By)
		if err != nil {
			return nil, s.fail(err)
		}
		if !platform.IsOwner(who) {
			return nil, s.fail(platform.Forbidden("只有主人能刪備份；秘書可以先預演"))
		}
	}
	list, err := ListBackups(home)
	if err != nil {
		return nil, s.fail(err)
	}
	var plan map[string]bool
	if req.Prune {
		plan = PlanPrune(list, int(req.Keep), int(req.Days), s.m.now())
		keepRescueBackups(home, plan)
	}
	out := &sdk.SystemBackupsResponse{}
	var doomed []string
	for _, b := range list {
		out.Total += b.Size
		f := sdk.BackupFile{Path: b.Path, Group: b.Group, Size: b.Size, ModTime: b.ModTime, Prune: plan[b.Path]}
		if f.Prune {
			out.Pruned++
			out.Freed += b.Size
			doomed = append(doomed, b.Path)
		}
		out.Files = append(out.Files, f)
	}
	if req.Apply && len(doomed) > 0 {
		if err := DeleteBackups(home, doomed); err != nil {
			return nil, s.fail(err)
		}
		out.Applied = true
		if log := s.m.logger(); log != nil {
			log.Info("備份已整理", "by", req.By.Name, "deleted", len(doomed))
		}
	}
	return out, nil
}
