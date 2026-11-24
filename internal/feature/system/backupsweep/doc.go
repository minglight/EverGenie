// internal/feature/system/backupsweep — 說明卡 ai_docs/modules/system.md（system/backupsweep，BSW-n）。

// Package backupsweep 是每週的備份掃描（DEV-163，憲法第 9 條「備份有期限」，備份規則見 system 卡 BSW-4）：
// 系統級排程 backup-sweep（job action backup_sweep）到了，在固定的幾處找名字像備份的檔案與資料夾，分成已到期（可刪／在用／沒查完）、
// 名字不合規（附建議）、期限內（只算數量）、略過的例外，寫成一份報告（狀態資料夾的 backup-sweep/），出錯發 system.alert。
// 只出報告、不刪；例外是狀態資料夾 runtime/backups/ 的舊設定備份，照 settings 既有的保留規則自動清（settings.AutoPrune）。
package backupsweep
