package job

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

func day(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

const nightSample = `# 夜班清單

說明一行。

## 怎麼放
一項一行。

## 待做
- [ ] 整理 A｜加入 2026-10-20
  進度：做到一半
- [ ] 補測試 B｜加入 2026-10-21

## 要主人

## 做完（留最近 14 天）
- [x] 舊的｜結果：好｜完成 2026-10-01
  舊進度
- [x] 新的｜結果：好｜完成 2026-10-20
`

// 三段、序號、項目連同縮排的進度說明一起搬；解析再輸出不改內容。
// Guards: REQ-19, REQ-111
func TestNightQueueParseAndRewrite(t *testing.T) {
	d := parseNight(nightSample)
	if got := d.render(); got != nightSample {
		t.Fatalf("解析再輸出要一樣：\n%s", got)
	}
	list := d.list()
	for _, want := range []string{"待做（2）", "1. 整理 A｜加入 2026-10-20", "      進度：做到一半", "2. 補測試 B", "要主人（0）", "（沒有）", "做完（2）", "  - 舊的｜結果：好｜完成 2026-10-01"} {
		if !strings.Contains(list, want) {
			t.Errorf("list 少了「%s」：\n%s", want, list)
		}
	}

	// add：加在待做最下面，單行、換掉全形直線。
	if err := d.add("  寫 C\n 的文件｜順便 ", day("2026-10-25")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.render(), "- [ ] 補測試 B｜加入 2026-10-21\n- [ ] 寫 C 的文件|順便｜加入 2026-10-25\n\n## 要主人") {
		t.Fatalf("add 位置不對：\n%s", d.render())
	}
	if err := d.add("   ", day("2026-10-25")); err == nil {
		t.Fatal("空的要擋")
	}

	// block：連進度說明移到「要主人」，附原因。
	if err := d.block(1, "要 sudo"); err != nil {
		t.Fatal(err)
	}
	out := d.render()
	if !strings.Contains(out, "## 要主人\n- [ ] 整理 A｜加入 2026-10-20｜卡住：要 sudo\n  進度：做到一半\n\n## 做完") {
		t.Fatalf("block：\n%s", out)
	}
	if strings.Contains(strings.Split(out, "## 要主人")[0], "整理 A") {
		t.Fatal("待做裡不該還有被移走的項目")
	}

	// done：序號重新算（原本第 2 項現在是第 1 項）；移到做完、附結果與日期；做完超過 14 天的清掉（連進度）。
	if err := d.done(1, "已補完，在 internal/x", day("2026-10-25")); err != nil {
		t.Fatal(err)
	}
	out = d.render()
	if !strings.Contains(out, "- [x] 補測試 B｜加入 2026-10-21｜結果：已補完，在 internal/x｜完成 2026-10-25") {
		t.Fatalf("done：\n%s", out)
	}
	if strings.Contains(out, "舊的") || strings.Contains(out, "舊進度") {
		t.Fatalf("超過 14 天的要清掉（連進度說明）：\n%s", out)
	}
	if !strings.Contains(out, "- [x] 新的｜結果：好｜完成 2026-10-20") {
		t.Fatalf("14 天內的要留：\n%s", out)
	}
	// 剛好 14 天的還留著。
	edge := parseNight("## 做完（留最近 14 天）\n- [x] 邊界｜結果：好｜完成 2026-10-11\n- [x] 太舊｜結果：好｜完成 2026-10-10\n- [x] 沒日期\n")
	edge.pruneDone(day("2026-10-25"))
	if o := edge.render(); !strings.Contains(o, "邊界") || strings.Contains(o, "太舊") || !strings.Contains(o, "沒日期") {
		t.Fatalf("邊界：\n%s", o)
	}

	// rm 與序號錯誤。
	if err := d.rm(1); err != nil {
		t.Fatal(err)
	}
	if err := d.rm(1); err == nil || !strings.Contains(err.Error(), "空的") {
		t.Fatalf("待做空了要說空的：%v", err)
	}
	if err := d.add("X", day("2026-10-25")); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, 2, -1} {
		if err := d.done(n, "好", day("2026-10-25")); err == nil {
			t.Errorf("序號 %d 要擋", n)
		}
	}
	if err := d.done(1, "  ", day("2026-10-25")); err == nil {
		t.Error("done 沒附結果要擋")
	}
	if err := d.block(1, ""); err == nil {
		t.Error("block 沒附原因要擋")
	}
}

// 檔案不存在：照範本建立（第一次寫入），說明裡是「主人」不是人名；缺段落時補在最後。
// Guards: REQ-111
func TestNightQueueEmptyAndMissingSections(t *testing.T) {
	d := parseNight(nightTemplate)
	if !strings.Contains(nightTemplate, "\n主人排好、要在") || !strings.Contains(nightTemplate, "要主人決定") {
		t.Fatal("範本裡的說明一律寫「主人」，不寫人名")
	}
	for _, h := range []string{"## 待做", "## 要主人", "## 做完（留最近 14 天）"} {
		if !strings.Contains(nightTemplate, h+"\n") {
			t.Errorf("範本少了 %s", h)
		}
	}
	if err := d.add("第一項", day("2026-10-26")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.render(), "## 待做\n- [ ] 第一項｜加入 2026-10-26\n\n## 要主人") {
		t.Fatalf("\n%s", d.render())
	}
	// 重新輸出再解析，不會再變。
	again := parseNight(d.render()).render()
	if again != d.render() {
		t.Fatalf("不穩定：\n%s\n---\n%s", d.render(), again)
	}
	// 只有標題或亂掉的檔案：補上缺的段落，原有內容不丟。
	odd := parseNight("隨手寫的\n\n## 待做\n- [ ] 一項\n")
	if err := odd.done(1, "好", day("2026-10-26")); err != nil {
		t.Fatal(err)
	}
	out := odd.render()
	if !strings.HasPrefix(out, "隨手寫的\n") || !strings.Contains(out, "## 做完（留最近 14 天）\n- [x] 一項｜結果：好｜完成 2026-10-26") {
		t.Fatalf("\n%s", out)
	}
	if got := parseNight("").list(); !strings.Contains(got, "待做（0）") {
		t.Fatalf("空檔案的 list：%s", got)
	}
}

// 假 vault 的 get／put 只認夜班清單那一筆；put 照 revision 樂觀鎖，成功就換新 revision。
func (s fakeNoteVault) Get(_ context.Context, r *sdk.VaultGetRequest) (*sdk.VaultGetResponse, error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	if r.Kind != nightKind || r.ID != nightID || s.f.night == nil {
		return nil, &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "沒有這一筆"}
	}
	rec := *s.f.night
	return &sdk.VaultGetResponse{Record: &rec}, nil
}

func (s fakeNoteVault) Put(_ context.Context, r *sdk.VaultPutRequest) (*sdk.VaultPutResponse, error) {
	s.f.record("vault.put", r)
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	cur := ""
	if s.f.night != nil {
		cur = s.f.night.Revision
	}
	if r.ExpectedRevision != cur {
		return nil, &sdk.Error{Code: sdk.ErrorCodeConflict, Message: "revision 不符"}
	}
	rec := *r.Record
	rec.Revision = cur + "+"
	s.f.night = &rec
	return &sdk.VaultPutResponse{Record: &rec}, nil
}

// --expect：第 n 項不是那樣開頭就拒絕、不寫；對得上才改；空的 --expect 是用法錯誤。
// Guards: REQ-19, REQ-111
func TestNightExpectMismatch(t *testing.T) {
	c, f := serveFakes(t)
	f.night = &sdk.Record{Kind: nightKind, ID: nightID, Body: nightSample, Revision: "r1"}

	for _, args := range [][]string{
		{"night", "rm", "2", "--expect", "整理"},
		{"night", "done", "2", "--result", "好", "--expect", "整理 A"},
		{"night", "block", "1", "--reason", "要 sudo", "--expect=補測試"},
	} {
		code, _, errOut := run(t, c, "", args...)
		if code != 1 || !strings.Contains(errOut, "沒有改") || !strings.Contains(errOut, "night list") {
			t.Fatalf("%v → %d %q", args, code, errOut)
		}
	}
	if f.got("vault.put") != nil {
		t.Fatal("對不上時不能寫")
	}
	if f.night.Body != nightSample {
		t.Fatal("內容不該變")
	}

	if code, _, errOut := run(t, c, "", "night", "rm", "1", "--expect", "  "); code != 2 || !strings.Contains(errOut, "--expect") {
		t.Fatalf("空的 --expect 要是用法錯誤：%d %q", code, errOut)
	}

	// 對得上：連續空白當一個，開頭相同就好。
	out := mustRun(t, c, "night", "rm", "2", "--expect", "補測試  B")
	if !strings.Contains(out, "已刪掉第 2 項") || strings.Contains(f.night.Body, "補測試 B") || !strings.Contains(f.night.Body, "整理 A") {
		t.Fatalf("%q\n%s", out, f.night.Body)
	}
	// 沒給 --expect 照舊。
	mustRun(t, c, "night", "done", "1", "--result", "好")
	if strings.Contains(strings.Split(f.night.Body, "## 要主人")[0], "整理 A") {
		t.Fatalf("\n%s", f.night.Body)
	}
}
