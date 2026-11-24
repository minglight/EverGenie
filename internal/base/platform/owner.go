package platform

import "strings"

// ownerPrefix 是主人在 Actor 名稱裡的樣子：`owner(<入口>)`，例 `owner(web)`、`owner(cli)`。
const ownerPrefix = "owner("

// IsOwner 回這個操作者（Actor 名稱，已經過 ActorName 驗證）是不是主人。
// 「只有主人能改」的判斷只有這一份：業務模組不要自己寫 strings.HasPrefix(by, "owner(")。
func IsOwner(by string) bool { return strings.HasPrefix(by, ownerPrefix) }

// IsOwnerOrSystem 回這個操作者是不是主人或系統（`system`）：名冊、角色庫這類「主人或核心自己才能管」的寫入用。
// 秘書本人不算；要「主人或某位秘書本人」的就寫 `by == id || IsOwner(by)`。
func IsOwnerOrSystem(by string) bool { return by == "system" || IsOwner(by) }
