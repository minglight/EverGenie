import { existsSync, lstatSync, mkdirSync, symlinkSync, writeFileSync } from "node:fs";
import path from "node:path";
import { expect, shot, freshHomeTest as test } from "./fixtures";

// 整個家的備份與還原（DEV-153）：自己一個家（還原會把整個家換掉，不能跟別的 spec 共用）。
// 截圖只在給了 EVERGENIE_SHOTS_DIR 時才存（手機寬度 390）。
const shots = !!process.env.EVERGENIE_SHOTS_DIR;

// 立刻備份做得出一份可以下載的備份檔；從它還原會先給預覽、確認後把家換回備份當時的樣子。
test("立刻備份、預覽、還原", async ({ page, hgServer }) => {
  test.setTimeout(180_000);
  if (shots) await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/settings/machine");
  const section = page.getByTestId("home-backup");
  await expect(section).toBeVisible();
  await expect(page.getByTestId("backup-last")).toContainText("還沒有備份");
  // 備份只放在這台電腦（法律第 7 條）：沒有「第二個位置」那一列，改成一句白話提醒主人自己下載另存。
  await expect(page.getByTestId("backup-only-here")).toContainText("備份只放在這台電腦");
  await expect(page.getByTestId("backup-copy-row")).toHaveCount(0);
  if (shots) {
    await section.scrollIntoViewIfNeeded();
    await shot(page, "backup-section-390");
  }

  // 立刻備份：做完說多大、裡面有什麼，並給下載連結（真的抓得到檔）。
  await page.getByTestId("backup-now").click();
  await expect(page.getByTestId("backup-created")).toBeVisible({ timeout: 60_000 });
  await expect(page.getByTestId("backup-created")).toContainText("備份好了");
  await expect(page.getByTestId("backup-sections").locator("li").first()).toBeVisible();
  const href = await page.getByTestId("backup-download").getAttribute("href");
  expect(href).toMatch(/^\/home-backup\/manual\/.+\.egbackup$/);
  const file = await page.request.get(href as string);
  expect(file.status()).toBe(200);
  expect((await file.body()).length).toBeGreaterThan(100);
  if (shots) await shot(page, "backup-created-390");
  await page.getByTestId("backup-close").click();
  await expect(page.getByTestId("backup-last")).toContainText("手動");
  await expect(page.getByTestId("backup-total")).toContainText("1 份");

  // 備份之後才多出來的東西：還原後應該不見。
  const later = path.join(hgServer.home, "vault", "notes", "備份之後才寫的.md");
  mkdirSync(path.dirname(later), { recursive: true });
  writeFileSync(later, "還原後這個檔不該在\n");
  // 家裡現有的捷徑：備份不收，還原後會不見——換之前的畫面要先講（HBK-11）。
  const link = path.join(hgServer.home, "vault", "notes", "我的捷徑");
  symlinkSync(path.join(hgServer.home, "home.toml"), link);

  // 從備份還原：挑這台現有的那一份 → 預覽（時間、內容、提醒）→ 確認。
  await page.getByTestId("backup-restore-open").click();
  await page.getByTestId("backup-restore-pick").first().click();
  const preview = page.getByTestId("backup-restore-preview");
  await expect(preview).toBeVisible();
  await expect(page.getByTestId("backup-preview-when")).toContainText("年");
  await expect(preview.getByTestId("backup-sections").locator("li").first()).toBeVisible();
  await expect(page.getByTestId("backup-preview-warning")).toContainText("換之前會自動留一份");
  await expect(page.getByTestId("backup-preview-other")).toHaveCount(0);
  const lost = page.getByTestId("backup-preview-lost");
  await expect(lost).toContainText("還原後會不見，要自己重建");
  await expect(lost).toContainText("vault/notes/我的捷徑");
  if (shots) await shot(page, "backup-preview-390");
  await page.getByTestId("backup-restore-confirm").click();

  // 服務自己重開、換好之後畫面說結果。
  const done = page.getByTestId("backup-restore-done");
  await expect(done).toBeVisible({ timeout: 120_000 });
  await expect(done).toContainText("還原好了");
  await expect(page.getByTestId("backup-restore-notdone")).toHaveCount(0);
  expect(existsSync(later)).toBe(false);
  expect(lstatSync(link, { throwIfNoEntry: false })).toBeUndefined();
  if (shots) await shot(page, "backup-restored-390");

  // 重新整理後：這一頁記得剛還原過，清單裡多了一份「還原前自動留的」。
  await page.getByTestId("backup-restore-reload").click();
  await expect(page.getByTestId("backup-last-restore")).toContainText("還原過");
  await page.getByTestId("backup-restore-open").click();
  await expect(
    page.locator('[data-testid="backup-restore-pick"][data-kind="before-restore"]'),
  ).toHaveCount(1);
});

// 不是備份檔的東西傳上來：用白話說不能用，不會進到確認那一步。
test("傳錯檔案時說明不能用", async ({ page }) => {
  await page.goto("/settings/machine");
  await page.getByTestId("backup-restore-open").click();
  await page.getByTestId("backup-restore-file").setInputFiles({
    name: "不是備份.egbackup",
    mimeType: "application/octet-stream",
    buffer: Buffer.from("這不是備份檔"),
  });
  await expect(page.getByTestId("backup-restore-error")).toContainText("不是備份檔");
  await expect(page.getByTestId("backup-restore-confirm")).toHaveCount(0);
});

// 不是這台 Mac 本機開的看板（手機、別台：經 Tailscale 進來的請求一定帶 X-Forwarded-For）：不能上傳備份檔來還原
// （畫面只說要在那台 Mac 上做；直接打 API 也是 403）。從公網（Funnel）轉進來的連現有的備份也不能還原。
// Guards: WEB-190
test("TestUploadRestoreOnlyOnThisMac", async ({ browser, hgServer: server, request }) => {
  const away = { "X-Forwarded-For": "192.0.2.10" };
  const context = await browser.newContext({ baseURL: server.base, extraHTTPHeaders: away });
  const page = await context.newPage();
  await page.goto("/settings/machine");
  await page.getByTestId("backup-restore-open").click();
  await expect(page.getByTestId("backup-restore-thismac")).toContainText(
    "要在跑看板的那台 Mac 上做",
  );
  await expect(page.getByTestId("backup-restore-choose")).toHaveCount(0);
  await context.close();

  const upload = await request.post(`${server.base}/api/web/home-backup-upload`, {
    data: Buffer.from("x"),
    headers: { ...away, "X-Requested-With": "evergenie" },
  });
  expect(upload.status()).toBe(403);
  for (const method of ["preview", "restore"]) {
    const res = await request.post(`${server.base}/api/backup/${method}`, {
      data: { upload: "20261116-103000-0a1b2c3d.egbackup" },
      headers: away,
    });
    expect(res.status(), method).toBe(403);
    const viaFunnel = await request.post(`${server.base}/api/backup/${method}`, {
      data: { kind: "manual", name: "x-20261116-103000.egbackup" },
      headers: { "Tailscale-Funnel-Request": "?1" },
    });
    expect(viaFunnel.status(), `${method} via funnel`).toBe(403);
  }
  // 這台 Mac 本機開的看板：有上傳的按鈕。
  const here = await browser.newContext({ baseURL: server.base });
  const local = await here.newPage();
  await local.goto("/settings/machine");
  await local.getByTestId("backup-restore-open").click();
  await expect(local.getByTestId("backup-restore-choose")).toBeVisible();
  await here.close();
});
