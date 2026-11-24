import type { Page } from "@playwright/test";
import { expect, twoHomesTest as test } from "./fixtures";

// 聊天合併兩台（S3-6，計畫 2026-11-01-board-multi-home）：家 A 的看板直接連家 B 的 API。
// 清單照電腦分組（同一位秘書在兩台是兩列）、在 A 的看板對 B 的秘書送話（B 的假引擎回覆）、
// B 停掉時只有 B 那段寫原因，A 的對話照常。只跑桌機（不列進 mobileSpecs）。

const rand = () => Math.random().toString(36).slice(2, 8);
const PNG_1X1 = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

const chatLog = (page: Page) => page.getByRole("log", { name: "對話" });
const section = (page: Page, label: string) =>
  page.getByRole("region", { name: `${label} 的討論串` });

async function say(page: Page, text: string) {
  await page.locator("#hg-composer").fill(text);
  await page.locator('button[aria-label="送出訊息"]').click();
}

// 清單模式（1280 寬預設是角色欄，這裡要看得到清單）。
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    try {
      localStorage.setItem("eg.chatPane.v1", "list");
    } catch {
      // 沒有 localStorage 就用預設
    }
  });
});

test("TestChatListGroupsThreadsByHome", async ({ page, homes }) => {
  await page.goto("/chat");
  const a = section(page, homes.a.label);
  const b = section(page, homes.b.label);
  await expect(a.locator('[data-thread="code"]')).toBeVisible();
  await expect(b.locator('[data-thread="code"]')).toBeVisible();
  // 同一位秘書在兩台是兩列；B 那列連到 ?home=<B 的台名>，A 那列網址跟只有一台時一樣。
  await expect(a.locator('[data-thread="code"]')).toHaveAttribute("href", "/chat/code");
  await expect(b.locator('[data-thread="code"]')).toHaveAttribute(
    "href",
    `/chat/code?home=${homes.b.label}`,
  );
  await expect(b.getByTestId("site-tag")).toHaveText(homes.b.label);
});

// 同一位秘書在兩台：討論串頂端一排分頁 [A | B]，各標那台的她的狀態；點了換到那台她的對話（作者 2026-11-02）。
test("TestChatSiteTabsSwitchSameSecretaryBetweenHomes", async ({ page, homes }) => {
  // B 的 code 有一串工作 session 在等你（假引擎看到【權限】就停下來問），A 的 code 有空：兩個分頁各寫各的狀態。
  // 用工作 session 不用她的主要串：B 的主要串還要給同一份 spec 的其他測試送話。
  const b = homes.b.seed;
  await b.closeAllWork();
  const task = await b.doingTask(`分頁狀態 ${rand()}`);
  const work = `code-sec-${task.replace("SEC-", "")}`;
  await b.api("/api/session/open", { agent: "code", topic: task, by: { name: "owner(web)" } });
  await b.api("/api/bus/send", {
    to: work,
    from: "owner(web)",
    kind: "chat",
    body: `【權限】分頁狀態 ${rand()}`,
  });
  try {
    await page.goto("/chat/code");
    const tabs = page.getByTestId("site-tabs");
    const tabA = tabs.locator(`a[data-site=""]`);
    const tabB = tabs.locator(`a[data-site="${homes.b.label}"]`);
    await expect(tabA).toHaveAttribute("aria-current", "page");
    await expect(tabA).toContainText(homes.a.label);
    await expect(tabB).toContainText(homes.b.label);
    await expect(tabB).toHaveAttribute("data-presence", "waiting", { timeout: 15_000 });
    await expect(tabA).toHaveAttribute("data-presence", "idle");
    await expect(tabB).toHaveAttribute("aria-label", `${homes.b.label}（等你處理）`);
    // 點了到那台她的主要討論串（不是等你的那串工作 session）。
    await tabB.click();
    await expect(page).toHaveURL(new RegExp(`/chat/code\\?home=${homes.b.label}$`));
    await expect(page.getByTestId("thread-site")).toContainText(homes.b.label);
    await expect(tabB).toHaveAttribute("aria-current", "page");
    await tabA.click();
    await expect(page).toHaveURL(/\/chat\/code$/);
  } finally {
    await b.closeAllWork();
  }
});

// B 停掉時：B 的分頁還在、灰的、寫連不上，不會整排無聲消失。
test("TestChatSiteTabsShowHomeBDown", async ({ page, homes }) => {
  homes.b.proxy.mode = "reset";
  await page.goto("/chat/code");
  const tabB = page.getByTestId("site-tabs").locator("button:disabled");
  await expect(tabB).toBeVisible({ timeout: 15_000 });
  await expect(tabB).toContainText("連不上");
  await expect(page.getByTestId("site-tabs").locator('a[data-site=""]')).toHaveAttribute(
    "aria-current",
    "page",
  );
});

test("TestChatSendToSecretaryOnHomeB", async ({ page, homes }) => {
  const word = `跨台 ${rand()}`;
  await page.goto("/chat");
  await section(page, homes.b.label).locator('[data-thread="code"]').click();
  await expect(page).toHaveURL(new RegExp(`/chat/code\\?home=${homes.b.label}$`));
  // 對話頂端：誰（人名）、哪台、正式／測試、版本。
  const site = page.getByTestId("thread-site");
  await expect(site).toContainText(homes.b.label);
  await expect(site).toContainText("正式");
  await expect(site).toContainText("版本");
  await expect(page.getByTestId("thread-identity").getByRole("heading")).not.toBeEmpty();

  const sent = page.waitForRequest(
    (r) => r.url().startsWith(`${homes.b.url}/api/bus/send`) && (r.postData() ?? "").includes(word),
  );
  await say(page, word);
  await sent;
  // B 的假引擎收到、回覆經 B 的串流回來。
  await expect(chatLog(page).getByText(`收到：${word}`)).toBeVisible();
  const b = await homes.b.seed.api<{ threads?: { name: string; last_preview?: string }[] }>(
    "/api/session/list_threads",
    { include_stopped: false },
  );
  expect(b.threads?.find((th) => th.name === "code")?.last_preview ?? "").toContain(word);
  // A 的 code 沒收到這句。
  const a = await homes.a.seed.api<{ threads?: { name: string; last_preview?: string }[] }>(
    "/api/session/list_threads",
    { include_stopped: false },
  );
  expect(a.threads?.find((th) => th.name === "code")?.last_preview ?? "").not.toContain(word);
});

test("TestChatAttachmentGoesToHomeB", async ({ page, homes }) => {
  const fileName = `跨台圖-${rand()}.png`;
  await page.goto(`/chat/code?home=${homes.b.label}`);
  await expect(page.getByTestId("thread-site")).toContainText(homes.b.label);
  const uploaded = page.waitForResponse((r) => r.url() === `${homes.b.url}/api/web/upload`);
  await page.getByTestId("composer-file").setInputFiles({
    name: fileName,
    mimeType: "image/png",
    buffer: PNG_1X1,
  });
  expect((await uploaded).status()).toBe(200);
  await expect(
    page.locator('[data-attachment="done"]').filter({ hasText: fileName }),
  ).toBeVisible();
  await page.locator('button[aria-label="送出訊息"]').click();
  // 縮圖從 B 載入（訊息裡存的是 B 的相對路徑，畫面補上 B 的網址）。
  const thumb = page.getByTestId("user-attachments").getByRole("img", { name: fileName }).last();
  await expect(thumb).toBeVisible();
  await expect(thumb).toHaveAttribute("src", new RegExp(`^${homes.b.url}/share/`));
  await expect
    .poll(() => thumb.evaluate((el: HTMLImageElement) => el.naturalWidth))
    .toBeGreaterThan(0);
});

test("TestChatHomeBDownOnlyItsSection", async ({ page, homes }) => {
  homes.b.proxy.mode = "reset";
  await page.goto("/chat");
  // B 整台不在：B 那段只寫「連不到」（這個分頁沒連上過 B，台名用它的網址），A 那段照常。
  const host = new URL(homes.b.url).host;
  const b = section(page, host);
  await expect(b.getByTestId("site-notice")).toContainText("現在連不到", { timeout: 15_000 });
  await expect(b.locator("[data-thread]")).toHaveCount(0);
  const a = section(page, homes.a.label);
  await expect(a.locator('[data-thread="code"]')).toBeVisible();
  await expect(a.getByTestId("site-notice")).toHaveCount(0);

  // A 的對話照常送得出去、收得到回覆。
  const word = `B 不在 ${rand()}`;
  await a.locator('[data-thread="code"]').click();
  await expect(page).toHaveURL(/\/chat\/code$/);
  await say(page, word);
  await expect(chatLog(page).getByText(`收到：${word}`)).toBeVisible();
});

test("TestChatOpenHomeBThreadWhileBDown", async ({ page, homes }) => {
  // 推播或書籤直接開 B 的對話，B 剛好不在、這個分頁也沒連上過它：寫它連不到，不說「設定裡沒有這台」。
  homes.b.proxy.mode = "reset";
  await page.goto(`/chat/code?home=${homes.b.label}`);
  // 清單那段與對話區各寫一次（清單在桌機也看得到；側欄秘書篩選 B 那段的一行不算）。
  const notes = page.getByRole("main").getByTestId("site-notice");
  await expect(notes).toHaveCount(2, { timeout: 15_000 });
  for (const n of await notes.all()) await expect(n).toContainText("現在連不到");
  await expect(page.getByText("看板設定裡沒有這台電腦")).toHaveCount(0);
  await expect(page.locator("#hg-composer")).toHaveCount(0);
});

test("TestChatHomeBStopsWhileOpen", async ({ page, homes }) => {
  await page.goto(`/chat/code?home=${homes.b.label}`);
  await expect(page.getByTestId("thread-site")).toContainText(homes.b.label);
  await expect(chatLog(page)).toBeVisible();
  homes.b.proxy.mode = "reset";
  // 打開著 B 的對話時 B 停掉：送不出去的那句標「沒送到」，不會改送到 A。
  const word = `B 停了 ${rand()}`;
  await say(page, word);
  await expect(page.locator('[data-queued="failed"]').filter({ hasText: word })).toBeVisible();
  const a = await homes.a.seed.api<{ threads?: { name: string; last_preview?: string }[] }>(
    "/api/session/list_threads",
    { include_stopped: false },
  );
  expect(a.threads?.find((th) => th.name === "code")?.last_preview ?? "").not.toContain(word);
  // 換到 A 的對話照常。
  const ok = `A 照常 ${rand()}`;
  await page.goto("/chat/code");
  await say(page, ok);
  await expect(chatLog(page).getByText(`收到：${ok}`)).toBeVisible();
});
