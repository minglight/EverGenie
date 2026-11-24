import { describe, expect, test } from "vitest";
import { canEdit, toSupports } from "./caps";
import {
  adultOn,
  buildCharacters,
  characterHref,
  checkFile,
  checkName,
  costOf,
  deletedOf,
  isMinePerson,
  liveOutfits,
  locked,
  MAX_BYTES,
  type MineOutfit,
  type MinePerson,
  outfitParts,
  type PhotoRec,
  personaFromResult,
  photoGroups,
  pointsText,
  requestFields,
  requestOutcomeText,
  sortOutfits,
  statusChips,
  T,
  tagsOf,
  toOutfitRec,
  toPerson,
  toPhotoRec,
  toRequest,
  validCode,
} from "./mine";

const file = (name: string, type: string, size = 1000) => ({ name, type, size });

const outfit = (
  over: Partial<MinePerson["outfits"][number]> = {},
): MinePerson["outfits"][number] => ({
  record: "athena-base",
  outfit: "base",
  name: "原樣",
  cost: 0,
  state: "active",
  owned: true,
  preview: false,
  promotable: false,
  source: "official",
  full: "",
  photos: [{ url: "/pkg-files/closet/o/athena/base/1.png", real: true, clips: 0 }],
  ...over,
});

describe("上傳衣服與角色頁的純函式", () => {
  // Guards: REQ-126
  test("TestClientSideFileChecks：本機先擋太大與類型不對", () => {
    expect(checkFile(file("a.png", "image/png"), "image")).toBe("");
    expect(checkFile(file("a.jpg", "image/jpeg"), "image")).toBe("");
    expect(checkFile(file("a.webp", "image/webp"), "image")).toBe("");
    expect(checkFile(file("a.png", ""), "image")).toBe(""); // 沒有 MIME 時看副檔名
    expect(checkFile(file("a.png", "image/png", MAX_BYTES), "image")).toBe("");
    expect(checkFile(file("a.png", "image/png", MAX_BYTES + 1), "image")).toBe(T.tooBig);
    expect(checkFile(file("note.txt", "text/plain"), "image")).toBe(T.imageOnly);
    expect(checkFile(file("a.gif", "image/gif"), "image")).toBe(T.imageOnly);
    expect(checkFile(file("clip.mp4", "video/mp4"), "image")).toBe(T.imageOnly);
    expect(checkFile(file("clip.mp4", "video/mp4"), "video")).toBe("");
    expect(checkFile(file("clip.mp4", ""), "video")).toBe("");
    expect(checkFile(file("a.png", "image/png"), "video")).toBe(T.videoOnly);
    expect(checkFile(file("clip.mov", "video/quicktime"), "video")).toBe(T.videoOnly);
    expect(checkFile(null, "image")).toBe(T.needFile);
    // 名稱與代號。
    expect(checkName("  ")).toBe(T.needName);
    expect(checkName("字".repeat(40))).toBe("");
    expect(checkName("字".repeat(41))).toBe(T.nameTooLong);
    expect(validCode("myhelper")).toBe(true);
    expect(validCode("my_helper")).toBe(true);
    expect(validCode("my_")).toBe(false);
    expect(validCode("helper")).toBe(false);
    expect(validCode("my")).toBe(false);
    expect(validCode("myHelper")).toBe(false);
  });

  // Guards: REQ-126
  test("TestRequestOutcomeText：done、rejected 顯示原因、逾時、還在等", () => {
    const req = (state: string, reason = "") =>
      toRequest({ id: "U-1", fields: { state, reason, result: "myhelper-base" } });
    // done 帶程式回填的 result：新增角色用它開到新角色的頁（DEV-143）。
    expect(requestOutcomeText(req("done"), false)).toEqual({
      kind: "done",
      text: T.done,
      result: "myhelper-base",
    });
    expect(requestOutcomeText(toRequest({ id: "U-2", fields: { state: "done" } }), false)).toEqual({
      kind: "done",
      text: T.done,
    });
    expect(personaFromResult("myhelper-base")).toBe("myhelper");
    expect(personaFromResult("myhelper-my-3")).toBe("");
    expect(personaFromResult(undefined)).toBe("");
    expect(requestOutcomeText(req("rejected", "官方的衣服不能改"), false)).toEqual({
      kind: "rejected",
      text: "官方的衣服不能改",
    });
    expect(requestOutcomeText(req("rejected"), false)).toEqual({
      kind: "rejected",
      text: T.unknownReject,
    });
    expect(requestOutcomeText(req("pending"), false)).toEqual({ kind: "waiting" });
    expect(requestOutcomeText(undefined, false)).toEqual({ kind: "waiting" });
    expect(requestOutcomeText(req("pending"), true)).toEqual({ kind: "timeout", text: T.timeout });
    expect(requestOutcomeText(undefined, true)).toEqual({ kind: "timeout", text: T.timeout });
    // 逾時前一刻已經 done 的，以 done 為準。
    expect(requestOutcomeText(req("done"), true).kind).toBe("done");
    expect(toRequest({ id: "U-1", fields: { state: "done", result: "x-base" } }).result).toBe(
      "x-base",
    );
  });

  // Guards: REQ-233
  test("TestCharacterGrid：所有角色一格一位、誰坐哪個位置、沒衣服的也列、被蓋住的卡不列", () => {
    const official = toPerson({
      id: "athena",
      fields: {
        persona: "athena",
        name: "Athena",
        outfits: [
          outfit(),
          outfit({
            record: "athena-my-3",
            outfit: "my-3",
            name: "海邊",
            source: "mine",
            cost: 1,
            owned: false,
          }),
        ],
      },
    });
    const mine = toPerson({
      id: "myfox",
      fields: {
        persona: "myfox",
        name: "小狐",
        outfits: [
          outfit({
            record: "myfox-base",
            outfit: "base",
            name: "基本款",
            source: "mine",
            full: "/f/1.png",
          }),
          outfit({
            record: "myfox-my-5",
            outfit: "my-5",
            name: "草稿",
            source: "mine",
            state: "draft",
          }),
        ],
      },
    });
    const empty = toPerson({
      id: "ghost",
      fields: { persona: "ghost", name: "沒衣服", outfits: [] },
    });
    const nobody = toPerson({
      id: "zed",
      fields: { persona: "zed", name: "Zed", outfits: [outfit({ record: "zed-base" })] },
    });
    const people = [official, empty, nobody, mine];
    const card = (slug: string, over: Record<string, unknown> = {}) => ({
      slug,
      name: slug,
      source: "official",
      status: "live",
      seats: [] as string[],
      shadowed: false,
      ...over,
    });
    const cards = [
      card("athena", { name: "Athena", seats: ["code"], avatar_url: "/avatars/code.png" }),
      card("helper", { name: "Helper", seats: ["life"] }),
      card("ghost", { name: "沒衣服" }),
      card("myfox", { name: "小狐", source: "mine" }),
      card("mydraft", { name: "草稿角色", source: "mine", status: "design" }),
      card("myfox", { name: "被蓋住", source: "mine", shadowed: true }),
    ];

    expect(isMinePerson(mine)).toBe(true);
    expect(isMinePerson(official)).toBe(false);
    const chars = buildCharacters(people, cards);
    // 有人坐的在前（依位置：code、life），再來是我的、官方的；角色庫沒有、衣櫃有衣服的（zed）也列；
    // 沒衣服的角色（helper、ghost）一樣列出來，才能替她加第一套。
    expect(chars.map((c) => c.persona)).toEqual([
      "athena",
      "helper",
      "mydraft",
      "myfox",
      "ghost",
      "zed",
    ]);
    const [athena, helper, mydraft, myfox] = chars;
    expect(athena?.seats).toEqual(["code"]);
    // 有人坐就用現在穿的頭像，沒人坐用第一張真圖，什麼都沒有是空字串（畫人形）。
    expect(athena?.thumb).toBe("/avatars/code.png");
    expect(helper?.thumb).toBe("");
    expect(helper?.outfits).toEqual([]);
    expect(myfox?.mine).toBe(true);
    expect(myfox?.thumb).toBe("/pkg-files/closet/o/athena/base/1.png");
    expect(mydraft?.design).toBe(true);
    // 衣服排序：基本款、官方、我的、草稿最後。
    expect(sortOutfits(official.outfits).map((o) => o.record)).toEqual([
      "athena-base",
      "athena-my-3",
    ]);
    expect(sortOutfits(mine.outfits).map((o) => o.record)).toEqual(["myfox-base", "myfox-my-5"]);
    // 一位角色一頁（DEV-143）：衣服在核心角色頁的「衣服」分頁。
    expect(characterHref("myfox")).toBe("/personas/myfox?tab=closet-mine.outfits");
    expect(characterHref("")).toBe("/personas");
    // 標籤：沒兌換的只標未兌換與點數；設計中、下架只在能修改衣服的家（開發家）標。一般家沒有上架／下架這件事
    // （作者 11-15）：設計中的照一般的標（衣櫃每輪自動上架），下架的不標（我的下架的列在「刪掉的」）。
    const fox = mine.outfits;
    const sea = official.outfits[1];
    expect(tagsOf(sea as never)).toEqual([T.tagLocked, "1 點"]);
    expect(tagsOf(fox[1] as never, true)).toEqual([T.tagDraft]);
    expect(tagsOf(fox[1] as never)).not.toContain(T.tagDraft);
    const retired = { ...fox[1], state: "retired" } as never;
    expect(tagsOf(retired, true)).toContain(T.tagRetired);
    expect(tagsOf(retired)).not.toContain(T.tagRetired);
    const offRetired = { ...official.outfits[0], state: "retired" } as never;
    expect((official.outfits[0] as { source: string }).source).toBe("official");
    expect(tagsOf(offRetired)).not.toContain(T.tagRetired);
    expect(tagsOf(offRetired, true)).toContain(T.tagRetired);
    for (const o of [fox[1], retired, offRetired, sea])
      for (const t of tagsOf(o as never)) expect(t).not.toMatch(/上架|下架|設計中/);
    expect(tagsOf(fox[0] as never)).toEqual([T.tagOwned, "免費"]);
    // 讀回來的資料：full 與 source。
    expect(fox[0]?.full).toBe("/f/1.png");
  });

  test("自己的隱藏版：開關只有 on 算開；只有照片、全身照、動畫會帶 adult", () => {
    expect(adultOn({ "closet.adult": "on" })).toBe(true);
    expect(adultOn({ "closet.adult": " ON " })).toBe(true);
    const offs: (Record<string, string> | undefined)[] = [
      undefined,
      {},
      { "closet.adult": "off" },
      { "closet.adult": "yes" },
    ];
    for (const v of offs) {
      expect(adultOn(v)).toBe(false);
    }
    for (const type of ["photo", "full", "video"] as const) {
      expect(
        requestFields({ type, upload: "/share/a.png", persona: "a", outfit: "b", adult: true })
          .adult,
      ).toBe(true);
      // 畫了「放進自己的隱藏版」但沒勾：明說是一般版（adult = false），程式不會照「整套都是隱藏版」猜成隱藏版。
      expect(
        requestFields({ type, upload: "/share/a.png", persona: "a", outfit: "b", adult: false }),
      ).toHaveProperty("adult", false);
    }
    // 沒畫（開關關著）：不寫 adult，照程式的預設。
    expect(
      requestFields({ type: "photo", upload: "/share/a.png", persona: "a", outfit: "b" }),
    ).not.toHaveProperty("adult");
    expect(
      requestFields({
        type: "outfit",
        upload: "/share/a.png",
        persona: "a",
        name: "x",
        adult: true,
      }),
    ).not.toHaveProperty("adult");
  });

  // Guards: REQ-126
  test("requestFields：空值不寫、state 一律 pending、outfit 才有 free、頁面不送 draft、video 預設第 1 張", () => {
    expect(
      requestFields({
        type: "character",
        upload: "/share/a.png",
        persona: " myfox ",
        name: " 小狐 ",
        desc: "",
      }),
    ).toEqual({
      type: "character",
      upload: "/share/a.png",
      state: "pending",
      persona: "myfox",
      name: "小狐",
    });
    expect(
      requestFields({
        type: "outfit",
        upload: "/share/a.png",
        persona: "athena",
        name: "海邊",
        free: true,
      }),
    ).toEqual({
      type: "outfit",
      upload: "/share/a.png",
      state: "pending",
      persona: "athena",
      name: "海邊",
      free: true,
    });
    // 頁面不送 draft（請求的 draft = true 留給秘書；開發家在「修改」分頁「放回開發中」，CLO-35）。
    expect(
      requestFields({ type: "outfit", upload: "/share/a.png", persona: "a", name: "x", cost: 0 }),
    ).not.toHaveProperty("draft");
    // 價錢（DEV-141）：有給 cost 就帶 cost、不帶 free。
    const priced = requestFields({
      type: "outfit",
      upload: "/share/a.png",
      persona: "athena",
      name: "晚宴",
      cost: 3,
    });
    expect(priced.cost).toBe(3);
    expect("free" in priced).toBe(false);
    expect(
      requestFields({ type: "outfit", upload: "/share/a.png", persona: "a", name: "x", cost: 0 })
        .cost,
    ).toBe(0);
    expect(
      requestFields({ type: "video", upload: "/share/a.mp4", persona: "a", outfit: "b" }).photo,
    ).toBe(1);
    expect(
      requestFields({ type: "video", upload: "/share/a.mp4", persona: "a", outfit: "b", photo: 3 })
        .photo,
    ).toBe(3);
    const clip2 = requestFields({
      type: "video",
      upload: "/share/a.mp4",
      persona: "a",
      outfit: "b",
      clip: 2,
    });
    expect(clip2.clip).toBe(2);
    expect(
      "clip" in
        requestFields({
          type: "video",
          upload: "/share/a.mp4",
          persona: "a",
          outfit: "b",
          clip: 1,
        }),
    ).toBe(false);
    expect(
      "photo" in
        requestFields({ type: "photo", upload: "/share/a.png", persona: "a", outfit: "b" }),
    ).toBe(false);
  });
});

// 衣服分頁一眼看到缺什麼（DEV-143）：半身＝第 1 張、輪播＝2 張以上、動畫＝任一張有。
test("TestOutfitPartsShowWhatIsMissing", () => {
  const none = outfitParts(outfit({ photos: [], full: "" }));
  expect(none.map((p) => [p.key, p.ok, p.label])).toEqual([
    ["half", false, T.missHalf],
    ["full", false, T.missFull],
    ["rotation", false, T.missRotation],
    ["video", false, T.missVideo],
  ]);
  const all = outfitParts(
    outfit({
      full: "/f.png",
      photos: [
        { url: "/1.png", real: true, clips: 2 },
        { url: "/2.png", real: true, clips: 1 },
      ],
    }),
  );
  expect(all.every((p) => p.ok)).toBe(true);
  expect(all.map((p) => p.label)).toEqual([
    T.partHalf,
    T.partFull,
    T.partRotation(2),
    T.partVideo(3),
  ]);
});

test("要用點數換：關＝0 點；開＝1 到 99 的整數，預設 1", () => {
  expect(costOf(false, "abc")).toBe(0);
  expect(costOf(true, " 3 ")).toBe(3);
  expect(costOf(true, "99")).toBe(99);
  for (const bad of ["", "0", "-1", "100", "1.5", "abc"]) expect(costOf(true, bad)).toBeNull();
  expect(pointsText(0)).toBe("1");
  expect(pointsText(5)).toBe("5");
});

test("不帶檔的整理請求：刪、拿回、改點數、上架", () => {
  expect(requestFields({ type: "remove_photo", persona: "a", photoId: "a-my-1--u3" })).toEqual({
    type: "remove_photo",
    state: "pending",
    persona: "a",
    photo_id: "a-my-1--u3",
  });
  expect(requestFields({ type: "set_cost", persona: "a", outfit: "my-1", cost: 2 })).toEqual({
    type: "set_cost",
    state: "pending",
    persona: "a",
    outfit: "my-1",
    cost: 2,
  });
  // 換一張：寫 photo_id；video 照編號指那一張，不寫位置 photo。
  const v = requestFields({
    type: "video",
    upload: "/share/a.mp4",
    persona: "a",
    outfit: "my-1",
    photoId: "a-my-1--u3",
    clip: 2,
  });
  expect(v).toMatchObject({ photo_id: "a-my-1--u3", clip: 2 });
  expect(v).not.toHaveProperty("photo");
  expect(
    requestOutcomeText({ ...toRequest({ id: "U-1" }), state: "done" }, false, "刪掉了"),
  ).toEqual({ kind: "done", text: "刪掉了" });
});

test("這個家支援什麼：只看 closet.capabilities 的 supports，讀不到當不能修改衣服", () => {
  expect(canEdit(toSupports({ fields: { supports: ["mine", "edit"] } }))).toBe(true);
  expect(canEdit(toSupports({ fields: { supports: ["mine"] } }))).toBe(false);
  expect(canEdit(toSupports({ fields: { supports: ["mine", "edit"] }, archived: true }))).toBe(
    false,
  );
  expect(canEdit(toSupports(null))).toBe(false);
  expect(canEdit(undefined)).toBe(false);
});

const photo = (id: string, over: Record<string, unknown> = {}, archived = false): PhotoRec =>
  toPhotoRec({
    id,
    archived,
    updated_at: String(over.at ?? ""),
    fields: {
      persona: "a",
      outfit: "a-my-1",
      type: "half",
      order: 1,
      hidden: false,
      source: "mine",
      ...over,
    },
  });

// 一套的每張照片（CMI-24）：照照片紀錄，半身照照 order 排、影片接在配的那張底下；隱藏版只在開關開著時另成一組。
test("TestPhotoGroupsFollowPhotoRecords", () => {
  const photos = [
    photo("h2", { order: 2 }),
    photo("h1", { order: 1 }),
    photo("v2", { type: "video", of: "h1", clip: 2, order: 1 }),
    photo("v1", { type: "video", of: "h1", clip: 1, order: 1 }),
    photo("f1", { type: "full" }),
    photo("x1", { hidden: true }),
    photo("gone", { order: 3 }, true),
    photo("other", { outfit: "a-my-2" }),
  ];
  const urls = { h1: "/o/1.png", v1: "/o/1.mp4" };
  const [std, ...rest] = photoGroups(photos, "a-my-1", urls, false);
  expect(rest).toEqual([]);
  expect(std?.halves.map((h) => [h.id, h.label, h.url])).toEqual([
    ["h1", T.photoHalf(1), "/o/1.png"],
    ["h2", T.photoHalf(2), ""],
  ]);
  expect(std?.halves[0]?.clips.map((c) => [c.id, c.label])).toEqual([
    ["v1", T.photoClip(1)],
    ["v2", T.photoClip(2)],
  ]);
  expect(std?.full?.id).toBe("f1");
  expect(std?.halves.every((h) => h.removable)).toBe(true);
  // 開關開著：隱藏版另一組；一般版只剩一張半身照時那張不能單獨刪。
  const groups = photoGroups(
    photos.filter((p) => p.id !== "h2"),
    "a-my-1",
    urls,
    true,
  );
  expect(groups.map((g) => g.hidden)).toEqual([false, true]);
  expect(groups[0]?.halves[0]?.removable).toBe(false);
  expect(groups[1]?.halves.map((h) => [h.id, h.removable])).toEqual([["x1", true]]);
});

// 刪掉的（CMI-19、CMI-20）：刪掉的整套（基本款、升成官方換了編號的不算）與還在的衣服裡刪掉的單張；
// 影片配的那張也刪了就跟著那張拿回，不另列；隱藏版只在開關開著時列。
test("TestDeletedListsWhatCanComeBack", () => {
  const outfit = (record: string, over: Record<string, unknown> = {}, archived = false) =>
    toOutfitRec({
      id: record,
      archived,
      updated_at: String(over.at ?? ""),
      fields: { persona: "a", outfit: record.slice(2), name: record, source: "mine", ...over },
    });
  const outfits = [
    outfit("a-base", {}, true),
    outfit("a-my-1"),
    outfit("a-my-2", { at: "2026-11-15T02:00:00Z" }, true),
    outfit("a-my-3", {}, true),
    outfit("a-sea", { from: "a-my-3", source: "official" }),
    outfit("a-off", { source: "official" }, true),
  ];
  const photos = [
    photo("p1", { at: "2026-11-15T03:00:00Z" }, true),
    photo("p2", { hidden: true }, true),
    photo("p3", { type: "video", of: "p1" }, true),
    photo("p4", { outfit: "a-my-2" }, true),
    photo("p5"),
    photo("p6", { type: "video", of: "p5" }, true),
    photo("p7", { source: "official" }, true),
  ];
  expect(deletedOf(outfits, photos, false).map((d) => [d.kind, d.id, d.record])).toEqual([
    ["photo", "p1", "a-my-1"],
    ["outfit", "my-2", "a-my-2"],
    ["photo", "p6", "a-my-1"],
  ]);
  expect(deletedOf(outfits, photos, true).map((d) => d.id)).toContain("p2");
  // 總覽：closet.person 還沒跟上時，紀錄已封存的那套先不列（不會同時在列上與「刪掉的」）。
  const shown = [{ record: "a-my-1" }, { record: "a-my-2" }] as MineOutfit[];
  expect(liveOutfits(shown, outfits).map((o) => o.record)).toEqual(["a-my-1"]);
  expect(liveOutfits(shown, undefined)).toHaveLength(2);
  expect(deletedOf(outfits, photos, true).find((d) => d.id === "p2")?.label).toContain(
    T.photoHiddenTitle,
  );
  // 一般家沒有下架（作者 11-15）：我的、下架的那套算刪掉的——列在「刪掉的」（拿回來＝改回上架），總覽不列，
  // 它的照片不另外列；開發家照舊在總覽、標「下架」。基本款與官方的下架不算。
  const withRetired = [
    ...outfits,
    outfit("a-my-4", { state: "retired" }),
    outfit("a-old", { state: "retired", source: "official" }),
  ];
  const morePhotos = [...photos, photo("p8", { outfit: "a-my-4" }, true)];
  const ordinary = deletedOf(withRetired, morePhotos, false, false);
  expect(ordinary.filter((d) => d.kind === "outfit").map((d) => d.record)).toEqual([
    "a-my-2",
    "a-my-4",
  ]);
  expect(ordinary.map((d) => d.id)).not.toContain("p8");
  expect(ordinary.map((d) => d.id)).not.toContain("old");
  const dev = deletedOf(withRetired, morePhotos, false, true);
  expect(dev.filter((d) => d.kind === "outfit").map((d) => d.record)).toEqual(["a-my-2"]);
  expect(dev.map((d) => d.id)).toContain("p8");
  const live = [{ record: "a-my-1" }, { record: "a-my-4" }, { record: "a-old" }] as MineOutfit[];
  expect(liveOutfits(live, withRetired, false).map((o) => o.record)).toEqual(["a-my-1", "a-old"]);
  expect(liveOutfits(live, withRetired, true)).toHaveLength(3);
});

// 每張照片的狀態（DEV-143、CLO-21）：person 的 status／hidden 讀進來；總覽小標算照片、全身、動畫、每個隱藏版有幾張。
test("TestStatusChipsCountPhotosAndHiddenVersions", () => {
  const person = toPerson({
    id: "athena",
    fields: {
      persona: "athena",
      hidden: [{ id: "adult", name: "隱藏版" }, { id: "" }],
      outfits: [
        {
          record: "athena-base",
          outfit: "base",
          owned: true,
          photos: [{ url: "/1.png", real: true }],
          status: {
            photos: [
              { n: 1, std: { img: true, videos: 2 }, hidden: { adult: { img: true, videos: 0 } } },
              { n: 2, std: { img: false, videos: 0 }, hidden: { adult: { img: true, videos: 1 } } },
              { n: 3, std: { img: true, videos: 0 }, hidden: {} },
            ],
            full: { std: false, hidden: { adult: true } },
          },
        },
        {
          record: "athena-night",
          outfit: "night",
          owned: false,
          photos: [{ url: "", real: false }],
        },
      ],
    },
  });
  expect(person.hidden).toEqual([{ id: "adult", name: "隱藏版" }]);
  const [base, night] = person.outfits;
  if (!base || !night) throw new Error("少了衣服");
  expect(base.status?.photos[1]?.hidden.adult).toEqual({ img: true, videos: 1 });
  expect(statusChips(base, person.hidden).map((c) => [c.key, c.ok, c.label])).toEqual([
    ["photos", true, T.chipPhotos(2)],
    ["full", false, T.missFull],
    ["video", true, T.chipVideo(1, 2)],
    ["h-adult", true, T.chipHidden("隱藏版", 2, 3)],
  ]);
  // 沒兌換：衣櫃不給 status，小標是空的（頁面改寫「還沒兌換」）
  expect(night.status).toBeUndefined();
  expect(statusChips(night, person.hidden)).toEqual([]);
});

// 開發家不遮（衣櫃 CLO-27）：沒兌換的衣服衣櫃照樣給真圖與 status、多一個 preview；這裡照常列每張的狀態，
// 標籤改說「未兌換（開發家看得到全部）」，不再寫「真圖兌換後才看得到」。正式家沒有 preview，照舊。
test("TestPreviewOutfitListedNotLocked", () => {
  const person = toPerson({
    id: "athena",
    fields: {
      outfits: [
        {
          record: "athena-night",
          outfit: "night",
          cost: 1,
          owned: false,
          preview: true,
          photos: [{ url: "/pkg-files/closet/o/athena/night/1.png", real: true }],
          status: { photos: [{ n: 1, std: { img: true, videos: 0 }, hidden: {} }], full: {} },
        },
        { record: "athena-sea", outfit: "sea", cost: 2, owned: false, photos: [{ real: false }] },
      ],
    },
  });
  const [night, sea] = person.outfits as [MineOutfit, MineOutfit];
  expect(night.preview).toBe(true);
  expect(locked(night)).toBe(false);
  expect(tagsOf(night)).toEqual(["未兌換（開發家看得到全部）", "1 點"]);
  expect(statusChips(night, []).map((c) => c.key)).toEqual(["photos", "full", "video"]);
  // 正式家（沒有 preview）：照舊是鎖著的。
  expect(sea.preview).toBe(false);
  expect(locked(sea)).toBe(true);
  expect(tagsOf(sea)).toEqual([T.tagLocked, "2 點"]);
  expect(locked(outfit({ owned: true }))).toBe(false);
  // 還沒上架的（draft）衣櫃照樣公開真圖：不算只看得到剪影。
  expect(locked(outfit({ owned: false, preview: false, state: "draft" }))).toBe(false);
});
