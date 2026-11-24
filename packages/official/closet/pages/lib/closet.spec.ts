import { describe, expect, test } from "vitest";
import {
  type ClosetState,
  chosenFilter,
  chosenPersona,
  detailTagsOf,
  editTabHref,
  emptyNotice,
  filterEntries,
  filterHref,
  filterKindOf,
  filterOptions,
  findOutfit,
  gridOf,
  idFromSearch,
  lockedHintOf,
  mineAvailable,
  mineHref,
  mineOutfitHref,
  nextOutfitHint,
  nextRotation,
  orderPeople,
  outfitHref,
  ownedView,
  type Person,
  type PersonOutfit,
  packKind,
  personaFromSearch,
  pickerPeople,
  requestOutcome,
  seesAll,
  showMineLink,
  shownOnDetail,
  showroomPeople,
  T,
  tagsOf,
  toPerson,
  toRequest,
  toState,
  undoOf,
  unlockButton,
  variantSwitches,
  viewerItems,
  walletHint,
  wardrobeHref,
  wearOutcome,
  withPersona,
  wornImage,
} from "./closet";

const outfit = (over: Partial<PersonOutfit> = {}): PersonOutfit => ({
  record: "athena-sea",
  outfit: "sea",
  name: "海邊",
  desc: "",
  cost: 2,
  state: "active",
  owned: false,
  rotating: false,
  photos: [
    {
      n: 1,
      url: "/pkg-files/closet/s/athena/sea/1.png",
      real: false,
      video: "",
      videos: [],
      variant: "",
    },
  ],
  full: "",
  fullHidden: "",
  source: "official",
  preview: false,
  promotable: false,
  ...over,
});

const person = (over: Partial<Person> = {}): Person => ({
  persona: "athena",
  name: "Athena",
  agent: "code",
  worn: "base",
  wornPhoto: 1,
  rotation: ["base"],
  picked: false,
  wornUntil: "",
  outfits: [outfit({ record: "athena-base", outfit: "base", name: "原樣", cost: 0, owned: true })],
  photoUrls: {},
  ownerHidden: [],
  hiddenOff: [],
  ...over,
});

const state = (over: Partial<ClosetState> = {}): ClosetState => ({
  earned: 3,
  spent: 1,
  points: 2,
  wallet: true,
  ...over,
});

describe("衣櫃的純函式", () => {
  // Guards: REQ-125
  test("TestPeopleOrderSeatedFirst", () => {
    const people = [
      person({ persona: "zed", agent: "" }),
      person({ persona: "bob", agent: "work" }),
      person({ persona: "amy", agent: "art" }),
      person({ persona: "cat", agent: "" }),
      person({ persona: "empty", agent: "code", outfits: [] }),
    ];
    const { seated, unseated } = orderPeople(people);
    expect(seated.map((p) => p.persona)).toEqual(["amy", "bob"]);
    expect(unseated.map((p) => p.persona)).toEqual(["cat", "zed"]);
  });

  // Guards: REQ-125
  test("TestUnlockButtonState", () => {
    // 能兌換。
    expect(unlockButton(outfit(), state())).toEqual({ show: true, enabled: true, hint: "" });
    // 點數剛好夠也行；差一點停用並說明。
    expect(unlockButton(outfit({ cost: 2 }), state({ points: 2 })).enabled).toBe(true);
    const short = unlockButton(outfit({ cost: 3 }), state({ points: 2 }));
    expect(short).toEqual({ show: true, enabled: false, hint: "點數不夠：要 3 點，現在 2 點" });
    // 已擁有、草稿、下架：不顯示按鈕。
    expect(unlockButton(outfit({ owned: true }), state()).show).toBe(false);
    expect(unlockButton(outfit({ state: "draft" }), state()).show).toBe(false);
    expect(unlockButton(outfit({ state: "retired" }), state()).show).toBe(false);
    // 還沒讀到 state：先不擋（判斷以程式為準）。
    expect(unlockButton(outfit(), null).enabled).toBe(true);
    // 標籤。
    expect(tagsOf(outfit())).toEqual(["2 點"]);
    expect(tagsOf(outfit({ cost: 0, owned: true }))).toEqual([T.tagFree]);
    expect(tagsOf(outfit({ owned: true }))).toEqual([T.tagOwned]);
    // 「下架」只在能修改衣服的家（開發家）標；一般家沒有上架／下架這些字（作者 11-15，M5）。
    expect(tagsOf(outfit({ owned: true, state: "retired" }), true)).toEqual([
      T.tagOwned,
      T.tagRetired,
    ]);
    expect(tagsOf(outfit({ owned: true, state: "retired" }))).toEqual([T.tagOwned]);
    for (const t of [T.notFoundHint, T.retiredCannot]) expect(t).not.toMatch(/上架|下架/);
    expect(tagsOf(outfit({ state: "draft" }))).toEqual([T.tagDraft]);
  });

  test("衣服詳情頁的標籤不重複價格：沒擁有標「未兌換」，其他照衣櫃格子", () => {
    expect(detailTagsOf(outfit())).toEqual([T.tagLocked]);
    expect(detailTagsOf(outfit({ state: "retired" }), true)).toEqual([T.tagLocked, T.tagRetired]);
    expect(detailTagsOf(outfit({ state: "retired" }))).toEqual([T.tagLocked]);
    expect(detailTagsOf(outfit({ cost: 0, owned: true }))).toEqual([T.tagFree]);
    expect(detailTagsOf(outfit({ owned: true }))).toEqual([T.tagOwned]);
    expect(detailTagsOf(outfit({ state: "draft" }))).toEqual([T.tagDraft]);
    // 衣櫃格子沒有價格那行，照舊標「2 點」。
    expect(tagsOf(outfit())).toEqual(["2 點"]);
  });

  // 開發家不遮（CLO-27）：程式對沒兌換的衣服照樣給真圖、多一個 preview。頁面照樣標「未兌換」、兌換按鈕照舊，
  // 只是說明不能再寫「只看得到剪影」，輪播照與動畫也看得到。正式家沒有 preview，全部照舊。
  test("TestPreviewOutfitStillLockedButVisible", () => {
    const p = toPerson({
      id: "athena",
      fields: {
        outfits: [
          { record: "athena-sea", outfit: "sea", cost: 2, owned: false, preview: true },
          { record: "athena-night", outfit: "night", cost: 1, owned: false },
        ],
      },
    });
    const [sea, night] = p.outfits as [PersonOutfit, PersonOutfit];
    expect(sea.preview).toBe(true);
    expect(night.preview).toBe(false);
    // 展示間的格子：圖上沒有鎖頭了，標籤多「未兌換」；詳情頁照舊只標「未兌換」（價格另有一行）。
    expect(tagsOf(sea)).toEqual([T.tagLocked, "2 點"]);
    expect(detailTagsOf(sea)).toEqual([T.tagLocked]);
    expect(tagsOf(night)).toEqual(["1 點"]);
    // 說明：開發家說「未兌換（開發家看得到全部）」，正式家照舊說只看得到剪影；擁有的、設計中的沒有這行。
    expect(lockedHintOf(sea)).toContain("未兌換（開發家看得到全部）");
    expect(lockedHintOf(sea)).not.toContain("剪影");
    expect(lockedHintOf(night)).toBe(T.lockedHint);
    expect(lockedHintOf(outfit({ owned: true }))).toBe("");
    expect(lockedHintOf(outfit({ state: "draft" }))).toBe("");
    // 輪播照與動畫：擁有的與開發家沒兌換的看得到；正式家沒兌換的看不到。
    expect(seesAll(sea)).toBe(true);
    expect(seesAll(night)).toBe(false);
    expect(seesAll(outfit({ owned: true }))).toBe(true);
    // 兌換照舊：沒擁有就有兌換按鈕、點數不夠照樣擋；衣櫃照舊只放擁有的。
    expect(unlockButton(sea, { earned: 0, spent: 0, points: 0, wallet: true }).enabled).toBe(false);
    expect(unlockButton(sea, { earned: 2, spent: 0, points: 2, wallet: true })).toEqual({
      show: true,
      enabled: true,
      hint: "",
    });
    expect(ownedView(p).outfits).toEqual([]);
  });

  // 衣服詳情頁「到角色頁管理這套（加照片、升成官方）」（CLO-27）：只在開發家、closet-mine 能用、這套是主人自己加的時出現，
  // 連到那位角色的「衣服」分頁並直接打開這一套（?item=）。
  // 看的是能力（edit，CLO-37），不是哪種家：頁面傳 canEdit(caps) 進來。
  // 衣服詳情頁（CLO-38）：設計中的只有能修改衣服的家看得到；一般家打開它的網址當找不到，不顯示「設計中」。
  test("TestDraftOutfitNotFoundWithoutEdit", () => {
    expect(shownOnDetail({ state: "draft" }, false)).toBe(false);
    expect(shownOnDetail({ state: "draft" }, true)).toBe(true);
    expect(shownOnDetail({ state: "active" }, false)).toBe(true);
    expect(shownOnDetail({ state: "retired" }, false)).toBe(true);
  });

  // 開發家展示間到「修改」的入口（CLO-37、CLO-38）：沒採用的張數、等上架的套數，有幾個寫幾個。
  test("TestToEditTextCountsPhotosAndReadyOutfits", () => {
    expect(T.toEdit(3)).toBe("3 張照片還沒採用：到角色頁的「修改」看 ›");
    expect(T.toEdit(0, 2)).toBe("2 套等上架：到角色頁的「修改」看 ›");
    expect(T.toEdit(1, 1)).toBe("1 張照片還沒採用、1 套等上架：到角色頁的「修改」看 ›");
  });

  test("TestMineLinkOnlyOnDevHomeForOwnOutfit", () => {
    const mine = { source: "mine" };
    expect(showMineLink(true, true, mine)).toBe(true);
    expect(showMineLink(false, true, mine)).toBe(false);
    // closet-mine 沒開或還沒讀到：不出現（closet 不依賴 closet-mine）。
    expect(showMineLink(true, false, mine)).toBe(false);
    expect(showMineLink(true, undefined, mine)).toBe(false);
    // 官方的衣服沒有東西要管理。
    expect(showMineLink(true, true, { source: "official" })).toBe(false);
    expect(mineOutfitHref("helper", "helper-my-1")).toBe(
      "/personas/helper?tab=closet-mine.outfits&item=helper-my-1",
    );
    expect(mineOutfitHref("", "helper-my-1")).toBe("/personas");
    // 升成官方搬到「修改」分頁（DEV-160 審查 S1）：到「衣服」分頁的連結不再說升成官方，另一條連到「修改」。
    expect(T.toMineOutfit).not.toContain("升成官方");
    expect(T.toEditPromote).toContain("升成官方");
    expect(editTabHref("helper")).toBe("/personas/helper?tab=closet.edit");
  });

  // 看大圖的清單（FEN-30）：真圖、看得到全部時接動畫、最後全身照；剪影不放大。
  test("TestViewerItemsOnlyWhatCanBeSeen", () => {
    const ph = (n: number, over: Partial<PersonOutfit["photos"][number]> = {}) => ({
      n,
      url: `/o/a/sea/${n}.png`,
      real: true,
      video: "",
      videos: [] as string[],
      variant: "",
      ...over,
    });
    const owned = outfit({
      owned: true,
      photos: [ph(1, { video: "/o/a/sea/1.mp4", videos: ["/o/a/sea/1.mp4"] }), ph(2)],
      full: "/f/a/sea/1.png",
      fullHidden: "/f/a/sea/h-1.png",
    });
    expect(viewerItems(owned).map((i) => `${i.type}:${i.src}`)).toEqual([
      "image:/o/a/sea/1.png",
      "video:/o/a/sea/1.mp4",
      "image:/o/a/sea/2.png",
      "image:/f/a/sea/1.png",
      "image:/f/a/sea/h-1.png",
    ]);
    // 沒兌換（正式家）：只有剪影，沒有東西可以放大。
    expect(
      viewerItems(outfit({ photos: [ph(1, { url: "/s/a/sea/1.png", real: false })] })),
    ).toEqual([]);
  });

  // 開發家展示間的篩選（CLO-29）：全部／可以升成官方／已經是官方／只在開發家。跨所有角色、每個按鈕帶數量；
  // 「可以升」只認程式給的 promotable；沒有 edit 能力的家（一般家）沒有這排（CLO-37）；closet-mine 沒開沒有「可以升成官方」。
  test("TestShowroomFilterAcrossPeople", () => {
    const people = [
      toPerson({
        id: "athena",
        fields: {
          name: "Athena",
          agent: "code",
          outfits: [
            { record: "athena-base", outfit: "base", source: "official", owned: true },
            { record: "athena-my-1", outfit: "my-1", source: "mine", promotable: true },
            { record: "athena-adult-1", outfit: "adult-1", source: "mine" },
            { record: "athena-my-2", outfit: "my-2", source: "mine", state: "draft" },
          ],
        },
      }),
      toPerson({
        id: "myfox",
        fields: {
          name: "小狐",
          outfits: [
            { record: "myfox-base", outfit: "base", source: "mine", owned: true },
            { record: "myfox-my-1", outfit: "my-1", source: "mine" },
          ],
        },
      }),
      toPerson({
        id: "builder",
        fields: {
          name: "Builder",
          outfits: [{ record: "builder-my-9", outfit: "my-9", source: "mine", promotable: true }],
        },
      }),
    ];
    const ids = (f: Parameters<typeof filterEntries>[1], mine = true) =>
      filterEntries(people, f, mine).map((e) => e.outfit.record);
    // 一套只算一類；設計中的不算（在「修改」分頁看）。
    expect(ids("promotable")).toEqual(["athena-my-1", "builder-my-9"]);
    expect(ids("official")).toEqual(["athena-base"]);
    expect(ids("mine")).toEqual(["athena-adult-1", "myfox-base", "myfox-my-1"]);
    expect(ids("all")).toHaveLength(6);
    expect(filterEntries(people, "promotable", true).map((e) => e.person.name)).toEqual([
      "Athena",
      "Builder",
    ]);
    // 按鈕與數量：三類加起來等於全部。
    const opts = filterOptions(true, true, people);
    expect(opts.map((o) => `${o.label}:${o.count}`)).toEqual([
      "全部:6",
      "可以升成官方:2",
      "已經是官方:1",
      "只在開發家（我自己的）:3",
    ]);
    // 沒有 edit 能力（一般家、還沒讀到能力）：沒有這排。
    expect(filterOptions(false, true, people)).toEqual([]);
    // closet-mine 沒開（或還沒讀到）：沒有「可以升成官方」，自己加的全部算只在開發家。
    for (const mine of [false, undefined]) {
      expect(filterOptions(true, mine, people).map((o) => `${o.value}:${o.count}`)).toEqual([
        "all:6",
        "official:1",
        "mine:5",
      ]);
    }
    expect(filterKindOf({ source: "mine", promotable: true }, false)).toBe("mine");
    // 網址的 ?filter=：不認得、或現在沒有那個按鈕都當全部。
    expect(chosenFilter("promotable", opts)).toBe("promotable");
    expect(chosenFilter("moved", opts)).toBe("all");
    expect(chosenFilter("promotable", filterOptions(false, true, people))).toBe("all");
    expect(chosenFilter("promotable", filterOptions(true, false, people))).toBe("all");
    // 點了去哪：可以升的到那位角色的「修改」分頁（升成官方在那裡，DEV-160 審查 S1），其他到衣服詳情。
    const [first] = filterEntries(people, "promotable", true);
    if (!first) throw new Error("沒有可以升的");
    expect(filterHref("promotable", first)).toBe("/personas/athena?tab=closet.edit");
    expect(filterHref("mine", first)).toBe("/p/closet/outfit?id=athena-my-1");
  });

  // Guards: REQ-125
  test("TestRotationKeepsOneOwned", () => {
    const p = person({ rotation: ["base", "night"] });
    expect(nextRotation(p, "sea", true)).toEqual(["base", "night", "sea"]);
    expect(nextRotation(p, "night", false)).toEqual(["base"]);
    // 已經在裡面再開、不在裡面再關：清單不變。
    expect(nextRotation(p, "night", true)).toEqual(["base", "night"]);
    expect(nextRotation(p, "sea", false)).toEqual(["base", "night"]);
    // 不能取消最後一個勾。
    expect(nextRotation(person({ rotation: ["base"] }), "base", false)).toBeNull();
  });

  // Guards: REQ-125
  test("TestRequestOutcomeText", () => {
    expect(requestOutcome(undefined, false)).toEqual({ kind: "waiting" });
    expect(requestOutcome(toRequest({ id: "Q-1", fields: { state: "pending" } }), false)).toEqual({
      kind: "waiting",
    });
    expect(
      requestOutcome(toRequest({ id: "Q-1", fields: { state: "done", unlock: "a-b" } }), false),
    ).toEqual({
      kind: "done",
      text: "兌換了",
    });
    expect(
      requestOutcome(
        toRequest({
          id: "Q-1",
          fields: { state: "rejected", reason: "點數不夠：要 2 點，現在 0 點" },
        }),
        false,
      ),
    ).toEqual({ kind: "rejected", text: "點數不夠：要 2 點，現在 0 點" });
    // 被拒但沒有原因：給一句白話。
    expect(requestOutcome(toRequest({ id: "Q-1", fields: { state: "rejected" } }), false)).toEqual({
      kind: "rejected",
      text: T.unknownReject,
    });
    // 逾時：說明已送出；已經有結果就不看逾時。
    expect(requestOutcome(undefined, true)).toEqual({
      kind: "timeout",
      text: "已送出，等衣櫃處理（套件沒在跑時會晚一點）",
    });
    expect(requestOutcome(toRequest({ id: "Q-1", fields: { state: "done" } }), true).kind).toBe(
      "done",
    );
  });

  // 現在穿這套（CLO-30）
  test("TestWearPersonFieldsAndUndo", () => {
    // person 的 picked、worn_until：沒寫當沒指定。
    const p = toPerson({
      id: "athena",
      fields: { worn: "night", picked: true, worn_until: "2026-11-15T00:00:00+08:00" },
    });
    expect(p.picked).toBe(true);
    expect(p.wornUntil).toBe("2026-11-15T00:00:00+08:00");
    const plain = toPerson({ id: "athena", fields: { worn: "base" } });
    expect(plain.picked).toBe(false);
    expect(plain.wornUntil).toBe("");
    // 復原：剛才是照輪流穿的 → 取消這次的指定（回到輪流穿）。
    expect(undoOf({ worn: "base", picked: false }, "night")).toEqual({
      type: "unwear",
      outfit: "night",
    });
    // 剛才那套也是指定穿的 → 再指定穿它。
    expect(undoOf({ worn: "sea", picked: true }, "night")).toEqual({ type: "wear", outfit: "sea" });
    // 剛才沒有穿任何一套、或指定的就是同一套：取消指定。
    expect(undoOf({ worn: "", picked: true }, "night")).toEqual({
      type: "unwear",
      outfit: "night",
    });
    expect(undoOf({ worn: "night", picked: true }, "night")).toEqual({
      type: "unwear",
      outfit: "night",
    });
  });

  test("TestWearOutcomeAndTexts", () => {
    expect(wearOutcome(undefined, false)).toEqual({ kind: "waiting" });
    expect(wearOutcome(toRequest({ id: "Q-1", fields: { state: "done" } }), false)).toEqual({
      kind: "done",
      text: "換好了",
    });
    expect(
      wearOutcome(
        toRequest({
          id: "Q-1",
          fields: { state: "rejected", reason: "還沒擁有這套，要先兌換才能穿" },
        }),
        false,
      ),
    ).toEqual({ kind: "rejected", text: "還沒擁有這套，要先兌換才能穿" });
    expect(wearOutcome(toRequest({ id: "Q-1", fields: { state: "rejected" } }), false)).toEqual({
      kind: "rejected",
      text: T.wearRejected,
    });
    // 等太久還沒處理完：中性的一句「已送出」，不是失敗。
    expect(wearOutcome(undefined, true)).toEqual({
      kind: "timeout",
      text: "已送出，等一下就會換好",
    });
    // 指定穿的穿到當天結束（到期時間由衣櫃的程式算，頁面不用瀏覽器的時區算）。
    expect(T.wearToday).toBe("今天都穿這套，明天起照輪流穿的換");
    expect(T.wearingToday).toBe("現在穿著（今天都穿這套）");
    // 輪流穿的說明寫實際的節奏（每小時），並說指定穿的那天不輪。
    expect(T.rotateHint).toBe("「輪流穿」打開的幾套會每小時輪流換；指定「現在穿這套」的那天不輪。");
    expect(wardrobeHref("athena")).toBe("/p/closet/wardrobe?persona=athena");
    expect(wardrobeHref("")).toBe("/p/closet/wardrobe");
  });

  // Guards: REQ-125
  test("TestWalletMissingHint", () => {
    // 沒有錢包：頂端提示去「習慣」賺點數；兌換按鈕停用並顯示同一句。
    const none = state({ wallet: false, points: 0, earned: 0, spent: 0 });
    expect(walletHint(none)).toBe("還沒有點數：到「習慣」約定習慣、達標就會賺到");
    const btn = unlockButton(outfit(), none);
    expect(btn.enabled).toBe(false);
    expect(btn.hint).toBe(walletHint(none));
    // 有錢包就沒有提示；還沒讀到 state 是另一句。
    expect(walletHint(state())).toBe("");
    expect(walletHint(null)).toBe(T.stateMissing);
  });

  // Guards: REQ-125
  test("TestPersonRecordsAndHelpers", () => {
    const p = toPerson({
      id: "athena",
      fields: {
        persona: "athena",
        name: "Athena",
        agent: "",
        worn: "sea",
        worn_photo: 2,
        rotation: ["sea"],
        outfits: [
          {
            record: "athena-sea",
            outfit: "sea",
            name: "海邊",
            cost: 2,
            state: "active",
            owned: true,
            rotating: true,
            full: "/pkg-files/closet/f/athena/sea/1.png",
            source: "mine",
            photos: [
              { url: "/a/1.png", real: true, video: "/a/1.mp4" },
              { url: "/a/2.png", real: true, video: "" },
            ],
          },
          {
            record: "athena-night",
            outfit: "night",
            cost: 1,
            state: "active",
            owned: false,
            photos: [{ url: "", real: false }],
          },
        ],
      },
    });
    expect(p.outfits[1]?.name).toBe("night");
    // 全身照與來源（REQ-126）：沒寫的 full 是空字串、source 預設 official。
    expect(p.outfits[0]).toMatchObject({
      full: "/pkg-files/closet/f/athena/sea/1.png",
      source: "mine",
    });
    expect(p.outfits[1]).toMatchObject({ full: "", source: "official" });
    expect(wornImage(p)).toBe("/a/2.png");
    // 現穿的是剪影（不該發生）或沒有圖：不顯示。
    expect(wornImage({ ...p, worn: "night" })).toBe("");
    expect(wornImage({ ...p, worn: "" })).toBe("");
    expect(findOutfit([p], "athena-night")?.outfit.outfit).toBe("night");
    expect(findOutfit([p], "nope-x")).toBeUndefined();
    expect(idFromSearch("?id=athena-sea")).toBe("athena-sea");
    expect(idFromSearch("")).toBe("");
    expect(toState(null)).toBeNull();
    expect(toState({ fields: { earned: 3, spent: 1, points: 2, wallet: true } })).toEqual(state());
  });
});

describe("衣櫃與展示間分開（DEV-112）", () => {
  // CLO-14：衣櫃只放擁有的；沒擁有的、設計中的不出現，一套都沒擁有的角色整個不出現。
  test("衣櫃只留擁有的衣服，沒擁有任何一套的角色不出現", () => {
    const p = person({
      outfits: [
        outfit({ record: "athena-base", outfit: "base", cost: 0, owned: true }),
        outfit({ record: "athena-sea", outfit: "sea", owned: false }),
        outfit({ record: "athena-wip", outfit: "wip", state: "draft", owned: false }),
        outfit({ record: "athena-old", outfit: "old", state: "retired", owned: true }),
      ],
    });
    expect(ownedView(p).outfits.map((o) => o.outfit)).toEqual(["base", "old"]);
    expect(ownedView(p).rotation).toEqual(p.rotation);
    const nobody = person({ persona: "zed", outfits: [outfit({ owned: false })] });
    const { seated, unseated } = orderPeople([p, nobody].map(ownedView));
    expect([...seated, ...unseated].map((x) => x.persona)).toEqual(["athena"]);
  });

  // CLO-16：詳情頁的返回交給看板外殼的「上一頁」，網址不再帶從哪一頁來。
  test("衣服詳情的網址只帶衣服", () => {
    expect(outfitHref("athena-sea")).toBe("/p/closet/outfit?id=athena-sea");
  });

  // CLO-15、DEV-143：新增角色到角色清單的「新增角色」、加衣服到那位角色頁的「衣服」分頁；
  // closet-mine 沒開（停用、依賴沒開被拒、沒裝）就不顯示按鈕。
  test("展示間的新增按鈕連到角色頁，closet-mine 不能用就不顯示", () => {
    expect(mineHref("new")).toBe("/personas?new=1");
    expect(mineHref("person")).toBe("/personas");
    expect(mineHref("person", "my kid")).toBe("/personas/my%20kid?tab=closet-mine.outfits");
    expect(mineAvailable({ enabled: true, state: "running" })).toBe(true);
    expect(mineAvailable({ enabled: true, state: "stopped" })).toBe(true);
    expect(mineAvailable({ enabled: true, state: "rejected" })).toBe(false);
    expect(mineAvailable({ enabled: false, state: "stopped" })).toBe(false);
    expect(mineAvailable(undefined)).toBe(false);
  });
});

describe("隱藏版開關（CLO-17）", () => {
  test("只列 button 的隱藏版，照程式的順序；開關與時段照 state，不自己算", () => {
    const r = {
      fields: {
        variants: [
          { id: "b", name: "晚上", button: true, on: false, in_window: true },
          { id: "a", name: "", button: true, on: true, in_window: false },
          { id: "c", name: "只給秘書", button: false, on: true, in_window: false },
          { name: "沒編號", button: true },
        ],
      },
    };
    expect(variantSwitches(r)).toEqual([
      { id: "b", name: "晚上", ids: ["b"], on: false, inWindow: true },
      { id: "a", name: "a", ids: ["a"], on: true, inWindow: false },
    ]);
    // DEV-160 S2 起只有一組（id = h）：ids 是這一組的每一筆設定，按開關一起改。
    const one = {
      fields: {
        variants: [
          {
            id: "h",
            name: "隱藏版",
            button: true,
            on: true,
            in_window: false,
            ids: ["demo", "adult"],
          },
        ],
      },
    };
    expect(variantSwitches(one)).toEqual([
      { id: "h", name: "隱藏版", ids: ["demo", "adult"], on: true, inWindow: false },
    ]);
    expect(variantSwitches(null)).toEqual([]);
    expect(variantSwitches({ fields: { earned: 1 } })).toEqual([]);
  });
});

// DEV-143：衣櫃與展示間上面一排頭像，一次只看一位；網址的 ?persona= 優先、再來是上次看的、都沒有就第一位。
describe("一次看一位角色（DEV-143）", () => {
  test("TestPickerOrderAndChoice", () => {
    const a = person({ persona: "athena", agent: "code" });
    const b = person({ persona: "bea", agent: "" });
    const empty = person({ persona: "zed", agent: "", outfits: [] });
    const list = pickerPeople([b, empty, a]);
    expect(list.map((p) => p.persona)).toEqual(["athena", "bea"]);
    expect(chosenPersona(list, "bea", "athena")).toBe("bea");
    expect(chosenPersona(list, "", "bea")).toBe("bea");
    expect(chosenPersona(list, "gone", "gone2")).toBe("athena");
    expect(chosenPersona([], "bea", "")).toBe("");
    expect(personaFromSearch("?persona=bea")).toBe("bea");
    expect(withPersona("?drafts=1", "bea")).toBe("?drafts=1&persona=bea");
    expect(withPersona("?persona=bea", "")).toBe("");
  });

  // 展示間不列設計中的衣服（CLO-37）：一般家只有設計中衣服的角色不出現在頭像列；開發家照列（那一段有到「修改」的入口）。
  test("TestShowroomPeopleHidesDraftOnlyWithoutEdit", () => {
    const a = person({
      persona: "athena",
      agent: "code",
      outfits: [
        outfit({ record: "athena-base", outfit: "base" }),
        outfit({ record: "athena-wip", outfit: "wip", state: "draft" }),
      ],
    });
    const b = person({ persona: "bea", agent: "", outfits: [outfit({ record: "bea-base" })] });
    const c = person({
      persona: "cat",
      agent: "",
      outfits: [outfit({ record: "cat-wip", state: "draft" })],
    });
    const plain = showroomPeople([c, b, a], false);
    expect(plain.map((p) => p.persona)).toEqual(["athena", "bea"]);
    expect(plain[0]?.outfits.map((o) => o.record)).toEqual(["athena-base"]);
    expect(showroomPeople([c, b, a], true).map((p) => p.persona)).toEqual(["athena", "bea", "cat"]);
    // 格子本來就不含設計中的。
    expect(gridOf(a).map((o) => o.record)).toEqual(["athena-base"]);
  });
});

describe("展示間的「再 N 點可以換下一套」（CLO-24）", () => {
  const st = (points: number): ClosetState => ({ earned: points, spent: 0, points, wallet: true });
  const people = (...outfits: PersonOutfit[]) => [person({ outfits })];

  test("點數不夠說還差幾點，夠了說可以換一套", () => {
    const list = people(outfit({ owned: false, cost: 3 }), outfit({ owned: false, cost: 2 }));
    expect(nextOutfitHint(list, st(0))).toBe(T.nextShort(2));
    expect(nextOutfitHint(list, st(1))).toBe("再 1 點可以換下一套");
    expect(nextOutfitHint(list, st(2))).toBe(T.nextReady);
    expect(nextOutfitHint(list, st(9))).toBe("可以換一套了");
  });

  test("沒有可換的衣服、或點數還沒算出來，不顯示", () => {
    expect(nextOutfitHint(people(outfit({ owned: true, cost: 1 })), st(5))).toBe("");
    expect(nextOutfitHint(people(outfit({ owned: false, cost: 1, state: "draft" })), st(5))).toBe(
      "",
    );
    expect(nextOutfitHint(people(outfit({ owned: false, cost: 1, state: "retired" })), st(5))).toBe(
      "",
    );
    expect(nextOutfitHint(people(outfit({ owned: false, cost: 0 })), st(5))).toBe("");
    expect(nextOutfitHint([], st(5))).toBe("");
    expect(nextOutfitHint(people(outfit({ owned: false, cost: 1 })), null)).toBe("");
    expect(nextOutfitHint(people(outfit({ owned: false, cost: 1 })), undefined)).toBe("");
  });

  test("正在看某位角色：照那位的下一套算、說是誰的；總覽照全部（DEV-157）", () => {
    const luna = person({
      persona: "luna",
      name: "露娜",
      outfits: [outfit({ owned: false, cost: 5 })],
    });
    const mira = person({
      persona: "mira",
      name: "米拉",
      outfits: [outfit({ owned: false, cost: 1 })],
    });
    const done = person({
      persona: "done",
      name: "阿基里斯",
      outfits: [outfit({ owned: true, cost: 1 })],
    });
    const all = [luna, mira, done];
    expect(nextOutfitHint(all, st(2))).toBe(T.nextReady);
    expect(nextOutfitHint(all, st(2), luna)).toBe("再 3 點可以換露娜的下一套");
    expect(nextOutfitHint(all, st(5), luna)).toBe("可以換露娜的一套了");
    expect(nextOutfitHint(all, st(0), mira)).toBe(T.nextShortOf(1, "米拉"));
    // 這位沒有可換的：不顯示（不拿別人的衣服來說）。
    expect(nextOutfitHint(all, st(9), done)).toBe("");
  });
});

describe("沒有衣服時照角色包的狀態說明（CLO-25）", () => {
  const fb = { title: "原本的標題", hint: "原本的說明" };

  test("這一版沒有官方衣服包：指標沒有版號", () => {
    expect(packKind({ source: false })).toBe("none");
    const n = emptyNotice({ source: false, latest: "" }, "", fb);
    expect([n.kind, n.title, n.retry]).toEqual(["none", T.packNoneTitle, false]);
  });

  test("正在下載或上次沒成功：會自動再試，給「現在再試」", () => {
    // 有包、還沒同步過
    const first = emptyNotice({ latest: "2026.11.1" }, "", fb);
    expect([first.kind, first.hint, first.retry]).toEqual(["pending", T.packDownloading, true]);
    // 圖下載好了、紀錄等衣櫃起來再寫
    expect(
      emptyNotice(
        { latest: "2026.11.1", registered: "2026.11.1", last: { records_later: true } },
        "",
        fb,
      ).hint,
    ).toBe(T.packDownloading);
    // 上次失敗：說原因
    const failed = emptyNotice(
      {
        latest: "2026.11.1",
        pending: { reason: "下載不到角色包" },
        last: {
          status: "failed",
          reason: "下載不到角色包",
          message: "角色包沒有同步成功：下載不到角色包（什麼都沒換；有網路時會自動再試）",
        },
      },
      "athena",
      fb,
    );
    expect(failed.kind).toBe("pending");
    expect(failed.hint).toBe("上次沒成功：下載不到角色包。會自動再試。");
    expect(failed.retry).toBe(true);
    // 同步成功過、之後已是最新：舊的失敗結果還留在 last，不算沒成功
    expect(
      packKind({ latest: "1", registered: "1", last: { status: "failed", reason: "舊的" } }),
    ).toBe("synced");
    // 待補：沒有上次的原因時用待補的原因；都沒有就不寫冒號
    expect(
      emptyNotice({ latest: "1", registered: "1", pending: { reason: "沒有網路" } }, "", fb).hint,
    ).toBe("上次沒成功：沒有網路。會自動再試。");
    expect(emptyNotice({ latest: "1", registered: "1", pending: {} }, "", fb).hint).toBe(
      "上次沒成功。會自動再試。",
    );
  });

  test("已經同步但沒有衣服：指定了哪位就說這位", () => {
    const ok = { latest: "2026.11.1", registered: "2026.11.1", last: { status: "synced" } };
    const one = emptyNotice(ok, "athena", fb);
    expect([one.kind, one.title, one.retry]).toEqual(["synced", T.packSyncedTitle, false]);
    expect(emptyNotice(ok, "", fb).title).toBe(T.packSyncedAllTitle);
  });

  // 一般家看不到「官方／我的」（CLO-37）：衣服包的說明不說「官方」。
  test("TestPackTextsSayNoOfficial", () => {
    for (const s of [
      T.packNoneTitle,
      T.packNoneHint,
      T.packPendingTitle,
      T.packDownloading,
      T.packFailed("原因"),
      T.packSyncedTitle,
      T.packSyncedAllTitle,
      T.packSyncedHint,
      T.packSyncedAllHint,
    ]) {
      expect(s).not.toContain("官方");
      expect(s).not.toContain("我的");
    }
  });

  test("讀不到角色包狀態、或這個家是角色包的來源：用原本的說明", () => {
    expect(emptyNotice(null, "", fb)).toEqual({ kind: "unknown", ...fb, retry: false });
    expect(emptyNotice(undefined, "athena", fb).title).toBe("原本的標題");
    expect(emptyNotice({ source: true, latest: "1" }, "", fb).kind).toBe("unknown");
  });
});
