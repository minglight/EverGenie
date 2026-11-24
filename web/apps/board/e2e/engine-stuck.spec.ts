import type { Page } from "@playwright/test";
import { expect, freshHomeTest as test } from "./fixtures";

// 秘書那一側（引擎）整個不回應時，看板照常（WEB-175，DEV-159；作者 2026-11-14「Claude code 卡住不應該讓 web application 卡住」）。
// 自己一個家（freshHomeTest）：假引擎收到「【不回】」之後每個呼叫都卡著，這個家之後都是卡住的，不能跟別的 spec 共用。
// 手機與桌機都跑（列在 playwright.config.ts 的 mobileSpecs）。

const mainNav = (page: Page) => page.getByRole("navigation", { name: "主選單" });
const rand = () => Math.random().toString(36).slice(2, 8);
// 每一頁「幾秒內畫出來」的上限：比前後端的讀取時限（8、10 秒）短，等到時限才出現的不算過。
const SOON = 5_000;

test("TestBoardUsableWhenEngineNeverAnswers", async ({ page, seed }) => {
  test.setTimeout(120_000);
  const notice = `引擎卡住時的通知 ${rand()}`;
  const task = `引擎卡住時的單 ${rand()}`;
  await seed.approve(notice);
  await seed.reviewTask(task);
  // 從這一句起引擎完全不回：這一句的貼上、之後的每個呼叫（對話記錄、啟動提示…）都卡著。
  await seed.api("/api/bus/send", {
    from: "owner(web)",
    to: "code",
    kind: "chat",
    body: "【不回】",
  });
  await page.waitForTimeout(3_000); // 讓核心真的卡在跟引擎的呼叫上

  // 今天頁：外殼、等你處理的事。
  await page.goto("/");
  await expect(mainNav(page)).toBeVisible({ timeout: SOON });
  await expect(page.getByText(notice).first()).toBeVisible({ timeout: SOON });
  await expect(page.getByTestId("menu-basic-notice")).toHaveCount(0);

  // 訊息。
  await page.goto("/messages");
  await expect(page.getByText(notice).first()).toBeVisible({ timeout: SOON });

  // 任務單。
  await page.goto("/tasks");
  await expect(page.getByText(task).first()).toBeVisible({ timeout: SOON });

  // 對話清單：卡住的那位也列得出來。
  await page.goto("/chat");
  await expect(page.locator('[data-thread="code"]').first()).toBeVisible({ timeout: SOON });
  // 她的話一直沒被收：半分鐘後清單上寫她那邊沒回應（只看核心記下的排隊狀態，不去問引擎）。
  await expect(page.locator('[data-thread="code"]').first()).toContainText("秘書那邊沒回應", {
    timeout: 60_000,
  });
  // 另一位秘書不受影響。
  const rows = page.getByRole("navigation", { name: "討論串" }).locator("[data-thread]");
  expect(await rows.count()).toBeGreaterThan(1);
  await expect(rows.filter({ hasText: "沒回應" })).toHaveCount(1);
});
