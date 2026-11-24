// Package doctor — 說明卡 ai_docs/modules/system.md（REQ-117, REQ-333, REQ-353, REQ-387, REQ-390）。
// 業務模組：整體健康檢查（`evergenie doctor`、看板健康狀態共用）。收集各業務模組登記的 Checks()，
// 加上還沒搬走的舊模組的檢查（設定、session、套件、權限、資料），依群組回報；只報告，不修復。
package doctor
