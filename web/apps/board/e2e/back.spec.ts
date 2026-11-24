import type { Page } from "@playwright/test";
import { expect, test } from "./fixtures";

// 上一頁（FEN-29，DEV-122）：有站內歷史就退一步，退到頭才往上一層（replace）；一路按一定停在今天，不繞圈。

const back = (page: Page) => page.getByRole("link", { name: "上一頁", exact: true });
const rand = () => Math.random().toString(36).slice(2, 8);
const historyLength = (page: Page) => page.evaluate(() => window.history.length);
const pathOf = (page: Page) => {
  const u = new URL(page.url());
  return `${u.pathname}${u.search}`;
};

test("TestBackPopsHistoryAndNeverLoops @smoke", async ({ page, seed }) => {
  const id = await seed.reviewTask(`退回去 ${rand()}`);
  await page.goto("/tasks");
  const before = await historyLength(page);

  // 清單 → 詳情 → 上一頁回清單：不多疊歷史。
  await page.locator(`a[href^="/tasks/${id}"]`).first().click();
  await expect(page).toHaveURL(new RegExp(`/tasks/${id}`));
  const depth = await historyLength(page);
  expect(depth).toBeGreaterThanOrEqual(before);
  await back(page).click();
  await expect(page).toHaveURL(/\/tasks$/);
  expect(await historyLength(page)).toBe(depth);

  // 再按一次往上到今天，不是跳回詳情（沒有繞圈）；今天沒有上一頁，到此為止。
  await back(page).click();
  await expect.poll(() => pathOf(page)).toBe("/");
  await expect(back(page)).toHaveCount(0);
});

test("TestBackWithoutHistoryClimbsToToday", async ({ page, seed }) => {
  const id = await seed.reviewTask(`直接進來 ${rand()}`);
  // 新分頁直接開詳情（通知、書籤）：沒有站內歷史。
  await page.goto(`/tasks/${id}`);
  await expect(page).toHaveURL(new RegExp(`/tasks/${id}`));
  const start = await historyLength(page);

  await back(page).click();
  await expect(page).toHaveURL(/\/tasks$/);
  await back(page).click();
  await expect.poll(() => pathOf(page)).toBe("/");
  await expect(back(page)).toHaveCount(0);
  // 全程用 replace：歷史沒有變長，瀏覽器的上一頁也不會回到剛才那些頁。
  expect(await historyLength(page)).toBe(start);
});

test("TestBackAfterDeleteHasNoDeadPress", async ({ page, seed }) => {
  const id = await seed.reviewTask(`刪掉它 ${rand()}`);
  // 設定 → 清單 → 詳情：都走站內連結（內容區的 <a href> 走前端路由），歷史才接得起來。
  await page.goto("/settings/notify");
  await expect(back(page)).toBeVisible(); // 外殼畫好了，連結才走前端路由
  await page.evaluate(() => {
    const a = document.createElement("a");
    a.href = "/tasks";
    document.querySelector("main")?.append(a);
    a.click();
  });
  await expect(page).toHaveURL(/\/tasks$/);
  await page.locator(`a[href^="/tasks/${id}"]`).first().click();
  await expect(page).toHaveURL(new RegExp(`/tasks/${id}`));

  // 刪掉：回到清單（replace），歷史裡有兩筆清單。
  await page.getByRole("button", { name: `刪除 ${id}` }).click();
  await page.getByRole("button", { name: "確定刪除？" }).click();
  await expect(page).toHaveURL(/\/tasks$/);

  // 上一頁一按就回設定，不會原地不動。
  await back(page).click();
  await expect.poll(() => pathOf(page)).toBe("/settings/notify");
});

test("TestPackageLinksKeepHistory", async ({ page }) => {
  await page.goto("/p/closet/showroom");
  await expect(page.getByTestId("showroom")).toBeVisible();
  await page.evaluate(() => {
    (window as unknown as { __noReload: boolean }).__noReload = true;
  });
  // 套件頁的 <a href> 不整頁重載，站內歷史接得上：上一頁回到剛才那頁。
  const tile = page.getByTestId("outfit-tile").locator("a").first();
  if ((await tile.count()) === 0) {
    // 「加一套衣服」開到核心的角色頁（DEV-143；還沒有角色時是角色清單）。
    await page.getByTestId("showroom-add").click();
    await page.getByTestId("showroom-add-outfit").click();
    await expect(page).toHaveURL(/\/personas/);
  } else {
    await tile.click();
    await expect(page).toHaveURL(/\/p\/closet\/outfit/);
  }
  await back(page).click();
  await expect(page.getByTestId("showroom")).toBeVisible();
  await expect(page).toHaveURL(/\/p\/closet\/showroom$/);
  expect(
    await page.evaluate(() => (window as unknown as { __noReload?: boolean }).__noReload),
  ).toBe(true);
});

test("TestServerFileLinksStillLeaveTheBoard", async ({ page }) => {
  // /share 不是看板的路由：不能交給前端路由，不然會變成「找不到這頁」。
  await page.goto("/settings/notify");
  await expect(back(page)).toHaveCount(1);
  const handled = await page.evaluate(() => {
    const a = document.createElement("a");
    a.href = "/share/none.txt";
    a.target = "";
    document.querySelector("main")?.append(a);
    let prevented = false;
    document.addEventListener(
      "click",
      (e) => {
        prevented = e.defaultPrevented;
        e.preventDefault();
      },
      { once: true },
    );
    a.click();
    a.remove();
    return prevented;
  });
  expect(handled).toBe(false);
});
