// Package board — 說明卡 ai_docs/modules/board.md（REQ-104、REQ-111、REQ-112、REQ-113、REQ-114、REQ-140、REQ-175、REQ-82）。
// 看板外殼（底層，在 internal/base/app 底下）：聽 127.0.0.1 的連接埠、送內嵌的畫面檔、把 /api/ 的請求（限白名單、身分一律換成主人）轉給核心的 API、
// 頭像與分享檔、聊天上傳、套件頁面的編譯與公開檔案。沒有自己的 sdk 服務：選單、名冊、總覽…各由業務模組登記。
package board
