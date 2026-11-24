package platform

import (
	"errors"
	"github.com/minglight/evergenie/sdk"
)

type ErrorCode = sdk.ErrorCode
type Err struct {
	Code           ErrorCode
	Field, Message string
	Details        map[string]string
}

func (e *Err) Error() string { return e.Message }
func Invalid(field, msg string) error {
	return &Err{Code: sdk.ErrorCodeInvalid, Field: field, Message: msg}
}
func NotFound(msg string) error    { return &Err{Code: sdk.ErrorCodeNotFound, Message: msg} }
func Conflict(msg string) error    { return &Err{Code: sdk.ErrorCodeConflict, Message: msg} }
func Forbidden(msg string) error   { return &Err{Code: sdk.ErrorCodeForbidden, Message: msg} }
func Unavailable(msg string) error { return &Err{Code: sdk.ErrorCodeUnavailable, Message: msg} }
func Internal(msg string) error    { return &Err{Code: sdk.ErrorCodeInternal, Message: msg} }
func CodeOf(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var e *Err
	if errors.As(err, &e) {
		return e.Code
	}
	return sdk.ErrorCodeInternal
}
