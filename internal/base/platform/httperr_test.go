package platform

import (
	"errors"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// SDKError：platform 錯誤保留代碼、欄位、細節與白話訊息（包過的也認得）；sdk 錯誤原樣；
// 其他錯誤一律「內部錯誤」不外洩原文；訊息永遠不是空的。
// Guards: REQ-116
func TestSDKErrorMapping(t *testing.T) {
	err := SDKError(&Err{Code: sdk.ErrorCodeInvalid, Field: "title", Message: "請填寫標題", Details: map[string]string{"min": "1"}})
	if err.Code != sdk.ErrorCodeInvalid || err.Field != "title" || err.Message != "請填寫標題" || err.Details["min"] != "1" {
		t.Fatalf("platform error changed: %+v", err)
	}
	wrapped := SDKError(errors.Join(errors.New("ctx"), Conflict("狀態不允許")))
	if wrapped.Code != sdk.ErrorCodeConflict || wrapped.Message != "狀態不允許" {
		t.Fatalf("wrapped error: %+v", wrapped)
	}
	fromHost := SDKError(&sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "找不到對話"})
	if fromHost.Code != sdk.ErrorCodeNotFound || fromHost.Message != "找不到對話" {
		t.Fatalf("sdk error: %+v", fromHost)
	}
	secret := SDKError(errors.New("open /Users/someone/secret.toml: permission denied\ngoroutine 1 [running]"))
	if secret.Code != sdk.ErrorCodeInternal || secret.Message != "內部錯誤" || secret.Field != "" || len(secret.Details) != 0 {
		t.Fatalf("internal error leaked: %+v", secret)
	}
	if empty := SDKError(&Err{Code: sdk.ErrorCodeForbidden}); empty.Message == "" {
		t.Fatal("message must never be empty")
	}
}
