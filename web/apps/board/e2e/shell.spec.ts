import { expect, test } from "./fixtures";

const mainNav = (page: import("@playwright/test").Page) =>
  page.getByRole("navigation", { name: "主選單" });

// Guards: REQ-105
test("TestMenuNotShownBeforeLoad", async ({ page }) => {
  await page.route("**/api/web/get_menu", async (route) => {
    await new Promise((r) => setTimeout(r, 1000));
    await route.continue();
  });
  await page.goto("/");
  // 拿到選單之前：只有全螢幕的載入畫面，沒有選單，也沒有頁面內容。
  await expect(page.getByTestId("boot-loading")).toBeVisible();
  await expect(page.locator("nav")).toHaveCount(0);
  await expect(page.getByRole("heading")).toHaveCount(0);
  // 拿到之後：選單出現，載入畫面消失。
  await expect(mainNav(page)).toBeVisible();
  await expect(page.getByTestId("boot-loading")).toHaveCount(0);
});

const urls: { path: string; final?: string }[] = [
  { path: "/" },
  { path: "/chat" },
  { path: "/chat/code" },
  { path: "/messages" },
  { path: "/messages/M-1" },
  { path: "/tasks" },
  // DEV-107：舊的「等你」分頁變成「未結」＋「等你」篩選，網址補上 status。
  { path: "/tasks?view=waiting", final: "/tasks?view=waiting&status=waiting_user" },
  { path: "/tasks/SEC-1" },
  { path: "/reminders" },
  { path: "/reminders?view=today" },
  { path: "/reminders/R-1" },
  // 手機的 /settings 是設定首頁的清單，桌機轉到第一頁（DEV-156 U-12）。
  { path: "/settings", final: "desktop:/settings/notify" },
  { path: "/settings/notify" },
  { path: "/settings/trust" },
  { path: "/settings/devices" },
  { path: "/settings/health" },
];

// Guards: REQ-107
test("TestEveryScreenHasURL @smoke", async ({ page, isMobile }) => {
  for (const u of urls) {
    await page.goto(u.path);
    await expect(page.getByRole("heading").first()).toBeVisible();
    const final = u.final?.startsWith("desktop:")
      ? isMobile
        ? u.path
        : u.final.slice("desktop:".length)
      : u.final;
    const want = final ?? u.path;
    const here = () => new URL(page.url()).pathname + new URL(page.url()).search;
    await expect.poll(here).toBe(want);
    const title = await page.title();
    expect(title, `${u.path} 的標題`).not.toContain("找不到");
    expect(title).toContain("看板");
    await page.reload();
    await expect(page.getByRole("heading").first()).toBeVisible();
    expect(await page.title(), `${u.path} 重新整理後的標題`).toBe(title);
    expect(here()).toBe(want);
  }
});

// Guards: REQ-108
test("TestOldTabLinkShows404", async ({ page, isMobile }) => {
  const res = await page.goto("/?tab=chat&room=code");
  expect(res?.status()).toBe(404);
  await expect(page.getByText("舊看板的連結")).toBeVisible();
  // 看板自己找不到的網址：白話說明＋回今天，外殼（選單）還在。
  await page.goto("/no-such-page");
  await expect(page.getByRole("heading", { name: "找不到這一頁" })).toBeVisible();
  await expect(mainNav(page)).toBeVisible();
  await page.getByRole("link", { name: "回到今天" }).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/");
  // 設定底下打錯的網址也是找不到頁，不默默轉到「通知」（DEV-156 U-32）；/settings 本身桌機照舊轉到第一個分頁，手機是設定首頁（U-12）。
  await page.goto("/settings/no-such-tab");
  await expect(page.getByRole("heading", { name: "找不到這一頁" })).toBeVisible();
  await expect(mainNav(page)).toBeVisible();
  expect(new URL(page.url()).pathname).toBe("/settings/no-such-tab");
  await page.goto("/settings");
  await expect
    .poll(() => new URL(page.url()).pathname)
    .toBe(isMobile ? "/settings" : "/settings/notify");
});

// Guards: REQ-106
test("TestSettingsAlwaysInMenu", async ({ page, isMobile }) => {
  await page.goto("/");
  await expect(mainNav(page)).toBeVisible();
  if (isMobile) {
    // 手機：底部列放前 5 個，設定在「更多」裡。
    await page.getByRole("button", { name: "更多" }).click();
    const sheet = page.getByRole("dialog", { name: "更多" });
    await expect(sheet).toBeVisible();
    await sheet.getByRole("link", { name: "設定" }).click();
  } else {
    await mainNav(page).getByRole("link", { name: "今天" }).waitFor();
    await page.getByRole("link", { name: "設定" }).click();
  }
  // 手機到設定首頁的清單，桌機到第一頁（DEV-156 U-12）。
  await expect
    .poll(() => new URL(page.url()).pathname)
    .toBe(isMobile ? "/settings" : "/settings/notify");
});

// 選單的樹是存在家裡的設定：這個測試自己設成「今天、聊天、待辦、訊息、任務單、設定」，結束一定改回預設。
// Guards: REQ-105
test("TestMobileBarSixSlots @smoke", async ({ page, request, baseURL, isMobile }, info) => {
  info.skip(!isMobile, "底部列只在手機寬度");
  const setTree = async (tree: unknown[]) => {
    const res = await request.post(`${baseURL}/api/web/set_menu_prefs`, {
      data: { prefs: { tree, renderer: "" } },
    });
    expect(res.ok(), await res.text()).toBe(true);
  };
  await setTree(
    [
      "core.overview",
      "core.chat",
      "core.reminders",
      "core.messages",
      "core.tasks",
      "core.settings",
    ].map((item) => ({ item })),
  );
  try {
    await page.goto("/");
    const nav = mainNav(page);
    // 底部列 6 個按鈕：前 5 個是選單的前 5 項（第 5 個是任務單），最後一個是「更多」（裝了套件，放不下的進去）。
    const tabs = nav.locator("a, button");
    await expect(tabs).toHaveCount(6);
    const names = ["今天", "聊天", "待辦", "訊息", "任務單", "更多"];
    for (let i = 0; i < names.length; i++) {
      await expect(tabs.nth(i)).toContainText(names[i]);
    }
    await expect(nav.getByRole("link", { name: /^任務單/ })).toBeVisible();
    // 字沒被截斷：每一格的文字都在格子裡、只有一行；每格可點範圍 ≥ 44×44。
    for (let i = 0; i < names.length; i++) {
      const m = await tabs.nth(i).evaluate((el) => {
        const label = [...el.childNodes].find((n) => n.nodeType === Node.TEXT_NODE);
        if (!label) return { ok: false, lines: 0, fits: false };
        const range = document.createRange();
        range.selectNodeContents(label);
        const rects = [...range.getClientRects()];
        const box = el.getBoundingClientRect();
        const fits = rects.every((r) => r.left >= box.left - 0.5 && r.right <= box.right + 0.5);
        return { ok: true, lines: rects.length, fits };
      });
      expect(m.ok).toBe(true);
      expect(m.lines).toBe(1);
      expect(m.fits).toBe(true);
      const box = await tabs.nth(i).boundingBox();
      expect(box?.width ?? 0).toBeGreaterThanOrEqual(44);
      expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
    }
    // 「更多」裡有設定，Esc 關閉。
    await nav.getByRole("button", { name: /更多/ }).click();
    const sheet = page.getByRole("dialog", { name: "更多" });
    await expect(sheet.getByRole("link", { name: /設定/ })).toBeVisible();
    await expect(sheet.getByRole("link", { name: /任務單/ })).toHaveCount(0);
    await page.keyboard.press("Escape");
    await expect(sheet).toHaveCount(0);
  } finally {
    await setTree([]);
  }
});

// Guards: REQ-115
test("TestMobileBarHeight", async ({ page, isMobile }, info) => {
  info.skip(!isMobile, "底部列只在手機寬度");
  await page.goto("/");
  const nav = mainNav(page);
  await expect(nav).toBeVisible();
  // Playwright 的 safe-area 是 0：底部選單不再被硬墊高，整條 ≤ 56px，每個項目仍 ≥ 44×44。
  const bar = await nav.boundingBox();
  expect(bar?.height ?? 999).toBeLessThanOrEqual(56);
  const tabs = nav.locator("a, button");
  const n = await tabs.count();
  expect(n).toBeGreaterThan(0);
  for (let i = 0; i < n; i++) {
    const box = await tabs.nth(i).boundingBox();
    expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
    expect(box?.width ?? 0).toBeGreaterThanOrEqual(44);
  }
});

// 手機往下捲時頭像那排收起來、往上捲回來，標題列不動（2026-11-02 稽核）。
test("TestMobileFilterBarHidesOnScrollDown", async ({ page, isMobile }) => {
  test.skip(!isMobile, "手機版面");
  // 矮一點的畫面，測試資料的今天頁才捲得動。
  await page.setViewportSize({ width: 390, height: 520 });
  await page.goto("/");
  const wrap = page.getByTestId("filter-bar-wrap");
  await expect(wrap).toBeVisible();
  await expect(wrap).not.toHaveAttribute("data-hidden", "true");
  const main = page.locator("main");
  const scrollable = await main.evaluate((el) => el.scrollHeight - el.clientHeight);
  expect(scrollable).toBeGreaterThan(200);
  // 收起後位置要還在新的捲動範圍內（範圍夠才收，DEV-111），所以不用固定捲到很下面；範圍多少都能收。
  await main.evaluate((el) => el.scrollTo({ top: 100 }));
  await expect(wrap).toHaveAttribute("data-hidden", "true");
  await main.evaluate((el) => el.scrollTo({ top: 60 }));
  await expect(wrap).not.toHaveAttribute("data-hidden", "true");
  await main.evaluate((el) => el.scrollTo({ top: 0 }));
  await expect(wrap).not.toHaveAttribute("data-hidden", "true");
});

// DEV-111（跑在今天頁，用視窗高度調出範圍）：可捲的範圍只比頭像列多一點（約 200～290px）時，收起來後範圍會縮到 200 以下；舊寫法這時一有捲動就展開、
// 展開範圍又變大，捲一下抖一下。現在收起來之後繼續捲要維持收起，範圍太短（放不下收起後的位置）就一直不收。
test("TestMobileFilterBarNoFlickerWhenRangeShort", async ({ page, isMobile }) => {
  test.skip(!isMobile, "手機版面");
  await page.setViewportSize({ width: 390, height: 520 });
  await page.goto("/");
  const wrap = page.getByTestId("filter-bar-wrap");
  await expect(wrap).toBeVisible();
  const main = page.locator("main");
  const range = () => main.evaluate((el) => el.scrollHeight - el.clientHeight);
  // 範圍隨畫面高度一比一變；資料可能晚到，所以邊量邊調，直到落在 230～270。
  await expect(async () => {
    const r = await range();
    if (r < 230 || r > 270) {
      const h = page.viewportSize()?.height ?? 520;
      await page.setViewportSize({ width: 390, height: Math.max(300, h + r - 250) });
    }
    const after = await range();
    expect(after).toBeGreaterThanOrEqual(230);
    expect(after).toBeLessThanOrEqual(270);
  }).toPass({ timeout: 10_000 });
  const hidden = "data-hidden";
  await main.evaluate((el) => el.scrollTo({ top: 100 }));
  await expect(wrap).toHaveAttribute(hidden, "true");
  for (const top of [110, 125, 140]) {
    await main.evaluate((el, y) => el.scrollTo({ top: y }), top);
    await page.waitForTimeout(300);
    await expect(wrap).toHaveAttribute(hidden, "true");
  }
  // 回到頂端才展開；再一路捲到底（收起後位置放不下）不能收起又彈回來。
  await main.evaluate((el) => el.scrollTo({ top: 0 }));
  await expect(wrap).not.toHaveAttribute(hidden, "true");
  await main.evaluate((el) => el.scrollTo({ top: el.scrollHeight }));
  await page.waitForTimeout(600);
  await expect(wrap).not.toHaveAttribute(hidden, "true");
});

// DEV-156 U-27：手機的「更多」抽屜與搜尋面板右上角有關閉鈕（不必知道要點外面或往下拉）。
test("TestMobileSheetAndSearchHaveCloseButton", async ({ page, isMobile }, info) => {
  info.skip(!isMobile, "手機版面");
  await page.goto("/");
  await page.getByRole("button", { name: "更多" }).click();
  const sheet = page.getByRole("dialog", { name: "更多" });
  await expect(sheet).toBeVisible();
  const close = sheet.getByRole("button", { name: "關閉" });
  const box = await close.boundingBox();
  expect(box?.width).toBeGreaterThanOrEqual(44);
  expect(box?.height).toBeGreaterThanOrEqual(44);
  await close.click();
  await expect(sheet).toHaveCount(0);

  await page.getByRole("banner").getByRole("button", { name: "搜尋", exact: true }).click();
  const palette = page.getByRole("dialog", { name: "搜尋或下指令" });
  await expect(palette).toBeVisible();
  await palette.getByRole("button", { name: "關閉搜尋" }).click();
  await expect(palette).toHaveCount(0);
});

// DEV-156 U-25：最小寬降到 320（WCAG 1.4.10）——主要頁面在 320 寬都不能左右捲。
test("TestNoHorizontalScrollAt320", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 640 });
  const packageContent: Record<string, string> = {
    "/p/habits/list": "habits-list",
    "/p/closet/showroom": "closet-points",
  };
  for (const path of [
    "/",
    "/chat",
    "/reminders",
    "/messages",
    "/tasks",
    "/goals",
    "/personas",
    "/settings/agents",
    "/settings/trust",
    "/settings/health",
    "/p/habits/list",
    "/p/closet/showroom",
    "/no-such-page",
  ]) {
    await page.goto(path);
    await expect(page.getByRole("main")).toBeVisible();
    // 等內容真的畫出來再量（不用固定等待）：套件頁等它自己的內容，其他頁等頁首的標題；再等字型載完。
    const ready = packageContent[path];
    if (ready) await expect(page.getByTestId(ready)).toBeVisible();
    else await expect(page.getByRole("banner").getByRole("heading", { level: 1 })).toBeVisible();
    await page.evaluate(() => document.fonts.ready);
    const wide = await page.evaluate(() =>
      Math.max(document.documentElement.scrollWidth, document.body.scrollWidth),
    );
    expect(wide, `${path} 在 320 寬不能左右捲`).toBeLessThanOrEqual(320);
  }
  // 角色頁上面那排分頁（介紹／衣櫃／展示間…）：字不折行，放不下是那一排自己橫向捲，頁面不捲（DEV-156）。
  await page.goto("/personas/helper");
  const tabs = page.getByTestId("page-tabs");
  await expect(tabs).toBeVisible();
  const heights = await tabs
    .getByRole("link")
    .evaluateAll((els) => els.map((el) => el.getBoundingClientRect().height));
  expect(heights.length).toBeGreaterThan(1);
  for (const h of heights) expect(h).toBeLessThanOrEqual(48);
  expect(await tabs.evaluate((el) => getComputedStyle(el).overflowX)).toBe("auto");
  expect(
    await page.evaluate(() =>
      Math.max(document.documentElement.scrollWidth, document.body.scrollWidth),
    ),
  ).toBeLessThanOrEqual(320);
});

// DEV-156 U-31：每頁一個第一層標題（h1）。手機的頁首是 h1；詳情頁內文自己有標題時頁首退成一般文字。
// 桌機今天頁的 h1 是問候語，不是「等你處理」。
test("TestOneH1PerPage", async ({ page, seed, isMobile }) => {
  const content = `繳電話費 ${Date.now()}`;
  const id = await seed.reminder(content);
  const h1 = page.getByRole("heading", { level: 1 });
  for (const path of [
    "/",
    "/reminders",
    `/reminders/${id}`,
    "/reminders/R-999999",
    "/messages",
    "/tasks",
    "/tasks/T-999999",
    "/goals",
    "/personas",
    "/personas/no-such-persona-xyz",
    "/settings/health",
    "/p/habits/list",
    "/no-such-page",
  ]) {
    await page.goto(path);
    await expect(page.getByRole("main")).toBeVisible();
    await expect(h1, `${path} 要剛好一個 h1`).toHaveCount(1);
  }
  await page.goto(`/reminders/${id}`);
  await expect(h1).toHaveText(content);
  if (!isMobile) {
    // 桌機今天頁：有事等你（兩欄）時 h1 是問候語、不是「等你處理」；點開一件事看細節時問候語不在，h1 才是「等你處理」。
    await seed.reviewTask(`一個標題 ${Date.now()}`);
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "等你處理", level: 2 })).toBeVisible();
    await expect(h1).toHaveCount(1);
    await expect(h1).not.toHaveText("等你處理");
  }
});
