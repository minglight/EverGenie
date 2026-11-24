import { expect, type Page, type Seed, test } from "./fixtures";

// 不吵的通知（P7-6、D-34）：工作 session「還在等你」的提醒不是訊息，依主人正在看哪裡出現在頁內卡片或站內提示；
// 任一處回答，其他地方一起收。推播與 2 分鐘改推播在 Go 測試（假時鐘）；這裡驗看板這一端。
// 計時用 page.clock（TST-13），不真的等。

const rand = () => Math.random().toString(36).slice(2, 8);
const num = (id: string) => id.replace("SEC-", "");

// 開一張單的工作 session，回單號與 session 名稱。同一個 worker 共用一個家：先把前面的測試留下的工作 session 收掉，
// 不然超過 work_max 的會排隊、變成「要你回覆」的提問。
async function workThread(seed: Seed) {
  await seed.closeAllWork();
  const task = await seed.doingTask(`提醒測試 ${rand()}`);
  await seed.api("/api/session/open", { agent: "code", topic: task, by: { name: "owner(web)" } });
  return { task, session: `code-sec-${num(task)}` };
}

const nudge = (seed: Seed, session: string, task: string) =>
  seed.api<{ nudge: { id: string; surface: string } }>("/api/notify/nudge", {
    agent: "code",
    session,
    task,
    title: `${task} 還在等你`,
    context: "整理三月報表",
    todo: "請確認要不要上線",
    by: { name: "code" },
  });

// 看板回報在看哪裡：等到伺服器收到這一頁的回報，再讓秘書發提醒，結果才是確定的。
const reported = (page: Page, match: (body: { path?: string; thread?: string }) => boolean) =>
  page.waitForResponse((r) => {
    if (!r.url().includes("/api/notify/presence")) return false;
    try {
      return match(r.request().postDataJSON() ?? {});
    } catch {
      return false;
    }
  });

// Guards: REQ-225, REQ-227, REQ-228
test("TestNudgeInlineCard @smoke", async ({ page, seed }) => {
  test.setTimeout(90_000);
  const { task, session } = await workThread(seed);
  const seen = reported(page, (b) => b.thread === session);
  await page.goto(`/chat/${session}`);
  await seen;

  // 主人正在看那個討論串：頁內黃色卡片，不是站內提示。
  const res = await nudge(seed, session, task);
  expect(res.nudge.surface).toBe("inline");
  const card = page.getByTestId("nudge-card");
  await expect(card).toBeVisible({ timeout: 20_000 });
  await expect(card).toContainText(`${task} 還在等你`);
  await expect(card).toContainText("請確認要不要上線");
  await expect(card).toContainText("2 分鐘沒回覆，會改推到手機");
  await expect(page.getByTestId("nudge-toast")).toHaveCount(0);

  // 按「知道了」：卡片收掉。
  await card.getByRole("button", { name: "知道了" }).click();
  await expect(page.getByTestId("nudge-card")).toHaveCount(0);
});

// Guards: REQ-225, REQ-227
test("TestNudgeToastAndAnsweredElsewhere", async ({ page, seed }) => {
  test.setTimeout(90_000);
  const { task, session } = await workThread(seed);
  const seen = reported(page, (b) => b.path === "/reminders");
  await page.goto("/reminders");
  await seen;

  // 看板開著、在別頁：站內提示。
  const res = await nudge(seed, session, task);
  expect(res.nudge.surface).toBe("toast");
  const toast = page.getByTestId("nudge-toast");
  await expect(toast).toBeVisible({ timeout: 20_000 });
  await expect(toast).toContainText(`${task} 還在等你`);

  // 去看看：到那個討論串，提示變成頁內卡片。
  await toast.getByRole("link", { name: "去看看" }).click();
  await expect(page).toHaveURL(new RegExp(`/chat/${session}`));
  await expect(page.getByTestId("nudge-card")).toBeVisible({ timeout: 20_000 });
  await expect(page.getByTestId("nudge-toast")).toHaveCount(0);

  // 在別處（這裡用 API 假裝手機上回話）回答：卡片一起收掉。
  await seed.api("/api/bus/send", {
    from: "owner(web)",
    to: session,
    kind: "chat",
    body: "上線吧",
  });
  await expect(page.getByTestId("nudge-card")).toHaveCount(0, { timeout: 20_000 });
});

// Guards: REQ-227
test("TestNudgeToastRetractsWhenTaskMoves", async ({ page, seed }) => {
  test.setTimeout(90_000);
  const { task, session } = await workThread(seed);
  // 單要先在「等你回覆」，離開它才算回答。
  await seed.api("/api/tasks/update", {
    id: task,
    status: "waiting_user",
    ask: "要不要上線？",
    by: { name: "code" },
  });
  const seen = reported(page, (b) => b.path === "/reminders");
  await page.goto("/reminders");
  await seen;
  await nudge(seed, session, task);
  await expect(page.getByTestId("nudge-toast")).toBeVisible({ timeout: 20_000 });
  await seed.api("/api/tasks/update", { id: task, status: "doing", by: { name: "code" } });
  await expect(page.getByTestId("nudge-toast")).toHaveCount(0, { timeout: 20_000 });
});

// Guards: REQ-228
test("TestPresenceHeartbeat", async ({ page, seed }) => {
  test.setTimeout(60_000);
  const { session } = await workThread(seed);
  const posts: { path?: string; thread?: string; away?: boolean }[] = [];
  page.on("request", (r) => {
    if (r.url().includes("/api/notify/presence")) posts.push(r.postDataJSON());
  });
  await page.clock.install();
  await page.goto(`/chat/${session}`);
  await expect.poll(() => posts.length, { timeout: 20_000 }).toBeGreaterThanOrEqual(1);
  expect(posts[0]).toMatchObject({ path: `/chat/${session}`, thread: session, away: false });

  // 30 秒後再報一次；到別頁馬上報新的位置（舊的那頁離開時另外報一次「不在了」，不用等 90 秒過期）。
  const before = posts.length;
  await page.clock.runFor(31_000);
  await expect.poll(() => posts.length).toBeGreaterThan(before);
  await page.goto("/reminders");
  await expect
    .poll(() => posts.some((p) => p.path === "/reminders" && p.thread === "" && p.away === false))
    .toBe(true);
  expect(posts.some((p) => p.away === true)).toBe(true);
});

// Guards: REQ-228
test("TestWorkCacheCountdown", async ({ page, seed }) => {
  test.setTimeout(90_000);
  const { session } = await workThread(seed);
  // 讓假引擎跑完一輪：上一輪結束的時間就是 hot cache 倒數的起點。
  await seed.api("/api/bus/send", { from: "owner(web)", to: session, kind: "chat", body: "開工" });
  await page.clock.install();
  await page.goto(`/chat/${session}`);
  const status = page.getByTestId("thread-status");
  await expect(status).toContainText("快取剩", { timeout: 30_000 });
  await expect(status).toContainText(/快取剩 (59|60) 分/);

  // 過了 55 分鐘：只剩幾分鐘，換成「快過期」的說法。
  await page.clock.fastForward("55:00");
  await expect(status).toContainText("快取快過期了", { timeout: 20_000 });
  // 超過 60 分鐘：倒數消失（cache 已經冷了，不再顯示）。
  await page.clock.fastForward("10:00");
  await expect(status).not.toContainText("快取", { timeout: 20_000 });
});
