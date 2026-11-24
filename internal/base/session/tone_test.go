package session

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func toneErrLine(t *testing.T, err error, line string) {
	t.Helper()
	if platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), line) {
		t.Fatalf("要回 INVALID 並指出 %q：%v", line, err)
	}
}

// TestScenarioParse：情境檔 `## <id>｜<標題>[｜職位,職位]`：沒寫職位＝每個座位都跑，同 id 可依座位寫不同版本，
// {name} 留到執行時才換；格式錯回 INVALID 並指出第幾行。
// Guards: REQ-130
func TestScenarioParse(t *testing.T) {
	text := `# 標題說明
隨便寫的前言，不是情境。

## report｜長回報
第一行
第二行

## incident｜出事了｜code,mac
CI 紅了 {name}

## incident｜出事了｜life
忘記紀念日

## tired｜他累了｜work
好累
`
	code, err := parseScenarios(text, "code")
	if err != nil || len(code) != 2 || code[0].id != "report" || code[0].body != "第一行\n第二行" || code[1].id != "incident" || code[1].body != "CI 紅了 {name}" || code[1].title != "出事了" {
		t.Fatalf("code 要看到 report 與 code 版的 incident：%+v %v", code, err)
	}
	life, err := parseScenarios(text, "life")
	if err != nil || len(life) != 2 || life[1].body != "忘記紀念日" {
		t.Fatalf("life 要看到 life 版的 incident：%+v %v", life, err)
	}
	work, err := parseScenarios(text, "work")
	if err != nil || len(work) != 2 || work[1].id != "tired" {
		t.Fatalf("work：%+v %v", work, err)
	}

	// 錯誤要指出第幾行。
	_, err = parseScenarios("## report｜長回報\n內容\n\n## 沒有標題\n內容", "code")
	toneErrLine(t, err, "第 4 行")
	_, err = parseScenarios("## bad id｜標題\n內容", "code")
	toneErrLine(t, err, "第 1 行")
	_, err = parseScenarios("## a｜甲\n內容\n## b｜乙\n   \n", "code")
	toneErrLine(t, err, "第 3 行")
	_, err = parseScenarios("## a｜甲\n一\n## a｜甲二\n二", "code")
	toneErrLine(t, err, "第 3 行")
	_, err = parseScenarios("## a｜甲｜code\n一\n## a｜甲二｜code,life\n二", "code")
	toneErrLine(t, err, "第 3 行")
	if _, err = parseScenarios("## a｜甲｜code\n一\n## a｜甲二｜life\n二", "code"); err != nil {
		t.Fatalf("不同座位的同 id 不算重複：%v", err)
	}
}

// waitTone 等這位人設最新一次測試離開 running，回它的結果。
func waitTone(t *testing.T, f *fixture, slug string) sdk.PersonaTestRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r, err := f.m.PersonaTestResults(context.Background(), &sdk.PersonasTestResultsRequest{Slug: slug, Limit: 1})
		must(t, err)
		if len(r.Runs) == 1 && r.Runs[0].Status != "running" {
			return r.Runs[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("測試沒有在時限內跑完")
	return sdk.PersonaTestRun{}
}

func writeScenarios(t *testing.T, f *fixture, text string) {
	t.Helper()
	dir := filepath.Join(f.h.Dir, "personas", "_test")
	must(t, os.MkdirAll(dir, 0o700))
	must(t, os.WriteFile(filepath.Join(dir, "scenarios.md"), []byte(text), 0o600))
}

// TestPersonaTestWritesResult：每個情境一次 Ask（同一個座位的帳號、模型），並行跑完寫成一份檔：檔頭、每則一段、
// {name} 換成被測人設的名字；一則失敗其他照寫；不開 session。
// Guards: REQ-130
func TestPersonaTestWritesResult(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	writeScenarios(t, f, "## one｜第一則\n{name}，你好\n第二行\n\n## boom｜會失敗的\n這則會失敗\n\n## three｜第三則\n平安\n")
	f.e.Configure(fake.Behavior{Delivered: true, AskFailOn: "會失敗"})

	got, err := f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "test", Style: "b"})
	must(t, err)
	if got.Scenarios != 3 || !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-\d{6}\.md$`).MatchString(got.Run) {
		t.Fatalf("回應：%+v", got)
	}
	run := waitTone(t, f, "test")
	if run.Run != got.Run || run.Status != "done" || run.Seat != "code" || run.Style != "b" || run.Model == "" || run.At.IsZero() {
		t.Fatalf("結果：%+v", run)
	}
	for _, want := range []string{
		"# Person 說話測試（", "personas/test.md", "等級 B", "`code`（帳號 main", "不開工具、不留 session",
		"## one｜第一則\n\n> Person，你好\n> 第二行\n\n[fake] Person，你好\n第二行",
		"## boom｜會失敗的\n\n> 這則會失敗\n\n（這則沒跑成：假引擎指定這則失敗）",
		"## three｜第三則\n\n> 平安\n\n[fake] 平安",
	} {
		if !strings.Contains(run.Markdown, want) {
			t.Errorf("結果少了 %q：\n%s", want, run.Markdown)
		}
	}
	if strings.Contains(run.Markdown, "status:") {
		t.Errorf("markdown 不含檔頭：\n%s", run.Markdown)
	}
	// 三則都是 Ask：同一個帳號、說話層 B 的文字、座位的指令，沒有 Start／Deliver。
	asks := 0
	for _, c := range f.e.Calls() {
		switch c.Method {
		case "Ask":
			asks++
			r := c.Request.(*sdk.EngineAskRequest)
			if r.Account != "main" || r.Agent != "code" || r.Cwd == "" || r.TimeoutSeconds != 600 || !strings.Contains(r.Instructions, "測試人設") || !strings.Contains(r.Instructions, "## 職責\n\n測試職責") {
				t.Errorf("Ask 的內容：%+v", r)
			}
		case "Start", "Deliver":
			t.Errorf("測口吻不能開 session：%s", c.Method)
		}
	}
	if asks != 3 {
		t.Fatalf("要 Ask 三次：%d", asks)
	}
	if rows, err := f.m.List(ctx, "", nil); err != nil || len(rows) != 0 {
		t.Fatalf("不留 session：%v %v", rows, err)
	}

	// only 與 model：只跑指定的情境，模型可覆蓋（換了模型就不帶座位的 effort）。
	_, err = f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "test", Only: []string{"three"}, Model: "haiku"})
	must(t, err)
	run = waitTone(t, f, "test")
	if run.Model != "haiku" || strings.Contains(run.Markdown, "## one") || !strings.Contains(run.Markdown, "## three") {
		t.Fatalf("only／model：%+v", run)
	}
	if _, err = f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "test", Only: []string{"nope"}}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒有情境可以跑要回 INVALID：%v", err)
	}
	// 每個情境都失敗：整次標 failed 並說原因。
	f.e.Configure(fake.Behavior{Delivered: true, AskError: "引擎壞了"})
	_, err = f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "test"})
	must(t, err)
	run = waitTone(t, f, "test")
	if run.Status != "failed" || !strings.Contains(run.Reason, "引擎壞了") || strings.Count(run.Markdown, "（這則沒跑成：引擎壞了）") != 3 {
		t.Fatalf("全部失敗：%+v", run)
	}
	// 最近的在前面。
	list, err := f.m.PersonaTestResults(ctx, &sdk.PersonasTestResultsRequest{Slug: "test"})
	must(t, err)
	if len(list.Runs) != 3 || list.Runs[0].Status != "failed" || list.Runs[2].Status != "done" || list.Runs[2].Style != "b" {
		t.Fatalf("結果列表要新到舊：%+v", list.Runs)
	}
	if one, _ := f.m.PersonaTestResults(ctx, &sdk.PersonasTestResultsRequest{Slug: "test", Limit: 1}); len(one.Runs) != 1 {
		t.Fatal("limit")
	}
}

// TestPersonaTestSeatCardAndStyle：座位沒指定時用坐這位人設、編號最小的啟用秘書，沒人坐回 INVALID 並說怎麼辦；
// card 是還沒存的草稿全文，style 蓋掉說話層。
// Guards: REQ-130
func TestPersonaTestSeatCardAndStyle(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	must(t, os.WriteFile(filepath.Join(f.h.Dir, "personas", "other.md"), []byte("---\nname: 路人\n---\n別人的人設\n"), 0o600))
	_, err := f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "other"})
	if platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "路人 還沒坐任何秘書，請指定要在哪一位的位置測") {
		t.Fatalf("沒人坐：%v", err)
	}
	if _, err = f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "ghost"}); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("沒有這個人設：%v", err)
	}
	if _, err = f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "other", Seat: "nobody"}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("座位不存在：%v", err)
	}
	if _, err = f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "other", Seat: "code", Style: "z"}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("說話層不認得：%v", err)
	}
	if _, err = f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "other", Seat: "code", Style: "custom"}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒有自己的說話層：%v", err)
	}
	if _, err = f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "other", Seat: "code", Card: "---\n壞掉的檔頭"}); err == nil {
		t.Fatal("草稿卡檔頭壞掉要擋下來")
	}

	// 指定座位 + 草稿 + 說話層：指令用草稿與 D 級文字，名冊 code 這一行是草稿的名字。
	writeScenarios(t, f, "## one｜第一則\n你好\n")
	_, err = f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "other", Seat: "code", Card: "---\nname: 草稿名\n---\n還沒存的人設\n", Style: "d"})
	must(t, err)
	run := waitTone(t, f, "other")
	if run.Status != "done" || !strings.Contains(run.Markdown, "# 草稿名 說話測試") || !strings.Contains(run.Markdown, "看板上還沒存的內容（原本是 `personas/other.md`（官方）") || !strings.Contains(run.Markdown, "等級 D") {
		t.Fatalf("結果：%+v", run)
	}
	var ins string
	for _, c := range f.e.Calls() {
		if c.Method == "Ask" {
			ins = c.Request.(*sdk.EngineAskRequest).Instructions
		}
	}
	if !strings.Contains(ins, "## 人設\n\n還沒存的人設") || !strings.Contains(ins, "- code：草稿名（測試") || strings.Contains(ins, "測試人設") {
		t.Fatalf("指令：\n%s", ins)
	}
}

// TestPersonaTestOneAtATime：同一位人設同時只能有一個在跑（第二個回 CONFLICT）；跑完才能再開，同一秒的檔名不撞。
// Guards: REQ-130
func TestPersonaTestOneAtATime(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	writeScenarios(t, f, "## one｜第一則\n你好\n")
	f.e.Configure(fake.Behavior{Delivered: true, AskDelayMillis: 400})
	first, err := f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "test"})
	must(t, err)
	_, err = f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "test"})
	if platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "Person 的測試還在跑") {
		t.Fatalf("第二個要 CONFLICT：%v", err)
	}
	if r, _ := f.m.PersonaTestResults(ctx, &sdk.PersonasTestResultsRequest{Slug: "test"}); len(r.Runs) != 1 || r.Runs[0].Status != "running" || !strings.Contains(r.Runs[0].Markdown, "測試進行中") {
		t.Fatalf("跑的時候檔案是 running：%+v", r)
	}
	waitTone(t, f, "test")
	f.e.Configure(fake.Behavior{Delivered: true})
	second, err := f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "test"})
	must(t, err)
	if second.Run == first.Run {
		t.Fatalf("同一秒開第二次，檔名不能撞：%s", second.Run)
	}
	waitTone(t, f, "test")
	r, _ := f.m.PersonaTestResults(ctx, &sdk.PersonasTestResultsRequest{Slug: "test"})
	if len(r.Runs) != 2 || r.Runs[0].Run != second.Run || r.Runs[1].Run != first.Run {
		t.Fatalf("新的在前：%+v", r.Runs)
	}
}

// TestPersonaTestRecoversRunning：行程重開時，留在 running 的結果改成 failed「核心重開，這次測試中斷」。
// Guards: REQ-130
func TestPersonaTestRecoversRunning(t *testing.T) {
	f := setup(t)
	dir := filepath.Join(f.h.Dir, "runtime", "persona-tests", "test")
	must(t, os.MkdirAll(dir, 0o700))
	stale := "---\nstatus: running\nat: \"2026-10-26T10:00:00+08:00\"\nslug: test\nseat: code\nmodel: sonnet\nstyle: c\nscenarios: 3\n---\n# Person 說話測試（2026-10-26 10:00）\n\n（測試進行中，跑完會出現在這裡）\n"
	must(t, os.WriteFile(filepath.Join(dir, "2026-10-26-100000.md"), []byte(stale), 0o600))
	f.m.recoverToneRuns()
	r, err := f.m.PersonaTestResults(context.Background(), &sdk.PersonasTestResultsRequest{Slug: "test"})
	must(t, err)
	if len(r.Runs) != 1 || r.Runs[0].Status != "failed" || r.Runs[0].Reason != "核心重開，這次測試中斷" || r.Runs[0].Seat != "code" || !strings.Contains(r.Runs[0].Markdown, "核心重開，這次測試中斷") || r.Runs[0].At.IsZero() {
		t.Fatalf("要改成 failed：%+v", r.Runs)
	}
	// 沒有在跑的不動。
	before, _ := os.ReadFile(filepath.Join(dir, "2026-10-26-100000.md"))
	f.m.recoverToneRuns()
	after, _ := os.ReadFile(filepath.Join(dir, "2026-10-26-100000.md"))
	if string(before) != string(after) {
		t.Fatal("已經結束的結果不能再改")
	}
}

// TestScenarioQuipLines：情境內文裡的 `[梗本] <字>/<別的說法>｜<那一行梗>` 與 `[檢查] 提梗 <n>[-<m>]` 拿出來，不送給秘書；
// 格式錯、有檢查卻沒梗本、一則兩個檢查都回 INVALID 並指出第幾行。
func TestScenarioQuipLines(t *testing.T) {
	text := "## recall｜回頭提梗\n[梗本] 鬧鐘人/人形鬧鐘｜「鬧鐘人」五點半就醒——出處：聊天（life 11-02）\n[檢查] 提梗 1\n週六又早起\n\n## loose｜範圍\n[梗本] 泡麵王｜「泡麵王」——出處：晚餐\n[檢查] 提梗 0-2\n晚餐吃什麼\n\n## plain｜沒有梗\n你好\n"
	list, err := parseScenarios(text, "code")
	must(t, err)
	if len(list) != 3 || list[0].body != "週六又早起" || len(list[0].quips) != 1 || list[0].quips[0] != "「鬧鐘人」五點半就醒——出處：聊天（life 11-02）" ||
		strings.Join(list[0].quipKeys[0], ",") != "鬧鐘人,人形鬧鐘" || *list[0].check != (quipCheck{1, 1}) {
		t.Fatalf("recall：%+v", list[0])
	}
	if *list[1].check != (quipCheck{0, 2}) || list[2].quips != nil || list[2].check != nil {
		t.Fatalf("loose／plain：%+v %+v", list[1], list[2])
	}
	_, err = parseScenarios("## a｜b\n[梗本] 沒有直線\n內容", "code")
	toneErrLine(t, err, "第 2 行")
	_, err = parseScenarios("## a｜b\n[梗本] x｜y\n[檢查] 提很多\n內容", "code")
	toneErrLine(t, err, "第 3 行")
	_, err = parseScenarios("## a｜b\n[梗本] x｜y\n[檢查] 提梗 1\n[檢查] 提梗 0\n內容", "code")
	toneErrLine(t, err, "第 4 行")
	_, err = parseScenarios("## a｜b\n內容\n\n## c｜d\n[檢查] 提梗 1\n內容", "code")
	toneErrLine(t, err, "第 4 行")
	_, err = parseScenarios("## a｜b\n[梗本] x｜y\n[檢查] 提梗 1\n", "code")
	toneErrLine(t, err, "沒有內容")
}

// TestPersonaTestJudgesQuipMentions：情境帶了梗本就換掉那一則開場的梗本段（只那一則），跑完照回覆判提梗條數：
// 完全不提、提太多、不該提卻提都抓得到；沒跑成的不算；結果寫進報告與檔頭的 checks。
func TestPersonaTestJudgesQuipMentions(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	book := "[梗本] 鬧鐘人｜「鬧鐘人」五點半就醒——出處：聊天（life 11-02）\n[梗本] 泡麵王/泡麵大王｜「泡麵王」最拿手泡麵——出處：晚餐（life 11-05）\n"
	writeScenarios(t, f, "## none｜完全不提\n"+book+"[檢查] 提梗 1\n今天早起\n\n"+
		"## one｜提一條\n"+book+"[檢查] 提梗 1\n鬧鐘人又上線\n\n"+
		"## many｜提太多\n"+book+"[檢查] 提梗 1\n鬧鐘人跟泡麵大王，鬧鐘人\n\n"+
		"## quiet｜不該提\n"+book+"[檢查] 提梗 0\n好累，泡麵王\n\n"+
		"## boom｜沒跑成\n"+book+"[檢查] 提梗 0\n這則會失敗\n\n"+
		"## plain｜沒有檢查\n平安\n")
	f.e.Configure(fake.Behavior{Delivered: true, AskFailOn: "會失敗"}) // 假引擎回 "[fake] <訊息>"，用訊息控制回覆裡有哪些梗
	_, err := f.m.TestPersona(ctx, &sdk.PersonasTestRequest{Slug: "test"})
	must(t, err)
	run := waitTone(t, f, "test")
	if run.Status != "done" || run.Checks != "通過 1／4（沒過：none、many、quiet）（沒跑成不算：boom）" {
		t.Fatalf("檢查結果：%q\n%s", run.Checks, run.Markdown)
	}
	for _, want := range []string{
		"## none｜完全不提\n\n> 今天早起\n\n[fake] 今天早起\n\n（這則開場的梗本換成情境寫的 2 條：鬧鐘人、泡麵王）\n\n檢查：✗ 完全沒回頭提梗，規定 1 條",
		"檢查：✓ 回頭提了 1 條梗（鬧鐘人），規定 1 條",
		"檢查：✗ 提太多：2 條（鬧鐘人、泡麵王），規定最多 1 條",
		"檢查：✗ 這則不該提梗，卻提了 1 條（泡麵王）",
		"檢查：沒跑成，不算",
		"## plain｜沒有檢查\n\n> 平安\n\n[fake] 平安",
	} {
		if !strings.Contains(run.Markdown, want) {
			t.Errorf("報告少了 %q：\n%s", want, run.Markdown)
		}
	}
	if strings.Contains(run.Markdown[strings.Index(run.Markdown, "## plain"):], "檢查：") {
		t.Error("沒有檢查的情境不寫檢查")
	}
	for _, c := range f.e.Calls() {
		r, ok := c.Request.(*sdk.EngineAskRequest)
		if !ok || c.Method != "Ask" {
			continue
		}
		if strings.Contains(r.Message, "[梗本]") || strings.Contains(r.Message, "[檢查]") {
			t.Errorf("梗本與檢查行不送給秘書：%q", r.Message)
		}
		hasBook := strings.Contains(r.Instructions, "- 「鬧鐘人」五點半就醒——出處：聊天（life 11-02）")
		if (r.Message == "平安") == hasBook || !strings.Contains(r.Instructions, "## 梗本") {
			t.Errorf("只有帶梗本的情境換掉開場的梗本段（%q）", r.Message)
		}
	}
}

// TestPersonaTestDefaultScenarios：家裡沒有情境檔時用內建的通用情境（含三個提梗的），而且內建的不含真實人名。
// Guards: REQ-130
func TestPersonaTestDefaultScenarios(t *testing.T) {
	f := setup(t)
	_, err := f.m.TestPersona(context.Background(), &sdk.PersonasTestRequest{Slug: "test"})
	must(t, err)
	run := waitTone(t, f, "test")
	if run.Status != "done" || !strings.Contains(run.Markdown, "內建的預設情境") {
		t.Fatalf("結果：%+v", run)
	}
	list, err := parseScenarios(defaultScenarios, "code")
	ids := []string{}
	for _, sc := range list {
		ids = append(ids, sc.id)
	}
	if err != nil || strings.Join(ids, ",") != "report,tired,incident,quip_recall,quip_once,quip_tired" {
		t.Fatalf("內建要有三個通用情境與三個提梗情境：%v %v", ids, err)
	}
	// 提梗的情境要抓得到「完全不提」（自然回頭提的那則規定 1 條）與「提太多」（最多 1 條；已經提過、累的時候規定 0 條）。
	for _, sc := range list[3:] {
		if sc.check == nil || len(sc.quips) < 2 {
			t.Fatalf("%s 要自己帶梗本（至少兩條才抓得到提太多）與檢查：%+v", sc.id, sc)
		}
		for _, keys := range sc.quipKeys {
			for _, k := range keys {
				if strings.Contains(sc.body, k) {
					t.Errorf("%s：主人的訊息不能含梗的字「%s」（回覆照抄他的話會被當成提梗）", sc.id, k)
				}
			}
		}
	}
	if *list[3].check != (quipCheck{1, 1}) || *list[4].check != (quipCheck{0, 0}) || *list[5].check != (quipCheck{0, 0}) {
		t.Fatalf("自然回頭提＝1 條；這次對話提過、主人累＝0 條：%+v %+v %+v", list[3].check, list[4].check, list[5].check)
	}
	// 這份名單是假名、公開角色名與通用的例子；真正的私人字由公開前的檢查（scripts/public-check.sh）把關，不寫進 repo。
	for _, name := range []string{"Amy", "露娜", "Athena", "雅典娜", "輝夜", "阿媞米斯", "米卡", "Acme", "Jira"} {
		if strings.Contains(defaultScenarios, name) {
			t.Errorf("內建情境不能出現真實人名或內部名詞：%s", name)
		}
	}
	// 通用的內部痕跡：單號、作者電腦上的路徑、內部網路的網址。
	for _, trace := range []string{"DEV-", "SEC-", "OF-", "~/Workspace", "/Users/", "ts.net"} {
		if strings.Contains(defaultScenarios, trace) {
			t.Errorf("內建情境不能出現內部痕跡：%s", trace)
		}
	}
	if id := regexp.MustCompile(`\b[A-Z]{2,}-[0-9]+\b`).FindString(defaultScenarios); id != "" {
		t.Errorf("內建情境不能出現單號：%s", id)
	}
	if !strings.Contains(defaultScenarios, "主人") || !strings.Contains(defaultScenarios, "{name}") {
		t.Error("內建情境用「主人」與 {name}")
	}
}
