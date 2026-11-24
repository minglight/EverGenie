import { beforeEach, describe, expect, test, vi } from "vitest";

// 假的 vault：記憶體裡的紀錄＋revision，patch 照 expected_revision 檢查（對不上丟 conflict），每次呼叫讓出一下事件迴圈，才抓得到連按的競態。
const h = vi.hoisted(() => {
  interface Rec {
    fields: Record<string, unknown>;
    archived: boolean;
    revision: number;
  }
  const state = {
    db: new Map<string, Rec>(),
    log: [] as string[],
    fail: new Set<string>(),
    sent: [] as { to?: string; body?: string }[],
    nextN: 1,
    beforePatch: undefined as undefined | ((kind?: string) => void),
    // 名冊：id → 啟用中與否（get_roster 只回啟用中的，get_agents 回全部）；預設家裡有啟用中的 art。
    agents: new Map<string, boolean>([["art", true]]),
  };
  const tick = () => new Promise<void>((r) => setTimeout(r, 0));
  const key = (kind?: string, id?: string) => `${kind}/${id}`;
  const err = (code: string, message = code) => Object.assign(new Error(message), { code });
  const api = {
    vault: {
      get: async (req: { kind?: string; id?: string }) => {
        await tick();
        if (state.fail.has("get")) throw err("unavailable", "get 壞了");
        const r = state.db.get(key(req.kind, req.id));
        if (!r) throw err("not_found");
        return {
          record: {
            kind: req.kind,
            id: req.id,
            fields: structuredClone(r.fields),
            archived: r.archived,
            revision: String(r.revision),
          },
        };
      },
      patch: async (req: {
        kind?: string;
        id?: string;
        set?: Record<string, unknown>;
        unset?: string[];
        expected_revision?: string;
      }) => {
        await tick();
        state.beforePatch?.(req.kind);
        state.log.push(
          `patch ${req.id} ${JSON.stringify(req.set)} unset=${(req.unset ?? []).join(",")}`,
        );
        if (state.fail.has("patch")) throw err("unavailable", "patch 壞了");
        const r = state.db.get(key(req.kind, req.id));
        if (!r) throw err("not_found");
        if (req.expected_revision !== undefined && req.expected_revision !== String(r.revision)) {
          throw err("conflict", "資料已更新");
        }
        Object.assign(r.fields, req.set ?? {});
        for (const k of req.unset ?? []) delete r.fields[k];
        r.revision += 1;
        return { record: {} };
      },
      list: async (req: { kind?: string }) => {
        await tick();
        if (state.fail.has("list")) throw err("unavailable", "list 壞了");
        const prefix = `${req.kind}/`;
        return {
          records: [...state.db]
            .filter(([k, r]) => k.startsWith(prefix) && !r.archived)
            .map(([k, r]) => ({
              kind: req.kind,
              id: k.slice(prefix.length),
              fields: structuredClone(r.fields),
              archived: r.archived,
              revision: String(r.revision),
            })),
        };
      },
      nextId: async () => {
        await tick();
        if (state.fail.has("nextId")) throw err("unavailable", "nextId 壞了");
        return { id: `CN-${state.nextN++}` };
      },
      put: async (req: {
        record: { kind: string; id: string; fields: Record<string, unknown> };
      }) => {
        await tick();
        state.log.push(`put ${req.record.id}`);
        if (state.fail.has("put")) throw err("unavailable", "put 壞了");
        state.db.set(key(req.record.kind, req.record.id), {
          fields: req.record.fields,
          archived: false,
          revision: 1,
        });
        return {};
      },
    },
    web: {
      getRoster: async () => {
        await tick();
        state.log.push("roster");
        if (state.fail.has("roster")) throw err("unavailable", "roster 壞了");
        return {
          agents: [...state.agents].filter(([, on]) => on).map(([id]) => ({ id, enabled: true })),
        };
      },
      getAgents: async () => {
        await tick();
        state.log.push("agents");
        if (state.fail.has("agents")) throw err("unavailable", "agents 壞了");
        return { agents: [...state.agents].map(([id, enabled]) => ({ id, enabled })) };
      },
    },
    bus: {
      send: async (req: { to?: string; body?: string }) => {
        await tick();
        state.log.push("bus");
        if (state.fail.has("bus")) throw err("unavailable", "bus 壞了");
        state.sent.push(req);
        return {};
      },
    },
  };
  return { state, api };
});

vi.mock("@evergenie/sdk-web", () => ({ api: h.api }));

import { loadArtStatus, loadUnadopted, sendUnlock, sendWear } from "./store";

beforeEach(() => {
  h.state.db.clear();
  h.state.log.length = 0;
  h.state.fail.clear();
  h.state.sent.length = 0;
  h.state.nextN = 1;
  h.state.beforePatch = undefined;
  h.state.agents = new Map([["art", true]]);
});

// 生圖秘書（art 職位）只有開發家能有（CLO-23）；只有「修改」分頁的評論用得到她（傳話前看名冊）。
describe("生圖秘書在不在（CLO-23）", () => {
  test("TestArtStatusFromRoster：啟用中＝ready、停用＝off、沒有＝none、讀不到名冊＝unknown", async () => {
    expect(await loadArtStatus()).toBe("ready");
    h.state.agents = new Map([["art", false]]);
    expect(await loadArtStatus()).toBe("off");
    h.state.agents = new Map([["life", true]]);
    expect(await loadArtStatus()).toBe("none");
    h.state.fail.add("agents");
    expect(await loadArtStatus()).toBe("none");
    h.state.fail.add("roster");
    expect(await loadArtStatus()).toBe("unknown");
  });
});

// 開發家展示間到「修改」分頁的入口（CLO-37）：張數照「修改」分頁打開「開發中」與「顯示隱藏版」看到的算。
describe("展示間到「修改」的入口（CLO-37）", () => {
  const put = (kind: string, id: string, fields: Record<string, unknown>, archived = false) =>
    h.state.db.set(`${kind}/${id}`, { fields, archived, revision: 1 });
  const photo = (id: string, persona: string, outfit: string, adopted: boolean, hidden = false) =>
    put("closet.photo", id, {
      persona,
      outfit,
      type: "half",
      order: 1,
      hidden,
      adopted,
      source: "official",
      file: `${id}.png`,
    });

  test("TestUnadoptedCountsWhatEditTabLists：只算這位、還在的衣服、沒採用的（含隱藏版）", async () => {
    put("closet.outfit", "athena-wip", { persona: "athena", outfit: "wip", state: "draft" });
    put("closet.outfit", "athena-sea", { persona: "athena", outfit: "sea", state: "active" });
    put("closet.outfit", "athena-old", { persona: "athena", outfit: "old", state: "draft" }, true);
    put("closet.outfit", "bea-wip", { persona: "bea", outfit: "wip", state: "draft" });
    photo("athena-wip--half1", "athena", "athena-wip", false);
    photo("athena-wip--half2", "athena", "athena-wip", true);
    photo("athena-wip--half1-h", "athena", "athena-wip", false, true);
    photo("athena-sea--half1", "athena", "athena-sea", false);
    // 封存了的那套的照片「修改」分頁不列，這裡也不算；別人的不算。
    photo("athena-old--half1", "athena", "athena-old", false);
    photo("bea-wip--half1", "bea", "bea-wip", false);
    expect(await loadUnadopted("athena")).toEqual({ count: 3, hidden: 1, ready: 0 });
    expect(await loadUnadopted("bea")).toEqual({ count: 1, hidden: 0, ready: 0 });
    expect(await loadUnadopted("nobody")).toEqual({ count: 0, hidden: 0, ready: 0 });
  });

  // CLO-38：照片都採用了、整套還是開發中的（等「上架」）也要有入口；沒照片的開發中不算。
  test("TestUnadoptedCountsOutfitsReadyToPublish", async () => {
    put("closet.outfit", "cara-done", { persona: "cara", outfit: "done", state: "draft" });
    put("closet.outfit", "cara-empty", { persona: "cara", outfit: "empty", state: "draft" });
    put("closet.outfit", "cara-live", { persona: "cara", outfit: "live", state: "active" });
    photo("cara-done--half1", "cara", "cara-done", true);
    photo("cara-done--half1-h", "cara", "cara-done", true, true);
    photo("cara-live--half1", "cara", "cara-live", true);
    expect(await loadUnadopted("cara")).toEqual({ count: 0, hidden: 0, ready: 1 });
  });

  test("TestUnadoptedFailsLoudly：讀不到就丟錯（頁面不顯示入口）", async () => {
    h.state.fail.add("list");
    await expect(loadUnadopted("athena")).rejects.toThrow();
  });
});

describe("現在穿這套（CLO-30）", () => {
  test("TestSendWearWritesPendingRequestOnly", async () => {
    // 頁面只寫一筆 pending 的請求（wear／unwear），不自己改輪播設定；能不能穿由程式判斷。
    const id = await sendWear("athena", { type: "wear", outfit: "night" });
    expect(h.state.db.get(`closet.request/${id}`)?.fields).toEqual({
      type: "wear",
      persona: "athena",
      outfit: "night",
      state: "pending",
    });
    const undo = await sendWear("athena", { type: "unwear", outfit: "night" });
    expect(h.state.db.get(`closet.request/${undo}`)?.fields.type).toBe("unwear");
    expect([...h.state.db.keys()].some((k) => k.startsWith("closet.rotation/"))).toBe(false);
    // 兌換照舊是 unlock。
    const q = await sendUnlock("athena", "sea");
    expect(h.state.db.get(`closet.request/${q}`)?.fields.type).toBe("unlock");
  });

  test("TestSendWearFailsWithoutId", async () => {
    h.state.fail.add("nextId");
    await expect(sendWear("athena", { type: "wear", outfit: "night" })).rejects.toThrow();
    expect(h.state.db.size).toBe(0);
  });
});
