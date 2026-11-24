# Schema：persona（人設卡）

Status: DRAFT — 設計 2026-10-23（P0-S5），pending author review. 共通規則見 `README.md`。

人設是「個性與口吻」，跟職責分開（換人設不換職責）。需求：REQ-43、REQ-130（角色庫，P6-5a，D-31）。

## 路徑
- 官方角色：`personas/<slug>.md`（唯讀；角色庫也讀這裡）。代號 `^[a-z][a-z0-9]*$`；`_` 開頭與 `README` 不是卡。
- 使用者自己的角色：`personas/mine/<slug>.md`（角色庫的新增、複製與 `closet-mine` 建的都在這；只有這裡可寫）。代號一律 `my_<名字>`（`^my_[a-z0-9]{1,24}$`，DEV-154；官方代號不收底線，所以撞不到）；以前建的、沒有 `my_` 的照樣讀得到，那種舊代號跟官方角色資料夾的卡同一個代號時**官方優先**（套件角色的代號不同，不參與這條）：我的那張在角色庫標「被官方蓋住」、`check` 報問題，指派時用官方那張；跟的是跟著程式的公開卡（官方角色資料夾沒有那張）時**我的優先**，程式帶的那張不列（說明卡 agents 的 PRS-27）。兩種情況 `evergenie doctor` 都會提醒改成 `my_` 開頭。
- 套件帶來的角色（SEC-302 S2、U-1、U-3，REQ-313）：套件資料夾內的卡檔，由 `package.toml` 的 `[[contributes.persona]]`（`{slug, path}`，慣例 `personas/<slug>.md`）宣告；`slug` 要符合 `^[a-z][a-z0-9]*$`、套件內不重複。**有效代號是 `<套件id>.<slug>`**（例 `garden.fairy`；套件 id 沒有點，所以第一個點把代號切成兩半），人設欄位（agent 的 `persona`）、`agents.assign`、`personas get|check`、`/avatars` 都用這個代號；官方與我的代號不含點，所以不會撞名，兩個套件都有 `fairy` 也不撞。唯讀（改套件檔，或 `personas copy` 成我的）；`packages/mine/` 的套件可以「放進套件」寫入新卡（`personas new|copy|move --to-package`，REQ-320：卡寫進 `personas/<slug>.md`、`package.toml` 附 `[[contributes.persona]]`、頭像複製進 `personas/avatars/`、起初 `status: design`；來源「我的角色」不動），官方與 installed 層拒絕；`personas live|design` 可改 mine 層套件內卡的 `status`（REQ-323）。套件樣板 `agents/<id>/agent.md` 的選填 `persona` 可指到官方、我的或套件內角色，作為 `packages sync` 的預設角色（REQ-324）。卡格式與欄位跟官方卡一樣（`status: design` 也適用，設計中不能指派）。套件停用／被拒時角色標「不能新指派」，已指派的秘書不受影響（說明卡 agent 的 AGT-24）。**頭像**放套件內 `personas/avatars/<slug>.png`（動態 `.mp4`），解析鏈見說明卡 board 的 WEB-25。
- 說話層強度：內建四份 A–D 在核心程式裡（`internal/base/agent/styles/`）；`custom` 讀 `personas/_style.md`（共用，所有人設共用，由 `agent` 組裝指令時附上）。選哪一份存 `home.toml` 的 `[persona] style`（schema home）。
- 測口吻的情境檔（P6-5b）：`personas/_test/scenarios.md`（使用者自己的檔，沒有就用核心內建的六個情境：三個通用的加三個提梗的）。格式：`## <id>｜<標題>[｜<職位>,<職位>]`，下面是主人這一輪送進去的訊息；沒寫職位＝每個座位都跑，同 id 可依座位寫不同版本；`{name}` 換成被測人設的名字；第一個 `##` 之前的文字略過；內文的 `[梗本] <字>/<別的說法>｜<那一行梗>` 與 `[檢查] 提梗 <n>[-<m>]` 兩種行不送出，給這則換開場的梗本、跑完自動判提梗條數（DEV-164，說明卡 session 的 SES-24）。測試結果是衍生檔 `runtime/persona-tests/<代號>/<時間>.md`（不進 git，欄位與狀態見說明卡 session 的 SES-25），不是人設卡的一部分。
- 套件 `closet-mine`（展示間的「新增角色」）建立的角色：`personas/mine/<代號>.md`（`---` YAML 檔頭 `schema: 1`、`name: "<名>"`，內文＝一句話介紹；代號 `my_<名字>`，沒給是 `my_<n>`；以前的 `my<名字>`、`mine<n>` 照樣讀得到，圖在 `personas/mine/avatars/`，見 schema closet「closet-mine 的資料」、D-30）；程式只建最小的卡，個性與說話口吻的段落由角色庫（P6-5）補
- 頭像、衣櫃（`personas/avatars/`、`personas/wardrobe*/`）屬於 `closet` 套件的資料，不在這份 schema 內：圖片位置與命名、目錄與兌換資料見 schema closet（`personas/avatars/<人設>[@<衣服>][~N].<ext>`、`personas/mine/avatars/`）

## 欄位
frontmatter（沒有 `id`：`slug`＝檔名）：

| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema` | int | 是 | `1` | |
| `name` | string | 是 | — | 人名（對主人講話、看板顯示用） |
| `aka` | list of string | 否 | `[]` | 別名（主人可能這樣叫，例 `雅典娜`；`agent` 對照時使用） |
| `status` | enum | 否 | `live` | `design`＝設計中（不能指派、不進換角色的選單、不進名冊選單）；沒寫或 `live`＝上線。**只有我的卡會是 `design`**，官方卡的這個欄位一律當 `live`（D-31）。 |
| `intro` | string | 否 | — | 她第一次來跟主人打招呼、自我介紹的話（角色包裝進新角色時發的那則訊息，PRS-13），用她自己的口吻寫。沒寫就用內文「說話」段 `開場白：` 那一行的第一句＋「我是<name>，<`一句話：` 那一行>。」組出來（`composeIntro`）。 |

內文：以 `## 個性`、`## 人格`、`## 說話`、`## 日常小事`、`## 範例` 等二級標題分段的 Markdown。標題名稱是慣例，`agent` 組裝時整份原樣放進 session 指令，不解析段落內容。

`## 日常小事`（DEV-126，2026-11-05）：角色每天第一次開場帶一件自己的小事或編的小故事，讓她有自己的日子。官方卡與新卡範本都有這段，`開場白` 也寫成「招呼＋小事」。編的只是氣氛，不取代、不竄改跟主人有關的事實；說話層怎麼配合見 AGT-21。

`## 角色故事`（選填，2026-11-05）：她這個人的核心矛盾、在乎、害怕、小場景，放在 `## 說話` 之前；只寫她這個人，不寫工作。

**讀取相容**：上線前的檔案仍是舊格式（`+++` 包住的 TOML 檔頭，內容只有 `name`、`aka`）。解析器必須兩種都讀：`---` YAML 與 `+++` TOML；寫入（角色庫）一律寫 `---` YAML（`schema: 1`、`name`、`aka`、`status`，其他欄位原樣保留），內文原樣保留（結尾的空行會被整理掉）。角色庫改卡之前先備份到 `runtime/backups/personas/<代號>-<時間>.md`，再原子寫入。（遷移時不改寫 `personas/`。）

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 入 | agent 的 `persona` | `<slug>` | 由索引算；刪除或改名一個被綁定的人設會被擋下 |
| 出 | None. | — | — |

## 保存規則
- 不封存、不刪除（只讀或使用者自己的資料）；角色庫沒有刪除。
- 官方角色由官方發版覆蓋；使用者自己的永遠不碰（D-3）。公開角色（發行者的家 `[admin] public_personas` 列的）的卡跟著程式（官方套件 `personas` 的 `cards/<代號>.md`，PRS-25）：官方角色資料夾沒有同代號的卡時角色庫讀那張，有就用資料夾那張（角色包同步來的）；圖照舊走角色包。官方卡唯讀（PKC-6）：角色庫不能編輯、不能移到設計中，要改就「複製成我的」。
- 「設計中」是檔頭的 `status: design`，不是搬資料夾（D-31）。

## 從舊系統遷移
不用遷移：`+++` TOML 檔頭的舊格式卡照原位置讀（見上面的讀取相容）。
