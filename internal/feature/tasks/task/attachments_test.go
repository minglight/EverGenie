package task

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// shared 在 <家>/runtime/share/ 放一個檔，回它的 /share/ 連結（看板上傳、evergenie share 放的位置）。
func shared(t *testing.T, home, name string) string {
	t.Helper()
	dir := filepath.Join(home, "runtime", "share")
	must(t, os.MkdirAll(dir, 0o700))
	must(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600))
	return "/share/" + name
}

// TSK-28：主人開單時帶附件、秘書之後再加（重複的略過、完整網址存成相對連結）、可以拿掉；Get 與 List 都讀得到。
func TestAttachmentsOnCreateAndUpdate(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	a := shared(t, h.Dir, "20261103-101500-ab12-截圖.png")
	b := shared(t, h.Dir, "20261103-101600-cd34-報價.pdf")
	c := shared(t, h.Dir, "01JBXYZ-log.txt")
	r, err := m.Create(ctx, &sdk.TasksCreateRequest{Title: "看這張圖", Agent: "code", Attachments: []string{a, " " + a + " "}, By: &sdk.Actor{Name: "owner(web)"}})
	must(t, err)
	if !slices.Equal(r.Attachments, []string{a}) {
		t.Fatalf("開單的附件（重複的只留一個）：%v", r.Attachments)
	}
	// 秘書加：`evergenie share` 有對外網址時印完整網址，存成這台的 /share/ 相對連結。
	r, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{AddAttachments: []string{a, "https://hub.example.ts.net" + b, c}}, "code")
	must(t, err)
	if !slices.Equal(r.Attachments, []string{a, b, c}) {
		t.Fatalf("加附件接在後面、已有的略過：%v", r.Attachments)
	}
	r, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{RemoveAttachments: []string{b}}, "owner(web)")
	must(t, err)
	got, err := m.Get(ctx, r.ID)
	must(t, err)
	if !slices.Equal(got.Attachments, []string{a, c}) {
		t.Fatalf("拿掉之後讀回來：%v", got.Attachments)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "runtime", "share", strings.TrimPrefix(b, "/share/"))); err != nil {
		t.Fatalf("拿掉附件不刪檔：%v", err)
	}
	list, err := m.List(ctx, &sdk.TasksListRequest{})
	must(t, err)
	if len(list) != 1 || !slices.Equal(list[0].Attachments, []string{a, c}) {
		t.Fatalf("清單也帶附件：%+v", list)
	}
	// 全部拿掉（拿掉也認完整網址）：欄位不留。
	r, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{RemoveAttachments: []string{a, "https://hub.example.ts.net" + c}}, "owner(web)")
	must(t, err)
	if len(r.Attachments) != 0 {
		t.Fatalf("全部拿掉：%v", r.Attachments)
	}
}

// TSK-28：附件只收這台分享資料夾裡真的有的一般檔案；別的連結、路徑穿越、找不到、符號連結都回 INVALID 並指出欄位，單不變。
func TestAttachmentMustBeSharedFile(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	ok := shared(t, h.Dir, "ok.png")
	outside := filepath.Join(h.Dir, "secret.txt")
	must(t, os.WriteFile(outside, []byte("s"), 0o600))
	must(t, os.Symlink(outside, filepath.Join(h.Dir, "runtime", "share", "link.txt")))
	must(t, os.MkdirAll(filepath.Join(h.Dir, "runtime", "share", "dir"), 0o700))
	r := makeTask(t, m, sdk.TaskTypeTask)
	for _, bad := range []string{"", "ok.png", "/tmp/ok.png", "https://example.com/x.png", "/share/", "/share/../secret.txt", "/share/a/b.png", "/share/.hidden", "/share/missing.png", "/share/link.txt", "/share/dir"} {
		_, err := m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{AddAttachments: []string{ok, bad}}, "code")
		var e *platform.Err
		if platform.CodeOf(err) != sdk.ErrorCodeInvalid || !asErr(err, &e) || e.Field != "add_attachments" {
			t.Fatalf("%q 要回 INVALID（add_attachments）：%v", bad, err)
		}
		_, err = m.Create(ctx, &sdk.TasksCreateRequest{Title: "x", Agent: "code", Attachments: []string{bad}, By: &sdk.Actor{Name: "owner(web)"}})
		if platform.CodeOf(err) != sdk.ErrorCodeInvalid || !asErr(err, &e) || e.Field != "attachments" {
			t.Fatalf("開單帶 %q 要回 INVALID（attachments）：%v", bad, err)
		}
	}
	got, err := m.Get(ctx, r.ID)
	must(t, err)
	if len(got.Attachments) != 0 {
		t.Fatalf("有一個不對就整批不加：%v", got.Attachments)
	}
}

// TSK-28：結束的單照現有規則不能再改，附件也一樣（CONFLICT）。
func TestEndedTaskRejectsAttachments(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	a := shared(t, h.Dir, "a.png")
	r := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Close(ctx, r.ID, &sdk.TasksCloseRequest{Status: sdk.TaskStatusCancelled, Result: "不用了"}, "owner(web)")
	must(t, err)
	if _, err := m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{AddAttachments: []string{a}}, "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("結束的單不能加附件：%v", err)
	}
}

// TSK-28：交給工作 session 的第一句列出附件連結（秘書照共用規則讀檔）。
func TestBriefListsAttachments(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	a := shared(t, h.Dir, "a.png")
	r, err := m.Create(ctx, &sdk.TasksCreateRequest{Title: "看圖", Agent: "code", Body: "說明", Attachments: []string{a}, By: &sdk.Actor{Name: "owner(web)"}})
	must(t, err)
	s, err := m.Brief(ctx, r.ID)
	must(t, err)
	if !strings.Contains(s, "說明\n附件：\n- "+a) {
		t.Fatalf("第一句要列附件：%q", s)
	}
}

func asErr(err error, target **platform.Err) bool {
	e, ok := err.(*platform.Err)
	if ok {
		*target = e
	}
	return ok
}
