// closet-photos 是照片轉換器的預演入口（DEV-160 S1）：照一個家（或它的複本）現有的衣服紀錄、照片紀錄與圖檔，
// 印出衣櫃的程式下一輪會補登記幾筆 closet.photo（每位角色每套）、哪些已經登記、哪些檔沒登記與原因。只讀，不寫任何東西。
//
//	go run ./cmd/closet-photos --home <家或複本> [--official <官方角色資料夾>] [--mine <主人的角色資料夾>]
//
// 紀錄讀家裡的 vault 與官方層（<官方角色資料夾>/records/，VLT-19）。真的補登記是衣櫃的程式自己做（DEV-160 S2：服務啟動、
// 角色包同步完、衣服目錄有變、定期重掃都會補；只新增紀錄、不碰檔），這裡不另做寫入的入口；--dry-run 照舊接受（本來就只預演）。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/minglight/evergenie/packages/official/closet/internal/photomigrate"
	closet "github.com/minglight/evergenie/packages/official/closet/internal/run"
	"github.com/minglight/evergenie/sdk"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("closet-photos", flag.ContinueOnError)
	_ = fs.Bool("dry-run", true, "只預演、不寫（只有這個模式；舊的呼叫帶著也可以）")
	home := fs.String("home", sdk.Getenv("EVERGENIE_HOME"), "家（或它的複本）的資料夾")
	official := fs.String("official", "", "官方角色資料夾（沒給照家的設定算，sdk.OfficialPersonasDir）")
	mine := fs.String("mine", "", "主人的角色資料夾（沒給是 <家>/personas/mine）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *home == "" {
		return fmt.Errorf("要給 --home（或設 EVERGENIE_HOME）")
	}
	if *official == "" {
		*official = sdk.OfficialPersonasDir(*home)
	}
	if *mine == "" {
		*mine = filepath.Join(*home, "personas", "mine")
	}
	var bad []string
	read := func(kind string) []sdk.Record {
		recs, b, err := photomigrate.ReadKindFiles(*home, *official, kind)
		if err != nil {
			bad = append(bad, fmt.Sprintf("closet.%s：%v", kind, err))
		}
		bad = append(bad, b...)
		return recs
	}
	outfits, variants, photos := read("outfit"), read("variant"), read("photo")
	in, invalid := photomigrate.InputFromRecords(outfits, variants, *official, *mine)
	bad = append(bad, invalid...)
	writable := filepath.Clean(*official) == filepath.Join(*home, "personas") // 同衣櫃的 officialWritable
	fmt.Printf("預演（不寫）：家 %s，官方角色 %s，主人的角色 %s\n", *home, *official, *mine)
	fmt.Printf("讀到 closet.outfit %d 筆（合格沒封存 %d）、closet.variant %d 筆（合格沒封存 %d）、已經有的 closet.photo %d 筆\n",
		len(outfits), len(in.Outfits), len(variants), len(in.Variants), len(photos))
	if !writable {
		fmt.Println("官方層不在家裡：官方照片的紀錄不寫進家（跟角色包走），下面只算主人自己的照片；官方層沒帶紀錄的衣服，衣櫃在記憶體裡照檔案算")
	}
	for _, b := range bad {
		fmt.Printf("略過：%s\n", b)
	}
	photomigrate.Report(os.Stdout, photomigrate.Fill(photomigrate.Plan(in), photos, closet.SkipOfficial(writable)))
	return nil
}
