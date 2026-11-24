# 模組：trust（`internal/feature/trust`）
Status: DRAFT — 2026-11-02 S8-3 合併 trust.md、policy.md，pending author review.

**目的：** 秘書能做多少事：信任等級、權限現況、快照與拒絕紀錄（rules），以及秘書被擋住時的權限變更申請——秘書送申請、主人審、核心套用，秘書自己不能套用也不能審（requests，D-26）。
**種類：** 業務塊（7 塊之一；子套件 `trust/rules`（原 trust）、`trust/requests`（原 policy））

## MUST
### rules（信任等級）
- **TRU-1** — 自己用 `Routes()`（`Mount.Methods`）登記 `system.trust`、`system.set_permission_levels`、`web.get_trust`；套件層級 `Commands(printDoctor)` 登記 `trust show|scan|diff|denied|snapshot` 與 `setup permissions`（`printDoctor` 由 registry 傳 doctor 的列印函式）。
- **TRU-2** — 等級依序取秘書單獨設定、全家預設、內建 full（`LevelFor`、`LevelSource`）。只有主人能改；改前備份、其他欄位保留、寫後重讀驗證。
- **TRU-3** — engine 與 session 只透過它們自己定義的介面或函式取得能力（`LevelFor`、`AppendDenied`），由 registry 注入；未接上時啟動回錯。
- **TRU-4** — 健檢（`checks.go`，`Checks()`，doctor 的 `permissions` 群組）：`Deps.Engine.CheckPermissions` 的結果，產物跟 `settings/trust.toml` 不一致是紅字 `permissions.drift`（都一致一條綠字），引擎沒在跑／沒辦法比對是黃字 `permissions.check`；`Deps.Engine` 沒接就不登記。測試 `checks_test.go` → `TestPermissionChecks`。

### requests（權限變更申請）
- **POL-1** — `Request` 只寫申請單（`pending`）與發一則 `approve` 通知（選項「核准」「拒絕」「晚點再看」，掛在 `system` 底下）；不改任何權限、不呼叫引擎。`why` 空、`kind` 不認得、`rule` 的動作不在 read／write／commit／push／delete、範圍不是資料夾、`guidance` 不是一行都回 `Invalid`。
- **POL-2** — 過寬（沒範圍、或從根算不到四層）不拒收，`broad: true`，影響預覽開頭加警語。每個送件人同時最多 `MaxPendingPerSender`（10）張待審，超過回 `Conflict`。
- **POL-3** — `Decide`／`Revoke` 只接受 `by` 以 `owner(` 開頭；其他一律 `Forbidden`（HTTP 403）。
- **POL-4** — 核准要帶 `fingerprint`，跟現在算出的不同（申請改過、或有效規則世代變了）回 `Conflict` 要求重看。通知按鈕的答案不比指紋（通知內容就是申請當時的內容）。
- **POL-5** — 核准一律 `permanent = true`、不設 `expires_at`（D-27⑤：一直有效，撤銷才失效；請求的 `permanent` 收下但不影響結果）；`scope` 只能改窄（在原範圍裡面），`text` 可以換一句；改了重算 `broad` 與 `impact`。
- **POL-6** — 套用：`rule` 放進「套用中」清單，之後新開的 session 由引擎讀 `ActiveRules(agent)`；`guidance` 放進清單後呼叫 `Engine.PushPolicy`，失敗就換回原清單、再推一次（還原），申請標 `failed` 並記原因。存檔失敗也換回原清單。
- **POL-7** — `Revoke`（已套用才行）把申請拿出清單、有 guidance 就重推；引擎沒回應回 `Unavailable`，清單不動。`Tick`（每分鐘；`Jobs()` 登記的 `policy.tick`，由 `timer.JobRunner` 跑，沒啟動時是空操作）只處理帶 `expires_at` 的舊單（D-27⑤ 之前核准的），到期一樣拿出清單、引擎沒回應下一分鐘再試。
- **POL-8** — 每次離開 `pending`（核准、拒絕、失敗、撤銷、舊單到期）都用 `bus.Send` 送一則 `echo`（From `system`）回申請的秘書；主人自己送的不回。
- **POL-9** — 訂閱 `notice.answered`：答案「核准」以 `owner(notice)` 核准、「拒絕」拒絕；「晚點再看」與自動回答不動作。
- **POL-10** — `Start` 從 vault 重建「套用中」清單（`applied` 的申請），重開 serve 不會掉授權。
- **POL-11** — 每週摘要：每週一 09:00（家的時區）之後第一次 `Tick` 送一則 `digest` 通知（`system`、連到 `/policy`），列出上週一 09:00 到這週一 09:00 之間 `decided_at` 落在裡面的已套用、已撤銷、已到期、套用失敗的申請，以及目前套用中的清單；沒有變動不送。送過的週記在 `runtime/policy/digest-week`，同一週不重送、重開也不重送；送失敗下一分鐘再試。
- **POL-12** — 自己登記對外面（`conventions/module-pattern.md`）：`Routes()` 登記 sdk 的 `policy` 服務（`http.go`，驗操作者後直接呼叫本模組；只有主人能審與撤銷由模組判斷，秘書 `forbidden` → HTTP 403；錯誤經 `platform.SDKError`）；套件層級 `Commands()` 登記 `policy`（說明）、`policy request|list|show|decide|revoke`（`cli.go`，只用 `sdk` client 與 `cmdline`）。沒有首頁資料段、健檢。engine 的 `SetRuleSource`／`SetGuidanceSource` 接線在 registry。測試 `http_test.go` → `TestRoutesRegisterPolicyService`、`TestHTTPWritesRequireActor`、`TestHTTPRoundTripOwnerOnlyDecides`；`cli_test.go` → `TestCommandsListsAllPolicyCommands`、`TestFlagsFillRequestFields`、`TestDecideNeedsExplicitChoice`、`TestShowAndHelp`。
- **POL-13** — 登記通知來源（`notice.go`，APP-11）：`NoticeSources()` 回 `{policy, policy.changed, waitingNotices}`；`waitingNotices` 列出每張申請指到的審核通知（known），還在待審的算在等（waiting）。notify 拿著自己的鎖時叫它，所以只讀 vault、不拿 `m.mu`。測試 `TestNoticeSourceWaitingFollowsPending`。
- **POL-14** — 看板權限申請頁（`/policy`，`pages/policy/PolicyPage.tsx`）按完核准、拒絕、撤銷，那張卡會離開目前的檢視：照 `decide`／`revoke` 回的 `request.status` 用一句 toast 說結果（`decidedToastKey`：「已核准 P-n，在『已授權』看得到」「已拒絕」「已撤銷」）；**核准了但套用失敗**（`failed`）要講明「設定維持原樣、原因在紀錄」並留 10 秒——以前卡片默默消失，看不出成功還是失敗。測試 `pages/policy/policyText.spec.ts`「按完照伺服器回的狀態說結果」。
- **POL-15** — 權限申請頁按核准、拒絕、撤銷**送出失敗**時，原因用 toast 說（留 10 秒），不寫在卡片裡：失敗後清單會重抓（指紋過期要主人重看），那張要是已經在別處決定或撤銷了，卡片整張離開目前的檢視，以前寫在卡片裡的紅字跟著不見，看不出為什麼。撤銷失敗也重抓。vitest `pages/policy/policyText.spec.ts`「送出失敗的 toast 寫伺服器的原因、留 10 秒」；e2e `policy.spec.ts` 的 `TestPolicyDecideFailureShowsToast`。

## NEVER
- **TRU-N1** — 不 import 別的業務塊；不在 platform 留規則、編輯、快照或拒絕紀錄的實作。
- **POL-N1** — 不寫任何平台的原生設定、不讀 `trust.toml`：原生設定由引擎套件產生（D-21），`trust.toml` 由 platform 管。
- **POL-N2** — `ActiveRules`／`ActiveGuidance` 不取 `mu`（只取 `am`）：引擎會在 `Decide` 持有 `mu`、呼叫 `PushPolicy` 時回頭讀它們。
- **POL-N3** — 不 import 別的業務塊（depcheck 照路徑擋）；要別塊的能力就在自己的 `Deps` 定義小 interface，由 registry 注入。

## 職責
- rules：信任等級的優先順序與政策、權限現況與來源表、設定編輯、快照差異、拒絕紀錄；信任 API、指令與權限健檢。
- requests：申請單的建立、列出、審核、撤銷（舊單才有到期）；有效規則與 guidance 的清單；結果訊息與每週摘要。

## 不負責
- 設定型別、解析與驗證 → platform；執行時怎麼擋 → 底層 engine／session；規則怎麼翻成平台的設定 → 引擎套件（`engine-claude` 的 ECL-18）；把有效規則交給新開的 session → 底層 engine（`SetRuleSource`）；通知的送法與安靜時段 → inbox 塊的 notify；看板畫面 → web；動作分類（`policy decide|revoke` 算改設定）→ 引擎套件。

## 組裝（塊的 Module，S8-3）
`internal/feature/trust/module.go`（內嵌 `*app.Group`，APP-13）。Deps（塊外）：Vault、Notify、Bus、Engine（權限健檢＋套用 guidance，可以是 nil）、Clock。Start 順序 rules → requests；**整塊在底層 engine 之前啟動**（引擎握手讀已核准的 guidance）。沒有 setter：引擎讀 `rules.Policy`、`Requests().ActiveGuidance／ActiveRules`，由 registry 接。存取 `Levels()`／`Requests()`；指令 `trust.Commands(printDoctor)`。測試 `module_test.go`。

## 介面
### rules
API：sdk `system.trust`、`system.set_permission_levels`、`web.get_trust`（`GetTrust` 是獨立 API，不提供首頁資料段）。Go：`Show`、`SetLevels`、`LevelFor`、`LevelSource`、`DefaultFor`、`AppendDenied`／`ReadDenied`、`Checks()`、`Commands(printDoctor DoctorPrinter)`、`WizardLevel`。`Deps`：`Vault`（讀名冊）、`Engine`（`Permissions`：`CheckPermissions`）、`Clock`。

| 檔案 | 內容 |
|---|---|
| `module.go` | 生命週期、`Deps` |
| `rules.go` | 等級優先順序、政策 |
| `service.go` | 現況、來源表、快照差異 |
| `store.go`、`denied.go` | 設定編輯、快照、拒絕紀錄 |
| `checks.go` | 權限健檢 |
| `http.go`、`cli.go` | 路由與指令登記 |

### requests
API：`sdk/policy.go` 的 `PolicyAPI`（`Request`、`List`、`Get`、`Decide`、`Revoke`）。Go：
```go
package requests

type Applier interface{ PushPolicy(ctx context.Context) error }
func (m *Module) Request(ctx context.Context, in *sdk.PolicyRequestRequest, by string) (*sdk.PolicyRequest, error)
func (m *Module) List(ctx context.Context, statuses []sdk.PolicyStatus, agent string) ([]sdk.PolicyRequest, error)
func (m *Module) Get(ctx context.Context, id string) (*sdk.PolicyRequest, error)
func (m *Module) Decide(ctx context.Context, in *sdk.PolicyDecideRequest, by string) (*sdk.PolicyRequest, error)
func (m *Module) Revoke(ctx context.Context, id, note, by string) (*sdk.PolicyRequest, error)
func (m *Module) Tick(ctx context.Context, now time.Time)
func (m *Module) ActiveRules(agent string) []sdk.PolicyRule
func (m *Module) ActiveGuidance() []string
```
`Deps`：`Vault`、`Notify`、`Bus`、`Engine`（`Applier`，可以是 nil：guidance 只存著不套用）、`Clock`。
- Evidence: `internal/feature/trust/requests/module.go` → `Module`、`Deps`、`Start`、`reload`；`service.go` → `Request`、`cleanScope`、`broadScope`、`impactOf`、`fingerprint`；`decide.go` → `Decide`、`Revoke`、`swap`、`finish`；`events.go` → `Tick`、`handleAnswer`、`publish`、`digest`、`weekStart`；`schema.go` → `spec`、`encode`、`decode`；`http.go` → `Routes`；`cli.go` → `Commands`。
- 組裝（`cmd/evergenie/registry.go`）：trust 塊整塊在 engine 之前啟動；`engines.SetTrustSource(rules.Policy)`、`engines.SetGuidanceSource(trust 塊.Requests().ActiveGuidance)`、`engines.SetRuleSource(trust 塊.Requests().ActiveRules)`。

## 事件
- 發出：`policy.changed`（`{id, status, kind, by}`，requests）。
- 訂閱：`notice.answered`（requests，POL-9）。
- 送出封包：結果 `echo`（`bus.Send`，POL-8）。
- rules 沒有事件。

## 儲存
- rules：`schemas/home.md` 的 `settings/trust.toml` 與 `runtime/trust`（快照、拒絕紀錄）；編輯備份 `runtime/backups/trust`。
- requests：`schemas/policy.md`（`vault/policy/P-<n>.md`）；每週摘要記號 `runtime/policy/digest-week`。

## 依賴
`platform`、`vault`、`app`（登記介面與 `cmdline`，import）、`engine`（只有 rules 的健檢用它的 `NotRunning`、`CheckFailedPrefix` 常數；比對本身經 `Deps.Engine` 小 interface）。同塊兩個子套件彼此不 import。通知、訊息匯流排、引擎的套用都不 import：在各自的 `Deps` 定義小 interface（rules 的 `Engine`；requests 的 `Notify`、`Bus`、`Engine`），由 `cmd/evergenie/registry.go` 接上 inbox 塊的 notify、底層 bus 與 engine。

## 錯誤
- rules：`INVALID`（設定或等級錯誤）、`FORBIDDEN`（非主人編輯）、`UNAVAILABLE`（家尚未設定或檔案存取失敗）。
- requests：`INVALID`（欄位不合格、改寬範圍、拒絕時帶修改）、`FORBIDDEN`（秘書審或撤銷）、`CONFLICT`（不是 `pending` 還要審、不是 `applied` 還要撤、指紋過期、待審超過上限）、`NOT_FOUND`（沒有這張）、`UNAVAILABLE`（服務沒啟動、撤銷時引擎沒回應）。全部以 `ERROR_CODE_*` 回。

## 不變條件
- session 重讀信任設定失敗時仍用啟動時的設定；允許的動作不寫拒絕紀錄。
- `Request` 前後 `ActiveRules`、`ActiveGuidance` 不變、引擎沒有被呼叫。
- 撤銷後 `ActiveGuidance` 跟申請前相同，引擎拿到的 guidance 也相同。
- 引擎第一次推送失敗時，申請是 `failed`、清單與引擎拿到的 guidance 都回到申請前。
- 每週摘要同一週最多一則，重開 serve 也不重送；那週沒有權限變動就不送。

## 測試計畫
- rules：`internal/feature/trust/rules/http_test.go`、`cli_test.go`、`rules_test.go`、`checks_test.go`（TRU-4）；`internal/base/session/dispatch_test.go` 的重讀與 fallback；engine／session 的 `TestStartRejectsMissingTrust`。
- requests：`TestRequestOnlyWritesRequest`、`TestRequestValidation`、`TestPendingLimitPerSender`、`TestNotStartedIsUnavailable`、`TestOnlyOwnerDecidesAndRevokes`、`TestApproveRuleTakesEffectForNewSessions`、`TestApproveNeedsFreshFingerprint`、`TestGuidanceApplyAndRevokeRestores`、`TestApplyFailureRollsBack`、`TestApprovedNeverExpires`、`TestAppliedSurvivesRestart`、`TestNoticeAnswerDecides`、`TestWeeklyDigestListsGrantChanges`；HTTP 與指令見 POL-12。
- 跨模組：`TestPushPolicyCarriesGuidance`、`TestStartFillsActiveRules`（`internal/base/engine`）；`TestRulesBecomeAllowedTools`、`TestGuidanceWritesAutoModeAllow`（`engine-claude`）；`TestPolicyCommandsOnFixtureHome`（`cmd/evergenie`，真 serve＋fixture 家，含 403）；`TestBoardPolicyProxy`（`internal/base/app/board`）。

## 設計決定
- **D-26** — 秘書被擋時不能自己改權限，也不叫主人去改設定，而是提出申請：核心存一張 `P-<n>`（`vault/policy/`）並通知主人；主人在看板「權限申請」頁或通知按鈕上審，**套用由核心做，秘書的回合一律不能核准、拒絕或撤銷**（只認 `owner(…)`）。兩種中立類型：`rule`＝一條精準規則（哪位秘書、讀／改檔／commit／push／刪除、哪個資料夾；對外發訊息、花錢、改設定不能開成固定規則）；`guidance`＝一句話告訴平台的自動判斷「這類操作是主人要的」，影響同帳號的所有秘書，留給一條規則寫不完、同類被誤擋兩次以上的情況。引擎把它們翻成原生設定（產物不准手改）。核准不到期，只能撤銷收回；過寬的申請不拒收、標紅字；`guidance` 核准時附影響說明；核准帶預覽指紋，內容或有效規則變了要重看；套用失敗自動還原並標失敗；結果一定送回申請的秘書。跟 D-7 的 `RequestApproval`（這一次放不放行）是兩件事。否決：秘書直接改產物；過寬直接拒收；`guidance` 按兩次確認。

## 已知限制
- 核准不到期（D-27⑤）：權限會一直留著，只能靠撤銷收回；`expires_at`、`expired`、`Tick` 為相容之前核准的舊單而保留，新單用不到。
- 本機 API 沒有身分驗證：秘書可以自己呼叫 API 冒用 `owner(…)`；這擋的是模型判斷錯，不是惡意冒用（D-24）。`policy decide|revoke` 在引擎的動作分類算「改設定」，秘書回合會先問主人。
- 稽核：逐條比對在引擎套件（ECL-6 的 `doctor`），這裡只送每週摘要（POL-11）。規則（rule）是開 session 時帶進去的，不落在設定檔，沒有檔案可以比；只比 guidance 與手加的規則。
- 指令類的規則（commit、push、刪除）平台沒辦法限定資料夾，範圍只當說明。
- rules 沒有背景排程；trust diff 用 doctor 的權限健檢（TRU-4）。
