package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// home tidy、personas promote 的指令（DEV-154 第 10、13 件）：家在 t.TempDir()（第 1 版佈局，暫存在家裡），
// 套件宣告讀 repo 裡真的官方套件（衣櫃、自己加的衣服），確認衣服、解鎖、輪替、便條…都照 package.toml 的宣告跟著改。

const tidyHomeTOML = `[user]
name = "h"
timezone = "Asia/Taipei"

[slots]
default = "life"

[slots.life]
label = "生活"
remind = "08:30"
check = "18:00"

[accounts]
default = "main"

[accounts.main]
engine = "test-engine"

[web]
port = 18850
`

func tidyWrite(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func tidyTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			out[rel] = "dir"
			return nil
		}
		data, _ := os.ReadFile(p)
		h := sha256.Sum256(data)
		out[rel] = hex.EncodeToString(h[:])
		return nil
	})
	return out
}

func tidyClosetHome(t *testing.T, runtimeEnv, persona string) string {
	t.Helper()
	t.Setenv("EVERGENIE_OFFICIAL_PACKAGES_DIR", filepath.Join("..", "..", "packages", "official"))
	home := filepath.Join(t.TempDir(), "home")
	conf := tidyHomeTOML
	if runtimeEnv != "" {
		conf += "\n[runtime]\nenv = \"" + runtimeEnv + "\"\n"
	}
	p := persona
	tidyWrite(t, home, map[string]string{
		"home.toml":                                    conf,
		"personas/luna.md":                             "官方",
		"personas/mine/" + p + ".md":                   "---\nname: Mika\n---\n",
		"personas/mine/avatars/" + p + ".png":          "y",
		"personas/mine/avatars/" + p + "~a.png":        "a",
		"vault/agents/life/agent.md":                   "---\nid: life\npersona: " + p + "\n---\n",
		"vault/pkg/closet/outfit/" + p + "-a.md":       "---\nid: " + p + "-a\npersona: " + p + "\nsource: mine\n---\n",
		"vault/pkg/closet/unlock/" + p + "-a.md":       "---\nid: " + p + "-a\npersona: " + p + "\n---\n",
		"vault/pkg/closet/rotation/" + p + ".md":       "---\nid: " + p + "\n---\n",
		"vault/pkg/closet/person/" + p + ".md":         "---\nid: " + p + "\npersona: " + p + "\n---\n",
		"vault/pkg/closet/note/CN-1.md":                "---\nid: CN-1\npersona: " + p + "\nrecord: " + p + "-a\nphoto: " + p + "-a--half1\n---\n",
		"vault/pkg/closet/request/Q-1.md":              "---\nid: Q-1\npersona: " + p + "\nunlock: " + p + "-a\n---\n",
		"vault/pkg/closet-mine/request/U-1.md":         "---\nid: U-1\npersona: " + p + "\nresult: " + p + "-a\n---\n",
		"vault/pkg/closet/outfit/luna-b.md":            "---\nid: luna-b\npersona: luna\nsource: official\n---\n",
		"vault/pkg/closet/photo/" + p + "-a--half1.md": photoRecord(p, "avatars/"+p+"~a.png"),
	})
	return home
}

// photoRecord 是衣櫃的照片紀錄（DEV-160）：代號 n 的 a 那套第 1 張，file 指著 file（改代號時不改名）。
func photoRecord(n, file string) string {
	return "---\nid: " + n + "-a--half1\npersona: " + n + "\noutfit: " + n + "-a\ntype: half\norder: 1\nhidden: false\nadopted: true\nsource: mine\nfile: " + file + "\n---\n"
}

// 衣櫃與自己加的衣服的紀錄，代號 old → new 之後的樣子。
func checkClosetRecords(t *testing.T, home, n string) {
	t.Helper()
	for rel, want := range map[string]string{
		"vault/pkg/closet/outfit/" + n + "-a.md": "---\nid: " + n + "-a\npersona: " + n + "\nsource: mine\n---\n",
		"vault/pkg/closet/unlock/" + n + "-a.md": "---\nid: " + n + "-a\npersona: " + n + "\n---\n",
		"vault/pkg/closet/rotation/" + n + ".md": "---\nid: " + n + "\n---\n",
		"vault/pkg/closet/person/" + n + ".md":   "---\nid: " + n + "\npersona: " + n + "\n---\n",
		// 意見指著的照片編號（closet.note 的 photo，persona_prefixed）也跟著換代號，評論紀錄不會斷（DEV-160）。
		"vault/pkg/closet/note/CN-1.md":        "---\nid: CN-1\npersona: " + n + "\nrecord: " + n + "-a\nphoto: " + n + "-a--half1\n---\n",
		"vault/pkg/closet/request/Q-1.md":      "---\nid: Q-1\npersona: " + n + "\nunlock: " + n + "-a\n---\n",
		"vault/pkg/closet-mine/request/U-1.md": "---\nid: U-1\npersona: " + n + "\nresult: " + n + "-a\n---\n",
		"vault/pkg/closet/outfit/luna-b.md":    "---\nid: luna-b\npersona: luna\nsource: official\n---\n",
		"vault/agents/life/agent.md":           "---\nid: life\npersona: " + n + "\n---\n",
	} {
		data, err := os.ReadFile(filepath.Join(home, rel))
		if err != nil || string(data) != want {
			t.Errorf("%s：要是 %q，得到 %q（%v）", rel, want, data, err)
		}
	}
}

func TestHomeTidyCommandWithRealCloset(t *testing.T) {
	home := tidyClosetHome(t, "", "mika")
	before := tidyTree(t, home)
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	var out, errw bytes.Buffer
	if code := homeTidy("", []string{"--home", home}, &out, &errw, now); code != 0 {
		t.Fatalf("預演 code=%d err=%s out=%s", code, errw.String(), out.String())
	}
	for _, want := range []string{"預演", "mika → my_mika", "套件紀錄 8 筆", "名冊 1 個職位（life）", "luna", "--apply"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("預演要有 %q：\n%s", want, out.String())
		}
	}
	if !reflect.DeepEqual(before, tidyTree(t, home)) {
		t.Fatal("預演不能動檔")
	}
	out.Reset()
	if code := homeTidy("", []string{"--home", home, "--apply"}, &out, &errw, now); code != 0 {
		t.Fatalf("套用 code=%d err=%s", code, errw.String())
	}
	if !strings.Contains(out.String(), "--revert --apply") {
		t.Errorf("套用後要說怎麼退回：\n%s", out.String())
	}
	checkClosetRecords(t, home, "my_mika")
	// 圖不改名（DEV-160）：照片紀錄換成新代號，file 照舊指著原檔。
	if got, _ := os.ReadFile(filepath.Join(home, "vault/pkg/closet/photo/my_mika-a--half1.md")); string(got) != photoRecord("my_mika", "avatars/mika~a.png") {
		t.Errorf("照片紀錄：%q", got)
	}
	for _, rel := range []string{"personas/mine/my_mika.md", "personas/mine/avatars/mika.png", "personas/mine/avatars/mika~a.png", "settings/home-tidy.json"} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Errorf("要有 %s", rel)
		}
	}
	out.Reset()
	if code := homeTidy("", []string{"--home", home}, &out, &errw, now); code != 0 || !strings.Contains(out.String(), "沒有要做的") {
		t.Errorf("整理完再預演要沒事做：code=%d\n%s", code, out.String())
	}
	out.Reset()
	if code := homeTidy("", []string{"--home", home, "--revert", "--json"}, &out, &errw, now); code != 0 || !strings.Contains(out.String(), `"applied": false`) {
		t.Errorf("退回預演（JSON）：code=%d\n%s", code, out.String())
	}
	if code := homeTidy("", []string{"--home", home, "--revert", "--apply"}, &out, &errw, now); code != 0 {
		t.Fatalf("退回 code=%d err=%s", code, errw.String())
	}
	if after := tidyTree(t, home); !reflect.DeepEqual(before, after) {
		t.Errorf("退回後要跟之前逐檔相同：\n之前 %v\n之後 %v", before, after)
	}

	// 用法錯誤、服務在跑。
	if code := homeTidy("", []string{"--home", home, "--rename", "mika"}, &out, &errw, now); code != 2 {
		t.Errorf("--rename 沒有 = 要是用法錯誤：%d", code)
	}
	tidyWrite(t, home, map[string]string{"runtime/serve.pid": strconv.Itoa(os.Getpid())})
	errw.Reset()
	out.Reset()
	if code := homeTidy("", []string{"--home", home, "--apply"}, &out, &errw, now); code != 1 || !strings.Contains(out.String(), "服務還在跑") {
		t.Errorf("服務在跑要擋下：code=%d\n%s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(home, "personas/mine/mika.md")); err != nil {
		t.Error("服務在跑時什麼都不能動")
	}
}

func TestPersonasPromoteCommand(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	var out, errw bytes.Buffer

	prod := tidyClosetHome(t, "", "my_mika")
	before := tidyTree(t, prod)
	if code := personasPromote("", "owner(cli)", []string{"--home", prod, "my_mika", "--apply"}, &out, &errw, now); code != 1 || !strings.Contains(errw.String(), "開發家") {
		t.Errorf("不是開發家要拒絕：code=%d err=%s", code, errw.String())
	}
	if !reflect.DeepEqual(before, tidyTree(t, prod)) {
		t.Error("拒絕時什麼都不能動")
	}

	dev := tidyClosetHome(t, "dev", "my_mika")
	before = tidyTree(t, dev)
	// 秘書的 session 也能做（離線指令，主人只透過秘書做事；代跑前要主人點頭由共用規則管）。先預演。
	errw.Reset()
	if code := personasPromote("", "admin", []string{"--home", dev, "my_mika"}, &out, &errw, now); code != 0 {
		t.Errorf("秘書可以預演：code=%d err=%s", code, errw.String())
	}
	if code := personasPromote("", "owner(cli)", []string{"--home", dev, "../x", "--revert"}, &out, &errw, now); code != 1 {
		t.Errorf("退回的代號要是 my_ 開頭：code=%d", code)
	}
	if !reflect.DeepEqual(before, tidyTree(t, dev)) {
		t.Error("擋下與預演時什麼都不能動")
	}
	out.Reset()
	if code := personasPromote("", "owner(cli)", []string{"--home", dev, "my_mika"}, &out, &errw, now); code != 0 {
		t.Fatalf("預演 code=%d err=%s", code, errw.String())
	}
	for _, want := range []string{"my_mika → mika", "展示間", "public_personas", "--apply"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("預演要有 %q：\n%s", want, out.String())
		}
	}
	if code := personasPromote("", "admin", []string{"--home", dev, "my_mika", "--apply"}, &out, &errw, now); code != 0 {
		t.Fatalf("套用 code=%d err=%s", code, errw.String())
	}
	checkClosetRecords(t, dev, "mika")
	if got, _ := os.ReadFile(filepath.Join(dev, "vault/pkg/closet/photo/mika-a--half1.md")); string(got) != photoRecord("mika", "avatars/my_mika~a.png") {
		t.Errorf("照片紀錄跟著新代號、file 不變：%q", got)
	}
	for _, rel := range []string{"personas/mika.md", "personas/mine/avatars/my_mika.png", "personas/mine/avatars/my_mika~a.png", "settings/persona-promote/my_mika.json"} {
		if _, err := os.Stat(filepath.Join(dev, rel)); err != nil {
			t.Errorf("要有 %s", rel)
		}
	}
	if code := personasPromote("", "owner(cli)", []string{"--home", dev, "my_mika", "--revert", "--apply"}, &out, &errw, now); code != 0 {
		t.Fatalf("退回 code=%d err=%s", code, errw.String())
	}
	if after := tidyTree(t, dev); !reflect.DeepEqual(before, after) {
		t.Errorf("退回後要跟之前逐檔相同：\n之前 %v\n之後 %v", before, after)
	}

	// 再升一次，這次確定不退：--forget 把紀錄改名成備份，之後不能 --revert。
	if code := personasPromote("", "owner(cli)", []string{"--home", dev, "my_mika", "--apply"}, &out, &errw, now); code != 0 {
		t.Fatalf("再升 code=%d err=%s", code, errw.String())
	}
	if code := personasPromote("", "owner(cli)", []string{"--home", dev, "my_mika", "--revert", "--forget"}, &out, &errw, now); code != 2 {
		t.Errorf("--revert 與 --forget 只能選一個：code=%d", code)
	}
	out.Reset()
	if code := personasPromote("", "owner(cli)", []string{"--home", dev, "my_mika", "--forget", "--apply"}, &out, &errw, now); code != 0 || !strings.Contains(out.String(), "收尾好了") {
		t.Fatalf("收尾 code=%d out=%s err=%s", code, out.String(), errw.String())
	}
	if _, err := os.Stat(filepath.Join(dev, "settings/persona-promote/my_mika.json.bak-20261113-done")); err != nil {
		t.Error("紀錄要改名成 .bak-<日期>-done")
	}
	errw.Reset()
	if code := personasPromote("", "owner(cli)", []string{"--home", dev, "my_mika", "--revert"}, &out, &errw, now); code != 1 || !strings.Contains(errw.String(), "找不到紀錄檔") {
		t.Errorf("收尾後不能退回：code=%d err=%s", code, errw.String())
	}
}
