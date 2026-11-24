package notify

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的 sdk 服務（app.RouteProvider）：服務名 "notify"，實作是下面的 service。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "notify", Impl: &service{m: m}}}
}

// service 實作 sdk.NotifyAPI：驗操作者、直接呼叫本模組、把錯誤轉成 API 錯誤。
type service struct{ m *Module }

var _ sdk.NotifyAPI = (*service)(nil)

// markReadBy 是 MarkRead 記的操作者：請求本身不帶操作者，只有主人從看板標已讀。
const markReadBy = "owner(api)"

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if out.Code == sdk.ErrorCodeInternal && s.m.log != nil && !errors.Is(err, context.Canceled) {
		s.m.log.Error("request failed", "error", err)
	}
	return out
}

func (s *service) Send(ctx context.Context, req *sdk.NotifySendRequest) (*sdk.NotifySendResponse, error) {
	n, err := s.m.Send(ctx, &sdk.Notification{Category: req.Category, Title: req.Title, Context: req.Context, Todo: req.Todo, Options: req.Options, URL: req.URL, Image: req.Image, Agent: req.Agent, Room: req.Room, Task: req.Task, Reminder: req.Reminder, Urgent: req.Urgent, Inline: req.Inline, Lane: req.Lane, Link: req.Link})
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.NotifySendResponse{Notification: n}, nil
}

func (s *service) Get(ctx context.Context, req *sdk.NotifyGetRequest) (*sdk.NotifyGetResponse, error) {
	n, err := s.m.Get(ctx, req.ID)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.NotifyGetResponse{Notification: n}, nil
}

func (s *service) List(ctx context.Context, req *sdk.NotifyListRequest) (*sdk.NotifyListResponse, error) {
	list, err := s.m.List(ctx, req.States, req.Agent, req.IncludeArchived)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.NotifyListResponse{Notifications: list, Unread: int32(s.m.Unread())}, nil
}

func (s *service) MarkRead(ctx context.Context, req *sdk.NotifyMarkReadRequest) (*sdk.NotifyMarkReadResponse, error) {
	if err := s.m.MarkRead(ctx, req.IDS, markReadBy); err != nil {
		return nil, s.fail(err)
	}
	return &sdk.NotifyMarkReadResponse{}, nil
}

// Archive 是看板的刪除：只有主人能刪（誰能刪由 Module.Archive 判斷）。
func (s *service) Archive(ctx context.Context, req *sdk.NotifyArchiveRequest) (*sdk.NotifyArchiveResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	archived, skipped, err := s.m.Archive(ctx, req.IDS, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.NotifyArchiveResponse{Archived: archived, Skipped: skipped}, nil
}

// Answer 是按鈕與打字回答的路徑（NTF-8、NTF-14）：記下回答並發 notice.answered，擁有那則通知的模組照事件行動。
func (s *service) Answer(ctx context.Context, req *sdk.NotifyAnswerRequest) (*sdk.NotifyAnswerResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	n, err := s.m.Answer(ctx, req.ID, req.Text, req.Via, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.NotifyAnswerResponse{Notification: n, Delivered: n.Answer != nil && !n.Answer.DeliveredAt.IsZero()}, nil
}

func (s *service) GetPrefs(ctx context.Context, req *sdk.NotifyGetPrefsRequest) (*sdk.NotifyGetPrefsResponse, error) {
	p, err := s.m.GetPrefs(ctx)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.NotifyGetPrefsResponse{Prefs: p}, nil
}

func (s *service) SetPrefs(ctx context.Context, req *sdk.NotifySetPrefsRequest) (*sdk.NotifySetPrefsResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	p, err := s.m.SetPrefsExpand(ctx, req.Prefs, name, req.Expand)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.NotifySetPrefsResponse{Prefs: p}, nil
}

func (s *service) Watch(ctx context.Context, req *sdk.NotifyWatchRequest, send func(*sdk.NotifyWatchResponse) error) error {
	ch, err := s.m.Watch(ctx)
	if err != nil {
		return s.fail(err)
	}
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

// Share 把本機檔案複製進分享資料夾並回 /share 網址（REQ-34）。
// 安全限制在 Module.Share；請求的 Agent 只說是誰分享的。
func (s *service) Share(ctx context.Context, req *sdk.ShareRequest) (*sdk.ShareResponse, error) {
	out, err := s.m.Share(ctx, req.Path)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

// 下面四個是看板「裝置推播」頁用的：誰能管理由 Module 判斷（by 要以 owner( 開頭，REQ-28）。
func (s *service) PushKey(ctx context.Context, req *sdk.NotifyPushKeyRequest) (*sdk.NotifyPushKeyResponse, error) {
	out, err := s.m.PushKey(ctx)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

func (s *service) SubscribePush(ctx context.Context, req *sdk.NotifySubscribePushRequest) (*sdk.NotifySubscribePushResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	n, err := s.m.SubscribePush(ctx, req.Endpoint, req.P256dh, req.Auth, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.NotifySubscribePushResponse{Subscriptions: n}, nil
}

func (s *service) UnsubscribePush(ctx context.Context, req *sdk.NotifyUnsubscribePushRequest) (*sdk.NotifyUnsubscribePushResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	n, err := s.m.UnsubscribePush(ctx, req.Endpoint, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.NotifyUnsubscribePushResponse{Subscriptions: n}, nil
}

func (s *service) PushTest(ctx context.Context, req *sdk.NotifyPushTestRequest) (*sdk.NotifyPushTestResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	if err := s.m.PushTest(ctx, req.Endpoint, name); err != nil {
		return nil, s.fail(err)
	}
	return &sdk.NotifyPushTestResponse{}, nil
}

func (s *service) Nudge(ctx context.Context, req *sdk.NotifyNudgeRequest) (*sdk.NotifyNudgeResponse, error) {
	n, err := s.m.Nudge(ctx, req)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.NotifyNudgeResponse{Nudge: n}, nil
}

// Presence 與 ResolveNudge 只給主人（by 以 owner( 開頭，由 Module 判斷）：看板在前景時回報在看哪裡、按「知道了」。
func (s *service) Presence(ctx context.Context, req *sdk.NotifyPresenceRequest) (*sdk.NotifyPresenceResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	out, err := s.m.Presence(ctx, req, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

func (s *service) ResolveNudge(ctx context.Context, req *sdk.NotifyResolveNudgeRequest) (*sdk.NotifyResolveNudgeResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	if err := s.m.ResolveNudge(ctx, req.ID, name); err != nil {
		return nil, s.fail(err)
	}
	return &sdk.NotifyResolveNudgeResponse{}, nil
}
