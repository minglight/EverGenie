package chat

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的 sdk 服務（app.RouteProvider）：傳話 bus、對話 session、引擎回報 engine_host。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{
		{Service: "bus", Impl: &busService{m: m}},
		{Service: "session", Impl: &sessionService{m: m}},
		{Service: "engine_host", Impl: &hostService{m: m}},
		{Service: "web", Impl: &bootPromptService{m: m}, Methods: []string{"get_boot_prompts", "answer_boot_prompt"}},
		{Service: "web", Impl: &voiceService{m: m}, Methods: []string{"voice_send", "voice_answer"}}, // 語音入口（CHT-9、CHT-10）
	}
}

var (
	_ sdk.BusAPI        = (*busService)(nil)
	_ sdk.SessionAPI    = (*sessionService)(nil)
	_ sdk.EngineHostAPI = (*hostService)(nil)
)

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (m *Module) fail(err error) error {
	out := platform.SDKError(err)
	if out.Code == sdk.ErrorCodeInternal && m.log != nil && !errors.Is(err, context.Canceled) {
		m.log.Error("request failed", "error", err)
	}
	return out
}

// by 是每個寫入都要帶的操作者名稱。
func by(a *sdk.Actor) (string, error) { return platform.ActorName(a) }

// pump 把模組的 channel 轉給串流的 send，直到任何一邊結束。
func pump[T any](ctx context.Context, ch <-chan T, send func(T) error) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case v, ok := <-ch:
			if !ok {
				return nil
			}
			if err := send(v); err != nil {
				return err
			}
		}
	}
}

func values[T any](in []*T) []T {
	out := make([]T, 0, len(in))
	for _, v := range in {
		if v != nil {
			out = append(out, *v)
		}
	}
	return out
}

// ---- bus ----

type busService struct{ m *Module }

func (s *busService) Send(ctx context.Context, req *sdk.BusSendRequest) (*sdk.BusSendResponse, error) {
	e, err := s.m.d.Bus.Send(ctx, &sdk.Envelope{From: req.From, To: req.To, Kind: req.Kind, Body: req.Body, ReplyTo: req.ReplyTo, Notice: req.Notice, IdempotencyKey: req.IdempotencyKey})
	if err != nil {
		return nil, s.m.fail(err)
	}
	return &sdk.BusSendResponse{Envelope: e}, nil
}
func (s *busService) Get(ctx context.Context, req *sdk.BusGetRequest) (*sdk.BusGetResponse, error) {
	e, err := s.m.d.Bus.Get(ctx, req.ID)
	if err != nil {
		return nil, s.m.fail(err)
	}
	return &sdk.BusGetResponse{Envelope: e}, nil
}
func (s *busService) List(ctx context.Context, req *sdk.BusListRequest) (*sdk.BusListResponse, error) {
	list, err := s.m.d.Bus.List(ctx, req.To, req.States...)
	if err != nil {
		return nil, s.m.fail(err)
	}
	return &sdk.BusListResponse{Envelopes: values(list)}, nil
}
func (s *busService) Watch(ctx context.Context, req *sdk.BusWatchRequest, send func(*sdk.BusWatchResponse) error) error {
	ch, err := s.m.d.Bus.Watch(ctx, req.To, req.ID)
	if err != nil {
		return s.m.fail(err)
	}
	return pump(ctx, ch, send)
}
func (s *busService) Publish(ctx context.Context, req *sdk.BusPublishRequest) (*sdk.BusPublishResponse, error) {
	if _, err := by(req.By); err != nil {
		return nil, s.m.fail(err)
	}
	if err := s.m.d.Bus.Publish(ctx, req.Topic, req.Payload); err != nil {
		return nil, s.m.fail(err)
	}
	return &sdk.BusPublishResponse{}, nil
}
func (s *busService) Subscribe(ctx context.Context, req *sdk.BusSubscribeRequest, send func(*sdk.BusSubscribeResponse) error) error {
	ch, err := s.m.d.Bus.Subscribe(ctx, req.Topics...)
	if err != nil {
		return s.m.fail(err)
	}
	return pump(ctx, ch, send)
}

// ---- session ----

type sessionService struct{ m *Module }

// ids 是這個家的編號字首（home.toml [ids]）：Topic 是不是任務單號照它認（CHT-2、GOAL-2 W3）。
func (s *sessionService) ids() platform.IDPrefixes { return platform.ReadIDPrefixes(s.m.home) }

func (s *sessionService) List(ctx context.Context, req *sdk.SessionListRequest) (*sdk.SessionListResponse, error) {
	list, err := s.m.d.Sessions.List(ctx, req.Agent, req.States)
	if err != nil {
		return nil, s.m.fail(err)
	}
	out := values(list)

	return &sdk.SessionListResponse{Sessions: out}, nil
}
func (s *sessionService) Get(ctx context.Context, req *sdk.SessionGetRequest) (*sdk.SessionGetResponse, error) {

	v, err := s.m.d.Sessions.Get(ctx, req.Name)
	if err != nil {
		return nil, s.m.fail(err)
	}
	if mf, ok := s.m.d.Sessions.(messageFiller); ok {
		mf.WithMessages(ctx, v)
	}
	return &sdk.SessionGetResponse{Session: v}, nil
}

// messageFiller 把送進 session、還沒結束的話填進 Session.Messages（session.Module 符合，REQ-302）。
type messageFiller interface {
	WithMessages(ctx context.Context, list ...*sdk.Session)
}

// Open starts a session. A ticket's work session (Topic <tasks prefix><n> from home.toml [ids], kind work) is named
// <agent>-<prefix lowercased><n>; the session module decides how to open it and sends the first message (CHT-2, REQ-270). When the
// secretary lives on another computer, that computer opens it (REQ-271).
func (s *sessionService) Open(ctx context.Context, req *sdk.SessionOpenRequest) (*sdk.SessionOpenResponse, error) {

	spec := *req
	if !s.ids().IsTask(spec.Topic) {
		v, err := s.m.d.Sessions.Open(ctx, &spec)
		if err != nil {
			return nil, s.m.fail(err)
		}
		return &sdk.SessionOpenResponse{Session: v}, nil
	}
	task, err := s.m.d.Tasks.Get(ctx, spec.Topic)
	if err != nil {
		return nil, s.m.fail(err)
	}

	agent := spec.Agent
	if agent == "" {
		agent = task.Agent
	}
	v, err := s.m.d.Sessions.OpenWork(ctx, spec.Topic, agent, spec.By)
	if err != nil {
		return nil, s.m.fail(err)
	}
	return &sdk.SessionOpenResponse{Session: v}, nil
}

// workOwner 讀單（任何佇列都是任務單）決定誰負責、開在哪台：req 給的秘書優先；單上已經寫了別台的工作 session

func (s *sessionService) Close(ctx context.Context, req *sdk.SessionCloseRequest) (*sdk.SessionCloseResponse, error) {

	if err := s.m.d.Sessions.Close(ctx, req.Name); err != nil {
		return nil, s.m.fail(err)
	}
	return &sdk.SessionCloseResponse{}, nil
}
func (s *sessionService) Rotate(ctx context.Context, req *sdk.SessionRotateRequest) (*sdk.SessionRotateResponse, error) {
	if err := s.m.d.Sessions.Rotate(ctx, req.Name); err != nil {
		return nil, s.m.fail(err)
	}
	v, err := s.m.d.Sessions.Get(ctx, req.Name)
	if err != nil {
		return nil, s.m.fail(err)
	}
	return &sdk.SessionRotateResponse{Session: v}, nil
}

// Resolve returns a permission or question answer to the waiting engine (CHT-3).
func (s *sessionService) Resolve(ctx context.Context, req *sdk.SessionResolveRequest) (*sdk.SessionResolveResponse, error) {
	name, err := by(req.By)
	if err != nil {
		return nil, s.m.fail(err)
	}
	if req.Decision == nil {
		return nil, s.m.fail(platform.Invalid("decision", "請提供決定"))
	}

	applied, err := s.m.d.Sessions.Resolve(ctx, req.Session, req.PendingID, req.Decision, name)
	if err != nil {
		return nil, s.m.fail(err)
	}
	return &sdk.SessionResolveResponse{Applied: applied}, nil
}
func (s *sessionService) Chat(ctx context.Context, req *sdk.SessionChatRequest, send func(*sdk.SessionChatResponse) error) error {

	ch, err := s.m.d.Sessions.Chat(ctx, req.Session, req.SinceUuid, req.Follow)
	if err != nil {
		return s.m.fail(err)
	}
	return pump(ctx, ch, func(m *sdk.ChatMessage) error { return send(&sdk.SessionChatResponse{Message: m}) })
}

func (s *sessionService) Watch(ctx context.Context, req *sdk.SessionWatchRequest, send func(*sdk.SessionWatchResponse) error) error {
	lat := s.m.latencyStream()
	measured := func(v *sdk.SessionWatchResponse) error {
		if err := send(v); err != nil {
			return err
		}
		if v != nil {
			lat.sent(v.Session)
		}
		return nil
	}

	ch, err := s.m.d.Sessions.Watch(ctx, req.Agent)
	if err != nil {
		return s.m.fail(err)
	}
	return pump(ctx, ch, measured)
}

func (s *sessionService) ListThreads(ctx context.Context, req *sdk.SessionListThreadsRequest) (*sdk.SessionListThreadsResponse, error) {
	list, waiting, err := s.m.d.Sessions.Threads(ctx, req.Agent, req.IncludeStopped)
	if err != nil {
		return nil, s.m.fail(err)
	}
	out := values(list)

	return &sdk.SessionListThreadsResponse{Threads: out, WaitingUser: int32(waiting)}, nil
}

func (s *sessionService) WatchThreads(ctx context.Context, req *sdk.SessionWatchThreadsRequest, send func(*sdk.SessionWatchThreadsResponse) error) error {
	ch, err := s.m.d.Sessions.WatchThreads(ctx, req.Agent)
	if err != nil {
		return s.m.fail(err)
	}
	return pump(ctx, ch, send)
}
func (s *sessionService) MarkSeen(ctx context.Context, req *sdk.SessionMarkSeenRequest) (*sdk.SessionMarkSeenResponse, error) {
	name, err := by(req.By)
	if err != nil {
		return nil, s.m.fail(err)
	}

	t, err := s.m.d.Sessions.MarkSeen(ctx, req.Session, req.Uuid, name)
	if err != nil {
		return nil, s.m.fail(err)
	}
	return &sdk.SessionMarkSeenResponse{Thread: t}, nil
}

// Command：主人對這段對話下的指令（SES-59）。身分只認 by（看板強制成主人，WEB-83）；白名單與忙碌規則在 session。
func (s *sessionService) Command(ctx context.Context, req *sdk.SessionCommandRequest) (*sdk.SessionCommandResponse, error) {
	name, err := by(req.By)
	if err != nil {
		return nil, s.m.fail(err)
	}
	note, err := s.m.d.Sessions.Command(ctx, req.Session, req.Command, req.Arg, name)
	if err != nil {
		return nil, s.m.fail(err)
	}
	return &sdk.SessionCommandResponse{Note: note}, nil
}

// ---- engine_host ----

// hostService is what the engine process calls; the session module owns the behavior.
type hostService struct{ m *Module }

func (s *hostService) host() (sdk.EngineHostAPI, error) {
	if s.m.d.Sessions == nil {
		return nil, s.m.fail(platform.Unavailable("對話服務尚未啟動"))
	}
	return s.m.d.Sessions.EngineHost(), nil
}
func (s *hostService) ReportEvent(ctx context.Context, req *sdk.EngineHostReportEventRequest) (*sdk.EngineHostReportEventResponse, error) {
	h, err := s.host()
	if err != nil {
		return nil, err
	}
	out, err := h.ReportEvent(ctx, req)
	if err != nil {
		return nil, s.m.fail(err)
	}
	return out, nil
}
func (s *hostService) RequestApproval(ctx context.Context, req *sdk.EngineHostRequestApprovalRequest) (*sdk.EngineHostRequestApprovalResponse, error) {
	h, err := s.host()
	if err != nil {
		return nil, err
	}
	out, err := h.RequestApproval(ctx, req)
	if err != nil {
		return nil, s.m.fail(err)
	}
	return out, nil
}
func (s *hostService) GetContext(ctx context.Context, req *sdk.EngineHostGetContextRequest) (*sdk.EngineHostGetContextResponse, error) {
	h, err := s.host()
	if err != nil {
		return nil, err
	}
	out, err := h.GetContext(ctx, req)
	if err != nil {
		return nil, s.m.fail(err)
	}
	return out, nil
}
func (s *hostService) CheckAction(ctx context.Context, req *sdk.EngineHostCheckActionRequest) (*sdk.EngineHostCheckActionResponse, error) {
	h, err := s.host()
	if err != nil {
		return nil, err
	}
	out, err := h.CheckAction(ctx, req)
	if err != nil {
		return nil, s.m.fail(err)
	}
	return out, nil
}
