package main

import (
	"bytes"
	"encoding/json"
	"github.com/minglight/evergenie/sdk"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrustFeatureThroughCLIAndBoard(t *testing.T) {
	_, sock, board := startFixtureWebWith(t, nil)
	if out := hgOK(t, sock, "help"); !strings.Contains(out, "trust") {
		t.Fatal(out)
	}
	hgOK(t, sock, "setup", "permissions", "--choice", "read")
	if out := hgOK(t, sock, "trust", "show", "--json"); !strings.Contains(out, `"default_level":"readonly"`) {
		t.Fatal(out)
	}
	for _, path := range []string{"/api/web/get_trust", "/api/web/get_overview"} {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString("{}"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		board.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if strings.HasSuffix(path, "get_trust") {
			var out sdk.WebGetTrustResponse
			if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Policy == nil {
				t.Fatal(w.Body.String())
			}
		}
	}
}
