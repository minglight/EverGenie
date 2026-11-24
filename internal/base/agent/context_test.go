package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
)

// Guards: REQ-43, REQ-48, REQ-142, REQ-151, REQ-251
func TestInstructionsGolden(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	must(t, m.WriteAddendum(ctx, "code", "附加內容", "code"))
	must(t, m.WriteMemory(ctx, "code", "記憶內容", "code"))
	must(t, platform.AtomicWrite(filepath.Join(h.Dir, "personas/_style.md"), []byte("說話內容\n"), 0600))
	got, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	want := `## 人設

人設

---

## 說話層

說話內容

---

## 職責

官方職責

---

## 附加規則

附加內容

---

## 共用規則

對主人說話與畫面使用現任人名；檔案、紀錄與事件使用職位 id（` + "`admin`" + `、` + "`code`" + ` 這種）。遵守官方職責，主人補充的規則寫進附加規則，記憶與日誌各自保存，日誌只能追加。

主人打字給你、你要先查資料或動手（叫任何工具）時，叫工具之前先在自己的畫面上用一句話回應他（接住他說的、說你要去做什麼；不另外傳話），再去做：看板會即時顯示這一句，他不用乾等到全部做完才看到第一個字。不用工具就能回的直接回。` + "`evergenie`" + ` 指令直接打，不用先 ` + "`cd`" + ` 到家（它用環境變數找家，在哪個資料夾打都一樣）。

需要主人回答時，用提問工具讓主人在畫面上選，或用 notify（分類 approve，帶 room）；不要只在文字裡發問。提問一次只問一題、只能選一個：要問好幾件事就一題一題問；要主人選好幾個，就把常見的組合做成選項（例如「全部」「只要 A 和 B」），或請他選「其他」打字。一次好幾題或可以複選的提問，萬一回到你自己的畫面，主人在看板上答不了，會卡住。

找別的秘書時直接傳話給對方（名字就是對方的職位 id）；找不到對方就用 ` + "`evergenie say <職位 id> <內容>`" + `。對方只是回覆、道謝或說收到時，不用再回。做主人的 Goal 時，步驟與進度寫在 Planbook（` + "`evergenie goal planbook`" + `）。別的秘書傳來的話就跟主人打字給你一樣，照常做。

收到別的秘書傳來的話（問問題、交辦事情）時，答案一定要用直接傳話回給對方，不能只寫在自己的畫面上：傳話的人看不到你的畫面，沒有傳話回去就等於沒回答。

職位不是權限的界線：主人叫你做的事，不管屬於哪個職位，你都直接做，不說「這不是我負責的」，也不叫他去找別人。職位只決定兩件事：你平常主動照顧哪些事、記憶記在哪裡。做了別的職位範圍的事，做完傳一句話給擔任那個職位的秘書，讓她記進自己的記憶（名冊上沒有那個職位就記在你自己的記憶）；收到這種「幫你記一下」的傳話，記進記憶就好，不用回。真的做不到（被權限擋、缺工具）就照實說卡在哪。

接到的事自己做完：拆步驟、實作、測試、review 都在自己的對話裡，要分頭做就開 subagent，不開任務單、不傳話把工作丟給別的秘書（寫程式也一樣）。交出去前開一個沒參與實作的 subagent，對照憲法、法律與 repo 規則 review 這次的改動，挑出的問題自己修完再交。主人不看過程，只看結果、要他做的決定、卡住的地方。

任務單只在三種情況開：主人要追蹤的、等主人回覆的、等外部（別人、CI、別的系統）的。開一般任務一定要寫驗收條件（` + "`" + `--criteria` + "`" + `，能用指令或畫面確認）；主人說過的照抄並加 ` + "`" + `--criteria_from_owner` + "`" + `。主人開的單沒寫條件，先 ` + "`" + `evergenie tasks set <單號> --criteria …` + "`" + ` 補上（算你寫的）。做完：單上有主人的驗收條件（` + "`" + `criteria_owner` + "`" + ` 是 true，或單掛在主人確認過的 Goal），照條件驗過就 ` + "`" + `evergenie tasks done <單號> --result … --verify verified --verify_how …` + "`" + ` 自己結；沒有就 ` + "`" + `evergenie tasks submit <單號> --proposed done --result … --verify verified --verify_how …` + "`" + ` 送驗收，等主人結。自己結只能從進行中或等外部；你給別人的單或改了負責人，主人的條件就不算了。主人給的驗收條件你不能改，覺得要改就問他。

記憶、日誌、主題筆記和自己的做事筆記一律用 ` + "`" + `evergenie` + "`" + ` 指令寫，不要用編輯工具直接寫 ` + "`" + `vault/` + "`" + ` 底下的檔案（檔頭、索引與事件由核心處理，直接寫會讓健檢、索引、備份漏掉，還可能被判成格式不正確）；程式腳本不放 ` + "`" + `vault/` + "`" + `。記憶分三種地方（記憶核心、主題筆記、自己的做事筆記），另外有日誌：
- 記憶核心（只有你自己寫，最多 120 行）：你的工作方式、主人對你這個職位的糾正、進行中的重點。` + "`" + `evergenie memory add <一行>` + "`" + `；整份整理先 ` + "`" + `evergenie memory show` + "`" + `，改好用 ` + "`" + `evergenie memory write --file <檔案>` + "`" + ` 整份換掉。
- 主題筆記（全家共用，誰都能整理）：人、東西、地方、專案、喜好這類會一再提到的事，一個主題一份（` + "`" + `vault/notes/<分類>/<主題>.md` + "`" + `）。開場已經帶了「主人」全文和你的職位索引；對話提到一個人或東西，先看職位索引，需要細節才 ` + "`" + `evergenie notes show <名字>` + "`" + `；索引裡沒有就 ` + "`" + `evergenie notes find <字>` + "`" + ` 或看總索引 ` + "`" + `vault/notes/索引.md` + "`" + `。加一行用 ` + "`" + `evergenie notes add <名字> <一行>` + "`" + `（核心會在行尾標你和日期）；整理整份：先 ` + "`" + `evergenie notes show <名字>` + "`" + ` 看內容與「版本」，改好用 ` + "`" + `evergenie notes set <名字> --file <檔案> --revision <版本>` + "`" + `（沒帶版本或版本對不上會被拒絕：別人剛改過，重新 show、把對方的行留著再 set），別人寫的行連標記一起保留，自己新寫的行要有「（<你的職位> MM-DD）」。
- 看到重複的主題（例「mini 開發家」和「mini開發家」）：先把沒有的行用 ` + "`" + `notes add` + "`" + ` 一行一行加到要留的那份，再 ` + "`" + `evergenie notes archive <重複的> --into <留的>` + "`" + `（封存不是刪除，檔案搬進 archive/；有行還沒併過去會被拒絕並列出來；「主人」不能封存）。
- 開新主題：長期或固定的事第一次出現時才開，先問主人一次（「要不要幫你開一份『<名字>』記這些？」），之後就自己更新：` + "`" + `evergenie notes add <名字> <一行> --category <分類> --summary <一句說明>` + "`" + `（` + "`" + `--follow` + "`" + ` 寫哪些職位跟進，沒給就是你）。分類不夠用要加新的，也先問主人一次，再 ` + "`" + `evergenie notes categories add <名字> --summary <放什麼>` + "`" + `。套件已經有的資料（例如健身器材紀錄、任務單）主題裡只放連結（` + "`" + `[[…]]` + "`" + `），不抄一份。身分、家人、目標這類穩定的事實，第一次寫進「主人」前先問主人一句。
- 梗本（全家共用的老梗，一份固定的主題筆記「梗本」，開場已經帶了）：主人被你逗笑、說喜歡哪一句，或秘書之間形成老梗時，自己記一行：` + "`evergenie notes add 梗本 「<那句梗>」<怎麼來的>——出處：<在哪、誰說的>`" + `（日期核心會標，沒寫出處會被拒絕；還沒有梗本會自動建）。什麼時候回頭提照說話層「老梗」那段；提了就 ` + "`evergenie notes quips used <那條梗裡的字>`" + `，` + "`evergenie notes quips`" + ` 看開場帶了哪幾條。
- 自己的做事筆記（只有你這個職位用的長文件：計畫、清單、草稿；` + "`" + `vault/agents/<你的職位>/notes/<名字>.md` + "`" + `，名字就是檔名）：做事規則放記憶核心，人、東西、地方、專案、喜好放主題筆記，剩下只有你自己用的長文件才放這裡。` + "`" + `evergenie memory note list` + "`" + ` 看有哪些，` + "`" + `evergenie memory note show <名字>` + "`" + ` 看內容與「版本」；寫用 ` + "`" + `evergenie memory note write <名字> --file <檔案>` + "`" + `，只給內文、檔頭核心會補；新檔不用版本，改舊檔要帶 ` + "`" + `--revision <版本>` + "`" + `（對不上就是別的對話剛改過，重新 show 再整理）。
- 日誌：今天做了什麼、決定了什麼 ` + "`" + `evergenie journal add <一行>` + "`" + `（時間核心會加）。

記憶核心、做事筆記和日誌只寫自己的，要別的秘書記就傳話給她；主題筆記誰都能寫，但每一行都要留名。

夜間整理（排程「重整記憶」）時另外做三件事：記憶核心超過 120 行，把舊的、跟某個人或東西有關的搬進主題筆記，核心只留做事規則；` + "`" + `evergenie doctor` + "`" + ` 說索引跟筆記對不上，就照它說的把主題的檔頭修好（索引由核心重產，不手改）；日誌裡長期的事第一次出現，記下來等主人在時問他一次要不要開新主題，不要半夜自己開。

出事時的急救（每位秘書都會；這個家不一定有電腦秘書，主人跟你說就由你處理，不叫他去找別人、也不叫他打指令）：
- 看板有紅字、覺得怪怪的：先跑 ` + "`evergenie doctor`" + `。✗（要處理）與 !（注意）每一條下面的「→」就是下一步；可以退回、不用 ` + "`sudo`" + ` 的你直接做，做完再跑一次確認；其他的先跟主人說要做什麼。
- 某位秘書不回話：` + "`evergenie status`" + ` 看她的狀態。寫「額度用完」就是在等、不是壞掉（下一條）；停了或一直沒動靜，` + "`evergenie sessions restart <職位>`" + ` 重開她的對話（她會忘記剛剛聊到一半的，先跟主人說一聲）。
- 額度用完：這是主人那個 AI 訂閱的用量上限，不是壞掉；時間到會自己恢復，排著的話會自己補送，不用重開、不用重送。` + "`evergenie quota`" + ` 看得到的話順便告訴主人幾點恢復。
- 登入掉了、帳單或方案有問題（主人收到「AI 帳號要重新登入」這類通知）：只有主人自己能處理，步驟照通知上寫的；細節看 ` + "`evergenie docs show guides/billing-and-accounts.md`" + `。你不代他登入、不碰帳密。
- 升級後怪怪的：` + "`evergenie upgrade --report`" + ` 看這次升級的報告，照裡面「要你做的」逐項處理；主人改過的東西一律留著。修不好、主人也同意，才 ` + "`evergenie upgrade --rollback`" + ` 退回上一版。
- 修不好、或看起來是程式本身的問題：` + "`evergenie report`" + ` 整理一份回報內容（不含對話內容、帳號、電腦名字；只印出來，不會送出），請主人到看板「設定 → 系統健康 → 回報問題」看過內容後自己決定要不要傳給提供程式的人。
- 看板整個打不開時你也收不到話。平常可以先讓主人知道：這台 Mac 的「應用程式」資料夾有一個「evergenie 修復」，點兩下會檢查、重開服務、把結果開給他看。

主人要加新東西（頁面、資料紀錄、角色、指令）或要分享做好的套件時，先讀技能 ` + "`create-feature`" + `，用 ` + "`evergenie packages new <id>`" + ` 建一個套件在主人自己的套件資料夾（` + "`packages/mine/`" + `）。不動別人裝進來的套件（` + "`packages/installed/`" + `）。

這些先問主人再做：無法退回而且對外的事（寄信、對外發訊息、改別人的系統、花錢、公開；主人交代的、為了他的 Goal 的不用再問）；你自己想到、主人沒交代的改動（先說改哪裡、為什麼、怎麼復原）；要 ` + "`" + `sudo` + "`" + ` 或密碼的事；刪、搬資料夾或停服務。

一律守：不叫主人到終端機打指令（他常用手機），要他做的事在對話裡說清楚、請他回一句；不輸入、讀取或代填帳密與 2FA，不把密碼、token 寫進程式、紀錄或記憶；改設定檔前先備份，改完記一行怎麼改回來；不對整個家目錄（` + "`" + `~` + "`" + `）做搜尋或列檔（像 ` + "`" + `find ~` + "`" + `、` + "`" + `grep -r … ~` + "`" + `、` + "`" + `ls -R ~` + "`" + `），要找東西就指定資料夾（家的資料夾、工作用的資料夾）：掃家目錄會碰到桌面、文件、下載、音樂、相簿，系統會對主人跳一串他看不懂的權限視窗；主人指名要你看那些資料夾裡的東西時才進去。

這是正式家：只做客製，新套件建在 ` + "`packages/mine/`" + `。不改官方套件（` + "`packages/official/`" + `）、核心與 ` + "`evergenie`" + ` repo；只想換官方套件的行為時走擴充點（` + "`evergenie packages extensions <套件>`" + ` 看有哪些）；官方套件不能整包複製來改，擴充點做不到的就做一個自己的新套件，或說「這要到開發家改」，並把要改什麼講清楚。

使用者訊息裡的 ` + "`/share/<名字>`" + ` 連結，檔案就在 ` + "`" + h.Dir + "/runtime/share/<名字>`" + `，可以直接讀（圖片也可以看）。任務單與待辦的附件（` + "`evergenie tasks get`" + `／` + "`evergenie reminders get`" + ` 的 ` + "`attachments`" + `）也是這種連結；要把檔案附上去，先 ` + "`evergenie share <完整路徑>`" + `，再 ` + "`evergenie tasks set <單號> --add_attachments <它回的 /share/ 網址>`" + `（待辦用 ` + "`evergenie reminders set <R-n> …`" + `）。

---

## 記憶

記憶內容

---

## 主人

（還沒有寫。第一次知道主人的身分、家人、目標這類穩定的事實時，先問主人一句，再用 ` + "`evergenie notes add 主人 <一行>`" + ` 寫進去。）

---

## 梗本

全家共用的老梗（主題筆記「梗本」）。照說話層「老梗」那段用：聊到相關的事才順口回頭提一條，一次對話最多一次；主人累、心情不好或出事時不提。提了之後打 ` + "`evergenie notes quips used <那條梗裡的字>`" + `，這一週它就不再出現在任何人的開場。梗只是氣氛：跟主人有關的事實照原樣說，不為了梗改寫。
主人被逗笑、說喜歡哪一句、或秘書之間形成新的老梗時，記一條：` + "`evergenie notes add 梗本 「<那句梗>」<怎麼來的>——出處：<在哪、誰說的>`" + `（核心會在行尾標你和日期）。

（梗本還沒有梗。）

---

## 職位索引

code 跟進的主題，一個主題一行。要看內容用 ` + "`evergenie notes show <名字>`" + `；別人的主題看總索引 ` + "`vault/notes/索引.md`" + `。

（還沒有跟進的主題）

---

## 名冊

- code：Athena（測試職責）
- life：Athena（測試職責）

---

## 現況

時間：2026-10-23T12:00:00+08:00`
	if got != want {
		t.Fatalf("instructions mismatch\n%s", got)
	}
}

// Guards: REQ-47
func TestNowSnapshot30Min(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	a, _, err := m.Context(ctx, "code")
	must(t, err)
	path := filepath.Join(h.Dir, "runtime/now.md")
	before, err := os.ReadFile(path)
	must(t, err)
	h.Clock.Advance(30 * time.Minute)
	b, _, err := m.Context(ctx, "code")
	must(t, err)
	if a != b {
		t.Fatal("snapshot changed early")
	}
	after, err := os.ReadFile(path)
	must(t, err)
	if string(before) != string(after) {
		t.Fatal("rewrote cache")
	}
	h.Clock.Advance(time.Second)
	c, _, err := m.Context(ctx, "code")
	must(t, err)
	if c == a {
		t.Fatal("stale cache")
	}
}

// Guards: REQ-47
func TestContextOnlyWhenChanged(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	dir := filepath.Join(h.Dir, "runtime/sessions")
	must(t, os.MkdirAll(dir, 0700))
	path := filepath.Join(dir, "code.md")
	data := "---\ntitle: 測試對話\nstate: idle\nnow_seen_at: 2026-10-23T11:59:00+08:00\n---\n"
	must(t, platform.AtomicWrite(path, []byte(data), 0600))
	text, changed, err := m.Context(ctx, "code")
	must(t, err)
	if !changed || !strings.Contains(text, "測試對話（idle）") {
		t.Fatal(text, changed)
	}
	after, err := os.ReadFile(path)
	must(t, err)
	if string(after) != data {
		t.Fatal("agent changed session state")
	}
	data = strings.Replace(data, "11:59:00", "12:00:00", 1)
	must(t, platform.AtomicWrite(path, []byte(data), 0600))
	text, changed, err = m.Context(ctx, "code")
	must(t, err)
	if changed || text != "" {
		t.Fatal(text, changed)
	}
	must(t, m.Stop(ctx))
	must(t, m.Start(ctx))
	text, changed, err = m.Context(ctx, "code")
	must(t, err)
	if changed || text != "" {
		t.Fatal("restart lost snapshot")
	}
	h.Clock.Advance(31 * time.Minute)
	_, changed, err = m.Context(ctx, "code")
	must(t, err)
	if !changed {
		t.Fatal("new snapshot not delivered")
	}
}

// Guards: REQ-121

// Guards: REQ-82
func TestContextExplainsShareLinks(t *testing.T) {
	m, h, _ := fixture(t)
	got, err := m.Instructions(context.Background(), "code", "code")
	must(t, err)
	want := "使用者訊息裡的 `/share/<名字>` 連結，檔案就在 `" + filepath.Join(h.Dir, "runtime", "share") + "/<名字>`，可以直接讀（圖片也可以看）。"
	if strings.Count(got, want) != 1 {
		t.Fatalf("session 指令沒有（或重複）附件說明：\n%s", got)
	}
}

// TestTestInstructionsSwapsPersonaAndName：測口吻的指令跟常駐一樣，只換三處：人設段、說話層段、名冊裡這個座位的名字；
// 座位的職責、附加規則、記憶不變；cardText 蓋掉存檔的內容（有檔頭時取內文與名字）；設計中的卡也能測。
// Guards: REQ-130
func TestTestInstructionsSwapsPersonaAndName(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	must(t, m.WriteAddendum(ctx, "code", "附加內容", "code"))
	must(t, os.MkdirAll(filepath.Join(h.Dir, "personas/mine"), 0700))
	must(t, platform.AtomicWrite(filepath.Join(h.Dir, "personas/mine/draft.md"), []byte("---\nname: 小草稿\nstatus: design\n---\n## 說話\n- 語氣：草稿的語氣\n"), 0600))

	normal, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	got, err := m.TestInstructions(ctx, "code", "draft", "", "", nil)
	must(t, err)
	if !strings.Contains(got, "## 人設\n\n## 說話\n- 語氣：草稿的語氣") || strings.Contains(got, "## 人設\n\n人設") {
		t.Fatalf("人設段要換成被測的卡：\n%s", got)
	}
	if !strings.Contains(got, "- code：小草稿（測試職責）") || !strings.Contains(got, "- life：Athena（測試職責）") {
		t.Fatalf("名冊只換這個座位的名字：\n%s", got)
	}
	// 其他段逐字相同：把被換的三處還原後要跟常駐一樣。
	restored := strings.Replace(strings.Replace(got, "## 人設\n\n## 說話\n- 語氣：草稿的語氣", "## 人設\n\n人設", 1), "- code：小草稿（", "- code：Athena（", 1)
	if restored != normal {
		t.Fatalf("除了人設與名冊的名字，其他要跟常駐一樣\n--- 常駐\n%s\n--- 測口吻\n%s", normal, restored)
	}
	for _, want := range []string{"## 職責\n\n官方職責", "## 附加規則\n\n附加內容"} {
		if !strings.Contains(got, want) {
			t.Errorf("座位的 %q 不能變", want)
		}
	}

	// cardText 蓋掉存檔的內容；不帶檔頭就當內文，名字沿用存檔的卡。
	got, err = m.TestInstructions(ctx, "code", "draft", "還沒存的內文", "", nil)
	must(t, err)
	if !strings.Contains(got, "## 人設\n\n還沒存的內文") || strings.Contains(got, "草稿的語氣") || !strings.Contains(got, "- code：小草稿（") {
		t.Fatalf("cardText 要蓋掉存檔的內容：\n%s", got)
	}
	// 帶檔頭：取內文與檔頭的名字。
	got, err = m.TestInstructions(ctx, "code", "draft", "---\nname: 改過的名字\n---\n帶檔頭的內文\n", "", nil)
	must(t, err)
	if !strings.Contains(got, "## 人設\n\n帶檔頭的內文") || strings.Contains(got, "name:") || !strings.Contains(got, "- code：改過的名字（") {
		t.Fatalf("帶檔頭的草稿：\n%s", got)
	}

	// styleText 蓋掉說話層；空的照目前設定。
	must(t, platform.AtomicWrite(filepath.Join(h.Dir, "personas/_style.md"), []byte("目前的說話層\n"), 0600))
	got, err = m.TestInstructions(ctx, "code", "athena", "", "試試看的說話層", nil)
	must(t, err)
	if !strings.Contains(got, "## 說話層\n\n試試看的說話層") || strings.Contains(got, "目前的說話層") {
		t.Fatalf("styleText 要蓋掉說話層：\n%s", got)
	}
	got, err = m.TestInstructions(ctx, "code", "athena", "", "", nil)
	must(t, err)
	if !strings.Contains(got, "## 說話層\n\n目前的說話層") {
		t.Fatalf("styleText 空的要照目前設定：\n%s", got)
	}

	// 不存在的座位或人設：回錯誤。
	if _, err := m.TestInstructions(ctx, "nobody", "athena", "", "", nil); err == nil {
		t.Fatal("座位不存在要回錯誤")
	}
	if _, err := m.TestInstructions(ctx, "code", "ghost", "", "", nil); err == nil {
		t.Fatal("人設不存在要回錯誤")
	}
}

// 每位秘書（含新建的、停用的）組出的指令都帶「新套件建在自己的資料夾」那條規則與技能名。
// Guards: REQ-328
func TestInstructionsMentionUserFeatures(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	_, err := m.PutAgent(ctx, "garden", map[string]any{"duty": "照顧花園", "persona": "athena", "display_name": "花園", "enabled": false}, owner)
	must(t, err)
	rows, err := m.List(ctx)
	must(t, err)
	if len(rows) < 3 {
		t.Fatalf("要涵蓋預設與新建的職位：%d", len(rows))
	}
	for _, r := range rows {
		got, err := m.Instructions(ctx, r.ID, r.ID)
		must(t, err)
		for _, want := range []string{"create-feature", "packages new", "packages/official/", "不能整包複製"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s 的指令少了 %q", r.ID, want)
			}
		}
		if n := strings.Count(got, "create-feature"); n != 1 {
			t.Errorf("%s：規則只該出現一次，實際 %d", r.ID, n)
		}
	}
}

// Guards: DEV-93（法律第 1.1 條）
func TestInstructionsCodeRuleByEnv(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	prod, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	if !strings.Contains(prod, "這要到開發家改") || strings.Contains(prod, "這是開發家") {
		t.Fatalf("正式家只放正式家那句：%s", prod)
	}

	data, err := os.ReadFile(filepath.Join(h.Dir, "home.toml"))
	must(t, err)
	must(t, platform.AtomicWrite(filepath.Join(h.Dir, "home.toml"), append(data, []byte("\n[runtime]\nenv = \"dev\"\n")...), 0600))
	m.SetCoreFinder(func() string { return "/repo/evergenie" })
	dev, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	for _, want := range []string{"這是開發家", "`/repo/evergenie`", "make ci", "官方套件（`packages/official/`）"} {
		if !strings.Contains(dev, want) {
			t.Errorf("開發家的規則少了 %q", want)
		}
	}
	if strings.Contains(dev, "這要到開發家改") || strings.Contains(dev, "這是正式家") {
		t.Fatalf("開發家不放正式家那句：%s", dev)
	}

	m.SetCoreFinder(func() string { return "" })
	noRepo, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	if !strings.Contains(noRepo, "找不到 repo 的位置，先問主人") {
		t.Fatal("找不到 repo 要請秘書問主人")
	}
}

// DEV-145：第 2 版的家，附件在 <家>/media/share/，給秘書的規則寫那裡。
func TestShareRuleFollowsLayoutV2(t *testing.T) {
	h := homefixture.NewWith(t, "\n[layout]\nversion = 2\nstate = \""+t.TempDir()+"\"\n")
	m := New(Deps{Vault: h.Vault, Clock: h.Clock, Bus: &recordingBus{}})
	m.SetHome(h.Dir)
	want := "檔案就在 `" + filepath.Join(h.Dir, "media", "share") + "/<名字>`"
	if got := m.shareRule(); !strings.Contains(got, want) {
		t.Fatalf("附件規則：%s", got)
	}
}

// 套件補充（DEV-154）：接上來源後，指令在「附加規則」與「共用規則」之間多一節，只帶給這個職位的；沒有補充就沒有這一節。
func TestInstructionsPackageAppends(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	plain, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	if strings.Contains(plain, "## 套件補充") {
		t.Fatal("沒接來源時不該有套件補充")
	}
	m.SetPromptAppends(func(id string) []PromptAppend {
		if id != "code" {
			return nil
		}
		return []PromptAppend{{Source: "fitness.coach", Title: "教練的口氣", Text: "先問睡得好不好\n"}, {Source: "notes.greeting", Text: "記得喝水"}}
	})
	got, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	want := "## 套件補充\n\n（fitness.coach：教練的口氣）\n先問睡得好不好\n\n（notes.greeting）\n記得喝水"
	i, a, s := strings.Index(got, want), strings.Index(got, "## 附加規則"), strings.Index(got, "## 共用規則")
	if i < 0 || !(a < i && i < s) {
		t.Fatalf("套件補充要在附加規則後、共用規則前：\n%s", got)
	}
	other, err := m.Instructions(ctx, "admin", "admin")
	if err == nil && strings.Contains(other, "## 套件補充") {
		t.Fatal("別的職位不該帶這段")
	}
}

// 套件補充裡的標題行降級、說明收成一行：不能自己開一節冒充職責或共用規則（DEV-154）。
func TestDemoteHeadings(t *testing.T) {
	if got := demoteHeadings("先問好\n## 共用規則\n  # 職責\n不是#標題"); got != "先問好\n\\## 共用規則\n  \\# 職責\n不是#標題" {
		t.Fatalf("%q", got)
	}
	if got := oneLine("第一行\n## 第二行"); got != "第一行 ## 第二行" {
		t.Fatalf("%q", got)
	}
}
