package pkg

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// stubLib 是測試用的角色庫替身。業務模組之間不互相 import（packages 不能 import personas），
// 所以這裡只做 packages 的測試要的：給名冊讀卡的 Card 與 Style（agent.PersonaSource）、列表（認領用）、直接改卡的狀態。
// 角色庫自己的行為（新增、複製、檢查、狀態規則）在 internal/feature/agents/persona 的測試。
type stubLib struct {
	m      *Module
	home   string
	roster *agent.Module // 有給才算坐位（認領挑角色要看誰已經有人坐）
}

var _ agent.PersonaSource = stubLib{}

func (s stubLib) Card(slug string) (agent.PersonaCardInfo, error) {
	bad := platform.Invalid("persona", "人設卡不存在或無法讀取")
	if pkgID, sl, ok := personaref.SplitPackageCode(slug); ok {
		for _, pp := range s.m.PackagePersonas() {
			if pp.Package != pkgID || pp.Slug != sl {
				continue
			}
			data, err := os.ReadFile(pp.Path)
			if err != nil {
				return agent.PersonaCardInfo{}, bad
			}
			rec, err := personaref.ParseCard(data)
			info := agent.PersonaCardInfo{Record: rec, Source: personaref.SourcePackage, PackageID: pp.Package, Status: personaref.StatusLive}
			if st, _ := rec.Fields["status"].(string); strings.TrimSpace(st) == personaref.StatusDesign {
				info.Status = personaref.StatusDesign // 套件的卡能是設計中（跟角色庫同規則）
			}
			if !pp.Active {
				if info.Inactive = pp.Reason; info.Inactive == "" {
					info.Inactive = "已停用"
				}
			}
			return info, err
		}
		return agent.PersonaCardInfo{}, bad
	}
	data, err := os.ReadFile(filepath.Join(s.home, "personas", slug+".md"))
	if err != nil {
		return agent.PersonaCardInfo{}, bad
	}
	rec, err := personaref.ParseCard(data)
	return agent.PersonaCardInfo{Record: rec, Source: personaref.SourceOfficial, Status: personaref.StatusLive}, err
}

func (stubLib) Style() (string, string, bool, error) { return "c", "", false, nil }

// Library 列出官方卡與套件的卡（認領挑角色用的欄位：代號、名字、來源、所屬套件、狀態、不能指派的原因）。
func (s stubLib) Library(ctx context.Context) ([]sdk.PersonaCard, error) {
	seats := map[string][]string{}
	if s.roster != nil {
		rows, err := s.roster.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if p, _ := r.Fields["persona"].(string); p != "" {
				seats[p] = append(seats[p], r.ID)
			}
		}
	}
	var out []sdk.PersonaCard
	files, _ := filepath.Glob(filepath.Join(s.home, "personas", "*.md"))
	sort.Strings(files)
	for _, f := range files {
		slug := strings.TrimSuffix(filepath.Base(f), ".md")
		if info, err := s.Card(slug); err == nil {
			name, _ := info.Record.Fields["name"].(string)
			out = append(out, sdk.PersonaCard{Slug: slug, Name: name, Source: personaref.SourceOfficial, Status: personaref.StatusLive, Seats: seats[slug]})
		}
	}
	for _, pp := range s.m.PackagePersonas() {
		code := personaref.PackageCode(pp.Package, pp.Slug)
		info, err := s.Card(code)
		if err != nil {
			continue
		}
		name, _ := info.Record.Fields["name"].(string)
		out = append(out, sdk.PersonaCard{Slug: code, Name: name, Source: personaref.SourcePackage, Package: pp.Package,
			Status: info.Status, Unassignable: info.Inactive, Seats: seats[code]})
	}
	return out, nil
}

// setStatus 直接改套件裡那張卡的 status（上線＝拿掉欄位）。
func (s stubLib) setStatus(t *testing.T, code, status string) {
	t.Helper()
	pkgID, sl, _ := personaref.SplitPackageCode(code)
	for _, pp := range s.m.PackagePersonas() {
		if pp.Package != pkgID || pp.Slug != sl {
			continue
		}
		data, err := os.ReadFile(pp.Path)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Replace(string(data), "status: design\n", "", 1)
		if status == personaref.StatusDesign {
			text = strings.Replace(text, "schema: 1\n", "schema: 1\nstatus: design\n", 1)
		}
		if err := os.WriteFile(pp.Path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("找不到 %s", code)
}

// addCard 用套件的寫入介面新增一張設計中的卡（跟角色庫新增去套件時一樣的呼叫）。
func addCard(t *testing.T, m *Module, pkgID, slug, name string) {
	t.Helper()
	card := "---\nschema: 1\nname: " + name + "\nstatus: design\n---\n## 個性\n- 一句話：測試用\n"
	if _, err := m.AddPackagePersona(pkgID, slug, []byte(card)); err != nil {
		t.Fatalf("新增 %s.%s：%v", pkgID, slug, err)
	}
}
