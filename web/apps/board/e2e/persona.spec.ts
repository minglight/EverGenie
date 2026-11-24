import { copyFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { Page } from "@playwright/test";
import { expect, test } from "./fixtures";

const rand = () => Math.random().toString(36).slice(2, 8);
const fixtures = path.join(path.dirname(fileURLToPath(import.meta.url)), "fixtures");

const chatLog = (page: Page) => page.getByRole("log", { name: "對話" });
const hero = (page: Page) => page.locator("header[data-hero]");

async function say(page: Page, text: string) {
  await page.locator("#hg-composer").fill(text);
  await page.locator('button[aria-label="送出訊息"]').click();
}

/** 在討論串裡造夠長的對話，讓訊息區可以捲動（假引擎會把收到的字原樣回來）。 */
async function seedLongChat(
  request: import("@playwright/test").APIRequestContext,
  base: string,
  to: string,
) {
  const word = `長對話 ${rand()}`;
  const lines = Array.from({ length: 60 }, (_, i) => `第 ${i + 1} 行`).join("\n");
  const res = await request.post(`${base}/api/bus/send`, {
    data: { to, body: `${word}\n${lines}` },
  });
  expect(res.ok()).toBe(true);
  return word;
}

// Guards: REQ-156
test("TestPersonaHeroMobile @smoke", async ({ page, request, baseURL, isMobile }, info) => {
  info.skip(!isMobile, "手機版的角色區");
  const word = await seedLongChat(request, baseURL ?? "", "code");
  await page.goto("/chat/code");
  await expect(chatLog(page).getByText(`收到：${word}`)).toBeVisible();

  // 展開：有影片，src 是 /avatars/code.mp4，高度 ≥ 300（390×844 下 40vh＝337）、人名在。
  const top = hero(page);
  await expect(top).toHaveAttribute("data-hero", "expanded");
  const video = top.locator("video");
  await expect(video).toHaveAttribute("src", "/avatars/code.mp4");
  expect((await top.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(300);
  await expect(top.getByRole("heading", { name: "Builder" })).toBeVisible();
  await expect(top.getByTestId("persona-status")).toContainText("測試開發");
  // 影片真的在播（靜音自動播放）。
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => !v.paused && v.videoWidth === 256))
    .toBe(true);

  // 往上捲 → 收成 60 高的標題欄，人名還在。
  const log = chatLog(page);
  await log.evaluate((el) => {
    el.scrollTop = 0;
  });
  await expect(top).toHaveAttribute("data-hero", "collapsed");
  await expect.poll(async () => (await top.boundingBox())?.height).toBe(60);
  await expect(top.getByRole("heading", { name: "Builder" })).toBeVisible();
  await expect(top.getByTestId("thread-status")).toBeVisible();

  // 點頭像（整塊身分）回展開。
  await top.getByRole("button", { name: "展開 Builder 的角色畫面" }).click();
  await expect(top).toHaveAttribute("data-hero", "expanded");
  await expect.poll(async () => (await top.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(300);

  // life 沒有影片：只有圖。
  await page.goto("/chat/life");
  const life = hero(page);
  await expect(life.locator("[data-persona-media]")).toHaveAttribute("data-persona-media", "image");
  await expect(life.locator("video")).toHaveCount(0);
  await expect(life.locator('img[src="/avatars/life.png"]').first()).toBeAttached();
});

// Guards: REQ-156
test("TestPersonaCollapseOnFocusKeepsBottom", async ({
  page,
  request,
  baseURL,
  isMobile,
}, info) => {
  info.skip(!isMobile, "手機版的角色區");
  const word = await seedLongChat(request, baseURL ?? "", "code");
  await page.goto("/chat/code");
  const log = chatLog(page);
  await expect(log.getByText(`收到：${word}`)).toBeVisible();
  const gap = () => log.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight);
  await expect.poll(gap).toBeLessThanOrEqual(2);

  // 聚焦輸入框（鍵盤要空間）→ 收合；訊息區原本貼著底部，高度變了仍貼著底部。
  await page.locator("#hg-composer").focus();
  await expect(hero(page)).toHaveAttribute("data-hero", "collapsed");
  await expect.poll(async () => (await hero(page).boundingBox())?.height).toBe(60);
  await expect.poll(gap).toBeLessThanOrEqual(2);
  await expect(log.getByText(`收到：${word}`)).toBeInViewport();

  // 再展開：還是貼著底部。
  await hero(page).getByRole("button", { name: "展開 Builder 的角色畫面" }).click();
  await expect(hero(page)).toHaveAttribute("data-hero", "expanded");
  await expect.poll(async () => (await hero(page).boundingBox())?.height ?? 0).toBeGreaterThan(300);
  await expect.poll(gap).toBeLessThanOrEqual(2);
});

// Guards: REQ-156
test("TestPersonaPaneDesktop @smoke", async ({ page, seed, isMobile }, info) => {
  info.skip(isMobile, "桌機版的右側角色欄");
  const title = `角色欄驗收 ${rand()}`;
  const id = await seed.reviewTask(title, "code");
  await page.goto("/chat/code");

  // ≥ 1280 預設：側欄｜討論串｜角色欄，清單收起。
  const pane = page.getByRole("complementary", { name: "Builder 的角色欄" });
  await expect(pane).toBeVisible();
  await expect(pane.locator("video")).toHaveAttribute("src", "/avatars/code.mp4");
  await expect(pane.getByText("測試開發")).toBeVisible();
  await expect(page.getByRole("navigation", { name: "討論串" })).toHaveCount(0);
  await expect(pane.getByText("Builder 手上的事")).toBeVisible();
  const row = pane.getByRole("link", { name: new RegExp(`要你驗收.*${title}`) });
  await expect(row).toBeVisible();
  await expect(row).toContainText("要你驗收");
  await expect(row).toContainText(id);

  await row.click();
  await expect.poll(() => new URL(page.url()).pathname).toBe(`/tasks/${id}`);
  await page.goBack();

  // 「對話清單」：清單出現、角色欄消失；重新整理後維持清單模式。
  const toggle = page.getByRole("button", { name: "對話清單" });
  await expect(toggle).toHaveAttribute("aria-pressed", "false");
  await toggle.click();
  await expect(page.getByRole("navigation", { name: "討論串" })).toBeVisible();
  await expect(pane).toHaveCount(0);
  await expect(toggle).toHaveAttribute("aria-pressed", "true");
  await page.reload();
  await expect(page.getByRole("navigation", { name: "討論串" })).toBeVisible();
  await expect(pane).toHaveCount(0);
  expect(await page.evaluate(() => localStorage.getItem("eg.chatPane.v1"))).toBe("list");

  // 再按一次回到角色欄。
  await page.getByRole("button", { name: "對話清單" }).click();
  await expect(pane).toBeVisible();
  await expect(page.getByRole("navigation", { name: "討論串" })).toHaveCount(0);
});

// WEB-92：對話裡有確認卡在等你時，角色欄的「她手上的事」不能寫「目前沒有等你處理的事」；回答完才回到原樣。
test("TestPersonaPaneShowsChatWaiting", async ({ page, isMobile }, info) => {
  info.skip(isMobile, "桌機版的右側角色欄");
  await page.goto("/chat/life");
  const pane = page.getByRole("complementary", { name: "Helper 的角色欄" });
  await expect(pane).toBeVisible();
  await page.locator("#hg-composer").fill(`【權限】角色欄 ${rand()}`);
  await page.locator('button[aria-label="送出訊息"]').click();
  await expect(page.getByRole("button", { name: "允許" })).toBeVisible({ timeout: 15_000 });
  await expect(pane.getByTestId("pane-chat-waiting")).toContainText("要你回答");
  await expect(pane.getByText("目前沒有等你處理的事。")).toHaveCount(0);
  await page.getByRole("button", { name: "允許" }).click();
  await expect(chatLog(page).getByText("已允許").last()).toBeVisible();
  await expect(pane.getByTestId("pane-chat-waiting")).toHaveCount(0);
});

// Guards: REQ-156
test("TestPersonaPaneImageOnly", async ({ page, isMobile }, info) => {
  info.skip(isMobile, "桌機版的右側角色欄");
  // life 沒有影片：角色欄只放圖。
  await page.goto("/chat/life");
  const pane = page.getByRole("complementary", { name: "Helper 的角色欄" });
  await expect(pane).toBeVisible();
  await expect(pane.locator("video")).toHaveCount(0);
  await expect(pane.locator('[data-persona-media="image"] img')).toHaveAttribute(
    "src",
    "/avatars/life.png",
  );
});

// Guards: REQ-156
test("TestPersonaReducedMotion", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/chat/code");
  const media = page.locator("[data-persona-media]").first();
  await expect(media).toHaveAttribute("data-persona-media", "image");
  await expect(page.locator("[data-persona-media] video")).toHaveCount(0);
  await expect(media.locator("img")).toHaveAttribute("src", "/avatars/code.png");
});

// Guards: REQ-156
test("TestPersonaMessageAvatar", async ({ page, isMobile }) => {
  const word = `頭像 ${rand()}`;
  await page.goto("/chat/code");
  await say(page, word);
  const log = chatLog(page);
  await expect(log.getByText(`收到：${word}`)).toBeVisible();
  const rows = log.locator(":scope > div");
  const last = rows.last();
  // 最後一則秘書的話左邊有她的頭像（手機 30、桌機 32）；主人自己的話旁邊沒有。
  const avatar = last.locator("span.hg-agent");
  await expect(avatar).toHaveCount(1);
  const box = await avatar.boundingBox();
  expect(Math.round(box?.width ?? 0)).toBe(isMobile ? 30 : 32);
  await expect(avatar.locator("img")).toHaveAttribute("src", "/avatars/code.png");
  // 她在這句話之前先用了工具：同一段連續的訊息，只有最後一則放頭像，工具膠囊那一列留同寬的空白。
  const pill = rows.filter({ has: page.getByRole("button", { name: /用了 \d+ 個工具/ }) }).last();
  await expect(pill.locator("span.hg-agent")).toHaveCount(0);
  const mine = rows.filter({ hasText: word }).filter({ hasNotText: "收到：" }).last();
  await expect(mine.locator("span.hg-agent")).toHaveCount(0);
  // 對齊：工具膠囊與最後一則的內容左邊緣在同一條線上。
  const pillLeft = (await pill.getByRole("button").boundingBox())?.x ?? -1;
  const bubbleLeft = (await last.getByTestId("assistant-bubble").boundingBox())?.x ?? -2;
  expect(Math.abs(pillLeft - bubbleLeft)).toBeLessThan(1);
});

// Guards: REQ-156
test("TestShareVideoPlays", async ({ page, request, seed }) => {
  const dir = mkdtempSync(path.join(tmpdir(), "hg-share-"));
  try {
    const mp4 = path.join(dir, `片 ${rand()}.mp4`);
    copyFileSync(path.join(fixtures, "persona.mp4"), mp4);
    const shared = await seed.api<{ url: string }>("/api/notify/share", {
      path: mp4,
      agent: "code",
    });
    expect(shared.url).toMatch(/^\/share\/.*\.mp4$/);

    // 伺服器：inline、video/mp4、支援 Range（iOS Safari 一定要）。
    const url = encodeURI(shared.url);
    const ranged = await request.get(url, { headers: { Range: "bytes=0-9" } });
    expect(ranged.status()).toBe(206);
    expect(ranged.headers()["content-type"]).toBe("video/mp4");
    expect(ranged.headers()["content-disposition"]).toBe("inline");
    expect(ranged.headers()["content-range"]).toMatch(/^bytes 0-9\//);

    // 畫面：聊天裡的連結直接變成可以播的 <video>，下面一行小字是原檔名。
    await page.goto("/chat/code");
    await say(page, `影片：![片](${url})`);
    const video = chatLog(page).locator("video").last();
    await expect(video).toHaveAttribute("src", url);
    await expect(video).toHaveAttribute("controls", "");
    await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.videoWidth)).toBe(256);
    // 檔名點了在這頁放大看（不開新分頁，FEN-30）；✕ 關掉回到聊天。
    await chatLog(page)
      .getByRole("button", { name: /片.*\.mp4$/ })
      .last()
      .click();
    await expect(page.getByTestId("media-viewer-video")).toHaveAttribute("src", url);
    await page.getByTestId("media-viewer-close").click();
    await expect(page.getByTestId("media-viewer")).toHaveCount(0);
    await expect(page).toHaveURL(/\/chat\/code/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
