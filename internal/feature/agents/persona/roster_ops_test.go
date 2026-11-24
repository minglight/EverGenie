package persona

import (
	"context"
	"strconv"

	"github.com/minglight/evergenie/internal/base/agent"
)

// 換角色、建秘書、改欄位的規則在業務塊 agents（manage.go），業務塊之間不 import；這裡的測試只要名冊照著改，
// 用底層 agent 的單筆操作做這些測試要守的那一段：人設要能指派（LivePersona 擋設計中、套件停用、不存在），再寫名冊。

func assign(ctx context.Context, r *agent.Module, id, persona, by string) error {
	if _, err := r.LivePersona(persona); err != nil {
		return err
	}
	_, err := r.PatchAgent(ctx, id, map[string]any{"persona": persona}, nil, by)
	return err
}

func newAgent(ctx context.Context, r *agent.Module, id, persona, duty, by string) error {
	if _, err := r.LivePersona(persona); err != nil {
		return err
	}
	if duty == "" {
		duty = "（還沒寫）"
	}
	_, err := r.PutAgent(ctx, id, map[string]any{"duty": duty, "persona": persona, "enabled": false}, by)
	return err
}

func setAgentField(ctx context.Context, r *agent.Module, id, field, value, by string) error {
	var v any = value
	if b, err := strconv.ParseBool(value); err == nil && field == "enabled" {
		v = b
	}
	_, err := r.PatchAgent(ctx, id, map[string]any{field: v}, nil, by)
	return err
}
