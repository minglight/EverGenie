import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import type { Page } from "@playwright/test";
import { expect, test } from "./fixtures";

// 今天頁的升級結果橫幅（WEB-173、system 卡 UPD-14，DEV-155 S6）：服務起得來、但上一次更新沒成功時，首頁最上面講結果與可以按的下一步。
// 直接寫這個 worker 的家裡的升級紀錄（runtime/upgrade/state.json、check.json、rescue.json）造出三種結果，經真的服務讀回來。
// 「再試一次」「退回上一版」攔下 api 回假的進度（不真的叫起升級行程：e2e 的服務找得到這個 repo，真的送出會在程式資料夾上跑升級），
// 只驗看板送了對的要求、就地顯示進度。每個測試結束都把寫的檔刪掉（同一個 worker 的別的 spec 共用這個家）。

const FILES = ["state.json", "check.json", "rescue.json"] as const;
type UpgradeFile = (typeof FILES)[number];

function writeUpgrade(home: string, files: Partial<Record<UpgradeFile, unknown>>) {
  const dir = path.join(home, "runtime", "upgrade");
  mkdirSync(dir, { recursive: true });
  for (const [name, body] of Object.entries(files)) {
    writeFileSync(path.join(dir, name), JSON.stringify(body));
  }
}

function clearUpgrade(home: string) {
  for (const name of FILES) rmSync(path.join(home, "runtime", "upgrade", name), { force: true });
}

const now = () => new Date().toISOString();
const failedState = (extra: Record<string, unknown>) => ({
  kind: "upgrade",
  from: "0.0.1",
  to: "0.0.2",
  ok: false,
  log: "/tmp/e2e-home/runtime/logs/upgrade.log",
  started_at: now(),
  finished_at: now(),
  ...extra,
});

const banner = (page: Page) => page.getByTestId("upgrade-banner");

test.afterEach(async ({ hgServer }) => {
  clearUpgrade(hgServer.home);
});

test("TestUpgradeBannerUntouched @smoke", async ({ page, hgServer }) => {
  writeUpgrade(hgServer.home, {
    "state.json": failedState({
      phase: "failed",
      outcome: "untouched",
      reason: "下載不到新版的檔案，沒有升級；舊版照常在跑",
      next: "等網路正常了再更新一次",
    }),
    "check.json": {
      current: "0.0.1",
      target: "0.0.2",
      tag: "v0.0.2",
      channel: "release",
      checked_at: now(),
    },
  });
  let sent: unknown = null;
  await page.route("**/api/system/update_apply", async (route) => {
    sent = route.request().postDataJSON();
    await route.fulfill({
      json: {
        current: "0.0.1",
        core_found: true,
        state: {
          kind: "upgrade",
          phase: "fetch",
          from: "0.0.1",
          to: "0.0.2",
          ok: false,
          started_at: now(),
        },
      },
    });
  });
  await page.goto("/");
  const b = banner(page);
  await expect(b).toHaveAttribute("data-outcome", "untouched");
  await expect(b.getByTestId("update-headline")).toHaveText(
    "更新到 0.0.2 沒成功，舊版 0.0.1 照常在跑。",
  );
  await expect(b.getByTestId("update-next")).toHaveText("下一步：等網路正常了再更新一次");
  await expect(b.getByRole("button", { name: "退回上一版" })).toHaveCount(0);

  await b.getByRole("button", { name: "看細節" }).click();
  await expect(b.getByTestId("upgrade-details")).toContainText("這次：0.0.1 → 0.0.2");
  await expect(b.getByTestId("upgrade-details")).toContainText("upgrade.log");

  await b.getByRole("button", { name: "再試一次" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("現在更新到 0.0.2？");
  await dialog.getByRole("button", { name: "更新" }).click();
  await expect(b.getByTestId("update-progress")).toContainText("正在更新到 0.0.2");
  expect(sent).toMatchObject({ version: "0.0.2" });
  await page.waitForResponse("**/api/system/update_status");
  await expect(b.getByTestId("update-progress")).toContainText("正在更新到 0.0.2");
});

test("TestUpgradeBannerRolledBack", async ({ page, hgServer }) => {
  writeUpgrade(hgServer.home, {
    "state.json": failedState({
      phase: "rolled_back",
      outcome: "rolled_back",
      reason: "新版檢查沒過：重開之後服務一直沒有起來",
      next: "這一版先跳過，已經請電腦秘書查原因；你不用做什麼，舊版照常可以用",
    }),
  });
  await page.goto("/");
  const b = banner(page);
  await expect(b).toHaveAttribute("data-outcome", "rolled_back");
  await expect(b.getByTestId("update-headline")).toHaveText(
    "更新到 0.0.2 沒成功，已換回 0.0.1，你的資料沒動。",
  );
  await expect(b.getByTestId("update-reason")).toHaveText(
    "原因：新版檢查沒過：重開之後服務一直沒有起來",
  );
  await expect(b.getByRole("button", { name: "再試一次" })).toHaveCount(0);
  await expect(b.getByRole("button", { name: "看細節" })).toBeVisible();
  // 知道了：收起來，重新整理也不再出現（記在這個瀏覽器）。
  await b.getByRole("button", { name: "知道了" }).click();
  await expect(b).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole("button", { name: "看細節" })).toHaveCount(0);
});

test("TestUpgradeBannerStranded", async ({ page, hgServer }) => {
  writeUpgrade(hgServer.home, {
    "state.json": failedState({
      phase: "failed",
      outcome: "stranded",
      reason: "新版檢查沒過；換回時換不回舊版的資料夾",
      next: "到看板首頁按「退回上一版」再試一次換回；還是不行就請電腦秘書處理",
    }),
    "rescue.json": { from: "0.0.1", to: "0.0.2", install: "download", dir: "0.0.1", at: now() },
  });
  let asked = 0;
  await page.route("**/api/system/update_rollback", async (route) => {
    asked++;
    await route.fulfill({
      json: {
        current: "0.0.2",
        core_found: true,
        rescue: true,
        state: {
          kind: "rescue",
          phase: "build",
          from: "0.0.1",
          to: "0.0.2",
          ok: false,
          started_at: now(),
        },
      },
    });
  });
  await page.goto("/");
  const b = banner(page);
  await expect(b).toHaveAttribute("data-outcome", "stranded");
  await expect(b.getByTestId("update-headline")).toHaveText(
    "更新到 0.0.2 沒成功，也沒能換回 0.0.1，要處理。",
  );
  await expect(b.getByRole("button", { name: "知道了" })).toHaveCount(0);
  await b.getByRole("button", { name: "看細節" }).click();
  await expect(b.getByTestId("upgrade-details")).toBeVisible();

  await b.getByRole("button", { name: "退回上一版" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("退回 0.0.1？");
  await dialog.getByRole("button", { name: "退回" }).click();
  await expect(b.getByTestId("update-progress")).toContainText("正在換回 0.0.1");
  expect(asked).toBe(1);
  // 下一次抓現況（服務還沒寫出這一次的進度）：照樣講「正在換回」，不變成「正在更新到」。
  await page.waitForResponse("**/api/system/update_status");
  await expect(b.getByTestId("update-progress")).toContainText("正在換回 0.0.1");
});
