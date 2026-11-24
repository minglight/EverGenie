package promote

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// promoteFake 是記憶體裡的 vault（Get 讀得到封存的，跟真的一樣）。
type promoteFake struct {
	recs map[string]sdk.Record
	rev  int
}

func (f *promoteFake) key(kind, id string) string { return kind + "/" + id }

func (f *promoteFake) seed(kind, id string, fields map[string]any) {
	f.rev++
	f.recs[f.key(kind, id)] = sdk.Record{Kind: kind, ID: id, Fields: fields, Revision: fmt.Sprint(f.rev)}
}

func (f *promoteFake) Get(_ context.Context, kind, id string) (*sdk.Record, error) {
	r, ok := f.recs[f.key(kind, id)]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (f *promoteFake) List(_ context.Context, kind string, _ bool) ([]sdk.Record, error) {
	var out []sdk.Record
	for _, r := range f.recs {
		if r.Kind == kind && !r.Archived {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *promoteFake) Put(_ context.Context, rec sdk.Record, rev string) error {
	cur, ok := f.recs[f.key(rec.Kind, rec.ID)]
	if (!ok && rev != "") || (ok && cur.Revision != rev) {
		return errors.New("版本不符")
	}
	f.rev++
	rec.Revision = fmt.Sprint(f.rev)
	f.recs[f.key(rec.Kind, rec.ID)] = rec
	return nil
}

func (f *promoteFake) Archive(_ context.Context, kind, id string) error {
	r := f.recs[f.key(kind, id)]
	r.Archived = true
	f.recs[f.key(kind, id)] = r
	return nil
}

// promoteHome：有官方角色 athena，主人自己的衣服 my-12（採用了、有輪播照、全身照、動畫，還有一張自己的隱藏版）、
// 一套設計中的 my-13、別套 my-120；輪播設定勾了 my-12。照片紀錄（closet.photo）指著各自的圖。
func promoteHome(t *testing.T) (string, *promoteFake) {
	t.Helper()
	h := t.TempDir()
	w := func(rel, body string) {
		p := filepath.Join(h, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("personas/athena.md", "---\nschema: 1\nname: Athena\n---\n")
	w("personas/avatars/athena.png", "official")
	for _, f := range []string{"athena@my-12.png", "athena@my-12~2.png", "athena@my-12.mp4", "full/athena@my-12.png", "athena@my-120.png"} {
		w("personas/mine/avatars/"+f, "mine "+f)
	}
	w("personas/mine/adult/athena@my-12.png", "adult")
	v := &promoteFake{recs: map[string]sdk.Record{}}
	v.seed("closet.outfit", "athena-my-12", map[string]any{"persona": "athena", "outfit": "my-12", "name": "海邊", "cost": 1, "state": "active", "source": "mine", "by": "closet-mine"})
	v.seed("closet.outfit", "athena-my-13", map[string]any{"persona": "athena", "outfit": "my-13", "name": "草稿", "cost": 1, "state": "draft", "source": "mine"})
	v.seed("closet.rotation", "athena", map[string]any{"outfits": []any{"base", "my-12"}})
	photo := func(id, outfit, typ, file string, hidden bool, extra map[string]any) {
		f := map[string]any{"persona": "athena", "outfit": outfit, "type": typ, "order": 1, "hidden": hidden, "source": "mine", "file": file, "adopted": true}
		for k, x := range extra {
			f[k] = x
		}
		v.seed("closet.photo", id, f)
	}
	photo("athena-my-12--half1", "athena-my-12", "half", "avatars/athena@my-12.png", false, nil)
	photo("athena-my-12--half2", "athena-my-12", "half", "avatars/athena@my-12~2.png", false, map[string]any{"order": 2})
	photo("athena-my-12--video1-c1", "athena-my-12", "video", "avatars/athena@my-12.mp4", false, map[string]any{"of": "athena-my-12--half1", "clip": 1})
	photo("athena-my-12--full1", "athena-my-12", "full", "avatars/full/athena@my-12.png", false, nil)
	photo("athena-my-12--half1-h", "athena-my-12", "half", "adult/athena@my-12.png", true, map[string]any{"adopted": false}) // 自己的隱藏版不升，沒採用也不擋
	photo("athena-my-120--half1", "athena-my-120", "half", "avatars/athena@my-120.png", false, nil)
	return h, v
}

func readRel(h, rel string) string {
	b, _ := os.ReadFile(filepath.Join(h, filepath.FromSlash(rel)))
	return string(b)
}

// 開發家：照片紀錄指著的圖搬到官方位置（檔名不改，DEV-160 S2）、舊的圖改名成備份、目錄新增官方的一筆（帶 from）並封存舊的、
// 照片紀錄換到新編號（搬了的改 source = official）、輪播跟著換；自己的隱藏版的圖不碰、紀錄跟著換編號。
func TestPromoteMovesFilesAndRecord(t *testing.T) {
	h, v := promoteHome(t)
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.Local)
	res, err := Run(context.Background(), Options{Home: h, Target: "athena-my-12", NewID: "seaside", Vault: v, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if res.From != "athena-my-12" || res.To != "athena-seaside" || len(res.Files) != 4 {
		t.Fatalf("結果：%+v", res)
	}
	for to, want := range map[string]string{
		"personas/avatars/athena@my-12.png":      "mine athena@my-12.png",
		"personas/avatars/athena@my-12~2.png":    "mine athena@my-12~2.png",
		"personas/avatars/athena@my-12.mp4":      "mine athena@my-12.mp4",
		"personas/avatars/full/athena@my-12.png": "mine full/athena@my-12.png",
	} {
		if readRel(h, to) != want {
			t.Errorf("%s：%q", to, readRel(h, to))
		}
	}
	if readRel(h, "personas/mine/avatars/athena@my-12.png") != "" || readRel(h, "personas/mine/avatars/athena@my-12.png.bak-20261107-promote") != "mine athena@my-12.png" {
		t.Fatal("舊的圖要改名成備份（能退回）")
	}
	if readRel(h, "personas/mine/avatars/athena@my-120.png") == "" || readRel(h, "personas/avatars/athena@my-120.png") != "" {
		t.Fatal("別套衣服（my-120）不能被抓進來")
	}
	if readRel(h, "personas/mine/adult/athena@my-12.png") != "adult" || readRel(h, "personas/avatars/adult/athena@my-12.png") != "" {
		t.Fatal("自己的隱藏版不碰、不升")
	}
	for id, want := range map[string]map[string]any{
		"athena-seaside--half1":     {"outfit": "athena-seaside", "source": "official", "file": "avatars/athena@my-12.png"},
		"athena-seaside--video1-c1": {"outfit": "athena-seaside", "source": "official", "of": "athena-seaside--half1", "file": "avatars/athena@my-12.mp4"},
		"athena-seaside--full1":     {"source": "official", "file": "avatars/full/athena@my-12.png"},
		"athena-seaside--half1-h":   {"outfit": "athena-seaside", "source": "mine", "hidden": true, "file": "adult/athena@my-12.png"},
		"athena-my-120--half1":      {"outfit": "athena-my-120", "source": "mine"},
	} {
		r, _ := v.Get(context.Background(), "closet.photo", id)
		if r == nil || r.Archived {
			t.Fatalf("照片紀錄 %s 要在", id)
		}
		for k, x := range want {
			if r.Fields[k] != x {
				t.Fatalf("照片紀錄 %s 的 %s = %v，要 %v", id, k, r.Fields[k], x)
			}
		}
	}
	if old, _ := v.Get(context.Background(), "closet.photo", "athena-my-12--half1"); old == nil || !old.Archived {
		t.Fatal("舊的照片紀錄只封存")
	}
	if fmt.Sprint(res.Hidden) != "[athena-seaside--half1-h]" {
		t.Fatalf("隱藏版的照片要列出（不升）：%v", res.Hidden)
	}
	n, _ := v.Get(context.Background(), "closet.outfit", "athena-seaside")
	if n == nil || n.Fields["source"] != "official" || n.Fields["outfit"] != "seaside" || n.Fields["from"] != "athena-my-12" || n.Fields["cost"] != 1 || n.Fields["by"] != nil {
		t.Fatalf("新的那筆：%+v", n)
	}
	if old, _ := v.Get(context.Background(), "closet.outfit", "athena-my-12"); old == nil || !old.Archived {
		t.Fatal("舊的那筆只封存（能退回）")
	}
	rot, _ := v.Get(context.Background(), "closet.rotation", "athena")
	if fmt.Sprint(rot.Fields["outfits"]) != "[base seaside]" {
		t.Fatalf("輪播設定：%v", rot.Fields["outfits"])
	}
	if len(res.Undo) == 0 {
		t.Fatal("要列出怎麼退回")
	}
}

// 預演什麼都不動。
func TestPromoteDryRunChangesNothing(t *testing.T) {
	h, v := promoteHome(t)
	res, err := Run(context.Background(), Options{Home: h, Target: "athena-my-12", NewID: "seaside", Vault: v, DryRun: true})
	if err != nil || !res.DryRun || len(res.Files) != 4 {
		t.Fatalf("%+v %v", res, err)
	}
	if readRel(h, "personas/avatars/athena@my-12.png") != "" || readRel(h, "personas/mine/avatars/athena@my-12.png") == "" {
		t.Fatal("預演不能動檔案")
	}
	if r, _ := v.Get(context.Background(), "closet.outfit", "athena-seaside"); r != nil {
		t.Fatal("預演不能寫紀錄")
	}
	if r, _ := v.Get(context.Background(), "closet.photo", "athena-seaside--half1"); r != nil {
		t.Fatal("預演不能寫照片紀錄")
	}
}

// 不能升的情況：設計中、my- 開頭沒給 --id、官方的、照片全是隱藏版、新的 id 用 my-、已經有同編號、照片紀錄的 file 不在 avatars/ 底下。
func TestPromoteRefusals(t *testing.T) {
	cases := []struct {
		name, target, id, want string
	}{
		{"設計中", "athena-my-13", "draft", "還沒上架"},
		{"my- 沒給 id", "athena-my-12", "", "new_id"},
		{"已經是官方", "athena-base", "", "已經是官方"},
		{"照片全是隱藏版", "athena-secret", "secret2", "照片全是隱藏版"},
		{"file 跑出 avatars/", "athena-odd", "odd2", "不是 avatars/ 或 photos/ 底下"},
		{"新 id 用 my-", "athena-my-12", "my-99", "不能用 my- 開頭"},
		{"已經有同編號", "athena-my-12", "taken", "已經有 athena-taken"},
		{"有照片沒採用", "athena-wip", "wip2", "還有 2 張照片沒採用（athena-wip--half1、athena-wip--half2）"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, v := promoteHome(t)
			v.seed("closet.outfit", "athena-base", map[string]any{"persona": "athena", "outfit": "base", "name": "原樣", "cost": 0, "state": "active", "source": "official"})
			v.seed("closet.outfit", "athena-taken", map[string]any{"persona": "athena", "outfit": "taken", "name": "x", "cost": 0, "state": "active", "source": "official"})
			for _, o := range []string{"secret", "odd", "wip"} {
				v.seed("closet.outfit", "athena-"+o, map[string]any{"persona": "athena", "outfit": o, "name": o, "cost": 0, "state": "active", "source": "mine"})
			}
			v.seed("closet.photo", "athena-secret--half1-h", map[string]any{"persona": "athena", "outfit": "athena-secret", "type": "half", "order": 1, "hidden": true, "source": "mine", "file": "adult/athena@secret.png"})
			v.seed("closet.photo", "athena-odd--half1", map[string]any{"persona": "athena", "outfit": "athena-odd", "type": "half", "order": 1, "hidden": false, "source": "mine", "file": "../athena.md", "adopted": true})
			// 秘書換過圖、回到還沒採用的那張（CMI-14 的預檢）：自己的隱藏版沒採用不算。
			v.seed("closet.photo", "athena-wip--half1", map[string]any{"persona": "athena", "outfit": "athena-wip", "type": "half", "order": 1, "hidden": false, "source": "mine", "file": "avatars/athena@my-120.png", "adopted": false})
			v.seed("closet.photo", "athena-wip--half2", map[string]any{"persona": "athena", "outfit": "athena-wip", "type": "half", "order": 2, "hidden": false, "source": "mine", "file": "avatars/athena@my-12~2.png"})
			v.seed("closet.photo", "athena-wip--half1-h", map[string]any{"persona": "athena", "outfit": "athena-wip", "type": "half", "order": 1, "hidden": true, "source": "mine", "file": "adult/athena@my-12.png", "adopted": false})
			_, err := Run(context.Background(), Options{Home: h, Target: c.target, NewID: c.id, Vault: v})
			var rj *Rejected
			if !errors.As(err, &rj) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("要拒絕（%s）：%v", c.want, err)
			}
			if readRel(h, "personas/mine/avatars/athena@my-12.png") == "" {
				t.Fatal("被拒時不能動檔案")
			}
		})
	}
}

// 不是官方角色（我的角色）的衣服不能升。
func TestPromoteNeedsOfficialPersona(t *testing.T) {
	h, v := promoteHome(t)
	v.seed("closet.outfit", "myluna-my-2", map[string]any{"persona": "myluna", "outfit": "my-2", "name": "x", "cost": 0, "state": "active", "source": "mine"})
	if _, err := Run(context.Background(), Options{Home: h, Target: "myluna-my-2", NewID: "x", Vault: v}); err == nil || !strings.Contains(err.Error(), "不是官方角色") {
		t.Fatalf("%v", err)
	}
}

// failVault 讓某一步失敗一次。
type failVault struct {
	*promoteFake
	failPut, failArchive bool
	failPhotoPut         int // 第幾次寫照片紀錄時失敗一次（0＝不失敗）
	photoPuts            int
}

func (f *failVault) Put(ctx context.Context, rec sdk.Record, rev string) error {
	if f.failPut && rec.Kind == "closet.outfit" {
		f.failPut = false
		return errors.New("put 壞了")
	}
	if rec.Kind == "closet.photo" {
		f.photoPuts++
		if f.photoPuts == f.failPhotoPut {
			return errors.New("put 壞了")
		}
	}
	return f.promoteFake.Put(ctx, rec, rev)
}

func (f *failVault) Archive(ctx context.Context, kind, id string) error {
	if f.failArchive {
		f.failArchive = false
		return errors.New("archive 壞了")
	}
	return f.promoteFake.Archive(ctx, kind, id)
}

// 改紀錄失敗：這次新寫的官方圖刪掉，舊的圖不動。
func TestPromoteRecordFailureRemovesWrittenFiles(t *testing.T) {
	h, v := promoteHome(t)
	fv := &failVault{promoteFake: v, failPut: true}
	if _, err := Run(context.Background(), Options{Home: h, Target: "athena-my-12", NewID: "seaside", Vault: fv}); err == nil {
		t.Fatal("要失敗")
	}
	if readRel(h, "personas/avatars/athena@my-12.png") != "" || readRel(h, "personas/mine/avatars/athena@my-12.png") == "" {
		t.Fatal("新寫的官方圖要刪掉、舊的圖不動")
	}
}

// 新的那筆寫好、封存舊的失敗：重跑同一行接著做完（不報已經有），兌換照樣接得上。
func TestPromoteResumesAfterArchiveFailure(t *testing.T) {
	h, v := promoteHome(t)
	fv := &failVault{promoteFake: v, failArchive: true}
	opts := Options{Home: h, Target: "athena-my-12", NewID: "seaside", Vault: fv}
	var rj *Rejected
	if _, err := Run(context.Background(), opts); err == nil || errors.As(err, &rj) || !strings.Contains(err.Error(), "接著做") {
		t.Fatalf("要是暫時失敗（請求維持 pending、下一輪接著做）：%v", err)
	}
	res, err := Run(context.Background(), opts)
	if err != nil || !res.Resumed {
		t.Fatalf("重跑要接著做：%+v %v", res, err)
	}
	if old, _ := v.Get(context.Background(), "closet.outfit", "athena-my-12"); !old.Archived {
		t.Fatal("舊的要封存")
	}
	if readRel(h, "personas/mine/avatars/athena@my-12.png.bak-"+time.Now().Format("20060102")+"-promote") == "" {
		t.Fatal("舊的圖要改名成備份")
	}
	// 全做完之後再跑一次也還是接得上（什麼都不多做）。
	if res, err := Run(context.Background(), opts); err != nil || len(res.Files) != 0 {
		t.Fatalf("做完再跑：%+v %v", res, err)
	}
}

// 同一個編號升（不換 new_id）：衣服先改成官方，照片紀錄改到一半失敗——重跑認得是上次做到一半，接著改完、舊的圖改名成備份；
// 只剩舊的圖沒改名（紀錄都改好了、主人那份跟官方的一模一樣）也一樣接著做；全做完再跑就是「已經是官方的衣服」。
func TestPromoteSameIDResumes(t *testing.T) {
	h, v := promoteHome(t)
	if err := os.WriteFile(filepath.Join(h, "personas", "mine", "avatars", "athena@beach.png"), []byte("beach"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h, "personas", "mine", "avatars", "athena@beach~2.png"), []byte("beach2"), 0o644); err != nil {
		t.Fatal(err)
	}
	v.seed("closet.outfit", "athena-beach", map[string]any{"persona": "athena", "outfit": "beach", "name": "沙灘", "cost": 1, "state": "active", "source": "mine"})
	for i, f := range []string{"avatars/athena@beach.png", "avatars/athena@beach~2.png"} {
		v.seed("closet.photo", fmt.Sprintf("athena-beach--half%d", i+1), map[string]any{"persona": "athena", "outfit": "athena-beach", "type": "half", "order": i + 1,
			"hidden": false, "source": "mine", "file": f, "adopted": true})
	}
	fv := &failVault{promoteFake: v, failPhotoPut: 2}
	opts := Options{Home: h, Target: "athena-beach", Vault: fv}
	var rj *Rejected
	if _, err := Run(context.Background(), opts); err == nil || errors.As(err, &rj) || !strings.Contains(err.Error(), "接著做") {
		t.Fatalf("照片紀錄改到一半：暫時失敗、下一輪接著做：%v", err)
	}
	if r, _ := v.Get(context.Background(), "closet.outfit", "athena-beach"); r.Fields["source"] != "official" {
		t.Fatal("前提：衣服已經改成官方")
	}
	res, err := Run(context.Background(), opts)
	if err != nil || !res.Resumed {
		t.Fatalf("重跑要接著做：%+v %v", res, err)
	}
	for _, id := range []string{"athena-beach--half1", "athena-beach--half2"} {
		if r, _ := v.Get(context.Background(), "closet.photo", id); r.Fields["source"] != "official" {
			t.Fatalf("%s 要改成官方：%v", id, r.Fields)
		}
	}
	bak := ".bak-" + time.Now().Format("20060102") + "-promote"
	if readRel(h, "personas/mine/avatars/athena@beach.png"+bak) != "beach" || readRel(h, "personas/avatars/athena@beach~2.png") != "beach2" {
		t.Fatal("圖搬到官方位置、舊的改名成備份")
	}
	// 只剩舊的圖沒改名：主人那份放回去（跟官方的一模一樣）→ 接著做（改名）。
	if err := os.Rename(filepath.Join(h, "personas", "mine", "avatars", "athena@beach~2.png"+bak), filepath.Join(h, "personas", "mine", "avatars", "athena@beach~2.png")); err != nil {
		t.Fatal(err)
	}
	if res, err := Run(context.Background(), Options{Home: h, Target: "athena-beach", Vault: v}); err != nil || !res.Resumed || len(res.Files) != 1 {
		t.Fatalf("只剩改名也接著做：%+v %v", res, err)
	}
	if _, err := Run(context.Background(), Options{Home: h, Target: "athena-beach", Vault: v}); !errors.As(err, &rj) || !strings.Contains(err.Error(), "已經是官方的衣服") {
		t.Fatalf("全做完再跑：%v", err)
	}
}

// 退回步驟只用現成指令與改檔名：刪掉官方位置的圖、備份改回原名，新的那筆與照片紀錄改回 mine（不寫「從封存拿回來」）。
func TestPromoteUndo(t *testing.T) {
	h, v := promoteHome(t)
	res, err := Run(context.Background(), Options{Home: h, Target: "athena-my-12", NewID: "seaside", Vault: v})
	if err != nil {
		t.Fatal(err)
	}
	undo := strings.Join(res.Undo, "\n")
	mine := filepath.Join("personas", "mine", "avatars", "athena@my-12.png")
	for _, want := range []string{
		`data set closet.outfit athena-seaside --json '{"source":"mine"}'`,
		`data set closet.photo athena-seaside--half1 --json '{"source":"mine"}'`,
		"把 " + mine + ".bak-" + time.Now().Format("20060102") + "-promote 改回 " + mine,
		"刪掉 " + filepath.Join("personas", "avatars", "athena@my-12.png"),
	} {
		if !strings.Contains(undo, want) {
			t.Fatalf("退回步驟少了 %q：\n%s", want, undo)
		}
	}
	if strings.Contains(undo, "從封存拿回來") || strings.Contains(undo, "athena-seaside--half1-h") {
		t.Fatalf("退回步驟：\n%s", undo)
	}
}

// 結果寫成給人看的幾行：預演標明沒動、列出要搬的檔與退回步驟。
func TestPromoteSummary(t *testing.T) {
	h, v := promoteHome(t)
	res, err := Run(context.Background(), Options{Home: h, Target: "athena-my-12", NewID: "seaside", Vault: v, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	sum := res.Summary()
	for _, want := range []string{"預演（什麼都沒動）", "athena@my-12.png", "要退回", "closet.rotation/athena", "照片紀錄 4 筆", "注意：closet.photo/athena-seaside--half1-h"} {
		if !strings.Contains(sum, want) {
			t.Fatalf("少了 %q：\n%s", want, sum)
		}
	}
	if strings.Contains(sum, "public_personas") {
		t.Fatalf("預演不用講帶出去：\n%s", sum)
	}
	res, err = Run(context.Background(), Options{Home: h, Target: "athena-my-12", NewID: "seaside", Vault: v})
	if err != nil {
		t.Fatal(err)
	}
	if sum = res.Summary(); !strings.Contains(sum, "已放進這個家的官方位置") || !strings.Contains(sum, "public_personas") || strings.Contains(sum, "下一次建角色包會帶出去") {
		t.Fatalf("要說實話：只放進這個家，要帶出去得列 public_personas 再建包：\n%s", sum)
	}
}

// 新上傳的照片（photos/<照片編號>.<ext>，DEV-160 S3）升成官方搬到官方的 avatars/photos/<照片編號>.<ext>（官方的圖都在 avatars/ 底下），
// 紀錄的 source、file 跟著換；自己的隱藏版留在 personas/mine/photos/；同一個編號做到一半也接得上；退回步驟把 file 改回去。
func TestPromotePhotosFolderFiles(t *testing.T) {
	h, v := promoteHome(t)
	for _, f := range []string{"athena-beach--u1.png", "athena-beach--u2.mp4", "athena-beach--u3.png"} {
		if err := os.MkdirAll(filepath.Join(h, "personas", "mine", "photos"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(h, "personas", "mine", "photos", f), []byte("mine "+f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v.seed("closet.outfit", "athena-beach", map[string]any{"persona": "athena", "outfit": "beach", "name": "沙灘", "cost": 1, "state": "active", "source": "mine"})
	photo := func(id, typ, file string, hidden bool, extra map[string]any) {
		f := map[string]any{"persona": "athena", "outfit": "athena-beach", "type": typ, "order": 1, "hidden": hidden, "source": "mine", "file": file, "adopted": true}
		for k, x := range extra {
			f[k] = x
		}
		v.seed("closet.photo", id, f)
	}
	photo("athena-beach--u1", "half", "photos/athena-beach--u1.png", false, nil)
	photo("athena-beach--u2", "video", "photos/athena-beach--u2.mp4", false, map[string]any{"of": "athena-beach--u1", "clip": 1})
	photo("athena-beach--u3", "half", "photos/athena-beach--u3.png", true, nil)
	fv := &failVault{promoteFake: v, failPhotoPut: 2}
	opts := Options{Home: h, Target: "athena-beach", Vault: fv}
	if _, err := Run(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "接著做") {
		t.Fatalf("照片紀錄改到一半：%v", err)
	}
	res, err := Run(context.Background(), opts)
	if err != nil || !res.Resumed {
		t.Fatalf("重跑要接著做：%+v %v", res, err)
	}
	for id, want := range map[string]map[string]any{
		"athena-beach--u1": {"source": "official", "file": "avatars/photos/athena-beach--u1.png"},
		"athena-beach--u2": {"source": "official", "file": "avatars/photos/athena-beach--u2.mp4", "of": "athena-beach--u1"},
		"athena-beach--u3": {"source": "mine", "file": "photos/athena-beach--u3.png", "hidden": true},
	} {
		r, _ := v.Get(context.Background(), "closet.photo", id)
		for k, x := range want {
			if r.Fields[k] != x {
				t.Fatalf("%s 的 %s = %v，要 %v", id, k, r.Fields[k], x)
			}
		}
	}
	bak := ".bak-" + time.Now().Format("20060102") + "-promote"
	if readRel(h, "personas/avatars/photos/athena-beach--u1.png") != "mine athena-beach--u1.png" || readRel(h, "personas/mine/photos/athena-beach--u1.png"+bak) == "" {
		t.Fatal("搬到官方的 avatars/photos/、舊的改名成備份")
	}
	if readRel(h, "personas/mine/photos/athena-beach--u3.png") == "" || readRel(h, "personas/avatars/photos/athena-beach--u3.png") != "" {
		t.Fatal("隱藏版留在原地")
	}

	// 換編號升官方時的退回步驟：file 改回 photos/。
	h2, v2 := promoteHome(t)
	if err := os.MkdirAll(filepath.Join(h2, "personas", "mine", "photos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h2, "personas", "mine", "photos", "athena-my-12--u7.png"), []byte("u7"), 0o644); err != nil {
		t.Fatal(err)
	}
	v2.seed("closet.photo", "athena-my-12--u7", map[string]any{"persona": "athena", "outfit": "athena-my-12", "type": "half", "order": 3, "hidden": false,
		"source": "mine", "file": "photos/athena-my-12--u7.png", "adopted": true})
	res, err = Run(context.Background(), Options{Home: h2, Target: "athena-my-12", NewID: "seaside", Vault: v2})
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := v2.Get(context.Background(), "closet.photo", "athena-seaside--u7"); r == nil || r.Fields["file"] != "avatars/photos/athena-my-12--u7.png" {
		t.Fatalf("換編號：%v", r)
	}
	if undo := strings.Join(res.Undo, "\n"); !strings.Contains(undo, `data set closet.photo athena-seaside--u7 --json '{"source":"mine","file":"photos/athena-my-12--u7.png"}'`) {
		t.Fatalf("退回要把 file 改回去：\n%s", undo)
	}
}

// 主人只有新上傳的照片（沒有 personas/mine/avatars/）也能升；personas/mine/photos 是捷徑就不做。
func TestPromoteWithoutMineAvatarsFolder(t *testing.T) {
	h, v := promoteHome(t)
	if err := os.RemoveAll(filepath.Join(h, "personas", "mine", "avatars")); err != nil {
		t.Fatal(err)
	}
	for id := range v.recs {
		if strings.HasPrefix(id, "closet.photo/") {
			delete(v.recs, id)
		}
	}
	if err := os.MkdirAll(filepath.Join(h, "personas", "mine", "photos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h, "personas", "mine", "photos", "athena-my-12--u1.png"), []byte("u1"), 0o644); err != nil {
		t.Fatal(err)
	}
	v.seed("closet.photo", "athena-my-12--u1", map[string]any{"persona": "athena", "outfit": "athena-my-12", "type": "half", "order": 1, "hidden": false,
		"source": "mine", "file": "photos/athena-my-12--u1.png", "adopted": true})
	if _, err := Run(context.Background(), Options{Home: h, Target: "athena-my-12", NewID: "seaside", Vault: v, DryRun: true}); err != nil {
		t.Fatalf("沒有 avatars/ 也能升：%v", err)
	}
	outside := t.TempDir()
	if err := os.RemoveAll(filepath.Join(h, "personas", "mine", "photos")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(h, "personas", "mine", "photos")); err != nil {
		t.Fatal(err)
	}
	var rj *Rejected
	if _, err := Run(context.Background(), Options{Home: h, Target: "athena-my-12", NewID: "seaside", Vault: v, DryRun: true}); !errors.As(err, &rj) {
		t.Fatalf("photos/ 是捷徑要拒絕：%v", err)
	}
}
