import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { expect, test } from "./fixtures";

// 設定 → 職位與秘書 → 同時進行的對話（WEB-186、SES-73）：settings/sessions.toml 的 work_max。
// 改了別的測試的家也吃得到（同一個 worker 共用），結束一定還原原檔。
const owner = { name: "owner(test)" };

// Guards: SES-73
test("TestWorkMaxSettingSaves", async ({ page, seed, hgServer }) => {
  const file = path.join(hgServer.home, "settings", "sessions.toml");
  const original = existsSync(file) ? readFileSync(file, "utf8") : null;
  const mode = (await seed.api<{ mode: string }>("/api/system/get_mode", {})).mode;
  try {
    // 先放一份有註解、有別的欄位的檔：改完只動 work_max 那一行。
    mkdirSync(path.dirname(file), { recursive: true });
    writeFileSync(file, "# 主人的註解\nschema = 1\nwork_max = 3 # 預設\n", { mode: 0o600 });
    // 簡單模式也看得到（一般使用者會遇到的事）。
    await seed.api("/api/system/set_mode", { mode: "simple", by: owner });
    await page.goto("/settings/agents");
    const group = page
      .locator("section")
      .filter({ has: page.getByRole("heading", { name: "同時進行的對話" }) });
    await expect(group).toBeVisible();
    await expect(group).toContainText("每張「進行中」的任務單會各開一個對話同時做");
    const value = page.getByTestId("work-max-value");
    await expect(value).toContainText("3 個");
    await expect(value).toContainText("預設");

    const more = group.getByRole("button", { name: "多一個" });
    const box = await more.boundingBox();
    expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
    await more.click();
    await more.click();
    await expect(value).toHaveText("5 個");
    await expect
      .poll(() => readFileSync(file, "utf8"))
      .toBe("# 主人的註解\nschema = 1\nwork_max = 5 # 預設\n");

    // 重新整理還在。
    await page.reload();
    await expect(page.getByTestId("work-max-value")).toHaveText("5 個");
  } finally {
    await seed.api("/api/system/set_mode", { mode, by: owner });
    if (original === null) rmSync(file, { force: true });
    else writeFileSync(file, original);
  }
});
