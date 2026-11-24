import type { Page } from "@playwright/test";
import { expect, test } from "./fixtures";

// 輸入框的「/」指令（WEB-83、SES-59）：打 / 跳出選單、/clear 先確認、送 session.command（不進訊息佇列、沒有泡泡），
// 引擎做成了核心記 session.last_command。

const chatLog = (page: Page) => page.getByRole("log", { name: "對話" });

async function idle(seed: { api: <T>(route: string, body: unknown) => Promise<T> }) {
  // 前面的測試可能讓她還在忙：等她回到待命再下清掉的指令。
  await expect
    .poll(
      async () =>
        (await seed.api<{ session?: { state?: string } }>("/api/session/get", { name: "code" }))
          .session?.state,
      { timeout: 15_000 },
    )
    .toBe("idle");
}

test("TestSlashMenuClearConfirmSendsCommand", async ({ page, seed }) => {
  await page.goto("/chat/code");
  await expect(chatLog(page)).toBeVisible();
  await idle(seed);
  const box = page.locator("#hg-composer");
  await box.fill("/");
  const menu = page.getByRole("listbox", { name: "對話指令" });
  await expect(menu).toBeVisible();
  for (const cmd of ["/clear", "/compact", "/model", "/effort"]) {
    await expect(menu.getByRole("option", { name: new RegExp(`^${cmd}`) })).toBeVisible();
  }
  const sent = page.waitForResponse("**/api/session/command");
  await menu.getByRole("option", { name: /^\/clear/ }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("長期記憶和日誌不受影響");
  await dialog.getByRole("button", { name: "清掉" }).click();
  const res = await sent;
  expect(res.ok()).toBe(true);
  expect(res.request().postDataJSON()).toMatchObject({ session: "code", command: "clear" });
  await expect(page.getByText("已送出 /clear")).toBeVisible();
  await expect(chatLog(page).getByText("你下了指令 /clear")).toBeVisible();
  await expect(box).toHaveValue("");
  // 不是訊息：對話裡沒有「/clear」的泡泡、秘書也沒回「收到」。
  await expect(chatLog(page).getByText("/clear", { exact: true })).toHaveCount(0);
  await expect(chatLog(page).getByText(/^收到：\/clear/)).toHaveCount(0);
  // 引擎做成了：核心記下這次的指令。
  const got = await seed.api<{ session?: { last_command?: { command?: string; by?: string } } }>(
    "/api/session/get",
    { name: "code" },
  );
  expect(got.session?.last_command).toMatchObject({ command: "clear", by: "owner(web)" });
});

test("TestSlashModelSecondLevelAndUnknownHint", async ({ page, seed }) => {
  await page.goto("/chat/code");
  await expect(chatLog(page)).toBeVisible();
  const box = page.locator("#hg-composer");
  await box.fill("/mo");
  await box.press("Enter");
  await expect(box).toHaveValue("/model ");
  const menu = page.getByRole("listbox", { name: "對話指令" });
  await expect(menu.getByRole("option")).toHaveCount(4);
  const sent = page.waitForResponse("**/api/session/command");
  await menu.getByRole("option", { name: /^opus/ }).click();
  expect((await sent).request().postDataJSON()).toMatchObject({ command: "model", arg: "opus" });
  await expect(page.getByText("已送出 /model opus")).toBeVisible();
  const got = await seed.api<{ session?: { last_command?: { command?: string; arg?: string } } }>(
    "/api/session/get",
    { name: "code" },
  );
  expect(got.session?.last_command).toMatchObject({ command: "model", arg: "opus" });

  // 不認得的指令：提示看板支援哪些，不送。
  await box.fill("/help");
  await page.locator('button[aria-label="送出訊息"]').click();
  await expect(page.getByTestId("slash-hint")).toContainText(
    "看板支援 /clear、/compact、/model、/effort",
  );
  await expect(box).toHaveValue("/help");
  await box.fill("");
});

test("TestSlashClearRefusedWhileBusy", async ({ page, seed }) => {
  await page.goto("/chat/code");
  await expect(chatLog(page)).toBeVisible();
  await idle(seed);
  const box = page.locator("#hg-composer");
  // 假引擎看到「【慢】」會做 3 秒：趁她在忙下 /clear。
  await box.fill(`【慢】${Math.random().toString(36).slice(2, 8)}`);
  await page.locator('button[aria-label="送出訊息"]').click();
  await expect
    .poll(
      async () =>
        (await seed.api<{ session?: { state?: string } }>("/api/session/get", { name: "code" }))
          .session?.state,
    )
    .toBe("busy");
  await box.fill("/clear");
  await box.press("Enter");
  await page.getByRole("dialog").getByRole("button", { name: "清掉" }).click();
  await expect(page.getByRole("alert")).toContainText("還在忙");
  await expect(box).toHaveValue("/clear");
  await box.fill("");
  await idle(seed);
});

// 標題欄的「現在用的模型 · 思考力度」（WEB-84、SES-60）：/model、/effort 做成了不用重新整理，3 秒內換成新的；
// 點那顆小鈕打開「/model」選單。假引擎給的名字是「Fake <別名>」。
test("TestThreadHeaderModelChipUpdatesAndOpensModelMenu", async ({ page, seed }) => {
  await page.goto("/chat/code");
  await expect(chatLog(page)).toBeVisible();
  await idle(seed);
  const chip = page.getByTestId("thread-model");
  await expect(chip).toContainText(/Fake /);
  await chip.click();
  const box = page.locator("#hg-composer");
  await expect(box).toHaveValue("/model ");
  const menu = page.getByRole("listbox", { name: "對話指令" });
  await menu.getByRole("option", { name: /^haiku/ }).click();
  await expect(chip).toContainText("Fake Haiku", { timeout: 3_000 });

  await box.fill("/effort high");
  await box.press("Enter");
  await page.getByRole("dialog").getByRole("button", { name: "改", exact: true }).click();
  await expect(chip).toContainText("Fake Haiku · 思考 high", { timeout: 3_000 });
  const got = await seed.api<{
    session?: { model?: string; model_label?: string; effort?: string };
  }>("/api/session/get", { name: "code" });
  expect(got.session).toMatchObject({ model: "haiku", model_label: "Fake Haiku", effort: "high" });
});

// 她在做事時輸入框上面有「停下她」（SES-62）：按了送 session.command interrupt，她這一輪被中斷、回到待命。
test("TestStopBarInterruptsBusySecretary", async ({ page, seed }) => {
  await page.goto("/chat/code");
  await expect(chatLog(page)).toBeVisible();
  await idle(seed);
  await page.locator("#hg-composer").fill(`【慢】做久一點 ${Date.now()}`);
  await page.locator('button[aria-label="送出訊息"]').click();
  const bar = page.getByTestId("stop-bar");
  await expect(bar).toBeVisible();
  const sent = page.waitForResponse("**/api/session/command");
  await bar.getByRole("button", { name: "停下", exact: true }).click();
  const res = await sent;
  expect(res.ok()).toBe(true);
  expect(res.request().postDataJSON()).toMatchObject({ session: "code", command: "interrupt" });
  await expect(bar).toHaveCount(0);
  await idle(seed);
  await expect(chatLog(page).getByText("（被中斷）").last()).toBeVisible();
});

// WEB-114：手機上「已叫 X 停下」放在上面，不蓋住停下列與「/」選單（390×520 要看得到 /model）；
// 沒有按鈕的 toast 不接滑鼠（桌機滑鼠停在上面不會一直不消失）與觸控（點得到底下的東西）。
test("TestStopToastStaysOffComposerOnPhone", async ({ page, seed }) => {
  await page.setViewportSize({ width: 390, height: 520 });
  await page.goto("/chat/code");
  await expect(chatLog(page)).toBeVisible();
  await idle(seed);
  await page.locator("#hg-composer").fill(`【慢】做久一點 ${Date.now()}`);
  await page.locator('button[aria-label="送出訊息"]').click();
  const bar = page.getByTestId("stop-bar");
  await bar.getByRole("button", { name: "停下", exact: true }).click();
  const toast = page.locator("[data-sonner-toast]").filter({ hasText: "已叫" });
  await expect(toast).toBeVisible();
  await page.locator("#hg-composer").fill("/");
  const model = page.getByRole("option").filter({ hasText: "/model" });
  await expect(model).toBeVisible();
  const t = await toast.boundingBox();
  const m = await model.boundingBox();
  expect(t && m && t.y + t.height <= m.y).toBe(true);
  expect(await toast.evaluate((el) => getComputedStyle(el).pointerEvents)).toBe("none");
  await idle(seed);
});

// WEB-90：她在忙時補一句、再按「停下她」，平台接著把補的那句做完了：那句只畫一次（對話記錄裡），不留「這句沒送到」紅字與重送鈕。
test("TestStopAfterInterjectionDoesNotShowNotDelivered", async ({ page, seed }) => {
  await page.goto("/chat/code");
  await expect(chatLog(page)).toBeVisible();
  await idle(seed);
  const box = page.locator("#hg-composer");
  const send = page.locator('button[aria-label="送出訊息"]');
  const extra = `補一句 ${Date.now()}`;
  await box.fill(`【慢】做久一點 ${Date.now()}`);
  await send.click();
  const bar = page.getByTestId("stop-bar");
  await expect(bar).toBeVisible();
  await box.fill(extra);
  await send.click();
  await expect(page.getByText("正在忙，這句已經送達了")).toBeVisible();
  await bar.getByRole("button", { name: "停下", exact: true }).click();
  await expect(chatLog(page).getByText(`收到：${extra}`)).toBeVisible();
  // 伺服器還記著那句「這句還沒開始，可以重送」：等它的狀態拿回來，畫面仍然只有對話記錄裡那一則。
  await expect
    .poll(async () => {
      const got = await seed.api<{
        session?: { messages?: { body?: string; lifecycle?: string }[] };
      }>("/api/session/get", { name: "code" });
      return got.session?.messages?.find((m) => m.body === extra)?.lifecycle ?? "";
    })
    .toBe("failed");
  await page.waitForTimeout(500);
  await expect(chatLog(page).getByText(extra, { exact: true })).toHaveCount(1);
  await expect(page.getByText(/這句沒送到/)).toHaveCount(0);
  await expect(page.getByRole("button", { name: "重送" })).toHaveCount(0);
});
