import { expect, twoHomesTest as test } from "./fixtures";

// 別台不通（不回應、資料卡住）時，這一台的首頁照常（WEB-175，DEV-159）。只跑桌機（沒有手機專屬內容）。

const rand = () => Math.random().toString(36).slice(2, 8);

test("TestOwnHomeUsableWhenOtherHomeNeverAnswers", async ({ page, homes, seed }) => {
  test.setTimeout(90_000);
  const title = `別台不回時 A 的通知 ${rand()}`;
  await seed.approve(title);
  // B 的每個請求都一直不回（連線接了、沒有回應；跟斷線不一樣，瀏覽器不會自己放棄）。
  await page.route(`${homes.b.url}/**`, async () => {
    // 永遠不回
  });
  await page.goto("/");

  // A 自己的導覽與內容照常，不等 B。
  await expect(page.getByRole("navigation", { name: "主選單" })).toBeVisible();
  await expect(page.getByText(title).first()).toBeVisible();
  // B 試連等到時限：只在 B 的那一行說連不到。
  await expect(page.locator('[data-testid="site-notice"][data-state="down"]').first()).toBeVisible({
    timeout: 20_000,
  });
  await expect(page.getByText(title).first()).toBeVisible();
});

test("TestOwnHomeUsableWhenOtherHomeDataNeverAnswers", async ({ page, homes, seed }) => {
  test.setTimeout(90_000);
  const title = `別台資料卡住時 A 的通知 ${rand()}`;
  await seed.approve(title);
  // B 連得上（get_mode 照常），但它的總覽一直不回。
  await page.route(`${homes.b.url}/api/web/get_overview`, async () => {
    // 永遠不回
  });
  await page.goto("/");

  await expect(page.getByRole("navigation", { name: "主選單" })).toBeVisible();
  await expect(page.getByText(title).first()).toBeVisible();
  // 等到時限：說 B 的那一份讀不到（以前沒有時限，永遠不說）；A 的照常。
  await expect(page.getByText(/的等你處理的事讀不到/).first()).toBeVisible({ timeout: 45_000 });
  await expect(page.getByText(title).first()).toBeVisible();
});
