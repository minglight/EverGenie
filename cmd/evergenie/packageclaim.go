package main

import (
	"context"

	"github.com/minglight/evergenie/internal/feature/agents/persona"
	"github.com/minglight/evergenie/internal/feature/agents/roster"
	"github.com/minglight/evergenie/internal/feature/inbox/notify"
	"github.com/minglight/evergenie/internal/feature/packages/pkg"
	"github.com/minglight/evergenie/sdk"
)

// packageClaim 把 pkg 的認領職位通知（SEC-302 S9）接到角色庫、通知與名冊：pkg 不直接依賴 notify，由這裡組裝。
type packageClaim struct {
	roster   *roster.Module // 名冊管理（agents 塊的 roster）：打開秘書
	personas *persona.Module
	notices  *notify.Module
}

var _ pkg.ClaimHost = packageClaim{}

func (c packageClaim) Personas(ctx context.Context) ([]sdk.PersonaCard, error) {
	return c.personas.Library(ctx)
}

func (c packageClaim) Notify(ctx context.Context, n *sdk.Notification) (*sdk.Notification, error) {
	return c.notices.Send(ctx, n)
}

func (c packageClaim) EnableAgent(ctx context.Context, id, by string) error {
	_, err := c.roster.SetField(ctx, id, "enabled", "true", by)
	return err
}

// personaNotices 把角色庫的「新角色來打招呼」（DEV-115）接到通知：角色庫不直接依賴 notify，由這裡組裝。
type personaNotices struct{ notices *notify.Module }

var _ persona.Notifier = personaNotices{}

func (p personaNotices) Notify(ctx context.Context, n *sdk.Notification) (*sdk.Notification, error) {
	return p.notices.Send(ctx, n)
}

// packages 要做到角色庫要的兩件事（列出、寫入套件角色；介面由角色庫定義）。
var (
	_ persona.PackageSource = (*pkg.Module)(nil)
	_ persona.PackageWriter = (*pkg.Module)(nil)
)
