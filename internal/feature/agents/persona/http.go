package persona

import (
	"context"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的 sdk 服務（app.RouteProvider）：服務名 "personas"，整個服務由這裡實作。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "personas", Impl: &service{m: m}}}
}

// service 實作 sdk.PersonasAPI：驗操作者、直接呼叫角色庫、把錯誤轉成 API 錯誤。
// 寫入類方法的權限（只有主人）由角色庫判斷。
type service struct{ m *Module }

var _ sdk.PersonasAPI = (*service)(nil)

func (s *service) fail(err error) error { return platform.SDKError(err) }

// write 取操作者名字。
func (s *service) write(actor *sdk.Actor) (string, error) { return platform.ActorName(actor) }

func (s *service) List(ctx context.Context, req *sdk.PersonasListRequest) (*sdk.PersonasListResponse, error) {
	rows, err := s.m.Library(ctx)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PersonasListResponse{Personas: rows}, nil
}

func (s *service) Get(ctx context.Context, req *sdk.PersonasGetRequest) (*sdk.PersonasGetResponse, error) {
	card, body, raw, err := s.m.ShowPersona(ctx, req.Slug)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PersonasGetResponse{Persona: &card, Body: body, Raw: raw}, nil
}

func (s *service) Create(ctx context.Context, req *sdk.PersonasCreateRequest) (*sdk.PersonasCreateResponse, error) {
	who, err := s.write(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	var card sdk.PersonaCard
	if req.Package != "" {
		card, err = s.m.CreatePersonaIn(ctx, req.Package, req.Slug, req.Name, req.Aka, who)
	} else {
		card, err = s.m.CreatePersona(ctx, req.Slug, req.Name, req.Aka, who)
	}
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PersonasCreateResponse{Persona: &card}, nil
}

func (s *service) Copy(ctx context.Context, req *sdk.PersonasCopyRequest) (*sdk.PersonasCopyResponse, error) {
	who, err := s.write(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	var card sdk.PersonaCard
	if req.Package != "" {
		card, err = s.m.CopyPersonaIn(ctx, req.Package, req.From, req.Slug, req.Name, req.Aka, who)
	} else {
		card, err = s.m.CopyPersona(ctx, req.From, req.Slug, req.Name, req.Aka, who)
	}
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PersonasCopyResponse{Persona: &card}, nil
}

func (s *service) Update(ctx context.Context, req *sdk.PersonasUpdateRequest) (*sdk.PersonasUpdateResponse, error) {
	who, err := s.write(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	card, backup, err := s.m.UpdatePersona(ctx, req.Slug, req.Name, req.Aka, req.Body, req.BaseUpdatedAt, who)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PersonasUpdateResponse{Persona: &card, Backup: backup}, nil
}

func (s *service) SetStatus(ctx context.Context, req *sdk.PersonasSetStatusRequest) (*sdk.PersonasSetStatusResponse, error) {
	who, err := s.write(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	card, err := s.m.SetPersonaStatus(ctx, req.Slug, req.Status, who)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PersonasSetStatusResponse{Persona: &card}, nil
}

func (s *service) Check(ctx context.Context, req *sdk.PersonasCheckRequest) (*sdk.PersonasCheckResponse, error) {
	problems, err := s.m.CheckPersonas(ctx, req.Slug)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PersonasCheckResponse{Problems: problems}, nil
}

func (s *service) GetStyle(ctx context.Context, req *sdk.PersonasGetStyleRequest) (*sdk.PersonasGetStyleResponse, error) {
	level, text, exists, levels, err := s.m.StyleInfo(ctx)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PersonasGetStyleResponse{Level: level, Text: text, CustomExists: exists, Levels: levels}, nil
}

func (s *service) SetStyle(ctx context.Context, req *sdk.PersonasSetStyleRequest) (*sdk.PersonasSetStyleResponse, error) {
	who, err := s.write(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	backup, err := s.m.SetStyle(ctx, req.Level, who)
	if err != nil {
		return nil, s.fail(err)
	}
	level, text, exists, _, err := s.m.StyleInfo(ctx)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PersonasSetStyleResponse{Level: level, Text: text, CustomExists: exists, Backup: backup}, nil
}

func (s *service) tester() (Tester, error) {
	s.m.setMu.RLock()
	defer s.m.setMu.RUnlock()
	if s.m.tester == nil {
		return nil, platform.Unavailable("說話測試還沒接上")
	}
	return s.m.tester, nil
}

// Test 誰都可以叫（主人、秘書）；花的是座位那個帳號的額度。
func (s *service) Test(ctx context.Context, req *sdk.PersonasTestRequest) (*sdk.PersonasTestResponse, error) {
	t, err := s.tester()
	if err != nil {
		return nil, s.fail(err)
	}
	out, err := t.TestPersona(ctx, req)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

func (s *service) TestResults(ctx context.Context, req *sdk.PersonasTestResultsRequest) (*sdk.PersonasTestResultsResponse, error) {
	t, err := s.tester()
	if err != nil {
		return nil, s.fail(err)
	}
	out, err := t.PersonaTestResults(ctx, req)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

// PackStatus 角色包現況（REQ-283）：讀家裡的登記檔、指標與 runtime/personas-pack/，不連網路。誰都可以看。
func (s *service) PackStatus(ctx context.Context, req *sdk.PersonasPackStatusRequest) (*sdk.PersonasPackStatusResponse, error) {
	st, err := personapack.Status(s.m.home, s.m.packPointer())
	if err != nil {
		return nil, s.fail(err)
	}
	out := &sdk.PersonasPackStatusResponse{Registered: st.Registered, Latest: st.Latest, Source: st.Source}
	if st.Pending != nil {
		out.Pending = &sdk.PersonaPackPending{Version: st.Pending.Version, Reason: st.Pending.Reason, At: st.Pending.At}
	}
	if st.Last != nil {
		out.Last = &sdk.PersonaPackLast{Status: st.Last.Status, Version: st.Last.Version, Installed: int32(len(st.Last.Installed)), Unchanged: int32(st.Last.Unchanged), UserEdited: st.Last.UserEdited, Reason: st.Last.Reason, At: st.Last.At,
			RecordsLater: st.Last.RecordsLater, Message: st.Last.Message()}
	}
	return out, nil
}

// PackSync 請服務同步角色包（DEV-157、PRS-28）：誰都可以叫；force、或指定跟服務不一樣的指標檔只有主人（與系統）。
// 同步沒成功也回結果（status failed），只有根本沒進行（家讀不懂、指標格式不對）才回錯誤。
func (s *service) PackSync(ctx context.Context, req *sdk.PersonasPackSyncRequest) (*sdk.PersonasPackSyncResponse, error) {
	who, err := s.write(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	if req.Force || (req.Pointer != "" && !samePointer(req.Pointer, s.m.packPointer())) {
		if !platform.IsOwnerOrSystem(who) {
			return nil, s.fail(platform.Forbidden("只有主人能強制蓋掉改過的角色包檔案，或指定別的角色包指標"))
		}
	}
	res, err := s.m.SyncPack(ctx, PackSyncOptions{Force: req.Force, Pointer: req.Pointer})
	if res == nil {
		return nil, s.fail(err)
	}
	return packSyncResponse(res), nil
}

// packSyncResponse 把同步結果換成 API 的回應（指令收到後再換回 SyncResult，--json 印的跟以前一樣）。
func packSyncResponse(r *personapack.SyncResult) *sdk.PersonasPackSyncResponse {
	return &sdk.PersonasPackSyncResponse{Status: r.Status, Version: r.Version, Registered: r.Registered, Installed: r.Installed, Unchanged: int32(r.Unchanged),
		UserEdited: r.UserEdited, Backup: r.Backup, NewCards: r.NewCards, RecordsAdded: r.RecordsAdded, RecordsReplaced: r.RecordsReplaced,
		RecordsUnchanged: int32(r.RecordsUnchanged), RecordsKept: r.RecordsKept, RecordsSkipped: r.RecordsSkipped, RecordsLater: r.RecordsLater,
		Reason: r.Reason, Message: r.Message()}
}

// syncResultFrom 是 packSyncResponse 反過來。
func syncResultFrom(r *sdk.PersonasPackSyncResponse) *personapack.SyncResult {
	return &personapack.SyncResult{Status: r.Status, Version: r.Version, Registered: r.Registered, Installed: r.Installed, Unchanged: int(r.Unchanged),
		UserEdited: r.UserEdited, Backup: r.Backup, NewCards: r.NewCards, RecordsAdded: r.RecordsAdded, RecordsReplaced: r.RecordsReplaced,
		RecordsUnchanged: int(r.RecordsUnchanged), RecordsKept: r.RecordsKept, RecordsSkipped: r.RecordsSkipped, RecordsLater: r.RecordsLater, Reason: r.Reason}
}
