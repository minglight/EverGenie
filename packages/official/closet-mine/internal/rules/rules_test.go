package rules

import (
	"strings"
	"testing"
)

func png(payload string) []byte  { return append([]byte("\x89PNG\r\n\x1a\n"), payload...) }
func jpeg(payload string) []byte { return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, payload...) }
func webp(payload string) []byte { return append([]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), payload...) }
func mp4(payload string) []byte {
	return append([]byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00"), payload...)
}

// TestSniffTypes：看檔案開頭認類型，完全不看檔名；假副檔名的文字檔不是圖。
// Guards: REQ-126
func TestSniffTypes(t *testing.T) {
	for name, tc := range map[string]struct {
		data []byte
		want FileKind
		ext  string
	}{
		"png":           {png("x"), KindPNG, "png"},
		"jpeg":          {jpeg("x"), KindJPEG, "jpg"},
		"webp":          {webp("x"), KindWEBP, "webp"},
		"mp4":           {mp4("x"), KindMP4, "mp4"},
		"文字檔假裝成 png":    {[]byte("this is not a picture.png"), KindUnknown, ""},
		"空檔":            {nil, KindUnknown, ""},
		"太短的 PNG 開頭":    {[]byte("\x89PNG"), KindUnknown, ""},
		"RIFF 但不是 WEBP": {[]byte("RIFF\x10\x00\x00\x00WAVEfmt "), KindUnknown, ""},
		"GIF":           {[]byte("GIF89a......"), KindUnknown, ""},
		"HTML":          {[]byte("<html><script>alert(1)</script>"), KindUnknown, ""},
	} {
		got := Sniff(tc.data)
		if got != tc.want || got.Ext() != tc.ext {
			t.Errorf("%s：Sniff = %v（ext %q），要 %v（%q）", name, got, got.Ext(), tc.want, tc.ext)
		}
	}
	// 類型檢查：video 只收 MP4，其他只收圖。
	for _, tc := range []struct {
		typ  string
		kind FileKind
		want string
	}{
		{TypeVideo, KindMP4, ""}, {TypeVideo, KindPNG, ReasonVideoOnly}, {TypeVideo, KindUnknown, ReasonVideoOnly},
		{TypeCharacter, KindPNG, ""}, {TypeOutfit, KindWEBP, ""}, {TypePhoto, KindJPEG, ""}, {TypeFull, KindPNG, ""},
		{TypeCharacter, KindMP4, ReasonImageOnly}, {TypePhoto, KindUnknown, ReasonImageOnly}, {TypeFull, KindMP4, ReasonImageOnly},
	} {
		if got := CheckKind(tc.typ, tc.kind); got != tc.want {
			t.Errorf("CheckKind(%s, %v) = %q，要 %q", tc.typ, tc.kind, got, tc.want)
		}
	}
}

// TestCodesAndIDs：自訂代號 my 開頭小寫英數；沒給用 mine<編號>、衣服 id my-<編號>（可重算）；上傳檔名只能是單一檔名。
// Guards: REQ-126
func TestCodesAndIDs(t *testing.T) {
	for in, want := range map[string]bool{
		"myhelper": true, "my1": true, "myabcdefghij0123456789": true, "my": false, "myA": false, "helper": false,
		"my-x": false, "": false, "mine12": false, "my_kaguya": true, "my_": false, "my_Bad": false, "my" + strings.Repeat("a", 20): true, "my" + strings.Repeat("a", 21): false,
	} {
		if got := ValidMineCode(in); got != want {
			t.Errorf("ValidMineCode(%q) = %v", in, got)
		}
	}
	if c, ok := DefaultCode("U-12"); !ok || c != "my_12" {
		t.Errorf("DefaultCode：%q %v", c, ok)
	}
	if id, ok := OutfitIDFor("U-12"); !ok || id != "my-12" {
		t.Errorf("OutfitIDFor：%q %v", id, ok)
	}
	for _, bad := range []string{"", "Q-1", "U-", "U-x", "U--1", "U-1234567890"} {
		if _, ok := DefaultCode(bad); ok {
			t.Errorf("DefaultCode(%q) 不該成功", bad)
		}
	}
	// 衣服編號與照片編號（DEV-160 S3：照片編號是 <衣服編號>--u<請求編號>，不從種類、第幾張推）。
	if RecordID("myhelper", "my-3") != "myhelper-my-3" {
		t.Error("衣服編號")
	}
	if id, ok := PhotoIDFor("athena-my-12", "U-15"); !ok || id != "athena-my-12--u15" || !ValidPhotoID(id) {
		t.Errorf("PhotoIDFor：%q %v", id, ok)
	}
	if _, ok := PhotoIDFor("athena-my-12", "X-1"); ok {
		t.Error("PhotoIDFor 不認得的請求編號")
	}
	// 上傳檔名。
	for in, want := range map[string]string{"/share/a.png": "a.png", "a.png": "a.png", "/share/我的照片.png": "我的照片.png", " /share/x.png ": "x.png"} {
		if got, ok := UploadName(in); !ok || got != want {
			t.Errorf("UploadName(%q) = %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "/share/", "/share/../etc/passwd", "../a.png", "a/b.png", `a\b.png`, ".hidden", "/share/.hidden", "a\x00b", "a..b.png", "/share/sub/a.png"} {
		if got, ok := UploadName(bad); ok {
			t.Errorf("UploadName(%q) = %q，不該通過", bad, got)
		}
	}
}

func req(typ string, f map[string]any) Request {
	if f == nil {
		f = map[string]any{}
	}
	f["type"] = typ
	if f["upload"] == nil {
		f["upload"] = "/share/a.png"
	}
	return ParseRequest("U-12", f)
}

// TestRequestRules：第 5 點的每個拒絕原因＋通過。
// Guards: REQ-126
func TestRequestRules(t *testing.T) {
	long := strings.Repeat("字", 41)
	for name, tc := range map[string]struct {
		r    Request
		want string
	}{
		"character 通過":      {req("character", map[string]any{"name": "小狐"}), ""},
		"character 帶代號":     {req("character", map[string]any{"name": "小狐", "persona": "myhelper"}), ""},
		"character 沒名字":     {req("character", nil), ReasonNeedName},
		"character 名字 40 字": {req("character", map[string]any{"name": strings.Repeat("字", 40)}), ""},
		"character 名字 41 字": {req("character", map[string]any{"name": long}), ReasonNameTooLong},
		"character 壞代號":     {req("character", map[string]any{"name": "x", "persona": "helper"}), ReasonBadCode},
		"沒有上傳":              {req("character", map[string]any{"name": "x", "upload": ""}), ReasonNoUpload},
		"上傳檔名有 ..":          {req("character", map[string]any{"name": "x", "upload": "/share/../x.png"}), ReasonNoUpload},
		"outfit 通過":         {req("outfit", map[string]any{"persona": "athena", "name": "海邊"}), ""},
		"outfit 沒角色":        {req("outfit", map[string]any{"name": "海邊"}), ReasonNeedPersona},
		"outfit 角色格式不對":     {req("outfit", map[string]any{"persona": "../x", "name": "海邊"}), ReasonNoPersona},
		"outfit 沒衣服名":       {req("outfit", map[string]any{"persona": "athena"}), ReasonNeedName},
		"photo 通過":          {req("photo", map[string]any{"persona": "athena", "outfit": "my-3"}), ""},
		"photo 沒衣服":         {req("photo", map[string]any{"persona": "athena"}), ReasonNeedOutfit},
		"photo 衣服格式不對":      {req("photo", map[string]any{"persona": "athena", "outfit": "Bad!"}), ReasonNoOutfit},
		"full 通過":           {req("full", map[string]any{"persona": "athena", "outfit": "my-3"}), ""},
		"video 通過（預設第 1 張）": {req("video", map[string]any{"persona": "athena", "outfit": "my-3"}), ""},
		"video 第 0 張":       {req("video", map[string]any{"persona": "athena", "outfit": "my-3", "photo": 0}), ReasonNoPhoto(0)},
		"video 第 7 支":       {req("video", map[string]any{"persona": "athena", "outfit": "my-3", "clip": 7}), ReasonNoClip(7)},
		"video 第 0 支":       {req("video", map[string]any{"persona": "athena", "outfit": "my-3", "clip": 0}), ReasonNoClip(0)},
		"video 第 3 支格式通過":   {req("video", map[string]any{"persona": "athena", "outfit": "my-3", "clip": 3}), ""},
		"不認得的種類":            {req("dance", nil), ReasonUnknownType},
		"photo 換一張":         {req("photo", map[string]any{"persona": "athena", "outfit": "my-3", "photo_id": "athena-my-3--u4"}), ""},
		"photo 照片編號不對":      {req("photo", map[string]any{"persona": "athena", "outfit": "my-3", "photo_id": "Bad/x"}), ReasonNoPhotoRecord},
	} {
		if got := Validate(tc.r); got != tc.want {
			t.Errorf("%s：Validate = %q，要 %q", name, got, tc.want)
		}
	}
	// 整理用的請求不帶上傳檔（DEV-160 S3）。
	plain := func(typ string, f map[string]any) Request { f["type"] = typ; return ParseRequest("U-12", f) }
	for name, tc := range map[string]struct {
		r    Request
		want string
	}{
		"刪單張通過":     {plain(TypeRemovePhoto, map[string]any{"photo_id": "athena-my-3--u4"}), ""},
		"拿回單張通過":    {plain(TypeRestorePhoto, map[string]any{"photo_id": "athena-my-3--half2", "persona": "athena"}), ""},
		"刪單張沒寫哪一張":  {plain(TypeRemovePhoto, map[string]any{}), ReasonNeedPhotoID},
		"刪單張編號不對":   {plain(TypeRemovePhoto, map[string]any{"photo_id": "../x"}), ReasonNoPhotoRecord},
		"刪一套通過":     {plain(TypeRemoveOutfit, map[string]any{"persona": "athena", "outfit": "my-3"}), ""},
		"刪一套不能刪基本款": {plain(TypeRemoveOutfit, map[string]any{"persona": "my_fox", "outfit": "base"}), ReasonBaseOutfit},
		"拿回一套通過":    {plain(TypeRestoreOutfit, map[string]any{"persona": "athena", "outfit": "my-3"}), ""},
		"拿回一套沒寫衣服":  {plain(TypeRestoreOutfit, map[string]any{"persona": "athena"}), ReasonNeedOutfit},
		"改點數通過":     {plain(TypeSetCost, map[string]any{"persona": "athena", "outfit": "my-3", "cost": 0}), ""},
		"改點數沒寫幾點":   {plain(TypeSetCost, map[string]any{"persona": "athena", "outfit": "my-3"}), ReasonBadCost},
		"改點數超過 99":  {plain(TypeSetCost, map[string]any{"persona": "athena", "outfit": "my-3", "cost": 100}), ReasonBadCost},
		"基本款不能改點數":  {plain(TypeSetCost, map[string]any{"persona": "my_fox", "outfit": "base", "cost": 2}), ReasonBaseCost},
		"改點數沒寫角色":   {plain(TypeSetCost, map[string]any{"outfit": "my-3", "cost": 2}), ReasonNeedPersona},
	} {
		if got := Validate(tc.r); got != tc.want {
			t.Errorf("%s：Validate = %q，要 %q", name, got, tc.want)
		}
	}
	for _, typ := range []string{TypeRemovePhoto, TypeRestorePhoto, TypeRemoveOutfit, TypeRestoreOutfit, TypeSetCost, TypePromote} {
		if NeedsUpload(typ) {
			t.Errorf("%s 不帶上傳檔", typ)
		}
	}
	if r := req("video", map[string]any{"persona": "a", "outfit": "b", "photo": float64(3)}); r.Photo != 3 {
		t.Errorf("photo 欄位：%d", r.Photo)
	}
	if r := req("video", map[string]any{"persona": "a", "outfit": "b"}); r.Clip != 1 {
		t.Errorf("video 預設第 1 支：%d", r.Clip)
	}
	if r := req("video", map[string]any{"persona": "a", "outfit": "b"}); r.Photo != 1 {
		t.Errorf("video 預設第 1 張：%d", r.Photo)
	}
	if c := CharacterCode(req("character", map[string]any{"name": "x"})); c != "my_12" {
		t.Errorf("預設代號 %q", c)
	}
	if c := CharacterCode(req("character", map[string]any{"name": "x", "persona": "myfox"})); c != "myfox" {
		t.Errorf("自訂代號 %q", c)
	}

	// 現況：代號被用、找不到角色、官方衣服、張數上限、沒有第 N 張。
	for name, tc := range map[string]struct {
		r    Request
		f    Facts
		want string
	}{
		"代號被用":           {req("character", map[string]any{"name": "x"}), Facts{CodeTaken: true}, ReasonCodeTaken},
		"代號可用":           {req("character", map[string]any{"name": "x"}), Facts{}, ""},
		"outfit 找不到角色":   {req("outfit", map[string]any{"persona": "ghost", "name": "x"}), Facts{}, ReasonNoPersona},
		"outfit 通過":      {req("outfit", map[string]any{"persona": "athena", "name": "x"}), Facts{PersonaExists: true}, ""},
		"outfit 編號被用":    {req("outfit", map[string]any{"persona": "athena", "name": "x"}), Facts{PersonaExists: true, OutfitTaken: true}, ReasonOutfitTaken},
		"photo 沒這套":      {req("photo", map[string]any{"persona": "a", "outfit": "b"}), Facts{}, ReasonNoOutfit},
		"photo 官方衣服":     {req("photo", map[string]any{"persona": "a", "outfit": "b"}), Facts{OutfitExists: true, OutfitSource: "official"}, ReasonOfficial},
		"full 官方衣服":      {req("full", map[string]any{"persona": "a", "outfit": "b"}), Facts{OutfitExists: true, OutfitSource: "official"}, ReasonOfficial},
		"video 官方衣服":     {req("video", map[string]any{"persona": "a", "outfit": "b"}), Facts{OutfitExists: true, OutfitSource: "official", PhotoCount: 1}, ReasonOfficial},
		"photo 11 張通過":   {req("photo", map[string]any{"persona": "a", "outfit": "b"}), Facts{OutfitExists: true, OutfitSource: "mine", PhotoCount: 11}, ""},
		"photo 12 張":     {req("photo", map[string]any{"persona": "a", "outfit": "b"}), Facts{OutfitExists: true, OutfitSource: "mine", PhotoCount: 12}, ReasonTooManyPhoto},
		"photo 12 張但重複":  {req("photo", map[string]any{"persona": "a", "outfit": "b"}), Facts{OutfitExists: true, OutfitSource: "mine", PhotoCount: 12, DuplicatePhoto: true}, ""},
		"photo 12 張換一張":  {req("photo", map[string]any{"persona": "a", "outfit": "b"}), Facts{OutfitExists: true, OutfitSource: "mine", PhotoCount: 12, Replace: true}, ""},
		"full 通過":        {req("full", map[string]any{"persona": "a", "outfit": "b"}), Facts{OutfitExists: true, OutfitSource: "mine"}, ""},
		"video 沒有第 2 張":  {req("video", map[string]any{"persona": "a", "outfit": "b", "photo": 2}), Facts{OutfitExists: true, OutfitSource: "mine", PhotoCount: 1}, ReasonNoPhoto(2)},
		"video 第 2 支前面缺": {req("video", map[string]any{"persona": "a", "outfit": "b", "clip": 2}), Facts{OutfitExists: true, OutfitSource: "mine", PhotoCount: 1, ClipCount: 0}, ReasonClipGap(2)},
		"video 第 2 支通過":  {req("video", map[string]any{"persona": "a", "outfit": "b", "clip": 2}), Facts{OutfitExists: true, OutfitSource: "mine", PhotoCount: 1, ClipCount: 1}, ""},
		"video 換第 2 支":   {req("video", map[string]any{"persona": "a", "outfit": "b", "clip": 2}), Facts{OutfitExists: true, OutfitSource: "mine", PhotoCount: 1, ClipCount: 3}, ""},
		"video 通過":       {req("video", map[string]any{"persona": "a", "outfit": "b", "photo": 2}), Facts{OutfitExists: true, OutfitSource: "mine", PhotoCount: 2}, ""},
	} {
		if got := CheckFacts(tc.r, tc.f); got != tc.want {
			t.Errorf("%s：CheckFacts = %q，要 %q", name, got, tc.want)
		}
	}
}

// TestCharacterCardYAML：角色卡用 `---` YAML 檔頭，名字寫成雙引號字串並跳脫；讀的時候 YAML 與 TOML 兩種都認。
// Guards: REQ-126
func TestCharacterCardYAML(t *testing.T) {
	card := CharacterCard(`小"狐"\ 第一行`+"\n第二行", "一句話介紹\n\n第二段")
	if !strings.HasPrefix(card, "---\nschema: 1\nname: \"") || strings.Contains(card, "+++") {
		t.Fatalf("檔頭：%q", card)
	}
	if !strings.Contains(card, `name: "小\"狐\"\\ 第一行\n第二行"`) {
		t.Fatalf("跳脫：%q", card)
	}
	if !strings.HasSuffix(card, "---\n一句話介紹\n\n第二段\n") {
		t.Fatalf("內文：%q", card)
	}
	for in, want := range map[string]string{
		card:                       "小\"狐\"\\ 第一行\n第二行",
		"---\nname: Athena\n---\n": "Athena",
		"+++\nname = \"阿媞\"\naka = []\n+++\n": "阿媞",
		"+++\nname = 'Zed'\n+++\n":            "Zed",
		"沒有檔頭\nname: x":                       "",
		"---\nschema: 1\n---\nname: Body\n":   "",
	} {
		if got := CardName(in); got != want {
			t.Errorf("CardName(%q) = %q，要 %q", in, got, want)
		}
	}
	// 控制字元也能來回。
	if got := CardName(CharacterCard("a\x01b\tc", "")); got != "a\x01b\tc" {
		t.Errorf("控制字元：%q", got)
	}
}

// 清單欄位（刪一套記下的照片編號）：YAML／JSON 解出來的兩種樣子都認，空的略過。
func TestStringsField(t *testing.T) {
	if got := Strings([]any{"a", "", 3, " b "}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("[]any：%v", got)
	}
	if got := Strings([]string{"x"}); len(got) != 1 || got[0] != "x" {
		t.Fatalf("[]string：%v", got)
	}
	if Strings(nil) != nil || Strings("a") != nil {
		t.Fatal("不是清單回 nil")
	}
}
