import { expect, shot, test } from "./fixtures";

// DEV-107 任務單與待辦清單的截圖：手機亮／暗、桌機亮／暗（UXC 檢查要四張）。

const rand = () => Math.random().toString(36).slice(2, 8);

for (const scheme of ["light", "dark"] as const) {
  // 截圖：預設「未結」與選了「等你＋待做」兩個標籤。
  test(`TestShotsLists @shots ${scheme}`, async ({ page, seed, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    test.setTimeout(90_000);
    await page.emulateMedia({ colorScheme: scheme });
    await seed.reviewTask(`等你驗收的單 ${rand()}`);
    await seed.doingTask(`進行中的單 ${rand()}`);
    await seed.statusTask(`還沒開始的點子 ${rand()}`, "idea");
    await seed.statusTask(`等外部回覆的單 ${rand()}`, "waiting_external");
    const tonight = new Date();
    tonight.setHours(23, 59, 0, 0);
    const lastWeek = new Date(Date.now() - 3 * 24 * 3600_000);
    const nextWeek = new Date(Date.now() + 9 * 24 * 3600_000);
    await seed.reminder(`三天前沒做的事 ${rand()}`, lastWeek);
    await seed.reminder(`今天要做的事 ${rand()}`, tonight);
    await seed.reminder(`下週的事 ${rand()}`, nextWeek);
    await seed.reminder(`還沒定時間的事 ${rand()}`);

    await page.goto("/tasks");
    await expect(page.getByTestId("tasks-status-filter")).toBeVisible({ timeout: 20_000 });
    await expect(page.getByRole("link", { name: /還沒開始的點子/ }).first()).toBeVisible();
    await page.waitForTimeout(400);
    await shot(page, `lists-tasks-${d}-${scheme}`);
    // 選了「等你」「待做」兩個標籤。
    const chips = page.getByRole("group", { name: "依狀態篩選" });
    await chips.getByRole("button", { name: /^等你處理/ }).click();
    await chips.getByRole("button", { name: /^待做/ }).click();
    await expect(page.getByRole("link", { name: /還沒開始的點子/ }).first()).toBeVisible();
    await page.waitForTimeout(300);
    await shot(page, `lists-tasks-filtered-${d}-${scheme}`);

    await page.goto("/reminders");
    await expect(page.getByTestId("reminders-group-late")).toBeVisible({ timeout: 20_000 });
    await page.waitForTimeout(400);
    await shot(page, `lists-reminders-${d}-${scheme}`);
  });
}
