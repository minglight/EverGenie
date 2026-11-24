import { existsSync } from "node:fs";
import path from "node:path";
import type { APIRequestContext, Locator, Page } from "@playwright/test";
import { expect, test } from "./fixtures";

const rand = () => Math.random().toString(36).slice(2, 8);
const PNG_1X1_B64 =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

// 模擬 ⌘V 貼上一張截圖：剪貼簿只有一個沒名字的 image.png（跟瀏覽器貼截圖一樣）。
async function pasteImage(target: Locator) {
  await target.evaluate((el, b64) => {
    const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
    const dt = new DataTransfer();
    dt.items.add(new File([bytes], "image.png", { type: "image/png" }));
    el.dispatchEvent(
      new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }),
    );
  }, PNG_1X1_B64);
}

interface ReminderRecord {
  id: string;
  content: string;
  status: string;
  body?: string;
  attachments?: string[];
  at?: string;
  follow?: string;
  owner?: string;
  difficulty?: string;
}

async function listAll(seed: import("./fixtures").Seed): Promise<ReminderRecord[]> {
  const res = await seed.api<{ reminders?: ReminderRecord[] }>("/api/reminders/list", {
    include_archived: false,
  });
  return res.reminders ?? [];
}

async function reminderOf(seed: import("./fixtures").Seed, id: string): Promise<ReminderRecord> {
  const res = await seed.api<{ reminder: ReminderRecord }>("/api/reminders/get", { id });
  return res.reminder;
}

// 清單是 <ul>；toast（sonner）是 <ol>，所以只看 ul 底下的列。
const rowOf = (page: Page | Locator, content: string) =>
  page.locator("ul > li").filter({ hasText: content });

async function rosterNames(request: APIRequestContext, base: string) {
  const res = await request.post(`${base}/api/web/get_roster`, { data: {} });
  const body = (await res.json()) as { agents?: { id: string; name: string; enabled: boolean }[] };
  return (body.agents ?? []).filter((a) => a.enabled);
}

// Guards: REQ-59
test("TestReminderQuickAdd @smoke", async ({ page, seed }) => {
  const what = `打給牙醫 ${rand()}`;
  // 「未完成」一頁依時間分組，明天不管落在「這週」還是「之後」都找得到。
  await page.goto("/reminders");
  const input = page.getByLabel("新增待辦");
  await input.fill(`明天 9 點 ${what}`);

  // 預覽列：意圖、時間膠囊（解析出來的明天 09:00）、內容膠囊（不含「明天 9 點」）、負責秘書。
  const preview = page.getByTestId("quick-preview");
  await expect(preview).toContainText("新增待辦");
  await expect(preview).toContainText("09:00");
  await expect(preview).toContainText(what);
  await expect(preview).not.toContainText("明天 9 點");
  await expect(preview).toContainText("負責：");
  await input.press("Enter");
  await expect(page.getByText(/已新增：/)).toBeVisible();
  await expect(input).toHaveValue("");

  const tomorrow = new Date();
  tomorrow.setDate(tomorrow.getDate() + 1);
  tomorrow.setHours(9, 0, 0, 0);
  await expect
    .poll(async () => (await listAll(seed)).find((r) => r.content === what)?.content)
    .toBe(what);
  const made = (await listAll(seed)).find((r) => r.content === what);
  expect(new Date(made?.at ?? "").getTime()).toBe(tomorrow.getTime());
  expect(made?.owner).toBe("life");
  expect(made?.follow).toBe("remind");
  await expect(rowOf(page, what)).toBeVisible();
});

// DEV-107：「未完成」依時間分組：今天、之後各在自己那組。
// Guards: REQ-59
test("TestReminderOpenGroupedByTime", async ({ page, seed }) => {
  const later = `下週的事 ${rand()}`;
  const today = `今天的事 ${rand()}`;
  const nextWeek = new Date();
  nextWeek.setDate(nextWeek.getDate() + 8);
  nextWeek.setHours(10, 0, 0, 0);
  const tonight = new Date();
  tonight.setHours(23, 59, 0, 0);
  await seed.reminder(later, nextWeek);
  await seed.reminder(today, tonight);
  await page.goto("/reminders");
  const tabs = page.getByRole("group", { name: "待辦篩選" });
  await expect(tabs.getByRole("button", { name: /^未完成/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await expect(rowOf(page.getByTestId("reminders-group-later"), later)).toBeVisible();
  await expect(rowOf(page.getByTestId("reminders-group-today"), today)).toBeVisible();
  await expect(rowOf(page.getByTestId("reminders-group-later"), today)).toHaveCount(0);
});

// Guards: REQ-59
test("TestReminderQuickAddWithoutTime", async ({ page, seed }) => {
  const what = `買牛奶 ${rand()}`;
  await page.goto("/reminders?view=no_time");
  const input = page.getByLabel("新增待辦");
  await input.fill(what);
  await expect(page.getByTestId("quick-preview")).toContainText("未定時間");
  await input.press("Enter");
  await expect(page.getByText(/已新增：/)).toBeVisible();
  // 沒時間：不送 at（送空字串會壞），跟進方式是「不用跟進」。
  await expect
    .poll(async () => (await listAll(seed)).find((r) => r.content === what)?.follow)
    .toBe("none");
  await expect(rowOf(page, what)).toBeVisible();
});

// Guards: REQ-59
test("TestReminderUndoCreate", async ({ page, seed }) => {
  const what = `建立後復原 ${rand()}`;
  await page.goto("/reminders?view=no_time");
  const input = page.getByLabel("新增待辦");
  await input.fill(what);
  await input.press("Enter");
  await expect(rowOf(page, what)).toBeVisible();
  await page.getByRole("button", { name: "復原", exact: true }).click();
  await expect
    .poll(async () => (await listAll(seed)).find((r) => r.content === what)?.status)
    .toBe("cancelled");
  await expect(rowOf(page, what)).toHaveCount(0);
});

// Guards: REQ-59
test("TestReminderDoneUndo @smoke", async ({ page, seed }) => {
  const content = `做了又復原 ${rand()}`;
  const id = await seed.reminder(content);
  // 「復原」的 5 秒用假時鐘快轉（不是改短等待）：clock 要在開頁之前裝。
  await page.clock.install();
  await page.goto("/reminders?view=no_time");
  await expect(rowOf(page, content)).toBeVisible();

  // 按「做了」：畫面先移除，toast 帶「復原」。
  await rowOf(page, content).getByRole("button", { name: "標成做完" }).click();
  await expect(rowOf(page, content)).toHaveCount(0);
  await expect(page.getByText(`做完了：${content}`)).toBeVisible();
  // 按復原：回來，而且經 API 讀到的狀態沒變。
  await page.getByRole("button", { name: "復原", exact: true }).click();
  await expect(rowOf(page, content)).toBeVisible();
  expect((await reminderOf(seed, id)).status).toBe("todo");
  await page.clock.runFor(5_500);
  expect((await reminderOf(seed, id)).status).toBe("todo");

  // 再按一次「做了」不復原：5 秒後才真的送。
  await rowOf(page, content).getByRole("button", { name: "標成做完" }).click();
  await expect(rowOf(page, content)).toHaveCount(0);
  expect((await reminderOf(seed, id)).status).toBe("todo");
  await page.clock.runFor(6_000);
  await expect.poll(async () => (await reminderOf(seed, id)).status).toBe("done");
});

// Guards: REQ-59
test("TestReminderLeavingFlushesPending", async ({ page, seed }) => {
  const content = `離開就送 ${rand()}`;
  const id = await seed.reminder(content);
  await page.goto("/reminders?view=no_time");
  await rowOf(page, content).getByRole("button", { name: "標成做完" }).click();
  await expect(rowOf(page, content)).toHaveCount(0);
  expect((await reminderOf(seed, id)).status).toBe("todo");
  // 換畫面：等著的動作立刻送出，不用等 5 秒。
  await page.goto("/messages");
  await expect
    .poll(async () => (await reminderOf(seed, id)).status, { timeout: 4000 })
    .toBe("done");
});

// Guards: REQ-59
test("TestReminderCancelAndMenu", async ({ page, seed }) => {
  const content = `取消測試 ${rand()}`;
  const id = await seed.reminder(content);
  await page.clock.install();
  await page.goto("/reminders?view=no_time");
  await rowOf(page, content).getByRole("button", { name: "更多動作" }).click();
  // 手機是底部抽屜、桌機是小浮窗：選單裡有延後、還在做、不做了。
  // 「不做了」不寫「取消」（RMD-19）：底部抽屜最下面的「取消」大家會當成「關掉選單」，按了卻把待辦取消掉。
  const menu = page.getByRole("dialog", { name: /更多動作|待辦|取消測試/ });
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("button", { name: "1 小時後" })).toBeVisible();
  await expect(menu.getByRole("button", { name: "還在做" })).toBeVisible();
  await expect(menu.getByRole("button", { name: "取消", exact: true })).toHaveCount(0);
  await menu.getByRole("button", { name: "不做了", exact: true }).click();
  await expect(rowOf(page, content)).toHaveCount(0);
  await expect(page.getByText(`取消了：${content}`)).toBeVisible();
  expect((await reminderOf(seed, id)).status).toBe("todo");
  await page.clock.runFor(5_000);
  await expect.poll(async () => (await reminderOf(seed, id)).status).toBe("cancelled");
});

// Guards: REQ-59
test("TestReminderSnooze", async ({ page, seed }) => {
  const content = `延後測試 ${rand()}`;
  const id = await seed.reminder(content);
  await page.goto("/reminders?view=no_time");
  await rowOf(page, content).getByRole("button", { name: "更多動作" }).click();
  const before = Date.now();
  await page.getByRole("button", { name: "1 小時後" }).click();
  await expect(page.getByText(`延後到`)).toBeVisible();
  await expect
    .poll(async () => new Date((await reminderOf(seed, id)).at ?? "").getTime() - before)
    .toBeGreaterThan(55 * 60_000);
  const at = new Date((await reminderOf(seed, id)).at ?? "").getTime();
  expect(at - before).toBeLessThan(65 * 60_000);
});

// Guards: REQ-59
test("TestReminderDetailInlineEdit", async ({ page, seed }) => {
  const content = `詳細頁 ${rand()}`;
  const id = await seed.reminder(content);
  await page.goto(`/reminders/${id}`);
  await expect(page.getByText(id, { exact: true }).first()).toBeVisible();
  // 先看再改：點內容變輸入框，Enter 存，旁邊顯示「已儲存」。
  const next = `${content} 改過`;
  await page
    .getByRole("button", { name: new RegExp(content) })
    .first()
    .click();
  const box = page.getByLabel("內容");
  await box.fill(next);
  await box.press("Enter");
  await expect(page.getByText("已儲存")).toBeVisible();
  await expect.poll(async () => (await reminderOf(seed, id)).content).toBe(next);

  // Esc 還原：不存。
  await page
    .getByRole("button", { name: new RegExp(next) })
    .first()
    .click();
  await page.getByLabel("內容").fill("亂打的");
  await page.keyboard.press("Escape");
  expect((await reminderOf(seed, id)).content).toBe(next);

  // 跟進方式：分段按鈕，選了就存。
  await page.getByRole("button", { name: /不用跟進/ }).click();
  await page.getByRole("button", { name: "提醒", exact: true }).click();
  await expect.poll(async () => (await reminderOf(seed, id)).follow).toBe("remind");
});

// Guards: REQ-62
test("TestReminderDetailEditTime", async ({ page, seed }) => {
  const id = await seed.reminder(`改時間 ${rand()}`);
  await page.goto(`/reminders/${id}`);
  // 沒有時間：欄位寫「沒有時間」；點了開時間欄位，打自然語言，下面即時顯示解析結果。
  await page.getByRole("button", { name: "沒有時間" }).click();
  await page.getByLabel("時間", { exact: true }).fill("明天 9 點");
  await expect(page.getByRole("status").filter({ hasText: "09:00" })).toBeVisible();
  await page.getByRole("button", { name: "確定", exact: true }).click();
  await expect(page.getByText("已儲存")).toBeVisible();
  const tomorrow = new Date();
  tomorrow.setDate(tomorrow.getDate() + 1);
  tomorrow.setHours(9, 0, 0, 0);
  await expect
    .poll(async () => new Date((await reminderOf(seed, id)).at ?? "").getTime())
    .toBe(tomorrow.getTime());
});

// Guards: REQ-59
test("TestReminderDetailActions", async ({ page, seed }) => {
  const content = `詳細頁動作 ${rand()}`;
  const id = await seed.reminder(content);
  await page.clock.install();
  await page.goto(`/reminders/${id}`);
  await page.getByRole("button", { name: "還在做", exact: true }).click();
  await expect.poll(async () => (await reminderOf(seed, id)).status).toBe("doing");
  await page.getByRole("button", { name: "做了", exact: true }).click();
  await expect(page.getByText("剛剛的操作稍後才會生效")).toBeVisible();
  expect((await reminderOf(seed, id)).status).toBe("doing");
  await page.clock.runFor(5_000);
  await expect.poll(async () => (await reminderOf(seed, id)).status).toBe("done");
});

// DEV-107：三個分頁（未完成、等回覆、已完成）＋搜尋；舊網址的 no_time 當成「未完成」。
// Guards: REQ-107
test("TestReminderViews", async ({ page, seed }) => {
  const noTime = `沒時間檢視 ${rand()}`;
  const other = `別的待辦 ${rand()}`;
  await seed.reminder(noTime);
  await seed.reminder(other);
  await page.goto("/reminders");
  const tabs = page.getByRole("group", { name: "待辦篩選" });
  for (const name of [/^未完成/, /^要你回答/, /^已完成/]) {
    await expect(tabs.getByRole("button", { name })).toBeVisible();
  }
  // 沒有時間的在「未定時間」那組。
  await expect(rowOf(page.getByTestId("reminders-group-none"), noTime)).toContainText("R-");
  // 搜尋只留符合的。
  const search = page.getByRole("searchbox", { name: "搜尋待辦" });
  await search.fill(noTime);
  await expect(rowOf(page, noTime)).toBeVisible();
  await expect(rowOf(page, other)).toHaveCount(0);
  await search.fill(`沒有這個字 ${rand()}`);
  await expect(page.getByText("沒有符合")).toBeVisible();
  await search.fill("");
  // 分頁寫在網址，重新整理還在。
  await tabs.getByRole("button", { name: /^已完成/ }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get("view")).toBe("done");
  await page.reload();
  await expect(tabs.getByRole("button", { name: /^已完成/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  // 舊網址（沒時間）進「未完成」。
  await page.goto("/reminders?view=no_time");
  await expect(tabs.getByRole("button", { name: /^未完成/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await expect(rowOf(page, noTime)).toContainText("R-");
});

// Guards: REQ-59
test("TestReminderMoreOptions", async ({ page, seed }) => {
  const what = `秘書幫我確認 ${rand()}`;
  await page.goto("/reminders?view=no_time");
  await page.getByRole("button", { name: "更多選項" }).click();
  // 漸進揭露：選「秘書幫我確認」才出現必填的「要確認什麼」；「想聊什麼」是另一個選項才有。
  await expect(page.getByLabel("要確認什麼")).toHaveCount(0);
  await page.getByRole("button", { name: "秘書幫我確認", exact: true }).click();
  await expect(page.getByLabel("要確認什麼")).toBeVisible();
  await expect(page.getByLabel("想聊什麼")).toHaveCount(0);
  const input = page.getByLabel("新增待辦");
  await input.fill(`後天 10 點 ${what}`);
  await input.press("Enter");
  await expect(page.getByText("請寫要確認什麼")).toBeVisible();
  expect((await listAll(seed)).some((r) => r.content === what)).toBe(false);
  await page.getByLabel("要確認什麼").fill("帳單繳了沒");
  await input.press("Enter");
  await expect
    .poll(async () => (await listAll(seed)).find((r) => r.content === what)?.follow)
    .toBe("check");
});

// Guards: REQ-142
test("TestQuickInputSayToAgent", async ({ page, request, baseURL }) => {
  const names = await rosterNames(request, baseURL ?? "");
  const code = names.find((a) => a.id === "code");
  expect(code, "名冊裡要有 code").toBeTruthy();
  const word = `進度如何 ${rand()}`;
  await page.goto("/");
  const input = page.getByLabel("快速輸入");
  // 打 @ 列出秘書讓人點。
  await input.fill("@");
  await expect(page.getByRole("list", { name: "選一位秘書" })).toBeVisible();
  await page
    .getByRole("list", { name: "選一位秘書" })
    .getByRole("button", { name: code?.name ?? "" })
    .click();
  await expect(input).toHaveValue(`@${code?.name} `);
  // 焦點和游標是下一個畫面才回到輸入框（同一個 requestAnimationFrame）；先等焦點，不然字會打在最前面。
  await expect(input).toBeFocused();
  await input.pressSequentially(word);
  const preview = page.getByTestId("quick-preview");
  await expect(preview).toContainText(`傳給 ${code?.name}`);
  await expect(preview).toContainText(word);
  await input.press("Enter");
  // toast「已傳給 …」帶「去聊」，到那位的討論串，秘書收到並回覆。
  await expect(page.getByText(`已傳給 ${code?.name}`)).toBeVisible();
  await page.getByRole("button", { name: "去聊" }).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/chat/code");
  await expect(page.getByRole("log", { name: "對話" }).getByText(`收到：${word}`)).toBeVisible();
});

// Guards: REQ-142
test("TestQuickInputUnknownAgent", async ({ page }) => {
  await page.goto("/");
  const input = page.getByLabel("快速輸入");
  await input.fill("@沒這個人 哈囉");
  await expect(page.getByText("找不到「沒這個人」這位秘書")).toBeVisible();
  await expect(page.getByRole("button", { name: "送出", exact: true })).toBeVisible();
  await input.press("Enter");
  await expect(input).toHaveValue("@沒這個人 哈囉");
});

// Guards: REQ-59
test("TestQuickInputEditChips", async ({ page }) => {
  await page.goto("/");
  const input = page.getByLabel("快速輸入");
  await input.fill("明天 9 點 健身房");
  const preview = page.getByTestId("quick-preview");
  await expect(preview).toContainText("09:00");
  // 時間膠囊的 × 拿掉時間：整句變成內容，膠囊變成「沒時間」。
  await preview.getByRole("button", { name: "拿掉時間" }).click();
  await expect(preview).toContainText("未定時間");
  await expect(preview).toContainText("明天 9 點 健身房");
  // 負責秘書的膠囊：點了列出秘書，選一位。
  await preview.getByRole("button", { name: "改負責秘書" }).click();
  await expect(page.getByRole("group", { name: "改負責秘書" })).toBeVisible();
});

// Guards: REQ-59
test("TestPaletteAddReminderAndSay", async ({ page, seed, request, baseURL, isMobile }) => {
  const what = `指令面板新增 ${rand()}`;
  await page.goto("/");
  await expect(page.getByRole("navigation", { name: "主選單" })).toBeVisible();
  const open = async () => {
    if (isMobile) await page.getByRole("button", { name: "搜尋", exact: true }).click();
    else await page.keyboard.press("Control+k");
    const dialog = page.getByRole("dialog", { name: "搜尋或下指令" });
    await expect(dialog).toBeVisible();
    return dialog;
  };
  let dialog = await open();
  await dialog.getByRole("combobox").fill(`明天 9 點 ${what}`);
  // 有解析結果時，第一個結果是「新增待辦：<時間>　<內容>」。
  const first = dialog.getByRole("option").first();
  await expect(first).toContainText("新增待辦：");
  await expect(first).toContainText(what);
  await dialog.getByRole("combobox").press("Enter");
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText(/已新增：/)).toBeVisible();
  await expect
    .poll(async () => (await listAll(seed)).find((r) => r.content === what)?.status)
    .toBe("todo");

  // @名字 內容 → 傳給 <人名>。
  const code = (await rosterNames(request, baseURL ?? "")).find((a) => a.id === "code");
  const word = `面板傳話 ${rand()}`;
  dialog = await open();
  await dialog.getByRole("combobox").fill(`@${code?.name} ${word}`);
  await expect(dialog.getByRole("option").first()).toContainText(`傳給 ${code?.name}：${word}`);
  await dialog.getByRole("combobox").press("Enter");
  await expect(page.getByText(`已傳給 ${code?.name}`)).toBeVisible();
});

// 「未完成」第一組是逾期：前幾天沒做的在那裡，寫日期與幾點（只有「今天」那組才只寫幾點，RMD-21）。
test("TestReminderLateGroupShowsDate", async ({ page, seed }) => {
  const word = `三天前的事 ${rand()}`;
  const at = new Date(Date.now() - 3 * 24 * 3600_000);
  at.setHours(8, 30, 0, 0);
  await seed.reminder(word, at);
  await page.goto("/reminders");
  const row = rowOf(page.getByTestId("reminders-group-late"), word);
  await expect(row).toBeVisible();
  await expect(row).toContainText("08:30");
  await expect(row).toContainText(`${at.getMonth() + 1}/${at.getDate()}`);
});

// RMD-22：快速輸入打「交代 整理照片」是交代任務：預覽寫「交代任務」，Enter 開任務單（標題不含「交代」、toast 可以點開），不新增待辦。
test("TestQuickInputAssignOpensTask", async ({ page, seed }) => {
  const what = `整理照片 ${rand()}`;
  await page.goto("/reminders");
  const input = page.getByLabel("新增待辦");
  await input.fill(`交代 ${what}`);
  const preview = page.getByTestId("quick-preview");
  await expect(preview).toContainText("交代任務");
  await expect(preview).toContainText("交給：");
  await expect(preview).not.toContainText("新增待辦");
  await input.press("Enter");
  // 開好了：toast「已開任務單 SEC-n 給…」，按「看這張單」到那張單（跟「改開任務單」同一條路）。
  const toast = page.locator("[data-sonner-toast]").filter({ hasText: "已開任務單" });
  await expect(toast).toBeVisible();
  const res = await seed.api<{ tasks?: { id: string; title: string }[] }>("/api/tasks/list", {});
  const made = (res.tasks ?? []).find((x) => x.title === what);
  expect(made).toBeTruthy();
  await toast.getByRole("button", { name: "看這張單" }).click();
  await expect(page).toHaveURL(new RegExp(`/tasks/${made?.id}`));
  expect((await listAll(seed)).some((r) => r.content.includes(what))).toBe(false);
});

// DEV-104／TSK-28：「交代 …」時貼的截圖跟著進單的附件（以前只送標題，圖不見）。
test("TestQuickInputAssignCarriesImage", async ({ page, seed }) => {
  const what = `看截圖 ${rand()}`;
  await page.goto("/reminders");
  const input = page.getByLabel("新增待辦");
  await input.fill(`交代 ${what}`);
  await pasteImage(input);
  await expect(page.getByRole("list", { name: "附上的圖片與檔案" }).getByRole("img")).toBeVisible();
  await expect(input).toHaveValue(`交代 ${what}`);
  // 傳完才送得出去（上傳中按 Enter 不會開少了圖的單）。
  await expect(page.getByText("上傳中")).toHaveCount(0);
  await input.press("Enter");
  await expect(page.locator("[data-sonner-toast]").filter({ hasText: "已開任務單" })).toBeVisible();
  const res = await seed.api<{ tasks?: { title: string; attachments?: string[] }[] }>(
    "/api/tasks/list",
    {},
  );
  const made = (res.tasks ?? []).find((x) => x.title === what);
  expect(made?.attachments ?? []).toHaveLength(1);
  expect(made?.attachments?.[0]).toMatch(/^\/share\/.+-paste-\d+\.png$/);
});

// RMD-23：記待辦時在「更多選項」寫描述、貼圖、選檔；記好之後描述與附件都在單上（附件是這台 /share/ 裡真的有的檔，描述裡不再塞圖片行）。
test("TestReminderCreateWithDescAndAttachments", async ({ page, seed, hgServer }) => {
  const what = `看牙醫 ${rand()}`;
  await page.goto("/reminders?view=no_time");
  const input = page.getByLabel("新增待辦");
  await input.fill(what);
  await page.getByRole("button", { name: "更多選項" }).click();
  await page.getByLabel("描述").fill("帶健保卡");
  await pasteImage(input);
  const doc = `掛號單-${rand()}.pdf`;
  await page.getByTestId("quick-file").setInputFiles({
    name: doc,
    mimeType: "application/pdf",
    buffer: Buffer.from("%PDF-1.4"),
  });
  const list = page.getByRole("list", { name: "附上的圖片與檔案" });
  await expect(list.getByRole("img")).toBeVisible();
  await expect(list.getByText(doc)).toBeVisible();
  await expect(page.getByRole("button", { name: "送出" })).toBeEnabled();
  await input.press("Enter");
  await expect
    .poll(async () => (await listAll(seed)).find((r) => r.content === what)?.body)
    .toBe("帶健保卡");
  const made = (await listAll(seed)).find((r) => r.content === what);
  expect(made?.attachments ?? []).toHaveLength(2);
  expect(made?.attachments?.[0]).toMatch(/^\/share\/.+-paste-\d+\.png$/);
  expect(made?.attachments?.[1]).toMatch(new RegExp(`^/share/.+-${doc.replace(".", "\\.")}$`));
  for (const url of made?.attachments ?? []) {
    expect(
      existsSync(path.join(hgServer.home, "runtime", "share", url.slice("/share/".length))),
    ).toBe(true);
  }
  await expect(list).toHaveCount(0);
});

// RMD-23：開好的待辦在詳細頁可以改描述（就地編輯，⌘/Ctrl+Enter 存）、再加附件（選檔或貼截圖）；圖片直接顯示，拿掉只從單上拿掉。
test("TestReminderDetailDescAndAttachments", async ({ page, seed }) => {
  const id = await seed.reminder(`補描述 ${rand()}`);
  await page.goto(`/reminders/${id}`);
  await page.getByRole("button", { name: /沒有描述/ }).click();
  const box = page.getByLabel("描述");
  await box.fill("第一行\n第二行");
  await box.press("ControlOrMeta+Enter");
  await expect.poll(async () => (await reminderOf(seed, id)).body).toBe("第一行\n第二行");
  await expect(page.getByText("第二行")).toBeVisible();
  // 有描述之後：內容照 Markdown 畫（連結、圖可以點），改要按標籤旁的「改描述」。
  await page.getByRole("button", { name: "改描述" }).click();
  await box.fill("看 **這裡**");
  await box.press("ControlOrMeta+Enter");
  await expect.poll(async () => (await reminderOf(seed, id)).body).toBe("看 **這裡**");
  await expect(page.locator("strong", { hasText: "這裡" })).toBeVisible();

  const section = page.getByRole("region", { name: /^附件/ });
  await expect(section.getByText("還沒有附件。")).toBeVisible();
  const shot = `收據-${rand()}.png`;
  await page.getByTestId("attach-file").setInputFiles({
    name: shot,
    mimeType: "image/png",
    buffer: Buffer.from(PNG_1X1_B64, "base64"),
  });
  const img = section.getByRole("img", { name: shot });
  await expect(img).toBeVisible();
  await expect
    .poll(() => img.evaluate((el: HTMLImageElement) => el.naturalWidth))
    .toBeGreaterThan(0);
  await pasteImage(page.locator("body"));
  await expect(section.getByRole("img", { name: /^paste-\d+\.png$/ })).toBeVisible();
  await expect.poll(async () => (await reminderOf(seed, id)).attachments?.length ?? 0).toBe(2);
  await section.getByRole("button", { name: `拿掉附件 ${shot}` }).click();
  await expect(img).toHaveCount(0);
  await expect.poll(async () => (await reminderOf(seed, id)).attachments?.length ?? 0).toBe(1);
});

// WEB-161：右上角「編輯」打開整張表單，改內容、難度、跟進方式、描述，儲存一次，畫面跟著更新。
test("TestReminderDetailEditForm", async ({ page, seed }) => {
  const content = `整張改 ${rand()}`;
  const id = await seed.reminder(content);
  await page.goto(`/reminders/${id}`);
  await page.getByRole("button", { name: "編輯", exact: true }).click();
  const form = page.getByRole("dialog", { name: "編輯待辦" });
  await expect(form).toBeVisible();
  await expect(form.getByLabel("內容")).toHaveValue(content);

  // 跟進改成「秘書幫我確認」才出現「要確認什麼」，沒寫擋下、表單不關。
  await form.getByRole("button", { name: "秘書幫我確認", exact: true }).click();
  await form.getByRole("button", { name: "儲存", exact: true }).click();
  await expect(form.getByRole("alert").filter({ hasText: "請寫要確認什麼" })).toBeVisible();
  await expect(form).toBeVisible();

  const next = `${content} 改過`;
  await form.getByLabel("內容").fill(next);
  await form.getByLabel("要確認什麼").fill("繳了沒");
  await form.getByRole("button", { name: "困難", exact: true }).click();
  await form.getByLabel("描述").fill("帶健保卡");
  await form.getByRole("button", { name: "儲存", exact: true }).click();
  await expect(form).toHaveCount(0);

  await expect(page.getByText(next).first()).toBeVisible();
  await expect(page.getByText("困難", { exact: true })).toBeVisible();
  await expect(page.getByText("帶健保卡")).toBeVisible();
  const saved = await reminderOf(seed, id);
  expect(saved.content).toBe(next);
  expect(saved.difficulty).toBe("hard");
  expect(saved.follow).toBe("check");
  expect(saved.body).toBe("帶健保卡");
});

// 待辦清單每列標難度與做完可得幾點（WEB-162、RMD-26）：只有「問我做了沒」「秘書幫我確認」才標；
// 沒寫的是「普通 累積 +2」，改成困難的標「困難 累積 +3」（DEV-156 U-14：不叫「點」）；往回填的時間不算過期；只提醒的不標（過期加點看 go 測試）。
test("TestReminderDifficultyLabel", async ({ page, request, baseURL, seed }) => {
  const plain = `沒寫難度 ${rand()}`;
  const hard = `很難的事 ${rand()}`;
  const late = `拖很久的事 ${rand()}`;
  const remind = `只是提醒 ${rand()}`;
  const confirm = async (content: string, at: Date, difficulty?: string) => {
    const res = await request.post(`${baseURL}/api/reminders/create`, {
      data: { content, owner: "life", follow: "confirm", at: at.toISOString(), difficulty },
    });
    expect(res.ok()).toBe(true);
  };
  const soon = new Date(Date.now() + 60 * 60 * 1000);
  await confirm(plain, soon);
  await confirm(hard, soon, "hard");
  await confirm(late, new Date(Date.now() - 8 * 24 * 60 * 60 * 1000)); // 往回填的時間不算過期（RMD-26）
  await seed.reminder(remind, soon);
  await page.goto("/reminders");
  const row = (text: string) => page.locator("li").filter({ hasText: text });
  await expect(row(plain).getByTestId("reminder-difficulty")).toHaveText("普通");
  await expect(row(plain).getByTestId("reminder-points")).toContainText("累積 +2");
  await expect(row(hard).getByTestId("reminder-difficulty")).toHaveText("困難");
  await expect(row(hard).getByTestId("reminder-points")).toContainText("累積 +3");
  await expect(row(late).getByTestId("reminder-points")).toContainText("累積 +2");
  await expect(row(remind)).toBeVisible();
  await expect(row(remind).getByTestId("reminder-difficulty")).toHaveCount(0);
});

// DEV-156 U-30：每列「標成做完」的名字帶待辦內容，讀屏才分得出是哪一筆。
test("TestMarkDoneButtonNamesTheReminder", async ({ page, seed }) => {
  const a = `繳電話費 ${Date.now()}`;
  const b = `倒垃圾 ${Date.now()}`;
  await seed.reminder(a, new Date(Date.now() + 3600_000));
  await seed.reminder(b, new Date(Date.now() + 7200_000));
  await page.goto("/reminders");
  await expect(page.getByRole("button", { name: `把〈${a}〉標成做完` })).toHaveCount(1);
  await expect(page.getByRole("button", { name: `把〈${b}〉標成做完` })).toHaveCount(1);
});
