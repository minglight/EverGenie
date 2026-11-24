import { beforeEach, describe, expect, test, vi } from "vitest";
import { toCapabilities } from "./caps";
import { toPerson } from "./closet";
import {
  buildView,
  canDraft,
  clipLagsBehind,
  commentFields,
  commentMessage,
  type EditPhoto,
  editHref,
  type Filters,
  filtersItem,
  fullTaken,
  givenRequestId,
  hasClips,
  hasListableHidden,
  hiddenPatch,
  isHiddenOff,
  isOwnerHidden,
  NO_FILTERS,
  nextOrder,
  notesOfOutfit,
  notesOfPhoto,
  parseFilters,
  photoUrl,
  publishBlocked,
  sortPhotos,
  stateAction,
  T,
  toEditNote,
  toEditOutfit,
  toEditPhoto,
  unadoptedOf,
} from "./edit";

// 假的 vault 與名冊（評論、上架、外掛動作的寫入用）。
const h = vi.hoisted(() => {
  const state = {
    db: new Map<string, { fields: Record<string, unknown>; archived: boolean; rev?: number }>(),
    sent: [] as { to?: string; body?: string }[],
    roster: ["art"] as string[],
    nextIdFails: false,
    n: 1,
  };
  const key = (kind?: string, id?: string) => `${kind}/${id}`;
  const err = (code: string) => Object.assign(new Error(code), { code });
  const api = {
    vault: {
      get: async (req: { kind?: string; id?: string }) => {
        const r = state.db.get(key(req.kind, req.id));
        if (!r) throw err("not_found");
        const { rev, ...rest } = structuredClone(r);
        return { record: { kind: req.kind, id: req.id, ...rest, revision: String(rev ?? 1) } };
      },
      list: async (req: { kind?: string; where?: Record<string, string> }) => ({
        records: [...state.db]
          .filter(([k]) => k.startsWith(`${req.kind}/`))
          .map(([k, r]) => ({ id: k.slice(String(req.kind).length + 1), ...structuredClone(r) }))
          .filter((r) => Object.entries(req.where ?? {}).every(([f, v]) => r.fields[f] === v)),
      }),
      patch: async (req: {
        kind?: string;
        id?: string;
        set?: Record<string, unknown>;
        expected_revision?: string;
      }) => {
        const r = state.db.get(key(req.kind, req.id));
        if (!r) throw err("not_found");
        if (req.expected_revision && req.expected_revision !== String(r.rev ?? 1))
          throw err("conflict");
        Object.assign(r.fields, req.set ?? {});
        r.rev = (r.rev ?? 1) + 1;
        return { record: {} };
      },
      put: async (req: {
        record: { kind: string; id: string; fields: Record<string, unknown> };
      }) => {
        state.db.set(key(req.record.kind, req.record.id), {
          fields: structuredClone(req.record.fields),
          archived: false,
        });
        return {};
      },
      nextId: async (req: { kind?: string }) => {
        if (state.nextIdFails) throw err("invalid");
        return { id: req.kind === "closet.note" ? `CN-${state.n++}` : `M-${state.n++}` };
      },
    },
    web: {
      getRoster: async () => ({ agents: state.roster.map((id) => ({ id, enabled: true })) }),
      getAgents: async () => ({ agents: state.roster.map((id) => ({ id, enabled: true })) }),
    },
    bus: {
      send: async (req: { to?: string; body?: string }) => {
        state.sent.push(req);
        return {};
      },
    },
  };
  return { state, api };
});
vi.mock("@evergenie/sdk-web", () => ({ api: h.api }));

import {
  sendAction,
  sendEditComment,
  setOutfitState,
  setPhotoAdopted,
  setPhotoHidden,
  setPhotoType,
} from "./store";

const ph = (id: string, f: Partial<Record<string, unknown>>): EditPhoto =>
  toEditPhoto({
    id,
    fields: {
      persona: "amy",
      outfit: "amy-sea",
      type: "half",
      order: 1,
      adopted: true,
      source: "official",
      file: `${id}.png`,
      ...f,
    },
  });

const outfits = [
  toEditOutfit({
    id: "amy-sea",
    fields: {
      persona: "amy",
      outfit: "sea",
      name: "海邊",
      state: "draft",
      source: "official",
      order: 2,
    },
  }),
  toEditOutfit({
    id: "amy-my-1",
    fields: {
      persona: "amy",
      outfit: "my-1",
      name: "自己的",
      state: "active",
      source: "mine",
      order: 1,
    },
  }),
  toEditOutfit({
    id: "amy-base",
    fields: { persona: "amy", outfit: "base", name: "第一套", state: "active", source: "official" },
  }),
  toEditOutfit({
    id: "bob-base",
    fields: { persona: "bob", outfit: "base", name: "鮑伯", state: "active", source: "official" },
  }),
];

const photos = [
  ph("amy-sea--half1", { adopted: false }),
  ph("amy-sea--half2", { order: 2 }),
  ph("amy-sea--half1-h", { hidden: true, adopted: false }),
  ph("amy-my-1--half1", { outfit: "amy-my-1", source: "mine" }),
  ph("amy-base--half1", { outfit: "amy-base", hidden: true }),
  ph("bob-base--half1", { persona: "bob", outfit: "bob-base", adopted: false }),
];

// 外掛動作（測試用的登記套件 studio）：單張的「留一份」帶篩選鈕「留著的」。
const caps = toCapabilities({
  fields: {
    supports: ["mine", "edit"],
    actions: [
      {
        id: "studio-keep",
        package: "studio",
        label: "留一份",
        scope: "photo",
        request_kind: "studio.mark",
        request_fields: ["persona", "outfit", "photo"],
        request_set: { state: "kept" },
        status_kind: "studio.mark",
        status_field: "state",
        status_labels: { kept: "留著" },
        filter_label: "留著的",
        filter_values: ["kept"],
      },
    ],
  },
});
const statuses = {
  "studio.mark": [
    {
      fields: { persona: "amy", outfit: "amy-sea", photo: "amy-sea--half2", state: "kept" },
      updated_at: "2026-11-15T01:00:00Z",
    },
  ],
};

const view = (f: Partial<Filters>) =>
  buildView({
    persona: "amy",
    filters: { ...NO_FILTERS, ...f },
    outfits,
    photos,
    people: [],
    caps,
    statuses,
  });
const shape = (f: Partial<Filters>) =>
  view(f).map((s) => [
    s.persona,
    s.blocks.map((b) => [b.outfit.record, b.photos.map((p) => p.id)]),
  ]);

describe("「修改」分頁的篩選", () => {
  test("TestEditFiltersIndependent", () => {
    // 都關：這位角色每一套（照 order，沒寫的最後），隱藏版的不列。
    expect(shape({})).toEqual([
      [
        "amy",
        [
          ["amy-my-1", ["amy-my-1--half1"]],
          ["amy-sea", ["amy-sea--half1", "amy-sea--half2"]],
          ["amy-base", []],
        ],
      ],
    ]);
    // 「顯示隱藏版」關著只是不列，那套照列，並記下有幾張沒列。
    expect(view({})[0]?.blocks.find((b) => b.outfit.record === "amy-base")?.hiddenOut).toBe(1);
    // 官方：只看官方的衣服。
    expect(shape({ official: true })[0]?.[1]).toEqual([
      ["amy-sea", ["amy-sea--half1", "amy-sea--half2"]],
      ["amy-base", []],
    ]);
    // 開發中：只看沒採用的；一張都不符合的那套不列。
    expect(shape({ wip: true })[0]?.[1]).toEqual([["amy-sea", ["amy-sea--half1"]]]);
    // 顯示隱藏版：一起列。
    expect(shape({ hidden: true })[0]?.[1]).toEqual([
      ["amy-my-1", ["amy-my-1--half1"]],
      ["amy-sea", ["amy-sea--half1", "amy-sea--half2", "amy-sea--half1-h"]],
      ["amy-base", ["amy-base--half1"]],
    ]);
    // 外掛篩選鈕：只留狀態是 filter_values 的。
    expect(shape({ actions: ["studio-keep"] })[0]?.[1]).toEqual([["amy-sea", ["amy-sea--half2"]]]);
  });

  test("TestEditFiltersIntersect", () => {
    // 同時開＝交集。
    expect(shape({ wip: true, hidden: true })[0]?.[1]).toEqual([
      ["amy-sea", ["amy-sea--half1", "amy-sea--half1-h"]],
    ]);
    expect(shape({ official: true, hidden: true, wip: true })[0]?.[1]).toEqual([
      ["amy-sea", ["amy-sea--half1", "amy-sea--half1-h"]],
    ]);
    // 開發中＋外掛篩選：half2 已採用，沒有符合的。
    expect(shape({ wip: true, actions: ["studio-keep"] })[0]?.[1]).toEqual([]);
    // 符合的只有隱藏版、而隱藏版關著：那套也不列。
    const onlyHidden = buildView({
      persona: "amy",
      filters: { ...NO_FILTERS, wip: true },
      outfits,
      photos: [ph("amy-sea--half1-h", { hidden: true, adopted: false })],
      people: [],
      caps,
      statuses,
    });
    expect(onlyHidden[0]?.blocks).toEqual([]);
    // 不認得的外掛篩選鈕略過。
    expect(shape({ actions: ["gone"] })).toEqual(shape({}));
  });

  // 隱藏版開關關著不留框（作者 11-15，S3a）：檢視的 hidden_off 列的隱藏版整張不列、不算進 hiddenOut，
  // 「顯示隱藏版」也不出現；上架照樣看每一張（all 含它們），沒採用的張數另外記。沒有檢視時照舊列（佔位）。
  test("TestEditHiddenOffNotListed", () => {
    const off = [
      toPerson({
        id: "amy",
        fields: {
          persona: "amy",
          outfits: [],
          hidden_off: ["amy-sea--half1-h", "amy-base--half1"],
        },
      }),
    ];
    const sections = buildView({
      persona: "amy",
      filters: { ...NO_FILTERS, hidden: true },
      outfits,
      photos,
      people: off,
      caps,
      statuses,
    });
    const blocks = sections[0]?.blocks ?? [];
    const ids = blocks.flatMap((b) => b.photos.map((p) => p.id));
    expect(ids).not.toContain("amy-sea--half1-h");
    expect(ids).not.toContain("amy-base--half1");
    const sea = blocks.find((b) => b.outfit.record === "amy-sea");
    expect(sea?.all.map((p) => p.id)).toContain("amy-sea--half1-h");
    expect(sea?.offLeft).toBe(1);
    expect(publishBlocked(sea?.outfit as never, sea?.all ?? [])).toBe(2);
    const closed = buildView({
      persona: "amy",
      filters: NO_FILTERS,
      outfits,
      photos,
      people: off,
      caps,
      statuses,
    })[0]?.blocks;
    expect(closed?.find((b) => b.outfit.record === "amy-base")?.hiddenOut).toBe(0);
    expect(hasListableHidden(photos, off, "amy", false)).toBe(false);
    expect(hasListableHidden(photos, [], "amy", false)).toBe(true);
    expect(isHiddenOff(ph("amy-sea--half1-h", { hidden: true }), [])).toBe(false);
    expect(isHiddenOff(ph("amy-sea--half1", {}), off)).toBe(false);
    // 展示間入口的張數也不算開關關著的（跟點過去看到的一樣）。
    expect(unadoptedOf("amy", outfits, photos, off).count).toBe(1);
    expect(unadoptedOf("amy", outfits, photos).hidden).toBe(1);
  });

  test("TestEditAllPersonas", () => {
    const people = [
      toPerson({
        id: "bob",
        fields: { persona: "bob", name: "鮑伯", agent: "life", outfits: [{ record: "bob-base" }] },
      }),
      toPerson({
        id: "amy",
        fields: { persona: "amy", name: "艾咪", outfits: [{ record: "amy-sea" }] },
      }),
    ];
    const all = buildView({
      persona: "amy",
      filters: { ...NO_FILTERS, all: true },
      outfits,
      photos,
      people,
      caps,
      statuses,
    });
    // 一位一段，順序同頭像列（有人擔任職位的在前），名字用檢視的。
    expect(all.map((s) => [s.persona, s.name, s.total])).toEqual([
      ["bob", "鮑伯", 1],
      ["amy", "艾咪", 5],
    ]);
    // 只看這位：一筆照片都沒有時 total 是 0（頁面畫空狀態）。
    expect(
      buildView({ persona: "cat", filters: NO_FILTERS, outfits, photos, people, caps, statuses }),
    ).toEqual([{ persona: "cat", name: "cat", total: 0, blocks: [] }]);
  });

  test("TestEditFiltersInUrl", () => {
    expect(parseFilters("")).toEqual(NO_FILTERS);
    const f = parseFilters("hidden,wip,a:studio-keep,nope,a:");
    expect(f).toEqual({
      all: false,
      official: false,
      wip: true,
      hidden: true,
      actions: ["studio-keep"],
    });
    // 固定順序，都關是空字串（網址沒有 ?item=）。
    expect(filtersItem(f)).toBe("wip,hidden,a:studio-keep");
    expect(filtersItem(NO_FILTERS)).toBe("");
    expect(
      parseFilters(
        filtersItem({ all: true, official: true, wip: false, hidden: false, actions: ["b", "a"] }),
      ),
    ).toEqual({
      all: true,
      official: true,
      wip: false,
      hidden: false,
      actions: ["a", "b"],
    });
    // 開發家展示間的入口（CLO-37）：到那位的「修改」分頁、打開「開發中」；有隱藏版沒採用的再打開「顯示隱藏版」。
    const wip = new URL(editHref("my_fox", { hidden: 0, ready: 0 }), "http://x");
    expect(wip.pathname).toBe("/personas/my_fox");
    expect(wip.searchParams.get("tab")).toBe("closet.edit");
    expect(parseFilters(wip.searchParams.get("item") ?? "")).toEqual({
      ...NO_FILTERS,
      wip: true,
    });
    const both = new URL(editHref("athena", { hidden: 2, ready: 0 }), "http://x");
    expect(both.searchParams.get("item")).toBe("wip,hidden");
    // 有等上架的整套（CLO-38）：不打開「開發中」（它只列沒採用的照片，等上架的那套會被篩掉）。
    const ready = new URL(editHref("athena", { hidden: 0, ready: 1 }), "http://x");
    expect(ready.searchParams.has("item")).toBe(false);
    expect(parseFilters(ready.searchParams.get("item") ?? "")).toEqual(NO_FILTERS);
  });
});

describe("「修改」分頁的照片", () => {
  test("TestEditSortPhotos", () => {
    const list = [
      ph("v2", { type: "video", order: 2, clip: 1, of: "h2" }),
      ph("full", { type: "full", order: 1 }),
      ph("v1b", { type: "video", order: 1, clip: 2, of: "h1" }),
      ph("h2", { order: 2 }),
      ph("h1h", { order: 1, hidden: true }),
      ph("lonely", { type: "video", order: 9, clip: 1, of: "gone" }),
      ph("h1", { order: 1 }),
      ph("v1a", { type: "video", order: 1, clip: 1, of: "h1" }),
    ];
    // 半身（一般版在前、再照 order）→ 全身 → 影片（跟著配的那張，同一張照 clip；配的那張不在的排最後）。
    expect(sortPhotos(list).map((p) => p.id)).toEqual([
      "h1",
      "h2",
      "h1h",
      "full",
      "v1a",
      "v1b",
      "v2",
      "lonely",
    ]);
  });

  test("TestEditPhotoUrl", () => {
    // 照編號查 closet.person 的 photo_urls（衣櫃的程式每一輪寫的，CLO-35），不照位置猜：
    // 改了類型或一般／隱藏之後，衣櫃重新發佈，同一個編號就對到新的公開檔。
    const people = [
      toPerson({
        id: "amy",
        fields: {
          persona: "amy",
          outfits: [],
          photo_urls: {
            "amy-sea--half1": "/pkg-files/closet/o/amy/sea/1.png",
            "amy-sea--half1-h": "/pkg-files/closet/o/amy/sea/h-1.png?v=h",
            "amy-sea--video1-c2": "/pkg-files/closet/o/amy/sea/1+2.mp4",
            "amy-sea--half2": "/pkg-files/closet/f/amy/sea/1.png",
            "amy-sea--bad": 3,
          },
        },
      }),
    ];
    const at = (id: string, f: Partial<Record<string, unknown>> = {}) =>
      photoUrl(ph(id, f), people);
    expect(at("amy-sea--half1")).toBe("/pkg-files/closet/o/amy/sea/1.png");
    expect(at("amy-sea--half1-h", { hidden: true })).toBe(
      "/pkg-files/closet/o/amy/sea/h-1.png?v=h",
    );
    expect(at("amy-sea--video1-c2", { type: "video", clip: 2 })).toBe(
      "/pkg-files/closet/o/amy/sea/1+2.mp4",
    );
    // 本來是第 2 張半身、改成全身之後：網址看編號，不看類型與 order。
    expect(at("amy-sea--half2", { type: "full", order: 2 })).toBe(
      "/pkg-files/closet/f/amy/sea/1.png",
    );
    // 這一輪沒公開（隱藏版沒打開、同一版的第二張全身照、還沒發佈）、別位角色、值不是網址：空字串，畫面顯示佔位。
    expect(at("amy-sea--half3")).toBe("");
    expect(at("amy-sea--half1", { persona: "bob" })).toBe("");
    expect(at("amy-sea--bad")).toBe("");
  });

  test("TestEditFullTakenAndOwnerHidden", () => {
    const list = [ph("f", { type: "full" }), ph("h", {}), ph("fh", { type: "full", hidden: true })];
    const [f, half] = list as [EditPhoto, EditPhoto, EditPhoto];
    expect(fullTaken(list, half, false)).toBe(true); // 一般版已經有 f
    expect(fullTaken(list, f, false)).toBe(false); // 自己不算
    expect(fullTaken([ph("f", { type: "full" })], half, true)).toBe(false);
    expect(fullTaken([ph("f", { type: "full", outfit: "amy-x" })], half, false)).toBe(false);
    const people = [
      toPerson({ id: "amy", fields: { persona: "amy", outfits: [], owner_hidden: ["h", 3, ""] } }),
    ];
    expect(people[0]?.ownerHidden).toEqual(["h"]);
    expect(isOwnerHidden(half, people)).toBe(true);
    expect(isOwnerHidden(f, people)).toBe(false);
    expect(
      isOwnerHidden({ id: "h", persona: "bob", hidden: true, source: "official" }, people),
    ).toBe(false);
    // 紀錄本身是自己的隱藏版（source = mine、hidden）：檢視還沒算進去也算。
    expect(isOwnerHidden(ph("m", { hidden: true, source: "mine" }), people)).toBe(true);
    expect(isOwnerHidden(ph("m", { hidden: false, source: "mine" }), people)).toBe(false);
    // 影片跟配的半身不同版：回那張半身（畫面給「跟上半身」）；同版、不是影片、配的不在都不算。
    const clips = [
      ph("a", { hidden: true }),
      ph("v", { type: "video", of: "a", hidden: false }),
      ph("w", { type: "video", of: "a", hidden: true }),
      ph("x", { type: "video", of: "gone", hidden: false }),
    ];
    const [a, v, w, x] = clips as [EditPhoto, EditPhoto, EditPhoto, EditPhoto];
    expect(clipLagsBehind(clips, v)?.id).toBe("a");
    expect(clipLagsBehind(clips, w)).toBeUndefined();
    expect(clipLagsBehind(clips, x)).toBeUndefined();
    expect(clipLagsBehind(clips, a)).toBeUndefined();
  });

  test("TestEditHiddenPatchAvoidsClashAndCarriesClips", () => {
    const list = [
      ph("h1", { order: 1 }),
      ph("h2", { order: 2 }),
      ph("h1h", { order: 1, hidden: true }),
      ph("v1", { type: "video", order: 1, of: "h1", clip: 1 }),
      ph("f", { type: "full" }),
    ];
    const [h1, h2, , , f] = list as [EditPhoto, EditPhoto, EditPhoto, EditPhoto, EditPhoto];
    // h1 改成隱藏版：隱藏版已經有 order 1（衣櫃一版裡同一個 order 只公開一張）→ 放到半身最後一張；配它的影片一起改。
    expect(hiddenPatch(list, h1, true)).toEqual({
      set: { hidden: true, order: 3 },
      videos: ["v1"],
    });
    // h2 改成隱藏版：沒撞號，order 不動、沒有影片。
    expect(hiddenPatch(list, h2, true)).toEqual({ set: { hidden: true }, videos: [] });
    // 全身照只改 hidden。
    expect(hiddenPatch(list, f, true)).toEqual({ set: { hidden: true }, videos: [] });
  });

  test("TestEditRetypeAndPublish", () => {
    const list = [
      ph("a", { order: 1 }),
      ph("b", { order: 3 }),
      ph("c", { type: "full", order: 1 }),
    ];
    // 改類型放到那一類最後一張。
    expect(nextOrder(list, list[0] as EditPhoto, "full")).toBe(2);
    expect(nextOrder(list, list[2] as EditPhoto, "half")).toBe(4);
    expect(nextOrder([], list[0] as EditPhoto, "full")).toBe(1);
    // 開發中的那套：每張都採用了才能上架。
    expect(publishBlocked({ state: "draft" }, [ph("a", { adopted: false }), ph("b", {})])).toBe(1);
    expect(publishBlocked({ state: "draft" }, [ph("b", {})])).toBe(0);
    expect(publishBlocked({ state: "active" }, [ph("a", { adopted: false })])).toBe(0);
    expect(stateAction("active")).toEqual({ label: "下架", to: "retired" });
    expect(stateAction("retired").to).toBe("active");
    expect(stateAction("draft")).toEqual({ label: "上架", to: "active" });
  });
});

describe("「修改」分頁的評論", () => {
  const sea = outfits[0] as (typeof outfits)[number];

  test("TestEditCommentFields", () => {
    expect(
      commentFields(sea, ph("amy-sea--half2", { order: 2, hidden: true }), " 臉柔和一點 "),
    ).toEqual({
      persona: "amy",
      outfit: "sea",
      record: "amy-sea",
      comment: "臉柔和一點",
      state: "open",
      intent: "comment",
      scope: "photo",
      photo: "amy-sea--half2",
      piece: "half-2",
      type: "half",
      level: "隱藏版",
      slot: 2,
    });
    expect(commentFields(sea, ph("v", { type: "video", order: 1, clip: 2 }), "x")).toMatchObject({
      piece: "video-1-2",
      type: "video",
      slot: 1,
      clip: 2,
      level: "標準",
    });
    expect(commentFields(sea, ph("f", { type: "full" }), "x")).not.toHaveProperty("slot");
    // 整套：piece、type 照固定寫法，不寫 photo。
    expect(commentFields(sea, null, "整套換個色")).toEqual({
      persona: "amy",
      outfit: "sea",
      record: "amy-sea",
      comment: "整套換個色",
      state: "open",
      intent: "comment",
      scope: "outfit",
      piece: "outfit",
      type: "half",
    });
    const msg = commentMessage(
      { persona: "amy", name: "艾咪" },
      sea,
      ph("amy-sea--half2", { order: 2 }),
      "CN-7",
      "臉",
    );
    expect(msg).toContain("closet.note/CN-7");
    // 換圖一律用換圖請求，系統自動回到還沒採用（M4）；不再叫秘書自己改 adopted。
    expect(msg).toContain("換圖請求（closet-mine.request 帶 photo_id = amy-sea--half2）");
    expect(msg).toContain("系統自動回到還沒採用，不要自己改 adopted");
    expect(msg).not.toContain("改回 false");
    expect(msg).toContain("「臉」");
  });

  test("TestEditNotesHistory", () => {
    const p = ph("amy-sea--half2", { order: 2 });
    const notes = [
      {
        id: "CN-1",
        fields: {
          persona: "amy",
          outfit: "sea",
          record: "amy-sea",
          piece: "half-2",
          level: "標準",
          comment: "舊的",
          state: "redone",
          result: "換好了",
          created_at: "2026-11-01T00:00:00Z",
        },
      },
      {
        id: "CN-2",
        fields: {
          persona: "amy",
          outfit: "sea",
          record: "amy-sea",
          piece: "h:half-2",
          comment: "隱藏版的",
          state: "open",
        },
      },
      {
        id: "CN-3",
        fields: {
          persona: "amy",
          outfit: "sea",
          record: "amy-sea",
          piece: "half-2",
          photo: "amy-sea--half2",
          scope: "photo",
          comment: "新的",
          state: "open",
          created_at: "2026-11-15T00:00:00Z",
        },
      },
      {
        id: "CN-4",
        fields: {
          persona: "amy",
          outfit: "sea",
          record: "amy-sea",
          piece: "outfit",
          type: "half",
          scope: "outfit",
          comment: "整套",
          state: "dismissed",
          result: "不用改",
        },
      },
      {
        id: "CN-5",
        fields: {
          persona: "amy",
          outfit: "sea",
          record: "amy-sea",
          piece: "half-2",
          photo: "amy-sea--half2-h",
          comment: "別張",
          state: "open",
        },
      },
    ].map(toEditNote);
    // 這張的：photo 指到它的、舊的照 piece 與版本對；新的在前。
    expect(notesOfPhoto(notes, p).map((n) => n.id)).toEqual(["CN-3", "CN-1"]);
    expect(
      notesOfPhoto(notes, ph("amy-sea--half2-x", { order: 2, hidden: true })).map((n) => n.id),
    ).toEqual(["CN-2"]);
    expect(notesOfOutfit(notes, "amy-sea").map((n) => [n.id, n.state, n.result])).toEqual([
      ["CN-4", "dismissed", "不用改"],
    ]);
  });

  test("TestGivenRequestId", () => {
    expect(givenRequestId("studio-keep", 36 ** 3)).toBe("studio-keep-1000");
    expect(givenRequestId("_X y", 1)).toMatch(/^[a-z0-9][a-z0-9_-]*$/);
  });
});

describe("「修改」分頁的寫入", () => {
  beforeEach(() => {
    h.state.db.clear();
    h.state.sent = [];
    h.state.roster = ["art"];
    h.state.nextIdFails = false;
    h.state.n = 1;
  });
  const sea = toEditOutfit({
    id: "amy-sea",
    fields: { persona: "amy", outfit: "sea", name: "海邊", state: "draft" },
  });

  test("TestEditCommentWritesNoteAndTellsArt", async () => {
    const p = ph("amy-sea--half1", { adopted: true });
    h.state.db.set("closet.photo/amy-sea--half1", { fields: { ...p }, archived: false });
    const r = await sendEditComment({ persona: "amy", name: "艾咪" }, sea, p, "再亮一點");
    expect(r).toEqual({ art: "ready" });
    expect(h.state.db.get("closet.note/CN-1")?.fields).toMatchObject({
      scope: "photo",
      photo: "amy-sea--half1",
      intent: "comment",
      state: "open",
      comment: "再亮一點",
    });
    expect(h.state.sent).toHaveLength(1);
    expect(h.state.sent[0]?.to).toBe("art");
    // 評論本身不改採用。
    expect(h.state.db.get("closet.photo/amy-sea--half1")?.fields.adopted).toBe(true);
    // 家裡沒有生圖秘書：照樣記下、不傳話。
    h.state.roster = [];
    const r2 = await sendEditComment({ persona: "amy", name: "艾咪" }, sea, null, "整套");
    expect(r2.art).toBe("none");
    expect(h.state.db.get("closet.note/CN-2")?.fields).toMatchObject({
      scope: "outfit",
      piece: "outfit",
    });
    expect(h.state.sent).toHaveLength(1);
    await expect(
      sendEditComment({ persona: "amy", name: "艾咪" }, sea, null, "  "),
    ).rejects.toThrow();
  });

  test("TestEditPublishNeedsEveryPhotoAdopted", async () => {
    h.state.db.set("closet.outfit/amy-sea", {
      fields: { persona: "amy", outfit: "sea", state: "draft" },
      archived: false,
    });
    h.state.db.set("closet.photo/a", {
      fields: { outfit: "amy-sea", type: "half", order: 1, adopted: false },
      archived: false,
    });
    await expect(setOutfitState("amy-sea", "active")).rejects.toThrow("還有 1 張沒採用");
    expect(h.state.db.get("closet.outfit/amy-sea")?.fields.state).toBe("draft");
    (h.state.db.get("closet.photo/a") as { fields: Record<string, unknown> }).fields.adopted = true;
    await setOutfitState("amy-sea", "active");
    expect(h.state.db.get("closet.outfit/amy-sea")?.fields.state).toBe("active");
    await setOutfitState("amy-sea", "retired");
    expect(h.state.db.get("closet.outfit/amy-sea")?.fields.state).toBe("retired");
    // 放回開發中（S1）：只給自己的衣服、不是基本款；官方的不給（寫入前重讀判斷）。
    await expect(setOutfitState("amy-sea", "draft")).rejects.toThrow(T.draftMineOnly);
    h.state.db.set("closet.outfit/amy-my-1", {
      fields: { persona: "amy", outfit: "my-1", state: "active", source: "mine" },
      archived: false,
    });
    await setOutfitState("amy-my-1", "draft");
    expect(h.state.db.get("closet.outfit/amy-my-1")?.fields.state).toBe("draft");
    expect(canDraft({ source: "mine", outfit: "my-1", state: "retired" })).toBe(true);
    expect(canDraft({ source: "mine", outfit: "my-1", state: "draft" })).toBe(false);
    expect(canDraft({ source: "mine", outfit: "base", state: "active" })).toBe(false);
    expect(canDraft({ source: "official", outfit: "sea", state: "active" })).toBe(false);
  });

  // 按「採用」寫入前比對畫面那張（S2）：秘書剛換了圖（檔或版本變了）就不寫，請主人再看一次。
  test("TestAdoptRefusesReplacedPhoto", async () => {
    h.state.db.set("closet.photo/p", {
      fields: { outfit: "amy-sea", type: "half", order: 1, file: "photos/p.png", adopted: false },
      archived: false,
    });
    const seen = toEditPhoto((await h.api.vault.get({ kind: "closet.photo", id: "p" })).record);
    expect(seen.revision).toBe("1");
    // 換圖請求換了新圖（同一個檔名、版本變了）：拒絕，沒寫。
    const rec = h.state.db.get("closet.photo/p") as {
      fields: Record<string, unknown>;
      rev?: number;
    };
    rec.rev = 2;
    await expect(setPhotoAdopted("p", true, seen)).rejects.toThrow("這張剛換過，請再看一次");
    expect(rec.fields.adopted).toBe(false);
    // 檔換了也一樣。
    await expect(
      setPhotoAdopted("p", true, { file: "photos/old.png", revision: "2" }),
    ).rejects.toThrow(T.photoChanged);
    // 畫面上就是現在這張：寫進去。
    await setPhotoAdopted("p", true, { file: "photos/p.png", revision: "2" });
    expect(rec.fields.adopted).toBe(true);
  });

  test("TestEditRetypeGoesLast", async () => {
    h.state.db.set("closet.photo/a", {
      fields: { outfit: "amy-sea", type: "half", order: 1 },
      archived: false,
    });
    // 隱藏版那邊已經有一張全身照（order 1）：一般版的 a 改成全身，order 照「全身最後一張」算（兩版一起算）→ 2。
    h.state.db.set("closet.photo/f", {
      fields: { outfit: "amy-sea", type: "full", order: 1, hidden: true },
      archived: false,
    });
    h.state.db.set("closet.photo/v", {
      fields: { outfit: "amy-sea", type: "video", order: 1 },
      archived: false,
    });
    await setPhotoType("a", "full");
    expect(h.state.db.get("closet.photo/a")?.fields).toMatchObject({ type: "full", order: 2 });
    // 全身改回半身：放到半身最後一張。
    await setPhotoType("f", "half");
    expect(h.state.db.get("closet.photo/f")?.fields).toMatchObject({ type: "half", order: 1 });
    // 影片不能改類型。
    await setPhotoType("v", "half");
    expect(h.state.db.get("closet.photo/v")?.fields.type).toBe("video");
  });

  test("TestEditRetypeQueuedAndPairedBlocked", async () => {
    for (const [id, f] of [
      ["a", { type: "half", order: 1, hidden: false }],
      ["b", { type: "half", order: 2, hidden: false }],
      ["c", { type: "half", order: 3, hidden: false }],
      ["f", { type: "full", order: 1, hidden: true }],
      ["v", { type: "video", order: 3, of: "c", clip: 1, hidden: false }],
    ] as const) {
      h.state.db.set(`closet.photo/${id}`, {
        fields: { outfit: "amy-sea", ...f },
        archived: false,
      });
    }
    // 同一套連按兩張改全身：排隊一個接一個——第一張放到全身最後（隱藏版的全身是 1 → 2），
    // 第二張看到一般版已經有全身照就不給改（一版只公開一張，不做出永遠是佔位的那張）。
    const res = await Promise.allSettled([setPhotoType("a", "full"), setPhotoType("b", "full")]);
    expect(res.map((r) => r.status).sort()).toEqual(["fulfilled", "rejected"]);
    const rejected = res.find((r) => r.status === "rejected") as PromiseRejectedResult;
    expect(String(rejected.reason)).toContain(T.fullTaken);
    const types = ["a", "b"].map((id) => h.state.db.get(`closet.photo/${id}`)?.fields);
    expect(types.filter((f) => f?.type === "full").map((f) => f?.order)).toEqual([2]);
    // 配了影片的半身不能改成全身（影片只配半身照）：說清楚、不改。
    await expect(setPhotoType("c", "full")).rejects.toThrow(T.retypeHasVideo);
    expect(h.state.db.get("closet.photo/c")?.fields.type).toBe("half");
    expect(hasClips([ph("c", {}), ph("v", { type: "video", of: "c" })], { id: "c" })).toBe(true);
    expect(hasClips([ph("c", {})], { id: "c" })).toBe(false);
  });

  test("TestEditSetHiddenMovesClipsAlong", async () => {
    for (const [id, f] of [
      ["a", { type: "half", order: 1, hidden: false }],
      ["ah", { type: "half", order: 1, hidden: true }],
      ["v", { type: "video", order: 1, of: "a", clip: 1, hidden: false }],
    ] as const) {
      h.state.db.set(`closet.photo/${id}`, {
        fields: { outfit: "amy-sea", ...f },
        archived: false,
      });
    }
    // a 改成隱藏版：隱藏版的 order 1 已經有了 → 放到最後；配 a 的影片跟著變隱藏版（不然衣櫃不會公開它）。
    await setPhotoHidden("a", true);
    expect(h.state.db.get("closet.photo/a")?.fields).toMatchObject({ hidden: true, order: 2 });
    expect(h.state.db.get("closet.photo/v")?.fields.hidden).toBe(true);
    // 影片不能自己改一般／隱藏：說清楚、不改。
    await expect(setPhotoHidden("v", false)).rejects.toThrow(T.videoLevelFollows);
    expect(h.state.db.get("closet.photo/v")?.fields.hidden).toBe(true);
    // 上次寫到一半（半身改好了、影片沒跟上）：再按一次同一版，把影片補齊。
    h.state.db.set("closet.photo/v", {
      fields: { outfit: "amy-sea", type: "video", order: 1, of: "a", clip: 1, hidden: false },
      archived: false,
    });
    await setPhotoHidden("a", true);
    expect(h.state.db.get("closet.photo/v")?.fields.hidden).toBe(true);
    expect(h.state.db.get("closet.photo/a")?.fields.order).toBe(2);
  });

  test("TestEditOwnerHiddenAndSecondFullRefused", async () => {
    h.state.db.set("closet.person/amy", {
      fields: { persona: "amy", outfits: [], owner_hidden: ["mh"] },
      archived: false,
    });
    for (const [id, f] of [
      ["mh", { type: "half", order: 1, hidden: true, source: "mine" }],
      ["oh", { type: "half", order: 2, hidden: true, source: "official" }],
      ["f", { type: "full", order: 1, hidden: false, source: "official" }],
      ["fh", { type: "full", order: 2, hidden: true, source: "official" }],
    ] as const) {
      h.state.db.set(`closet.photo/${id}`, {
        fields: { persona: "amy", outfit: "amy-sea", ...f },
        archived: false,
      });
    }
    // 自己的隱藏版（檢視的 owner_hidden）不給改成一般版：改了就不再跟著「自己的隱藏版」開關。
    await expect(setPhotoHidden("mh", false)).rejects.toThrow(T.ownerHiddenStays);
    expect(h.state.db.get("closet.photo/mh")?.fields.hidden).toBe(true);
    // 自己的（source = mine）隱藏版：檢視還沒把它算進 owner_hidden 也不給改（剛切成隱藏版的那幾秒）。
    h.state.db.set("closet.photo/mh2", {
      fields: {
        persona: "amy",
        outfit: "amy-sea",
        type: "half",
        order: 7,
        hidden: true,
        source: "mine",
      },
      archived: false,
    });
    await expect(setPhotoHidden("mh2", false)).rejects.toThrow(T.ownerHiddenStays);
    // 跟著半身改成一般版的影片是自己的隱藏版：整個不給改（不然影片會跟一般版一起公開）。
    h.state.db.set("closet.photo/ov", {
      fields: {
        persona: "amy",
        outfit: "amy-sea",
        type: "video",
        order: 2,
        of: "oh",
        clip: 1,
        hidden: true,
        source: "mine",
      },
      archived: false,
    });
    await expect(setPhotoHidden("oh", false)).rejects.toThrow(T.ownerHiddenStays);
    expect(h.state.db.get("closet.photo/oh")?.fields.hidden).toBe(true);
    h.state.db.delete("closet.photo/ov");
    // 官方的隱藏版照樣能改成一般版。
    await setPhotoHidden("oh", false);
    expect(h.state.db.get("closet.photo/oh")?.fields.hidden).toBe(false);
    // 一般版已經有全身照：全身照從隱藏版搬過來、半身改成全身都不給做（一版只公開一張）。
    await expect(setPhotoHidden("fh", false)).rejects.toThrow(T.fullTaken);
    await expect(setPhotoType("oh", "full")).rejects.toThrow(T.fullTaken);
    expect(h.state.db.get("closet.photo/oh")?.fields.type).toBe("half");
    // 檢視讀不到（還沒算好）：寧可多擋。
    h.state.db.delete("closet.person/amy");
    h.state.db.set("closet.photo/oh2", {
      fields: { persona: "amy", outfit: "amy-sea", type: "half", order: 5, hidden: true },
      archived: false,
    });
    await expect(setPhotoHidden("oh2", false)).rejects.toThrow(T.ownerHiddenStays);
  });

  test("TestEditActionWritesRequest", async () => {
    const [a] = caps.actions;
    await sendAction(a as (typeof caps.actions)[number], {
      persona: "amy",
      outfit: "amy-sea",
      photo: "p1",
    });
    expect(h.state.db.get("studio.mark/M-1")?.fields).toEqual({
      state: "kept",
      persona: "amy",
      outfit: "amy-sea",
      photo: "p1",
    });
    // 那種資料不能 next_id（given）：自己給編號。
    h.state.nextIdFails = true;
    await sendAction(a as (typeof caps.actions)[number], {
      persona: "amy",
      outfit: "amy-sea",
      photo: "p2",
    });
    expect(
      [...h.state.db.keys()].some((k) => /^studio\.mark\/studio-keep-[a-z0-9]+$/.test(k)),
    ).toBe(true);
    // 缺了要帶的欄位：不送。
    await expect(
      sendAction(a as (typeof caps.actions)[number], { persona: "amy" }),
    ).rejects.toThrow();
  });
});
