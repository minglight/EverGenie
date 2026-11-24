import { expect, test } from "./fixtures";

// DEV-156 U-21、WEB-185：連不上看板時，service worker 回自己的離線頁；連回來自動回原頁。
test.use({ serviceWorkers: "allow" }); // 其他測試預設擋掉 worker（playwright.config.ts）

// 正式版才註冊 worker（e2e 跑的就是正式版）；第一次載入後 worker 接手，之後的導覽才經過它。
test("TestOfflinePageComesBack", async ({ page, context }) => {
  await page.goto("/");
  await expect
    .poll(() => page.evaluate(() => !!navigator.serviceWorker.controller), { timeout: 15_000 })
    .toBe(true);
  // 正常時照常拿伺服器那份。
  await page.goto("/reminders");
  await expect(page.getByTestId("offline-page")).toHaveCount(0);

  await context.setOffline(true);
  await page.goto("/tasks").catch(() => {});
  const offline = page.getByTestId("offline-page");
  await expect(offline).toBeVisible();
  await expect(offline).toContainText("連不上看板");
  await expect(offline).toContainText("會自己重開，通常一分鐘內就好");
  // 白話的下一步（DEV-153）：等多久、還不行到那台 Mac 開「evergenie 修復」；不寫指令。
  const next = page.getByTestId("offline-next");
  await expect(next).toContainText("等了兩三分鐘還是這樣");
  await expect(next).toContainText("搜尋「evergenie 修復」打開它");
  await expect(offline).not.toContainText("終端機");
  await expect(page.getByRole("button", { name: "現在再試" })).toBeVisible();

  await context.setOffline(false);
  await expect(offline).toHaveCount(0, { timeout: 15_000 });
  await expect(page).toHaveURL(/\/tasks$/);
});
