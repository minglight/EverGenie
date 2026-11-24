import { readFileSync } from "node:fs";
import path from "node:path";
import type { Page } from "@playwright/test";
import { expect, test } from "./fixtures";

// P6-6：簡單／完整模式（通知、Goal 欄）、設定→紀錄、夜班清單。
// e2e 的家是完整模式；這些測試自己切到簡單，結束時一定切回完整、還原通知偏好，同一個 worker 的下一個測試才不受影響。

type Api = <T>(route: string, body: unknown) => Promise<T>;
interface Prefs {
  preset?: string;
  categories?: Record<string, { mode: string; digest_hours: number }>;
  agents?: { agent: string; categories?: Record<string, { mode: string }> }[];
  [key: string]: unknown;
}

const owner = { name: "owner(test)" };
const getPrefs = async (api: Api) =>
  (await api<{ prefs: Prefs }>("/api/notify/get_prefs", {})).prefs;
const getMode = async (api: Api) => (await api<{ mode: string }>("/api/system/get_mode", {})).mode;
const setMode = (api: Api, mode: "simple" | "full") =>
  api("/api/system/set_mode", { mode, by: owner });
const restore = async (api: Api, original: Prefs) => {
  await setMode(api, "full");
  await api("/api/notify/set_prefs", { prefs: original, by: owner });
};
const notifyToml = (home: string) =>
  readFileSync(path.join(home, "settings", "notify.toml"), "utf8");
const saved = (page: Page) => page.getByRole("status").getByText("已儲存");
const modeGroup = (page: Page) => page.getByRole("group", { name: "通知的設定方式" });

// Guards: REQ-25
test("TestNotifySimpleShowsPresetsOnly @smoke", async ({ page, seed, hgServer }) => {
  const original = await getPrefs(seed.api);
  try {
    await page.goto("/settings/notify");
    await expect(page.getByRole("group", { name: "摘要的送法" })).toBeVisible();
    await modeGroup(page).getByRole("button", { name: "簡單" }).click();
    await expect.poll(() => getMode(seed.api)).toBe("simple");

    // 簡單模式：三張預設組卡片＋安靜時段；沒有逐類設定、單一秘書另設、任務單跟進。
    for (const name of ["安靜", "標準", "積極"]) {
      await expect(page.getByRole("button", { name: new RegExp(`^${name}`) })).toBeVisible();
    }
    await expect(page.getByTestId("preset-standard").getByRole("button").first()).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await expect(page.getByRole("group", { name: "摘要的送法" })).toHaveCount(0);
    await expect(page.getByRole("heading", { name: "單一秘書另設" })).toHaveCount(0);
    await expect(page.getByText("任務單跟進")).toHaveCount(0);
    await expect(page.getByRole("heading", { name: "安靜時段" })).toBeVisible();

    // 展開「每類會怎麼送」：內容來自核心的預設表。
    await page
      .getByTestId("preset-standard")
      .getByRole("button", { name: "看每類會怎麼送" })
      .click();
    const detail = page.getByTestId("preset-standard-detail");
    await expect(detail).toContainText("摘要");
    await expect(detail).toContainText("每 3 小時合併一次");
    await expect(detail).toContainText("秘書主動開場");

    // 選「積極」：只改 preset，檔案裡不寫逐類設定。
    await page.getByTestId("preset-active").getByRole("button").first().click();
    await expect(saved(page)).toBeVisible();
    await expect.poll(async () => (await getPrefs(seed.api)).preset).toBe("active");
    const prefs = await getPrefs(seed.api);
    expect(prefs.categories?.opening?.mode).toBe("instant");
    const toml = notifyToml(hgServer.home);
    expect(toml).toContain('preset = "active"');
    expect(toml).not.toContain("[categories");

    // 切到完整：預設組展開成明確的逐類設定寫回檔案。
    await modeGroup(page).getByRole("button", { name: "完整" }).click();
    await expect.poll(() => getMode(seed.api)).toBe("full");
    await expect(page.getByRole("group", { name: "摘要的送法" })).toBeVisible();
    await expect.poll(() => notifyToml(hgServer.home)).toContain("[categories.opening]");
    expect(notifyToml(hgServer.home)).toContain("[categories.approve]");

    // 完整模式自己調一類（摘要改成只放看板，跟「積極」的即時不一樣）→ 切回簡單：提醒有 1 項自己調過的。
    await page
      .getByRole("group", { name: "摘要的送法" })
      .getByRole("button", { name: "只放看板" })
      .click();
    await expect(saved(page)).toBeVisible();
    await modeGroup(page).getByRole("button", { name: "簡單" }).click();
    await expect(page.getByTestId("notify-overrides")).toContainText("你有 1 項自己調過的設定");
    await expect(page.getByTestId("notify-overrides")).toContainText("切到完整模式才看得到");

    // 選預設組前先確認：「先不要」什麼都不變。
    await page.getByTestId("preset-standard").getByRole("button").first().click();
    const confirm = page.getByTestId("notify-overrides-confirm");
    await expect(confirm).toContainText("選「標準」會清掉你自己調過的 1 項設定");
    await confirm.getByRole("button", { name: "先不要" }).click();
    await expect(confirm).toHaveCount(0);
    expect((await getPrefs(seed.api)).preset).toBe("active");
    // 確定：覆寫清掉，檔案不再有逐類設定。
    await page.getByTestId("preset-standard").getByRole("button").first().click();
    await page
      .getByTestId("notify-overrides-confirm")
      .getByRole("button", { name: "確定，清掉並套用" })
      .click();
    await expect(saved(page)).toBeVisible();
    await expect.poll(async () => (await getPrefs(seed.api)).preset).toBe("standard");
    expect((await getPrefs(seed.api)).categories?.digest?.mode).toBe("digest");
    expect(notifyToml(hgServer.home)).not.toContain("[categories");
    await expect(page.getByTestId("notify-overrides")).toHaveCount(0);

    // 重新整理後還是簡單模式、標準。
    await page.reload();
    await expect(page.getByTestId("preset-standard").getByRole("button").first()).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  } finally {
    await restore(seed.api, original);
  }
});

// Guards: REQ-25, REQ-26
test("TestNotifyFullAgentOverride", async ({ page, request, seed }) => {
  const original = await getPrefs(seed.api);
  const roster = (await (await request.post("/api/web/get_roster", { data: {} })).json()) as {
    agents: { id: string; name: string }[];
  };
  const codeName = roster.agents.find((a) => a.id === "code")?.name ?? "";
  expect(codeName).not.toBe("");
  try {
    await page.goto("/settings/notify");
    await expect(page.getByRole("heading", { name: "單一秘書另設" })).toBeVisible();
    await expect(page.getByText("選了秘書，才會列出那位秘書每一類的送法。")).toBeVisible();
    await page.getByTestId("agent-override-pick").selectOption("code");

    // 「要你同意」沒有「關閉」；其他分類五種都有（跟全域一樣＋四種送法）。
    const approve = page.getByRole("group", { name: `${codeName}的要你同意送法` });
    await expect(approve.getByRole("button")).toHaveText([
      "跟全域一樣",
      "即時",
      "合併摘要",
      "只放看板",
    ]);
    const done = page.getByRole("group", { name: `${codeName}的事情做完送法` });
    await expect(done.getByRole("button")).toHaveText([
      "跟全域一樣",
      "即時",
      "合併摘要",
      "只放看板",
      "關閉",
    ]);
    await expect(done.getByRole("button", { name: "跟全域一樣" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    // 這位秘書的「事情做完」另設成只放看板；全域不動。
    await done.getByRole("button", { name: "只放看板" }).click();
    await expect(saved(page)).toBeVisible();
    await expect
      .poll(async () => (await getPrefs(seed.api)).agents?.[0]?.categories?.done?.mode)
      .toBe("board");
    const prefs = await getPrefs(seed.api);
    expect(prefs.agents).toHaveLength(1);
    expect(prefs.agents?.[0]?.agent).toBe("code");
    expect(prefs.categories?.done?.mode).toBe(original.categories?.done?.mode);
    // 選單上標出這位另設了幾項。
    await expect(page.getByTestId("agent-override-pick").locator("option:checked")).toContainText(
      "另設 1 項",
    );

    // 「要你同意」降到只放看板可以。
    await approve.getByRole("button", { name: "只放看板" }).click();
    await expect(saved(page)).toBeVisible();
    await expect
      .poll(async () => (await getPrefs(seed.api)).agents?.[0]?.categories?.approve?.mode)
      .toBe("board");

    // 重新整理後還在；改回「跟全域一樣」就拿掉，一項都不剩整位拿掉。
    await page.reload();
    await page.getByTestId("agent-override-pick").selectOption("code");
    await expect(done.getByRole("button", { name: "只放看板" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await done.getByRole("button", { name: "跟全域一樣" }).click();
    await approve.getByRole("button", { name: "跟全域一樣" }).click();
    await expect.poll(async () => (await getPrefs(seed.api)).agents ?? []).toHaveLength(0);
  } finally {
    await restore(seed.api, original);
  }
});

// 2026-10-28 起簡單模式也顯示 Goal（作者：Goal 直接放在任務單）；模式只影響通知分頁。
// Guards: REQ-58, REQ-237
test("TestSimpleModeKeepsGoalUI @smoke", async ({ page, seed }) => {
  const original = await getPrefs(seed.api);
  const title = `模式測試 ${Date.now().toString(36)}`;
  const taskId = await seed.doingTask(title);
  const goal = await seed.api<{ goal: { id: string } }>("/api/goals/propose", {
    title: `Goal ${title}`,
    text: "測試用的 Goal",
    criteria: "測試驗收條件",
    agent: "code",
    by: { name: "code" },
  });
  await seed.api("/api/tasks/update", { id: taskId, goal: goal.goal.id, by: { name: "code" } });
  const goalOf = async () =>
    (await seed.api<{ task: { goal?: string } }>("/api/tasks/get", { id: taskId })).task.goal;
  try {
    // 完整模式：詳情有 Goal 欄。
    await page.goto(`/tasks/${taskId}`);
    await expect(page.getByRole("heading", { name: title })).toBeVisible();
    await expect(page.getByTestId("goal-fact")).toContainText(goal.goal.id);
    await expect(page.getByTestId("goal-fact")).toContainText(`Goal ${title}`);

    // 切到簡單：Goal 欄還在，資料沒動。
    await page.goto("/settings/notify");
    await modeGroup(page).getByRole("button", { name: "簡單" }).click();
    await expect.poll(() => getMode(seed.api)).toBe("simple");
    await page.goto(`/tasks/${taskId}`);
    await expect(page.getByRole("heading", { name: title })).toBeVisible();
    await expect(page.getByTestId("goal-fact")).toContainText(goal.goal.id);
    expect(await goalOf()).toBe(goal.goal.id);
    // 任務清單的 Goal 標籤也在。
    await page.goto("/tasks?view=all");
    await expect(page.getByText(title).first()).toBeVisible();
    await expect(page.getByTestId("task-goal").first()).toBeVisible();

    // 切回完整：一樣，同一筆的 goal 沒變。
    await page.goto("/settings/notify");
    await modeGroup(page).getByRole("button", { name: "完整" }).click();
    await expect.poll(() => getMode(seed.api)).toBe("full");
    await page.goto(`/tasks/${taskId}`);
    await expect(page.getByTestId("goal-fact")).toContainText(goal.goal.id);
    expect(await goalOf()).toBe(goal.goal.id);
  } finally {
    await restore(seed.api, original);
  }
});

// Guards: REQ-111
test("TestLogTabShowsRecentChanges @smoke", async ({ page, seed }) => {
  const content = `紀錄測試 ${Date.now().toString(36)}`;
  const id = await seed.reminder(content);
  await page.goto("/settings/log");
  const row = page.getByTestId("log-row").filter({ hasText: `主人 新增 待辦 ${id}` });
  await expect(row).toBeVisible();
  // 點編號進到那一筆待辦。
  await row.getByRole("link", { name: id }).click();
  await expect(page).toHaveURL(new RegExp(`/reminders/${id}$`));
  await expect(page.getByText(content).first()).toBeVisible();

  // 篩選：種類＝任務單時看不到待辦；種類＝待辦、誰＝主人時看得到。
  await page.goto("/settings/log");
  await page.getByTestId("log-kind").selectOption("task");
  await expect(page.getByTestId("log-row").filter({ hasText: id })).toHaveCount(0);
  await page.getByTestId("log-kind").selectOption("reminder");
  await page.getByTestId("log-who").selectOption("owner");
  await expect(
    page.getByTestId("log-row").filter({ hasText: `主人 新增 待辦 ${id}` }),
  ).toBeVisible();
  // 日期晚於今天：沒有紀錄，空狀態說明下一步。
  await page.getByTestId("log-since").fill("2099-01-01");
  await expect(page.getByText("這個條件下沒有紀錄")).toBeVisible();
  await expect(page.getByText("換個條件，或把篩選清掉看看。")).toBeVisible();
  await expect(page.getByText("保留最近 30 天，更早的會自動清掉。")).toBeVisible();
});

// 「載入更早的」：第一頁帶 next_before，按了接第二頁，沒有更早的就不再出現。
// Guards: REQ-111
test("TestLogTabLoadsEarlier", async ({ page }) => {
  const item = (n: number, day: string) => ({
    at: `${day}T10:${String(n).padStart(2, "0")}:00+08:00`,
    by: "owner(web)",
    kind: "task",
    id: `SEC-${n}`,
    op: "patch",
  });
  const calls: string[] = [];
  await page.route("**/api/vault/changes", async (route) => {
    const body = route.request().postDataJSON() as { before?: string };
    calls.push(body.before ?? "");
    if (!body.before) {
      await route.fulfill({
        json: {
          items: [item(30, "2026-10-25"), item(29, "2026-10-25")],
          next_before: "2026-10-25:3",
        },
      });
    } else {
      await route.fulfill({ json: { items: [item(10, "2026-10-20")] } });
    }
  });
  await page.goto("/settings/log");
  await expect(page.getByTestId("log-row")).toHaveCount(2);
  await expect(page.getByTestId("log-row").first()).toContainText("主人 修改 任務單 SEC-30");
  await page.getByRole("button", { name: "載入更早的" }).click();
  await expect(page.getByTestId("log-row")).toHaveCount(3);
  await expect(page.getByTestId("log-row").last()).toContainText("SEC-10");
  await expect(page.getByRole("button", { name: "載入更早的" })).toHaveCount(0);
  expect(calls).toEqual(["", "2026-10-25:3"]);
});

// Guards: REQ-19, REQ-111
test("TestNightQueueAddAndRemove", async ({ page, hgServer }) => {
  const file = path.join(hgServer.home, "vault", "agents", "system", "notes", "night-queue.md");
  const text = `整理 e2e 筆記 ${Date.now().toString(36)}`;
  await page.goto("/settings/machine");
  await expect(page.getByText("夜班清單（進階）", { exact: true })).toBeVisible();
  const todo = page.getByTestId("night-todo");
  await expect(page.getByTestId("night-blocked")).toContainText("（沒有）");
  await expect(page.getByTestId("night-done")).toContainText("（沒有）");

  // 「新增一項」打開表單（DEV-34）：填標題按 Enter 送出；出現在「待做」，表單收起來，檔案是 note 的 Markdown。
  await page.getByRole("button", { name: "新增一項" }).click();
  await page.getByTestId("night-add").fill(text);
  await page.getByTestId("night-add").press("Enter");
  await expect(todo).toContainText(text);
  await expect(todo).toContainText("加入 ");
  await expect(page.getByTestId("night-form")).toHaveCount(0);
  expect(readFileSync(file, "utf8")).toContain(`- [ ] ${text}｜加入 `);
  expect(readFileSync(file, "utf8")).toContain("## 做完（留最近 14 天）");
  // 重新整理後還在。
  await page.reload();
  await expect(page.getByTestId("night-todo")).toContainText(text);

  // 刪除：按一次先展開確認，「取消」不刪；「確定刪除」才刪。
  await page.getByRole("button", { name: `刪除「${text}」` }).click();
  await page.getByRole("button", { name: "取消" }).click();
  await expect(page.getByTestId("night-todo")).toContainText(text);
  await page.getByRole("button", { name: `刪除「${text}」` }).click();
  await page.getByRole("button", { name: "確定刪除" }).click();
  await expect(page.getByTestId("night-todo")).not.toContainText(text);
  expect(readFileSync(file, "utf8")).not.toContain(text);
});

// 設定精靈沒有「介面」那一步了（2026-10-28）；簡單／完整只剩通知分頁的切換。
// Guards: REQ-237
test("TestSetupWizardHasNoModeStep", async ({ page }) => {
  await page.goto("/settings/setup");
  await expect(page.getByTestId("setup-step-agents")).toBeVisible();
  await expect(page.getByTestId("setup-step-mode")).toHaveCount(0);
  await expect(page.getByTestId("setup-step-agents")).toContainText(
    "1. 選職位，每個職位請一位角色擔任",
  );
});

// DEV-156 U-07：簡單模式下職位只寫中文名，不露 `system` 這類代號；完整模式照舊（settings-tabs.spec 守完整模式）。
test("TestSimpleModeHidesFunctionCodes", async ({ page, seed }) => {
  try {
    await setMode(seed.api, "simple");
    await page.goto("/settings/agents");
    const card = page.locator('[data-testid="function-unclaimed"][data-agent="system"]');
    await expect(card).toContainText("電腦");
    await expect(card).not.toContainText("職位 system");
    await expect(page.getByRole("main")).not.toContainText(/職位 [a-z]/);
  } finally {
    await setMode(seed.api, "full");
  }
});
