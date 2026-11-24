package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-116
func TestTSGenUpToDate(t *testing.T) {
	var out bytes.Buffer
	if got := run("../../..", true, &out); got != 0 {
		t.Fatalf("產生的 TypeScript 跟 git 裡的不一樣：%s", &out)
	}
}

// Guards: REQ-116
func TestTSGenTypeMapping(t *testing.T) {
	set, err := parseDir("testdata/mapping")
	if err != nil {
		t.Fatal(err)
	}
	got := string(set.render())
	want := []string{
		`export type Mode = "fast" | "slow";`,
		`export type Free = string;`,
		`export type Alias = Mode;`,
		"  name: string;\n",
		"  opt?: string;\n",
		"  flag: boolean;\n",
		"  count: number;\n",
		"  big: number;\n",
		"  ratio: number;\n",
		"  at: string;\n",
		"  wait: number;\n",
		"  list: string[];\n",
		"  matrix: number[][];\n",
		"  mapped: Record<string, Mode>;\n",
		"  ptr: Everything;\n",
		"  ptr_str?: string;\n",
		"  anything: unknown;\n",
		"  legacy: unknown;\n",
		"  raw: unknown;\n",
		"  data: string;\n",
		"  kind: Mode;\n",
		"  loose: Free;\n",
		"  refs?: Record<string, Everything>;\n",
		"  \"dashed-key\": string;\n",
		"export interface SdkRecord {\n",
		"  rec?: SdkRecord;\n",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("缺少 %q\n%s", w, got)
		}
	}
	for _, w := range []string{"Skipped", "skipped", "unexport", "Service", "interface Service"} {
		if strings.Contains(got, w) {
			t.Errorf("不該出現 %q\n%s", w, got)
		}
	}
	if !strings.HasPrefix(got, header) {
		t.Errorf("缺檔頭：%s", got)
	}
}

// Guards: REQ-116
func TestTSGenRejectsUnknownType(t *testing.T) {
	for dir, name := range map[string]string{"testdata/unknown": "url.URL", "testdata/unknown_ident": "Missing"} {
		_, err := parseDir(dir)
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s: 應該因為 %s 失敗，實際：%v", dir, name, err)
		}
	}
}

// Guards: REQ-116
func TestTSGenClientShape(t *testing.T) {
	set, err := parseDir("testdata/mapping")
	if err != nil {
		t.Fatal(err)
	}
	routes := []sdk.Route{
		{Service: "demo", Method: "get_thing", Path: "/api/demo/get_thing", Request: "Everything", Response: "Everything"},
		{Service: "demo", Method: "watch", Path: "/api/demo/watch", Request: "Everything", Response: "Everything", Stream: true},
	}
	out, err := renderClient(routes, set)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, w := range []string{
		`getThing: (req: Everything) => call<Everything, Everything>(base + "/api/demo/get_thing", req)`,
		`stream<Everything, Everything>(base + "/api/demo/watch", req, onItem, signal, onReconnect)`,
		`import { call, stream } from "../transport";`,
		"export function makeApi(base: string) {\n",
		"export type Api = ReturnType<typeof makeApi>;\n",
		`export const api: Api = makeApi("");`,
	} {
		if !strings.Contains(got, w) {
			t.Errorf("缺少 %q\n%s", w, got)
		}
	}
	routes[0].Response = "Nope"
	if _, err := renderClient(routes, set); err == nil || !strings.Contains(err.Error(), "Nope") {
		t.Fatalf("route 用到不存在的型別應該失敗：%v", err)
	}
}
