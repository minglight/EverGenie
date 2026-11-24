package settings

import (
	"context"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 看板連多台的設定（web.get_sites、set_sites、set_allow_boards、set_owner_login；S3-3）：home.toml 的
// [web] owner_login、allow_boards、[[web.sites]]。只開這幾個欄位給看板改，不開通用的 system.set_config。
// 規則與錯誤訊息只有 platform 一份（PLT-22 的 Normalize*，LoadHome 也用它們）：寫檔前先整理、壞值直接擋下
// （被拒絕的請求不留備份、檔案不動，也不會有一瞬間壞掉的 home.toml 被每個請求都重讀的 CORS 檢查讀到），
// 寫完照 SetHomeTable 再用 LoadHome 驗一次。

// GetSites 回整理過的 owner_login、allow_boards、sites。只讀。
func (s *service) GetSites(ctx context.Context, req *sdk.WebGetSitesRequest) (*sdk.WebGetSitesResponse, error) {
	h, err := s.loadHome()
	if err != nil {
		return nil, s.fail(err)
	}
	return sitesOf(h), nil
}

// SetSites 整份換掉 [[web.sites]]；只有主人。
func (s *service) SetSites(ctx context.Context, req *sdk.WebSetSitesRequest) (*sdk.WebSetSitesResponse, error) {
	who, err := ownerOnly(req.By, "只有主人能改看板要連的電腦")
	if err != nil {
		return nil, s.fail(err)
	}
	in := make([]platform.WebSite, 0, len(req.Sites))
	for _, site := range req.Sites {
		in = append(in, platform.WebSite{URL: site.URL, Color: site.Color})
	}
	sites, err := platform.NormalizeSites(in)
	if err != nil {
		return nil, s.fail(err)
	}
	rows := make([]map[string]any, 0, len(sites))
	for _, site := range sites {
		row := map[string]any{"url": site.URL}
		if site.Color != "" {
			row["color"] = site.Color
		}
		rows = append(rows, row)
	}
	h, backup, err := s.writeWeb(who, "sites", rows)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.WebSetSitesResponse{Sites: sitesOf(h).Sites, Backup: backup}, nil
}

// SetAllowBoards 整份換掉 allow_boards；只有主人。
func (s *service) SetAllowBoards(ctx context.Context, req *sdk.WebSetAllowBoardsRequest) (*sdk.WebSetAllowBoardsResponse, error) {
	who, err := ownerOnly(req.By, "只有主人能改哪些看板可以連進來")
	if err != nil {
		return nil, s.fail(err)
	}
	origins, err := platform.NormalizeAllowBoards(req.AllowBoards)
	if err != nil {
		return nil, s.fail(err)
	}
	if origins == nil {
		origins = []string{} // 清掉＝寫 allow_boards = []
	}
	h, backup, err := s.writeWeb(who, "allow_boards", origins)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.WebSetAllowBoardsResponse{AllowBoards: h.Web.AllowBoards, Backup: backup}, nil
}

// SetOwnerLogin 改 owner_login（空＝拿掉，不開放別台的看板連進來）；只有主人。
func (s *service) SetOwnerLogin(ctx context.Context, req *sdk.WebSetOwnerLoginRequest) (*sdk.WebSetOwnerLoginResponse, error) {
	who, err := ownerOnly(req.By, "只有主人能改主人的 tailnet 登入")
	if err != nil {
		return nil, s.fail(err)
	}
	login, err := platform.NormalizeOwnerLogin(req.OwnerLogin)
	if err != nil {
		return nil, s.fail(err)
	}
	h, backup, err := s.writeWeb(who, "owner_login", login)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.WebSetOwnerLoginResponse{OwnerLogin: h.Web.OwnerLogin, Backup: backup}, nil
}

// ownerOnly 回操作者名稱；不是主人回 FORBIDDEN，沒帶身分回 INVALID（跟 set_config 一樣用 platform.IsOwner）。
func ownerOnly(by *sdk.Actor, forbidden string) (string, error) {
	who, err := platform.ActorName(by)
	if err != nil {
		return "", err
	}
	if !platform.IsOwner(who) {
		return "", platform.Forbidden(forbidden)
	}
	return who, nil
}

// writeWeb 把 [web] 的一個欄位設成 v：platform.SetHomeTable 先備份、寫完用 LoadHome 驗證、不合格寫回舊檔；
// 成功後回重讀的家（整理過的值）與備份路徑。
func (s *service) writeWeb(who, field string, v any) (*platform.Home, string, error) {
	home, err := s.home()
	if err != nil {
		return nil, "", err
	}
	backup, err := platform.SetHomeTable(home, "web."+field, v, s.m.now())
	if err != nil {
		return nil, "", err
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		return nil, "", err
	}
	if log := s.m.logger(); log != nil {
		log.Info("看板連多台的設定已更新", "by", who, "field", "web."+field, "backup", backup)
	}
	return h, backup, nil
}

func (s *service) loadHome() (*platform.Home, error) {
	home, err := s.home()
	if err != nil {
		return nil, err
	}
	return platform.LoadHome(home)
}

func sitesOf(h *platform.Home) *sdk.WebGetSitesResponse {
	out := &sdk.WebGetSitesResponse{OwnerLogin: h.Web.OwnerLogin, AllowBoards: h.Web.AllowBoards}
	for _, site := range h.Web.Sites {
		out.Sites = append(out.Sites, sdk.WebSite{URL: site.URL, Color: site.Color})
	}
	return out
}
