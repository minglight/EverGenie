import os from "node:os";
import { expect, test } from "./fixtures";

// 設定 → 系統健康的「完整檢查」與「回報問題」（DEV-153、WEB-191、RPT-1～5）。
const owner = { name: "owner(test)" };

// Guards: WEB-191
test("TestHealthFullCheckProblemsFirst", async ({ page, seed }) => {
  const mode = (await seed.api<{ mode: string }>("/api/system/get_mode", {})).mode;
  try {
    await seed.api("/api/system/set_mode", { mode: "simple", by: owner });
    await page.goto("/settings/health");
    // 按了才跑。
    await expect(page.getByTestId("doctor-summary")).toHaveCount(0);
    const run = page.getByTestId("doctor-run");
    expect((await run.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(44);
    await run.click();
    await expect(page.getByTestId("doctor-summary")).toBeVisible({ timeout: 30_000 });
    // e2e 的家沒有推播金鑰：一定有要看的；列出來的都是有問題的，要處理的排在注意前面。
    const rows = page.getByTestId("doctor-check");
    await expect(rows.first()).toBeVisible();
    const severities = await rows.evaluateAll((els) =>
      els.map((el) => (el as HTMLElement).dataset.severity ?? ""),
    );
    expect(severities.length).toBeGreaterThan(0);
    expect(severities).not.toContain("ok");
    expect(severities).toEqual([...severities].sort((a, b) => rank(a) - rank(b)));
    // 簡單模式不露指令，改說跟秘書說。
    const result = page.getByTestId("doctor-result");
    await expect(result).not.toContainText("evergenie ");
    await expect(result).toContainText("跟任何一位秘書說");
    // 正常的收著，按了才展開。
    await page.getByTestId("doctor-ok-toggle").click();
    const all = await page
      .getByTestId("doctor-check")
      .evaluateAll((els) => els.map((el) => (el as HTMLElement).dataset.severity ?? ""));
    expect(all).toContain("ok");
  } finally {
    await seed.api("/api/system/set_mode", { mode, by: owner });
  }
});

function rank(severity: string): number {
  return severity === "error" ? 0 : severity === "warn" ? 1 : 2;
}

// Guards: RPT-2, RPT-4, RPT-5
test("TestReportProblemShowsContentThenCopy", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const calls: string[] = [];
  page.on("request", (req) => {
    if (req.url().includes("/api/system/report")) calls.push(req.url());
  });
  await page.goto("/settings/health");
  // 進頁面不會自己產生。
  await expect(page.getByTestId("report-make")).toBeVisible();
  await expect(page.getByTestId("report-preview")).toHaveCount(0);
  expect(calls).toHaveLength(0);

  await page.getByTestId("report-make").click();
  const preview = page.getByTestId("report-preview");
  await expect(preview).toBeVisible({ timeout: 30_000 });
  await expect(preview).toContainText("問題回報");
  await expect(preview).toContainText("健康檢查");
  // 遮乾淨：這台的登入名、家目錄路徑、電腦名不出現。
  const text = (await preview.textContent()) ?? "";
  expect(text).not.toContain(os.homedir());
  expect(text).not.toContain("/Users/");
  expect(text).not.toContain(os.hostname().split(".")[0] ?? "\u0000");
  // 回報頁網址還沒設定：沒有開網頁的按鈕，只有「複製內容」；複製的就是畫面上那一份。
  await expect(page.getByTestId("report-open")).toHaveCount(0);
  const copy = page.getByTestId("report-copy");
  expect((await copy.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(44);
  await copy.click();
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toContain("問題回報");
  // 除了載入主人自己按的那一次，沒有別的請求把內容送出去。
  expect(calls).toHaveLength(1);
});
