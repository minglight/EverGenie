import { api } from "@evergenie/sdk-web";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import {
  apiFor,
  buildSites,
  homePending,
  PRIMARY,
  PROBE_TIMEOUT_MS,
  type Probe,
  parseHomeSearch,
  probeSite,
  resolveHome,
  SITES_CONFIG_KEY,
  type SiteMode,
  siteHref,
  siteKey,
  siteReason,
  siteState,
  useSiteApi,
  useSites,
} from "./sites";

const OFFICE = "https://genie-amy-office.example.ts.net";

const mode = (over: Partial<SiteMode> = {}): SiteMode => ({
  env: "prod",
  label: "amy-mini",
  machine: "mini",
  userName: "",
  version: "1.2.3",
  boardApi: 1,
  publicUrl: "https://genie-amy-mini.example.ts.net",
  ...over,
});

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const fetchMock = vi.fn<typeof fetch>();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => vi.unstubAllGlobals());

describe("網址與 key", () => {
  test("主伺服器的 base 是空字串、api 就是原本的同源 api", () => {
    expect(PRIMARY).toEqual({ key: "", base: "" });
    expect(apiFor(PRIMARY)).toBe(api);
    expect(apiFor({ base: OFFICE })).toBe(apiFor({ base: OFFICE }));
    expect(apiFor({ base: OFFICE })).not.toBe(api);
  });

  test("siteHref：別台的相對網址補成那台的絕對網址；主伺服器維持相對；完整網址與空的原樣", () => {
    const office = { base: OFFICE };
    expect(siteHref(office, "/avatars/x.png")).toBe(`${OFFICE}/avatars/x.png`);
    expect(siteHref(office, "/share/a b.png")).toBe(`${OFFICE}/share/a b.png`);
    expect(siteHref(office, "share/x")).toBe(`${OFFICE}/share/x`);
    expect(siteHref(PRIMARY, "/avatars/x.png")).toBe("/avatars/x.png");
    expect(siteHref(office, "https://cdn.example/x.png")).toBe("https://cdn.example/x.png");
    expect(siteHref(office, "data:image/png;base64,AA")).toBe("data:image/png;base64,AA");
    expect(siteHref(office, "//cdn.example/x.png")).toBe("//cdn.example/x.png");
    expect(siteHref(office, "")).toBe("");
    expect(siteHref(office, undefined)).toBeUndefined();
  });

  test("查詢 key：主伺服器跟 S3 之前一樣不加前綴；別台加 site 與台名，兩台不共用快取", () => {
    expect(siteKey(PRIMARY, "task", "SEC-1")).toEqual(["task", "SEC-1"]);
    expect(siteKey({ key: "amy-office" }, "task", "SEC-1")).toEqual([
      "site",
      "amy-office",
      "task",
      "SEC-1",
    ]);
  });

  test("?home 解析：只收非空字串；沒寫或寫主伺服器自己的台名＝主伺服器", () => {
    expect(parseHomeSearch({ home: "amy-office" })).toEqual({ home: "amy-office" });
    expect(parseHomeSearch({})).toEqual({ home: undefined });
    expect(parseHomeSearch({ home: "" })).toEqual({ home: undefined });
    expect(parseHomeSearch({ home: 3 })).toEqual({ home: undefined });

    const sites = buildSites(
      mode(),
      false,
      [{ url: OFFICE }],
      [{ reach: "ok", mode: mode({ label: "amy-office", machine: "office" }) }],
    );
    expect(resolveHome(sites, undefined)?.primary).toBe(true);
    expect(resolveHome(sites, "")?.primary).toBe(true);
    expect(resolveHome(sites, "amy-mini")?.primary).toBe(true);
    expect(resolveHome(sites, "amy-office")?.base).toBe(OFFICE);
    expect(resolveHome(sites, "nobody")).toBeUndefined();
  });
});

describe("試連一台", () => {
  test("get_mode 讀得到＝ok，帶回台名、版本與看板介面等級", async () => {
    fetchMock.mockResolvedValueOnce(
      json(200, {
        env: "prod",
        label: "amy-office",
        machine: "office",
        version: "1.2.0",
        board_api: 1,
      }),
    );
    const probe = await probeSite(OFFICE);
    expect(fetchMock.mock.calls[0]?.[0]).toBe(`${OFFICE}/api/system/get_mode`);
    expect(probe).toEqual({
      reach: "ok",
      mode: mode({ label: "amy-office", machine: "office", version: "1.2.0", publicUrl: "" }),
    });
  });

  // 每個測試用自己的網址：probeSite 會記住這個分頁上次讀到的台（斷線時用），不同測試不互相影響。
  let n = 0;
  const fresh = () => `https://site-${++n}.example`;

  test("回 403（帶 CORS 標頭、讀得到）＝被拒；其他讀得到的錯誤（5xx）＝到不了，不誤報成沒允許", async () => {
    // S3-2 的兩種 403（沒設 owner_login、不是主人的裝置）都是被拒，伺服器的訊息留給設定頁顯示。
    fetchMock.mockResolvedValueOnce(
      json(403, { code: "forbidden", message: "這台只接受主人的裝置" }),
    );
    expect(await probeSite(fresh())).toEqual({ reach: "denied", reason: "這台只接受主人的裝置" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const base = fresh();
    fetchMock.mockResolvedValueOnce(
      json(403, {
        code: "forbidden",
        message: "這台還沒設定主人的 tailnet 登入（web.owner_login）",
      }),
    );
    const probe = await probeSite(base);
    expect(buildSites(mode(), false, [{ url: base }], [probe])[1]).toMatchObject({
      state: "denied",
      reason: "這台還沒設定主人的 tailnet 登入（web.owner_login）",
    });
    fetchMock.mockResolvedValueOnce(json(500, { code: "internal", message: "壞了" }));
    expect(await probeSite(fresh())).toEqual({ reach: "down" });
  });

  test("讀不到回應但 no-cors 抓得到 manifest＝到得了但被拒（沒允許或太舊）", async () => {
    const base = fresh();
    fetchMock
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockResolvedValueOnce(new Response(null, { status: 200 }));
    expect(await probeSite(base)).toEqual({ reach: "denied" });
    expect(fetchMock.mock.calls[1]?.[0]).toBe(`${base}/manifest.webmanifest`);
    expect(fetchMock.mock.calls[1]?.[1]?.mode).toBe("no-cors");
  });

  // 已知限制：代理也會回 manifest 的 502，判成被拒；跟瀏覽器讀不到 502 時的結果一樣。
  test("讀得到代理的 502 也走 no-cors 判斷，不直接當成到不了", async () => {
    const base = fresh();
    fetchMock
      .mockResolvedValueOnce(new Response("", { status: 502 }))
      .mockResolvedValueOnce(new Response(null, { status: 200 }));
    expect(await probeSite(base)).toEqual({ reach: "denied" });
    expect(fetchMock.mock.calls[1]?.[0]).toBe(`${base}/manifest.webmanifest`);
  });

  test("兩個都抓不到＝到不了", async () => {
    fetchMock.mockRejectedValue(new TypeError("Failed to fetch"));
    expect(await probeSite(fresh())).toEqual({ reach: "down" });
  });

  test("get_mode 等超過 PROBE_TIMEOUT_MS 也算到不了", async () => {
    vi.useFakeTimers();
    try {
      fetchMock.mockImplementation(() => new Promise<Response>(() => {}));
      const probe = probeSite(fresh());
      await vi.advanceTimersByTimeAsync(PROBE_TIMEOUT_MS);
      expect(await probe).toEqual({ reach: "down" });
    } finally {
      vi.useRealTimers();
    }
  });

  test("連上過的台斷線後仍記得它是誰（key 不會從台名變成網址）", async () => {
    const base = fresh();
    fetchMock.mockResolvedValueOnce(json(200, { label: "amy-office", board_api: 1 }));
    await probeSite(base);
    fetchMock.mockRejectedValue(new TypeError("Failed to fetch"));
    const probe = await probeSite(base);
    expect(probe).toMatchObject({ reach: "down", last: { label: "amy-office" } });
    const sites = buildSites(mode(), false, [{ url: base }], [probe]);
    expect(sites[1]).toMatchObject({ key: "amy-office", state: "down" });
  });

  test("版本太舊＝board_api 比主伺服器自己的小（單一正本是主伺服器的 get_mode）", () => {
    const ok = (boardApi: number, env: "prod" | "dev" = "prod"): Probe => ({
      reach: "ok",
      mode: mode({ boardApi, env }),
    });
    const primary = (boardApi: number) => mode({ boardApi });
    expect(siteState(ok(1), primary(2))).toBe("old");
    expect(siteState(ok(0), primary(1))).toBe("old");
    expect(siteState(ok(2), primary(2))).toBe("ok");
    expect(siteState(ok(3), primary(2))).toBe("ok");
    expect(siteState({ reach: "denied" }, primary(1))).toBe("denied");
    expect(siteState({ reach: "down" }, primary(1))).toBe("down");
    expect(siteState(undefined, primary(1))).toBe("checking");
    // 主伺服器自己還沒讀到：別台一律 checking（還不能判斷）。
    expect(siteState(ok(1), undefined)).toBe("checking");
  });

  test("正式看板不看開發家（env 不同當被拒）", () => {
    const dev: Probe = { reach: "ok", mode: mode({ env: "dev", label: "amy-office@dev" }) };
    expect(siteState(dev, mode({ env: "prod" }))).toBe("denied");
    expect(siteState(dev, mode({ env: "dev" }))).toBe("ok");
    expect(siteReason(dev, mode({ env: "prod" }))).toBe("這是開發家，正式看板不看");
    expect(siteReason(dev, mode({ env: "dev" }))).toBe("");
  });
});

describe("台清單", () => {
  test("主伺服器永遠第一個；別台連上時 key＝台名，從沒連上過時 key＝網址；寫了自己或重複的略過", () => {
    const sites = buildSites(
      mode(),
      false,
      [
        { url: OFFICE, color: "#7c5cff" },
        { url: "https://self.example" },
        { url: "https://dead.example" },
        { url: OFFICE },
      ],
      [
        { reach: "ok", mode: mode({ label: "amy-office", machine: "office" }) },
        { reach: "ok", mode: mode() },
        { reach: "down" },
        { reach: "ok", mode: mode({ label: "amy-office" }) },
      ],
    );
    expect(sites.map((s) => [s.key, s.base, s.primary, s.state, s.color])).toEqual([
      ["", "", true, "ok", ""],
      ["amy-office", OFFICE, false, "ok", "#7c5cff"],
      ["https://dead.example", "https://dead.example", false, "down", ""],
    ]);
  });

  test("主伺服器還沒讀到 get_mode 也在清單裡（不用等就能查）；讀不到就標 down", () => {
    expect(buildSites(undefined, false, [], [])).toMatchObject([
      { key: "", base: "", primary: true, state: "ok", label: "" },
    ]);
    expect(buildSites(undefined, true, [], [])[0]?.state).toBe("down");
  });

  test("?home 找不到時分得出「還在試連」與「真的沒有這台」", () => {
    const office: Probe = { reach: "ok", mode: mode({ label: "amy-office" }) };
    expect(homePending(buildSites(undefined, false, [{ url: OFFICE }], [office]))).toBe(true);
    expect(homePending(buildSites(mode(), false, [{ url: OFFICE }], [undefined]))).toBe(true);
    expect(homePending(buildSites(mode(), false, [{ url: OFFICE }], [office]))).toBe(false);
  });
});

function wrapper(client: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("useSites", () => {
  test("只有一台（沒設定別台）時就是主伺服器一台，只打自己的 get_mode 與 get_sites，不試連任何別台", async () => {
    fetchMock.mockImplementation(async (input) =>
      String(input) === "/api/web/get_sites"
        ? json(200, { sites: [] })
        : json(200, { label: "amy-mini", board_api: 1 }),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(() => useSites(), { wrapper: wrapper(client) });
    await waitFor(() => expect(result.current[0]?.label).toBe("amy-mini"));
    await waitFor(() => expect(client.getQueryData(SITES_CONFIG_KEY)).toEqual([]));
    expect(result.current).toHaveLength(1);
    expect(fetchMock.mock.calls.map((c) => c[0]).sort()).toEqual([
      "/api/system/get_mode",
      "/api/web/get_sites",
    ]);
  });

  test("台清單來自主伺服器的 web.get_sites（沒網址的略過）；讀不到時當成只有主伺服器", async () => {
    fetchMock.mockImplementation(async (input) => {
      const url = String(input);
      if (url === "/api/web/get_sites")
        return json(200, { sites: [{ url: OFFICE, color: "#7c5cff" }, { color: "#000" }] });
      if (url === "/api/system/get_mode") return json(200, { label: "amy-mini", board_api: 1 });
      if (url === `${OFFICE}/api/system/get_mode`)
        return json(200, { label: "amy-office", board_api: 1 });
      throw new TypeError("Failed to fetch");
    });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(() => useSites(), { wrapper: wrapper(client) });
    await waitFor(() =>
      expect(result.current.map((s) => [s.key, s.state, s.color])).toEqual([
        ["", "ok", ""],
        ["amy-office", "ok", "#7c5cff"],
      ]),
    );

    fetchMock
      .mockReset()
      .mockImplementation(async (input) =>
        String(input) === "/api/web/get_sites"
          ? json(403, { code: "forbidden", message: "不行" })
          : json(200, { label: "amy-mini", board_api: 1 }),
      );
    const fresh = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result: only } = renderHook(() => useSites(), { wrapper: wrapper(fresh) });
    await waitFor(() => expect(fresh.getQueryState(SITES_CONFIG_KEY)?.status).toBe("error"));
    expect(only.current.map((s) => s.key)).toEqual([""]);
  });

  test("設定了別台：試連後看得到它的狀態；版本太舊的標 old；useSiteApi 拿到那台的 api", async () => {
    const LAB = "https://lab.example";
    fetchMock.mockImplementation(async (input) => {
      const url = String(input);
      if (url === "/api/system/get_mode") return json(200, { label: "amy-mini", board_api: 2 });
      if (url === `${OFFICE}/api/system/get_mode`)
        return json(200, { label: "amy-office", machine: "office", board_api: 2 });
      if (url === `${LAB}/api/system/get_mode`)
        return json(200, { label: "amy-lab", board_api: 1 });
      throw new TypeError("Failed to fetch");
    });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(SITES_CONFIG_KEY, [{ url: OFFICE }, { url: LAB }]);
    const { result } = renderHook(
      () => ({
        sites: useSites(),
        office: useSiteApi("amy-office"),
        lab: useSiteApi("amy-lab"),
        none: useSiteApi("x"),
      }),
      { wrapper: wrapper(client) },
    );
    await waitFor(() =>
      expect(result.current.sites.map((s) => [s.key, s.state])).toEqual([
        ["", "ok"],
        ["amy-office", "ok"],
        ["amy-lab", "old"],
      ]),
    );
    expect(result.current.office).toBe(apiFor({ base: OFFICE }));
    // 找不到的台、太舊的台都不給 api，也不退回主伺服器（會寫到錯的家）。
    expect(result.current.lab).toBeUndefined();
    expect(result.current.none).toBeUndefined();
  });
});
