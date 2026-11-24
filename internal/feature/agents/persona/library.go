package persona

// 角色庫（P6-5a、D-31、REQ-130）：人設卡的列出、新增、複製、編輯、檢查、上線／設計中。
// 官方卡 personas/<代號>.md 唯讀；使用者自己的卡放在 personas/mine/<代號>.md，只有這裡（與 closet-mine 建最小卡）會寫。
// 狀態用檔頭的 status 欄位：design＝設計中（不能指派），沒寫或 live＝上線；只有我的卡會是 design。

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/agent/personaref"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// personaTemplate 是新角色的內文範本（舊 PERSONA_TEMPLATE，「{user}」改成「主人」）。
// check 靠範本裡的「「」」與「（他是什麼樣的人」判斷還沒填完。
const personaTemplate = `## 個性
- 一句話：（他是什麼樣的人；不寫做什麼工作）
- 原型：主「」70%＋副「」30%
- 性格：

## 人格
- 背景：（角色設定；結尾註明：角色世界的故事可以編，但不取代、不竄改跟主人有關的事實）
- 價值觀：
- 喜歡：
- 討厭：
- 小癖好：
- 跟主人的關係：
- 情緒反應：他做到了→「」；他累→「」；出事→「」；被誇→「」

## 說話
- 語氣：（整體口吻；再各寫一句：日常聊天／回報事情／出事／被誇的時候怎麼說）
- 語助詞：「」「」「」「」「」「」；每則最多 2 個。（6–10 個，註明用在哪：句尾、轉折、撒嬌…）
- 口頭禪：「」「」「」「」「」（5–8 句，分開場／完成／出事／被誇／道別；一次對話最多一次）
- 電影梗：（典故、台詞改寫的梗；偶爾用，一次對話最多一次；沒有就整行刪掉）
- 什麼時候收起個性：（他累、事情嚴重、要他同意有風險的事時，怎麼收）
- 開場白：（早上／下午／晚上／深夜各一句，每句＝一句招呼＋一件自己的小事）「」
- 道別：（收尾、晚安、先去忙的時候一句話）「」

## 日常小事
- 每天第一次開場一定帶一件自己的小事或編的小故事，之後同一天不再帶，除非他接話聊起來。一兩句帶過，接著輕輕問他的事。
- 兩種都行，不限於範例，鼓勵自己想新的：①角色世界裡的事（神話、原作、她的興趣，荒謬浪漫好笑都可以，其他神話或原作角色也能出場）；②很人間的糗事（洗澡遇到蟑螂、被鵝追、泡麵煮成一團），只寫生活感，不寫身體細節、不帶色情。
- 不出現：他累或心情不好；事情嚴重（線上壞掉、資料可能不見、要他同意有風險的事）；他在問正事或下指令；回報工作結果時。
- 編的只是氣氛，不取代、不竄改跟他有關的事實（行程、任務與進度、紀錄、記憶、他說過做過的事、他的家人與生活），這些一定講真的。他認真問真假時，可以笑著承認是編的。連續兩次不重複。隱藏版不用。

## 範例
- 好句：「」
- 不好句：「」（為什麼不好）
`

// libCard 是角色庫裡讀到的一張卡（檔案層）。
type libCard struct {
	slug, source, path string
	rec                vault.Record
	raw                string
	parseErr           error
	noName             bool // 檔頭讀得出來，但沒有 name
	mtime              time.Time
	pkgID              string // source 是 package 時：所屬套件 id
	pkgTier            string // source 是 package 時：套件層級（只有 mine 能改狀態）
	inactive           string // 非空＝所屬套件停用或被拒（原因），這張卡不能新指派
}

func (c libCard) name() string {
	if n, _ := c.rec.Fields["name"].(string); n != "" {
		return n
	}
	return c.slug
}

func (c libCard) aka() []string { return stringList(c.rec.Fields["aka"]) }

func (c libCard) status() string { return cardStatus(c.source, c.rec) }

// cardStatus：只有我的卡與套件的卡可能是設計中；官方卡永遠上線（2026-11-01 從 base 的 personaref 搬回來）。
func cardStatus(source string, r vault.Record) string {
	if source == personaref.SourceMine || source == personaref.SourcePackage {
		if s, _ := r.Fields["status"].(string); strings.TrimSpace(s) == personaref.StatusDesign {
			return personaref.StatusDesign
		}
	}
	return personaref.StatusLive
}

// cardDir：我的卡永遠在 <家>/personas/mine/；官方卡照家的佈局（第 1 版 <家>/personas/，第 2 版狀態資料夾的 personas/）。
func (m *Module) cardDir(source string) string {
	if source == personaref.SourceMine {
		return filepath.Join(m.home, "personas", "mine")
	}
	return platform.OfficialPersonasDir(m.home)
}

// cardPath：官方卡走共用的找卡入口 sdk.OfficialCardIn（同代號：官方資料夾＞我的卡＞跟著程式的公開卡，PRS-25、27）；
// 沒有官方卡（含跟著程式的那張被我的卡蓋過）回官方資料夾的位置（讀不到＝不存在）。
func (m *Module) cardPath(source, slug string) string {
	if source != personaref.SourceOfficial {
		return filepath.Join(m.cardDir(source), slug+".md")
	}
	p, _ := sdk.OfficialCardIn(m.cardDir(source), m.bundledDir(), m.cardDir(personaref.SourceMine), slug)
	return p
}

// OfficialCardFile 是官方角色 slug 的卡在哪（同 cardPath；session 測口吻的報告寫卡片位置用）：找到回 (位置, true)，
// 沒有回 (官方資料夾裡該在的位置, false)。
func (m *Module) OfficialCardFile(slug string) (string, bool) {
	return sdk.OfficialCardIn(m.cardDir(personaref.SourceOfficial), m.bundledDir(), m.cardDir(personaref.SourceMine), slug)
}

func (m *Module) readCard(source, slug string) (libCard, bool) {
	return readCardFile(source, slug, m.cardPath(source, slug))
}

func readCardFile(source, slug, path string) (libCard, bool) {
	info, err := os.Lstat(path)
	if err != nil || info.IsDir() {
		return libCard{}, false
	}
	c := libCard{slug: slug, source: source, path: path, mtime: info.ModTime()}
	data, err := os.ReadFile(path)
	if err != nil {
		c.parseErr = err
		return c, true
	}
	c.raw = string(data)
	c.rec, c.parseErr = parseCard(data)
	if c.parseErr == nil {
		if n, _ := c.rec.Fields["name"].(string); n == "" {
			c.noName = true
		}
	}
	if c.rec.Fields == nil {
		c.rec.Fields = map[string]any{}
	}
	return c, true
}

// cards 讀角色庫裡全部的卡：官方在前、我的在後，各自依代號排。_ 開頭與 README 不是卡。
// 官方卡是官方資料夾的加上跟著程式的公開卡（同代號用官方資料夾那張，PRS-25）；跟著程式的那張遇到同代號的我的卡就不列
// （被主人的卡蓋過，readCard 經 cardPath 讀不到它，PRS-27），列的是我的那張、不標 Shadowed。
func (m *Module) cards() []libCard {
	var out []libCard
	for _, source := range []string{personaref.SourceOfficial, personaref.SourceMine} {
		dirs := []string{m.cardDir(source)}
		if source == personaref.SourceOfficial {
			if b := m.bundledDir(); b != "" {
				dirs = append(dirs, b)
			}
		}
		var slugs []string
		seen := map[string]bool{}
		for _, dir := range dirs {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				slug, ok := strings.CutSuffix(e.Name(), ".md")
				if !ok || e.IsDir() || seen[slug] || !personaref.SlugPattern.MatchString(slug) && !(source == personaref.SourceMine && personaref.MineSlugPattern.MatchString(slug)) {
					continue
				}
				seen[slug] = true
				slugs = append(slugs, slug)
			}
		}
		sort.Strings(slugs)
		for _, slug := range slugs {
			if c, ok := m.readCard(source, slug); ok {
				out = append(out, c)
			}
		}
	}
	return append(out, m.packageCards()...)
}

// seats：人設代號 → 坐著的職位 id（依 id 排）。
func (m *Module) seats(ctx context.Context) (map[string][]string, []*sdk.Record, error) {
	rows, err := m.d.Roster.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	out := map[string][]string{}
	for _, r := range rows {
		if slug, _ := r.Fields["persona"].(string); slug != "" {
			out[slug] = append(out[slug], r.ID)
		}
	}
	for _, ids := range out {
		sort.Strings(ids)
	}
	return out, rows, nil
}

// effective 是指派時真正會用到的那張卡：cards() 的順序（官方在前），所以同代號官方資料夾那張優先；
// 跟著程式的卡被我的卡蓋過時不在 cards() 裡，用的就是我的（PRS-27）。
func effective(cards []libCard, slug string) (libCard, bool) {
	for _, c := range cards {
		if c.slug == slug {
			return c, true
		}
	}
	return libCard{}, false
}

// tagline 是卡上「一句話：」那一行（個性的一句話），給看板列角色時當一句介紹；沒寫、或還是樣板的說明（全形括號開頭）回空字串。
func tagline(body string) string {
	s := strings.TrimSpace(lineAfter(body, "一句話："))
	if strings.HasPrefix(s, "（") {
		return ""
	}
	return s
}

// hasImage：這張卡的人設有沒有頭像圖；沒接 Images（測試、舊的組法）照舊當有。
func (m *Module) hasImage(slug string) bool {
	return m.d.Images == nil || m.d.Images.HasImage(slug)
}

// toSDK 把卡轉成看板要的樣子；hasImage 說這個人設有沒有頭像圖（沒有就不給 avatar_url，DEV-157）。
func toSDK(c libCard, cards []libCard, seats map[string][]string, hasImage func(string) bool) sdk.PersonaCard {
	out := sdk.PersonaCard{Slug: c.slug, Name: c.name(), Aka: c.aka(), Source: c.source, Status: c.status(), UpdatedAt: c.mtime, Seats: []string{}, Tagline: tagline(c.rec.Body)}
	if c.source == personaref.SourcePackage {
		out.Package, out.Unassignable = c.pkgID, c.inactive
	}
	if c.aka() == nil {
		out.Aka = []string{}
	}
	// Shadowed 只有一個方向：我的卡被官方資料夾同代號的卡蓋住（看板標「被官方蓋住」、指令與指派略過它）。
	// 反方向（跟著程式的公開卡被我的卡蓋過）那張不在 cards() 裡、不列，免得官方卡被標成「被官方蓋住」（PRS-27）。
	if c.source == personaref.SourceMine {
		if first, ok := effective(cards, c.slug); ok && first.source == personaref.SourceOfficial {
			out.Shadowed = true
		}
	}
	if !out.Shadowed {
		out.Seats = append(out.Seats, seats[c.slug]...)
		// 有圖才給網址（DEV-157）：沒有圖時看板直接畫名字的前兩個字，不去抓一張一定 404 的圖。
		if !hasImage(c.slug) {
			return out
		}
		if len(out.Seats) > 0 {
			out.AvatarURL = "/avatars/" + out.Seats[0] + ".png"
		} else {
			// 沒人坐的角色照代號給頭像（WEB-150，DEV-115：新角色打招呼的按鈕開到這張卡，要看得到這位角色）。
			// 我的角色、套件角色也一樣：圖有沒有由上面的 hasImage 把關，送圖那條鏈（/avatars/persona/）本來就認得它們；
			// 以前只給官方卡，角色清單「我的」那區的角色明明有圖卻只畫名字（角色頁 feedback-1，2026-11-18）。
			out.AvatarURL = "/avatars/persona/" + c.slug + ".png"
		}
	}
	return out
}

// Library 列出角色庫（官方在前、我的在後）。
func (m *Module) Library(ctx context.Context) ([]sdk.PersonaCard, error) {
	seats, _, err := m.seats(ctx)
	if err != nil {
		return nil, err
	}
	cards := m.cards()
	out := make([]sdk.PersonaCard, 0, len(cards))
	for _, c := range cards {
		out = append(out, toSDK(c, cards, seats, m.hasImage))
	}
	return out, nil
}

// ShowPersona 讀一張卡（同代號照 effective：官方資料夾＞我的＞跟著程式的），回卡片資訊、內文與整份檔案文字。
func (m *Module) ShowPersona(ctx context.Context, slug string) (sdk.PersonaCard, string, string, error) {
	seats, _, err := m.seats(ctx)
	if err != nil {
		return sdk.PersonaCard{}, "", "", err
	}
	cards := m.cards()
	c, ok := effective(cards, slug)
	if !ok {
		return sdk.PersonaCard{}, "", "", platform.NotFound("角色庫裡沒有 " + slug)
	}
	return toSDK(c, cards, seats, m.hasImage), c.rec.Body, c.raw, nil
}

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// takenNames 是已經被用掉的名字 → 誰在用：庫裡其他卡的代號、人名、別名，職位 id，
// （former）各職位以前的人名。skip 是自己的代號（檢查既有的卡時不跟自己比）。比對不分大小寫。
func (m *Module) takenNames(ctx context.Context, rows []*sdk.Record, cards []libCard, skip string, former bool) map[string]string {
	used := map[string]string{}
	for _, c := range cards {
		if c.slug == skip || c.source == personaref.SourcePackage { // 套件的角色不佔名字：新套件不會讓既有的卡突然撞名
			continue
		}
		for _, n := range append([]string{c.slug, c.name()}, c.aka()...) {
			used[lower(n)] = "角色 " + c.slug
		}
	}
	for _, r := range rows {
		used[lower(r.ID)] = "職位 id " + r.ID
		if former {
			for _, n := range stringList(r.Fields["former_names"]) {
				used[lower(n)] = "職位 " + r.ID + " 以前的人名"
			}
		}
	}
	return used
}

func cleanNames(name string, aka []string) (string, []string) {
	out := []string{}
	for _, a := range aka {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return strings.TrimSpace(name), out
}

func (m *Module) checkTaken(ctx context.Context, skip, slug string, name string, aka []string, former bool) error {
	rows, err := m.d.Roster.List(ctx)
	if err != nil {
		return err
	}
	used := m.takenNames(ctx, rows, m.cards(), skip, former)
	names := append([]string{name}, aka...)
	if slug != "" {
		names = append([]string{slug}, names...)
	}
	for _, n := range names {
		if who, ok := used[lower(n)]; ok {
			return platform.Invalid("name", "名字「"+n+"」跟"+who+"撞名，換一個")
		}
	}
	return nil
}

// renderCard 寫成角色庫的格式：`---` YAML（schema、name、aka、status，其他欄位原樣保留）加內文。
func renderCard(fields map[string]any, order []string, body string) []byte {
	r := vault.Record{Fields: fields, Body: body}
	return vault.Encode(r, append([]string{"schema", "name", "aka", "status"}, order...), vault.Spec{})
}

func (m *Module) writeCard(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return platform.Unavailable("無法建立角色資料夾")
	}
	if err := platform.AtomicWrite(path, data, 0o644); err != nil {
		return platform.Unavailable("無法寫入角色卡")
	}
	return nil
}

// backupCard 把改之前的整份卡存到 runtime/backups/personas/<代號>-<時間>.md，回相對家的路徑（第 2 版暫存在家外面，回完整路徑）。
func (m *Module) backupCard(c libCard) (string, error) {
	first := platform.RuntimeDir(m.home, "backups", "personas", c.slug+"-"+m.d.Clock.Now().Format("20060102-150405.000000000")+".md")
	full := first
	for n := 2; ; n++ { // 同一個時間點連續備份（假時鐘）：不蓋掉前一份
		if _, err := os.Lstat(full); err != nil {
			break
		}
		full = strings.TrimSuffix(first, ".md") + "-" + strconv.Itoa(n) + ".md"
	}
	rel := full
	if r, err := filepath.Rel(m.home, full); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		rel = r
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return "", platform.Unavailable("無法備份角色卡")
	}
	if err := platform.AtomicWrite(full, []byte(c.raw), 0o600); err != nil {
		return "", platform.Unavailable("無法備份角色卡")
	}
	return filepath.ToSlash(rel), nil
}

// statusCard 找可以改上線／設計中狀態的卡：我的卡，或 packages/mine/ 套件裡的卡（別層的套件卡唯讀）。
func (m *Module) statusCard(slug string) (libCard, error) {
	if c, ok := m.packageCard(slug); ok {
		if c.pkgTier != "mine" {
			return libCard{}, platform.Forbidden("官方或別人分享的套件裡的角色是唯讀的（先複製成我的或放進自己的套件再改）")
		}
		return c, nil
	}
	return m.mineCard(slug)
}

func (m *Module) cardResult(ctx context.Context, source, slug string) (sdk.PersonaCard, error) {
	seats, _, err := m.seats(ctx)
	if err != nil {
		return sdk.PersonaCard{}, err
	}
	cards := m.cards()
	for _, c := range cards {
		if c.slug == slug && c.source == source {
			return toSDK(c, cards, seats, m.hasImage), nil
		}
	}
	return sdk.PersonaCard{}, platform.NotFound("角色庫裡沒有 " + slug)
}

// mineSlug 把主人給的名字變成自己的角色代號（my_<名字>，DEV-154）並確認還沒被用：官方代號不收底線，所以只會跟自己的撞。
// 主人打 hotaru 或 my_hotaru 都得到 my_hotaru；跟官方角色同名也可以（兩張都在）。
func (m *Module) mineSlug(given string) (string, error) {
	slug := personaref.MineSlug(strings.TrimSpace(given))
	if !personaref.MineSlugPattern.MatchString(slug) {
		return "", platform.Invalid("slug", "角色代號用小寫英文加數字，最多 24 個字，例：hotaru（系統會記成 my_hotaru）")
	}
	if _, err := os.Lstat(m.cardPath(personaref.SourceMine, slug)); err == nil {
		return "", platform.Conflict("已經有角色 " + slug)
	}
	return slug, nil
}

// CreatePersona 新增一張我的卡：設計中，內文是範本。
func (m *Module) CreatePersona(ctx context.Context, slug, name string, aka []string, by string) (sdk.PersonaCard, error) {
	if err := requireOwner(by); err != nil {
		return sdk.PersonaCard{}, err
	}
	name, aka = cleanNames(name, aka)
	if name == "" {
		return sdk.PersonaCard{}, platform.Invalid("name", "新角色要給名字")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	slug, err := m.mineSlug(slug)
	if err != nil {
		return sdk.PersonaCard{}, err
	}
	if err := m.checkTaken(ctx, "", slug, name, aka, true); err != nil {
		return sdk.PersonaCard{}, err
	}
	data := renderCard(map[string]any{"schema": 1, "name": name, "aka": aka, "status": personaref.StatusDesign}, nil, personaTemplate)
	if err := m.writeCard(m.cardPath(personaref.SourceMine, slug), data); err != nil {
		return sdk.PersonaCard{}, err
	}
	return m.cardResult(ctx, personaref.SourceMine, slug)
}

// CopyPersona 把一張卡複製成新的我的卡（設計中）；名字要改，不能照抄。
func (m *Module) CopyPersona(ctx context.Context, from, slug, name string, aka []string, by string) (sdk.PersonaCard, error) {
	if err := requireOwner(by); err != nil {
		return sdk.PersonaCard{}, err
	}
	name, aka = cleanNames(name, aka)
	if name == "" {
		return sdk.PersonaCard{}, platform.Invalid("name", "複製要給新的名字")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	src, ok := effective(m.cards(), from)
	if !ok {
		return sdk.PersonaCard{}, platform.NotFound("角色庫裡沒有 " + from)
	}
	if src.parseErr != nil || src.noName {
		return sdk.PersonaCard{}, platform.Invalid("from", "來源卡的格式有問題，先修好再複製")
	}
	slug, err := m.mineSlug(slug)
	if err != nil {
		return sdk.PersonaCard{}, err
	}
	if err := m.checkTaken(ctx, "", slug, name, aka, true); err != nil {
		return sdk.PersonaCard{}, err
	}
	data := renderCard(map[string]any{"schema": 1, "name": name, "aka": aka, "status": personaref.StatusDesign}, nil, src.rec.Body)
	if err := m.writeCard(m.cardPath(personaref.SourceMine, slug), data); err != nil {
		return sdk.PersonaCard{}, err
	}
	return m.cardResult(ctx, personaref.SourceMine, slug)
}

// mineCard 找「我的」卡的檔案（寫入類方法用）。只有官方卡的代號回 Forbidden（唯讀）。
func (m *Module) mineCard(slug string) (libCard, error) {
	if _, ok := m.packageCard(slug); ok {
		return libCard{}, platform.Forbidden("套件裡的角色是唯讀的（改套件資料夾裡的檔案，或先複製成我的再改）")
	}
	if !personaref.ValidSlug(slug) {
		return libCard{}, platform.Invalid("slug", "角色代號不正確")
	}
	if c, ok := m.readCard(personaref.SourceMine, slug); ok {
		return c, nil
	}
	if _, ok := m.readCard(personaref.SourceOfficial, slug); ok {
		return libCard{}, platform.Forbidden("官方角色卡是唯讀的，先複製成我的再改")
	}
	return libCard{}, platform.NotFound("角色庫裡沒有 " + slug)
}

// UpdatePersona 改我的卡的名字、別名、內文。baseUpdatedAt 是讀到時的檔案修改時間（樂觀鎖）；寫入前備份。
func (m *Module) UpdatePersona(ctx context.Context, slug string, name *string, aka *[]string, body *string, baseUpdatedAt time.Time, by string) (sdk.PersonaCard, string, error) {
	if err := requireOwner(by); err != nil {
		return sdk.PersonaCard{}, "", err
	}
	if name == nil && aka == nil && body == nil {
		return sdk.PersonaCard{}, "", platform.Invalid("name", "沒有要改的東西")
	}
	if baseUpdatedAt.IsZero() {
		return sdk.PersonaCard{}, "", platform.Invalid("base_updated_at", "請帶上讀到卡片時的修改時間")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.mineCard(slug)
	if err != nil {
		return sdk.PersonaCard{}, "", err
	}
	if !c.mtime.Equal(baseUpdatedAt) {
		return sdk.PersonaCard{}, "", platform.Conflict("卡片被別人改過，請重新載入")
	}
	if c.parseErr != nil || c.noName {
		return sdk.PersonaCard{}, "", platform.Invalid("slug", "這張卡的格式有問題（要有檔頭和 name），先用檔案修好")
	}
	fields := map[string]any{}
	for k, v := range c.rec.Fields {
		fields[k] = v
	}
	newName, newAka := c.name(), c.aka()
	if name != nil {
		newName, _ = cleanNames(*name, nil)
	}
	if aka != nil {
		_, newAka = cleanNames("", *aka)
	}
	if newName == "" {
		return sdk.PersonaCard{}, "", platform.Invalid("name", "名字不能空白")
	}
	if name != nil || aka != nil {
		if err := m.checkTaken(ctx, slug, "", newName, newAka, false); err != nil {
			return sdk.PersonaCard{}, "", err
		}
	}
	newBody := c.rec.Body
	if body != nil {
		newBody = *body
	}
	fields["schema"], fields["name"], fields["aka"] = 1, newName, newAka
	if _, ok := fields["status"]; !ok {
		fields["status"] = personaref.StatusLive
	}
	backup, err := m.backupCard(c)
	if err != nil {
		return sdk.PersonaCard{}, "", err
	}
	if err := m.writeCard(c.path, renderCard(fields, nil, newBody)); err != nil {
		return sdk.PersonaCard{}, "", err
	}
	out, err := m.cardResult(ctx, personaref.SourceMine, slug)
	return out, backup, err
}

// SetPersonaStatus 把我的卡上線（先檢查）或移回設計中（有秘書坐著不行）。已經是那個狀態就原樣回傳。
func (m *Module) SetPersonaStatus(ctx context.Context, slug, status, by string) (sdk.PersonaCard, error) {
	if err := requireOwner(by); err != nil {
		return sdk.PersonaCard{}, err
	}
	if status != personaref.StatusLive && status != personaref.StatusDesign {
		return sdk.PersonaCard{}, platform.Invalid("status", "狀態只能是 live 或 design")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.statusCard(slug)
	if err != nil {
		return sdk.PersonaCard{}, err
	}
	if c.parseErr == nil && c.status() == status {
		return m.cardResult(ctx, c.source, slug)
	}
	if status == personaref.StatusDesign && (c.parseErr != nil || c.noName) {
		return sdk.PersonaCard{}, platform.Invalid("slug", "這張卡的格式有問題（要有檔頭和 name），先用檔案修好")
	}
	switch status {
	case personaref.StatusLive:
		problems, err := m.checkCard(ctx, c, m.cards())
		if err != nil {
			return sdk.PersonaCard{}, err
		}
		var blocking []string
		for _, p := range problems {
			if p.blocking {
				blocking = append(blocking, p.text)
			}
		}
		if len(blocking) > 0 {
			return sdk.PersonaCard{}, platform.Invalid("slug", "上線前要先修好：\n"+strings.Join(blocking, "\n"))
		}
	case personaref.StatusDesign:
		seats, _, err := m.seats(ctx)
		if err != nil {
			return sdk.PersonaCard{}, err
		}
		if ids := seats[slug]; len(ids) > 0 {
			return sdk.PersonaCard{}, platform.Invalid("status", c.name()+" 現在坐在 "+strings.Join(ids, "、")+"，先換掉才能移到設計中")
		}
	}
	fields := map[string]any{}
	for k, v := range c.rec.Fields {
		fields[k] = v
	}
	fields["schema"], fields["name"], fields["status"] = 1, c.name(), status
	fields["aka"] = c.aka()
	if _, err := m.backupCard(c); err != nil {
		return sdk.PersonaCard{}, err
	}
	if err := m.writeCard(c.path, renderCard(fields, nil, c.rec.Body)); err != nil {
		return sdk.PersonaCard{}, err
	}
	return m.cardResult(ctx, c.source, slug)
}

type cardProblem struct {
	text     string
	blocking bool // 上線前一定要修的：撞名、格式錯、少了 name
}

// checkCard 檢查一張卡（規則照舊 check_card）：角色卡只放名字和個性；職責、職位 id 不能寫進去（角色要能換到任何職位）。
func (m *Module) checkCard(ctx context.Context, c libCard, cards []libCard) ([]cardProblem, error) {
	rows, err := m.d.Roster.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []cardProblem
	add := func(blocking bool, text string) { out = append(out, cardProblem{c.slug + "：" + text, blocking}) }
	if c.parseErr != nil {
		add(true, "卡片格式錯（要用 --- YAML --- 或 +++ TOML +++ 開頭）")
		return out, nil
	}
	if c.noName {
		add(true, "少了 name")
		return out, nil
	}
	used := m.takenNames(ctx, rows, cards, c.slug, false)
	for _, n := range append([]string{c.name()}, c.aka()...) {
		if who, ok := used[lower(n)]; ok {
			add(true, "名字「"+n+"」跟"+who+"撞名")
		}
	}
	if strings.Contains(c.rec.Body, "「」") || strings.Contains(c.rec.Body, "（他是什麼樣的人") {
		add(false, "卡片還沒填完（還有範本的空格）")
	}
	for _, r := range rows {
		if strings.Contains(c.rec.Body, "`"+r.ID+"`") {
			add(false, "卡片裡寫了職位 id `"+r.ID+"`（職責寫在職位，不寫在角色）")
		}
	}
	if c.inactive != "" {
		add(false, "所屬套件 "+c.pkgID+" 目前不能用（"+c.inactive+"），這張卡不能新指派；已經坐著的秘書照舊讀得到這張卡")
	}
	if c.source == personaref.SourceMine {
		if first, ok := effective(cards, c.slug); ok && first.source == personaref.SourceOfficial {
			add(false, "我的卡被同代號的官方卡蓋住了，指派時用的是官方那張（改個代號才會生效）")
		}
	}
	return out, nil
}

// CheckPersonas 檢查一張卡（slug 空＝全部）；回一條一句白話的問題清單，沒問題回空串列。
func (m *Module) CheckPersonas(ctx context.Context, slug string) ([]string, error) {
	cards := m.cards()
	problems := []string{}
	found := false
	for _, c := range cards {
		if slug != "" && c.slug != slug {
			continue
		}
		found = true
		ps, err := m.checkCard(ctx, c, cards)
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			problems = append(problems, p.text)
		}
	}
	if slug != "" && !found {
		return nil, platform.NotFound("角色庫裡沒有 " + slug)
	}
	return problems, nil
}
