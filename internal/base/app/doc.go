// Package app 是組裝框架（底層模組）：把業務模組依序啟停，並收集它們登記的 HTTP 路由、
// 指令、健檢、定時工作與首頁資料。
//
// 說明卡：ai_docs/modules/app.md；業務模組的形狀：ai_docs/conventions/module-pattern.md。
//
// 這個套件不認得任何業務模組：業務模組實作 Module，再選用 RouteProvider／CheckProvider／JobProvider／BoardProvider
// 登記自己有的東西；指令用套件層級函式 Commands() 提供（指令行程只是 client，不建 Module），
// 由 cmd/evergenie 交給 (*App).Commands 合併。沒有全域變數或 init() 登記（MOD-N1）：
// 誰被組進來，完全看 cmd/evergenie/registry.go 傳給 New 的清單。
package app
