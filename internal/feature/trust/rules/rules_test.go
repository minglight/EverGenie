package rules

import (
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func TestLevelDefaultsAndPrecedence(t *testing.T) {
	for _, id := range []string{"code", "life", ""} {
		if LevelFor(platform.Trust{}, id) != sdk.PermissionLevelFull {
			t.Fatal(id)
		}
	}
	config := platform.Trust{DefaultLevel: "readonly", Agents: map[string]platform.TrustAgent{"code": {Level: "standard"}, "work": {Level: " Full "}}}
	for id, want := range map[string]sdk.PermissionLevel{"code": sdk.PermissionLevelStandard, "work": sdk.PermissionLevelFull, "life": sdk.PermissionLevelReadOnly} {
		if got := LevelFor(config, id); got != want {
			t.Fatalf("%s: %s != %s", id, got, want)
		}
	}
}
func TestPolicyPreservesActionsAndRepos(t *testing.T) {
	push := true
	p := Policy(platform.Trust{Approve: map[string]any{"actions": []any{"read", "commit"}}, Deny: map[string]any{"actions": []any{"delete"}}, Repos: []platform.TrustedRepo{{Path: "/fixture", Push: &push, Writers: []string{"code"}}}})
	if p.Mode != sdk.PermissionModeAsk || len(p.Allow) != 2 || p.Allow[1] != "commit" || len(p.Deny) != 1 || p.Deny[0] != "delete" || !p.Repos[0].Push {
		t.Fatal(p)
	}
}
