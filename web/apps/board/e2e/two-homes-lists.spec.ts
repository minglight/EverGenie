import type { Page } from "@playwright/test";
import { expect, twoHomesTest as test } from "./fixtures";

// S3-7b：任務單、待辦、Goal 合併兩台（家 A 的看板 [[web.sites]] 有家 B）。每筆標它在哪台，詳情網址帶 ?home=，
// 動作送到那筆所屬的台，開單指給 B 的秘書就開在 B。兩個家的 fixture 一樣（秘書同名、單號可能撞），資料帶亂數（TST-13）。
// 只跑桌機（不列進 playwright.config.ts 的 mobileSpecs）。

const rand = () => Math.random().toString(36).slice(2, 8);

interface TaskFull {
  id: string;
  title: string;
  status?: string;
  agent?: string;
}

const num = (id: string) => Number(id.replace(/^\D+-/, ""));

// 兩台各開一張單、讓單號一樣（兩個家的 fixture 一樣，字首相同）：號碼小的那台再開（標題每張不同），直到兩邊一樣。
async function sameIdTasks(
  a: (title: string) => Promise<string>,
  b: (title: string) => Promise<string>,
  prefix: { a: string; b: string },
): Promise<{ id: string; aTitle: string; bTitle: string }> {
  let n = 0;
  let aTitle = `${prefix.a} 第${n}張`;
  let bTitle = `${prefix.b} 第${n}張`;
  let mine = await a(aTitle);
  let theirs = await b(bTitle);
  while (mine !== theirs && n < 200) {
    n++;
    if (num(mine) < num(theirs)) {
      aTitle = `${prefix.a} 第${n}張`;
      mine = await a(aTitle);
    } else {
      bTitle = `${prefix.b} 第${n}張`;
      theirs = await b(bTitle);
    }
  }
  expect(mine).toBe(theirs);
  return { id: mine, aTitle, bTitle };
}

// A 的看板要先試連到 B（ok）才會抓 B 的資料；B 的那一列出現前不能判斷「沒有」。
const rowOf = (page: Page, list: string, title: string) =>
  page.getByRole("list", { name: list }).getByRole("listitem").filter({ hasText: title });

test("TestTwoHomesTaskOnBShownTaggedAndChangedOnB", async ({ page, homes, seed }) => {
  // 撞號：兩台的單號一樣。
  const {
    id,
    aTitle: mineTitle,
    bTitle: theirsTitle,
  } = await sameIdTasks(
    (title) => seed.doingTask(title),
    (title) => homes.b.seed.doingTask(title),
    { a: `A 的單 ${rand()}`, b: `B 的單 ${rand()}` },
  );
  const mine = id;
  const theirs = id;

  await page.goto("/tasks?view=active");
  // 兩台的單在同一張清單，各自標台名（單號一樣也是兩列）。
  const bRow = rowOf(page, "任務單清單", theirsTitle);
  await expect(bRow).toBeVisible();
  await expect(bRow.getByTestId("site-tag")).toHaveAttribute("data-site", homes.b.label);
  await expect(bRow.getByTestId("site-tag")).toContainText(homes.b.label);
  const aRow = rowOf(page, "任務單清單", mineTitle);
  await expect(aRow.getByTestId("site-tag")).toHaveAttribute("data-site", "");
  await expect(aRow.getByRole("link")).toHaveAttribute("href", `/tasks/${mine}`);
  await expect(bRow.getByRole("link")).toHaveAttribute(
    "href",
    `/tasks/${theirs}?home=${homes.b.label}`,
  );

  // 詳情網址帶 ?home=B，重整還在（照網址找回那台）。
  await bRow.getByRole("link").click();
  await expect.poll(() => new URL(page.url()).pathname).toBe(`/tasks/${theirs}`);
  expect(new URL(page.url()).searchParams.get("home")).toBe(homes.b.label);
  const heading = page.getByRole("heading", { name: theirsTitle });
  await expect(heading).toBeVisible();
  await page.reload();
  await expect(heading).toBeVisible();
  // 側欄的秘書篩選也有台名標籤，只看頁面裡的。
  await expect(page.getByRole("main").getByTestId("site-tag").first()).toHaveAttribute(
    "data-site",
    homes.b.label,
  );

  // 在 A 的看板改狀態：B 那張真的改了，A 同號的單（如果有）不動。
  await page.getByRole("button", { name: "改成「等外部」" }).click();
  await expect
    .poll(
      async () =>
        (await homes.b.seed.api<{ task?: TaskFull }>("/api/tasks/get", { id: theirs })).task
          ?.status,
    )
    .toBe("waiting_external");
  const onA = await seed.api<{ task?: TaskFull }>("/api/tasks/get", { id: mine });
  expect(onA.task?.status).toBe("doing");
});

test("TestTwoHomesTaskStatusChangedOnBUpdatesListOnAWithin3s", async ({ page, homes }) => {
  const b = homes.b.seed;
  const title = `B 改狀態的單 ${rand()}`;
  const id = await b.doingTask(title);
  await page.goto("/tasks?view=all");
  const row = rowOf(page, "任務單清單", title);
  await expect(row.getByTestId("site-tag")).toHaveAttribute("data-site", homes.b.label);
  await expect(row).toContainText("進行中");
  // A 的看板對 B 的事件串流開好了，B 的變動才會即時到（沒開好前改，量到的是試連時間）。
  await expect
    .poll(
      () =>
        homes.b.proxy.forwarded.filter(
          (f) =>
            f.method === "POST" &&
            f.path === "/api/bus/subscribe" &&
            f.headers.origin === homes.a.url,
        ).length,
      { timeout: 20_000 },
    )
    .toBeGreaterThan(0);

  // B 那張單改成等你處理（秘書在問，畫面寫「要你回答」）：A 的清單不用重整，3 秒內那一列就變。
  await b.api("/api/tasks/update", {
    id,
    status: "waiting_user",
    ask: "要不要上線？",
    by: { name: "code" },
  });
  await expect(row).toContainText("要你回答", { timeout: 3_000 });
  await expect(row).not.toContainText("進行中");
});

test("TestTwoHomesNewTaskForBSecretaryCreatedOnB", async ({ page, homes, seed }) => {
  const title = `指給 B 的單 ${rand()}`;
  await page.goto("/tasks");
  // 等 B 連上（負責秘書的選單才會有 B 那一組）。
  await page.getByRole("textbox", { name: "交代秘書做什麼" }).fill(title);
  await page.getByRole("button", { name: /^(選一位秘書|換負責秘書)/ }).click();
  const bGroup = page.getByRole("group", { name: `負責秘書（${homes.b.label}）` });
  await bGroup.getByRole("button", { name: "Builder" }).click();
  // 畫面上看得出建在哪台：跟著秘書走。
  await expect(page.getByTestId("site-pick")).toHaveAttribute("data-site", homes.b.label);
  await page.getByRole("textbox", { name: "交代秘書做什麼" }).press("Enter");
  await expect(page.getByText(/已開任務單 SEC-\d+ 給Builder/)).toBeVisible();

  const find = async (api: typeof seed.api) =>
    ((await api<{ tasks?: TaskFull[] }>("/api/tasks/list", {})).tasks ?? []).find(
      (t) => t.title === title,
    );
  await expect.poll(async () => (await find(homes.b.seed.api))?.agent).toBe("code");
  expect(await find(seed.api)).toBeUndefined();
  // 清單上那一列標 B。
  await expect(rowOf(page, "任務單清單", title).getByTestId("site-tag")).toHaveAttribute(
    "data-site",
    homes.b.label,
  );
});

test("TestTwoHomesReminderOnBDoneFromA", async ({ page, homes }) => {
  const content = `B 的待辦 ${rand()}`;
  const id = await homes.b.seed.reminder(content, new Date(Date.now() - 60 * 60 * 1000));

  await page.goto("/reminders?view=overdue");
  const row = page.getByRole("listitem").filter({ hasText: content });
  await expect(row.getByTestId("site-tag")).toHaveAttribute("data-site", homes.b.label);
  await row.getByRole("link").click();
  await expect.poll(() => new URL(page.url()).pathname).toBe(`/reminders/${id}`);
  expect(new URL(page.url()).searchParams.get("home")).toBe(homes.b.label);
  await page.reload();
  await expect(page.getByRole("button", { name: "做了", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "做了", exact: true }).click();
  // 「做了」有幾秒可以復原，之後才送到 B。
  await expect
    .poll(
      async () =>
        (await homes.b.seed.api<{ reminder?: { status?: string } }>("/api/reminders/get", { id }))
          .reminder?.status,
      { timeout: 15_000 },
    )
    .toBe("done");
});

interface GoalRecord {
  id: string;
  title: string;
  state?: string;
}

test("TestTwoHomesGoalOnBConfirmedFromA", async ({ page, homes }) => {
  const title = `B 提的 Goal ${rand()}`;
  const draft = await homes.b.seed.api<{ goal: GoalRecord }>("/api/goals/propose", {
    criteria: "測試驗收條件",
    title,
    text: `${title} 的原話`,
    agent: "code",
    by: { name: "code" },
  });
  const id = draft.goal.id;

  await page.goto("/goals?view=draft");
  const row = page.getByRole("list", { name: "Goal 清單" }).getByRole("listitem").filter({
    hasText: title,
  });
  await expect(row.getByTestId("site-tag")).toHaveAttribute("data-site", homes.b.label);
  await row.getByRole("link").click();
  await expect.poll(() => new URL(page.url()).pathname).toBe(`/goals/${id}`);
  expect(new URL(page.url()).searchParams.get("home")).toBe(homes.b.label);
  await page.reload();
  // 在 A 的看板回答 B 的提議：確認送到 B（回答的是 B 的那則通知）。
  await page.getByTestId("goal-draft").getByRole("button", { name: "確認" }).click();
  await expect
    .poll(
      async () =>
        (await homes.b.seed.api<{ goal: GoalRecord }>("/api/goals/get", { id })).goal.state,
    )
    .toBe("active");
});

// Goal 聊天（WEB-82）：在 A 的看板聊 B 的 Goal，專屬對話開在 B、第一句送到 B，「到聊天頁」帶 B 的 ?home=；A 不開這個對話。
test("TestTwoHomesGoalChatOpensOnB", async ({ page, homes }) => {
  const title = `B 的 Goal 聊天 ${rand()}`;
  const draft = await homes.b.seed.api<{ goal: GoalRecord }>("/api/goals/propose", {
    criteria: "測試驗收條件",
    title,
    text: `${title} 的原話`,
    agent: "code",
    by: { name: "code" },
  });
  const id = draft.goal.id;
  const room = `code-${id.toLowerCase()}`;
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`/goals/${id}?home=${encodeURIComponent(homes.b.label)}`);
  const aside = page.getByTestId("goal-chat");
  await aside.getByTestId("goal-chat-start").click();
  await expect(aside.getByText(new RegExp(`^我們來聊 ${id}「`))).toHaveCount(1, {
    timeout: 15_000,
  });
  const href = await aside.getByRole("link", { name: "到聊天頁" }).getAttribute("href");
  const to = new URL(href ?? "", page.url());
  expect(to.pathname).toBe(`/chat/${room}`);
  expect(to.searchParams.get("home")).toBe(homes.b.label);

  // B 有這個房間；問對話、開對話都只送到 B，A（主伺服器）一個都沒收到（不退回主伺服器）。
  const onB = await homes.b.seed.api<{ session?: { name?: string; kind?: string } }>(
    "/api/session/get",
    { name: room },
  );
  expect(onB.session).toMatchObject({ name: room, kind: "room" });
  const hits = (h: typeof homes.a, path: string) =>
    h.proxy.forwarded.filter((f) => f.path === path).length;
  expect(hits(homes.b, "/api/session/open")).toBeGreaterThan(0);
  expect(hits(homes.a, "/api/session/open")).toBe(0);
  expect(hits(homes.a, "/api/session/get")).toBe(0);
});
