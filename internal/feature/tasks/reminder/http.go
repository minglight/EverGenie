package reminder

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的 sdk 服務（app.RouteProvider）：服務名 "reminders"，實作是下面的 service。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "reminders", Impl: &service{m: m}}}
}

// service 實作 sdk.RemindersAPI：驗操作者、直接呼叫本模組、把錯誤轉成 API 錯誤。
type service struct{ m *Module }

var _ sdk.RemindersAPI = (*service)(nil)

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if out.Code == sdk.ErrorCodeInternal && s.m.log != nil && !errors.Is(err, context.Canceled) {
		s.m.log.Error("request failed", "error", err)
	}
	return out
}

func (s *service) Create(ctx context.Context, req *sdk.RemindersCreateRequest) (*sdk.RemindersCreateResponse, error) {
	if _, err := platform.ActorName(req.By); err != nil {
		return nil, s.fail(err)
	}
	r, err := s.m.Create(ctx, req)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.RemindersCreateResponse{Reminder: r}, nil
}

func (s *service) Get(ctx context.Context, req *sdk.RemindersGetRequest) (*sdk.RemindersGetResponse, error) {
	r, err := s.m.Get(ctx, req.ID)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.RemindersGetResponse{Reminder: r}, nil
}

func (s *service) List(ctx context.Context, req *sdk.RemindersListRequest) (*sdk.RemindersListResponse, error) {
	list, err := s.m.List(ctx, req)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.RemindersListResponse{Reminders: values(list)}, nil
}

func (s *service) Update(ctx context.Context, req *sdk.RemindersUpdateRequest) (*sdk.RemindersUpdateResponse, error) {
	by, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	r, err := s.m.Update(ctx, req.ID, req, by)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.RemindersUpdateResponse{Reminder: r}, nil
}

func (s *service) Snooze(ctx context.Context, req *sdk.RemindersSnoozeRequest) (*sdk.RemindersSnoozeResponse, error) {
	by, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	r, err := s.m.Snooze(ctx, req.ID, req.Until, by)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.RemindersSnoozeResponse{Reminder: r}, nil
}

func (s *service) Done(ctx context.Context, req *sdk.RemindersDoneRequest) (*sdk.RemindersDoneResponse, error) {
	by, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	r, err := s.m.Done(ctx, req.ID, by)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.RemindersDoneResponse{Reminder: r}, nil
}

func (s *service) Doing(ctx context.Context, req *sdk.RemindersDoingRequest) (*sdk.RemindersDoingResponse, error) {
	by, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	r, err := s.m.Doing(ctx, req.ID, by)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.RemindersDoingResponse{Reminder: r}, nil
}

func (s *service) Cancel(ctx context.Context, req *sdk.RemindersCancelRequest) (*sdk.RemindersCancelResponse, error) {
	by, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	r, err := s.m.Cancel(ctx, req.ID, by)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.RemindersCancelResponse{Reminder: r}, nil
}

// values 把指標切片換成值切片（回應用值，nil 項目略過）。
func values(in []Reminder) []sdk.Reminder {
	out := make([]sdk.Reminder, 0, len(in))
	for _, v := range in {
		if v != nil {
			out = append(out, *v)
		}
	}
	return out
}
