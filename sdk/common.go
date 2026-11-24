package sdk

import (
	"time"
)

// ErrorCode 是跨模組、跨行程共用的錯誤分類（錯誤處理規範 errors）。
type ErrorCode string

const (
	ErrorCodeInvalid     ErrorCode = "invalid" // 欄位不合 schema、參數錯誤
	ErrorCodeNotFound    ErrorCode = "not_found"
	ErrorCodeConflict    ErrorCode = "conflict"    // 例如 id 已存在、狀態不允許這個轉移
	ErrorCodeForbidden   ErrorCode = "forbidden"   // 規則不允許（例如秘書不能改 Goal 原文）
	ErrorCodeUnavailable ErrorCode = "unavailable" // 對方暫時不在（可以重試）
	ErrorCodeInternal    ErrorCode = "internal"
)

// Error 是 HTTP 錯誤的 JSON 內容；message 是給人看的白話。
type Error struct {
	Code    ErrorCode         `json:"code"`
	Message string            `json:"message"`
	Field   string            `json:"field,omitempty"` // 出錯的欄位（若有）
	Details map[string]string `json:"details,omitempty"`
}

// Page 是列表 API 共用的分頁。
type PageRequest struct {
	Limit  int32  `json:"limit"` // 0 表示用伺服器預設
	Cursor string `json:"cursor,omitempty"`
}

type PageResponse struct {
	NextCursor string `json:"next_cursor,omitempty"` // 空字串表示沒有下一頁
}

// Actor 記錄「誰做的」（對應各 schema 的 by 欄位）。
type Actor struct {
	Name string `json:"name,omitempty"` // 職位 id、owner(web)、owner(cli)、system、migrate
}

type TimeRange struct {
	From time.Time `json:"from,omitempty"`
	To   time.Time `json:"to,omitempty"`
}

func (e *Error) Error() string { return e.Message }
