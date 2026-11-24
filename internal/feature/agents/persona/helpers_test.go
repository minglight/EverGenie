package persona

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

type recordingBus struct{ events []map[string]any }

func (b *recordingBus) Publish(ctx context.Context, topic string, p map[string]any) error {
	p["topic"] = topic
	b.events = append(b.events, p)
	return nil
}

// fixture 組一個真的名冊（agent）加角色庫（本模組），跟 registry 的接法一樣：名冊讀角色卡經角色庫，角色庫向名冊要職位清單。
// 家裡有官方卡 athena、new（舊 TOML 格式），秘書 code、life 都坐 athena。
func fixture(t *testing.T) (*agent.Module, *Module, *homefixture.Home, *recordingBus) {
	t.Helper()
	h := homefixture.New(t)
	b := &recordingBus{}
	m := agent.New(agent.Deps{Vault: h.Vault, Clock: h.Clock, Bus: b})
	m.SetHome(h.Dir)
	lib := New(Deps{Roster: m, Clock: h.Clock})
	lib.SetHome(h.Dir)
	m.SetPersonaSource(lib)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for slug, data := range map[string]string{"athena": "---\nname: Athena\naka: [雅典娜]\n---\n人設\n", "new": "+++\nname = \"New\"\naka = [\"新人\"]\n+++\n新的人設\n"} {
		must(t, os.MkdirAll(filepath.Join(h.Dir, "personas"), 0700))
		must(t, platform.AtomicWrite(filepath.Join(h.Dir, "personas", slug+".md"), []byte(data), 0600))
	}
	for _, id := range []string{"code", "life"} {
		h.WithRecords(vault.Record{Kind: "agent", ID: id, Fields: map[string]any{"duty": "測試職責", "persona": "athena", "former_names": []string{"Old" + id}}, Body: "官方職責"})
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	return m, lib, h, b
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func record(t *testing.T, h *homefixture.Home, kind, id string) vault.Record {
	t.Helper()
	r, err := h.Vault.Get(context.Background(), kind, id)
	must(t, err)
	return r
}

// cardRecord 讀一張卡的檔頭與內文（名冊讀角色卡的那條路：PersonaSource.Card）。
func cardRecord(lib *Module, slug string) (vault.Record, error) {
	info, err := lib.Card(slug)
	return info.Record, err
}

func (b *recordingBus) Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	return nil, nil
}
