import type { Page } from "@playwright/test";
import { expect, twoHomesTest as test } from "./fixtures";

// 秘書篩選「只看這位」連兩台（S3 收尾；作者 2026-11-02：兩台同一個職位合成一個頭像、一次選一位）。
// 家 A 的看板 [[web.sites]] 有家 B：兩個家的 fixture 一樣（秘書同名同職位），所以每位只有一個頭像、key 兩台都有；
// 點她＝兩台的她都看得到；B 不在時頭像下面寫一行灰字，A 照常。資料帶亂數（TST-13）。
// 只跑桌機（側欄；手機的小卡列由 vitest AgentFilterBar.sites.spec 守，不列進 mobileSpecs）。

const rand = () => Math.random().toString(36).slice(2, 8);

const filterRegion = (page: Page) => page.getByRole("region", { name: "秘書篩選" });
const chipOf = (page: Page, key: string) => filterRegion(page).locator(`[data-agent="${key}"]`);
const rowOf = (page: Page, title: string) =>
  page.getByRole("list", { name: "任務單清單" }).getByRole("listitem").filter({ hasText: title });

test("TestAgentFilterMergesSameFeatureAcrossHomes", async ({ page, homes }) => {
  await page.goto("/");
  const code = chipOf(page, "code");
  // 同一位秘書在兩台只有一個頭像：key 是 A 的，兩台的 key 都在 data-agents，讀屏名字寫兩台。
  await expect(code).toHaveAttribute("data-agents", `code ${homes.b.label}/code`);
  await expect(code).toHaveAttribute(
    "aria-label",
    new RegExp(`，在 ${homes.a.label}、${homes.b.label}$`),
  );
  await expect(chipOf(page, `${homes.b.label}/code`)).toHaveCount(0);
  await expect(filterRegion(page).getByTestId("site-notice")).toHaveCount(0);
});

// 今天的「你的秘書們」卡片也一樣：同一位在兩台只有一張（件數加總、不標台名），點了進她的首頁。
test("TestTodayStripMergesSameFeatureAcrossHomes", async ({ page }) => {
  await page.goto("/");
  const strip = page.getByRole("region", { name: "你的秘書們" });
  const code = strip.locator('[data-agent="code"]');
  await expect(code).toHaveAttribute("data-sites", "2");
  await expect(strip.locator('[data-agent$="/code"]')).toHaveCount(0);
  await expect(code.getByTestId("site-tag")).toHaveCount(0);
  // 狀態一台一行、各寫各的（不合成「比較要緊」的那一個）。
  await expect(code.locator("[data-presence]")).toHaveCount(2);
  await expect(code.locator('[data-site=""][data-presence]')).toBeVisible();
  await expect(code.locator('[data-site]:not([data-site=""])[data-presence]')).toBeVisible();
  await code.click();
  await expect(chipOf(page, "code")).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("region", { name: "你的秘書們" })).toHaveCount(0);
});

test("TestAgentFilterPickShowsHerOnBothHomes", async ({ page, homes, seed }) => {
  const tag = rand();
  const mine = `A 的 code ${tag}`;
  const theirs = `B 的 code ${tag}`;
  const theirLife = `B 的 life ${tag}`;
  await seed.doingTask(mine, "code");
  await homes.b.seed.doingTask(theirs, "code");
  await homes.b.seed.doingTask(theirLife, "life");

  await page.goto("/tasks?view=active");
  await expect(rowOf(page, mine)).toBeVisible();
  await expect(rowOf(page, theirs)).toBeVisible();
  const code = chipOf(page, "code");
  // 件數是兩台加起來（這一頁兩台的 code 至少各有剛開的那張）。
  await expect.poll(async () => Number(await code.getAttribute("data-count"))).toBeGreaterThan(1);

  await code.click();
  await expect(code).toHaveAttribute("aria-pressed", "true");
  // 任務單剩兩台的 code，B 的 life 不見。
  await expect(rowOf(page, theirs)).toBeVisible();
  await expect(rowOf(page, mine)).toBeVisible();
  await expect(rowOf(page, theirLife)).toHaveCount(0);

  // 今天變成她的首頁：「她手上的事」兩台的單都列。
  await page.goto("/");
  await expect(page.getByText(mine)).toBeVisible();
  await expect(page.getByText(theirs)).toBeVisible();
  await expect(page.getByText(theirLife)).toHaveCount(0);
  // 兩台的單編號可能一樣：每列標台名才分得出來。
  await expect(
    page.getByRole("link").filter({ hasText: theirs }).getByTestId("site-tag"),
  ).toHaveText(homes.b.label);

  // 聊天清單也是兩台的她（同一份選擇），進去之後照舊選哪一台。
  await page.addInitScript(() => {
    try {
      localStorage.setItem("eg.chatPane.v1", "list");
    } catch {
      // 沒有 localStorage 就用預設
    }
  });
  await page.goto("/chat");
  await expect(
    page.locator(`[data-thread="code"][href="/chat/code?home=${homes.b.label}"]`),
  ).toBeVisible();
  await expect(page.locator('[data-thread="code"][href="/chat/code"]')).toBeVisible();
  await expect(page.locator('[data-thread="life"]')).toHaveCount(0);
  // 重新整理後還記得；再點一次回到全部。
  await page.reload();
  await expect(chipOf(page, "code")).toHaveAttribute("aria-pressed", "true");
  await chipOf(page, "code").click();
  await expect(chipOf(page, "code")).toHaveAttribute("aria-pressed", "false");
  await expect(page.locator('[data-thread="life"]').first()).toBeVisible();
});

test("TestAgentFilterHomeBDownShowsNotice", async ({ page, homes }) => {
  homes.b.proxy.mode = "reset";
  await page.goto("/");
  // B 整台不在、這個分頁也沒連上過它：秘書列只放一格小標記（整句話收在讀屏名稱），頭像只剩 A 的。
  const mark = filterRegion(page).getByTestId("filter-site-notice");
  await expect(mark).toHaveText("有一台連不上", { timeout: 15_000 });
  await expect(mark).toHaveAttribute("aria-label", /現在連不到/);
  // 同一句提示整頁只出現一次（在內文），秘書列不再放第二次（DEV-156 U-23）。
  await expect(filterRegion(page).getByTestId("site-notice")).toHaveCount(0);
  await expect(page.getByTestId("site-notice")).toHaveCount(1);
  await expect(page.getByTestId("site-notice")).toContainText("現在連不到");
  await expect(chipOf(page, "code")).toHaveAttribute("data-agents", "code");
});

// 快速輸入的「@秘書」對得到 B 的秘書：建議清單兩台都有、標台名；點 B 那位，話送到 B（不送 A）。
test("TestQuickSayToHomeBSecretaryGoesToB", async ({ page, homes }) => {
  const word = `快速跨台 ${rand()}`;
  await page.goto("/");
  const box = page.getByLabel("快速輸入");
  await box.fill("@Build");
  const suggest = page.getByRole("list", { name: "選一位秘書" });
  await suggest.getByRole("button", { name: `Builder（${homes.b.label}）` }).click();
  await expect(box).toHaveValue(`@${homes.b.label}/code `);
  await box.fill(`@${homes.b.label}/code ${word}`);
  await expect(page.getByTestId("quick-preview").getByTestId("site-tag")).toHaveText(homes.b.label);
  const sent = page.waitForRequest(
    (r) => r.url().startsWith(`${homes.b.url}/api/bus/send`) && (r.postData() ?? "").includes(word),
  );
  // 主伺服器（A）一封都不能收到：不退回主伺服器送。
  const onA: string[] = [];
  page.on("request", (r) => {
    if (r.url().includes("/api/bus/send") && !r.url().startsWith(homes.b.url)) onA.push(r.url());
  });
  await box.press("Enter");
  const body = JSON.parse((await sent).postData() ?? "{}") as { to?: string };
  expect(body.to).toBe("code");
  await expect(page.getByText("已傳給 Builder")).toBeVisible();
  expect(onA).toEqual([]);
});

// 換秘書的浮窗也照電腦分段：B 不在時 B 那段還在、寫一行灰字（以前整段不見）。
test("TestChatSwitcherHomeBDownShowsNotice", async ({ page, homes }) => {
  homes.b.proxy.mode = "reset";
  await page.goto("/chat/code");
  await page.getByRole("button", { name: /^換一位秘書/ }).click();
  const pop = page.getByRole("dialog", { name: "換一位秘書" });
  const host = new URL(homes.b.url).host;
  await expect(pop.getByRole("region", { name: host }).getByTestId("site-notice")).toContainText(
    "現在連不到",
    { timeout: 15_000 },
  );
  await expect(
    pop.getByRole("region", { name: homes.a.label }).locator('[data-agent="code"]'),
  ).toBeVisible();
});
