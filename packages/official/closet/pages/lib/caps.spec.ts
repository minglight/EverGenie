import { beforeEach, describe, expect, test, vi } from "vitest";
import {
  actionRequest,
  actionStatus,
  actionsFor,
  CAP_EDIT,
  CAP_MINE,
  canEdit,
  filterKeeps,
  hasCap,
  NO_CAPABILITIES,
  statusLabel,
  toCapabilities,
} from "./caps";
import { loadCapabilities } from "./store";

const h = vi.hoisted(() => {
  const state = { record: undefined as unknown, error: undefined as unknown };
  const api = {
    vault: {
      get: async () => {
        if (state.error) throw state.error;
        return { record: state.record };
      },
    },
  };
  return { state, api };
});
vi.mock("@evergenie/sdk-web", () => ({ api: h.api }));

// 衣櫃程式寫的樣子（測試用的登記套件 studio）。
const move = {
  id: "studio-move",
  package: "studio",
  label: "搬到別處",
  scope: "outfit",
  needs: "edit",
  order: 0,
  request_kind: "studio.request",
  request_fields: ["persona", "outfit"],
  request_set: { type: "move" },
  status_kind: "studio.review",
  status_field: "state",
  status_labels: { done: "搬好了", stale: "改過要再搬" },
  filter_label: "搬過的",
  filter_values: ["done", "stale"],
};
const keep = {
  id: "studio-keep",
  package: "studio",
  label: "留一份",
  scope: "photo",
  needs: "mine",
  order: 2,
  request_kind: "studio.request",
  request_fields: ["persona", "photo"],
};

// 能力與動作插座（CLO-32、CLO-33，DEV-160 S4）：頁面只照 closet.capabilities/main 畫，不自己判斷是哪種家。
describe("這個家支援什麼", () => {
  beforeEach(() => {
    h.state.record = undefined;
    h.state.error = undefined;
  });

  test("TestCapabilitiesListRegisteredAction", () => {
    const c = toCapabilities({ fields: { supports: ["mine", "edit"], actions: [move, keep] } });
    expect(hasCap(c, CAP_EDIT)).toBe(true);
    expect(actionsFor(c, "outfit").map((a) => a.id)).toEqual(["studio-move"]);
    expect(actionsFor(c, "photo").map((a) => a.id)).toEqual(["studio-keep"]);
    const [a] = actionsFor(c, "outfit");
    expect(a).toMatchObject({
      label: "搬到別處",
      requestKind: "studio.request",
      status: { kind: "studio.review", field: "state" },
      filter: { label: "搬過的", values: ["done", "stale"] },
    });
    // 沒有狀態的動作：只有鈕。
    expect(actionsFor(c, "photo")[0]?.status).toBeUndefined();
    expect(actionsFor(c, "photo")[0]?.filter).toBeUndefined();
    expect(actionsFor(c, "photo")[0]?.label).toBe("留一份");
  });

  // 頁面不信紀錄裡的種類（DEV-160 審查 S4）：編號、request_kind、status_kind 都要是同一個套件的，套件不是衣櫃或 closet-mine，
  // 不然整個動作不列（寫壞或偽造的 closet.capabilities 不能讓主人按一顆鈕就寫別的套件的請求）。
  test("TestActionMustBelongToItsPackage", () => {
    const only = (a: Record<string, unknown>) =>
      toCapabilities({ fields: { supports: ["mine", "edit"], actions: [a] } }).actions.map(
        (x) => x.id,
      );
    expect(only(move)).toEqual(["studio-move"]);
    expect(only(keep)).toEqual(["studio-keep"]);
    // 請求寫到別的套件
    expect(only({ ...keep, request_kind: "closet-mine.request" })).toEqual([]);
    expect(only({ ...keep, request_kind: "studiox.request" })).toEqual([]);
    expect(only({ ...keep, request_kind: "studio." })).toEqual([]);
    expect(only({ ...keep, request_kind: "request" })).toEqual([]);
    // 狀態看別的套件的
    expect(only({ ...move, status_kind: "closet.photo" })).toEqual([]);
    // 編號不是那個套件的
    expect(only({ ...keep, id: "other-keep" })).toEqual([]);
    expect(only({ ...keep, id: "studio-" })).toEqual([]);
    // 沒寫套件、或冒用衣櫃自己與 closet-mine 的名字
    expect(only({ ...keep, package: "" })).toEqual([]);
    expect(
      only({ ...keep, package: "closet", id: "closet-keep", request_kind: "closet.request" }),
    ).toEqual([]);
    expect(
      only({
        ...keep,
        package: "closet-mine",
        id: "closet-mine-keep",
        request_kind: "closet-mine.request",
      }),
    ).toEqual([]);
  });

  test("TestCapabilitiesEmptyWhenNothingRegistered", () => {
    const c = toCapabilities({ fields: { supports: ["mine"], actions: [] } });
    expect(c.actions).toEqual([]);
    expect(hasCap(c, CAP_MINE)).toBe(true);
    expect(hasCap(c, CAP_EDIT)).toBe(false);
    // 頁面「只有開發家才有」的東西都問 canEdit（CLO-37）：沒有 edit、還沒讀到都當不能。
    expect(canEdit(c)).toBe(false);
    expect(canEdit(undefined)).toBe(false);
    expect(canEdit(NO_CAPABILITIES)).toBe(false);
    expect(canEdit(toCapabilities({ fields: { supports: ["mine", "edit"] } }))).toBe(true);
    // 還沒算好、封存了：什麼都不支援。
    expect(toCapabilities(null)).toEqual(NO_CAPABILITIES);
    expect(toCapabilities({ archived: true, fields: { supports: ["edit"] } })).toEqual(
      NO_CAPABILITIES,
    );
    // 格式壞掉的項目略過，不讓整份壞掉。
    expect(
      toCapabilities({ fields: { supports: ["mine"], actions: [{ id: "x" }, 3] } }).actions,
    ).toEqual([]);
  });

  test("TestActionRequestAndStatus", () => {
    const a = toCapabilities({ fields: { supports: ["edit"], actions: [move] } }).actions[0];
    if (!a) throw new Error("要有 studio-move");
    const target = { persona: "builder", outfit: "builder-night" };
    expect(actionRequest(a, target)).toEqual({
      type: "move",
      persona: "builder",
      outfit: "builder-night",
    });
    expect(actionRequest(a, { persona: "builder" })).toBeNull();

    const recs = [
      {
        fields: { persona: "builder", outfit: "builder-night", state: "done" },
        updated_at: "2026-11-15T01:00:00Z",
      },
      {
        fields: { persona: "builder", outfit: "builder-night", state: "stale" },
        updated_at: "2026-11-15T02:00:00Z",
      },
      {
        fields: { persona: "builder", outfit: "builder-sea", state: "done" },
        updated_at: "2026-11-15T03:00:00Z",
      },
      {
        fields: { persona: "builder", outfit: "builder-night", state: "done" },
        updated_at: "2026-11-15T04:00:00Z",
        archived: true,
      },
    ];
    const v = actionStatus(a, recs, target);
    expect(v).toBe("stale");
    expect(statusLabel(a, v)).toBe("改過要再搬");
    expect(statusLabel(a, "unknown")).toBe("");
    expect(actionStatus(a, recs, { persona: "builder", outfit: "builder-base" })).toBe("");
    expect(filterKeeps(a, "done")).toBe(true);
    expect(filterKeeps(a, "")).toBe(false);
  });

  test("TestLoadCapabilities", async () => {
    h.state.error = Object.assign(new Error("找不到"), { code: "not_found" });
    expect(await loadCapabilities()).toEqual(NO_CAPABILITIES);
    h.state.error = Object.assign(new Error("壞了"), { code: "unavailable" });
    await expect(loadCapabilities()).rejects.toThrow("壞了");
    h.state.error = undefined;
    h.state.record = { fields: { supports: ["mine", "edit"], actions: [move] }, archived: false };
    expect((await loadCapabilities()).actions.map((a) => a.id)).toEqual(["studio-move"]);
  });
});
