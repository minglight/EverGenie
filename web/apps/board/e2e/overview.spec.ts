import type { Page } from "@playwright/test";
import { expect, test } from "./fixtures";

const mainNav = (page: Page) => page.getByRole("navigation", { name: "主選單" });

// Guards: REQ-109
test("TestOverviewBadges @smoke", async ({ page, seed, isMobile }) => {
  const before = await seed.waitingTotal();
  const title = `徽章測試 ${Date.now()}`;
  const id = await seed.approve(title);
  const want = before + 1;
  expect(await seed.waitingTotal()).toBe(want);

  await page.goto("/");
  // 選單上「今天」的徽章，與頁面上的數字，都是伺服器算的同一個數字。
  const today = mainNav(page).getByRole("link", { name: /^今天/ });
  await expect(today).toContainText(String(want));
  if (isMobile) {
    await expect(page.getByText(`${want} 件等你處理`)).toBeVisible();
  } else {
    await expect(page.getByRole("heading", { name: "等你處理" })).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "等你處理" }).locator("xpath=following-sibling::span"),
    ).toHaveText(String(want));
  }
  // 這則通知出現在清單，旁邊有編號和白話的分類。
  await expect(page.getByText(title).first()).toBeVisible();
  await expect(page.getByText(id, { exact: true }).first()).toBeVisible();
  await expect(page.getByText("要你同意").first()).toBeVisible();
});

// Guards: REQ-109
test("TestOverviewLiveUpdate", async ({ page, seed }) => {
  await page.goto("/");
  await expect(mainNav(page)).toBeVisible();
  const before = await seed.waitingTotal();
  const title = `即時更新 ${Date.now()}`;
  await seed.approve(title);
  // 頁面開著，不用重新整理：通知的串流一有事件，選單徽章與清單就跟著更新。
  const today = mainNav(page).getByRole("link", { name: /^今天/ });
  await expect(today).toContainText(String(before + 1), { timeout: 15_000 });
  await expect(page.getByText(title).first()).toBeVisible();
});

// Guards: REQ-109
test("TestOverviewTaskWaiting", async ({ page, seed, isMobile }) => {
  const title = `等你驗收 ${Date.now()}`;
  const id = await seed.reviewTask(title);
  await page.goto("/");
  await expect(page.getByText(title).first()).toBeVisible();
  await expect(page.getByText("要你驗收").first()).toBeVisible();
  // 點進去到任務單詳細頁（桌機寬版先選取，再按「打開完整頁面」）。
  if (isMobile) {
    await page.locator(`a[href="/tasks/${id}"]`).click();
  } else {
    await page.locator(`[data-id="${id}"]`).click();
    await page.getByRole("link", { name: "打開完整頁面" }).click();
  }
  await expect.poll(() => new URL(page.url()).pathname).toBe(`/tasks/${id}`);
});

// Guards: REQ-109
test("TestWideTodaySelectionInURL", async ({ page, seed, isMobile }, info) => {
  info.skip(isMobile, "兩欄的今天只在桌機寬度");
  const a = `選取甲 ${Date.now()}`;
  const b = `選取乙 ${Date.now()}`;
  const idA = await seed.approve(a);
  const idB = await seed.approve(b);
  await page.goto("/");
  await expect(page.getByText("選左邊一件事來看細節")).toBeVisible();
  await page.getByRole("button", { name: new RegExp(a) }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get("sel")).toBe(idA);
  await expect(page.getByRole("heading", { name: a })).toBeVisible();
  // 快速鍵說明收在「？」裡（DEV-156 U-32）：平常不佔一排，按了才看到。
  await expect(page.getByText("同意／驗收")).toHaveCount(0);
  await page.getByTestId("today-keys-help").click();
  await expect(page.getByRole("dialog", { name: "快速鍵" })).toContainText("同意／驗收");
  await page.keyboard.press("Escape");
  // J／K 移動選取；重新整理後還在。
  await page.getByRole("group", { name: /J K/ }).focus();
  await page.keyboard.press("j");
  await expect.poll(() => new URL(page.url()).searchParams.get("sel")).not.toBe(idA);
  await page.reload();
  const sel = new URL(page.url()).searchParams.get("sel");
  expect(sel).toBeTruthy();
  // 篩選：驗收只剩任務單，同意只剩通知。
  // 只在篩選那一組裡找：選取的若剛好是通知，右欄的動作列也有一顆「同意 A」。
  await page
    .getByRole("group", { name: "篩選等你處理的事" })
    .getByRole("button", { name: /^同意/ })
    .click();
  await expect.poll(() => new URL(page.url()).searchParams.get("filter")).toBe("approve");
  await expect(page.getByRole("button", { name: new RegExp(idB) })).toBeVisible();
});

// Guards: REQ-234
test("TestTodayStartersAndOneColumn", async ({ page, seed, isMobile }) => {
  await seed.approve(`一欄測試 ${Date.now()}`);
  await page.goto("/");
  // 輸入框底下三個入口：問秘書先填「@」並跳出秘書清單。
  const starters = page.getByTestId("quick-starters");
  await expect(starters.getByRole("button")).toHaveText(["記待辦", "交代任務", "問秘書"]);
  await starters.getByRole("button", { name: "問秘書" }).click();
  await expect(page.getByRole("textbox", { name: "快速輸入" })).toHaveValue("@");
  await expect(page.getByRole("list", { name: /秘書/ })).toBeVisible();
  // 等畫面把焦點和游標放好（下一個畫格才做）再清空：fill("") 是全選後按 Delete，
  // 游標在中間被移到字尾的話，Delete 刪不到東西，「@」會留著。
  await expect(page.getByRole("textbox", { name: "快速輸入" })).toBeFocused();
  await page.getByRole("textbox", { name: "快速輸入" }).fill("");
  // 記待辦：給一句例子。
  await starters.getByRole("button", { name: "記待辦" }).click();
  await expect(page.getByText("直接打一句就記成待辦")).toBeVisible();
  // 交代任務：到任務單頁，開單的輸入框已經聚焦。
  await starters.getByRole("button", { name: "交代任務" }).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/tasks");
  await expect(page.locator("input:focus")).toHaveCount(1);
  if (isMobile) return;

  // 桌機：有事等你、看全部＝兩欄；只看一位＝一欄（她的首頁、等你處理的卡片）。
  await page.goto("/");
  await expect(page.getByRole("group", { name: /J K/ })).toBeVisible();
  await expect(page.getByTestId("today-one-column")).toHaveCount(0);
  const region = page.getByRole("region", { name: "秘書篩選" });
  await region.getByRole("button").first().click();
  await expect(page.getByTestId("today-one-column")).toBeVisible();
  await expect(page.getByRole("group", { name: /J K/ })).toHaveCount(0);
});

// 一件事一張卡（TSK-26）：等你驗收、等你決定的單，核心另外推的「請驗收／等你決定」通知不在今天頁再列一張，
// 等你處理的數字也只加 2。
test("TestOverviewWaitingTaskShownOnce", async ({ page, seed, isMobile }, info) => {
  info.skip(isMobile, "清單一列一顆按鈕是桌機的兩欄版");
  const before = await seed.waitingTotal();
  const review = `只列一次驗收 ${Date.now()}`;
  const ask = `只列一次決定 ${Date.now()}`;
  await seed.reviewTask(review);
  await seed.askTask(ask, "要 A 還是 B？", "life");
  expect(await seed.waitingTotal()).toBe(before + 2);
  await page.goto("/");
  const list = page.getByRole("group", { name: /J K/ });
  await expect(list.getByRole("button", { name: new RegExp(review) })).toHaveCount(1);
  await expect(list.getByRole("button", { name: new RegExp(ask) })).toHaveCount(1);
  await expect(mainNav(page).getByRole("link", { name: /^今天/ })).toContainText(
    String(before + 2),
  );
});

// 今天的待辦：前幾天到期還沒做的排在最前面、日期標紅；圓圈直接按做完（RMD-18，2026-11-02 稽核補回舊看板的做法）。
test("TestTodayRemindersShowOverdueAndMarkDone", async ({ page, seed }) => {
  const word = `逾期 ${Date.now()}`;
  await seed.reminder(word, new Date(Date.now() - 2 * 24 * 3600_000));
  await page.goto("/");
  const section = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "今天的待辦" }) });
  const row = section.locator("div").filter({ hasText: word }).last();
  await expect(row).toBeVisible();
  await expect(row.locator("[data-overdue]")).toBeVisible();
  await row.getByRole("button", { name: "標成做完" }).click();
  await expect(section.getByText(word)).toHaveCount(0);
});

// 手機選一位：今天頂端是她的首頁（照片、名字、跟她聊天），點「看全部秘書」回到全部（2026-11-02 稽核）。
test("TestTodaySingleAgentHeroOnPhone", async ({ page, isMobile }) => {
  test.skip(!isMobile, "手機版面");
  await page.goto("/");
  await expect(page.getByTestId("agent-hero")).toHaveCount(0);
  await page
    .getByRole("button", { name: /Builder/ })
    .first()
    .click();
  const hero = page.getByTestId("agent-hero");
  await expect(hero).toBeVisible();
  await expect(hero.getByRole("heading", { name: "Builder" })).toBeVisible();
  await expect(hero.getByRole("link", { name: /跟 Builder 聊天/ })).toBeVisible();
  await hero.getByRole("button", { name: "看全部秘書" }).click();
  await expect(page.getByTestId("agent-hero")).toHaveCount(0);
});

// 手機上衣櫃／展示間的入口：今天頁她的大頭像可以點，到核心的角色頁（上面有「介紹｜衣櫃｜展示間」分頁），
// 再點「衣櫃」就到衣櫃；按上一頁回得來。不擠底部五格。
test("TestTodayHeroOpensPersonasOnPhone", async ({ page, isMobile }) => {
  test.skip(!isMobile, "手機版面");
  await page.goto("/");
  await page
    .getByRole("button", { name: /Builder/ })
    .first()
    .click();
  const link = page.getByTestId("agent-hero").getByTestId("hero-personas");
  await expect(link).toHaveAccessibleName(/Builder 的介紹、衣櫃與展示間/);
  const box = await link.boundingBox();
  expect(box?.width ?? 0).toBeGreaterThanOrEqual(44);
  expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
  await link.click();
  await expect(page).toHaveURL(/\/personas$/);
  await page.getByRole("link", { name: "衣櫃", exact: true }).first().click();
  await expect(page).toHaveURL(/\/p\/closet\/wardrobe/);
  await page.goBack();
  await expect(page).toHaveURL(/\/personas$/);
});

// 手機今天頁的順序（DEV-156 U-04，作者 2026-11-14）：等你處理 → 快速輸入 → 今天的待辦 → 套件卡片；
// 沒有等你處理的事時那一塊不佔位置；宣告自己是空的套件卡片（EmptyLine）縮成一行，有資料就是整張卡。
// Guards: WEB-177, FEN-8, UXC-5
test("TestPhoneTodayOrderAndEmptyCards", async ({ page, request, baseURL, seed, isMobile }) => {
  test.skip(!isMobile, "手機版面");
  const post = async (route: string, data: unknown) => {
    const res = await request.post(`${baseURL ?? ""}/api/${route}`, { data });
    expect(res.status(), route).toBeLessThan(300);
    return (await res.json().catch(() => ({}))) as { records?: { id: string }[] };
  };
  const kinds = [
    "habits.habit",
    "habits.checkin",
    "body.entry",
    "nutrition.meal",
    "fitness.workout",
  ];
  const clear = async () => {
    for (const kind of kinds) {
      const got = await post("vault/list", { kind, include_archived: false });
      for (const r of got.records ?? []) await post("vault/archive", { kind, id: r.id });
    }
  };
  await clear();
  // 別的測試在同一個家留了等你處理的事：先假裝一件都沒有，看那一塊不佔位置。
  let none = true;
  await page.route("**/api/web/get_overview", async (route) => {
    if (!none) return route.continue();
    const res = await route.fetch();
    await route.fulfill({
      response: res,
      json: { ...(await res.json()), waiting_total: 0, approvals: [], waiting_tasks: [] },
    });
  });
  const top = async (loc: ReturnType<Page["locator"]>) => (await loc.boundingBox())?.y ?? -1;
  const quick = page.getByRole("textbox", { name: "快速輸入" });
  const todos = page.getByRole("heading", { name: /^今天的待辦/ });
  const cards = page.locator('[data-slot="core.overview.cards"]');
  const waiting = page.getByTestId("today-waiting-section");

  await page.goto("/");
  const lines: [string, string, string][] = [
    ["habits-today-empty", "今天的習慣：還沒約約一個", "habits-today"],
    ["body-today-empty", "體重：還沒記＋記一筆", "body-today"],
    ["nutrition-today-empty", "飲食：今天還沒記＋記一餐", "nutrition-today"],
    ["fitness-today-empty", "運動：這週還沒練＋記一筆", "fitness-today"],
  ];
  for (const [id, text, full] of lines) {
    const line = cards.getByTestId(id);
    await expect(line).toHaveText(text);
    await expect(page.getByTestId(full)).toHaveCount(0);
    // 一行：不到一張卡的高度；右邊的動作熱區至少 44px。
    expect((await line.boundingBox())?.height ?? 999).toBeLessThanOrEqual(56);
    expect((await line.locator("a, button").boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(44);
  }
  // 沒有等你處理的事：那一塊（標題與「都處理完了」）不出現，輸入框在最上面。
  await expect(waiting).toHaveCount(0);
  await expect(page.getByText("都處理完了")).toHaveCount(0);
  await expect(page.getByTestId("quick-starters")).toBeVisible();
  expect(await top(quick)).toBeLessThan(await top(todos));
  expect(await top(todos)).toBeLessThan(await top(cards));

  // 一行版的動作：「約一個」開「跟誰約？」；「＋記一筆」到身體頁。
  await cards.getByRole("button", { name: "約一個" }).click();
  const sheet = page.getByRole("dialog", { name: "跟誰約？" });
  await expect(sheet.getByTestId("habit-new-agent").first()).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(sheet).toHaveCount(0);
  await cards.getByTestId("body-today-empty").getByRole("link", { name: "＋記一筆" }).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/p/body/log");

  // 有資料的卡片照舊是整張；有事等你時「等你處理」排在輸入框上面。
  const now = new Date();
  const day = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
  await post("vault/put", {
    record: { kind: "body.entry", id: day, fields: { date: day, weight: 70 } },
  });
  const title = `手機順序 ${Date.now()}`;
  await seed.approve(title);
  none = false;
  await page.goto("/");
  await expect(page.getByTestId("body-today")).toContainText("體重 70");
  await expect(page.getByTestId("body-today-empty")).toHaveCount(0);
  await expect(waiting.getByText(title).first()).toBeVisible();
  await expect(waiting.getByRole("heading", { name: /^等你處理/ })).toBeVisible();
  expect(await top(waiting)).toBeLessThan(await top(quick));
  expect(await top(quick)).toBeLessThan(await top(todos));
  expect(await top(todos)).toBeLessThan(await top(cards));
  await clear();
});
