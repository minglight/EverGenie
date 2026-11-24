package run

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// fakeVault 是記憶體裡的 vault：照種類存、每次寫入換版本號、記得寫的人（by）；封存的 List 不給、Get 給，跟真的一樣。
type fakeVault struct {
	mu     sync.Mutex
	recs   map[string]map[string]sdk.Record
	rev    int
	puts   map[string]int // kind → 寫了幾次
	events chan struct{}
}

func newFakeVault() *fakeVault {
	return &fakeVault{recs: map[string]map[string]sdk.Record{}, puts: map[string]int{}, events: make(chan struct{}, 16)}
}

func (f *fakeVault) seedBy(kind, id string, fields map[string]any, by string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recs[kind] == nil {
		f.recs[kind] = map[string]sdk.Record{}
	}
	f.rev++
	fields = cloneMap(fields)
	fields["by"] = by
	f.recs[kind][id] = sdk.Record{Kind: kind, ID: id, Fields: fields, Revision: fmt.Sprint(f.rev), UpdatedAt: time.Date(2026, 10, 26, 9, 0, 0, 0, time.Local)}
}

func cloneMap(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (f *fakeVault) seed(kind, id string, fields map[string]any) {
	f.seedBy(kind, id, fields, "owner(cli)")
}

func (f *fakeVault) get(kind, id string) (sdk.Record, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recs[kind][id]
	return r, ok
}

// records 是某個種類沒封存的紀錄（照編號排）。
func (f *fakeVault) records(kind string) []sdk.Record {
	out, _ := f.List(context.Background(), kind, false)
	return out
}

func (f *fakeVault) count(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.recs[kind] {
		if !r.Archived {
			n++
		}
	}
	return n
}

func (f *fakeVault) List(_ context.Context, kind string, includeArchived bool) ([]sdk.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sdk.Record
	for _, r := range f.recs[kind] {
		if r.Archived && !includeArchived {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeVault) Get(_ context.Context, kind, id string) (*sdk.Record, error) {
	// 跟核心一樣：編號帶「..」或反斜線不是「沒有這筆」，是讀不了（回錯）。
	if strings.Contains(id, "..") || strings.Contains(id, `\`) {
		return nil, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "編號不合法"}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recs[kind][id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (f *fakeVault) Put(_ context.Context, rec sdk.Record, expected string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, exists := f.recs[rec.Kind][rec.ID]
	if exists && cur.Archived && expected == "" {
		exists = false // 跟真的一樣：自己給編號的種類，封存的那筆可以用同一個編號重新寫一筆沒封存的
	}
	if (!exists && expected != "") || (exists && cur.Revision != expected) {
		return &sdk.Error{Code: sdk.ErrorCodeConflict, Message: "版本不符"}
	}
	if f.recs[rec.Kind] == nil {
		f.recs[rec.Kind] = map[string]sdk.Record{}
	}
	f.rev++
	rec.Revision = fmt.Sprint(f.rev)
	rec.Fields = cloneMap(rec.Fields)
	rec.Fields["by"] = actor
	rec.Archived = exists && cur.Archived
	rec.UpdatedAt = time.Now()
	f.recs[rec.Kind][rec.ID] = rec
	f.puts[rec.Kind]++
	return nil
}

func (f *fakeVault) Patch(_ context.Context, kind, id string, set map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.recs[kind][id]
	if !ok {
		return &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "找不到"}
	}
	fields := cloneMap(cur.Fields)
	for k, v := range set {
		fields[k] = v
	}
	fields["by"] = actor
	f.rev++
	cur.Fields, cur.Revision, cur.UpdatedAt = fields, fmt.Sprint(f.rev), time.Now()
	f.recs[kind][id] = cur
	return nil
}

func (f *fakeVault) Archive(_ context.Context, kind, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.recs[kind][id]
	if !ok {
		return &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "找不到"}
	}
	cur.Archived = true
	f.recs[kind][id] = cur
	return nil
}

func (f *fakeVault) Watch(ctx context.Context, _ []string, onChange func()) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-f.events:
			onChange()
		}
	}
}

// ---- 場景 ----

func pngFile(t *testing.T, path string, seed uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(int(seed)*3+x*2+y) % 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type scene struct {
	t    *testing.T
	home string
	v    *fakeVault
	svc  *Service
	now  time.Time
	img  map[string][]byte // 代號 → 真圖位元組
}

// newScene：builder 有三套：base（免費）、night（1 點）、sea（2 點），圖都放好了；名冊 code 坐 builder。
func newScene(t *testing.T) *scene {
	t.Helper()
	home := t.TempDir()
	sc := &scene{t: t, home: home, v: newFakeVault(), now: time.Date(2026, 10, 26, 10, 30, 0, 0, time.Local), img: map[string][]byte{}}
	for i, code := range []string{"builder", "builder@night", "builder@sea"} {
		sc.img[code] = pngFile(t, filepath.Join(home, "personas", "avatars", code+".png"), uint8(i+1))
	}
	if err := os.WriteFile(filepath.Join(home, "personas", "builder.md"), []byte("---\nschema: 1\nname: Builder\n---\n人造角色\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sc.outfit("base", 0, "active", 1)
	sc.outfit("night", 1, "active", 2)
	sc.outfit("sea", 2, "active", 3)
	sc.v.seed("agent", "code", map[string]any{"persona": "builder", "enabled": true})
	sc.v.seed("agent", "art", map[string]any{"persona": "builder", "enabled": true})
	sc.svc = NewService(sc.v, home, io.Discard)
	sc.svc.Now = func() time.Time { return sc.now }
	sc.svc.Debounce, sc.svc.Retry = 10*time.Millisecond, 10*time.Millisecond
	return sc
}

func (sc *scene) outfit(id string, cost int, state string, order int) {
	sc.v.seed(KindOutfit, "builder-"+id, map[string]any{"persona": "builder", "outfit": id, "name": strings.ToUpper(id), "cost": cost, "state": state, "source": "official", "order": order})
}

func (sc *scene) wallet(earned int) {
	sc.v.seed(kindWallet, "main", map[string]any{"earned": earned, "spent": 0, "points": earned})
}

func (sc *scene) request(id, by, persona, outfit string) {
	sc.v.seedBy(KindRequest, id, map[string]any{"type": "unlock", "persona": persona, "outfit": outfit, "state": "pending"}, by)
}

func (sc *scene) round() {
	sc.t.Helper()
	if err := sc.svc.Round(context.Background(), false); err != nil {
		sc.t.Fatal(err)
	}
}

func (sc *scene) fields(kind, id string) map[string]any {
	sc.t.Helper()
	r, ok := sc.v.get(kind, id)
	if !ok {
		sc.t.Fatalf("沒有 %s/%s", kind, id)
	}
	return r.Fields
}

func (sc *scene) person() map[string]any { return sc.fields(KindPerson, "builder") }

func (sc *scene) outfitView(id string) map[string]any {
	sc.t.Helper()
	for _, x := range sc.person()["outfits"].([]any) {
		if m := x.(map[string]any); m["outfit"] == id {
			return m
		}
	}
	return nil
}

func (sc *scene) public(rel string) []byte {
	b, _ := os.ReadFile(filepath.Join(sc.home, "runtime", "packages", "closet", "public", filepath.FromSlash(rel)))
	return b
}

// Guards: REQ-125
func TestRunUnlockOnceAndReplaySafe(t *testing.T) {
	sc := newScene(t)
	sc.wallet(1)
	sc.round()
	if o := sc.outfitView("night"); o["owned"] != false {
		t.Fatalf("兌換前 night：%v", o)
	}
	if sc.public("o/builder/night/1.png") != nil {
		t.Fatal("兌換前不該有真圖")
	}
	if sc.public("s/builder/night/1.png") == nil {
		t.Fatal("兌換前要有剪影")
	}

	sc.request("Q-1", "owner(web)", "builder", "night")
	sc.round()
	u := sc.fields(KindUnlock, "builder-night")
	if u["cost"] != 1 || u["request"] != "Q-1" || u["by"] != "closet" || u["persona"] != "builder" {
		t.Fatalf("unlock：%v", u)
	}
	if q := sc.fields(KindRequest, "Q-1"); q["state"] != "done" || q["unlock"] != "builder-night" {
		t.Fatalf("請求：%v", q)
	}
	if st := sc.fields(KindState, "main"); st["points"] != 0 || st["spent"] != 1 || st["earned"] != 1 || st["wallet"] != true {
		t.Fatalf("state：%v", st)
	}
	if o := sc.outfitView("night"); o["owned"] != true {
		t.Fatalf("兌換後 night：%v", o)
	}
	if !bytes.Equal(sc.public("o/builder/night/1.png"), sc.img["builder@night"]) || sc.public("s/builder/night/1.png") != nil {
		t.Fatal("兌換後真圖取代剪影")
	}

	// 行程在「寫了 unlock、還沒標 done」時被殺：請求還是 pending，重開後處理結果相同、不重複扣點。
	sc.v.seedBy(KindRequest, "Q-1", map[string]any{"type": "unlock", "persona": "builder", "outfit": "night", "state": "pending"}, "owner(web)")
	sc.round()
	if q := sc.fields(KindRequest, "Q-1"); q["state"] != "done" || q["unlock"] != "builder-night" {
		t.Fatalf("重播後的請求：%v", q)
	}
	if n := sc.v.count(KindUnlock); n != 1 {
		t.Fatalf("unlock 筆數 %d", n)
	}
	if st := sc.fields(KindState, "main"); st["spent"] != 1 || st["points"] != 0 {
		t.Fatalf("重播後 state：%v", st)
	}

	// 同一套再請求一次：已經是她的了；點數用完後別套被拒。
	sc.request("Q-2", "owner(web)", "builder", "night")
	sc.request("Q-3", "owner(cli)", "builder", "sea")
	sc.round()
	if q := sc.fields(KindRequest, "Q-2"); q["state"] != "rejected" || !strings.Contains(q["reason"].(string), "已經擁有這套了") {
		t.Fatalf("Q-2：%v", q)
	}
	if q := sc.fields(KindRequest, "Q-3"); q["state"] != "rejected" || !strings.Contains(q["reason"].(string), "點數不夠：要 2 點，現在 0 點") {
		t.Fatalf("Q-3：%v", q)
	}
	if sc.public("o/builder/sea/1.png") != nil {
		t.Fatal("沒兌換的 sea 不該有真圖")
	}
}

// 同一輪兩個請求都想花同一筆點數：只有編號小的過（不會同時扣點）。
// Guards: REQ-125
func TestRunSpendsEachPointOnce(t *testing.T) {
	sc := newScene(t)
	sc.wallet(2)
	sc.request("Q-2", "owner(web)", "builder", "night")
	sc.request("Q-10", "owner(web)", "builder", "sea")
	sc.request("Q-3", "owner(web)", "builder", "sea")
	sc.round()
	// Q-2 花 1 點 → 剩 1；Q-3 要 2 點被拒；Q-10 也被拒（編號照數字排，不是字串）。
	if q := sc.fields(KindRequest, "Q-2"); q["state"] != "done" {
		t.Fatalf("Q-2：%v", q)
	}
	for _, id := range []string{"Q-3", "Q-10"} {
		if q := sc.fields(KindRequest, id); q["state"] != "rejected" || !strings.Contains(q["reason"].(string), "現在 1 點") {
			t.Fatalf("%s：%v", id, q)
		}
	}
	if st := sc.fields(KindState, "main"); st["points"] != 1 {
		t.Fatalf("state：%v", st)
	}
}

// Guards: REQ-125
func TestRunRejectsWithoutWalletAndNonOwner(t *testing.T) {
	sc := newScene(t)
	// 沒有錢包：拒絕並說明；秘書送的請求：只有主人可以。
	sc.request("Q-1", "owner(web)", "builder", "night")
	sc.request("Q-2", "code", "builder", "night")
	sc.round()
	if q := sc.fields(KindRequest, "Q-1"); q["state"] != "rejected" || !strings.Contains(q["reason"].(string), "還沒有點數") {
		t.Fatalf("Q-1：%v", q)
	}
	if q := sc.fields(KindRequest, "Q-2"); q["state"] != "rejected" || !strings.Contains(q["reason"].(string), "主人") {
		t.Fatalf("Q-2：%v", q)
	}
	if st := sc.fields(KindState, "main"); st["wallet"] != false || st["points"] != 0 {
		t.Fatalf("state：%v", st)
	}
	if sc.v.count(KindUnlock) != 0 {
		t.Fatal("不該有 unlock")
	}
	// 草稿、下架、不存在的請求都被拒。
	sc.outfit("wip", 1, "draft", 4)
	sc.outfit("old", 1, "retired", 5)
	sc.wallet(5)
	sc.request("Q-3", "owner(web)", "builder", "wip")
	sc.request("Q-4", "owner(web)", "builder", "old")
	sc.request("Q-5", "owner(web)", "builder", "nope")
	sc.round()
	for id, want := range map[string]string{"Q-3": "還不能兌換", "Q-4": "還不能兌換", "Q-5": "沒有這套衣服"} {
		if q := sc.fields(KindRequest, id); q["state"] != "rejected" || !strings.Contains(q["reason"].(string), want) {
			t.Fatalf("%s：%v", id, q)
		}
	}
	if sc.v.count(KindUnlock) != 0 {
		t.Fatal("不該有 unlock")
	}
}

// 頁面或秘書直接寫的 closet.unlock 不被承認：封存、不算擁有、不扣點、沒有真圖。
// Guards: REQ-125
func TestRunArchivesForgedUnlock(t *testing.T) {
	sc := newScene(t)
	sc.wallet(1)
	sc.v.seedBy(KindUnlock, "builder-sea", map[string]any{"persona": "builder", "outfit": "sea", "cost": 0, "name": "SEA", "at": "2026-10-26T10:00:00+08:00"}, "owner(web)")
	sc.v.seedBy(KindUnlock, "builder-night", map[string]any{"persona": "builder", "outfit": "night", "cost": 1, "name": "NIGHT", "at": "2026-10-26T10:00:00+08:00"}, "migrate")
	sc.round()
	if r, _ := sc.v.get(KindUnlock, "builder-sea"); !r.Archived {
		t.Fatal("偽造的 unlock 要封存")
	}
	if o := sc.outfitView("sea"); o["owned"] != false {
		t.Fatalf("sea：%v", o)
	}
	if sc.public("o/builder/sea/1.png") != nil {
		t.Fatal("偽造的 unlock 不該讓真圖公開")
	}
	if o := sc.outfitView("night"); o["owned"] != true {
		t.Fatalf("搬家腳本寫的要認：%v", o)
	}
	if st := sc.fields(KindState, "main"); st["spent"] != 1 {
		t.Fatalf("只算認得的：%v", st)
	}
	// 偽造的 unlock 封存後，主人真的送兌換請求：vault 的自己給編號種類，封存的編號可以重寫一筆沒封存的（DEV-151），照常兌換。
	// （以前的假 vault 讓封存的編號擋住新的一筆、這裡斷言被拒；真的 vault 不是這樣，2026-11-15 跟著改。）
	sc.request("Q-1", "owner(web)", "builder", "sea")
	sc.wallet(5)
	sc.round()
	if q := sc.fields(KindRequest, "Q-1"); q["state"] != "done" {
		t.Fatalf("Q-1：%v", q)
	}
	if r, _ := sc.v.get(KindUnlock, "builder-sea"); r.Archived || r.Fields["request"] != "Q-1" || r.Fields["by"] != actor {
		t.Fatalf("新的兌換紀錄：%v archived=%v", r.Fields, r.Archived)
	}
}

// Guards: REQ-125
func TestRunWritesPersonAndStateOnlyOnChange(t *testing.T) {
	sc := newScene(t)
	sc.wallet(1)
	sc.round()
	first := map[string]int{KindPerson: sc.v.puts[KindPerson], KindState: sc.v.puts[KindState]}
	if first[KindPerson] != 1 || first[KindState] != 1 {
		t.Fatalf("第一輪各寫一次：%v", first)
	}
	sc.now = sc.now.Add(10 * time.Minute) // 同一個小時內時間變了（computed_at 不算內容）
	sc.round()
	sc.round()
	if sc.v.puts[KindPerson] != 1 || sc.v.puts[KindState] != 1 {
		t.Fatalf("沒變不該再寫：%v", sc.v.puts)
	}
	p := sc.person()
	if p["name"] != "Builder" || p["agent"] != "art" || p["worn"] != "base" || p["worn_photo"] != 1 {
		t.Fatalf("person：%v", p)
	}
	// 名冊 art 排在 code 前面（編號最小）。
	sc.wallet(3)
	sc.round()
	if sc.v.puts[KindState] != 2 || sc.v.puts[KindPerson] != 1 {
		t.Fatalf("錢包變了只重寫 state：%v", sc.v.puts)
	}
	// 目錄變了才重寫 person；衣服全拿掉後 person 清空（不封存）。
	sc.v.seed(KindRotation, "builder", map[string]any{"outfits": []any{"base"}})
	sc.round()
	if sc.v.puts[KindPerson] != 1 {
		t.Fatalf("輪播設定沒改變有效清單：%v", sc.v.puts)
	}
	for _, id := range []string{"base", "night", "sea"} {
		if err := sc.v.Archive(context.Background(), KindOutfit, "builder-"+id); err != nil {
			t.Fatal(err)
		}
	}
	sc.round()
	if r, _ := sc.v.get(KindPerson, "builder"); r.Archived || len(r.Fields["outfits"].([]any)) != 0 || r.Fields["worn"] != nil {
		t.Fatalf("衣服都沒了：%v archived=%v", r.Fields, r.Archived)
	}
	if sc.public("o/builder/base/1.png") != nil {
		t.Fatal("衣服都沒了公開資料夾也要清空")
	}
	// 之後又有衣服：寫得進去。
	sc.outfit("again", 0, "active", 1)
	pngFile(t, filepath.Join(sc.home, "personas", "avatars", "builder@again.png"), 9)
	sc.round()
	if sc.outfitView("again") == nil {
		t.Fatal("衣服回來了 person 要更新")
	}
}

// Guards: REQ-113, REQ-125
func TestRunWornRotatesAndDropsStaleVideo(t *testing.T) {
	sc := newScene(t)
	sc.wallet(1)
	// 免費的 night，加一支 night 的動態；night、base 輪流（兩套）。
	sc.outfit("night", 0, "active", 2)
	if err := os.WriteFile(filepath.Join(sc.home, "personas", "avatars", "builder@night.mp4"), []byte("night-mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	sc.v.seed(KindRotation, "builder", map[string]any{"outfits": []any{"base", "night", "sea"}})
	// hour % 2：偶數小時穿 base，奇數小時穿 night。
	base := time.Date(2026, 10, 26, 0, 30, 0, 0, time.UTC)
	hour0 := base.Unix() / 3600
	for ; hour0%2 != 0; hour0++ {
	}
	sc.now = time.Unix(hour0*3600+60, 0)
	sc.round()
	if p := sc.person(); p["worn"] != "base" {
		t.Fatalf("偶數小時穿 base：%v", p["worn"])
	}
	if !bytes.Equal(sc.public("worn/builder.png"), sc.img["builder"]) || sc.public("worn/builder.mp4") != nil {
		t.Fatal("worn 要是 base 的圖，而且沒有 mp4")
	}
	sc.now = sc.now.Add(time.Hour)
	sc.round()
	if p := sc.person(); p["worn"] != "night" || p["rotation"].([]any)[1] != "night" {
		t.Fatalf("奇數小時穿 night：%v", p)
	}
	if !bytes.Equal(sc.public("worn/builder.png"), sc.img["builder@night"]) || string(sc.public("worn/builder.mp4")) != "night-mp4" {
		t.Fatal("worn 要換成 night 的圖與 mp4")
	}
	// 換回 base：舊的 mp4 要刪掉（圖是這套、影片不能是別套）。
	sc.now = sc.now.Add(time.Hour)
	sc.round()
	if sc.public("worn/builder.mp4") != nil {
		t.Fatal("穿 base 時不該留著 night 的 mp4")
	}
	// 沒擁有的 sea 即使勾了也不輪（有效清單只有擁有的）。
	for _, id := range sc.person()["rotation"].([]any) {
		if id == "sea" {
			t.Fatal("沒擁有的不該在輪播裡")
		}
	}
}

// Guards: REQ-125
func TestRunMineUsesItsOwnAvatarDir(t *testing.T) {
	sc := newScene(t)
	pngFile(t, filepath.Join(sc.home, "personas", "mine", "avatars", "pet.png"), 7)
	pngFile(t, filepath.Join(sc.home, "personas", "avatars", "pet.png"), 8) // 官方資料夾同名的不能被當成 mine 的
	sc.v.seed(KindOutfit, "pet-base", map[string]any{"persona": "pet", "outfit": "base", "name": "原樣", "cost": 0, "state": "active", "source": "mine"})
	if err := os.WriteFile(filepath.Join(sc.home, "personas", "mine", "pet.md"), []byte("+++\nname = \"小寵物\"\n+++\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sc.round()
	r, _ := sc.v.get(KindPerson, "pet")
	if r.Fields["name"] != "小寵物" {
		t.Fatalf("TOML 檔頭的名字：%v", r.Fields)
	}
	if got := sc.public("o/pet/base/1.png"); len(got) == 0 || bytes.Equal(got, sc.img["builder"]) {
		t.Fatal("mine 的圖要從 personas/mine/avatars 來")
	}
	if _, err := os.Stat(filepath.Join(sc.home, "personas", "avatars", "pet.png")); err != nil {
		t.Fatal(err)
	}
	// 壞資料（編號對不上）只略過那一筆。
	sc.v.seed(KindOutfit, "pet-oops", map[string]any{"persona": "pet", "outfit": "base", "name": "壞", "cost": 0, "state": "active", "source": "mine"})
	sc.round()
	if len(sc.fields(KindPerson, "pet")["outfits"].([]any)) != 1 {
		t.Fatal("壞資料不該算進去")
	}
}

// Guards: REQ-125
func TestHeaderName(t *testing.T) {
	for in, want := range map[string]string{
		"---\nname: Athena\n---\nbody":                      "Athena",
		"---\nschema: 1\nname: \"阿媞\"\n---\n":               "阿媞",
		"+++\nname = \"Athena\"\naka = [\"x\"]\n+++\n":      "Athena",
		"+++\nname = 'Zed'\n+++\n":                          "Zed",
		"---\nnickname: no\n---\n":                          "",
		"---\nx: 1\n---\nname: Body\n":                      "",
		"沒有檔頭\nname: x\n":                                   "",
		"":                                                  "",
		"---\nschema: 1\nname: \"A \\\"B\\\" \\\\\"\n---\n": `A "B" \`,
	} {
		if got := HeaderName(in); got != want {
			t.Errorf("HeaderName(%q) = %q，要 %q", in, got, want)
		}
	}
}

// 整個循環：開機做一輪、vault 有變去抖再做一輪。
// Guards: REQ-125
func TestRunRecomputesOnChange(t *testing.T) {
	sc := newScene(t)
	sc.wallet(1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sc.svc.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	wait := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if ok() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("等不到：%s", what)
	}
	wait("開機那輪寫了 person", func() bool { _, ok := sc.v.get(KindPerson, "builder"); return ok })
	sc.request("Q-1", "owner(web)", "builder", "night")
	sc.v.events <- struct{}{}
	wait("請求處理完", func() bool {
		r, ok := sc.v.get(KindRequest, "Q-1")
		return ok && r.Fields["state"] == "done"
	})
}

// TestRunFullOnlyOwnedAndSource：person.outfits[] 有 source；全身照只對擁有的與草稿的衣服給網址，沒擁有的是空字串且公開樹裡沒有。
// Guards: REQ-125, REQ-126
func TestRunFullOnlyOwnedAndSource(t *testing.T) {
	sc := newScene(t)
	sc.outfit("draft", 0, "draft", 4)
	pngFile(t, filepath.Join(sc.home, "personas", "avatars", "builder@draft.png"), 9)
	full := map[string][]byte{}
	for i, code := range []string{"builder", "builder@night", "builder@draft"} {
		full[code] = pngFile(t, filepath.Join(sc.home, "personas", "avatars", "full", code+".png"), uint8(20+i))
	}
	// mine 的衣服從 personas/mine/avatars/full/ 讀。
	pngFile(t, filepath.Join(sc.home, "personas", "mine", "avatars", "pet.png"), 7)
	mineFull := pngFile(t, filepath.Join(sc.home, "personas", "mine", "avatars", "full", "pet.png"), 31)
	sc.v.seed(KindOutfit, "pet-base", map[string]any{"persona": "pet", "outfit": "base", "name": "原樣", "cost": 0, "state": "active", "source": "mine"})
	sc.round()
	if o := sc.outfitView("base"); o["full"] != "/pkg-files/closet/f/builder/base/1.png" || o["source"] != "official" {
		t.Fatalf("擁有的：%v", o)
	}
	if o := sc.outfitView("night"); o["full"] != "" || o["owned"] != false {
		t.Fatalf("沒擁有的不給全身照：%v", o)
	}
	if o := sc.outfitView("draft"); o["full"] != "/pkg-files/closet/f/builder/draft/1.png" {
		t.Fatalf("草稿：%v", o)
	}
	if o := sc.outfitView("sea"); o["full"] != "" {
		t.Fatalf("沒有全身照檔的：%v", o)
	}
	if !bytes.Equal(sc.public("f/builder/base/1.png"), full["builder"]) || !bytes.Equal(sc.public("f/builder/draft/1.png"), full["builder@draft"]) {
		t.Fatal("公開的全身照要等於來源")
	}
	if sc.public("f/builder/night/1.png") != nil {
		t.Fatal("沒擁有的全身照不能在公開樹")
	}
	pet := sc.fields(KindPerson, "pet")["outfits"].([]any)[0].(map[string]any)
	if pet["source"] != "mine" || pet["full"] != "/pkg-files/closet/f/pet/base/1.png" || !bytes.Equal(sc.public("f/pet/base/1.png"), mineFull) {
		t.Fatalf("mine：%v", pet)
	}
	// 兌換後才公開。
	sc.wallet(1)
	sc.request("Q-1", "owner(web)", "builder", "night")
	sc.round()
	if o := sc.outfitView("night"); o["full"] != "/pkg-files/closet/f/builder/night/1.png" || !bytes.Equal(sc.public("f/builder/night/1.png"), full["builder@night"]) {
		t.Fatalf("兌換後：%v", o)
	}
}

// Guards: DEV-125
func TestRunMultipleVideosPublishedAndPruned(t *testing.T) {
	sc := newScene(t)
	av := filepath.Join(sc.home, "personas", "avatars")
	for f, c := range map[string]string{"builder.mp4": "v1", "builder+2.mp4": "v2", "builder+3.mp4": "v3"} {
		if err := os.WriteFile(filepath.Join(av, f), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sc.round()
	// base 是第一套且免費：擁有，真圖旁有 3 支動畫，現穿的 worn 也有 3 支。
	for rel, want := range map[string]string{
		"o/builder/base/1.mp4": "v1", "o/builder/base/1+2.mp4": "v2", "o/builder/base/1+3.mp4": "v3",
		"worn/builder.mp4": "v1", "worn/builder+2.mp4": "v2", "worn/builder+3.mp4": "v3",
	} {
		if string(sc.public(rel)) != want {
			t.Fatalf("%s = %q，要 %q", rel, sc.public(rel), want)
		}
	}
	photos := sc.outfitView("base")["photos"].([]any)
	item := photos[0].(map[string]any)
	videos, _ := item["videos"].([]any)
	if len(videos) != 3 || item["video"] != videos[0] || !strings.HasSuffix(videos[1].(string), "/o/builder/base/1+2.mp4") {
		t.Fatalf("photos[0] 的動畫：%+v", item)
	}
	// 拿掉第 2、3 支：公開的 +k 一起消失，清單只剩 1 支。
	for _, f := range []string{"builder+2.mp4", "builder+3.mp4"} {
		if err := os.Remove(filepath.Join(av, f)); err != nil {
			t.Fatal(err)
		}
	}
	sc.now = sc.now.Add(time.Minute)
	sc.round()
	if sc.public("worn/builder+2.mp4") != nil || sc.public("o/builder/base/1+3.mp4") != nil {
		t.Fatal("來源沒了，公開的 +k 要清掉")
	}
	item = sc.outfitView("base")["photos"].([]any)[0].(map[string]any)
	if v, _ := item["videos"].([]any); len(v) != 1 {
		t.Fatalf("清單只剩 1 支：%+v", item)
	}
}
