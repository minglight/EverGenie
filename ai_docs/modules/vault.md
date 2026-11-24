# 模組：vault（`internal/base/vault`）
Status: DRAFT — 設計 2026-10-23（P0-S7），pending author review.

**目的：** 檔案資料庫：Obsidian 格式（YAML frontmatter＋`[[連結]]`）、schema 驗證、索引、單一寫入者、清道夫（D-8）。
**種類：** 底層模組（`internal/base/`；沒有 HTTP、沒有指令，只給別人呼叫）

## MUST
- **VLT-1** — 一筆資料一個 `.md`，欄位放 YAML frontmatter，關聯用 `"[[id]]"`。
- **VLT-2** — 每家的 serve 是本家 vault 唯一寫入者，以家的檔案鎖保證。引擎、CLI、套件經 VaultAPI 存取。
- **VLT-3** — 寫入用 `platform.AtomicWrite`。
- **VLT-4** — 每次寫入依 `schemas/<種類>.md` 驗證（必填、型別、enum、連結存在），不合格拒絕並回帶欄位名的白話錯誤。
- **VLT-5** — 清道夫每晚跑，**先產生報告再動手**；正本類只搬進 `archive/<YYYY-MM>/`，不刪除；`runtime/` 才會刪。
- **VLT-6** — 結束的任務單隔天封存、擱置的 30 天後封存；封存後以 `id` 仍查得到。
- **VLT-7** — runtime 與 vault 含個人資料，不進 git。備份由主人自行處理；EverGenie 不做跨台備份。
- **VLT-8** — 每次寫入追加一行變更紀錄（誰、哪一筆、什麼時候）：`runtime/logs/changes-YYYY-MM-DD.jsonl`，一行 `{at, by, kind, id, op}`；`op` 是 `create`（新增，P6-6 起 `Put` 一筆新的寫 `create`，之前寫 `put`）、`put`（整筆更新）、`patch`、`append`、`archive`、`move`。`Changes(q)`（API `vault.changes`，P6-6）讀它給設定→紀錄：新到舊、壞行略過、可依 `by`（`owner` 表示任何 `owner(…)`，其他要完全相同）、種類、日期（`since`／`until`，含，依家的時區）篩選，`limit` 預設 100、最多 500，`before`（上一頁回的 `next_before`，格式 `<日期>:<行號>`）接著讀更早的；只讀、不改寫入格式；保留 30 天由清道夫刪。
- **VLT-9** — 不依 home.toml 的舊 backup 設定自動執行快照或 Git 同步。離線工具與本機設定備份仍保留。
- **VLT-10** — 保留本機快照工具函式，核心不排程執行；備份由主人自行處理。
- **VLT-11** — 跨行程：`serve` 啟動時對 `runtime/vault.lock` 取排他檔案鎖（`flock`），整個生命週期持有；拿不到鎖就拒絕啟動寫入。遷移工具在 `serve` 停止時取同一把鎖。
- **VLT-12** — 行程內：每一筆（`kind`＋`id`）有自己的互斥鎖（分片鎖，固定 64 組，不隨資料量長大），整個「讀取、驗證、比對 `revision`、寫檔、更新索引」在鎖內完成；不同筆之間可以同時寫。`NextID` 每種 `kind` 一把鎖，計數器原子寫入 `.counters.toml`。讀取走記憶體索引，不拿寫入鎖。
- **VLT-13** — 使用者在外面手改檔案（Obsidian、編輯器）時，`vault` 以檔案監看更新索引；寫入前比對磁碟檔案的雜湊與索引的 `revision`，不符就回 `ERROR_CODE_CONFLICT` 讓呼叫者重讀，**絕不覆蓋使用者的修改**。
- **VLT-14** — 遷移專用的兩個方法：`ImportArchived(ctx, r, month, by)` 把新的一筆直接寫進 `archive/<YYYY-MM>/`（驗證與變更紀錄同 `Put`；已存在回 `ERROR_CODE_CONFLICT`），`EnsureCounter(ctx, kind, atLeast)` 讓計數器至少是 `atLeast`（不往回調）。一般程式不呼叫，封存走 `Archive` 與清道夫。
- **VLT-15** — 套件的資料種類（D-28）：`SetPackageSchemas(pkgID, specs)` 把種類 `<套件 id>.<種類>` 整批換掉（跟 `RegisterSchema` 共用同一把鎖，換完重建索引）；檔案放 `vault/pkg/<套件 id>/<種類>/<id>.md`，封存搬到同種類的 `archive/`；沒註冊的套件種類（停用、解除安裝、被拒）的檔案留在原地、不進索引、`validate`／`doctor` 不報錯，重新註冊就回來。編號：`seq` 種類 `NextID` 發 `<prefix><n>`，`given` 種類 `NextID` 回 `INVALID`「這種資料的編號要自己給」、`given` 的 id 要符合 `^[a-z0-9][a-z0-9-]*$`。封存（DEV-151）：套件種類封存後的紀錄不能再改（`Patch`、帶 revision 的 `Put` 回 `NOT_FOUND`「這個編號已封存」）；`given` 種類封存後可以用同一個編號再新增一筆，封存的檔案留在 `archive/` 不動，索引與 `Get`／`List` 看正本，被蓋過的封存檔只在磁碟上（不算壞檔，正本被外部刪掉就接回來）；同一個月同一個編號再封存放 `archive/<YYYY-MM>-<n>/`。正本還在時重複新增回 `CONFLICT`「這個編號已經有資料了」。
- **VLT-16** — 廢除（2026-10-31，計畫 `2026-10-31-dev-workshop-split`）：種類 `dev` 拿掉；開發任務是工坊家的一般任務單（種類 `task`）。舊的 `vault/admin/DEV-*.md` 由搬家腳本搬進工坊，核心不再讀。
- **VLT-17** — 不再注入機器 origin；歷史記錄的 origin 原樣讀取與保留，不批次改資料。session/origin.go 的觸發者判定仍照常運作。
- **VLT-18** — 主人自己的東西不掃（DEV-154，作者 2026-11-12：家裡都是主人個人的，系統不認得的不掃、不碰、不擋升級）：`vault/` 底下只認核心的最上層名字（`agents`、`goals`、`messages`、`tasks`、`reminders`、`jobs`、`policy`、`rules`、`pkg`、`notes`〔全家共用的主題筆記，DEV-161〕、`profile.md`；清單在 `ignore.go` 的 `vaultRoots`，`RegisterSchema` 遇到自訂路徑用到清單外的資料夾會 panic，`TestRegisterSchemaRejectsUnknownRoot`）；其他最上層資料夾與檔案（主人的筆記、整份備份 `pkg.bak-…`）、任何一層隱藏的（`.` 開頭，例 `.obsidian`）、任何一層名字含 `.bak-` 的備份、核心產生的筆記索引（`notes/索引.md`、`agents/<職位>/索引.md`，`IndexFileName`／`generatedIndex`；DEV-161，`schemas/note.md`「索引」）、`notes/` 底下沒有自稱主題筆記（檔頭有 `name`）或分類說明（`kind: category`）的檔（檔頭讀不懂的也算；`OwnerNoteFile`，掃描與監看共用，測試 `TestOwnerNoteFile`），都不進索引、不監看、不算壞檔（`ignore.go` 的 `notOurs`，啟動掃描、套件種類重掃、檔案監看共用）。秘書自己的做事筆記資料夾 `agents/<職位>/notes/` 只認一層 `.md`（加上 `notes/archive/<月>/` 的封存），子資料夾整個不掃（`notOurs`）；那一層裡**沒有檔頭**（第一行不是 `---`）的 `.md` 不算壞檔：整份當內文，`updated_at`／`created_at` 取檔案修改時間、`by` 是 `external`、`agent` 是職位，讀得到、搜得到（`headerlessOwnNote`／`decodeIndexed`，啟動掃描與監看共用，DEV-161，測試 `TestHeaderlessOwnNote`）；經 `Put` 帶它的版本（檔案雜湊）寫回才補上真正的檔頭。檔頭有但壞掉的照舊是壞檔。核心資料夾裡格式不對或放錯位置的檔案照舊標成壞檔（VLT-13、VFS-8）。
- **VLT-19** — 官方層（DEV-154 第 1 件，作者 2026-11-12「官方的東西不放家裡、升級不寫家裡」；`official.go`）：官方角色放家外的家（佈局第 2 版而且沒寫 `official_personas = "home"`）多一個家外面的資料夾 `<官方角色資料夾>/records/`，排法跟 `vault/` 一樣（`pkg/<套件>/<種類>/<編號>.md`），放官方角色包帶來的套件紀錄（官方衣服的目錄）。
  - 讀：`Get`、`List` 把官方層跟家裡的合起來；同一個編號家裡有（含封存的）就用家裡的。
  - 寫：主人或套件改官方層的一筆＝在家裡寫一份（主人改過的留著，官方之後換新也不蓋它）；官方層的檔只有 `OfficialPut` 寫（角色包同步，`cmd/evergenie/personarecords.go` 的 `OfficialStore`）。
  - 只收套件自己給編號（`given`）的種類；不進變更紀錄、不能封存；寫完發變更事件給訂閱的（新增 `created`、換掉 `updated`，署名 `official`；家裡有同編號的那筆不發——大家看到的還是家裡那份），衣櫃這類套件照它馬上重算，不用等下一輪（DEV-157，測試 `TestOfficialPutNotifiesWatchers`）；種類沒註冊、讀不懂、是符號連結的檔略過（官方層壞了不擋家裡的資料）。
  - 其他的家（第 1 版、`official_personas = "home"`）沒有官方層，行為跟以前一樣。測試：`official_test.go` → `TestOfficialLayerStaysOutOfHome`、`TestNoOfficialLayerWhenPersonasAtHome`。

## NEVER
- **VLT-N1** — 不提供「覆寫日誌」的介面；`journal` 只有 `Append`。
- **VLT-N2** — 清道夫不刪正本類資料。
- **VLT-N3** — 不逐筆存高頻事件為檔案（只更新 session 最新狀態，另寫每日 JSONL log）。

## 職責
- 路徑與檔名規則、frontmatter 序列化、schema 註冊與驗證、`[[連結]]` 解析（以 `id` 找，不看路徑）與反向索引、計數器（`.counters.toml`）、樂觀並行（`revision`）、封存、清道夫報告、變更紀錄。

## 不負責
- 任務、待辦的商業規則 → `tasks`、`reminders`；什麼時候該封存哪種資料的**規則**寫在 schema，由 `vault` 執行；訊息佇列 → `bus`。

## 介面

API：`sdk/vault.go` 的 `VaultAPI`（`Get`、`List`、`Put`、`Patch`、`Append`、`Archive`、`NextId`、`Changes`、`Watch`）。Go（給同一個行程裡的上層模組用）：
```go
package vault

type Store interface {
    Get(ctx context.Context, kind, id string) (Record, error)
    List(ctx context.Context, kind string, q Query) ([]Record, error)
    Put(ctx context.Context, r Record, by string, expectedRev string) (Record, error)
    Patch(ctx context.Context, kind, id string, set map[string]any, unset []string, by string) (Record, error)
    Append(ctx context.Context, kind, id, line, by string) error
    Archive(ctx context.Context, kind, id, by string) error
    NextID(ctx context.Context, kind string) (string, error)
    Watch(ctx context.Context, kinds ...string) (<-chan Change, error)
}
type Record = sdk.Record
// Spec 的完整欄位與回呼：internal/base/vault/schema.go。
type Schema interface {
    Validate(Record) error
    Closed(Record) (closed bool, at time.Time)   // 這筆是不是已結束（封存與群組判斷共用，規則寫在 schema）
}
func (m *Module) RegisterSchema(sc Spec)   // 各模組啟動時註冊自己的種類（sc.Kind）
func (m *Module) Closed(r Record) (bool, time.Time) // 未註冊種類回 false
func (m *Module) SetPackageSchemas(pkgID string, specs []Spec) // 套件的資料種類（P6-3a）：種類以 "<pkgID>." 開頭的整批換成 specs，傳空＝全拿掉
```

### P1 骨架對照
- P2 的 Record 直接使用 SDK；Spec 定義在 `internal/base/vault/schema.go`，含 Kind、Fields、Path、ArchiveAt、ClosedFunc，值接收者實作 Schema。
- `SetHome(home string)` 只設定路徑；Start 呼叫 Open；New 不做 I/O。Open 取得排他鎖、建立索引與載入計數器，Stop 釋放鎖。未設定家的骨架生命週期仍可測試。
- Store 已實作 Get／List／Put／Patch／Append／Archive／NextID／Watch；Watch 使用 fsnotify，各訂閱 channel 緩衝 256，取消時關閉。寫入經分片鎖、revision 與磁碟雜湊檢查；Get／List 回傳副本。各 kind 計數器有自己的鎖，共用計數檔提交另以短鎖避免遺失更新。
- 搬檔透過 `internal/base/platform/move.go` 的 Move，資料寫入透過 AtomicWrite；沒有在 vault 直接呼叫 os.Rename 或 os.WriteFile。
- Evidence: `internal/base/vault/store_test.go` → TestWriteIsAtomic、TestNoPartialReads、TestSingleWriterLock、TestConcurrentPatchSameRecord、TestConcurrentPutOneConflicts、TestShardedLocksDoNotBlockOtherRecords、TestSchemaRejectsBadLink、TestChangeLogOneLinePerWrite、TestJournalAppendOnly、TestArchivedStillGettable、TestNextIDContinuesCounter、TestPutRejectsExternalChange。
- Fixture：`internal/base/vault/homefixture/homefixture.go` → New／WithRecords；包含完整必要設定、可注入時鐘、清理 Store 與正式家路徑防護。

- Evidence: `internal/base/vault/watch.go` → Watch／Invalid；`watch_test.go` → TestExternalEditNeverLost、TestInvalidExternalFileReported、TestWatchReportsOwnWrites、TestWatchFiltersKinds、TestExternalDeleteDropsFromIndex。`sweep.go`／`snapshot.go` → Sweep／Snapshot，驗證見 `maintenance_test.go`。
### P5 實作對照（S5：doctor 的資料檢查）
- Evidence: `internal/base/vault/validate.go` → `Module.InvalidRecords`、`InvalidRecord`；測試 `internal/base/vault/validate_test.go` → `TestInvalidListsBadRecords`（REQ-117）。
- `InvalidRecords(ctx)` 用已註冊的 schema 掃索引裡的全部紀錄（含封存），回對不上的：種類、id、路徑、哪個欄位、白話原因；另外把「檔案讀不了／放錯地方」的路徑（`Invalid()` 那一批）也一併列出。唯讀、不改索引；沒登記 schema 的種類不檢查。啟動時的索引只擋讀不了的檔案，欄位對不對要到這裡才檢查。名字不叫 `Invalid`，因為 `Invalid() []string`（只列讀不了的路徑）已經存在。

### P6-3a 實作對照（套件資料種類，D-28）
- Evidence: `internal/base/vault/pkgkinds.go` → `SetPackageSchemas`、`PackageKind`、`PackagePath`、`rebuildPackageIndex`；`index.go` → `inferIdentity`（`pkg/<套件>/<種類>/<id>.md` → 種類 `<套件>.<種類>`）、`indexFile`；`schema.go` → `Number` 型別、`Spec.IDMode`／`IDPrefix`、`checkID`；`counters.go` → `NextID`；測試 `internal/base/vault/pkgkinds_test.go` → `TestPackageSchemaRegisterAndIndex`、`TestNumberField`、`TestUnregisteredPackageDataKept`、`TestGivenKindHasNoNextID`（REQ-173）。
- 新型別 `Number`（`"number"`）：Go 的整數與浮點數都收，字串、布林不收。`Int` 也收整數值的浮點數（JSON 來的 `3`、`3.0`），3.5 不收。
- `Spec.IDMode` 是 `seq`（編號是 `IDPrefix` 加正整數，計數器 key 就是種類名稱，`.counters.toml` 裡寫成加引號的 key）或 `given`（編號由呼叫者給）；空＝核心種類，不檢查編號。`Put` 一律先檢查編號格式。`SetPackageSchemas` 重建索引時，seq 種類的計數器至少是索引裡（含封存）最大的編號。
- 索引規則：同一個種類與編號有兩個檔案時，`given` 種類留正本（都封存留 `updated_at` 較新的），其他種類算重複（啟動回 `CONFLICT`，套件重建索引標壞檔）。`vault/pkg/` 底下，種類沒註冊的檔案（讀得出來或讀不出來都一樣）留在原地，不進索引、不算壞檔（`Invalid()`、`InvalidRecords` 都不報）；啟動時的掃描與檔案監看一視同仁。套件的種類沒有 `ArchiveAt`，清道夫不動它們；使用者明確要封存走 `ArchiveNow`（API 的 `vault.archive`）。
- 套件的種類 schema 由 `pkg` 依 manifest 組出來：核心管的欄位（`schema`、`id`、`created_at`、`updated_at`、`by`）排最前面、都不必填，使用者在外面手改檔案缺了它們也不會壞；規格裡沒列的欄位不擋（Obsidian 手改友善），打錯欄位名由 `evergenie data` 的 CLI 擋。

## 事件
- 發出：None（`vault` 在 `bus` 之下，不直接發事件）。變更經 `Watch` 送出，由 `bus` 轉發成 `vault.changed`（`{kind, id, type, by}`），見 `bus.md`。
- 訂閱：None.

## 儲存
全部：`schemas/README.md` 與 `schemas/<種類>.md`。目錄樹見 `architecture.md`「家的目錄」。

## 依賴
`platform`

## 錯誤
`ERROR_CODE_INVALID`（schema 驗證失敗，帶 `field`）、`ERROR_CODE_NOT_FOUND`、`ERROR_CODE_CONFLICT`（`expected_revision` 不符、id 已存在）、`ERROR_CODE_UNAVAILABLE`（沒拿到單一寫入者的鎖）。

## 不變條件
- 對任何一筆資料，「寫入成功」之後磁碟上的檔案一定通過 schema 驗證。
- 同一時間只有一個寫入者；第二個行程啟動時拿不到鎖就拒絕寫入。
- 封存前後 `Get(kind, id)` 結果相同，`[[連結]]` 仍可解析（例外：`given` 種類封存後又新增同編號的一筆，`Get` 看新的，見 VLT-15）。
- 清道夫的報告列出的每一筆，動手後都能在 `archive/` 找到。
- `runtime/` 在 `git status` 中不出現。
- 每個成功的寫入對應恰好一行變更紀錄。
- 兩個 goroutine 同時 `Patch` 同一筆的不同欄位，結果兩個欄位都在；同時 `Put` 同一筆（帶相同 `expected_revision`），恰好一個成功、另一個得到 `ERROR_CODE_CONFLICT`。
- 一個行程持有 `vault.lock` 時，第二個 `serve` 啟動寫入失敗；持有者結束後可以啟動。
- 在寫入的同時用外部程式改同一個檔案：外部的修改要嘛被保留、要嘛寫入得到 `ERROR_CODE_CONFLICT`；不會有「外部修改消失而沒有錯誤」。（限制：比對雜湊到 rename 之間的極短空檔內的外部寫入不在保證內；測試驗的是外部寫入發生在 vault 讀取之後、寫入之前的情況。）
- 任一時刻讀到的檔案不是半個檔（用邊寫邊讀的壓力測試驗證）。
- 任何種類的資料檔都是 `---` YAML frontmatter 加 Markdown 內文；關聯欄位是 `"[[id]]"` 字串。
- 本機快照工具做的快照，還原後與當時的家逐檔相同（核心不排程快照，VLT-10）。

## 測試計畫
- `TestWriteIsAtomic`（REQ-3）、`TestSingleWriterLock`（REQ-2）
- `TestConcurrentPatchSameRecord`、`TestConcurrentPutOneConflicts`、`TestShardedLocksDoNotBlockOtherRecords`（REQ-2；用 `-race` 跑）
- `TestExternalEditNeverLost`、`TestNoPartialReads`（REQ-3）
- `TestSchemaRejectsMissingRequired`、`TestSchemaRejectsBadLink`（REQ-4）
- `TestSweeperReportThenArchive`、`TestSweeperNeverDeletesCanonical`（REQ-5）
- `TestTaskArchiveTiming`（REQ-6，凍結時鐘：done→隔天；shelved→30 天）
- `TestRuntimeNotInGit`（REQ-7）、`TestChangeLogOneLinePerWrite`（REQ-8）
- `TestDailySnapshot`（本機快照工具）
- `TestJournalAppendOnly`（REQ-44）
- `TestChangesPagingAndFilters`、`TestChangesEmptyHome`、`TestChangesReadsWhatWritesAppend`（REQ-8、REQ-111，P6-6：跨日檔、壞行、篩選、分頁）
- `TestPackageSchemaRegisterAndIndex`、`TestNumberField`、`TestUnregisteredPackageDataKept`、`TestGivenKindHasNoNextID`（REQ-173，P6-3a）
- `TestImportArchivedWritesIntoArchiveMonth`、`TestImportArchivedRejectsBadMonth`、`TestImportArchivedStillValidates`、`TestEnsureCounterContinuesFromImported`（REQ-121，P5-S2）
- 預期檔比對：舊格式樣本經遷移後 `Get` 的欄位對照（與 `cli` 的遷移測試共用 fixture）

## 已知限制
- 索引放記憶體，啟動時掃一次全部（家的資料量是幾千筆的量級）；超過再考慮落盤。
