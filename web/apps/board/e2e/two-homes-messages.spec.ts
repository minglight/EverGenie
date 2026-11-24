import { expect, twoHomesTest as test } from "./fixtures";

// S3-7a：今天頁與訊息頁合併兩台。瀏覽器開 A 的看板，B 的訊息也在清單裡、標 B 的台名；
// 在 A 回答 B 的那則，答案送到 B（B 那邊變已回）；B 連不上時 A 自己的訊息照常，只在清單上面說 B 那一台。
// 只跑桌機（不列進 playwright.config.ts 的 mobileSpecs）。

const rand = () => Math.random().toString(36).slice(2, 8);

interface Notice {
  id: string;
  state: string;
  answer?: { text: string; via: string };
}

test("TestAnswerOtherHomesMessageFromThisBoard", async ({ page, homes }) => {
  const title = `B 要你同意 ${rand()}`;
  const id = await homes.b.seed.approve(title);
  const bTag = `[data-site="${homes.b.label}"]`;

  // 今天頁（桌機寬版的清單）看得到 B 的那一列，標著 B；別台的編號帶台名，不會跟 A 的同號撞。
  await page.goto("/");
  const card = page.locator(`[data-id="${homes.b.label}/${id}"]`);
  await expect(card).toBeVisible();
  await expect(card.locator(bTag)).toBeVisible();

  // 訊息頁：B 那則在清單裡、標 B；點進去網址帶 ?home=B，重新整理還在。
  await page.goto("/messages");
  const row = page.locator("li", { hasText: title });
  await expect(row).toBeVisible();
  await expect(row.locator(bTag)).toBeVisible();
  await row.getByRole("link").first().click();
  await expect(page).toHaveURL(new RegExp(`/messages/${id}\\?.*home=${homes.b.label}`));
  await page.reload();
  await expect(page.getByRole("heading", { name: title })).toBeVisible();

  await page.getByRole("button", { name: "同意", exact: true }).click();
  await expect(page.getByText("已回：同意")).toBeVisible();
  // 答案送到 B：B 那則變成已回，答案是按的那個選項。
  await expect
    .poll(
      async () =>
        (await homes.b.seed.api<{ notification: Notice }>("/api/notify/get", { id })).notification
          .state,
    )
    .toBe("answered");
  const n = (await homes.b.seed.api<{ notification: Notice }>("/api/notify/get", { id }))
    .notification;
  expect(n.answer?.text).toBe("同意");
});

test("TestOwnMessagesStayWhenOtherHomeIsDown", async ({ page, homes, seed }) => {
  const mine = `A 自己的 ${rand()}`;
  const theirs = `B 的 ${rand()}`;
  await seed.approve(mine);
  await homes.b.seed.approve(theirs);
  homes.b.proxy.mode = "reset";

  await page.goto("/messages");
  await expect(page.locator("li", { hasText: mine })).toBeVisible();
  await expect(page.locator("li", { hasText: theirs })).toHaveCount(0);
  // 只在清單上面說 B 那一台連不上（側欄的秘書篩選 B 那段也有一行，不算在頁面裡）。
  const problem = page.getByRole("main").getByTestId("site-notice");
  await expect(problem).toHaveCount(1);
  await expect(problem).toHaveAttribute("data-state", "down");

  // 今天頁也一樣：A 的照常，B 只有一行提示。
  await page.goto("/");
  await expect(page.getByText(mine).first()).toBeVisible();
  await expect(page.getByText(theirs)).toHaveCount(0);
  await expect(page.getByRole("main").getByTestId("site-notice")).toHaveCount(1);
});
