package session

import (
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// 這段對話現在用的模型與思考力度（SES-60、D-46）。核心只存引擎與主人給的字，不解讀、不猜：
// 開的時候先記設定（EngineStartRequest 的 model、effort），引擎在啟動結果、事件、指令結果說得出實際的值就換成它的；
// 主人下 /model、/effort 做成了馬上換。都不知道就是空的，看板什麼都不畫。

// applyModel 把一組「現在的模型、名字、力度」記進 session；空的欄位不改。model 換了，給人看的名字跟著換
// （引擎沒給名字就清掉舊的，免得「Opus」配上新的 sonnet）。回有沒有變。
func applyModel(s *sdk.Session, model, label, effort string) bool {
	model, label, effort = strings.TrimSpace(model), strings.TrimSpace(label), strings.TrimSpace(effort)
	changed := false
	if model != "" && (model != s.Model || label != s.ModelLabel) {
		s.Model, s.ModelLabel = model, label
		changed = true
	}
	if effort != "" && effort != s.Effort {
		s.Effort = effort
		changed = true
	}
	return changed
}

// commandModel：換模型、換力度做成之後的值。引擎說得出就用引擎的，否則用這次的參數。
func commandModel(cmd sdk.SessionCommand, arg string, res *sdk.EngineCommandResponse) (model, label, effort string) {
	model, label, effort = res.Model, res.ModelLabel, res.Effort
	switch cmd {
	case sdk.SessionCommandModel:
		if model == "" {
			model = arg
		}
	case sdk.SessionCommandEffort:
		if effort == "" {
			effort = arg
		}
	}
	return model, label, effort
}
