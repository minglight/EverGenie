import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { expect, type Seed, test } from "./fixtures";

// 工作 session（P7-5、D-33）：一張任務單一個，列在秘書底下；每台同時 doing 的超過 work_max 時，
// 主人開的單在那張單的討論串裡問（照開／先排隊／關掉…），不推播。
// 同一個 worker 共用一個家：先把前面的測試留下的工作 session 收掉，再把上限設成 2，結束時還原。

const rand = () => Math.random().toString(36).slice(2, 8);
const num = (id: string) => id.replace("SEC-", "");

interface WorkSession {
  name: string;
  kind: string;
  task?: string;
  state: string;
  queued_at?: string;
}

async function sessionOf(seed: Seed, name: string): Promise<WorkSession | undefined> {
  const res = await seed.api<{ sessions?: WorkSession[] }>("/api/session/list", {});
  return (res.sessions ?? []).find((s) => s.name === name);
}

// 看板開對話：同 session.open 帶單號，身分是主人（看板代理會換成 owner(web)）。
const openRoom = (seed: Seed, task: string, by = "owner(web)") =>
  seed.api<{ session: WorkSession }>("/api/session/open", {
    agent: "code",
    topic: task,
    by: { name: by },
  });

// Guards: REQ-204, REQ-220, REQ-221
test("TestChatWorkSessions", async ({ page, seed, hgServer }) => {
  test.setTimeout(120_000);
  const settings = path.join(hgServer.home, "settings", "sessions.toml");
  mkdirSync(path.dirname(settings), { recursive: true });
  await seed.closeAllWork();
  writeFileSync(settings, "work_max = 2\n");
  try {
    const one = await seed.doingTask(`工作一 ${rand()}`);
    const two = await seed.doingTask(`工作二 ${rand()}`);
    const three = await seed.doingTask(`工作三 ${rand()}`);
    await openRoom(seed, one);
    await openRoom(seed, two);

    // 秘書底下展開她的工作 session：單號、標題、狀態。
    await page.goto("/chat");
    const list = page.getByRole("list", { name: /的工作 session/ });
    await expect(list.locator(`[data-work-task="${one}"]`)).toBeVisible({ timeout: 20_000 });
    await expect(list.locator(`[data-work-task="${two}"]`)).toBeVisible();

    // 第三張超過上限：先不開，在那張單的討論串裡問。
    const asked = await openRoom(seed, three);
    expect(asked.session.state).toBe("stopped");
    expect(asked.session.queued_at).toBeTruthy();
    // 照開／先排隊是核心送的通知（approval），照名詞表算「要你同意」（DEV-156 U-10）。
    await expect(list.locator(`[data-work-task="${three}"]`)).toContainText("要你同意", {
      timeout: 20_000,
    });
    await page.goto(`/chat/code-sec-${num(three)}`);
    const ask = page.getByTestId("work-ask");
    await expect(ask).toBeVisible({ timeout: 20_000 });
    await expect(ask).toContainText(one);
    await expect(ask).toContainText(two);
    await expect(ask.getByRole("button", { name: "照開" })).toBeVisible();
    await expect(ask.getByRole("button", { name: "先排隊" })).toBeVisible();
    await expect(ask.getByRole("button", { name: `關掉 ${one}` })).toBeVisible();
    // 不推播、不是一般的訊息卡：只有這個討論串有。
    await page.goto("/chat");

    // 先排隊：卡片消失，還是沒開；討論串寫「排隊等名額」。
    await page.goto(`/chat/code-sec-${num(three)}`);
    await page.getByTestId("work-ask").getByRole("button", { name: "先排隊" }).click();
    await expect(page.getByTestId("work-ask")).toHaveCount(0, { timeout: 20_000 });
    expect((await sessionOf(seed, `code-sec-${num(three)}`))?.state).toBe("stopped");
    // 排隊等名額不是在等你（S5 D6）：清單那一列是 queued、寫「排隊等名額」。
    await page.goto("/chat");
    const queuedRow = list.locator(`[data-work-task="${three}"]`);
    await expect(queuedRow).toHaveAttribute("data-thread-status", "queued", { timeout: 20_000 });
    await expect(queuedRow.getByTestId("work-status")).toHaveText("排隊等名額");

    // 工作一送驗收 → 空出名額 → 排隊的自動開起來（單一輪結束、閒了才關，所以要等一下）。
    await seed.api("/api/tasks/submit", {
      id: one,
      result: "做完了",
      verify: "verified",
      verify_how: "跑過測試",
      proposed: "done",
      by: { name: "code" },
    });
    await expect
      .poll(async () => (await sessionOf(seed, `code-sec-${num(one)}`))?.state, { timeout: 60_000 })
      .toBe("stopped");
    await expect
      .poll(async () => (await sessionOf(seed, `code-sec-${num(three)}`))?.state, {
        timeout: 60_000,
      })
      .not.toBe("stopped");
  } finally {
    rmSync(settings, { force: true });
    await seed.closeAllWork();
  }
});

// Guards: REQ-220
test("TestChatWorkAskCloseOne", async ({ page, seed, hgServer }) => {
  test.setTimeout(90_000);
  const settings = path.join(hgServer.home, "settings", "sessions.toml");
  mkdirSync(path.dirname(settings), { recursive: true });
  await seed.closeAllWork();
  writeFileSync(settings, "work_max = 1\n");
  try {
    const one = await seed.doingTask(`佔名額 ${rand()}`);
    const two = await seed.doingTask(`要開的 ${rand()}`);
    await openRoom(seed, one);
    await openRoom(seed, two);
    await page.goto(`/chat/code-sec-${num(two)}`);
    const ask = page.getByTestId("work-ask");
    await expect(ask).toBeVisible({ timeout: 20_000 });
    await ask.getByRole("button", { name: `關掉 ${one}` }).click();
    await expect(page.getByTestId("work-ask")).toHaveCount(0, { timeout: 20_000 });
    // 被點名的那個關掉（排回佇列），這張單開起來。
    await expect
      .poll(async () => (await sessionOf(seed, `code-sec-${num(two)}`))?.state, { timeout: 30_000 })
      .not.toBe("stopped");
    const closed = await sessionOf(seed, `code-sec-${num(one)}`);
    expect(closed?.state).toBe("stopped");
    expect(closed?.queued_at).toBeTruthy();
  } finally {
    rmSync(settings, { force: true });
    await seed.closeAllWork();
  }
});
