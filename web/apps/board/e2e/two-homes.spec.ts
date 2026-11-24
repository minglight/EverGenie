import type { Page } from "@playwright/test";
import { expect, type ServeProxy, TEST_OWNER_LOGIN, twoHomesTest as test } from "./fixtures";

// S3 看板連多台的 e2e 基礎（S3-E）：兩個家，各自前面一個模擬 tailscale serve 的 https proxy（e2e/serve-proxy.ts）。
// 瀏覽器開家 A 的看板（baseURL＝A 的 proxy），從頁面直接呼叫家 B 的 API；伺服器之間不連線。
// 只跑桌機（沒有手機專屬內容，不列進 playwright.config.ts 的 mobileSpecs）。

interface Mode {
  label?: string;
  env?: string;
  board_api?: number;
  public_url?: string;
}

interface Fetched {
  status: number;
  json: unknown;
}

// 從目前這一頁（瀏覽器裡）POST 一個 API；url 可以是同源路徑或別台的完整網址。拿不到回應（CORS 擋掉、連不上）時 status 是 0。
function pagePost(
  page: Page,
  url: string,
  body: unknown,
  headers: Record<string, string> = {},
): Promise<Fetched> {
  return page.evaluate(
    async ({ url, body, headers }) => {
      try {
        const res = await fetch(url, {
          method: "POST",
          headers: { "Content-Type": "application/json", ...headers },
          body: JSON.stringify(body),
        });
        return { status: res.status, json: await res.json().catch(() => null) };
      } catch (err) {
        return { status: 0, json: String(err) };
      }
    },
    { url, body, headers },
  );
}

test("TestTwoHomesStartWithDifferentLabels @smoke", async ({ page, homes }) => {
  expect(homes.a.url).not.toBe(homes.b.url);
  await page.goto("/");
  await expect(page.locator("body")).not.toBeEmpty();
  const a = await pagePost(page, "/api/system/get_mode", {});
  expect(a.status).toBe(200);
  expect(a.json as Mode).toMatchObject({ label: homes.a.label, public_url: homes.a.url });

  await page.goto(homes.b.url);
  const b = await pagePost(page, "/api/system/get_mode", {});
  expect(b.status).toBe(200);
  expect(b.json as Mode).toMatchObject({ label: homes.b.label, public_url: homes.b.url });
  expect(homes.a.label).not.toBe(homes.b.label);
});

// 看板自己也會打 get_mode（頂端的家標記、S3-5 起探測各台），所以測試自己的請求在網址帶一個獨一無二的查詢字串，
// 從 proxy 的 forwarded 挑出那幾筆（後端只看路徑、不看查詢；預檢快取按網址分，所以每個 probe 都會重新預檢）。
function probe(tag: string): string {
  return `/api/system/get_mode?e2e=${tag}-${Math.random().toString(36).slice(2)}`;
}
const sent = (proxy: ServeProxy, path: string) => proxy.forwarded.filter((f) => f.path === path);

// 頁面自己帶的 Tailscale-User-*（偽造身分）會被 serve 刪掉，後端只看到 serve 補的那一個；不補時後端完全收不到。
test("TestServeProxyDropsClientTailscaleHeaders", async ({ page, homes }) => {
  await page.goto(homes.b.url);
  const forged = { "Tailscale-User-Login": "evil@e2e.test", "Tailscale-User-Name": "Evil" };

  const withLogin = probe("forged");
  expect((await pagePost(page, withLogin, {}, forged)).status).toBe(200);
  const [got] = sent(homes.b.proxy, withLogin);
  expect(got?.headers["tailscale-user-login"]).toBe(TEST_OWNER_LOGIN);
  expect(got?.headers["tailscale-user-name"]).not.toBe("Evil");
  expect(got?.headers["x-forwarded-proto"]).toBe("https");

  homes.b.proxy.login = null;
  const noLogin = probe("tag");
  expect((await pagePost(page, noLogin, {}, forged)).status).toBe(200);
  const [bare] = sent(homes.b.proxy, noLogin);
  expect(bare).toBeDefined();
  expect(Object.keys(bare?.headers ?? {}).filter((k) => k.startsWith("tailscale-user-"))).toEqual(
    [],
  );
});

// 跨站要後端的 CORS＋身分（S3-2，WEB-78）。
test("TestBoardACallsHomeBGetModeAcrossSites", async ({ page, homes }) => {
  await page.goto("/");
  const path = probe("cross");
  const b = await pagePost(page, `${homes.b.url}${path}`, {});
  expect(b.status).toBe(200);
  expect(b.json as Mode).toMatchObject({ label: homes.b.label });
  // 跨站 JSON POST 會先預檢：B 前面的 proxy 看得到這個網址先 OPTIONS 再 POST。
  expect(sent(homes.b.proxy, path).map((f) => f.method)).toEqual(["OPTIONS", "POST"]);
});

// 跨站要後端的 CORS＋身分（S3-2，WEB-78）。
// 預檢不查身分（照樣轉到 B、回 204），POST 才查 login；403 也帶 ACAO，所以頁面讀得到狀態碼與錯誤碼（不是 0）。
test("TestHomeBRejectsCrossSiteWithoutOwnerLogin", async ({ page, homes }) => {
  await page.goto("/");
  for (const login of [null, "someone-else@e2e.test"]) {
    homes.b.proxy.login = login;
    const path = probe(login ? "other" : "none");
    const res = await pagePost(page, `${homes.b.url}${path}`, {});
    expect(res.status).toBe(403);
    expect(res.json).toMatchObject({ code: "forbidden" });
    expect(sent(homes.b.proxy, path).map((f) => f.method)).toEqual(["OPTIONS", "POST"]);
  }
});

// S3-5 試連（data/sites.ts 的 probeSite）靠的瀏覽器行為，在真的跨站環境驗一次：
// 被拒時讀得到伺服器的原因（存進 Site.reason）；整台不在時 get_mode 與 no-cors 的 manifest 都失敗＝down；
// serve 在、後端掛了（502）時 no-cors 拿得到不透明回應，所以會判成 denied（已知限制，寫在 probeSite 的註解）。
// 畫面上還沒有顯示各台狀態的地方（S3-9），所以這裡在頁面裡照 probeSite 的步驟打，驗瀏覽器真的這樣回。
test("TestProbeSignalsAcrossSites", async ({ page, homes }) => {
  await page.goto("/");
  const manifest = (url: string) =>
    page.evaluate(async (u) => {
      try {
        await fetch(u, { mode: "no-cors", cache: "no-store", signal: AbortSignal.timeout(5000) });
        return "reached";
      } catch {
        return "failed";
      }
    }, url);

  const ok = await pagePost(page, `${homes.b.url}${probe("site-ok")}`, {});
  expect(ok.status).toBe(200);
  expect(ok.json as Mode).toMatchObject({ label: homes.b.label });
  expect(typeof (ok.json as Mode).board_api).toBe("number");

  homes.b.proxy.login = null;
  const denied = await pagePost(page, `${homes.b.url}${probe("site-denied")}`, {});
  expect(denied.status).toBe(403);
  expect(denied.json).toMatchObject({
    code: "forbidden",
    // B 設了 owner_login，serve 沒補登入（tag 身分）＝不是主人的裝置。
    message: "這台只接受主人的裝置",
  });
  homes.b.proxy.login = TEST_OWNER_LOGIN;

  homes.b.proxy.mode = "reset";
  expect((await pagePost(page, `${homes.b.url}${probe("site-down")}`, {})).status).toBe(0);
  expect(await manifest(`${homes.b.url}/manifest.webmanifest`)).toBe("failed");

  homes.b.proxy.mode = "bad-gateway";
  expect(await manifest(`${homes.b.url}/manifest.webmanifest`)).toBe("reached");
  homes.b.proxy.mode = "up";
});

// S3-5：A 的看板照 web.get_sites 的清單自己試連 B，連得上（ok）才抓 B 的名冊；B 拒絕時只試連、不抓資料。
test("TestBoardProbesConfiguredHomeAndFetchesOnlyWhenOk", async ({ page, homes }) => {
  const fromBoard = (path: string) =>
    sent(homes.b.proxy, path).filter(
      (f) => f.method === "POST" && f.headers.origin === homes.a.url,
    );

  await page.goto("/");
  await expect.poll(() => fromBoard("/api/system/get_mode").length).toBeGreaterThan(0);
  await expect.poll(() => fromBoard("/api/web/get_roster").length).toBeGreaterThan(0);

  homes.b.proxy.login = null;
  homes.b.proxy.forwarded.length = 0;
  await page.reload();
  await expect.poll(() => fromBoard("/api/system/get_mode").length).toBeGreaterThan(0);
  // 要確認「沒有」發生只能等一下：試連結果一回來，ok 的台會馬上抓名冊；看板有串流，不能等 networkidle。
  await page.waitForTimeout(1_000);
  expect(fromBoard("/api/web/get_roster")).toEqual([]);
});

// 測「那台連不上」用的開關：bad-gateway＝serve 在、後端掛了（502）；reset＝整台不在（連線直接斷，頁面拿到網路錯誤）；up 恢復。
test("TestServeProxyModesSimulateHomeDown", async ({ page, homes }) => {
  await page.goto(homes.b.url);
  homes.b.proxy.mode = "bad-gateway";
  expect((await pagePost(page, "/api/system/get_mode", {})).status).toBe(502);
  homes.b.proxy.mode = "reset";
  expect((await pagePost(page, "/api/system/get_mode", {})).status).toBe(0);
  homes.b.proxy.mode = "up";
  expect((await pagePost(page, "/api/system/get_mode", {})).status).toBe(200);
});
