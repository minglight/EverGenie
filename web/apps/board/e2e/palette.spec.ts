import { expect, test } from "./fixtures";

// Guards: REQ-107
test("TestCommandPaletteNavigates @smoke", async ({ page, isMobile }) => {
  await page.goto("/");
  const opener = isMobile
    ? page.getByRole("button", { name: "搜尋" })
    : page.getByRole("button", { name: /搜尋或下指令/ });
  await expect(opener).toBeVisible();
  await opener.click();
  const dialog = page.getByRole("dialog", { name: "搜尋或下指令" });
  await expect(dialog).toBeVisible();
  const input = dialog.getByRole("combobox");
  await expect(input).toBeFocused();
  // 「前往」列出選單攤平後的每一項；打字就過濾。
  await expect(dialog.getByRole("option", { name: /設定/ })).toBeVisible();
  await input.fill("待辦");
  await expect(dialog.getByRole("option")).toHaveCount(1);
  await input.press("Enter");
  await expect(dialog).toHaveCount(0);
  await expect.poll(() => new URL(page.url()).pathname).toBe("/reminders");
});

// ↓ 選到哪一項、Enter 就跑哪一項；「等你處理」晚一步載進來排到最前面時，選取跟著那一項走、不會換成別的
// （DEV-128：整輪跑時前面的 spec 在同一個家留下等你處理，↓ 本來選聊天、清單更新後 Enter 開成首頁）。
test("TestCommandPaletteKeyboardAndFocus", async ({ page, isMobile, seed }) => {
  await page.goto("/");
  if (isMobile) {
    const search = page.getByRole("button", { name: "搜尋" });
    await search.click();
    await expect(page.getByRole("dialog", { name: "搜尋或下指令" })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog", { name: "搜尋或下指令" })).toHaveCount(0);
    await expect(search).toBeFocused();
    return;
  }
  const opener = page.getByRole("button", { name: /搜尋或下指令/ });
  await expect(opener).toBeVisible();
  await opener.focus();
  await page.keyboard.press("Control+k");
  const dialog = page.getByRole("dialog", { name: "搜尋或下指令" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("combobox")).toBeFocused();
  // ↑ 從第一項繞到最後一項（「前往」的最後一頁，一定排在所有等你處理後面），再 ↑↓ 一次回到它。
  await page.keyboard.press("ArrowUp");
  await page.keyboard.press("ArrowUp");
  await page.keyboard.press("ArrowDown");
  const active = dialog.getByRole("option", { selected: true });
  await expect(active).toHaveCount(1);
  const picked = (await active.textContent()) ?? "";
  const before = Number(await active.getAttribute("data-index"));
  // 面板開著時多一件等你處理：排在選取那項前面，把它往後推；選取仍是剛剛那一項。
  const title = `面板選取不跑掉 ${Date.now()}`;
  await seed.approve(title);
  await expect(dialog.getByRole("option", { name: new RegExp(title) })).toBeVisible();
  await expect(active).toHaveText(picked);
  await expect(active).toHaveAttribute("data-index", String(before + 1));
  await page.keyboard.press("Enter");
  await expect(dialog).toHaveCount(0);
  await expect.poll(() => new URL(page.url()).pathname).not.toBe("/");
  const viaKeyboard = new URL(page.url()).pathname;
  // 用滑鼠點同一項，到的地方要一樣。
  await page.goto("/");
  await opener.click();
  await dialog.getByRole("option").filter({ hasText: picked }).first().click();
  await expect.poll(() => new URL(page.url()).pathname).toBe(viaKeyboard);
  // 再開再關：Esc 關閉，焦點回到原本的按鈕。
  await page.goto("/");
  await opener.focus();
  await page.keyboard.press("Control+k");
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(opener).toBeFocused();
});

// Guards: REQ-109
test("TestCommandPaletteFindsWaitingItems", async ({ page, seed }) => {
  const title = `指令面板找得到 ${Date.now()}`;
  const id = await seed.approve(title);
  await page.goto("/");
  await expect(page.getByRole("navigation", { name: "主選單" })).toBeVisible();
  await page.keyboard.press("Control+k");
  const dialog = page.getByRole("dialog", { name: "搜尋或下指令" });
  await expect(dialog).toBeVisible();
  // 用編號過濾「等你處理」。
  await dialog.getByRole("combobox").fill(id);
  const hit = dialog.getByRole("option", { name: new RegExp(title) });
  await expect(hit).toBeVisible();
  await hit.click();
  await expect.poll(() => new URL(page.url()).pathname).toBe(`/messages/${id}`);
});

// 打字找得到還沒結束的任務單（標題）與秘書（帶去跟她聊天），2026-11-02 稽核。
test("TestCommandPaletteFindsTasksAndSecretaries", async ({ page, seed }) => {
  const title = `面板找單 ${Date.now()}`;
  const id = await seed.doingTask(title);
  await page.goto("/");
  await expect(page.getByRole("navigation", { name: "主選單" })).toBeVisible();
  await page.keyboard.press("Control+k");
  const dialog = page.getByRole("dialog", { name: "搜尋或下指令" });
  const box = dialog.getByRole("combobox");
  await box.fill(title);
  const hit = dialog.getByRole("option", { name: new RegExp(title) });
  await expect(hit).toBeVisible();
  await hit.click();
  await expect.poll(() => new URL(page.url()).pathname).toBe(`/tasks/${id}`);

  await page.keyboard.press("Control+k");
  await box.fill("Builder");
  const chat = dialog.getByRole("option", { name: /跟 Builder 聊天/ });
  await expect(chat).toBeVisible();
  await chat.click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/chat/code");
});

// 打設定分頁的名字（「通知」「權限」）找得到「設定 · 那一頁」，按了直接到那個分頁（WEB-101）；沒打字時不列，免得洗版。
test("TestCommandPaletteFindsSettingsTabs", async ({ page, isMobile }) => {
  await page.goto("/");
  const opener = isMobile
    ? page.getByRole("button", { name: "搜尋" })
    : page.getByRole("button", { name: /搜尋或下指令/ });
  await opener.click();
  const dialog = page.getByRole("dialog", { name: "搜尋或下指令" });
  await expect(dialog.getByRole("combobox")).toBeFocused();
  await expect(dialog.getByRole("option", { name: /設定 · 通知/ })).toHaveCount(0);
  const input = dialog.getByRole("combobox");
  await input.fill("權限");
  await expect(dialog.getByRole("option", { name: /設定 · 權限/ })).toBeVisible();
  await input.fill("通知");
  const notify = dialog.getByRole("option", { name: /設定 · 通知/ });
  await expect(notify).toBeVisible();
  await notify.click();
  await expect(dialog).toHaveCount(0);
  await expect.poll(() => new URL(page.url()).pathname).toBe("/settings/notify");
  // 「健康」合了飲食、運動、身體三頁：打分頁名也找得到，按了到那一頁。
  await opener.click();
  await dialog.getByRole("combobox").fill("運動");
  const part = dialog.getByRole("option", { name: /健康 · 運動/ });
  await expect(part).toBeVisible();
  await part.click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/p/fitness/log");
});

// WEB-128：iOS 打開鍵盤會把畫面往上推（visualViewport.offsetTop > 0、只剩鍵盤上面那塊看得到）。
// 瀏覽器沒有真的鍵盤，用假的 visualViewport 模擬：面板頂端要跟著往下，不跑到看不到的地方；清單底也不落到鍵盤後面。
test("TestCommandPaletteFollowsPushedViewport", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(() => {
    const vv = Object.assign(new EventTarget(), {
      width: 390,
      height: 470,
      offsetTop: 0,
      scale: 1,
    });
    Object.defineProperty(window, "visualViewport", { value: vv, configurable: true });
    (window as unknown as { __vv: typeof vv }).__vv = vv;
  });
  await page.goto("/");
  await page.getByRole("button", { name: "搜尋" }).click();
  const dialog = page.getByRole("dialog", { name: "搜尋或下指令" });
  await expect(dialog).toBeVisible();
  await page.evaluate(() => {
    const vv = (window as unknown as { __vv: EventTarget & { offsetTop: number } }).__vv;
    vv.offsetTop = 300;
    vv.dispatchEvent(new Event("scroll"));
  });
  await expect.poll(async () => (await dialog.boundingBox())?.y ?? 0).toBeGreaterThanOrEqual(311);
  const box = await dialog.boundingBox();
  expect((box?.y ?? 0) + (box?.height ?? 0)).toBeLessThanOrEqual(300 + 470);
});
