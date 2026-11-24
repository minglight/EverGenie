import type { Page } from "@playwright/test";
import { emptyHomeTest, expect, shot, test } from "./fixtures";

// 新家第一次打開的歡迎頁（WEB-176，DEV-156 U-03）。emptyHomeTest 的家名冊是空的（自己一個 worker），
// 測試照順序共用這個家：先看歡迎頁擋住每一頁，再走完流程請一位秘書；之後這個家就不再出現歡迎頁。

const welcome = (page: Page) => page.getByTestId("welcome");
const mainNav = (page: Page) => page.getByRole("navigation", { name: "主選單" });

async function expectStep(page: Page, step: string) {
  await expect(welcome(page)).toHaveAttribute("data-step", step);
}

// 沒有水平捲動、可點的卡片與按鈕至少 44px（UXC-5）。
async function expectFits(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
  for (const el of await welcome(page).locator("a, button").all()) {
    const box = await el.boundingBox();
    expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
  }
}

// 名冊空的家：打開任何一頁都先到全螢幕的歡迎頁——沒有主選單、沒有設定分頁、沒有頁首。
// Guards: WEB-176
emptyHomeTest("TestEmptyHomeOpensWelcomeOnEveryPage @smoke", async ({ page }) => {
  for (const url of ["/", "/chat", "/settings/setup", "/reminders"]) {
    await page.goto(url);
    await expect(page).toHaveURL(/\/home\/welcome$/);
    await expectStep(page, "intro");
  }
  await expect(welcome(page).getByRole("heading", { level: 1 })).toContainText("歡迎");
  await expect(welcome(page)).toContainText("這是你的秘書們。先請一位開始，其他的之後再加。");
  await expect(mainNav(page)).toHaveCount(0);
  await expect(page.getByRole("navigation", { name: "設定分頁" })).toHaveCount(0);
  await expect(page.getByTestId("setup-step-agents")).toHaveCount(0);
  await expect(page).toHaveTitle(/歡迎/);
  await expectFits(page);
});

// 歡迎頁四步在 320 寬都不左右捲（FEN-7）、每一步剛好一個 h1（FEN-31）、可點的至少 44px（UXC-5）。
// shell.spec 的 TestNoHorizontalScrollAt320／TestOneH1PerPage 用的是有秘書的家，看不到歡迎頁，所以在名冊空的這個家另外驗；
// 排在請秘書那一支前面（請了之後歡迎頁就不再出現）。
// Guards: FEN-7, FEN-31, WEB-176
emptyHomeTest("TestWelcomeFitsAt320WithOneH1", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 640 });
  const h1 = page.getByRole("heading", { level: 1 });
  const check = async (step: string) => {
    await expectStep(page, step);
    await expect(h1, `歡迎頁 ${step} 要剛好一個 h1`).toHaveCount(1);
    await page.evaluate(() => document.fonts.ready);
    const wide = await page.evaluate(() =>
      Math.max(document.documentElement.scrollWidth, document.body.scrollWidth),
    );
    expect(wide, `歡迎頁 ${step} 在 320 寬不能左右捲`).toBeLessThanOrEqual(320);
    await expectFits(page);
  };
  await page.goto("/");
  await check("intro");
  // 正式的家、只看這一台：歡迎頁不畫家名徽章（跟看板的 HomeLabel 同一條規則）。
  await expect(welcome(page).getByTestId("home-label")).toHaveCount(0);
  await page.getByTestId("welcome-begin").click();
  await check("job");
  await page.locator('[data-testid="welcome-job"][data-job="health"]').click();
  await check("persona");
  await page.getByTestId("welcome-persona").first().click();
  await check("start");
});

// DEV-161：職位有可選知識，就在最後一步（請她開始上班）的頭像下面，預設勾好、不多一步、不左右捲。
// 核心的 /api/agents/knowledge 先用假的回應（B 的後端合進來後，再改接真的，見 knowledge.spec.ts 的 fixme）。
// 排在請秘書那一支前面（請了之後歡迎頁就不再出現）；這裡不按「開始上班」，送出時帶哪些知識由 Welcome.spec.tsx 驗。
// Guards: WEB-176
emptyHomeTest("TestWelcomeShowsOptionalKnowledge", async ({ page }, info) => {
  await page.route("**/api/agents/knowledge", async (route) => {
    await route.fulfill({
      json: {
        agent: "life",
        blocks: [
          {
            id: "parenting",
            title: "育兒",
            summary: "依孩子的年齡陪你想",
            default: true,
            enabled: false,
          },
          {
            id: "balance",
            title: "工作與生活的平衡",
            summary: "工作吃掉家庭、睡眠與自己的時間時，提醒一句",
            default: false,
            enabled: false,
          },
        ],
      },
    });
  });
  await page.goto("/");
  await page.getByTestId("welcome-begin").click();
  await page.locator('[data-testid="welcome-job"][data-job="life"]').click();
  await page.getByTestId("welcome-persona").first().click();
  await expectStep(page, "start");
  const picker = page.getByTestId("knowledge");
  await expect(picker).toBeVisible();
  await expect(picker).toContainText("她帶的知識");
  await expect(picker).toContainText("基本的一定帶；可選的勾了她才會用。");
  const parenting = picker.locator('[data-block="parenting"]');
  const balance = picker.locator('[data-block="balance"]');
  await expect(parenting).toBeChecked();
  await expect(balance).not.toBeChecked();
  await balance.check();
  await expect(balance).toBeChecked();
  // 還是第 4 步、還是只有一顆主要按鈕；每個勾選列至少 44px、頁面不左右捲。
  await expect(page.getByTestId("welcome-progress")).toHaveText("第 4 步，共 4 步");
  await expect(page.getByTestId("welcome-hire")).toBeVisible();
  for (const row of await picker.locator("label").all()) {
    expect((await row.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(44);
  }
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
  await shot(page, `welcome-knowledge-${info.project.name}`);
});

// 走完流程：開始 → 選健康 → 選角色 → 請她開始上班 → 今天頁。步驟寫在網址：重新整理、上一頁、「上一步」都回到對的那一步。
// 畫面上不出現職位代號（health、life…）；寫程式、電腦不在這裡出現。
// Guards: WEB-176, WEB-47, UXC-3
emptyHomeTest("TestWelcomeHiresFirstSecretary @smoke", async ({ page, request }) => {
  await page.goto("/");
  await expectStep(page, "intro");
  await page.getByTestId("welcome-begin").click();

  await expectStep(page, "job");
  await expect(page).toHaveURL(/\/home\/welcome\?step=job$/);
  const jobs = page.getByTestId("welcome-job");
  await expect(jobs).toHaveCount(3);
  await expect(jobs.nth(0)).toContainText("生活");
  await expect(jobs.nth(1)).toContainText("健康");
  await expect(jobs.nth(2)).toContainText("工作");
  const jobText = (await welcome(page).textContent()) ?? "";
  for (const code of ["health", "life", "work", "code", "system", "職位"]) {
    expect(jobText).not.toContain(code);
  }
  await expectFits(page);
  // 重新整理留在同一步。
  await page.reload();
  await expectStep(page, "job");

  await page.locator('[data-testid="welcome-job"][data-job="health"]').click();
  await expectStep(page, "persona");
  await expect(welcome(page).getByRole("heading", { level: 1 })).toHaveText(
    "請誰來當你的健康秘書？",
  );
  // 角色是卡片（頭像＋名字），不是下拉。
  await expect(welcome(page).locator("select")).toHaveCount(0);
  const personas = page.getByTestId("welcome-persona");
  await expect(personas.first()).toBeVisible();
  await expect(personas.first().getByRole("img")).toBeVisible();
  await expectFits(page);

  // 瀏覽器的上一頁回到選職位；畫面上的「上一步」也一樣。
  await page.goBack();
  await expectStep(page, "job");
  await page.goForward();
  await expectStep(page, "persona");
  await page.getByTestId("welcome-back").click();
  await expectStep(page, "job");
  await page.locator('[data-testid="welcome-job"][data-job="health"]').click();
  await expectStep(page, "persona");

  const slug = (await personas.first().getAttribute("data-slug")) ?? "";
  const name = (
    (await personas.first().getByTestId("welcome-persona-name").textContent()) ?? ""
  ).trim();
  expect(slug).not.toBe("");
  await personas.first().click();
  await expectStep(page, "start");
  await expect(page).toHaveURL(new RegExp(`step=start&job=health&persona=${slug}$`));
  // 重新整理還在最後一步、還是同一位。
  await page.reload();
  await expectStep(page, "start");
  const hire = page.getByTestId("welcome-hire");
  await expect(hire).toHaveText(`請${name}開始上班`);
  await expectFits(page);
  await hire.click();

  // 請好了：到今天頁，看板的主選單回來了。
  await expect(page).toHaveURL(/\/$/);
  await expect(welcome(page)).toHaveCount(0);
  await expect(mainNav(page).first()).toBeVisible();
  // 名冊上多了啟用中的健康秘書，由剛才選的角色擔任（跟設定頁「請〈角色名字〉上班」同一個結果）。
  const res = await request.post("/api/web/get_agents", { data: {} });
  const body = (await res.json()) as {
    agents?: { id: string; persona: string; enabled: boolean }[];
  };
  expect(body.agents).toEqual([
    expect.objectContaining({ id: "health", persona: slug, enabled: true }),
  ]);

  // 之後不再出現：重新整理今天頁照常；直接開歡迎頁的網址會被帶回今天。
  await page.reload();
  await expect(mainNav(page).first()).toBeVisible();
  await expect(welcome(page)).toHaveCount(0);
  await page.goto("/home/welcome?step=job");
  await expect(page).toHaveURL(/\/$/);
  await expect(welcome(page)).toHaveCount(0);
});

// 已經有秘書的家（升級上來的舊家）：打開任何一頁都不出現歡迎頁；歡迎頁的網址帶回今天。
// Guards: WEB-176
test("TestHomeWithSecretariesNeverSeesWelcome", async ({ page }) => {
  await page.goto("/");
  await expect(mainNav(page).first()).toBeVisible();
  await expect(welcome(page)).toHaveCount(0);
  await expect(page).toHaveURL(/\/$/);
  await page.goto("/home/welcome");
  await expect(mainNav(page).first()).toBeVisible();
  await expect(page).toHaveURL(/\/$/);
  await expect(welcome(page)).toHaveCount(0);
});
