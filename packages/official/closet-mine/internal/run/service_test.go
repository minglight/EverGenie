package run

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/packages/official/closet-mine/internal/rules"
	"github.com/minglight/evergenie/sdk"
)

// fakeVault 是記憶體裡的 vault：照種類存、每次寫入換版本號；封存的 List 不給、Get 給，跟真的一樣。
type fakeVault struct {
	mu     sync.Mutex
	recs   map[string]map[string]sdk.Record
	rev    int
	puts   map[string]int
	events chan struct{}
	// beforePut、beforePatch：測試看寫的先後（在鎖裡呼叫，可以直接讀 recs，不能再呼叫 vault）。
	beforePut   func(rec sdk.Record)
	beforePatch func(kind, id string, set map[string]any)
	// getErr：不是 nil 時 Get 先問它，回錯就當這次讀不到（測試「讀不到能力就不做」）。
	getErr func(kind, id string) error
}

func newFakeVault() *fakeVault {
	return &fakeVault{recs: map[string]map[string]sdk.Record{}, puts: map[string]int{}, events: make(chan struct{}, 16)}
}

func cloneMap(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (f *fakeVault) seed(kind, id string, fields map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recs[kind] == nil {
		f.recs[kind] = map[string]sdk.Record{}
	}
	f.rev++
	f.recs[kind][id] = sdk.Record{Kind: kind, ID: id, Fields: cloneMap(fields), Revision: fmt.Sprint(f.rev), UpdatedAt: time.Now()}
}

// drop 把一筆整個拿掉（測試「還沒有這筆」）。
func (f *fakeVault) drop(kind, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.recs[kind], id)
}

func (f *fakeVault) get(kind, id string) (sdk.Record, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recs[kind][id]
	return r, ok
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
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		if err := f.getErr(kind, id); err != nil {
			return nil, err
		}
	}
	r, ok := f.recs[kind][id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (f *fakeVault) Put(_ context.Context, rec sdk.Record, expected string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beforePut != nil {
		f.beforePut(rec)
	}
	cur, exists := f.recs[rec.Kind][rec.ID]
	if exists && cur.Archived && expected == "" {
		exists = false // 跟真的一樣：自己給編號的種類，封存的那筆可以用同一個編號重新寫一筆（拿回）
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
	rec.UpdatedAt = time.Now()
	f.recs[rec.Kind][rec.ID] = rec
	f.puts[rec.Kind]++
	return nil
}

func (f *fakeVault) Patch(_ context.Context, kind, id string, set map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beforePatch != nil {
		f.beforePatch(kind, id, set)
	}
	cur, ok := f.recs[kind][id]
	if !ok || cur.Archived { // 跟真的一樣：封存的不能改
		return &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "找不到"}
	}
	fields := cloneMap(cur.Fields)
	for k, v := range set {
		fields[k] = v
	}
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

func png(payload string) []byte  { return append([]byte("\x89PNG\r\n\x1a\n"), payload...) }
func jpeg(payload string) []byte { return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, payload...) }
func mp4(payload string) []byte {
	return append([]byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00"), payload...)
}

type scene struct {
	t    *testing.T
	home string
	v    *fakeVault
	svc  *Service
	n    int // 下一個請求編號
}

// newScene：官方角色 athena（官方卡、官方圖、官方 base 衣服）；closet.person 也有 athena。
func newScene(t *testing.T) *scene {
	t.Helper()
	home := t.TempDir()
	sc := &scene{t: t, home: home, v: newFakeVault(), n: 1}
	sc.write("personas/athena.md", []byte("---\nname: Athena\n---\n"))
	sc.write("personas/avatars/athena.png", png("official-athena"))
	sc.v.seed(kindOutfit, "athena-base", map[string]any{"persona": "athena", "outfit": "base", "name": "原樣", "cost": 0, "state": "active", "source": "official"})
	sc.v.seed(kindPerson, "athena", map[string]any{"persona": "athena", "name": "Athena", "outfits": []any{}})
	sc.svc = NewService(sc.v, home, io.Discard)
	sc.svc.Debounce, sc.svc.Retry = 10*time.Millisecond, 10*time.Millisecond
	return sc
}

func (sc *scene) write(rel string, data []byte) {
	sc.t.Helper()
	p := filepath.Join(sc.home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		sc.t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		sc.t.Fatal(err)
	}
}

func (sc *scene) read(rel string) []byte {
	b, _ := os.ReadFile(filepath.Join(sc.home, filepath.FromSlash(rel)))
	return b
}

func (sc *scene) upload(name string, data []byte) { sc.write("runtime/share/"+name, data) }

// request 寫一筆 pending 請求，回編號 U-<n>。
func (sc *scene) request(fields map[string]any) string {
	id := fmt.Sprintf("U-%d", sc.n)
	sc.n++
	f := cloneMap(fields)
	f["state"] = "pending"
	sc.v.seed(KindRequest, id, f)
	return id
}

func (sc *scene) round() {
	sc.t.Helper()
	if err := sc.svc.Round(context.Background()); err != nil {
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

func (sc *scene) done(id string) map[string]any {
	sc.t.Helper()
	f := sc.fields(KindRequest, id)
	if f["state"] != "done" {
		sc.t.Fatalf("%s 應該 done：%v", id, f)
	}
	return f
}

func (sc *scene) rejected(id, reason string) {
	sc.t.Helper()
	f := sc.fields(KindRequest, id)
	if f["state"] != "rejected" || f["reason"] != reason {
		sc.t.Fatalf("%s 應該被拒「%s」：%v", id, reason, f)
	}
}

// snapshot 記下 personas/ 底下所有檔案與內容，好比較「處理兩次結果相同」。
func (sc *scene) snapshot() string {
	var lines []string
	root := filepath.Join(sc.home, "personas")
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			b, _ := os.ReadFile(p)
			lines = append(lines, fmt.Sprintf("%s=%x", filepath.ToSlash(rel), b))
		}
		return nil
	})
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// photoFile 是照片紀錄 pid 指著的檔（相對 personas/mine/）的內容；紀錄不在回 nil。
func (sc *scene) photoFile(pid string) []byte {
	sc.t.Helper()
	r, ok := sc.v.get(kindPhoto, pid)
	if !ok {
		return nil
	}
	return sc.read("personas/mine/" + rules.Text(r.Fields["file"]))
}

// photoWant 檢查照片紀錄 pid 的欄位。
func (sc *scene) photoWant(pid string, want map[string]any) {
	sc.t.Helper()
	got := sc.fields(kindPhoto, pid)
	for k, v := range want {
		if got[k] != v {
			sc.t.Fatalf("%s 的 %s = %v，要 %v（%v）", pid, k, got[k], v, got)
		}
	}
}

// files 列 personas/mine/<dir>/ 底下的檔名（排好）。
func (sc *scene) files(dir string) []string {
	entries, _ := os.ReadDir(filepath.Join(sc.home, "personas", "mine", filepath.FromSlash(dir)))
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// backupOf 是 personas/mine/<rel> 旁邊原因是 reason 的備份（相對 personas/mine/）；沒有回空字串。
func (sc *scene) backupOf(rel, reason string) string {
	dir, base := filepath.Dir(rel), filepath.Base(rel)
	for _, n := range sc.files(dir) {
		if strings.HasPrefix(n, base+".bak-") && strings.HasSuffix(n, "-"+reason) {
			return dir + "/" + n
		}
	}
	return ""
}

func (sc *scene) officialUntouched() {
	sc.t.Helper()
	if got := sc.read("personas/avatars/athena.png"); !bytes.Equal(got, png("official-athena")) {
		sc.t.Fatal("官方圖被改了")
	}
	if got := sc.read("personas/athena.md"); string(got) != "---\nname: Athena\n---\n" {
		sc.t.Fatalf("官方角色卡被改了：%q", got)
	}
	if f := sc.fields(kindOutfit, "athena-base"); f["source"] != "official" || f["cost"] != 0 {
		sc.t.Fatalf("官方衣服被改了：%v", f)
	}
	entries, _ := os.ReadDir(filepath.Join(sc.home, "personas", "avatars"))
	if len(entries) != 1 {
		sc.t.Fatalf("官方圖資料夾不該多東西：%v", entries)
	}
}

// Guards: REQ-126
func TestRunCharacterCreatesCardImageOutfit(t *testing.T) {
	sc := newScene(t)
	img := png("fox-face")
	sc.upload("fox.bin", img) // 副檔名亂取：看開頭不看檔名
	id := sc.request(map[string]any{"type": "character", "upload": "/share/fox.bin", "name": `小"狐"`, "desc": "一隻狐狸\n第二行", "persona": "myfox"})
	sc.round()
	f := sc.done(id)
	if f["result"] != "myfox-base" {
		t.Fatalf("result：%v", f)
	}
	// 圖是角色本身的頭像 avatars/<代號>.<ext>（角色卡的慣例路徑：複製角色進套件只認它），也是基本款第一張：登記一筆照片紀錄指著它，
	// 只有這一個檔（不另寫 photos/ 一份）。
	if got := sc.read("personas/mine/avatars/myfox.png"); !bytes.Equal(got, img) {
		t.Fatal("圖要等於上傳檔，放在 avatars/<代號>.png")
	}
	sc.photoWant("myfox-base--u1", map[string]any{"persona": "myfox", "outfit": "myfox-base", "type": "half", "order": 1, "hidden": false,
		"source": "mine", "file": "avatars/myfox.png", "adopted": true})
	if got := sc.files("avatars"); len(got) != 1 || sc.files("photos") != nil {
		t.Fatalf("只有頭像一個檔：%v %v", got, sc.files("photos"))
	}
	card := string(sc.read("personas/mine/myfox.md"))
	if !strings.HasPrefix(card, "---\nschema: 1\nname: \"小\\\"狐\\\"\"\n---\n一隻狐狸\n第二行") {
		t.Fatalf("角色卡：%q", card)
	}
	o := sc.fields(kindOutfit, "myfox-base")
	if o["persona"] != "myfox" || o["outfit"] != "base" || o["name"] != "基本款" || o["cost"] != 0 || o["state"] != "active" || o["source"] != "mine" || o["order"] != 0 {
		t.Fatalf("base 衣服：%v", o)
	}
	// 沒給代號：用 my_<編號>。
	img2 := jpeg("cat")
	sc.upload("cat.jpg", img2)
	id2 := sc.request(map[string]any{"type": "character", "upload": "cat.jpg", "name": "貓"})
	sc.round()
	if f := sc.done(id2); f["result"] != fmt.Sprintf("my_%d-base", 2) {
		t.Fatalf("預設代號：%v", f)
	}
	if got := sc.read("personas/mine/avatars/my_2.jpg"); !bytes.Equal(got, img2) {
		t.Fatal("jpeg 存成 .jpg")
	}
	sc.officialUntouched()
}

// Guards: REQ-126
func TestRunOutfitOnOfficialPersonaIsMine(t *testing.T) {
	sc := newScene(t)
	img := png("athena-beach")
	sc.upload("beach.png", img)
	id := sc.request(map[string]any{"type": "outfit", "upload": "beach.png", "persona": "athena", "name": "海邊", "desc": "夏天", "free": false})
	sc.round()
	if f := sc.done(id); f["result"] != "athena-my-1" {
		t.Fatalf("result：%v", f)
	}
	o := sc.fields(kindOutfit, "athena-my-1")
	if o["source"] != "mine" || o["cost"] != 1 || o["state"] != "active" || o["name"] != "海邊" || o["desc"] != "夏天" || o["outfit"] != "my-1" {
		t.Fatalf("衣服：%v", o)
	}
	if got := sc.read("personas/mine/photos/athena-my-1--u1.png"); !bytes.Equal(got, img) {
		t.Fatal("圖放在 personas/mine/photos/<照片編號>.png")
	}
	sc.photoWant("athena-my-1--u1", map[string]any{"outfit": "athena-my-1", "type": "half", "order": 1, "source": "mine", "adopted": true})
	// 免費＋草稿。
	sc.upload("draft.png", png("draft"))
	id2 := sc.request(map[string]any{"type": "outfit", "upload": "draft.png", "persona": "athena", "name": "草稿", "free": true, "draft": true})
	sc.round()
	sc.done(id2)
	if o := sc.fields(kindOutfit, "athena-my-2"); o["cost"] != 0 || o["state"] != "draft" || o["source"] != "mine" {
		t.Fatalf("免費草稿：%v", o)
	}
	sc.photoWant("athena-my-2--u2", map[string]any{"adopted": false}) // 設計中的照片還沒採用
	// 價錢（cost）有寫就照它，比 free 優先；不合格的拒絕。
	sc.upload("gala.png", png("gala"))
	id4 := sc.request(map[string]any{"type": "outfit", "upload": "gala.png", "persona": "athena", "name": "晚宴", "cost": 3, "free": true})
	sc.round()
	sc.done(id4)
	if o := sc.fields(kindOutfit, "athena-my-3"); o["cost"] != 3 {
		t.Fatalf("cost 要照寫的：%v", o)
	}
	for _, bad := range []any{-1, 100, 1.5, "2"} {
		idb := sc.request(map[string]any{"type": "outfit", "upload": "gala.png", "persona": "athena", "name": "壞價錢", "cost": bad})
		sc.round()
		sc.rejected(idb, rules.ReasonBadCost)
	}
	// 找不到角色。
	id3 := sc.request(map[string]any{"type": "outfit", "upload": "draft.png", "persona": "ghost", "name": "x"})
	sc.round()
	sc.rejected(id3, rules.ReasonNoPersona)
	sc.officialUntouched()
}

// Guards: REQ-126
func TestRunPhotoFullVideo(t *testing.T) {
	sc := newScene(t)
	sc.upload("base.png", png("base"))
	sc.done(sc.requestAndRound(map[string]any{"type": "character", "upload": "base.png", "name": "狐", "persona": "myfox"}))

	// 半身照：每張一個照片編號，order 排在最後；一套最多 12 張。
	for i := 2; i <= 12; i++ {
		sc.upload("p.png", png(fmt.Sprintf("photo-%d", i)))
		sc.done(sc.requestAndRound(map[string]any{"type": "photo", "upload": "p.png", "persona": "myfox", "outfit": "base"}))
		pid := fmt.Sprintf("myfox-base--u%d", i)
		if got := sc.read("personas/mine/photos/" + pid + ".png"); !bytes.Equal(got, png(fmt.Sprintf("photo-%d", i))) {
			t.Fatalf("第 %d 張沒寫對", i)
		}
		sc.photoWant(pid, map[string]any{"type": "half", "order": i, "hidden": false, "adopted": true})
	}
	sc.upload("p13.png", png("photo-13"))
	id := sc.requestAndRound(map[string]any{"type": "photo", "upload": "p13.png", "persona": "myfox", "outfit": "base"})
	sc.rejected(id, rules.ReasonTooManyPhoto)
	if sc.read("personas/mine/photos/myfox-base--u13.png") != nil {
		t.Fatal("第 13 張不該寫")
	}

	// 全身照：一套一張；再放一次＝換那一張（同一筆紀錄，新檔、舊檔改名成備份）。
	sc.upload("full.png", png("full-1"))
	sc.done(sc.requestAndRound(map[string]any{"type": "full", "upload": "full.png", "persona": "myfox", "outfit": "base"})) // U-14
	sc.photoWant("myfox-base--u14", map[string]any{"type": "full", "order": 1, "file": "photos/myfox-base--u14.png"})
	sc.upload("full.jpg", jpeg("full-2"))
	sc.done(sc.requestAndRound(map[string]any{"type": "full", "upload": "full.jpg", "persona": "myfox", "outfit": "base"})) // U-15
	sc.photoWant("myfox-base--u14", map[string]any{"file": "photos/myfox-base--u14.jpg"})
	if !bytes.Equal(sc.photoFile("myfox-base--u14"), jpeg("full-2")) || sc.read("personas/mine/photos/myfox-base--u14.png") != nil {
		t.Fatal("換全身照：紀錄指到新檔、舊檔不在原名")
	}
	if bak := sc.backupOf("photos/myfox-base--u14.png", "replace-u15"); bak == "" || !bytes.Equal(sc.read("personas/mine/"+bak), png("full-1")) {
		t.Fatalf("舊的全身照要留備份：%v", sc.files("photos"))
	}
	if _, ok := sc.v.get(kindPhoto, "myfox-base--u15"); ok {
		t.Fatal("換圖不另登記")
	}

	// 動畫：photo、clip 是位置（第幾張半身照、那張的第幾支）；等於現有支數 +1 是加一支、其他是換那一支；只收 MP4。
	sc.upload("v1.mp4", mp4("v1"))
	sc.done(sc.requestAndRound(map[string]any{"type": "video", "upload": "v1.mp4", "persona": "myfox", "outfit": "base"})) // U-16
	sc.photoWant("myfox-base--u16", map[string]any{"type": "video", "of": "myfox-base--u1", "clip": 1, "order": 1, "file": "photos/myfox-base--u16.mp4"})
	sc.upload("v1b.mp4", mp4("v1-new"))
	sc.done(sc.requestAndRound(map[string]any{"type": "video", "upload": "v1b.mp4", "persona": "myfox", "outfit": "base"})) // U-17 換第 1 支
	if !bytes.Equal(sc.photoFile("myfox-base--u16"), mp4("v1-new")) || sc.backupOf("photos/myfox-base--u16.mp4", "replace-u17") == "" {
		t.Fatal("動畫要換、舊的留備份")
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "video", "upload": "v1.mp4", "persona": "myfox", "outfit": "base", "photo": 3})) // U-18
	sc.photoWant("myfox-base--u18", map[string]any{"of": "myfox-base--u3", "clip": 1, "order": 3})
	sc.rejected(sc.requestAndRound(map[string]any{"type": "video", "upload": "v1.mp4", "persona": "myfox", "outfit": "base", "photo": 13}), rules.ReasonNoPhoto(13))
	// 多支動畫（DEV-125）：要連續放，第 7 支被拒，換第 2 支。
	sc.rejected(sc.requestAndRound(map[string]any{"type": "video", "upload": "v1.mp4", "persona": "myfox", "outfit": "base", "clip": 3}), rules.ReasonClipGap(3))
	sc.done(sc.requestAndRound(map[string]any{"type": "video", "upload": "v1.mp4", "persona": "myfox", "outfit": "base", "clip": 2})) // U-21
	sc.photoWant("myfox-base--u21", map[string]any{"of": "myfox-base--u1", "clip": 2})
	sc.done(sc.requestAndRound(map[string]any{"type": "video", "upload": "v1b.mp4", "persona": "myfox", "outfit": "base", "clip": 2})) // U-22
	if !bytes.Equal(sc.photoFile("myfox-base--u21"), mp4("v1-new")) {
		t.Fatal("第 2 支動畫要換")
	}
	sc.done(sc.requestAndRound(map[string]any{"type": "video", "upload": "v1.mp4", "persona": "myfox", "outfit": "base", "photo": 3, "clip": 2})) // U-23
	sc.photoWant("myfox-base--u23", map[string]any{"of": "myfox-base--u3", "clip": 2})
	sc.rejected(sc.requestAndRound(map[string]any{"type": "video", "upload": "v1.mp4", "persona": "myfox", "outfit": "base", "clip": 7}), rules.ReasonNoClip(7))
	sc.rejected(sc.requestAndRound(map[string]any{"type": "video", "upload": "p.png", "persona": "myfox", "outfit": "base"}), rules.ReasonVideoOnly)

	// 官方衣服不能改；沒有的衣服。
	sc.rejected(sc.requestAndRound(map[string]any{"type": "photo", "upload": "p.png", "persona": "athena", "outfit": "base"}), rules.ReasonOfficial)
	sc.rejected(sc.requestAndRound(map[string]any{"type": "full", "upload": "p.png", "persona": "athena", "outfit": "base"}), rules.ReasonOfficial)
	sc.rejected(sc.requestAndRound(map[string]any{"type": "photo", "upload": "p.png", "persona": "athena", "outfit": "nope"}), rules.ReasonNoOutfit)
	sc.officialUntouched()
}

func (sc *scene) requestAndRound(fields map[string]any) string {
	id := sc.request(fields)
	sc.round()
	return id
}

// Guards: REQ-126
func TestRunRejections(t *testing.T) {
	sc := newScene(t)
	sc.upload("ok.png", png("ok"))
	sc.upload("notimage.png", []byte("hello, I am text with a png extension"))
	sc.upload("clip.mp4", mp4("clip"))
	for name, tc := range map[string]struct {
		f    map[string]any
		want string
	}{
		"找不到上傳":      {map[string]any{"type": "character", "upload": "/share/missing.png", "name": "x"}, rules.ReasonNoUpload},
		"上傳檔名跳脫":     {map[string]any{"type": "character", "upload": "/share/../runtime/share/ok.png", "name": "x"}, rules.ReasonNoUpload},
		"文字檔假裝成 png": {map[string]any{"type": "character", "upload": "notimage.png", "name": "x"}, rules.ReasonImageOnly},
		"影片當角色圖":     {map[string]any{"type": "character", "upload": "clip.mp4", "name": "x"}, rules.ReasonImageOnly},
		"壞代號":        {map[string]any{"type": "character", "upload": "ok.png", "name": "x", "persona": "Fox"}, rules.ReasonBadCode},
		"官方代號被用":     {map[string]any{"type": "character", "upload": "ok.png", "name": "x", "persona": "athena"}, rules.ReasonBadCode},
		"沒名字":        {map[string]any{"type": "character", "upload": "ok.png"}, rules.ReasonNeedName},
		"不認得的種類":     {map[string]any{"type": "dance", "upload": "ok.png"}, rules.ReasonUnknownType},
		"outfit 影片":  {map[string]any{"type": "outfit", "upload": "clip.mp4", "persona": "athena", "name": "x"}, rules.ReasonImageOnly},
		"full 文字檔":   {map[string]any{"type": "full", "upload": "notimage.png", "persona": "athena", "outfit": "base"}, rules.ReasonImageOnly},
	} {
		id := sc.requestAndRound(tc.f)
		f := sc.fields(KindRequest, id)
		if f["state"] != "rejected" || f["reason"] != tc.want {
			t.Errorf("%s：%v，要被拒「%s」", name, f, tc.want)
		}
	}
	// 官方角色卡已存在的代號（athena 不是 my 開頭，所以是壞代號；用「已有人用」另外驗：自己的卡）。
	sc.write("personas/mine/myfox.md", []byte("---\nschema: 1\nname: \"別人的狐\"\n---\n"))
	sc.rejected(sc.requestAndRound(map[string]any{"type": "character", "upload": "ok.png", "name": "我的狐", "persona": "myfox"}), rules.ReasonCodeTaken)
	if sc.files("photos") != nil {
		t.Fatalf("被拒的請求不能留下圖：%v", sc.files("photos"))
	}
	// 已有 base 衣服（官方來源）的代號。
	sc.v.seed(kindOutfit, "myothers-base", map[string]any{"persona": "myothers", "outfit": "base", "name": "x", "cost": 0, "state": "active", "source": "official"})
	sc.rejected(sc.requestAndRound(map[string]any{"type": "character", "upload": "ok.png", "name": "x", "persona": "myothers"}), rules.ReasonCodeTaken)
	// 頭像 avatars/<代號>.<ext> 已經在、又不是這次上傳的檔（S3 以前做的角色，或別的請求）：這個代號已經被用了。
	sc.write("personas/mine/avatars/myimg.png", png("someone-else"))
	sc.rejected(sc.requestAndRound(map[string]any{"type": "character", "upload": "ok.png", "name": "x", "persona": "myimg"}), rules.ReasonCodeTaken)
	if got := sc.read("personas/mine/avatars/myimg.png"); !bytes.Equal(got, png("someone-else")) {
		t.Fatal("既有的圖不能被覆蓋")
	}
	// 被拒之後不會再處理第二次。
	puts := sc.v.puts[kindOutfit]
	sc.round()
	if sc.v.puts[kindOutfit] != puts {
		t.Fatal("被拒的請求不該再被處理")
	}
	sc.officialUntouched()
}

// 每種請求處理兩次（行程在標 done 前被殺，重開後同一個請求再來一次）：結果相同，photo 不多一張。
// Guards: REQ-126
func TestRunReplaySafe(t *testing.T) {
	sc := newScene(t)
	sc.upload("base.png", png("base"))
	sc.upload("beach.png", png("beach"))
	sc.upload("photo.png", png("photo"))
	sc.upload("full.png", png("full"))
	sc.upload("clip.mp4", mp4("clip"))
	reqs := []map[string]any{
		{"type": "character", "upload": "base.png", "name": "狐", "desc": "d", "persona": "myfox"},
		{"type": "outfit", "upload": "beach.png", "persona": "athena", "name": "海邊"},
		{"type": "photo", "upload": "photo.png", "persona": "myfox", "outfit": "base"},
		{"type": "full", "upload": "full.png", "persona": "myfox", "outfit": "base"},
		{"type": "video", "upload": "clip.mp4", "persona": "myfox", "outfit": "base"},
	}
	for _, f := range reqs {
		id := sc.requestAndRound(f)
		first := sc.done(id)
		snap, outfits, photos := sc.snapshot(), sc.v.puts[kindOutfit], sc.v.puts[kindPhoto]
		// 假裝標 done 之前行程被殺：把請求改回 pending，再處理一次。
		if err := sc.v.Patch(context.Background(), KindRequest, id, map[string]any{"state": "pending", "result": "", "reason": ""}); err != nil {
			t.Fatal(err)
		}
		sc.round()
		second := sc.done(id)
		if second["result"] != first["result"] {
			t.Fatalf("%v：兩次結果不同 %v / %v", f["type"], first["result"], second["result"])
		}
		if sc.snapshot() != snap || sc.v.puts[kindOutfit] != outfits || sc.v.puts[kindPhoto] != photos {
			t.Fatalf("%v：第二次處理改了檔案或又寫了衣服、照片紀錄", f["type"])
		}
	}
	list, _ := sc.svc.outfitPhotos(context.Background(), "myfox-base")
	if len(halvesOf(list, false)) != 2 {
		t.Fatal("photo 重播不能多一張")
	}

	// 做到一半（頭像和角色卡寫了、closet.outfit 沒寫）被殺：重開後補完，不是「代號被用了」（頭像就是這次上傳的檔）。
	sc.upload("half.png", png("half"))
	sc.write("personas/mine/avatars/myhalf.png", png("half"))
	sc.write("personas/mine/myhalf.md", []byte(rules.CharacterCard("半套", "")))
	id := sc.requestAndRound(map[string]any{"type": "character", "upload": "half.png", "name": "半套", "persona": "myhalf"})
	sc.done(id)
	if o := sc.fields(kindOutfit, "myhalf-base"); o["source"] != "mine" {
		t.Fatalf("要補寫 base 衣服：%v", o)
	}
	sc.photoWant("myhalf-base--u6", map[string]any{"file": "avatars/myhalf.png"})
	// 做到一半（衣服寫了、照片紀錄沒寫）：補登記照片紀錄。
	sc.upload("mid.png", png("mid"))
	sc.write("personas/mine/photos/athena-my-7--u7.png", png("mid"))
	sc.v.seed(kindOutfit, "athena-my-7", map[string]any{"persona": "athena", "outfit": "my-7", "name": "半套", "cost": 1, "state": "active", "source": "mine"})
	sc.done(sc.requestAndRound(map[string]any{"type": "outfit", "upload": "mid.png", "persona": "athena", "name": "半套"}))
	sc.photoWant("athena-my-7--u7", map[string]any{"outfit": "athena-my-7", "order": 1})
	sc.officialUntouched()
}

// 已處理超過 7 天的請求會封存；沒到的保留。
// Guards: REQ-126
func TestRunArchivesOldRequests(t *testing.T) {
	sc := newScene(t)
	sc.v.seed(KindRequest, "U-1", map[string]any{"type": "character", "state": "done"})
	sc.v.seed(KindRequest, "U-2", map[string]any{"type": "character", "state": "rejected"})
	sc.v.mu.Lock()
	r := sc.v.recs[KindRequest]["U-1"]
	r.UpdatedAt = time.Now().Add(-8 * 24 * time.Hour)
	sc.v.recs[KindRequest]["U-1"] = r
	sc.v.mu.Unlock()
	sc.round()
	if r, _ := sc.v.get(KindRequest, "U-1"); !r.Archived {
		t.Fatal("超過 7 天的要封存")
	}
	if r, _ := sc.v.get(KindRequest, "U-2"); r.Archived {
		t.Fatal("剛處理的要保留")
	}
}

// Run：開機一輪、vault 有變去抖後再做一輪（watch 事件）。
// Guards: REQ-126
func TestRunLoopProcessesOnWatch(t *testing.T) {
	sc := newScene(t)
	sc.upload("a.png", png("a"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sc.svc.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	time.Sleep(50 * time.Millisecond)
	id := sc.request(map[string]any{"type": "character", "upload": "a.png", "name": "甲", "persona": "myaaa"})
	sc.v.events <- struct{}{}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f := sc.fields(KindRequest, id); f["state"] == "done" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("watch 之後沒有處理請求")
}

// personas/mine 是符號連結：請求被拒（說明原因）、目標資料夾不變。
// Guards: REQ-126
func TestRunSymlinkedMineFolderRejected(t *testing.T) {
	sc := newScene(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(sc.home, "personas", "mine")); err != nil {
		t.Fatal(err)
	}
	sc.upload("a.png", png("a"))
	id := sc.requestAndRound(map[string]any{"type": "character", "upload": "a.png", "name": "甲", "persona": "myaaa"})
	sc.rejected(id, rules.ReasonUnsafeFolder)
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("符號連結指到的資料夾被寫入：%v", entries)
	}
	if _, ok := sc.v.get(kindOutfit, "myaaa-base"); ok {
		t.Fatal("不該登記衣服")
	}
}
