import type { Page } from "@playwright/test";
import { expect, freshHomeTest as test } from "./fixtures";

// 一位秘書那一側（引擎）卡住，其他秘書照常收話、回話（SES-72，DEV-159）。
// 自己一個家（freshHomeTest）：假引擎收到「【這位不回】」之後，code 這個 session 的每個呼叫都卡著，
// 這個家的 code 之後都是卡住的，不能跟別的 spec 共用。

const rand = () => Math.random().toString(36).slice(2, 8);
const chatLog = (page: Page) => page.getByRole("log", { name: "對話" });
// 「幾秒內」的上限：比核心等引擎的任何一個時限（最短的貼上 20 秒）都短，等卡住的那位等到時限才收到的不算過。
const SOON = 8_000;

async function say(page: Page, text: string) {
  await page.locator("#hg-composer").fill(text);
  await page.locator('button[aria-label="送出訊息"]').click();
}

test("TestOtherSecretaryAnswersWhileOneIsStuck", async ({ page, seed }) => {
  test.setTimeout(90_000);
  // code 先開始一輪慢的（3 秒），做到一半主人插一句「【這位不回】」：從這一句起她的引擎不回——
  // 這一句的貼上卡著，她那一輪結束後核心要讀她的對話記錄算未讀，也卡著。
  // （只送一句不夠：修正前定時那一輪還能繞過卡住的那一則把別人的話送出去；要連讀對話記錄也卡住，別人才會真的被拖住。）
  const send = (body: string) =>
    seed.api("/api/bus/send", { from: "owner(web)", to: "code", kind: "chat", body });
  await send(`【慢】卡住前的那一輪 ${rand()}`);
  await page.waitForTimeout(1_000);
  await send("【這位不回】");
  await page.waitForTimeout(5_000); // 等那一輪結束，讓核心真的卡在跟她引擎的呼叫上

  // 對另一位秘書說話：她幾秒內收到並回覆。
  const word = `一位卡住時給另一位的話 ${rand()}`;
  await page.goto("/chat/life");
  await say(page, word);
  await expect(chatLog(page).getByText(`收到：${word}`)).toBeVisible({ timeout: SOON });

  // 再說一句也一樣（不是只有第一句剛好排在前面）。
  const again = `再一句 ${rand()}`;
  await say(page, again);
  await expect(chatLog(page).getByText(`收到：${again}`)).toBeVisible({ timeout: SOON });

  // 卡住的那位還是卡著：她那一句沒有回覆。
  await page.goto("/chat/code");
  await expect(chatLog(page).getByText("收到：【這位不回】")).toHaveCount(0);
});
