package sdk

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// 附程式套件的入口（PKC-2）：套件的 main 只要 `sdk.ServePackage(ctx, impl)`。
// 核心啟動套件行程時不帶參數，資訊都在環境變數：EVERGENIE_API_SOCKET、EVERGENIE_PACKAGE_ID、EVERGENIE_PACKAGE_API、EVERGENIE_PACKAGE_VERSION、EVERGENIE_PACKAGE_DIR。
// ServePackage 先向核心報到（PackageAPI.Hello），成功了才呼叫套件自己的 Run；核心 15 秒內沒收到報到會把行程收掉。

// PackageHost 是報到成功後，套件拿到的東西：自己的資料夾、資料路徑、核心給的 token 與能力、呼叫核心用的 client。
type PackageHost struct {
	ID      string   `json:"id,omitempty"`
	Version string   `json:"version,omitempty"`
	Dir     string   `json:"dir,omitempty"`      // 套件資料夾（唯讀；官方與分享的套件不要寫）
	DataDir string   `json:"data_dir,omitempty"` // vault/pkg/<id>（相對於家；寫入一律經 VaultAPI，PKC-N3）
	Token   string   `json:"token,omitempty"`
	Granted []string `json:"granted,omitempty"`
	Client  *Client  `json:"-"`
}

// PackageProgram 是套件自己的程式：Run 跑到 ctx 結束或出錯為止。回傳 nil＝乾淨結束（依 program.restart 決定要不要重啟）。
type PackageProgram interface {
	Run(ctx context.Context, host *PackageHost) error
}

// ServePackage 讀環境變數、向核心報到，再把控制權交給 p.Run。報到失敗就回錯誤（不呼叫 Run）。
func ServePackage(ctx context.Context, p PackageProgram) error {
	socket, id := Getenv("EVERGENIE_API_SOCKET"), Getenv("EVERGENIE_PACKAGE_ID")
	if socket == "" || id == "" {
		return fmt.Errorf("缺少 EVERGENIE_API_SOCKET 或 EVERGENIE_PACKAGE_ID：套件要由 EverGenie 核心啟動")
	}
	c := Dial(socket)
	version := Getenv("EVERGENIE_PACKAGE_VERSION")
	resp, err := hello(ctx, c, &PackageHelloRequest{ID: id, API: Getenv("EVERGENIE_PACKAGE_API"), Version: version})
	if err != nil {
		return fmt.Errorf("向核心報到失敗：%w", err)
	}
	host := &PackageHost{ID: id, Version: version, Dir: Getenv("EVERGENIE_PACKAGE_DIR"), DataDir: resp.DataDir, Token: resp.Token, Granted: resp.Granted, Client: c}
	return p.Run(ctx, host)
}

// 核心剛啟動時，套件行程可能比 API socket 先起來（DEV-8：每次重開核心，衣櫃、習慣都「異常結束」一次）。
// 連不上或核心回 unavailable 就每 helloRetryEvery 重試，最多 helloRetryFor（核心等報到 15 秒）；其他錯誤馬上回。
var (
	helloRetryFor   = 10 * time.Second
	helloRetryEvery = 200 * time.Millisecond
)

func hello(ctx context.Context, c *Client, req *PackageHelloRequest) (*PackageHelloResponse, error) {
	deadline := time.Now().Add(helloRetryFor)
	for {
		resp, err := NewPackageClient(c).Hello(ctx, req)
		if err == nil || !helloRetryable(err) || !time.Now().Before(deadline) {
			return resp, err
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(helloRetryEvery):
		}
	}
}

func helloRetryable(err error) bool {
	var se *Error
	if errors.As(err, &se) {
		return se.Code == ErrorCodeUnavailable
	}
	return true // 連線層的錯：socket 還不在、被拒
}
