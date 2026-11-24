package platform

import (
	"context"
	"errors"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// SDKError 把模組的錯誤轉成 API 對外的錯誤本體（API-3、API-N3）：
// platform 錯誤保留代碼、欄位與白話訊息；其他錯誤一律是「內部錯誤」，不外洩原文。
// api 與各業務模組的 http.go 共用這一份。
func SDKError(err error) *sdk.Error {
	var e *Err
	if errors.As(err, &e) && e != nil {
		return &sdk.Error{Code: e.Code, Field: e.Field, Message: messageOr(e.Message), Details: e.Details}
	}
	var s *sdk.Error
	if errors.As(err, &s) && s != nil {
		return &sdk.Error{Code: s.Code, Field: s.Field, Message: messageOr(s.Message), Details: s.Details}
	}
	return &sdk.Error{Code: sdk.ErrorCodeInternal, Message: "內部錯誤"}
}

func messageOr(m string) string {
	if strings.TrimSpace(m) == "" {
		return "操作沒有完成，請稍後再試"
	}
	return m
}

// ActorName 回傳每個寫入都要帶的操作者名字；沒帶就回「請提供操作者」。
func ActorName(a *sdk.Actor) (string, error) {
	if a == nil || strings.TrimSpace(a.Name) == "" {
		return "", Invalid("by", "請提供操作者")
	}
	return a.Name, nil
}

// UpstreamError 把「向別的服務拿資料」失敗的錯誤變成給使用者看的：sdk 錯誤與 platform 錯誤原樣保留代碼與訊息，
// 取消（context.Canceled）原樣回，其他（例如連不上）一律 unavailable。看板的總覽與選單徽章用。
func UpstreamError(err error) error {
	var s *sdk.Error
	if errors.As(err, &s) && s != nil {
		return s
	}
	var e *Err
	if errors.As(err, &e) && e != nil {
		return &sdk.Error{Code: e.Code, Field: e.Field, Message: messageOr(e.Message), Details: e.Details}
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "核心服務暫時連不上，稍後會自動重試"}
}
