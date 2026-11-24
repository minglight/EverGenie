# EverGenie

**安裝（Mac）**：先裝好並登入 Claude Code，在 Claude Code 裡貼上這個 repo 的連結，說「幫我安裝」。Claude 會照 [`INSTALL.md`](INSTALL.md) 幫你裝好、告訴你看板網址（給 AI：先讀 `INSTALL.md` 照做，不要跑 git）。

**費用**：EverGenie 本身免費。秘書用的是你自己的 Claude 帳號，所以要先有一個 Claude Code 能用的 Claude 訂閱（付給 Claude，不是付給 EverGenie）；價錢與方案會變，請到 Claude 官網看目前的方案。秘書用得多，訂閱的用量就用得快；用完會停下來等恢復，不是壞掉。

**出問題時**：這台 Mac 的「應用程式」資料夾有「evergenie 修復」（按 ⌘＋空白鍵搜尋），點兩下會檢查、重開服務、把結果開給你看。要回報問題，到看板「設定 → 系統健康 → 回報問題」；內容你先看過，系統不會自己送出任何東西。

AI 秘書系統的核心：Go 寫的常駐服務，管理一組 AI 秘書（Claude Code session）、任務、待辦、通知與看板；主人要的東西全部做成套件。

開發者與 AI 從 [`ai_docs/INDEX.md`](ai_docs/INDEX.md) 開始讀（架構、模組規則卡、怎麼建置與測試、發版流程）。

**授權**：EverGenie（程式與官方角色）照 [`LICENSE`](LICENSE)，**不是開放原始碼授權**：可以自己使用（個人或工作）、修改，原樣或改過的版本都可以免費分享（要附授權檔）；不能拿去賣、架收費服務，或用在別的產品。你自己做的套件、角色與衣服是你的。用到的第三方程式的授權聲明在 [`LICENSES/`](LICENSES/)。
