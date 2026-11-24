import type { Page } from "@playwright/test";
import { expect, test } from "./fixtures";

const settingsNav = (page: Page) => page.getByRole("navigation", { name: "設定分頁" });

interface Prefs {
  quiet_hours?: string;
  categories?: Record<string, { mode: string; digest_hours: number }>;
  tasks?: { nudge_at?: string; stale_days: number; nudge_every_days: number; nudge_max: number };
  [key: string]: unknown;
}

const getPrefs = async (seed: { api: <T>(r: string, b: unknown) => Promise<T> }) =>
  (await seed.api<{ prefs: Prefs }>("/api/notify/get_prefs", {})).prefs;

// 測試改過的偏好一律還原，其他測試與截圖看到的是原本的樣子。
async function restore(seed: { api: <T>(r: string, b: unknown) => Promise<T> }, original: Prefs) {
  await seed.api("/api/notify/set_prefs", { prefs: original, by: { name: "owner(test)" } });
}

const saved = (page: Page) => page.getByRole("status").getByText("已儲存");

// Guards: REQ-110, REQ-24, REQ-26
test("TestSettingsNotifyAndTrust @smoke", async ({ page, seed, request }) => {
  const original = await getPrefs(seed);
  try {
    await page.goto("/settings/notify");
    await expect(page.getByRole("group", { name: "摘要的送法" })).toBeVisible();

    // 「要你同意」沒有「關閉」，其他分類四種都有（REQ-26）。
    const approve = page.getByRole("group", { name: "要你同意的送法" });
    await expect(approve.getByRole("button")).toHaveText(["即時", "合併摘要", "只放看板"]);
    await expect(
      page.getByRole("group", { name: "事情做完的送法" }).getByRole("button"),
    ).toHaveCount(4);

    // 改「摘要」的送法：600ms 後自動存，頁首出現「已儲存」，經 API 讀回來是新值。
    const digest = page.getByRole("group", { name: "摘要的送法" });
    await expect(digest.getByRole("button", { name: "合併摘要" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await digest.getByRole("button", { name: "只放看板" }).click();
    await expect(digest.getByRole("button", { name: "只放看板" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await expect(saved(page)).toBeVisible();
    await expect.poll(async () => (await getPrefs(seed)).categories?.digest?.mode).toBe("board");
    // 重新整理後還是新值（不是只有畫面上改了）。
    await page.reload();
    await expect(
      page.getByRole("group", { name: "摘要的送法" }).getByRole("button", { name: "只放看板" }),
    ).toHaveAttribute("aria-pressed", "true");

    // 改回「合併摘要」：出現間隔欄位；改成 6 小時 → 存起來。
    await page
      .getByRole("group", { name: "摘要的送法" })
      .getByRole("button", { name: "合併摘要" })
      .click();
    const hours = page.getByTestId("hours-digest");
    await expect(hours).toHaveValue("3");
    await hours.fill("6");
    await expect(saved(page)).toBeVisible();
    expect((await getPrefs(seed)).categories?.digest).toMatchObject({
      mode: "digest",
      digest_hours: 6,
    });
    // 填 0 失焦就在欄位下提示，也不會送出。
    await hours.fill("0");
    await hours.blur();
    await expect(page.getByText("請填 1 以上的整數")).toBeVisible();
    expect((await getPrefs(seed)).categories?.digest?.digest_hours).toBe(6);

    // 安靜時段：兩邊都填才存；清除就回到空的。
    await page.getByLabel("從").fill("23:00");
    await page.getByLabel("到").fill("07:00");
    await expect.poll(async () => (await getPrefs(seed)).quiet_hours).toBe("23:00-07:00");
    await page.getByRole("button", { name: "清除" }).click();
    await expect.poll(async () => (await getPrefs(seed)).quiet_hours ?? "").toBe("");

    // 任務單跟進（REQ-148）：預設 09:00／3／3／3；改一個數字，經 API 讀回來是新值，別的欄位不動。
    await expect(page.getByTestId("tasks-nudge_at")).toHaveValue("09:00");
    await expect(page.getByTestId("tasks-stale_days")).toHaveValue("3");
    await expect(page.getByTestId("tasks-nudge_every_days")).toHaveValue("3");
    await expect(page.getByTestId("tasks-nudge_max")).toHaveValue("3");
    await page.getByLabel("停住幾天進摘要").fill("5");
    await expect.poll(async () => (await getPrefs(seed)).tasks?.stale_days).toBe(5);
    expect((await getPrefs(seed)).tasks).toMatchObject({
      nudge_at: "09:00",
      nudge_every_days: 3,
      nudge_max: 3,
    });
    await page.getByLabel("每天幾點檢查").fill("08:30");
    await expect.poll(async () => (await getPrefs(seed)).tasks?.nudge_at).toBe("08:30");
    // 超出範圍：失焦就在欄位下提示，也不會送出。
    const max = page.getByLabel("最多單獨推幾次");
    await max.fill("11");
    await max.blur();
    await expect(page.getByText("請填 0 到 10 的整數")).toBeVisible();
    expect((await getPrefs(seed)).tasks?.nudge_max).toBe(3);
    // 重新整理後還是新值。
    await page.reload();
    await expect(page.getByTestId("tasks-stale_days")).toHaveValue("5");
    await expect(page.getByTestId("tasks-nudge_at")).toHaveValue("08:30");
  } finally {
    await restore(seed, original);
  }

  // 權限分頁（唯讀）：fixture 的資料夾、誰能寫（人名）、核准與不開放的動作。
  const roster = (await (await request.post("/api/web/get_roster", { data: {} })).json()) as {
    agents: { id: string; name: string }[];
  };
  const codeName = roster.agents.find((a) => a.id === "code")?.name ?? "";
  expect(codeName).not.toBe("");
  await page.goto("/settings/trust");
  const repos = page.getByTestId("trust-repo");
  await expect(repos).toHaveCount(2);
  await expect(repos.first()).toContainText("/work/e2e-demo");
  await expect(repos.first()).toContainText("不能推送");
  await expect(repos.first()).toContainText(codeName);
  await expect(repos.last()).toContainText("可以推送");
  await expect(page.getByText("run_tests")).toBeVisible();
  await expect(page.getByText("send_external")).toBeVisible();
  // 最後一行說怎麼改權限，不露內部單號（DEV-156 U-07）。
  await expect(page.getByText("要改權限，請跟秘書說")).toBeVisible();
  await expect(page.getByRole("main")).not.toContainText("SEC-320");
  // 唯讀：沒有任何可以改權限的按鈕或輸入框。
  await expect(page.getByRole("main").getByRole("textbox")).toHaveCount(0);
});

// Guards: REQ-110
test("TestSettingsRejectedFieldReverts", async ({ page, seed }) => {
  const original = await getPrefs(seed);
  try {
    await page.goto("/settings/notify");
    const done = page.getByRole("group", { name: "事情做完的送法" });
    await expect(done.getByRole("button", { name: "即時" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await page.route("**/api/notify/set_prefs", (route) =>
      route.fulfill({
        status: 400,
        contentType: "application/json",
        body: JSON.stringify({ code: "invalid", field: "mode", message: "通知送法不正確" }),
      }),
    );
    await done.getByRole("button", { name: "關閉" }).click();
    // 伺服器拒絕：還原那一格，原因寫在那一列下面，頁首說沒存成功。
    await expect(page.getByText("通知送法不正確")).toBeVisible();
    await expect(done.getByRole("button", { name: "即時" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await expect(page.getByRole("status").getByText("沒存成功，已還原")).toBeVisible();
    expect((await getPrefs(seed)).categories?.done?.mode).toBe("instant");
  } finally {
    await restore(seed, original);
  }
});

// Guards: REQ-110
test("TestSettingsSaveRetriesAfterNetworkFailure", async ({ page, seed }) => {
  const original = await getPrefs(seed);
  try {
    await page.goto("/settings/notify");
    const due = page.getByRole("group", { name: "待辦到期的送法" });
    await expect(due.getByRole("button", { name: "即時" })).toBeVisible();
    let blocked = true;
    await page.route("**/api/notify/set_prefs", (route) =>
      blocked ? route.abort("connectionfailed") : route.continue(),
    );
    await due.getByRole("button", { name: "只放看板" }).click();
    // 網路不通：改的內容留著，頁首有「重試」。
    await expect(page.getByRole("status").getByText("沒存成功")).toBeVisible();
    await expect(due.getByRole("button", { name: "只放看板" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    blocked = false;
    await page.getByRole("button", { name: "重試" }).click();
    await expect(saved(page)).toBeVisible();
    await expect.poll(async () => (await getPrefs(seed)).categories?.due?.mode).toBe("board");
  } finally {
    await restore(seed, original);
  }
});

// Guards: REQ-107
test("TestSettingsTabsNavigate", async ({ page, isMobile }) => {
  await page.goto("/settings");
  await expect(page).toHaveURL(isMobile ? /\/settings$/ : /\/settings\/notify$/);
  const tabs = [
    { name: "權限", path: "/settings/trust", cat: "秘書" },
    { name: "裝置推播", path: "/settings/devices", cat: "常用" },
    { name: "系統健康", path: "/settings/health", cat: "進階" },
    { name: "通知", path: "/settings/notify", cat: "常用" },
  ];
  for (const tab of tabs) {
    // 手機沒有分頁列：從設定首頁的清單點進去（那一組底下），上一頁回清單（DEV-156 U-12）。
    if (isMobile) {
      await page
        .getByTestId("settings-home")
        .getByRole("region", { name: tab.cat, exact: true })
        .getByRole("link", { name: tab.name, exact: true })
        .click();
      await expect(page).toHaveURL(new RegExp(`${tab.path}$`));
      await page.getByTestId("back-link").click();
      await expect(page).toHaveURL(/\/settings$/);
      continue;
    }
    await settingsNav(page).getByRole("link", { name: tab.name, exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`${tab.path}$`));
  }
});

// Guards: REQ-104
// 這支要真的註冊 service worker（其他測試預設擋掉，playwright.config.ts）。
test.describe(() => {
  test.use({ serviceWorkers: "allow" });
  test("TestManifestAndServiceWorker", async ({ page, request, seed }) => {
    // 主畫面的名稱依家產生（WEB-77）：e2e 的家是正式家，名稱「看板 <machine>」，machine 跟 get_mode 給的一樣。
    const home = await seed.api<{ env?: string; label?: string; machine?: string }>(
      "/api/system/get_mode",
      {},
    );
    expect(home.env ?? "prod").toBe("prod");
    expect(home.machine).toBeTruthy();
    const res = await request.get("/manifest.webmanifest");
    expect(res.status()).toBe(200);
    expect(res.headers()["content-type"]).toContain("application/manifest+json");
    const manifest = await res.json();
    expect(manifest).toMatchObject({
      name: `看板 ${home.machine}`,
      short_name: `看板 ${home.machine}`,
      start_url: "/",
      display: "standalone",
    });
    expect(manifest.theme_color).toMatch(/^#[0-9a-f]{6}$/i);
    expect(manifest.background_color).toMatch(/^#[0-9a-f]{6}$/i);
    const purposes = (manifest.icons as { purpose: string }[]).map((i) => i.purpose);
    expect(purposes).toContain("maskable");
    expect(manifest.icons).toHaveLength(3);
    for (const icon of manifest.icons as { src: string; type: string }[]) {
      const img = await request.get(icon.src);
      expect(img.status(), icon.src).toBe(200);
      expect(img.headers()["content-type"]).toBe(icon.type);
    }

    const sw = await request.get("/sw.js");
    expect(sw.status()).toBe(200);
    expect(sw.headers()["content-type"]).toContain("text/javascript");
    expect(sw.headers()["cache-control"]).toBe("no-cache");

    // 頁面宣告了 manifest、apple-touch-icon、亮暗各一個 theme-color；正式版註冊的 service worker 會就緒。
    await page.goto("/");
    await expect(page.locator('link[rel="manifest"]')).toHaveAttribute(
      "href",
      "/manifest.webmanifest",
    );
    await expect(page.locator('link[rel="apple-touch-icon"]')).toHaveCount(1);
    const touch = await page.locator('link[rel="apple-touch-icon"]').getAttribute("href");
    expect(touch).toMatch(/^\/icons\/apple-touch-icon\.png\?v=/);
    const touchImg = await request.get(touch ?? "");
    expect(touchImg.status()).toBe(200);
    expect(touchImg.headers()["content-type"]).toBe("image/png");
    await expect(page.locator('meta[name="theme-color"]')).toHaveCount(2);
    // 只有一個家的正式家不畫家標籤（DEV-156 U-07；兩個家以上或開發家才畫，vitest HomeMark.spec 守）、
    // 沒有開發用的橘色橫條、分頁標題沒有「開發｜」（WEB-76）。
    await expect(page.getByTestId("home-label")).toHaveCount(0);
    await expect(page.getByTestId("dev-strip")).toHaveCount(0);
    await expect(page).not.toHaveTitle(/^開發｜/);
    const scope = await page.evaluate(async () => (await navigator.serviceWorker.ready).scope);
    expect(new URL(scope).pathname).toBe("/");
  });
});

// Guards: REQ-28
test("TestDevicesPageExplainsMissingKey", async ({ page }) => {
  // e2e 的家沒有推播秘密檔：頁面要說清楚原因，開啟鈕不能按；iOS 的三個步驟一直都在。
  await page.goto("/settings/devices");
  await expect(page.getByText("推播還沒準備好").first()).toBeVisible();
  await expect(
    page.getByText("跟任何一位秘書說「幫我把推播準備好」，弄好之後這裡就能開啟。"),
  ).toBeVisible();
  // 畫面上不露指令（UXC-8，DEV-156 U-02）。
  await expect(page.getByRole("main")).not.toContainText("evergenie push");
  await expect(page.getByRole("button", { name: "在這台裝置開啟推播" })).toBeDisabled();
  const steps = page.getByRole("list").filter({ hasText: "加入主畫面" }).getByRole("listitem");
  await expect(steps).toHaveCount(3);
  await expect(steps.nth(1)).toContainText("加入主畫面");
});

// Guards: REQ-28, REQ-104
// 推播要真的註冊 service worker（其他測試預設擋掉，playwright.config.ts）。
test.describe(() => {
  test.use({ serviceWorkers: "allow" });
  test("TestDevicesEnableTestDisable", async ({ page }) => {
    // 真的推播要連推播服務，e2e 連不到：瀏覽器的訂閱與四個推播 API 都換成假的，驗畫面流程與送出的內容。
    await page.addInitScript(() => {
      let sub: unknown = null;
      const make = () => ({
        endpoint: "https://push.example/e2e-device",
        toJSON() {
          return { endpoint: this.endpoint, keys: { p256dh: "BPe2eKey_-", auth: "e2eAuth_-" } };
        },
        unsubscribe: async () => {
          sub = null;
          return true;
        },
      });
      PushManager.prototype.subscribe = async () => {
        sub = make();
        return sub as PushSubscription;
      };
      PushManager.prototype.getSubscription = async () => sub as PushSubscription | null;
      Notification.requestPermission = async () => "granted";
      Object.defineProperty(Notification, "permission", { get: () => "granted" });
    });
    const calls: { route: string; body: Record<string, unknown> }[] = [];
    let subscriptions = 0;
    await page.route("**/api/notify/*", async (route) => {
      const url = new URL(route.request().url());
      const name = url.pathname.split("/").pop() ?? "";
      if (!["push_key", "subscribe_push", "unsubscribe_push", "push_test"].includes(name)) {
        return route.continue();
      }
      calls.push({ route: name, body: route.request().postDataJSON() ?? {} });
      if (name === "subscribe_push") subscriptions = 1;
      if (name === "unsubscribe_push") subscriptions = 0;
      const body = name === "push_key" ? { public_key: "BAAA", subscriptions } : { subscriptions };
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    });

    await page.goto("/settings/devices");
    await expect(page.getByText("這台還沒開啟")).toBeVisible();
    await page.getByRole("button", { name: "在這台裝置開啟推播" }).click();
    await expect(page.getByText("這台已開啟")).toBeVisible();
    const subscribe = calls.find((c) => c.route === "subscribe_push");
    expect(subscribe?.body).toMatchObject({
      endpoint: "https://push.example/e2e-device",
      p256dh: "BPe2eKey_-",
      auth: "e2eAuth_-",
    });
    await expect(page.getByTestId("push-count")).toContainText("1 台");

    await page.getByRole("button", { name: "送一則測試通知" }).click();
    await expect(page.getByText("已送出測試通知")).toBeVisible();
    expect(calls.find((c) => c.route === "push_test")?.body).toMatchObject({
      endpoint: "https://push.example/e2e-device",
    });

    await page.getByRole("button", { name: "關閉這台的推播" }).click();
    await expect(page.getByText("這台還沒開啟")).toBeVisible();
    expect(calls.find((c) => c.route === "unsubscribe_push")?.body).toMatchObject({
      endpoint: "https://push.example/e2e-device",
    });
    await expect(page.getByTestId("push-count")).toContainText("還沒有裝置");
  });
});

// Guards: REQ-110
test("TestHealthPageListsPushItem", async ({ page, request }) => {
  const res = await request.post("/api/web/get_health", { data: {} });
  const health = (await res.json()) as { items: { name: string; ok: boolean; detail: string }[] };
  const names = health.items.map((i) => i.name);
  expect(names.slice(0, 3)).toEqual(["看板", "核心服務", "推播"]);
  await page.goto("/settings/health");
  const rows = page.getByTestId("health-item");
  await expect(rows).toHaveCount(health.items.length);
  await expect(rows.nth(0)).toContainText("看板運作中");
  await expect(rows.nth(0)).toHaveAttribute("data-ok", "true");
  // e2e 的家沒有推播金鑰：推播這一項是紅的，說的是白話。
  await expect(rows.nth(2)).toContainText("推播");
  await expect(rows.nth(2)).toContainText("還沒設定推播金鑰");
  await expect(rows.nth(2)).toHaveAttribute("data-ok", "false");
  // 底下一行小字指向更完整的檢查。
  // 不露指令（DEV-156 U-07）。
  await expect(page.getByTestId("health-doctor-hint")).toContainText("完整檢查");
  await expect(page.getByTestId("health-doctor-hint")).not.toContainText("evergenie doctor");
});
