package run

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/minglight/evergenie/packages/official/closet-mine/internal/rules"
)

// promoteScene：athena 有一套採用了的我的衣服 my-7（含輪播照）。
func promoteScene(t *testing.T, dev bool) *scene {
	sc := newScene(t)
	sc.write("personas/mine/avatars/athena@my-7.png", png("mine-7"))
	sc.v.seed(kindOutfit, "athena-my-7", map[string]any{"persona": "athena", "outfit": "my-7", "name": "海邊", "cost": 1, "state": "active", "source": "mine"})
	sc.v.seed(kindPhoto, "athena-my-7--half1", map[string]any{"persona": "athena", "outfit": "athena-my-7", "type": "half", "order": 1, "hidden": false, "source": "mine", "file": "avatars/athena@my-7.png", "adopted": true})
	sc.svc.Dev = func(context.Context) (bool, error) { return dev, nil }
	return sc
}

// 開發家、主人本人送的：圖搬到官方位置、目錄新增官方的一筆（from 指回舊的）、舊的封存；請求 done，detail 寫搬了什麼、怎麼退回。
func TestPromoteRequestOnDevByOwner(t *testing.T) {
	sc := promoteScene(t, true)
	id := sc.request(map[string]any{"type": "promote", "outfit": "athena-my-7", "new_id": "seaside", "by": "owner(web)"})
	sc.round()
	f := sc.fields(KindRequest, id)
	if f["state"] != "done" || f["result"] != "athena-seaside" || !strings.Contains(rules.Text(f["detail"]), "要退回") {
		t.Fatalf("請求：%v", f)
	}
	if !bytes.Equal(sc.read("personas/avatars/athena@my-7.png"), png("mine-7")) || sc.read("personas/mine/avatars/athena@my-7.png") != nil {
		t.Fatal("圖要搬到官方位置、舊的改名成備份")
	}
	n := sc.fields(kindOutfit, "athena-seaside")
	if n["source"] != "official" || n["from"] != "athena-my-7" {
		t.Fatalf("新的那筆：%v", n)
	}
	if old, _ := sc.v.get(kindOutfit, "athena-my-7"); !old.Archived {
		t.Fatal("舊的封存")
	}
}

// 預演：done、detail 說沒動，什麼檔與紀錄都不變。
func TestPromoteRequestDryRun(t *testing.T) {
	sc := promoteScene(t, true)
	id := sc.request(map[string]any{"type": "promote", "outfit": "athena-my-7", "new_id": "seaside", "dry_run": true, "by": "owner(web)"})
	sc.round()
	if f := sc.fields(KindRequest, id); f["state"] != "done" || !strings.Contains(rules.Text(f["detail"]), "預演") {
		t.Fatalf("請求：%v", f)
	}
	if sc.read("personas/avatars/athena@my-7.png") != nil || sc.read("personas/mine/avatars/athena@my-7.png") == nil {
		t.Fatal("預演不能動檔案")
	}
	if _, ok := sc.v.get(kindOutfit, "athena-seaside"); ok {
		t.Fatal("預演不能寫紀錄")
	}
}

// 不做的情況：正式家（這要到開發家做）、秘書送的、沒寫哪一套、沒有照片紀錄（或全是隱藏版）、套件裡的白話拒絕（還要 new_id）。
func TestPromoteRequestRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		dev    bool
		fields map[string]any
		want   string
	}{
		"正式家":    {false, map[string]any{"outfit": "athena-my-7", "new_id": "seaside", "by": "owner(web)"}, "這要到開發家做"},
		"秘書送的":   {true, map[string]any{"outfit": "athena-my-7", "new_id": "seaside", "by": "code"}, rules.ReasonNotOwnerPromote},
		"沒寫哪一套":  {true, map[string]any{"by": "owner(cli)"}, rules.ReasonNeedTarget},
		"沒有照片紀錄": {true, map[string]any{"outfit": "athena-my-8", "new_id": "eight", "by": "owner(web)"}, "沒有可以升的照片"},
		"沒給新 id": {true, map[string]any{"outfit": "athena-my-7", "by": "owner(web)"}, "new_id"},
	} {
		t.Run(name, func(t *testing.T) {
			sc := promoteScene(t, tc.dev)
			sc.v.seed(kindOutfit, "athena-my-8", map[string]any{"persona": "athena", "outfit": "my-8", "name": "沒登記", "cost": 0, "state": "active", "source": "mine"})
			f := cloneMap(tc.fields)
			f["type"] = "promote"
			id := sc.request(f)
			sc.round()
			got := sc.fields(KindRequest, id)
			if got["state"] != "rejected" || !strings.Contains(rules.Text(got["reason"]), tc.want) {
				t.Fatalf("要拒絕（%s）：%v", tc.want, got)
			}
			if sc.read("personas/mine/avatars/athena@my-7.png") == nil || sc.read("personas/avatars/athena@my-7.png") != nil {
				t.Fatal("被拒時不能動檔案")
			}
		})
	}
}

// 自己的隱藏版被封存或資料夾被改：放自己的隱藏版圖的請求回 rejected、說怎麼恢復，不寫檔；另有一筆 mine/adult 的隱藏版就照常。
func TestAdultUploadRejectedWhenVariantGone(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(sc *scene)
		ok    bool
	}{
		"被封存": {func(sc *scene) {
			sc.v.seed(kindVariant, adultVariantID, map[string]any{"name": "自己的隱藏版", "dir": "mine/adult"})
			_ = sc.v.Archive(context.Background(), kindVariant, adultVariantID)
		}, false},
		"資料夾被改": {func(sc *scene) {
			sc.v.seed(kindVariant, adultVariantID, map[string]any{"name": "自己的隱藏版", "dir": "mine/other"})
		}, false},
		"封存了但另有一筆": {func(sc *scene) {
			sc.v.seed(kindVariant, adultVariantID, map[string]any{"name": "自己的隱藏版", "dir": "mine/adult"})
			_ = sc.v.Archive(context.Background(), kindVariant, adultVariantID)
			sc.v.seed(kindVariant, "adult-2", map[string]any{"name": "自己的隱藏版", "dir": "mine/adult"})
		}, true},
	} {
		t.Run(name, func(t *testing.T) {
			sc := newScene(t)
			sc.svc.Adult = func(context.Context) bool { return true }
			tc.setup(sc)
			sc.upload("a.png", png("adult-1"))
			id := sc.request(map[string]any{"type": "photo", "upload": "a.png", "persona": "athena", "outfit": "base", "adult": true})
			sc.round()
			f := sc.fields(KindRequest, id)
			if tc.ok {
				if f["state"] != "done" {
					t.Fatalf("另有一筆 mine/adult 要照常：%v", f)
				}
				return
			}
			if f["state"] != "rejected" || f["reason"] != rules.ReasonAdultVariantGone || sc.read("personas/mine/adult/athena.png") != nil {
				t.Fatalf("要拒絕並說怎麼恢復：%v", f)
			}
		})
	}
}
