package sdk

import (
	"context"
	"time"
)

// BusAPI provides envelope lifecycle and event operations.
type BusAPI interface {
	// Send 建立一則封包，狀態 QUEUED。同一個 idempotency_key 重送回同一個 id（REQ-12）。
	Send(ctx context.Context, req *BusSendRequest) (*BusSendResponse, error)
	// Get 取得封包與它的生命週期。
	Get(ctx context.Context, req *BusGetRequest) (*BusGetResponse, error)
	// List 列出封包（看板的「排隊中」、除錯頁）。
	List(ctx context.Context, req *BusListRequest) (*BusListResponse, error)
	// Watch 訂閱封包的生命週期變化（看板顯示 排隊→已通知→已收→處理中→已回覆）。
	Watch(ctx context.Context, req *BusWatchRequest, send func(*BusWatchResponse) error) error
	// Publish 發一個事件（模組之間往上層通知，D-5；topic 命名見各模組卡）。
	Publish(ctx context.Context, req *BusPublishRequest) (*BusPublishResponse, error)
	// Subscribe 訂閱事件。
	Subscribe(ctx context.Context, req *BusSubscribeRequest, send func(*BusSubscribeResponse) error) error
}

type EnvelopeKind string

const (
	EnvelopeKindEcho     EnvelopeKind = "echo"
	EnvelopeKindChat     EnvelopeKind = "chat"     // 使用者說話
	EnvelopeKindSay      EnvelopeKind = "say"      // 秘書傳話
	EnvelopeKindDelegate EnvelopeKind = "delegate" // 大腦交辦分身
	EnvelopeKindReport   EnvelopeKind = "report"   // 分身回報
	EnvelopeKindAnswer   EnvelopeKind = "answer"   // 按鈕答案
	EnvelopeKindRoutine  EnvelopeKind = "routine"  // 排程指令
)

type EnvelopeRoute string

const (
	EnvelopeRouteCore   EnvelopeRoute = "core"
	EnvelopeRouteDirect EnvelopeRoute = "direct"
)

type Lifecycle string

const (
	LifecycleUnanswered  Lifecycle = "unanswered"
	LifecycleInterrupted Lifecycle = "interrupted"
	LifecycleQueued      Lifecycle = "queued"   // 排隊
	LifecycleNotified    Lifecycle = "notified" // 已通知 session 有信
	LifecycleReceived    Lifecycle = "received" // 收件方開始處理
	LifecycleWorking     Lifecycle = "working"  // 處理中
	LifecycleReplied     Lifecycle = "replied"  // 已回覆
	LifecycleBlocked     Lifecycle = "blocked"  // 卡在等確認
	LifecycleFailed      Lifecycle = "failed"
)

type LifecycleStep struct {
	State  Lifecycle `json:"state,omitempty"`
	At     time.Time `json:"at,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

// Envelope 對應 schema message 的 B 類（佇列封包）。
type Envelope struct {
	Route          EnvelopeRoute   `json:"route,omitempty"`
	Reason         string          `json:"reason,omitempty"`
	LastMessage    string          `json:"last_message,omitempty"`
	ID             string          `json:"id,omitempty"` // ULID
	From           string          `json:"from,omitempty"`
	To             string          `json:"to,omitempty"` // 目標 session 名稱
	Kind           EnvelopeKind    `json:"kind,omitempty"`
	Body           string          `json:"body,omitempty"`
	Lifecycle      Lifecycle       `json:"lifecycle,omitempty"`
	History        []LifecycleStep `json:"history,omitempty"`
	Attempts       int32           `json:"attempts"` // 通知 session 的次數
	ReplyTo        string          `json:"reply_to,omitempty"`
	Notice         string          `json:"notice,omitempty"` // 對應的通知 M-<n>
	CreatedAt      time.Time       `json:"created_at,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

type BusSendRequest struct {
	From           string       `json:"from,omitempty"`
	To             string       `json:"to,omitempty"`
	Kind           EnvelopeKind `json:"kind,omitempty"`
	Body           string       `json:"body,omitempty"`
	ReplyTo        string       `json:"reply_to,omitempty"`
	Notice         string       `json:"notice,omitempty"`
	IdempotencyKey string       `json:"idempotency_key,omitempty"`
}

type BusSendResponse struct {
	Envelope *Envelope `json:"envelope,omitempty"`
}

type BusGetRequest struct {
	ID string `json:"id,omitempty"`
}

type BusGetResponse struct {
	Envelope *Envelope `json:"envelope,omitempty"`
}

type BusListRequest struct {
	To     string       `json:"to,omitempty"`
	States []Lifecycle  `json:"states,omitempty"`
	Page   *PageRequest `json:"page,omitempty"`
}

type BusListResponse struct {
	Envelopes []Envelope    `json:"envelopes,omitempty"`
	Page      *PageResponse `json:"page,omitempty"`
}

type BusWatchRequest struct {
	To string `json:"to,omitempty"` // 空表示全部
	ID string `json:"id,omitempty"` // 只看某一則
}

type BusWatchResponse struct {
	Envelope *Envelope      `json:"envelope,omitempty"`
	Step     *LifecycleStep `json:"step,omitempty"`
}

type BusPublishRequest struct {
	Topic   string         `json:"topic,omitempty"` // 例 task.changed、reminder.due、session.state
	Payload map[string]any `json:"payload,omitempty"`
	By      *Actor         `json:"by,omitempty"`
}

type BusPublishResponse struct {
}

type BusSubscribeRequest struct {
	Topics []string `json:"topics,omitempty"` // 支援結尾 * 比對
}

type BusSubscribeResponse struct {
	Topic   string         `json:"topic,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
	At      time.Time      `json:"at,omitempty"`
}
