package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// 下載來源的規則（PLT-25，DEV-155）：升級的版本清單與包（[update] source、編進執行檔的 DefaultUpdateSource）、角色包、
// scripts/install.sh 的 --source、scripts/dist.sh 的 --source 都用這一條：
//   - https://<主機>[:port][/…]：一般的下載點。
//   - http:// 只收本機（127.0.0.1、[::1]、localhost；測試用）：別台電腦的 http 在路上就能被換掉，連檢查碼一起換。
//   - file:///<絕對路徑>：測試與離線用。
//
// 主機只收英數、. 與 -；路徑不收空白與控制字元（網址會放進 -ldflags 與 shell；中文路徑可以）；帳號密碼（user@）不收。
// scripts/install.sh、scripts/dist.sh 的 DOWNLOAD_URL_RE 是同一個式子（TestDownloadSourceRuleSameEverywhere 比對字面與行為）。
const DownloadURLPattern = `^(https://[A-Za-z0-9.-]+(:[0-9]+)?(/[^[:space:][:cntrl:]]*)?|http://(127\.0\.0\.1|localhost|\[::1\])(:[0-9]+)?(/[^[:space:][:cntrl:]]*)?|file:///[^[:space:][:cntrl:]]+)$`

var downloadURLRe = regexp.MustCompile(DownloadURLPattern)

// DownloadURLAllowed：raw 是可以下載的來源（見上面的規則）。
func DownloadURLAllowed(raw string) bool {
	if !downloadURLRe.MatchString(raw) {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.User == nil
}

// ErrUnsafeRedirect 是下載時被轉到不安全的地方（https 轉成不是 https、轉到規則以外的網址、轉太多次）。
var ErrUnsafeRedirect = errors.New("download redirected to a location that is not allowed")

// DownloadRedirectCheck 是下載用的 http.Client 的 CheckRedirect：轉過去的網址也要照規則；
// 一路上只要出現過 https，之後就只能是 https（不能被轉成 http，連本機也不行）；最多轉 10 次（跟 Go 預設一樣）。
func DownloadRedirectCheck(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return ErrUnsafeRedirect
	}
	if !DownloadURLAllowed(req.URL.String()) || req.URL.Scheme == "file" {
		return ErrUnsafeRedirect
	}
	for _, v := range via {
		if v.URL.Scheme == "https" && req.URL.Scheme != "https" {
			return ErrUnsafeRedirect
		}
	}
	return nil
}

// DownloadClient 回下載用的 http.Client（有 timeout、轉址照 DownloadRedirectCheck）。base 不是 nil（測試換掉的）就沿用它的
// Transport、Jar 與 Timeout，只蓋掉轉址的檢查。
func DownloadClient(base *http.Client, timeout time.Duration) *http.Client {
	c := &http.Client{Timeout: timeout}
	if base != nil {
		c.Transport, c.Jar = base.Transport, base.Jar
		if base.Timeout > 0 {
			c.Timeout = base.Timeout
		}
	}
	c.CheckRedirect = DownloadRedirectCheck
	return c
}

// DownloadFault 是 OpenDownload 失敗的種類（給呼叫的人挑白話）。
type DownloadFault int

const (
	DownloadNotAllowed     DownloadFault = iota + 1 // 網址不照下載來源的規則
	DownloadUnsafeRedirect                          // 被轉到規則以外的地方（ErrUnsafeRedirect）
	DownloadUnreachable                             // 連不上、讀不到檔、不是一般檔
	DownloadStatus                                  // 伺服器回的不是 200
)

// DownloadError 是 OpenDownload 的錯。Err 是英文細節（只進日誌，不含網址的查詢字串）；Status 是 DownloadStatus 時伺服器回的碼。
type DownloadError struct {
	Fault  DownloadFault
	Status int
	Err    error
}

func (e *DownloadError) Error() string {
	if e.Fault == DownloadStatus {
		return fmt.Sprintf("HTTP %d", e.Status)
	}
	if e.Err == nil {
		return "download failed"
	}
	return e.Err.Error()
}

func (e *DownloadError) Unwrap() error { return e.Err }

// OpenDownload 打開一個下載來源（PLT-25）：先照 DownloadURLAllowed；file:// 直接讀（要是一般檔）；https:// 與本機的 http:// 用
// DownloadClient(base, timeout)（轉址照 DownloadRedirectCheck）。回內容（呼叫的人關）與長度（不知道是 -1）。升級、角色包、
// 套件下載 Go 都用這一支，不另外各寫一套連線。
func OpenDownload(ctx context.Context, base *http.Client, raw string, timeout time.Duration) (io.ReadCloser, int64, error) {
	u, err := url.Parse(raw)
	if err != nil || !DownloadURLAllowed(raw) {
		return nil, 0, &DownloadError{Fault: DownloadNotAllowed, Err: errors.New("url is not an allowed download source (https, local http or file)")}
	}
	if u.Scheme == "file" {
		f, err := os.Open(filepath.FromSlash(u.Path))
		if err != nil {
			return nil, 0, &DownloadError{Fault: DownloadUnreachable, Err: err}
		}
		info, err := f.Stat()
		if err == nil && !info.Mode().IsRegular() {
			err = errors.New("not a regular file")
		}
		if err != nil {
			f.Close()
			return nil, 0, &DownloadError{Fault: DownloadUnreachable, Err: err}
		}
		return f, info.Size(), nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, 0, &DownloadError{Fault: DownloadNotAllowed, Err: errors.New("bad url")}
	}
	resp, err := DownloadClient(base, timeout).Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // url.Error 會把整個網址（含查詢字串）放進字串裡
		}
		if errors.Is(err, ErrUnsafeRedirect) {
			return nil, 0, &DownloadError{Fault: DownloadUnsafeRedirect, Err: err}
		}
		return nil, 0, &DownloadError{Fault: DownloadUnreachable, Err: err}
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, &DownloadError{Fault: DownloadStatus, Status: resp.StatusCode}
	}
	return resp.Body, resp.ContentLength, nil
}
