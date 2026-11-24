import { beforeEach, describe, expect, test, vi } from "vitest";
import type { Site } from "../../data/sites";
import { subscribeOtherSites, subscribeSite, unsubscribeOtherSites } from "./pushSites";

// 別台的 api：照 base 分一份 mock，記下送到哪一台。
const remote = vi.hoisted(() => new Map<string, Record<string, ReturnType<typeof vi.fn>>>());
vi.mock("@evergenie/sdk-web", () => {
  class ApiError extends Error {
    code: string;
    constructor(code: string, message: string) {
      super(message);
      this.code = code;
    }
  }
  const forBase = (base: string) => {
    let m = remote.get(base);
    if (!m) {
      m = { pushKey: vi.fn(), subscribePush: vi.fn(), unsubscribePush: vi.fn() };
      remote.set(base, m);
    }
    return m;
  };
  return {
    ApiError,
    api: { notify: forBase("") },
    makeApi: (base: string) => ({ notify: forBase(base) }),
  };
});

const { ApiError, makeApi } = (await import("@evergenie/sdk-web")) as unknown as {
  ApiError: new (code: string, message: string) => Error;
  makeApi: (base: string) => { notify: Record<string, ReturnType<typeof vi.fn>> };
};

const site = (key: string, over: Partial<Site> = {}): Site => ({
  key,
  base: `https://${key}.example.ts.net`,
  primary: false,
  label: key,
  machine: key,
  env: "prod",
  version: "1",
  boardApi: 1,
  publicUrl: "",
  color: "",
  state: "ok",
  reason: "",
  ...over,
});
const PRIMARY_SITE = site("", { base: "", primary: true });
const KEYS = { endpoint: "https://push.example/abc", p256dh: "p", auth: "a" };
const notifyOf = (s: Site) => makeApi(s.base).notify;

beforeEach(() => {
  for (const m of remote.values()) for (const f of Object.values(m)) f.mockReset();
});

describe("推播寫進每一台（S3-8）", () => {
  // 一個訂閱只認一把金鑰：別台的 push_key 跟這個看板一樣才寫，不一樣的標「收不到」。
  test("金鑰一樣才寫進去；不一樣就不寫、標 mismatch", async () => {
    const same = site("amy-office");
    const other = site("amy-home");
    notifyOf(same).pushKey?.mockResolvedValue({ public_key: "K1" });
    notifyOf(same).subscribePush?.mockResolvedValue({ subscriptions: 1 });
    notifyOf(other).pushKey?.mockResolvedValue({ public_key: "K2" });

    const got = await subscribeOtherSites([PRIMARY_SITE, same, other], KEYS, "K1");
    expect(got).toEqual({ "amy-office": { state: "on" }, "amy-home": { state: "mismatch" } });
    expect(notifyOf(same).subscribePush).toHaveBeenCalledWith(KEYS);
    expect(notifyOf(other).subscribePush).not.toHaveBeenCalled();
    // 主伺服器不在這裡寫（DevicesSettings 自己寫，失敗要整個退掉）。
    expect(notifyOf(PRIMARY_SITE).subscribePush).not.toHaveBeenCalled();
  });

  test("那台現在不能用：不打它，標 unreachable 並帶原因", async () => {
    const denied = site("amy-office", { state: "denied", reason: "這台只接受主人的裝置" });
    expect(await subscribeSite(denied, KEYS, "K1")).toEqual({
      state: "unreachable",
      reason: "這台只接受主人的裝置",
    });
    expect(notifyOf(denied).pushKey).not.toHaveBeenCalled();
  });

  test("那台沒設推播金鑰：標 missing；讀金鑰失敗：unreachable；寫入失敗：failed 帶原因", async () => {
    const s = site("amy-office");
    notifyOf(s).pushKey?.mockRejectedValueOnce(new ApiError("unavailable", "推播金鑰還沒設定"));
    expect(await subscribeSite(s, KEYS, "K1")).toEqual({ state: "missing" });

    notifyOf(s).pushKey?.mockRejectedValueOnce(new ApiError("unavailable", "連不到看板服務"));
    expect(await subscribeSite(s, KEYS, "K1")).toEqual({
      state: "unreachable",
      reason: "連不到看板服務",
    });

    notifyOf(s).pushKey?.mockResolvedValueOnce({ public_key: "K1" });
    notifyOf(s).subscribePush?.mockRejectedValueOnce(new ApiError("invalid", "endpoint 不對"));
    expect(await subscribeSite(s, KEYS, "K1")).toEqual({
      state: "failed",
      reason: "endpoint 不對",
    });
  });

  test("關閉：能用的別台都刪掉這個 endpoint，某台失敗不影響別台", async () => {
    const a = site("amy-office");
    const b = site("amy-home");
    const down = site("amy-old", { state: "down" });
    notifyOf(a).unsubscribePush?.mockRejectedValue(new Error("offline"));
    notifyOf(b).unsubscribePush?.mockResolvedValue({ subscriptions: 0 });
    await unsubscribeOtherSites([PRIMARY_SITE, a, b, down], KEYS.endpoint);
    expect(notifyOf(a).unsubscribePush).toHaveBeenCalledWith({ endpoint: KEYS.endpoint });
    expect(notifyOf(b).unsubscribePush).toHaveBeenCalledWith({ endpoint: KEYS.endpoint });
    expect(notifyOf(down).unsubscribePush).not.toHaveBeenCalled();
    expect(notifyOf(PRIMARY_SITE).unsubscribePush).not.toHaveBeenCalled();
  });
});
