package reminder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// shared 在家的分享資料夾放一個檔，回它的 /share/ 連結（看板上傳、evergenie share 都放這裡）。
func shared(t *testing.T, home, name string) string {
	t.Helper()
	dir := filepath.Join(home, "runtime", "share")
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644))
	return "/share/" + name
}

// RMD-23：待辦有描述（body）與附件（/share/ 連結，跟任務單同一套）；開的時候帶、之後可以改描述、加、拿掉附件（拿掉不刪檔），讀得回來。
func TestReminderBodyAndAttachments(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	a, b := shared(t, h.Dir, "20261103-101530-a1b2-a.png"), shared(t, h.Dir, "b.pdf")
	r := create(t, m, &sdk.RemindersCreateRequest{Body: "要帶的東西", Attachments: []string{a, " " + a + " "}})
	if r.Body != "要帶的東西" || !reflect.DeepEqual(r.Attachments, []string{a}) {
		t.Fatalf("開的時候：%+v", r)
	}
	r, err := m.Update(ctx, r.ID, &sdk.RemindersUpdateRequest{Body: ptr("  改過的描述\n第二行  "), AddAttachments: []string{b, a}}, "owner(web)")
	must(t, err)
	if r.Body != "改過的描述\n第二行" || !reflect.DeepEqual(r.Attachments, []string{a, b}) {
		t.Fatalf("改了之後：%+v", r)
	}
	got, err := m.Get(ctx, r.ID)
	must(t, err)
	if got.Body != "改過的描述\n第二行" || !reflect.DeepEqual(got.Attachments, []string{a, b}) {
		t.Fatalf("讀回來：%+v", got)
	}
	// 拿掉也認完整網址（`evergenie share` 有對外網址時印的那種）。
	r, err = m.Update(ctx, r.ID, &sdk.RemindersUpdateRequest{RemoveAttachments: []string{a, "https://hub.example.ts.net" + b}, Body: ptr("")}, "life")
	must(t, err)
	if len(r.Attachments) != 0 || r.Body != "" {
		t.Fatalf("拿光、清掉描述：%+v", r)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "runtime", "share", "b.pdf")); err != nil {
		t.Fatal("拿掉附件不刪檔", err)
	}
	// 只改別的欄位：描述不動。
	_, err = m.Update(ctx, r.ID, &sdk.RemindersUpdateRequest{Body: ptr("留著")}, "life")
	must(t, err)
	r, err = m.Update(ctx, r.ID, &sdk.RemindersUpdateRequest{Content: ptr("散步")}, "life")
	must(t, err)
	if r.Body != "留著" {
		t.Fatalf("沒給 body 不該清掉描述：%q", r.Body)
	}
}

// RMD-23：附件要是這台分享資料夾裡真的有的檔；不對的整筆不存，錯誤指到欄位。結束的待辦不能再加。
func TestReminderAttachmentMustBeSharedFile(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	for _, bad := range []string{"/share/nope.png", "https://example.com/x.png", "/share/../home.toml", "/etc/passwd"} {
		_, err := m.Create(ctx, &sdk.RemindersCreateRequest{Content: "x", Attachments: []string{bad}, By: &sdk.Actor{Name: "owner(web)"}})
		var pe *platform.Err
		if !errors.As(err, &pe) || pe.Code != sdk.ErrorCodeInvalid || pe.Field != "attachments" {
			t.Fatalf("%s：%v", bad, err)
		}
	}
	r := create(t, m, nil)
	_, err := m.Update(ctx, r.ID, &sdk.RemindersUpdateRequest{AddAttachments: []string{"/share/nope.png"}}, "life")
	var pe *platform.Err
	if !errors.As(err, &pe) || pe.Field != "add_attachments" {
		t.Fatalf("update：%v", err)
	}
	ok := shared(t, h.Dir, "ok.png")
	_, err = m.Done(ctx, r.ID, "owner(web)")
	must(t, err)
	if _, err = m.Update(ctx, r.ID, &sdk.RemindersUpdateRequest{AddAttachments: []string{ok}}, "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("結束的待辦：%v", err)
	}
}

// RMD-23：要查證的待辦到時間，送給秘書的那一則也帶描述與附件，她不用再查一次單。
func TestCheckRoutineListsAttachments(t *testing.T) {
	m, h, f := fixture(t)
	a := shared(t, h.Dir, "bill.png")
	r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowCheck, Check: "查付款", Body: "  帳號末四碼 1234  ", At: h.Clock.Now(), Room: "life-r-1", Attachments: []string{a}})
	fire(t, m, r.ID)
	if len(f.sent) != 1 || !strings.HasSuffix(f.sent[0].Body, "查付款\n描述：帳號末四碼 1234\n附件：\n- "+a) {
		t.Fatal(f.sent)
	}
}
