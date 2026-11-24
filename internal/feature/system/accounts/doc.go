// Package accounts 是「AI 帳號」業務模組：home.toml 的 AI 帳號清單與新增／更新（只有主人能改）、
// 各帳號的用量與額度查詢，以及 `evergenie quota`、`quota check` 指令。說明卡：ai_docs/modules/system.md。
//
// 分工：引擎能力本身（問各帳號的用量、可選的引擎清單）在底層 engine／engine/registry；
// home.toml 的載入驗證與通用讀寫（ReadHomeTable、SetHomeTable）在 platform。這個業務模組負責「看」與「改」帳號、
// 把用量整理給 API 與指令。引擎怎麼問用量由 registry 把 engine 接進 Deps.Usage。
package accounts
