package menu

import (
	"context"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的路由（app.RouteProvider）：sdk 的 web 服務裡選單與頁面的四個方法由這裡實作
// （其餘 get_overview、get_trust、get_roster、get_agents、get_health 各由 overview、trust、agents、doctor 登記）。網址不變：/api/web/…。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "web", Impl: &service{m: m}, Methods: []string{"get_menu", "get_pages", "get_menu_prefs", "set_menu_prefs"}}}
}

// service 只有 web 服務的選單四個方法（編譯期檢查簽名跟 sdk.WebAPI 一致）。
type service struct{ m *Module }

var _ interface {
	GetMenu(context.Context, *sdk.WebGetMenuRequest) (*sdk.WebGetMenuResponse, error)
	GetPages(context.Context, *sdk.WebGetPagesRequest) (*sdk.WebGetPagesResponse, error)
	GetMenuPrefs(context.Context, *sdk.WebGetMenuPrefsRequest) (*sdk.WebGetMenuPrefsResponse, error)
	SetMenuPrefs(context.Context, *sdk.WebSetMenuPrefsRequest) (*sdk.WebSetMenuPrefsResponse, error)
} = (*service)(nil)

func (s *service) GetMenu(ctx context.Context, req *sdk.WebGetMenuRequest) (*sdk.WebGetMenuResponse, error) {
	menu, err := s.m.Compute(ctx)
	if err != nil {
		return nil, err
	}
	return &sdk.WebGetMenuResponse{Menu: menu}, nil
}

func (s *service) GetPages(ctx context.Context, req *sdk.WebGetPagesRequest) (*sdk.WebGetPagesResponse, error) {
	pages := make([]sdk.PageInfo, 0, len(coreItems))
	for _, it := range coreItems {
		info := sdk.PageInfo{ID: it.ID, Title: it.Title, Route: it.Route}
		if it.ID == idOverview {
			info.Slots = []sdk.PageSlot{{ID: slotOverviewCards, Page: idOverview}}
		}
		if it.ID == idReminders {
			info.Slots = []sdk.PageSlot{{ID: slotRemindersTop, Page: idReminders}}
		}
		if it.ID == idPersonas {
			info.Slots = []sdk.PageSlot{{ID: slotPersonaTab, Page: idPersonas}, {ID: slotPersonasNew, Page: idPersonas}}
		}
		pages = append(pages, info)
	}
	pages = append(pages, s.m.packagePages(ctx)...)
	return &sdk.WebGetPagesResponse{Pages: pages}, nil
}

func (s *service) GetMenuPrefs(ctx context.Context, req *sdk.WebGetMenuPrefsRequest) (*sdk.WebGetMenuPrefsResponse, error) {
	prefs, isDefault := s.m.effectivePrefs(s.m.packageMenuItems(ctx))
	if isDefault {
		// 預設樹不含 core.settings（Compute 會補在最後）；編輯器要看得到它，所以這裡補上。
		prefs.Tree = append(prefs.Tree, sdk.MenuPrefEntry{Item: idSettings})
	}
	return &sdk.WebGetMenuPrefsResponse{Prefs: prefs}, nil
}

func (s *service) SetMenuPrefs(ctx context.Context, req *sdk.WebSetMenuPrefsRequest) (*sdk.WebSetMenuPrefsResponse, error) {
	if req.By == nil || !platform.IsOwner(req.By.Name) {
		return nil, sdkErr(sdk.ErrorCodeForbidden, "by", "只有使用者本人可以改選單")
	}
	saved, err := s.m.savePrefs(req.Prefs)
	if err != nil {
		return nil, err
	}
	return &sdk.WebSetMenuPrefsResponse{Prefs: saved}, nil
}
