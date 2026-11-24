package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// accounts 業務模組：registry 把它的路由掛進 api（看板經 api 代理拿得到），指令 quota 從業務模組的 Commands() 來。
func TestAccountsFeatureThroughCLIAndBoard(t *testing.T) {
	_, sock, board := startFixtureWebWith(t, nil)
	if out := hgOK(t, sock, "help"); !strings.Contains(out, "quota") {
		t.Fatal(out)
	}
	// quota 走 system.usage → 業務模組 → engine；測試家沒有引擎帳號在跑，不管答案是什麼，不能是「找不到這個指令」或 404。
	if code, out, errOut := hg(t, sock, "", "quota"); code == 2 && strings.Contains(out+errOut, "不認得") {
		t.Fatalf("quota 指令沒登記：%s%s", out, errOut)
	}
	post := func(path, body string) (int, []byte) {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		board.ServeHTTP(w, req)
		return w.Code, w.Body.Bytes()
	}
	code, body := post("/api/system/accounts", "{}")
	var out sdk.SystemAccountsResponse
	if code != 200 || json.Unmarshal(body, &out) != nil || len(out.Engines) == 0 {
		t.Fatalf("看板拿帳號清單：%d %s", code, body)
	}
}
