import type { Locator, Page } from "@playwright/test";
import { expect, type Seed, test } from "./fixtures";

// F6：看板開 Goal（D-39，REQ-202）與 Goal 頁（REQ-203）。
// e2e 的家是完整模式；簡單模式的測試自己切換，結束一定切回完整。

const rand = () => Math.random().toString(36).slice(2, 8);
const owner = { name: "owner(test)" };

interface GoalRecord {
  id: string;
  title: string;
  text: string;
  state: string;
  by?: string;
  notice?: string;
  confirmed_by?: string;
}

const goalOf = async (seed: Seed, id: string): Promise<GoalRecord> =>
  (await seed.api<{ goal: GoalRecord }>("/api/goals/get", { id })).goal;

async function ownerGoal(seed: Seed, title: string, text = `${title} 的原話`): Promise<GoalRecord> {
  const res = await seed.api<{ goal: GoalRecord }>("/api/goals/create", {
    criteria: "測試驗收條件",
    title,
    text,
    by: owner,
  });
  return res.goal;
}

async function draftGoal(seed: Seed, title: string): Promise<GoalRecord> {
  const res = await seed.api<{ goal: GoalRecord }>("/api/goals/propose", {
    criteria: "測試驗收條件",
    title,
    text: `${title} 的原話`,
    agent: "code",
    by: { name: "code" },
  });
  return res.goal;
}

const row = (page: Page, id: string): Locator => page.locator(`a[data-goal="${id}"]`);

// 新增 Goal 的表單（手機是抽屜、桌機是對話框，都是 dialog）。
const newDialog = (page: Page) => page.getByRole("dialog", { name: "新增 Goal" });

// Guards: REQ-202, REQ-203
test("TestBoardCreatesGoal @smoke", async ({ page, seed }) => {
  const title = `整理報表 ${rand()}`;
  const text = `把三月的報表整理好，${rand()}`;
  await page.goto("/goals");
  await page.getByRole("button", { name: "新增 Goal" }).click();
  const dialog = newDialog(page);
  await expect(dialog).toBeVisible();
  // 沒填就送：三個必填欄位下面各寫白話的錯，什麼都沒建。
  await dialog.getByRole("button", { name: "開始這個 Goal" }).click();
  await expect(dialog.getByText("請寫一行標題")).toBeVisible();
  await expect(dialog.getByText("請寫下做完時是什麼樣子")).toBeVisible();
  await expect(dialog.getByText("請寫驗收條件：怎樣算達成")).toBeVisible();
  await dialog.getByLabel("標題", { exact: true }).fill(title);
  await dialog.getByLabel("要做到什麼（完成的樣子）").fill(text);
  await dialog.getByLabel("驗收條件（怎樣算達成）").fill("三月每一天都有一列、數字對過");
  // 授權範圍的勾選已拿掉（DEV-95）：表單上不會再有。
  await expect(dialog.locator("input[data-grant]")).toHaveCount(0);
  await expect(dialog.getByText("授權範圍")).toHaveCount(0);
  // 負責秘書（選填）：選第一位；記下她的名字，詳情頁要看得到。
  const picker = dialog.getByRole("group", { name: "負責秘書" });
  const firstAgent = picker.getByRole("button").first();
  // 按鈕裡是頭像（名字第一個字）＋名字，取最後一行的名字。
  const agentName = (await firstAgent.innerText()).trim().split("\n").pop() ?? "";
  await firstAgent.click();
  await expect(firstAgent).toHaveAttribute("aria-pressed", "true");
  await dialog.getByRole("button", { name: "開始這個 Goal" }).click();

  // 清單出現這一筆（進行中），toast 說開了哪一個。
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText(/已開 GOAL-\d+/)).toBeVisible();
  const created = page.getByRole("link", { name: new RegExp(title) });
  await expect(created).toBeVisible();
  await expect(created).toContainText("進行中");
  await expect(created).toContainText("你開的");
  const id = (await created.getAttribute("data-goal")) ?? "";
  expect(id).toMatch(/^GOAL-\d+$/);

  // 核心裡：直接進行中、確認的人是看板的主人、原話一字不差、沒有確認通知。
  const goal = await goalOf(seed, id);
  expect(goal.state).toBe("active");
  expect(goal.text).toBe(text);
  expect(goal.by).toBe("owner(web)");
  expect(goal.confirmed_by).toBe("owner(web)");
  expect(goal).not.toHaveProperty("grants");
  expect(goal.notice ?? "").toBe("");
  expect(goal.agent ?? "").not.toMatch(/^owner\(/);

  // 詳情：原文唯讀，沒有任何可以編輯的欄位。
  await created.click();
  await expect(page.getByRole("heading", { name: title })).toBeVisible();
  await expect(page.getByTestId("goal-text")).toHaveText(text);
  await expect(page.getByTestId("goal-grants")).toHaveCount(0);
  await expect(page.locator("article textarea, article input")).toHaveCount(0);
  await expect(page.getByText("確認後內容不能改")).toBeVisible();
  await expect(page.locator("article")).toContainText(agentName);
  // 底下沒有等你或等外部的單：說沒卡住。
  await expect(page.getByTestId("goal-blockers-none")).toBeVisible();

  // 開任務單時，Goal 下拉看得到它。
  await page.goto("/tasks");
  await page.getByRole("button", { name: "更多選項" }).click();
  await expect(
    page.getByLabel("Goal（長期目標）").locator("option", { hasText: title }),
  ).toHaveCount(1);
});

// Guards: REQ-202
test("TestAgentCannotCreateGoal @smoke", async ({ page, seed, request, baseURL }) => {
  const title = `秘書偷開 ${rand()}`;
  // 秘書（socket 上帶秘書身分）直接開：FORBIDDEN，什麼都沒建。
  const before = ((await seed.api<{ goals?: GoalRecord[] }>("/api/goals/list", {})).goals ?? [])
    .length;
  await expect(
    seed.api("/api/goals/create", {
      criteria: "測試驗收條件",
      title,
      text: "x",
      by: { name: "code" },
    }),
  ).rejects.toThrow(/403/);
  const after = (await seed.api<{ goals: GoalRecord[] }>("/api/goals/list", {})).goals;
  expect(after.length).toBe(before);
  expect(after.some((g) => g.title === title)).toBe(false);

  // 看板這邊：身分一律換成主人（宣稱自己是 code 也一樣）；秘書提案的路不從看板來。
  const viaBoard = await request.post(`${baseURL}/api/goals/create`, {
    data: { title, text: "看板送的", criteria: "測試驗收條件", by: { name: "code" } },
  });
  expect(viaBoard.ok()).toBe(true);
  const made = ((await viaBoard.json()) as { goal: GoalRecord }).goal;
  expect(made.by).toBe("owner(web)");
  expect(made.state).toBe("active");
  const propose = await request.post(`${baseURL}/api/goals/propose`, {
    data: { title, text: "x", agent: "code" },
  });
  expect(propose.status()).toBe(404);
  await page.goto("/goals");
  await expect(page.getByRole("link", { name: new RegExp(title) })).toHaveCount(1);
});

// Guards: REQ-203
test("TestGoalDraftConfirmAndDecline", async ({ page, seed }) => {
  const yes = await draftGoal(seed, `秘書提的 ${rand()}`);
  const no = await draftGoal(seed, `秘書提的 ${rand()}`);
  // 「要你同意」那一段有它們，進行中那一段沒有。
  await page.goto("/goals?view=draft");
  await expect(row(page, yes.id)).toBeVisible();
  await expect(row(page, yes.id)).toContainText("要你同意");
  await page.goto("/goals");
  await expect(row(page, yes.id)).toHaveCount(0);

  await page.goto(`/goals/${yes.id}`);
  await expect(page.getByTestId("goal-draft")).toContainText("你同意後才開始接力");
  await page.getByRole("button", { name: "確認", exact: true }).click();
  await expect.poll(async () => (await goalOf(seed, yes.id)).state).toBe("active");
  expect((await goalOf(seed, yes.id)).confirmed_by).toBe("owner(web)");
  await expect(page.locator(`article[data-goal-state="active"]`)).toBeVisible();
  await expect(page.getByTestId("goal-draft")).toHaveCount(0);

  await page.goto(`/goals/${no.id}`);
  await page.getByRole("button", { name: "不要", exact: true }).click();
  await expect.poll(async () => (await goalOf(seed, no.id)).state).toBe("withdrawn");
  await page.goto("/goals?view=ended");
  await expect(row(page, no.id)).toContainText("已收回");
});

// Guards: REQ-203
test("TestGoalWithdrawNeedsConfirm", async ({ page, seed }) => {
  const goal = await ownerGoal(seed, `要收回的 ${rand()}`);
  await page.goto(`/goals/${goal.id}`);
  await page.getByRole("button", { name: "收回這個 Goal" }).click();
  // 畫面內確認區：先不要，什麼都沒變。
  const ask = page.getByRole("region", { name: "收回這個 Goal" });
  await expect(ask).toContainText(goal.title);
  await ask.getByRole("button", { name: "先不要" }).click();
  expect((await goalOf(seed, goal.id)).state).toBe("active");
  // 再按一次，確定收回。
  await page.getByRole("button", { name: "收回這個 Goal" }).click();
  await page.getByRole("button", { name: "確定收回" }).click();
  await expect.poll(async () => (await goalOf(seed, goal.id)).state).toBe("withdrawn");
  await expect(page.getByText(`已收回 ${goal.id}`)).toBeVisible();
  // 收回後詳情沒有動作，清單的「已結束」找得到。
  await expect(page.getByRole("button", { name: "收回這個 Goal" })).toHaveCount(0);
  await page.goto("/goals?view=ended");
  await expect(row(page, goal.id)).toContainText("已收回");
});

// Guards: REQ-203
test("TestGoalShowsMembers", async ({ page, seed }) => {
  const goal = await ownerGoal(seed, `有成員的 ${rand()}`);
  const taskTitle = `掛在 Goal 的單 ${rand()}`;
  const made = await seed.api<{ task: { id: string } }>("/api/tasks/create", {
    criteria: "測試驗收條件",
    title: taskTitle,
    agent: "code",
    goal: goal.id,
    by: { name: "code" },
  });
  await page.goto(`/goals/${goal.id}`);
  const members = page.getByTestId("goal-members");
  await expect(members).toContainText("進行中的任務單 1 張");
  await members.getByRole("link", { name: new RegExp(taskTitle) }).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe(`/tasks/${made.task.id}`);
  // 任務單詳情的 Goal 欄反過來連到這個 Goal。
  await page.getByTestId("goal-fact").getByRole("link", { name: goal.id }).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe(`/goals/${goal.id}`);
});

// Guards: REQ-203
test("TestGoalNotFound", async ({ page }) => {
  await page.goto("/goals/GOAL-99999");
  await expect(page.getByRole("heading", { name: "找不到這個 Goal" })).toBeVisible();
  // 桌機頁頂有一個「回到 Goal 清單」，找不到的提示裡也有一個，點哪個都回清單。
  await page.getByRole("link", { name: "回到 Goal 清單" }).last().click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/goals");
});

// 2026-10-28 起 Goal 不看簡單／完整模式，也不在選單裡（作者：Goal 放在任務單裡、排最後）：
// 任一模式，選單（桌機側欄、手機「更多」）都沒有 Goal，任務單頁的「Goal」分頁在、排在「排程」後面。
// Guards: REQ-203, REQ-237
test("TestGoalShownInSimpleMode", async ({ page, seed, isMobile }) => {
  const nav = page.getByRole("navigation", { name: "主選單" });
  const goalInMenu = async () => {
    if (isMobile) {
      await nav.getByRole("button", { name: /^更多/ }).click();
      const sheet = page.getByRole("dialog", { name: "更多" });
      const n = await sheet.getByRole("link", { name: /^Goal/ }).count();
      await page.keyboard.press("Escape");
      return n;
    }
    return nav.getByRole("link", { name: /^Goal/ }).count();
  };
  const checkBoth = async () => {
    await page.goto("/");
    await expect(nav).toBeVisible();
    expect(await goalInMenu()).toBe(0);
    await page.goto("/tasks");
    const tabs = page.getByRole("navigation", { name: "任務單頁的分頁" });
    await expect(tabs.getByRole("link", { name: "Goal" })).toBeVisible();
    // 順序：任務單、排程、Goal。
    await expect(tabs.getByRole("link")).toHaveText(["任務單", "排程", "Goal"]);
  };
  await checkBoth();
  await seed.api("/api/system/set_mode", { mode: "simple", by: owner });
  try {
    await checkBoth();
  } finally {
    await seed.api("/api/system/set_mode", { mode: "full", by: owner });
  }
  await checkBoth();
});

// Guards: REQ-232
test("TestGoalTabCountsAndClosedMembers", async ({ page, seed }) => {
  const goal = await ownerGoal(seed, `看得到底下的事 ${rand()}`);
  const openTitle = `還在做的 ${rand()}`;
  const doneTitle = `做完的 ${rand()}`;
  await seed.api("/api/tasks/create", {
    criteria: "測試驗收條件",
    title: openTitle,
    agent: "code",
    goal: goal.id,
    by: { name: "code" },
  });
  const done = await seed.api<{ task: { id: string } }>("/api/tasks/create", {
    criteria: "測試驗收條件",
    title: doneTitle,
    agent: "code",
    goal: goal.id,
    by: { name: "code" },
  });
  // 等外部的單：Goal 詳情最上面的「卡在誰」列出它。
  const waitTitle = `等廠商回覆 ${rand()}`;
  await seed.api("/api/tasks/create", {
    criteria: "測試驗收條件",
    title: waitTitle,
    agent: "code",
    goal: goal.id,
    status: "waiting_external",
    by: { name: "code" },
  });
  // 結單走 tasks/close，一般任務只有主人能結（D-22）；不做了也算已結束。
  await seed.api("/api/tasks/close", {
    id: done.task.id,
    status: "cancelled",
    result: "不做了",
    by: { name: "owner(web)" },
  });

  // 任務單頁的分頁有 Goal；點了就是 Goal 清單，每一列寫著底下掛了幾張。
  await page.goto("/tasks");
  const tabs = page.getByRole("navigation", { name: "任務單頁的分頁" });
  await tabs.getByRole("link", { name: "Goal" }).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/goals");
  await expect(tabs.locator('[aria-current="page"]')).toHaveText("Goal");
  await expect(row(page, goal.id).getByTestId("goal-counts")).toHaveText(
    "進行中 2 張 · 已結束 1 張",
  );

  // 詳情：卡在誰（等外部的那張）；進行中的直接列，已結束的收起來，點開才列。
  await row(page, goal.id).click();
  const blockers = page.getByTestId("goal-blockers");
  await expect(blockers.getByRole("link")).toHaveCount(1);
  await expect(blockers).toContainText(waitTitle);
  await expect(blockers).toContainText("等外部");
  const members = page.getByTestId("goal-members");
  await expect(members).toContainText("進行中的任務單 2 張");
  await expect(members.getByRole("link", { name: new RegExp(openTitle) })).toBeVisible();
  await expect(members.getByRole("link", { name: new RegExp(doneTitle) })).toHaveCount(0);
  await members.getByRole("button", { name: "已結束的任務單 1 張" }).click();
  await expect(members.getByRole("link", { name: new RegExp(doneTitle) })).toContainText("不做了");
});

// Guards: REQ-38
test("TestGoalPageFiltersByGoalAndNoGroupUI", async ({ page, seed }) => {
  const a = await ownerGoal(seed, `甲 Goal ${rand()}`);
  const b = await ownerGoal(seed, `乙 Goal ${rand()}`);
  const inA = `掛在甲 ${rand()}`;
  const inB = `掛在乙 ${rand()}`;
  for (const [goal, title] of [
    [a, inA],
    [b, inB],
  ] as const) {
    await seed.api("/api/tasks/create", {
      criteria: "測試驗收條件",
      title,
      agent: "code",
      goal: goal.id,
      by: { name: "code" },
    });
  }

  // 依 Goal 篩選：每個 Goal 頁只列掛在它底下的任務單。
  await page.goto(`/goals/${a.id}`);
  const members = page.getByTestId("goal-members");
  await expect(members.getByRole("link", { name: new RegExp(inA) })).toBeVisible();
  await expect(members.getByRole("link", { name: new RegExp(inB) })).toHaveCount(0);
  await page.goto(`/goals/${b.id}`);
  await expect(members.getByRole("link", { name: new RegExp(inB) })).toBeVisible();
  await expect(members.getByRole("link", { name: new RegExp(inA) })).toHaveCount(0);

  // 取代群組介面：任務單頁的分頁是 Goal，不再有「群組」。
  await page.goto("/tasks");
  const tabs = page.getByRole("navigation", { name: "任務單頁的分頁" });
  await expect(tabs.getByRole("link", { name: "Goal" })).toBeVisible();
  await expect(tabs.getByRole("link", { name: "群組" })).toHaveCount(0);
  await expect(page.getByText("群組", { exact: true })).toHaveCount(0);
});

// Guards: REQ-232
test("TestTaskGoalChangeInPlace @smoke", async ({ page, seed }) => {
  const goal = await ownerGoal(seed, `之後再掛 ${rand()}`);
  const title = `先開了再說 ${rand()}`;
  const id = await seed.doingTask(title);
  const goalOfTask = async () =>
    (await seed.api<{ task: { goal?: string } }>("/api/tasks/get", { id })).task.goal ?? "";

  // 沒掛 Goal 的單也有 Goal 欄，寫「沒有掛 Goal」，旁邊可以掛上。
  await page.goto(`/tasks/${id}`);
  const fact = page.getByTestId("goal-fact");
  await expect(fact).toContainText("沒有掛 Goal");
  await fact.getByRole("button", { name: "掛到 Goal" }).click();
  await fact.getByLabel("掛在哪個 Goal").selectOption(goal.id);
  await expect.poll(goalOfTask).toBe(goal.id);
  await expect(fact.getByRole("link", { name: goal.id })).toBeVisible();
  await expect(fact).toContainText(goal.title);

  // 清單那一列標出 Goal 的標題。
  await page.goto("/tasks?view=all");
  const line = page.getByRole("link", { name: new RegExp(title) });
  await expect(line.getByTestId("task-goal")).toContainText(goal.title);

  // 改回不掛：清單那一列就沒有 Goal 了。
  await page.goto(`/tasks/${id}`);
  await fact.getByRole("button", { name: "改掛別的 Goal" }).click();
  await fact.getByLabel("掛在哪個 Goal").selectOption("");
  await expect.poll(goalOfTask).toBe("");
  await expect(fact).toContainText("沒有掛 Goal");
  await page.goto("/tasks?view=all");
  await expect(line.getByTestId("task-goal")).toHaveCount(0);
});

// 跟負責的秘書聊這個 Goal（WEB-82）：寬螢幕在右欄開專屬對話、第一句帶 Goal 編號而且只送一次；
// 對話停了再回來看得到記錄、不重送；窄螢幕按鈕開到聊天頁的同一個對話。
test("TestGoalChatOpensDedicatedRoom", async ({ page, seed }) => {
  const goal = await draftGoal(seed, `要聊的 ${rand()}`);
  const room = `code-${goal.id.toLowerCase()}`;
  // 只比對開頭：假秘書會回「收到：我們來聊…」，那則不算第一句。
  const first = new RegExp(`^我們來聊 ${goal.id}「`);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`/goals/${goal.id}`);
  const aside = page.getByTestId("goal-chat");
  await expect(aside).toBeVisible();
  await aside.getByTestId("goal-chat-start").click();
  // 第一句是替主人說的：帶 Goal 編號，秘書才知道在談哪一個。
  await expect(aside.getByText(first)).toHaveCount(1, { timeout: 15_000 });
  await expect(aside.getByRole("link", { name: "到聊天頁" })).toHaveAttribute(
    "href",
    new RegExp(`/chat/${room}$`),
  );

  // 對話停了（閒置關掉）再回來：直接看到記錄、沒有「開始聊」，第一句還是只有一則。
  await seed.api("/api/session/close", { name: room });
  await page.reload();
  await expect(aside.getByText(first)).toHaveCount(1, { timeout: 15_000 });
  await expect(aside.getByTestId("goal-chat-start")).toHaveCount(0);

  // 窄螢幕：沒有右欄，按鈕接回同一個對話，也不重送第一句。
  await page.setViewportSize({ width: 820, height: 1180 });
  await expect(page.getByTestId("goal-chat")).toHaveCount(0);
  await page.getByTestId("goal-chat-start").click();
  await expect(page).toHaveURL(new RegExp(`/chat/${room}$`));
  await expect(page.getByText(first)).toHaveCount(1, { timeout: 15_000 });
});
