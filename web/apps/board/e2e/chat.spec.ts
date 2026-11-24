import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import type { APIRequestContext, Locator, Page } from "@playwright/test";
import { expect, test } from "./fixtures";

const rand = () => Math.random().toString(36).slice(2, 8);
const PNG_1X1 = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

interface Thread {
  name: string;
  status: string;
  unread: number;
  seen_uuid?: string;
  last_preview?: string;
}

async function threads(request: APIRequestContext, base: string) {
  const res = await request.post(`${base}/api/session/list_threads`, {
    data: { include_stopped: false },
  });
  return (await res.json()) as { threads?: Thread[]; waiting_user: number };
}

async function threadOf(request: APIRequestContext, base: string, name: string) {
  return (await threads(request, base)).threads?.find((t) => t.name === name);
}

async function say(page: Page, text: string) {
  await page.locator("#hg-composer").fill(text);
  await page.locator('button[aria-label="送出訊息"]').click();
}

const row = (page: Page, name: string) => page.locator(`[data-thread="${name}"]`);

// 這份測試寫的是「桌機兩欄：清單一直都在」；1280 寬預設改成角色欄（REQ-156，persona.spec 測那個版面），
// 所以這裡一律先選清單模式。
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    try {
      localStorage.setItem("eg.chatPane.v1", "list");
    } catch {
      // 沒有 localStorage 就用預設
    }
  });
});

/** 桌機兩欄時清單一直都在；手機要先從討論串按返回。 */
async function showList(page: Page) {
  const list = page.getByRole("navigation", { name: "討論串" });
  if (!(await list.isVisible())) await page.getByRole("link", { name: "上一頁" }).click();
  await expect(list).toBeVisible();
}

async function chatBadge(page: Page, isMobile: boolean): Promise<Locator> {
  const nav = page.getByRole("navigation", { name: "主選單" });
  return isMobile
    ? nav.getByRole("link", { name: /^聊天/ }).locator('[data-tone="attention"]')
    : nav.getByRole("link", { name: /^聊天/ });
}

// 同一個討論串在所有測試之間累積；停下來等回答的那一輪還沒結束時，最後一項不會是秘書的回覆，
// 所以「最後一項是『已允許』」就代表這次的回覆真的來了。
const chatLog = (page: Page) => page.getByRole("log", { name: "對話" });
// 秘書的訊息旁有頭像（頭像的首字也是文字），所以只比對最後一項裡的對話泡泡。
const lastItem = (page: Page) =>
  chatLog(page).locator(":scope > div").last().getByTestId("assistant-bubble");

// 每個會停下來等回答的測試都要自己收尾（allow／deny），別留 pending 影響後面的測試。
async function allowAndExpectReply(page: Page) {
  await page.getByRole("button", { name: "允許" }).click();
  await expect(lastItem(page)).toHaveText("已允許");
}

// Guards: REQ-157
test("TestThreadShowsTime @smoke", async ({ page, isMobile }, info) => {
  info.skip(!isMobile, "手機版的討論串");
  const word = `看時間 ${rand()}`;
  await page.goto("/chat/code");
  await say(page, word);
  await expect(chatLog(page).getByText(`收到：${word}`)).toBeVisible();
  // 第一則訊息前面有一個時間標記（同一天只寫 HH:mm）。
  const first = chatLog(page).locator(":scope > *").first();
  await expect(first).toHaveAttribute("role", "separator");
  await expect(first).toHaveText(/^\d{2}:\d{2}$/);
  await expect(chatLog(page).getByRole("separator").first()).toBeVisible();
});

// Guards: REQ-142
test("TestChatSendAndReply @smoke", async ({ page }) => {
  const word = `哈囉 ${rand()}`;
  await page.goto("/chat/code");
  await say(page, word);
  await expect(chatLog(page).getByText(`收到：${word}`)).toBeVisible();
  // 排隊中的泡泡被正式的訊息換掉：只剩一個泡泡，輸入框清空。
  await expect(page.getByText(word, { exact: true })).toHaveCount(1);
  await expect(page.getByText("排隊中", { exact: true })).toHaveCount(0);
  await expect(page.locator("#hg-composer")).toHaveValue("");
  // 只連一台：沒有切電腦的分頁。
  await expect(page.getByTestId("site-tabs")).toHaveCount(0);
  // 工具呼叫收成一個膠囊，點開只有工具的白話（引擎給的，SES-65），沒有工具的輸出。
  const pill = page.getByRole("button", { name: /用了 \d+ 個工具/ }).last();
  await expect(pill).toHaveAttribute("aria-expanded", "false");
  await pill.click();
  await expect(pill).toHaveAttribute("aria-expanded", "true");
  await expect(
    page
      .getByRole("listitem")
      .filter({ hasText: /^讀檔$/ })
      .last(),
  ).toBeVisible();
  await expect(page.getByText("工具的輸出")).toHaveCount(0);
});

// WEB-125、WEB-126：用了很多工具時，展開的清單把同一種合併成「跑指令 ×12」，順序照第一次出現；
// 沒有白話的（舊資料）照原名，不會壞；膠囊上的總數照實算。
test("TestToolListMergesSameKind", async ({ page }) => {
  const word = `多工具 ${rand()} 【多工具】`;
  await page.goto("/chat/code");
  await say(page, word);
  await expect(chatLog(page).getByText(`收到：${word}`)).toBeVisible();
  const pill = page.getByRole("button", { name: "用了 17 個工具" }).last();
  await pill.click();
  const list = pill.locator("xpath=following-sibling::ul");
  await expect(list.getByRole("listitem")).toHaveText([
    "讀檔 ×2",
    "跑指令 ×12",
    "找內容 ×2",
    "OldTool",
  ]);
  await expect(page.getByText("工具的輸出")).toHaveCount(0);
});

// Guards: REQ-142
test("TestComposerKeys", async ({ page, isMobile }) => {
  const word = `按鍵 ${rand()}`;
  await page.goto("/chat/code");
  const box = page.locator("#hg-composer");
  await box.fill(word);
  await box.press("Enter");
  if (isMobile) {
    // 觸控裝置：Enter 換行，不送出。
    await expect(box).toHaveValue(`${word}\n`);
    await page.waitForTimeout(300);
    await expect(chatLog(page).getByText(`收到：${word}`)).toHaveCount(0);
    await page.locator('button[aria-label="送出訊息"]').click();
  } else {
    // Shift+Enter 換行，Enter 送出。
    await expect(chatLog(page).getByText(`收到：${word}`)).toBeVisible();
    await box.fill("第一行");
    await box.press("Shift+Enter");
    await expect(box).toHaveValue("第一行\n");
    await box.fill("");
    return;
  }
  await expect(chatLog(page).getByText(`收到：${word}`)).toBeVisible();
});

// Guards: REQ-142
test("TestDraftSurvivesLeaving", async ({ page }) => {
  const word = `草稿 ${rand()}`;
  await page.goto("/chat/code");
  await page.locator("#hg-composer").fill(word);
  await page.goto("/chat");
  await page.goto("/chat/code");
  await expect(page.locator("#hg-composer")).toHaveValue(word);
  await page.locator("#hg-composer").fill("");
  await page.goto("/chat/code");
  await expect(page.locator("#hg-composer")).toHaveValue("");
});

// Guards: REQ-142
test("TestSendFailureCanResend", async ({ page }) => {
  const word = `重送 ${rand()}`;
  await page.goto("/chat/code");
  await page.route("**/api/bus/send", (route) => route.abort());
  await say(page, word);
  await expect(page.getByText(/這句沒送到/)).toBeVisible();
  await expect(page.getByText(word, { exact: true })).toHaveCount(1);
  await page.unroute("**/api/bus/send");
  await page.getByRole("button", { name: "重送" }).click();
  await expect(chatLog(page).getByText(`收到：${word}`)).toBeVisible();
  await expect(page.getByText(/這句沒送到/)).toHaveCount(0);
});

// Guards: REQ-142
test("TestWorkingShowsActivity", async ({ page }) => {
  const word = `慢慢來 ${rand()}`;
  await page.goto("/chat/code");
  await say(page, `【慢】${word}`);
  await showList(page);
  // 處理中：清單那一列顯示正在做什麼（Read），不顯示工具的輸出。
  const code = row(page, "code");
  await expect(code).toHaveAttribute("data-thread-status", "working");
  await expect(code.getByTestId("row-status")).toHaveText("Read");
  await expect(page.getByText("工具的輸出")).toHaveCount(0);
  await expect(code).not.toHaveAttribute("data-thread-status", "working", { timeout: 15_000 });
});

test("TestChatDefaultDisplayHidesTools", async ({ page, seed }) => {
  const set = (chat_display: string) =>
    seed.api("/api/system/set_chat_display", { chat_display, by: { name: "owner(test)" } });
  const word = `慢慢來 ${rand()}`;
  await set("default");
  try {
    await page.goto("/settings/display");
    await expect(
      page.getByRole("group", { name: "對話的顯示方式" }).getByRole("button", { name: "預設" }),
    ).toHaveAttribute("aria-pressed", "true");

    await page.goto("/chat/code");
    await say(page, `【慢】${word}`);
    // 工具在跑：只有三點和「處理中」，沒有工具字眼和膠囊。
    await expect(page.getByRole("status", { name: /正在回覆/ })).toBeVisible();
    await expect(page.getByRole("button", { name: /用了 \d+ 個工具/ })).toHaveCount(0);
    await showList(page);
    const code = row(page, "code");
    await expect(code).toHaveAttribute("data-thread-status", "working");
    await expect(code.getByTestId("row-status")).toHaveText("處理中");
    await expect(code).not.toHaveAttribute("data-thread-status", "working", { timeout: 15_000 });

    // 切到工程師：同一串的工具膠囊回來（紀錄一直都在）。
    await page.goto("/settings/display");
    await page
      .getByRole("group", { name: "對話的顯示方式" })
      .getByRole("button", { name: "工程師" })
      .click();
    await expect
      .poll(
        async () =>
          (await seed.api<{ chat_display: string }>("/api/system/get_mode", {})).chat_display,
      )
      .toBe("engineer");
    // 重新整理後還在。
    await page.reload();
    await expect(
      page.getByRole("group", { name: "對話的顯示方式" }).getByRole("button", { name: "工程師" }),
    ).toHaveAttribute("aria-pressed", "true");
    await page.goto("/chat/code");
    await expect(page.getByRole("button", { name: /用了 \d+ 個工具/ }).last()).toBeVisible();
  } finally {
    await set("engineer");
  }
});

// Guards: REQ-142
test("TestThreadStatusDots", async ({ page, request, baseURL, isMobile }) => {
  const base = baseURL ?? "";
  const before = (await threads(request, base)).waiting_user;
  await page.goto("/chat/code");
  await say(page, `【權限】動作 ${rand()}`);
  // 標題欄：橘點＋「要你同意」（等的是權限）。
  const header = page.locator("header").filter({ has: page.getByTestId("thread-status") });
  await expect(header.getByTestId("thread-identity").locator('[data-dot="waiting"]')).toBeVisible();
  await expect(header.getByTestId("thread-status")).toHaveText("要你同意");
  await expect(page.getByRole("region", { name: /想執行/ })).toContainText("建立檔案 e2e.txt");

  // 清單：黃點；分頁標題 ●；選單徽章比原來多 1（桌機有數字、手機是黃點）。
  await showList(page);
  const code = row(page, "code");
  await expect(code.locator('[data-dot="waiting"]')).toBeVisible();
  await expect(code.getByTestId("row-status")).toHaveText("要你同意");
  await expect(page).toHaveTitle(/^● /);
  const badge = await chatBadge(page, isMobile);
  if (isMobile) await expect(badge).toBeVisible();
  else await expect(badge).toContainText(String(before + 1));

  // 按「允許」：四處同時消失，出現「已允許」。
  await code.click();
  await allowAndExpectReply(page);
  await expect(page.getByText("已允許：建立檔案 e2e.txt")).toBeVisible();
  await expect(page.getByRole("region", { name: /想執行/ })).toHaveCount(0);
  await expect(header.getByTestId("thread-identity").locator('[data-dot="waiting"]')).toHaveCount(
    0,
  );
  await showList(page);
  await expect(code.locator('[data-dot="waiting"]')).toHaveCount(0);
  await expect(code).not.toHaveAttribute("data-thread-status", "waiting_user");
  await expect.poll(async () => (await threads(request, base)).waiting_user).toBe(before);
  if (!isMobile) {
    const after = await chatBadge(page, false);
    await expect(after).not.toContainText(String(before + 1));
  }
});

// Guards: REQ-142
test("TestPermissionDenied @smoke", async ({ page }) => {
  await page.goto("/chat/code");
  await say(page, `【權限】要拒絕 ${rand()}`);
  const card = page.getByRole("region", { name: /想執行/ });
  await card.getByRole("button", { name: "拒絕" }).click();
  // 就地展開理由，不用瀏覽器的對話框。
  await card.getByLabel("理由（選填）").fill("先等我看過");
  await card.getByRole("button", { name: "確定拒絕" }).click();
  await expect(page.getByText("已拒絕：建立檔案 e2e.txt（先等我看過）")).toBeVisible();
  await expect(page.getByText("已拒絕：先等我看過", { exact: true }).last()).toBeVisible();
  await expect(card).toHaveCount(0);
});

// Guards: REQ-139
test("TestAnswerShowsSentAtOnce", async ({ page }) => {
  await page.goto("/chat/code");
  await page.route("**/api/session/resolve", async (route) => {
    await new Promise((r) => setTimeout(r, 1200));
    await route.continue();
  });
  await say(page, `【權限】慢回答 ${rand()}`);
  const card = page.getByRole("region", { name: /想執行/ });
  await card.getByRole("button", { name: "允許" }).click();
  // 伺服器還沒回，按鈕已經換成停用的「已送出」。
  const sent = card.getByRole("button", { name: "已送出" });
  await expect(sent).toBeVisible();
  await expect(sent).toBeDisabled();
  await expect(page.getByText("已允許：建立檔案 e2e.txt")).toBeVisible();
});

// Guards: REQ-142
test("TestAnsweredElsewhere", async ({ page, seed }) => {
  await page.goto("/chat/code");
  await say(page, `【權限】搶先 ${rand()}`);
  const card = page.getByRole("region", { name: /想執行/ });
  await expect(card).toBeVisible();
  // 在使用者按下去之前，別處先回答了。
  await page.route("**/api/session/resolve", async (route) => {
    const got = await seed.api<{ session?: { pending?: { id: string } } }>("/api/session/get", {
      name: "code",
    });
    await seed.api("/api/session/resolve", {
      session: "code",
      pending_id: got.session?.pending?.id,
      decision: { allow: true },
      by: { name: "owner(test)" },
    });
    await route.continue();
  });
  await card.getByRole("button", { name: "允許" }).click();
  await expect(page.getByText("已經在別處回答了")).toBeVisible();
  await expect(card).toHaveCount(0, { timeout: 10_000 });
  // 沒有我們自己回答的膠囊。
  await expect(page.getByText(/^已允許：/)).toHaveCount(0);
  await expect(lastItem(page)).toHaveText("已允許");
});

// Guards: REQ-142
test("TestQuestionAnswered", async ({ page }) => {
  await page.goto("/chat/code");
  await say(page, `【選擇】選一個 ${rand()}`);
  const card = page.getByRole("region", { name: /問你/ });
  await expect(card).toContainText("要哪一個？");
  await card.getByRole("button", { name: "B", exact: true }).click();
  await expect(lastItem(page)).toHaveText("你選了 B");
  await expect(page.getByText("你回答了：B")).toBeVisible();
  await expect(card).toHaveCount(0);
});

// 選擇題卡片帶她問之前的話與每個選項的說明（WEB-85，作者 2026-11-02）。
test("TestQuestionShowsIntroAndOptionDescriptions", async ({ page }) => {
  await page.goto("/chat/code");
  await say(page, `【選說明】選一個 ${rand()}`);
  const card = page.getByRole("region", { name: /問你/ });
  await expect(card.getByTestId("pending-intro")).toHaveText(
    "假引擎問之前的說明：兩個選項各有優缺點。",
  );
  const b = card.getByRole("button", { name: /^B/ });
  await expect(b).toContainText("B 的說明：穩");
  await b.click();
  await expect(lastItem(page)).toHaveText("你選了 B");
});

// WEB-122、WEB-157：三題、說明很長，在手機上卡片排在訊息流最後、跟對話一起滑：輸入框還在畫面裡，從題目開頭看起，滑得到最後的「送出回答」。
test("TestManyQuestionsCardScrollsOnPhone", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 600 });
  await page.goto("/chat/code");
  await say(page, `【多題】三題 ${rand()}`);
  const card = page.getByRole("region", { name: /問你/ });
  await expect(card.getByRole("button", { name: /^甲/ })).toBeVisible();
  await expect(card.getByTestId("multi-hint")).toHaveCount(1);
  // 卡片在訊息流裡（不是固定在輸入框上方），剛出現時從開場白開始看。
  await expect(chatLog(page).getByRole("region", { name: /問你/ })).toHaveCount(1);
  await expect(card.getByTestId("pending-intro")).toBeInViewport();
  // 卡片比訊息區高、自己不捲：整張交給訊息區捲，真的滑到最後一個鈕。
  const log = chatLog(page);
  const logBox = await log.boundingBox();
  expect((await card.boundingBox())?.height ?? 0).toBeGreaterThan(logBox?.height ?? 0);
  const send = card.getByRole("button", { name: "送出回答", exact: true });
  await expect(send).not.toBeInViewport();
  await log.evaluate((el) => el.scrollTo({ top: el.scrollHeight }));
  await expect(send).toBeInViewport();
  // 輸入框還在畫面裡。
  const composer = await page.locator("#hg-composer").boundingBox();
  expect((composer?.y ?? 0) + (composer?.height ?? 0)).toBeLessThanOrEqual(600);
  for (const name of [/^甲/, /^丙/, /^二/, /^好$/])
    await card.getByRole("button", { name }).click();
  await card.getByRole("button", { name: "送出回答", exact: true }).click();
  await expect(lastItem(page)).toHaveText("你選了 甲, 丙／二／好");
  // 回答完回到自動捲：她的回覆看得到。
  await expect(lastItem(page)).toBeInViewport();
});

// WEB-157：滑到卡片的最後準備按，上面多了一則訊息：卡片留在原位，手指下的按鈕不被換掉，也不跳「↓ 新訊息」。
// 用權限確認：選擇題開著時主人打字會直接收掉那題（SES-70、WEB-166），只有權限確認會留著等他按。
test("TestCardStaysPutWhenMessageArrives", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 600 });
  await page.goto("/chat/code");
  // 先把對話撐到捲得動（卡片短、對話也短時，上面多一則訊息本來就會把卡片往下推）。
  for (let i = 0; i < 4; i++) {
    const filler = `墊高 ${i} ${rand()}\n${"一段比較長的話，".repeat(12)}`;
    await say(page, filler);
    await expect(chatLog(page).getByText(`收到：墊高 ${i}`, { exact: false })).toHaveCount(1);
  }
  await say(page, `【權限】原位 ${rand()}`);
  const card = page.getByRole("region", { name: /想執行/ });
  const send = card.getByRole("button", { name: "允許", exact: true });
  await expect(send).toHaveCount(1);
  const later = `多說一句 ${rand()}`;
  await page.locator("#hg-composer").fill(later);
  await expect(card).toBeVisible();
  // 輸入框聚焦會收合角色區，等版面穩定再滑、再記位置。
  await page.waitForTimeout(400);
  const log = chatLog(page);
  await log.evaluate((el) => el.scrollTo({ top: el.scrollHeight }));
  await expect(send).toBeInViewport();
  const before = (await send.boundingBox())?.y ?? 0;
  await page.locator('button[aria-label="送出訊息"]').click();
  await expect(log.getByText(later)).toHaveCount(1);
  expect(Math.abs(((await send.boundingBox())?.y ?? 0) - before)).toBeLessThanOrEqual(2);
  await expect(page.getByRole("button", { name: /新訊息/ })).toHaveCount(0);
  await send.click();
  await expect(card).toHaveCount(0);
});

// WEB-157：短卡片貼在底部；上面的角色區展開（訊息區變矮）時，按鈕不被切到畫面外。
test("TestShortCardStaysVisibleWhenHeroExpands", async ({ page, isMobile }, info) => {
  info.skip(!isMobile, "角色畫面收合只在手機");
  await page.goto("/chat/code");
  await page.locator("#hg-composer").click();
  await expect(page.locator('header[data-hero="collapsed"]')).toBeVisible();
  await say(page, `【權限】角色區展開 ${rand()}`);
  const allow = page.getByRole("button", { name: "允許" });
  await expect(allow).toBeInViewport();
  await page.getByRole("button", { name: /展開/ }).click();
  await expect(page.locator('header[data-hero="expanded"]')).toBeVisible();
  await expect(allow).toBeInViewport();
  await allowAndExpectReply(page);
});

// WEB-157：卡片裡很長、斷不開的一行（網址、路徑）要折行，整個訊息區不能變成能左右滑。
test("TestCardLongLineWraps", async ({ page }) => {
  await page.goto("/chat/code");
  await say(page, `【選說明】長行 ${rand()}`);
  const card = page.getByRole("region", { name: /問你/ });
  await card.getByTestId("pending-intro").evaluate((el) => {
    el.textContent = `/very/long/path/${"x".repeat(600)}`;
  });
  const overflow = await chatLog(page).evaluate((el) => el.scrollWidth - el.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);
  await card.getByRole("button", { name: /^B/ }).click();
  await expect(card).toHaveCount(0);
});

// Guards: REQ-139
test("TestQuestionAnsweredByTyping", async ({ page }) => {
  await page.goto("/chat/code");
  await say(page, `【選擇】打字回答 ${rand()}`);
  const card = page.getByRole("region", { name: /問你/ });
  await card.getByRole("button", { name: "其他（打字）" }).click();
  await card.getByLabel("你的答案").fill("C");
  await card.getByRole("button", { name: "送出回答", exact: true }).click();
  await expect(lastItem(page)).toHaveText("你選了 C");
  await expect(page.getByText("你回答了：C")).toBeVisible();
});

// Guards: REQ-143
test("TestDoneMarkerAndUnread", async ({ page, request, baseURL }) => {
  const base = baseURL ?? "";
  // 先打開討論串：算看過了，完成標記和未讀數都清掉。
  await page.goto("/chat/code");
  await expect(page.locator("#hg-composer")).toBeVisible();
  await expect
    .poll(async () => (await threadOf(request, base, "code"))?.unread, { timeout: 15_000 })
    .toBe(0);
  // 在別的頁面送訊息，回到清單看到完成標記與未讀數。
  await page.goto("/");
  const word = `背景 ${rand()}`;
  const sent = await request.post(`${base}/api/bus/send`, { data: { to: "code", body: word } });
  expect(sent.ok()).toBe(true);
  await expect
    .poll(async () => (await threadOf(request, base, "code"))?.status, { timeout: 15_000 })
    .toBe("done");
  await page.goto("/chat");
  const code = row(page, "code");
  await expect(code).toHaveAttribute("data-thread-status", "done");
  await expect(code.getByTestId("row-status")).toHaveText("完成");
  await expect(code.getByRole("img", { name: /則新訊息/ })).toBeVisible();
  await expect(code).toContainText(`收到：${word}`);
  // 打開它：完成標記與未讀數消失。
  await code.click();
  await expect(chatLog(page).getByText(`收到：${word}`)).toBeVisible();
  await expect
    .poll(async () => (await threadOf(request, base, "code"))?.status, { timeout: 15_000 })
    .toBe("idle");
  await showList(page);
  await expect(row(page, "code")).toHaveAttribute("data-thread-status", "idle");
  await expect(row(page, "code").getByRole("img", { name: /則新訊息/ })).toHaveCount(0);
});

// Guards: REQ-143
test("TestOpenThreadMarksSeenButKeepsDot", async ({ page, request, baseURL }) => {
  const base = baseURL ?? "";
  const word = `等你回覆 ${rand()}`;
  const sent = await request.post(`${base}/api/bus/send`, {
    data: { to: "code", body: `【權限】${word}` },
  });
  expect(sent.ok()).toBe(true);
  await expect
    .poll(async () => (await threadOf(request, base, "code"))?.status, { timeout: 15_000 })
    .toBe("waiting_user");
  // 打開討論串：已讀位置移到最後；黃點還在（問題還沒回答）。
  await page.goto("/chat/code");
  await expect(page.getByRole("region", { name: /想執行/ })).toBeVisible();
  await expect
    .poll(async () => (await threadOf(request, base, "code"))?.seen_uuid, { timeout: 15_000 })
    .toBeTruthy();
  expect((await threadOf(request, base, "code"))?.status).toBe("waiting_user");
  await showList(page);
  await expect(row(page, "code").locator('[data-dot="waiting"]')).toBeVisible();
  await row(page, "code").click();
  await allowAndExpectReply(page);
});

// Guards: REQ-112
test("TestAvatarShown", async ({ page, hgServer }) => {
  const dir = path.join(hgServer.home, "personas", "avatars");
  const file = path.join(dir, "builder.png");
  const { mkdirSync } = await import("node:fs");
  mkdirSync(dir, { recursive: true });
  // 起服務時（fixtures.ts）已經放了角色圖（REQ-156 要用）：測完要放回去，不能刪。
  const original = existsSync(file) ? readFileSync(file) : null;
  writeFileSync(file, PNG_1X1);
  try {
    await page.goto("/chat");
    const img = row(page, "code").locator('img[src="/avatars/code.png"]');
    await expect(img).toHaveCSS("opacity", "1");
    expect(await img.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBeGreaterThan(0);
    // 討論串的標題欄也用同一張圖。
    await row(page, "code").click();
    const header = page.locator("header").filter({ has: page.getByTestId("thread-status") });
    await expect(
      header
        .getByTestId("thread-identity")
        .locator('span[role="img"] img[src="/avatars/code.png"]'),
    ).toHaveCSS("opacity", "1");
  } finally {
    if (original) writeFileSync(file, original);
    else rmSync(file, { force: true });
  }
});

// Guards: REQ-34
test("TestShareImageAndFile", async ({ page, seed }) => {
  const dir = mkdtempSync(path.join(tmpdir(), "hg-share-"));
  try {
    const png = path.join(dir, `圖 ${rand()}.png`);
    const txt = path.join(dir, `note-${rand()}.txt`);
    writeFileSync(png, PNG_1X1);
    writeFileSync(txt, "分享的檔案\n");
    const img = await seed.api<{ url: string }>("/api/notify/share", { path: png, agent: "code" });
    const file = await seed.api<{ url: string }>("/api/notify/share", { path: txt, agent: "code" });
    expect(img.url).toMatch(/^\/share\//);
    await page.goto("/chat/code");
    // 假引擎會把收到的字原樣回來，所以這則回覆就是一段含連結的 Markdown。
    await say(page, `圖：![圖](${encodeURI(img.url)}) 檔案：[文件](${file.url})`);
    const thumb = page.getByRole("img", { name: /\.png$/ }).last();
    await expect(thumb).toBeVisible();
    await expect(thumb).toHaveAttribute("src", encodeURI(img.url));
    await expect(thumb).toHaveAttribute("loading", "lazy");
    expect(await thumb.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBeGreaterThan(0);
    // 點縮圖在這頁看大圖（不開新分頁），右上角 X 關掉（WEB-132）。
    await thumb.click();
    const viewer = page.getByTestId("media-viewer");
    await expect(viewer.getByRole("img")).toHaveAttribute("src", encodeURI(img.url));
    await viewer.getByRole("button", { name: "關閉" }).click();
    await expect(viewer).toBeHidden();
    const chip = page.getByRole("link", { name: /^note-.*\.txt$/ }).last();
    await expect(chip).toBeVisible();
    await expect(chip).toHaveAttribute("href", file.url);
    await expect(chip).toHaveAttribute("rel", "noopener noreferrer");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// Guards: REQ-82
test("TestChatAttachmentUpload", async ({ page, hgServer }) => {
  const word = `附件 ${rand()}`;
  const fileName = `午餐-${rand()}.png`;
  await page.goto("/chat/code");
  const sent = page.waitForRequest(
    (r) => r.url().endsWith("/api/bus/send") && (r.postData() ?? "").includes(word),
  );
  await page.getByTestId("composer-file").setInputFiles({
    name: fileName,
    mimeType: "image/png",
    buffer: PNG_1X1,
  });
  // chip 出現、傳完後送出鈕可以按（只有附件也能送，這裡另外打了一句字）。
  const chip = page.locator('[data-attachment="done"]').filter({ hasText: fileName });
  await expect(chip).toBeVisible();
  await page.locator("#hg-composer").fill(word);
  await page.locator('button[aria-label="送出訊息"]').click();
  const body = JSON.parse((await sent).postData() ?? "{}") as { body: string };
  expect(body.body).toContain(word);
  expect(body.body).toMatch(new RegExp(`!\\[${fileName}\\]\\(/share/\\d{8}-\\d{6}-[0-9a-f]{4}-`));
  // chip 清掉；訊息裡出現縮圖（使用者的泡泡），而且 runtime/share/ 真的有這個檔。
  await expect(page.locator("[data-attachment]")).toHaveCount(0);
  const thumb = page.getByTestId("user-attachments").getByRole("img", { name: fileName }).last();
  await expect(thumb).toBeVisible();
  await expect
    .poll(() => thumb.evaluate((el: HTMLImageElement) => el.naturalWidth))
    .toBeGreaterThan(0);
  const shared = readdirSync(path.join(hgServer.home, "runtime", "share")).filter((n) =>
    n.endsWith(`-${fileName}`),
  );
  expect(shared).toHaveLength(1);
  expect(readFileSync(path.join(hgServer.home, "runtime", "share", shared[0] as string))).toEqual(
    PNG_1X1,
  );
});

// Guards: REQ-142
test("TestNewMessageButton", async ({ page, request, baseURL }) => {
  const base = baseURL ?? "";
  const word = `很長 ${rand()}`;
  await page.goto("/chat/code");
  const lines = Array.from({ length: 45 }, (_, i) => `第 ${i + 1} 行`).join("\n");
  await say(page, `${word}\n${lines}`);
  await expect(chatLog(page).getByText(`收到：${word}`)).toBeVisible();
  const log = page.getByRole("log", { name: "對話" });
  // 自己往上捲超過 200px：新訊息進來時不自動捲，改顯示「↓ 新訊息」。
  await log.evaluate((el) => {
    el.scrollTop = 0;
  });
  await expect.poll(() => log.evaluate((el) => el.scrollTop)).toBe(0);
  const next = `新的 ${rand()}`;
  await request.post(`${base}/api/bus/send`, { data: { to: "code", body: next } });
  const jump = page.getByRole("button", { name: "跳到最新訊息" });
  await expect(jump).toBeVisible({ timeout: 15_000 });
  expect(await log.evaluate((el) => el.scrollTop)).toBe(0);
  await jump.click();
  await expect(log.getByText(`收到：${next}`)).toBeInViewport();
  await expect(jump).toHaveCount(0);
});

// Guards: REQ-107
test("TestThreadNotFound", async ({ page }) => {
  await page.goto("/chat/no-such-thread");
  await expect(page.getByRole("heading", { name: "找不到這個討論串" })).toBeVisible();
  await page.getByRole("link", { name: "回到聊天清單" }).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/chat");
});

// Guards: REQ-142
test("TestChatListRows", async ({ page, isMobile }) => {
  await page.goto("/chat");
  const code = row(page, "code");
  await expect(code).toBeVisible();
  // 一列：人名、職責、（相對時間）；整列是一個連結，高度至少 72。
  await expect(code).toContainText("Builder");
  await expect(code).toContainText("測試開發");
  const box = await code.boundingBox();
  expect(box?.height ?? 0).toBeGreaterThanOrEqual(72);
  if (!isMobile) {
    await expect(page.getByText("選一個討論串")).toBeVisible();
    await code.click();
    await expect(page.getByText("選一個討論串")).toHaveCount(0);
    await expect(code).toHaveAttribute("aria-current", "page");
  } else {
    await code.click();
    await expect.poll(() => new URL(page.url()).pathname).toBe("/chat/code");
    // 手機的討論串是全頁：沒有底部選單。
    await expect(page.getByRole("navigation", { name: "主選單" })).toHaveCount(0);
  }
});

// Guards: REQ-142
test("TestChatReconnectUsesSinceUuid", async ({ page }) => {
  // 串流一送完就斷線（假的伺服器回應）：1 秒後重連，帶上最後一則的 uuid，已有的訊息不重複。
  const bodies: Record<string, unknown>[] = [];
  const sse = `data: ${JSON.stringify({ message: { uuid: "fake-1", kind: "assistant", text: "歷史訊息", is_error: false } })}\n\n`;
  await page.route("**/api/session/chat", async (route) => {
    bodies.push(route.request().postDataJSON() as Record<string, unknown>);
    await route.fulfill({ status: 200, contentType: "text/event-stream", body: sse });
  });
  await page.goto("/chat/code");
  await expect(page.getByText("歷史訊息")).toBeVisible();
  await expect.poll(() => bodies.length, { timeout: 10_000 }).toBeGreaterThanOrEqual(2);
  expect(bodies[0]).toMatchObject({ session: "code", follow: true });
  expect(bodies[0]?.since_uuid).toBeUndefined();
  expect(bodies[1]).toMatchObject({ session: "code", follow: true, since_uuid: "fake-1" });
  await expect(page.getByText("歷史訊息")).toHaveCount(1);
});

test("TestChatDraftShowsUntilRealReply", async ({ page }) => {
  // 秘書在忙時，還在打、還沒寫進記錄的那段（draft）先淡色顯示；正式那則到了就收掉、不重複（DEV-150）。
  const ev = (m: Record<string, unknown>) => `data: ${JSON.stringify({ message: m })}\n\n`;
  const bodies: Record<string, unknown>[] = [];
  let final = false;
  await page.route("**/api/session/chat", async (route) => {
    bodies.push(route.request().postDataJSON() as Record<string, unknown>);
    const body = !final
      ? ev({ uuid: "fake-1", kind: "user", text: "早餐吃了豆漿", is_error: false }) +
        ev({ kind: "assistant", text: "收到，我先記", draft: true, is_error: false })
      : ev({ uuid: "fake-2", kind: "assistant", text: "收到，我先記下來。", is_error: false });
    await route.fulfill({ status: 200, contentType: "text/event-stream", body });
  });
  await page.goto("/chat/code");
  const draft = page.locator("[data-draft]");
  // 秘書沒在忙：不畫淡色那段（WEB-160 的打字點點規則管它）。
  await expect(page.getByText("早餐吃了豆漿")).toBeVisible();
  await expect(draft).toHaveCount(0);
  // 讓秘書忙起來（假引擎【慢】會跑十幾秒）。
  await say(page, `【慢】${rand()}`);
  await expect(draft).toHaveText("收到，我先記");
  // 還在打的那段沒有 uuid：重連的游標仍是最後一則正式訊息。
  await expect.poll(() => bodies.length, { timeout: 10_000 }).toBeGreaterThanOrEqual(2);
  expect(bodies[1]).toMatchObject({ since_uuid: "fake-1" });
  final = true;
  await expect(page.getByText("收到，我先記下來。")).toHaveCount(1);
  await expect(draft).toHaveCount(0);
});

const switchButton = (page: Page) => page.getByRole("button", { name: /^換一位秘書/ });
const switchDialog = (page: Page) => page.getByRole("dialog", { name: "換一位秘書" });
// 討論串的標題（桌機兩欄時，清單那一欄也有一個「聊天」的 h1，所以只取裡面有切換鈕的那個）。
const threadTitle = (page: Page) => page.locator("h1").filter({ has: switchButton(page) });

// 這個鈕「可以點到」的垂直範圍（含 ::after 撐出來的部分）：沿著鈕中線上下取樣，看最上層元素是不是它。
async function hitHeight(button: Locator): Promise<number> {
  return button.evaluate((el) => {
    const box = el.getBoundingClientRect();
    const x = box.left + Math.min(box.width / 2, 12);
    const cy = box.top + box.height / 2;
    let hits = 0;
    for (let y = cy - 40; y <= cy + 40; y += 1) {
      if (y < 0) continue;
      const top = document.elementFromPoint(x, y);
      if (top && (top === el || el.contains(top) || top.closest("button") === el)) hits++;
    }
    return hits;
  });
}

// Guards: REQ-200
test("TestChatSwitchAgent @smoke", async ({ page, isMobile }) => {
  await page.goto("/chat/code");
  await expect(threadTitle(page)).toContainText("Builder");
  const trigger = switchButton(page);
  await expect(trigger).toBeVisible();
  await expect(trigger).toHaveAttribute("aria-label", "換一位秘書，現在是 Builder");
  // 觸控目標 ≥ 44px。
  expect(await hitHeight(trigger)).toBeGreaterThanOrEqual(44);

  // 鍵盤可以開：焦點在鈕上按 Enter。
  await trigger.focus();
  await page.keyboard.press("Enter");
  const dialog = switchDialog(page);
  await expect(dialog).toBeVisible();
  // 名冊裡啟用的秘書：頭像、名字、狀態；目前這位打勾。
  const builder = dialog.getByRole("button", { name: /^和 Builder 聊天，/ });
  const helper = dialog.getByRole("button", { name: /^和 Helper 聊天，/ });
  await expect(builder).toHaveAttribute("aria-current", "true");
  await expect(helper).not.toHaveAttribute("aria-current", "true");
  await expect(builder).toContainText(/工作中|等你|有空/);
  await expect(builder.getByRole("img", { name: "目前這位" })).toBeVisible();
  expect((await helper.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(44);

  // Esc 關閉，焦點回到按鈕。
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();

  // 選 Helper：網址與標題都換成她的，上一頁回到 Builder。
  await trigger.click();
  await switchDialog(page)
    .getByRole("button", { name: /^和 Helper 聊天，/ })
    .click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/chat/life");
  await expect(threadTitle(page)).toContainText("Helper");
  await expect(switchDialog(page)).toHaveCount(0);
  await expect(switchButton(page)).toHaveAttribute("aria-label", "換一位秘書，現在是 Helper");
  await page.goBack();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/chat/code");
  await expect(threadTitle(page)).toContainText("Builder");

  // 選目前這位：只是關掉，網址不變。
  await switchButton(page).click();
  await switchDialog(page)
    .getByRole("button", { name: /^和 Builder 聊天，/ })
    .click();
  await expect(switchDialog(page)).toHaveCount(0);
  expect(new URL(page.url()).pathname).toBe("/chat/code");
  // 手機是底部抽屜、桌機是標題下的小浮窗。
  await switchButton(page).click();
  const box = await switchDialog(page).boundingBox();
  const viewport = page.viewportSize();
  if (isMobile)
    expect((box?.y ?? 0) + (box?.height ?? 0)).toBeGreaterThan((viewport?.height ?? 0) - 4);
  else expect(box?.y ?? 0).toBeLessThan(200);
  await page.keyboard.press("Escape");
  // 右上的「⋯」裡也有「換一位秘書」（DEV-156 U-19），打開同一個清單。
  await page.getByTestId("thread-menu").click();
  await page.getByTestId("thread-menu-switch").click();
  await expect(switchDialog(page)).toBeVisible();
  // 選單關掉、清單打開：焦點交到清單裡（不會被關掉的選單拉回「⋯」鈕），鍵盤、讀屏接著就在清單上。
  await expect
    .poll(() => switchDialog(page).evaluate((el) => el.contains(document.activeElement)))
    .toBe(true);
  await page.keyboard.press("Escape");
  await expect(switchDialog(page)).toHaveCount(0);
});

// 手機收合角色畫面後（聚焦輸入框就收合），換人在右上的「⋯」：一顆 44px 的鈕，裡面用字寫「換一位秘書」「重開對話」
// （DEV-156 U-19：以前是 ⇅、↻ 兩個看不出意思的圖示）。「重開對話」只打開說明列。
// Guards: REQ-200
test("TestChatSwitchAgentCollapsedHero", async ({ page, isMobile }, info) => {
  info.skip(!isMobile, "角色畫面收合只在手機");
  await page.goto("/chat/code");
  await expect(page.locator('header[data-hero="expanded"]')).toBeVisible();
  await page.locator("#hg-composer").click();
  await expect(page.locator('header[data-hero="collapsed"]')).toBeVisible();
  // 舊的兩個圖示鈕不在了：標題欄只有一顆「⋯」。
  await expect(page.getByRole("button", { name: /^換一位秘書，現在是/ })).toHaveCount(0);
  const menu = page.getByTestId("thread-menu");
  await expect(menu).toHaveCount(1);
  await expect(menu).toHaveAccessibleName("更多：換一位秘書、重開對話");
  const box = await menu.boundingBox();
  expect(box?.width ?? 0).toBeGreaterThanOrEqual(44);
  expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
  // 「重開對話」：關掉選單、打開說明列（不直接重開）。
  await menu.click();
  await page.getByTestId("thread-restart").click();
  await expect(page.locator("#restart-bar")).toBeVisible();
  // 「換一位秘書」：跟名字旁邊的切換同一個清單。
  await menu.click();
  await page.getByTestId("thread-menu-switch").click();
  await switchDialog(page)
    .getByRole("button", { name: /^和 Helper 聊天，/ })
    .click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/chat/life");
});

// WEB-91：很長、斷不開的一行（網址、程式碼）不會撐破泡泡：主人的泡泡在聊天區裡折行，她的泡泡不超出聊天區（程式碼區塊自己左右捲）。
test("TestLongLinesStayInsideBubbles", async ({ page }) => {
  const tag = rand();
  const url = `https://example.com/${"a".repeat(160)}${tag}`;
  const code = `const x = 1; // ${"很長".repeat(60)}`;
  await page.goto("/chat/code");
  await say(page, ["```", code, "```", "", url].join("\n"));
  const reply = chatLog(page).getByTestId("assistant-bubble").filter({ hasText: tag }).last();
  await expect(reply).toBeVisible();
  const area = await chatLog(page).boundingBox();
  if (!area) throw new Error("沒有聊天區");
  const inside = (box: { x: number; width: number } | null) => {
    expect(box).not.toBeNull();
    expect(box?.x ?? 0).toBeGreaterThanOrEqual(area.x - 1);
    expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(area.x + area.width + 1);
  };
  inside(await reply.boundingBox());
  // 主人的泡泡是原文（含 ```），她的回覆畫成 Markdown（沒有 ```）。
  const mine = chatLog(page).locator("div.whitespace-pre-wrap").filter({ hasText: "```" });
  inside(await mine.filter({ hasText: tag }).last().boundingBox());
  // 頁面本身不會被撐出橫向捲軸。
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
    page.viewportSize()?.width ?? 0,
  );
});
