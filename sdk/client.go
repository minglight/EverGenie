package sdk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// Client transports API requests over a local socket.
type Client struct {
	http *http.Client
}

func Dial(socketPath string) *Client {
	return &Client{http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if c == nil || c.http == nil {
		return nil, &Error{Code: ErrorCodeUnavailable, Message: "尚未連線，請先建立連線"}
	}
	return c.http.Do(req)
}
func request[Req any](ctx context.Context, c *Client, path string, req *Req) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	return c.Do(r)
}
func responseError(r *http.Response) error {
	var e Error
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		return &Error{Code: ErrorCodeInternal, Message: "無法讀取服務的回應"}
	}
	return &e
}
func Invoke[Req, Resp any](ctx context.Context, c *Client, path string, req *Req) (*Resp, error) {
	r, err := request(ctx, c, path, req)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return nil, responseError(r)
	}
	var out Resp
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}
func InvokeStream[Req, Resp any](ctx context.Context, c *Client, path string, req *Req, send func(*Resp) error) error {
	r, err := request(ctx, c, path, req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return responseError(r)
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "text/event-stream") {
		return &Error{Code: ErrorCodeInternal, Message: "服務沒有回傳串流"}
	}
	reader := bufio.NewReader(r.Body)
	var data []string
	event := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			if len(data) > 0 {
				raw := []byte(strings.Join(data, "\n"))
				if event == "error" {
					var e Error
					if err := json.Unmarshal(raw, &e); err != nil {
						return err
					}
					return &e
				}
				var out Resp
				if err := json.Unmarshal(raw, &out); err != nil {
					return fmt.Errorf("decode stream: %w", err)
				}
				if err := send(&out); err != nil {
					return err
				}
			}
			data = nil
			event = ""
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		} else if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
	}
}
