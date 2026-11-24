import type { Page } from "@playwright/test";
import { expect, test } from "./fixtures";

// 一個請求卡住、變慢或回錯，首頁不能整頁進不去（WEB-175，DEV-159；作者 2026-11-14「SPA 不應該因為一個功能卡住就不能進去」）：
// 外殼與導覽照常出現，只有那一塊說讀不到、可以再試。手機與桌機都跑（列在 playwright.config.ts 的 mobileSpecs）。

const mainNav = (page: Page) => page.getByRole("navigation", { name: "主選單" });
const quickInput = (page: Page) => page.getByRole("textbox", { name: "快速輸入" });
const rand = () => Math.random().toString(36).slice(2, 8);

// 手機的導覽在底部列、桌機在側欄：兩邊「聊天」都是一個連結。
const chatLink = (page: Page) => mainNav(page).getByRole("link", { name: /^聊天/ });

test("TestHomeUsableWhenMenuNeverAnswers", async ({ page, seed }) => {
  test.setTimeout(90_000);
  const title = `選單卡住時的通知 ${rand()}`;
  await seed.approve(title);
  let stuck = true;
  await page.route("**/api/web/get_menu", async (route) => {
    if (stuck) return; // 永遠不回
    await route.continue();
  });
  await page.goto("/");

  // 選單一直不回：等一下（MENU_GRACE_MS）外殼就用基本選單畫出來，上面一行白話說明；今天頁的內容照常。
  await expect(mainNav(page)).toBeVisible({ timeout: 8_000 });
  await expect(page.getByTestId("boot-loading")).toHaveCount(0);
  await expect(page.getByTestId("menu-basic-notice")).toContainText("基本的選單");
  await expect(quickInput(page)).toBeVisible();
  await expect(page.getByText(title).first()).toBeVisible();

  // 導覽真的能用：點「聊天」到聊天頁，再回今天。
  await chatLink(page).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/chat");
  await mainNav(page).getByRole("link", { name: /^今天/ }).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/");

  // 選單恢復：卡住的那次等到時限放棄、自己再試，讀到了說明就消失（不用重新整理）。
  stuck = false;
  await expect(page.getByTestId("menu-basic-notice")).toHaveCount(0, { timeout: 45_000 });
  await expect(mainNav(page)).toBeVisible();
});

test("TestHomeUsableWhenMenuFails", async ({ page }) => {
  let broken = true;
  await page.route("**/api/web/get_menu", async (route) => {
    if (!broken) return route.continue();
    await route.fulfill({
      status: 500,
      contentType: "application/json",
      body: JSON.stringify({ code: "internal", message: "內部錯誤" }),
    });
  });
  await page.goto("/");

  // 選單回錯：不是整頁「連不到看板服務」，外殼用基本選單畫出來，說明旁邊有「再試一次」。
  const notice = page.getByTestId("menu-basic-notice");
  await expect(notice).toContainText("選單讀不到", { timeout: 15_000 });
  await expect(mainNav(page)).toBeVisible();
  await expect(quickInput(page)).toBeVisible();

  broken = false;
  await notice.getByRole("button", { name: "再試一次" }).click();
  await expect(notice).toHaveCount(0);
  await expect(mainNav(page)).toBeVisible();
});

test("TestTodayOtherBlocksShowWhenOverviewNeverAnswers", async ({ page, seed, isMobile }) => {
  test.setTimeout(90_000);
  const title = `總覽卡住時的通知 ${rand()}`;
  await seed.approve(title);
  let stuck = true;
  await page.route("**/api/web/get_overview", async (route) => {
    if (stuck) return; // 永遠不回
    await route.continue();
  });
  await page.goto("/");

  // 總覽（等你處理的事）一直不回：導覽、輸入框照常，只有那一塊說「還在讀」。
  await expect(mainNav(page)).toBeVisible();
  await expect(quickInput(page)).toBeVisible({ timeout: 8_000 });
  await expect(page.getByTestId("today-waiting")).toBeVisible();

  // 等到時限：那一塊變成白話的「載不進來」＋「再試一次」，不會一直轉圈。
  const failed = page.getByTestId("today-unavailable");
  await expect(failed).toBeVisible({ timeout: 45_000 });
  await expect(failed).toContainText("今天的資料暫時載不進來");
  await expect(quickInput(page)).toBeVisible();

  // 在輸入框做個記號：總覽讀到、版面換回來之後要是同一個輸入框（沒有重掛，打到一半焦點不會掉）。
  await quickInput(page).evaluate((el) => {
    (el as HTMLElement & { hgMark?: string }).hgMark = "same";
  });

  // 恢復後按「再試一次」：等你處理的事出現。
  stuck = false;
  await failed.getByRole("button", { name: "再試一次" }).click();
  await expect(page.getByText(title).first()).toBeVisible();
  await expect(failed).toHaveCount(0);
  // 寬螢幕的今天頁是另一個版面（三欄），整個換掉；手機是同一個輸入框。
  if (isMobile) {
    expect(
      await quickInput(page).evaluate((el) => (el as HTMLElement & { hgMark?: string }).hgMark),
    ).toBe("same");
  }
});

test("TestBootScreenExplainsWhenProgramFailsToLoad", async ({ page }) => {
  // 看板的程式沒載到（服務正在重開時常見）：不是空白，是白話說明與「重新整理」。
  let broken = true;
  await page.route("**/assets/index-*.js", async (route) => {
    if (broken) return route.abort("failed");
    await route.continue();
  });
  await page.goto("/");
  const boot = page.locator("#boot");
  await expect(boot).toContainText("看板的程式沒有載到");
  const reload = boot.getByRole("button", { name: "重新整理" });
  await expect(reload).toBeVisible();

  broken = false;
  await reload.click();
  await expect(mainNav(page)).toBeVisible();
  await expect(page.locator("#boot")).toHaveCount(0);
});

test("TestFontSiteStuckDoesNotBlankThePage", async ({ page }) => {
  // 外面的字型站一直不回：看板照常畫出來（以前整頁空白到它回應為止）。
  await page.route("https://fonts.googleapis.com/**", async () => {
    // 永遠不回
  });
  await page.goto("/", { waitUntil: "commit" });
  await expect(mainNav(page)).toBeVisible();
  await expect(quickInput(page)).toBeVisible();
});
