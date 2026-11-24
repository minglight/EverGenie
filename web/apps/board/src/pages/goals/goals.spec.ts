import { ApiError, api, type Goal } from "@evergenie/sdk-web";
import { afterEach, describe, expect, test, vi } from "vitest";
import { apiFor, type SiteRef } from "../../data/sites";
import {
  goalRoomFirstMessage,
  goalRoomName,
  goalRoomQuery,
  goalRoomTopic,
  goalViewCounts,
  goalViewOf,
  groupGoals,
  openGoalRoom,
} from "./goals";

afterEach(() => vi.restoreAllMocks());

const goal = (id: string, state: Goal["state"], extra: Partial<Goal> = {}): Goal => ({
  id,
  title: id,
  state,
  ...extra,
});

describe("Goal 清單分段", () => {
  // Guards: REQ-203
  test("進行中、要你同意、已結束（完成與已收回）各一段", () => {
    expect(goalViewOf(goal("a", "active"))).toBe("active");
    expect(goalViewOf(goal("b", "draft"))).toBe("draft");
    expect(goalViewOf(goal("c", "done"))).toBe("ended");
    expect(goalViewOf(goal("d", "withdrawn"))).toBe("ended");
  });

  // Guards: REQ-203
  test("進行中看最近更新，已結束看結束時間，新的在前", () => {
    const groups = groupGoals([
      goal("old", "active", { updated_at: "2026-10-01T00:00:00Z" }),
      goal("new", "active", { updated_at: "2026-10-20T00:00:00Z" }),
      goal("d1", "done", { updated_at: "2026-10-25T00:00:00Z", ended_at: "2026-10-05T00:00:00Z" }),
      goal("d2", "withdrawn", {
        updated_at: "2026-10-02T00:00:00Z",
        ended_at: "2026-10-10T00:00:00Z",
      }),
      goal("draft", "draft"),
    ]);
    expect(groups.active.map((g) => g.id)).toEqual(["new", "old"]);
    expect(groups.ended.map((g) => g.id)).toEqual(["d2", "d1"]);
    expect(groups.draft.map((g) => g.id)).toEqual(["draft"]);
  });
});

describe("跟秘書聊這個 Goal", () => {
  test("專屬對話名稱是負責的秘書加 Goal 編號小寫", () => {
    expect(goalRoomName({ id: "GOAL-3", agent: "art" })).toBe("art-goal-3");
    expect(goalRoomName({ id: "DG-12", agent: "code" })).toBe("code-dg-12");
  });

  test("主人自己開、沒有負責秘書的 Goal 沒有專屬對話", () => {
    expect(goalRoomName({ id: "GOAL-3", agent: "" })).toBe("");
    expect(goalRoomName({ id: "GOAL-3", agent: "owner(web)" })).toBe("");
    expect(goalRoomName({ id: "GOAL 3", agent: "art" })).toBe("");
  });

  test("第一句帶 Goal 編號與標題，秘書才知道在談哪一個", () => {
    const text = goalRoomFirstMessage({ id: "GOAL-3", title: "做衣服整套流程" });
    expect(text).toContain("GOAL-3");
    expect(text).toContain("做衣服整套流程");
    expect(text).toContain("evergenie goal show GOAL-3");
  });

  test("對話標題帶 Goal 編號，標題長得像單號也不會被當成任務單", () => {
    expect(goalRoomTopic({ id: "GOAL-3", title: "SEC-12" })).toBe("GOAL-3 SEC-12");
    expect(goalRoomTopic({ id: "GOAL-3", title: "" })).toBe("GOAL-3");
  });
});

// 多台（WEB-82、FEN-28）：別台的 Goal，對話開在那台；主伺服器一個呼叫都不收。
describe("別台的 Goal 在那台聊", () => {
  const B: SiteRef = { key: "amy-office", base: "https://genie-amy-office.example.ts.net" };
  const notFound = () => new ApiError("not_found", "找不到這個對話");

  test("從沒開過：在那台問、在那台開，第一句跟著送", async () => {
    const get = vi.spyOn(apiFor(B).session, "get").mockRejectedValue(notFound());
    const open = vi
      .spyOn(apiFor(B).session, "open")
      .mockResolvedValue({ session: { name: "code-goal-3" } } as never);
    const localGet = vi.spyOn(api.session, "get");
    const localOpen = vi.spyOn(api.session, "open");
    const g = { id: "GOAL-3", title: "做衣服", agent: "code" };
    await expect(openGoalRoom(g, B)).resolves.toBe("code-goal-3");
    expect(get).toHaveBeenCalledWith({ name: "code-goal-3" });
    expect(open).toHaveBeenCalledWith(
      expect.objectContaining({ name: "code-goal-3", agent: "code", kind: "room" }),
    );
    expect(open.mock.calls[0]?.[0].first_message).toBe(goalRoomFirstMessage(g));
    expect(localGet).not.toHaveBeenCalled();
    expect(localOpen).not.toHaveBeenCalled();
  });

  test("那台已經有這個對話（停了也算）：接回、不重送第一句", async () => {
    vi.spyOn(apiFor(B).session, "get").mockResolvedValue({
      session: { name: "code-goal-3" },
    } as never);
    const open = vi
      .spyOn(apiFor(B).session, "open")
      .mockResolvedValue({ session: { name: "code-goal-3" } } as never);
    await openGoalRoom({ id: "GOAL-3", title: "做衣服", agent: "code" }, B);
    expect(open.mock.calls[0]?.[0].first_message).toBeUndefined();
  });

  test("查對話的 key 帶那台的台名，兩台同名的對話不共用快取", () => {
    expect(goalRoomQuery("code-goal-3", B).queryKey).toEqual([
      "site",
      "amy-office",
      "goal-room",
      "code-goal-3",
    ]);
    expect(goalRoomQuery("code-goal-3").queryKey).toEqual(["goal-room", "code-goal-3"]);
  });
});

describe("Goal 分段的數字", () => {
  // WEB-98：只看一位秘書時，「進行中 N」跟清單一樣只算她提的，不是全部的。
  test("跟清單同一個秘書篩選；沒選人是全部", () => {
    const site = { key: "" };
    const groups = {
      active: [
        { site, item: goal("GOAL-1", "active", { agent: "life" }) },
        { site, item: goal("GOAL-2", "active", { by: "owner(web)" }) },
      ],
      draft: [{ site, item: goal("GOAL-3", "draft", { agent: "code" }) }],
      ended: [],
    };
    expect(goalViewCounts(groups, [])).toEqual({ active: 2, draft: 1, ended: 0 });
    expect(goalViewCounts(groups, ["life"])).toEqual({ active: 1, draft: 0, ended: 0 });
  });
});
