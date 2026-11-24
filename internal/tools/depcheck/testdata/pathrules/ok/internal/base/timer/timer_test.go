package timer

// 測試檔裡的假實作不受「底層不准有 Routes／Commands」限制。
type fake struct{}

func (fake) Routes() []string { return nil }
