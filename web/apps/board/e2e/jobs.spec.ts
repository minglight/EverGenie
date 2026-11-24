import type { Page } from "@playwright/test";
import { expect, type Seed, test } from "./fixtures";

// 任務單 → 排程（/tasks/jobs，REQ-18、REQ-159；2026-10-26 從設定搬過來，舊網址轉址）。
// 每個測試造自己名稱獨一無二的排程，不依賴別的測試的資料。

const rand = () => Math.random().toString(36).slice(2, 8);

interface JobRecord {
  id: string;
  agent?: string;
  name?: string;
  cron?: string[];
  body?: string;
  enabled: boolean;
  archived: boolean;
  source?: string;
  by?: string;
}

const pageTabs = (page: Page) => page.getByRole("navigation", { name: "任務單頁的分頁" });

async function addJob(
  seed: Seed,
  name: string,
  extra: Partial<{ agent: string; cron: string[]; source: string; body: string }> = {},
): Promise<string> {
  const agent = extra.agent ?? "code";
  const id = `${agent}/e2e-${rand()}`;
  await seed.api("/api/scheduler/save_job", {
    job: {
      id,
      agent,
      name,
      cron: extra.cron ?? ["0 8 * * *"],
      body: extra.body ?? `${name} 的指令`,
      enabled: true,
      source: extra.source ?? "user",
    },
    by: { name: "owner(web)" },
  });
  return id;
}

async function jobOf(seed: Seed, id: string, archived = false): Promise<JobRecord | undefined> {
  const res = await seed.api<{ jobs?: JobRecord[] }>("/api/scheduler/list_jobs", {
    include_archived: archived,
  });
  return (res.jobs ?? []).find((j) => j.id === id);
}

const rowOf = (page: Page, name: string) =>
  page.getByRole("list", { name: "排程清單" }).getByRole("listitem").filter({ hasText: name });

async function openJob(page: Page, name: string) {
  await rowOf(page, name).getByRole("link").click();
  await expect(page.getByTestId("job-detail")).toBeVisible();
}

// Guards: REQ-18
test("TestJobsListAndToggle @smoke", async ({ page, seed }) => {
  const name = `晨間整理 ${rand()}`;
  const id = await addJob(seed, name);
  await page.goto("/tasks/jobs");
  await expect(pageTabs(page).locator('[aria-current="page"]')).toHaveText("排程");
  // 依秘書分組；每列有名稱、白話時間、下次執行。
  const row = rowOf(page, name);
  await expect(row).toBeVisible();
  await expect(row).toContainText("每天 08:00");
  await expect(row).toContainText("下次");
  await expect(page.getByRole("heading", { name: "Builder" })).toBeVisible();
  // 關：畫面先變、伺服器也真的關了；再開回來。
  const sw = row.getByRole("switch", { name: `啟用 ${name}` });
  await expect(sw).toHaveAttribute("aria-checked", "true");
  await sw.click();
  await expect(sw).toHaveAttribute("aria-checked", "false");
  await expect(row).toContainText("已停用");
  await expect.poll(async () => (await jobOf(seed, id))?.enabled).toBe(false);
  await sw.click();
  await expect.poll(async () => (await jobOf(seed, id))?.enabled).toBe(true);
  // 重新整理，畫面不變。
  await page.reload();
  await expect(rowOf(page, name)).toBeVisible();
});

// Guards: REQ-18
test("TestJobsEditTimeWeekly", async ({ page, seed }) => {
  const name = `改時間 ${rand()}`;
  const id = await addJob(seed, name, { cron: ["0 8 * * *"] });
  await page.goto("/tasks/jobs");
  await openJob(page, name);
  // 網址有 ?job=，重新整理還在同一個詳情。
  expect(new URL(page.url()).searchParams.get("job")).toBe(id);
  await page.reload();
  await expect(page.getByTestId("job-detail")).toBeVisible();
  // 點時間 → 每週幾 → 週一、週三、09:30 → 存。
  await page.getByRole("button", { name: /每天 08:00/ }).click();
  await page.getByRole("button", { name: "每週幾", exact: true }).click();
  await page.getByRole("button", { name: "週一", exact: true }).click();
  await page.getByRole("button", { name: "週三", exact: true }).click();
  await page.getByLabel("幾點").fill("09:30");
  await expect(page.getByTestId("schedule-preview")).toContainText("每週一三 09:30");
  await page.getByRole("button", { name: "存", exact: true }).click();
  await expect(page.getByRole("button", { name: /每週一三 09:30/ })).toBeVisible();
  await expect(page.getByText("已儲存")).toBeVisible();
  // 經 API 讀回來。
  await expect.poll(async () => (await jobOf(seed, id))?.cron).toEqual(["30 9 * * 1,3"]);
});

// Guards: REQ-18
test("TestJobsAdvancedCronPreview", async ({ page, seed }) => {
  const name = `進階 ${rand()}`;
  const id = await addJob(seed, name);
  await page.goto("/tasks/jobs");
  await openJob(page, name);
  await page.getByRole("button", { name: /每天 08:00/ }).click();
  await page.getByRole("button", { name: "進階", exact: true }).click();
  await page.getByLabel("cron（一行一組）").fill("0 9 * * 1-5\n30 13 * * 6");
  // 下方即時寫白話（多組用「；」）。
  await expect(page.getByTestId("schedule-preview")).toContainText(
    "每週一二三四五 09:00；每週六 13:30",
  );
  await page.getByRole("button", { name: "存", exact: true }).click();
  await expect
    .poll(async () => (await jobOf(seed, id))?.cron)
    .toEqual(["0 9 * * 1-5", "30 13 * * 6"]);
  // 看不懂的 cron：伺服器拒絕時原因寫在欄位下，內容沒有被改。
  await page.getByRole("button", { name: /每週一二三四五 09:00/ }).click();
  await page.getByLabel("cron（一行一組）").fill("不是 cron");
  await page.getByRole("button", { name: "存", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "排程格式不正確" })).toBeVisible();
  expect((await jobOf(seed, id))?.cron).toEqual(["0 9 * * 1-5", "30 13 * * 6"]);
});

// Guards: REQ-18, REQ-159
test("TestJobsEditNameBody", async ({ page, seed }) => {
  const name = `改名 ${rand()}`;
  const id = await addJob(seed, name, { cron: ["15 7 * * 1"] });
  await page.goto("/tasks/jobs");
  await openJob(page, name);
  const newName = `改好的名字 ${rand()}`;
  await page.getByRole("button", { name, exact: true }).click();
  const nameBox = page.getByLabel("名稱", { exact: true });
  await nameBox.fill(newName);
  await nameBox.press("Enter");
  await expect(page.getByText("已儲存")).toBeVisible();
  const newBody = `新的指令 ${rand()}`;
  await page.getByRole("button", { name: `${name} 的指令` }).click();
  const bodyBox = page.getByLabel("要做的事");
  await bodyBox.fill(newBody);
  await bodyBox.press("ControlOrMeta+Enter");
  await expect.poll(async () => (await jobOf(seed, id))?.body).toBe(newBody);
  const job = await jobOf(seed, id);
  expect(job?.name).toBe(newName);
  // 只改名稱與內容，時間沒被動到。
  expect(job?.cron).toEqual(["15 7 * * 1"]);
  // 名稱清空存不了：寫在欄位下，伺服器的資料不變。
  await page.getByRole("button", { name: newName, exact: true }).click();
  await page.getByLabel("名稱", { exact: true }).fill("   ");
  await page.getByLabel("名稱", { exact: true }).press("Enter");
  await expect(page.getByRole("alert").filter({ hasText: "名稱不能是空的" })).toBeVisible();
  expect((await jobOf(seed, id))?.name).toBe(newName);
});

// Guards: REQ-18
test("TestJobsWindowsReadOnly", async ({ page, seed }) => {
  const name = `時段型 ${rand()}`;
  const id = `life/e2e-${rand()}`;
  await seed.api("/api/scheduler/save_job", {
    job: {
      id,
      agent: "life",
      name,
      windows: ["10:00-12:00"],
      per_day: 2,
      body: "聊個天",
      enabled: true,
      source: "migrate",
    },
    by: { name: "owner(web)" },
  });
  await page.goto("/tasks/jobs");
  await expect(rowOf(page, name)).toContainText("時段 10:00–12:00、每天最多 2 次");
  await openJob(page, name);
  await expect(page.getByText("時段型排程目前只能開關。")).toBeVisible();
  await expect(page.getByRole("button", { name: /時段 10:00–12:00/ })).toHaveCount(0);
  await page.getByRole("switch", { name: `啟用 ${name}` }).click();
  await expect.poll(async () => (await jobOf(seed, id))?.enabled).toBe(false);
});

// Guards: REQ-18, REQ-159
test("TestJobsCreate", async ({ page, seed }) => {
  const word = `Weekly Sync ${rand()}`;
  await page.goto("/tasks/jobs");
  await page.getByRole("button", { name: "新增排程" }).click();
  const dialog = page.getByRole("dialog", { name: "新增排程" });
  // 秘書必選：沒選就送出，欄位下寫「選一位秘書」。
  await dialog.getByLabel("名稱", { exact: true }).fill(word);
  await dialog.getByLabel("要做的事", { exact: true }).fill("整理這週的重點");
  await dialog.getByRole("button", { name: "新增", exact: true }).click();
  await expect(dialog.getByRole("alert").filter({ hasText: "選一位秘書" })).toBeVisible();
  await dialog
    .getByRole("group", { name: "要做事的秘書" })
    .getByRole("button", { name: "Builder" })
    .click();
  await dialog.getByRole("button", { name: "新增", exact: true }).click();
  // 新增成功：進到那個排程的詳情；id 由名稱產生（英數字轉小寫減號）。
  await expect(page.getByTestId("job-detail")).toBeVisible();
  const slug = word.toLowerCase().replace(/[^a-z0-9]+/g, "-");
  const id = `code/${slug}`;
  expect(new URL(page.url()).searchParams.get("job")).toBe(id);
  await expect.poll(async () => (await jobOf(seed, id))?.name).toBe(word);
  const job = await jobOf(seed, id);
  expect(job?.source).toBe("user");
  expect(job?.enabled).toBe(true);
  expect(job?.cron).toEqual(["0 9 * * *"]);
  expect(job?.body).toBe("整理這週的重點");

  // 同名再新增一次：id 撞了，原因寫在名稱欄下，不會寫進第二筆。
  await page.goto("/tasks/jobs");
  await page.getByRole("button", { name: "新增排程" }).click();
  const again = page.getByRole("dialog", { name: "新增排程" });
  await again
    .getByRole("group", { name: "要做事的秘書" })
    .getByRole("button", { name: "Builder" })
    .click();
  await again.getByLabel("名稱", { exact: true }).fill(word);
  await again.getByLabel("要做的事", { exact: true }).fill("再一次");
  await again.getByRole("button", { name: "新增", exact: true }).click();
  await expect(again.getByRole("alert").filter({ hasText: "已經有這個排程" })).toBeVisible();
  expect((await jobOf(seed, id))?.body).toBe("整理這週的重點");
});

// Guards: REQ-18, REQ-159
test("TestJobsRemove", async ({ page, seed }) => {
  const name = `要移除 ${rand()}`;
  const id = await addJob(seed, name);
  await page.goto("/tasks/jobs");
  await openJob(page, name);
  // 移除要先在畫面內確認，寫明「會封存，不會刪除」；先不要就什麼都沒變。
  await page.getByRole("button", { name: "移除", exact: true }).click();
  await expect(page.getByText("它會被封存，不會刪除")).toBeVisible();
  await page.getByRole("button", { name: "先不要" }).click();
  expect((await jobOf(seed, id))?.archived).toBe(false);
  await page.getByRole("button", { name: "移除", exact: true }).click();
  await page.getByRole("button", { name: "確定移除" }).click();
  // 回到清單，那一筆不見；封存了，不是刪掉。
  await expect(page.getByRole("button", { name: "新增排程" })).toBeVisible();
  await expect(rowOf(page, name)).toHaveCount(0);
  expect(await jobOf(seed, id)).toBeUndefined();
  expect((await jobOf(seed, id, true))?.archived).toBe(true);
});

// Guards: REQ-18, REQ-159
test("TestJobsRemoveBuiltinOnlyDisables", async ({ page, seed }) => {
  const name = `搬來的 ${rand()}`;
  const id = await addJob(seed, name, { source: "migrate" });
  await page.goto("/tasks/jobs");
  await openJob(page, name);
  // 不是自己加的：按鈕寫「停用」、說明寫只能停用。
  await expect(page.getByRole("button", { name: "移除", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "停用", exact: true }).click();
  await expect(page.getByText("只能停用")).toBeVisible();
  await page.getByRole("button", { name: "確定停用" }).click();
  await expect.poll(async () => (await jobOf(seed, id))?.enabled).toBe(false);
  expect((await jobOf(seed, id))?.archived).toBe(false);
  await expect(page.getByText("已停用").first()).toBeVisible();
});

// Guards: REQ-18, REQ-159
test("TestJobsRunNow", async ({ page, seed }) => {
  const name = `立刻跑 ${rand()}`;
  // 用 Helper：有一輪對不上封包（算主人直接打字）之後 3 分鐘內，排程的話會先排隊；同一個 worker 的家是共用的，
  // 前面的聊天測試會讓 Builder 進入這段等待，這裡只等 20 秒，等不到。
  const id = await addJob(seed, name, { agent: "life" });
  await page.setViewportSize({ width: 390, height: 844 }); // 「全部 N 件」在手機的頂端篩選列
  await page.goto("/tasks/jobs");
  await openJob(page, name);
  // 打開一個排程時，秘書篩選列的「全部 N 件」照樣有數字，不是「— 件」（WEB-97）。
  const filter = page.getByRole("region", { name: "秘書篩選" });
  await expect(filter).toContainText(/\d+ 件/);
  await expect(filter).not.toContainText("— 件");
  await page.getByRole("button", { name: "現在跑一次" }).click();
  await expect(page.getByText("已叫 Helper 開始")).toBeVisible();
  // 跟時間到了一樣：秘書的對話裡收到「［排程 <id>］」開頭的那一句（假引擎收到）。
  await page.goto("/chat/life");
  await expect(page.getByRole("log", { name: "對話" })).toContainText(`［排程 ${id}］`, {
    timeout: 20_000,
  });
  // 指令不是主人說的：放秘書那邊一小排（WEB-158）。
  await expect(page.getByTestId("chat-aside").filter({ hasText: "排程：" })).toBeVisible();
});

// 跑過一次之後，詳情的「最近一次」與清單那一列都寫今天幾點跑的（還在做或已經回覆，看假引擎多快）；
// 沒跑過的寫「還沒跑過」，清單不多一行。
test("TestJobsShowLastRun", async ({ page, seed }) => {
  const name = `最近一次 ${rand()}`;
  await addJob(seed, name);
  await page.goto("/tasks/jobs");
  await expect(rowOf(page, name).getByTestId("job-last-run")).toHaveCount(0);
  await openJob(page, name);
  await expect(page.getByText("還沒跑過")).toBeVisible();
  await page.getByRole("button", { name: "現在跑一次" }).click();
  const ran = /今天 \d\d:\d\d (跑過|送出，還在做)/;
  await expect(page.getByTestId("job-last-run")).toHaveText(ran);
  await page.getByRole("link", { name: "上一頁" }).click();
  await expect(rowOf(page, name).getByTestId("job-last-run")).toHaveText(ran);
});

// Guards: REQ-18
test("TestJobsAgentFilter", async ({ page, seed }) => {
  const codeName = `Builder 的排程 ${rand()}`;
  const lifeName = `Helper 的排程 ${rand()}`;
  await addJob(seed, codeName, { agent: "code" });
  await addJob(seed, lifeName, { agent: "life" });
  await page.goto("/tasks/jobs");
  await expect(rowOf(page, codeName)).toBeVisible();
  await expect(rowOf(page, lifeName)).toBeVisible();
  // 跟其他頁共用的秘書篩選：只選 Builder，Helper 的排程不列。
  await page
    .getByRole("region", { name: "秘書篩選" })
    .getByRole("button", { name: /^Builder/ })
    .click();
  await expect(rowOf(page, codeName)).toBeVisible();
  await expect(rowOf(page, lifeName)).toHaveCount(0);
});

// 從任務單頁頂端的「排程」分頁進來，再點回「任務單」；兩邊各自一個網址，上一頁回得去。
// Guards: REQ-18
test("TestJobsTabFromTasks", async ({ page, seed }) => {
  const name = `分頁切換 ${rand()}`;
  await addJob(seed, name);
  await page.goto("/tasks");
  const tabs = pageTabs(page);
  await expect(tabs.locator('[aria-current="page"]')).toHaveText("任務單");
  await expect(page.getByRole("textbox", { name: "交代秘書做什麼" })).toBeVisible();
  await tabs.getByRole("link", { name: "排程" }).click();
  await expect(page).toHaveURL(/\/tasks\/jobs$/);
  await expect(tabs.locator('[aria-current="page"]')).toHaveText("排程");
  await expect(
    page.getByText("請秘書定時做的事：幾點做、做什麼，在這裡開關、修改、新增。"),
  ).toBeVisible();
  await expect(rowOf(page, name)).toBeVisible();
  // 開單輸入與「未結／已結束」分段只在「任務單」分頁。
  await expect(page.getByRole("textbox", { name: "交代秘書做什麼" })).toHaveCount(0);
  await expect(page.getByRole("group", { name: "任務單篩選" })).toHaveCount(0);
  await tabs.getByRole("link", { name: "任務單" }).click();
  await expect(page).toHaveURL(/\/tasks$/);
  await expect(tabs.locator('[aria-current="page"]')).toHaveText("任務單");
  await expect(page.getByRole("textbox", { name: "交代秘書做什麼" })).toBeVisible();
  await expect(page.getByRole("group", { name: "任務單篩選" })).toBeVisible();
  await expect(page.getByRole("button", { name: "新增排程" })).toHaveCount(0);
  // 上一頁回到排程。
  await page.goBack();
  await expect(page).toHaveURL(/\/tasks\/jobs$/);
  await expect(rowOf(page, name)).toBeVisible();
});

// /tasks/jobs 是靜態路徑，比 /tasks/$id 優先：jobs 不會被當成任務單編號；真的任務單網址照常開詳情。
// Guards: REQ-18, REQ-107
test("TestJobsRouteNotTaskId", async ({ page, seed }) => {
  await page.goto("/tasks/jobs");
  await expect(page.getByRole("button", { name: "新增排程" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "找不到這張任務單" })).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole("button", { name: "新增排程" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "找不到這張任務單" })).toHaveCount(0);
  const title = `不是排程的單 ${rand()}`;
  const id = await seed.doingTask(title);
  await page.goto(`/tasks/${id}`);
  await expect(page.getByText(title).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "新增排程" })).toHaveCount(0);
});

// 舊網址（排程原本在設定裡）：轉到 /tasks/jobs，?job= 跟著帶過去、不留在上一頁；設定頁沒有排程分頁了。
// Guards: REQ-18, REQ-107
test("TestJobsOldSettingsUrlRedirects", async ({ page, seed }) => {
  const name = `舊連結 ${rand()}`;
  const id = await addJob(seed, name);
  await page.goto(`/settings/jobs?job=${encodeURIComponent(id)}`);
  await expect(page.getByTestId("job-detail")).toBeVisible();
  const url = new URL(page.url());
  expect(url.pathname).toBe("/tasks/jobs");
  expect(url.searchParams.get("job")).toBe(id);
  await expect(page.getByRole("button", { name, exact: true })).toBeVisible();
  // 沒帶 job：轉到排程清單。
  await page.goto("/settings/jobs");
  await expect(page.getByRole("button", { name: "新增排程" })).toBeVisible();
  const list = new URL(page.url());
  expect(list.pathname).toBe("/tasks/jobs");
  expect(list.searchParams.get("job")).toBeNull();
  await expect(page.getByRole("navigation", { name: "設定分頁" })).toHaveCount(0);
});
