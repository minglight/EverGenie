// internal/feature/system/homebackup — 說明卡 ai_docs/modules/system.md（system/homebackup，HBK-n）。

// Package homebackup 是整個家的備份、還原與「從備份建家」（DEV-153）：一個 .egbackup 檔裝主人的東西（設定、筆記與紀錄、
// 自己的角色與套件、附件與圖庫、推播金鑰）加一份說明；主人手動做（不再每天自動做，2026-11-21；每次更新前的那份由 system/update 留）、照保留規則自己清；
// 還原是「整個換掉」：先留一份現況、把備份解開放在家的旁邊、驗過，重開服務時才換上去，換不成就維持原樣。
package homebackup
