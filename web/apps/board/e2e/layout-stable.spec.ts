import type { Page, Route } from "@playwright/test";
import { expect, test } from "./fixtures";

// 手機底部選單換分頁時，上方的秘書篩選列不能跳（REQ-155，F2）：同一個節點、同樣的位置與高度、頭像一出現就是不透明。

const bar = (page: Page) => page.locator('section[aria-label="秘書篩選"]');

async function box(page: Page) {
  const b = await bar(page).boundingBox();
  expect(b).not.toBeNull();
  return { y: (b as { y: number }).y, h: (b as { height: number }).height };
}

// 切換後「馬上」讀一次所有頭像的 computed opacity（不用會等待的斷言，才抓得到淡入的瞬間）。
const avatarOpacities = (page: Page) =>
  page.evaluate(() =>
    [...document.querySelectorAll('section[aria-label="秘書篩選"] img')].map(
      (img) => getComputedStyle(img).opacity,
    ),
  );

// 用底部選單換頁（不准 page.goto）；手機的任務單在「更多」裡。
async function navTo(page: Page, label: string) {
  const nav = page.getByRole("navigation", { name: "主選單" });
  const link = nav.getByRole("link", { name: new RegExp(`^${label}`) });
  if (await link.count()) {
    await link.first().click();
    return;
  }
  await nav.getByRole("button", { name: /^更多/ }).click();
  await page
    .getByRole("dialog", { name: "更多" })
    .getByRole("link", { name: new RegExp(`^${label}`) })
    .click();
}

// 回應延遲 ms 毫秒；served() 是已經回給頁面的次數。
async function delay(page: Page, pattern: string, ms: number) {
  let served = 0;
  await page.route(pattern, async (route: Route) => {
    await new Promise((r) => setTimeout(r, ms));
    // 等的時候測試可能已經收掉攔截，這個請求被放行過了：不算回給頁面，也不是錯。
    await route.continue().then(
      () => served++,
      () => {},
    );
  });
  return () => served;
}

const nextFrames = (page: Page) =>
  page.evaluate(
    () => new Promise<void>((r) => requestAnimationFrame(() => requestAnimationFrame(() => r()))),
  );

// Guards: REQ-155
test("TestTabSwitchNoLayoutShift @smoke", async ({ page, isMobile }, info) => {
  info.skip(!isMobile, "手機版的外殼篩選列");
  await page.goto("/");
  await expect(bar(page)).toBeVisible();
  await expect.poll(async () => (await avatarOpacities(page)).join()).toBe("1,1");
  await bar(page).evaluate((el) => {
    el.dataset.mark = "keep";
  });
  const start = await box(page);

  for (const [label, path] of [
    ["聊天", /\/chat$/],
    ["待辦", /\/reminders/],
    ["任務單", /\/tasks/],
    ["訊息", /\/messages/],
    ["今天", /\/$/],
  ] as const) {
    await navTo(page, label);
    await page.waitForURL(path);
    // 馬上讀：頭像不能是淡入前的透明。
    const opacities = await avatarOpacities(page);
    expect(opacities.length).toBeGreaterThan(0);
    expect(opacities.every((o) => o === "1")).toBe(true);
    await expect(page.locator('[data-mark="keep"]')).toHaveCount(1);
    const now = await box(page);
    expect(Math.abs(now.y - start.y)).toBeLessThanOrEqual(1);
    expect(Math.abs(now.h - start.h)).toBeLessThanOrEqual(1);
  }
});

// Guards: REQ-155
test("TestFilterBarStableWhileLoading @smoke", async ({ page, isMobile }, info) => {
  info.skip(!isMobile, "手機版的外殼篩選列");
  // 名冊晚 1.5 秒才到：篩選列已經在，高度跟名冊到了之後一樣。
  const roster = await delay(page, "**/api/web/get_roster", 1500);
  await page.goto("/");
  await expect(bar(page)).toBeVisible();
  const early = await box(page);
  expect(roster()).toBe(0);
  await expect(bar(page).getByRole("button", { name: /^Builder/ })).toBeVisible();
  expect(roster()).toBeGreaterThan(0);
  const late = await box(page);
  expect(Math.abs(late.y - early.y)).toBeLessThanOrEqual(1);
  expect(Math.abs(late.h - early.h)).toBeLessThanOrEqual(1);
  await page.unroute("**/api/web/get_roster");

  // 待辦、任務單、訊息的清單晚 1.5 秒才到：資料到之前與之後，篩選列都沒動。
  const start = late;
  const lists = [
    ["待辦", "**/api/reminders/list"],
    ["任務單", "**/api/tasks/*"],
    ["訊息", "**/api/notify/list"],
  ] as const;
  for (const [label, pattern] of lists) {
    const served = await delay(page, pattern, 1500);
    const before = served();
    await navTo(page, label);
    const during = await box(page);
    expect(Math.abs(during.y - start.y)).toBeLessThanOrEqual(1);
    expect(Math.abs(during.h - start.h)).toBeLessThanOrEqual(1);
    await expect.poll(served, { timeout: 8000 }).toBeGreaterThan(before);
    await nextFrames(page);
    const after = await box(page);
    expect(Math.abs(after.y - start.y)).toBeLessThanOrEqual(1);
    expect(Math.abs(after.h - start.h)).toBeLessThanOrEqual(1);
    await page.unroute(pattern);
  }
});

// Guards: REQ-155
test("TestTabletSingleFilter", async ({ page, isMobile }, info) => {
  info.skip(isMobile, "平板寬度走桌機的側欄版面");
  await page.setViewportSize({ width: 820, height: 1180 });
  for (const path of ["/", "/reminders", "/chat"]) {
    await page.goto(path);
    // 側欄的「只看這位」已經畫好（名冊到了），整個畫面只有這一個篩選。
    const filters = page.locator('section[aria-label="秘書篩選"]');
    await expect(filters.getByRole("button", { name: /^Builder/ })).toBeVisible();
    await expect(filters).toHaveCount(1);
  }
});
