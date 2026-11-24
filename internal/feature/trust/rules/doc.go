// Package rules 是「信任等級」業務模組：每位秘書的權限等級（full／standard／readonly）、權限來源攤表、被擋紀錄、
// 快照，以及主人改等級。說明卡：ai_docs/modules/trust.md。
//
// 資料與規則的分工：settings/trust.toml 的格式、載入驗證、等級的查法（LevelFor）在 platform（engine、session 開機時就要用，
// 它們是底層，不能 import 業務模組）；被擋動作的寫入在 session。這個業務模組負責「看」與「改」：HTTP（system 服務的 trust、
// set_permission_levels）、指令（trust …、setup permissions）、寫檔與備份、快照。
package rules
