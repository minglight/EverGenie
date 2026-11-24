import type { APIRequestContext } from "@playwright/test";
import { expect, test } from "./fixtures";

// 官方套件 habits 的頁面（P6-4a）：資料經看板的 vault 代理寫進去（只限套件的種類）；
// status、wallet 是套件的程式（bin/evergenie-habits，核心啟動）算好寫進 vault 的，所以打卡後要等幾秒才會更新。
// 同一個 worker 的家是共用的：每個測試開頭把習慣與打卡全部封存，從乾淨的狀態開始。

const pad = (n: number) => String(n).padStart(2, "0");
const dayOf = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
/** 這週的週一。 */
function thisMonday(): Date {
  const d = new Date();
  d.setDate(d.getDate() - ((d.getDay() + 6) % 7));
  return d;
}

interface Rec {
  id: string;
  fields: Record<string, unknown>;
}

function client(request: APIRequestContext, base: string) {
  const post = async (route: string, data: unknown) => {
    const res = await request.post(`${base}/api/${route}`, { data });
    const json = (await res.json().catch(() => ({}))) as Record<string, unknown>;
    expect(res.status(), `${route} ${JSON.stringify(json)}`).toBeLessThan(300);
    return json;
  };
  const list = async (kind: string) =>
    ((await post("vault/list", { kind, include_archived: false })) as { records?: Rec[] })
      .records ?? [];
  return {
    post,
    list,
    get: async (kind: string, id: string) =>
      ((await post("vault/get", { kind, id })) as { record: Rec }).record,
    reset: async () => {
      for (const kind of ["habits.habit", "habits.checkin"]) {
        for (const r of await list(kind)) await post("vault/archive", { kind, id: r.id });
      }
    },
    add: async (kind: string, fields: Record<string, unknown>, body?: string) => {
      const id = String((await post("vault/next_id", { kind })).id);
      await post("vault/put", { record: { kind, id, fields, ...(body ? { body } : {}) } });
      return id;
    },
  };
}

const weeklyHabit = (over: Record<string, unknown> = {}) => ({
  name: "重訓",
  agent: "code",
  cadence: "weekly",
  target: 3,
  mode: "cumulative",
  start: dayOf(thisMonday()),
  status: "active",
  ...over,
});

// Guards: REQ-124
test("TestHabitCheckinUpdatesCard @smoke", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  const id = await api.add("habits.habit", weeklyHabit());
  const sub = await api.add("habits.habit", {
    name: "照顧自己",
    agent: "code",
    cadence: "daily",
    mode: "cumulative",
    start: dayOf(thisMonday()),
    status: "active",
    subtasks: ["拉筋", "量體重=body:weight"],
    allow_miss: 1,
  });

  await page.goto("/p/habits/list");
  await expect(page.getByTestId("habits-list")).toBeVisible();
  await expect(page.getByTestId("habits-wallet")).toContainText("我的點數");
  const card = page.locator(`[data-testid="habit-card"][data-habit="${id}"]`);
  await expect(card).toContainText("重訓");
  await expect(card).toContainText("約的");
  // 程式算好 status 之後才有本週進度。
  await expect(card.getByTestId("habit-progress-text")).toHaveText("本週 0 / 3 次");
  // 怎樣算完成一行、點數只寫下一步（DEV-156 U-14）；「怎麼算」跳出三段完整規則（DEV-142）。
  await expect(card.getByTestId("habit-done-rule")).toHaveText("每週 3 次・做完自己按打卡");
  await expect(card.getByTestId("habit-point-rule")).toHaveText("再達標 2 週就拿到 1 點");
  await expect(card.getByTestId("habit-rules")).toHaveText("怎麼算");
  await card.getByTestId("habit-rules").click();
  await expect(page.getByTestId("rules-points")).toContainText("連續 4 週都達標再 +1");
  for (const part of ["rules-done", "rules-points", "rules-now"])
    await expect(page.getByTestId(part)).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("rules-done")).toHaveCount(0);

  await card.getByTestId("habit-checkin").click();
  await expect(card.getByTestId("habit-progress-text")).toHaveText("本週 1 / 3 次");
  await expect(card.getByTestId("habit-checkin")).toHaveText("今天已打卡 ✓");
  await expect(card.getByTestId("habit-checkin")).toBeDisabled();
  // 打卡寫成一筆 manual 的 habits.checkin。
  const rows = (await api.list("habits.checkin")).filter((r) => r.fields.habit === id);
  expect(rows).toHaveLength(1);
  expect(rows[0]?.fields).toMatchObject({ source: "manual", date: dayOf(new Date()) });

  // 撤銷今天：再寫一筆 undo。
  await card.getByTestId("habit-undo").click();
  await expect(card.getByTestId("habit-progress-text")).toHaveText("本週 0 / 3 次");
  await expect(card.getByTestId("habit-checkin")).toHaveText("今天打卡");
  await expect(card.getByTestId("habit-undo")).toHaveCount(0);

  // 有子項目：逐項勾；最多可漏 1 項，所以做一項就算當天打卡。
  const subCard = page.locator(`[data-testid="habit-card"][data-habit="${sub}"]`);
  await expect(subCard.getByTestId("habit-subs")).toBeVisible();
  await subCard.getByRole("button", { name: "打卡 拉筋" }).click();
  await expect(subCard.getByRole("button", { name: "已做 拉筋（再按一下撤銷）" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await expect(subCard.getByTestId("habit-progress-text")).toHaveText("本週 1 / 7 天");
  await subCard.getByRole("button", { name: "已做 拉筋（再按一下撤銷）" }).click();
  await expect(subCard.getByTestId("habit-progress-text")).toHaveText("本週 0 / 7 天");
  await api.reset();
});

// Guards: REQ-124
test("TestHabitEditValidation", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  const id = await api.add(
    "habits.habit",
    weeklyHabit({ identity: "會運動的人", cue: "下班之後" }),
    "約定時聊到：週一三五去健身房。",
  );

  await page.goto(`/p/habits/detail?id=${id}`);
  await expect(page.getByTestId("habit-detail")).toBeVisible();
  await expect(page.getByTestId("habit-summary")).toContainText("重訓");
  await expect(page.getByTestId("habit-notes")).toContainText("會運動的人");
  await expect(page.getByTestId("habit-notes")).toContainText("週一三五去健身房");
  // 近幾週的格子：程式算好之後才有。
  await expect(page.getByTestId("week-cell").first()).toBeVisible();

  const target = page.getByTestId("edit-target");
  await target.fill("9");
  await page.getByTestId("edit-save").click();
  await expect(page.getByTestId("edit-error")).toContainText("每週幾次要在 1 到 7 之間");
  expect((await api.get("habits.habit", id)).fields.target).toBe(3);

  // 修改約定只剩常用欄位（DEV-142）；連續加分週數只在約定時跟秘書定，頁面不能改。
  await expect(page.getByTestId("edit-allow-miss")).toHaveCount(0);
  await expect(page.getByTestId("edit-miss-days")).toHaveCount(0);
  await expect(page.getByTestId("edit-bonus-weeks")).toHaveCount(0);
  await target.fill("4");
  await page.getByTestId("edit-save").click();
  await expect(page.getByTestId("edit-saved")).toBeVisible();
  await expect(page.getByTestId("edit-error")).toHaveCount(0);
  const saved = (await api.get("habits.habit", id)).fields;
  expect(saved.target).toBe(4);
  expect(saved).not.toHaveProperty("bonus_weeks");
  // 詳情頁有規則說明。
  await expect(page.getByTestId("habit-rules-card")).toContainText("連續 4 週都達標再 +1");
  // 跟約這個習慣的秘書討論修改：開她的聊天、起好頭。
  await page.getByTestId("habit-rules-card").getByTestId("habit-discuss").click();
  await expect(page).toHaveURL(/\/chat\/code$/);
  await expect(page.getByRole("combobox", { name: /傳訊息給/ })).toHaveValue(
    `我想調整習慣「重訓」（${id}）：`,
  );
  await page.goto(`/p/habits/detail?id=${id}`);

  // 結束：寫下結束日，清單把它收進「已結束」。
  await page.getByRole("group", { name: "狀態" }).getByRole("button", { name: "已結束" }).click();
  await page.getByTestId("edit-save").click();
  await expect(page.getByTestId("edit-saved")).toBeVisible();
  expect((await api.get("habits.habit", id)).fields).toMatchObject({
    status: "ended",
    ended: dayOf(new Date()),
  });
  await page.goto("/p/habits/list");
  await expect(page.getByTestId("habits-ended")).toContainText("已結束（1）");
  await expect(page.getByTestId("habit-card")).toHaveCount(0);
  await api.reset();
});

// Guards: REQ-114, REQ-124
test("TestHabitsTodayCard", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  await page.goto("/");
  await expect(page.getByTestId("habits-today")).toContainText("還沒有約定習慣");

  const a = await api.add("habits.habit", weeklyHabit());
  await api.add("habits.habit", weeklyHabit({ name: "拉筋", target: 1 }));
  await api.add("habits.habit", weeklyHabit({ name: "舊習慣", status: "ended" }));
  await page.goto("/");
  const card = page.getByTestId("habits-today");
  await expect(card).toBeVisible();
  // 標題固定「今天的習慣」；「今天只有一件事」是內文（DEV-156 U-13）。
  await expect(card.getByRole("heading", { level: 2 })).toHaveText("今天的習慣");
  await expect(card.getByTestId("today-one")).toContainText("今天只有一件事");
  await expect(card.getByTestId("today-one")).toHaveAttribute("data-habit", a);
  await expect(card.getByTestId("today-points")).toBeVisible();

  // 打一個卡（寫 checkin，像秘書在聊天裡幫你打），程式算完之後今天就夠了；卡片自己更新、不用重新整理（PKG-49）。
  await api.add("habits.checkin", {
    habit: a,
    date: dayOf(new Date()),
    source: "manual",
    key: `manual:${a}:${dayOf(new Date())}:`,
  });
  await expect(card.getByTestId("today-enough")).toBeVisible({ timeout: 20_000 });
  // 別的頁面沒有這個 slot，不會冒出卡片。
  await page.goto("/messages");
  await expect(page.getByTestId("habits-today")).toHaveCount(0);
  await page.goto("/");
  await page.getByTestId("today-all").click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/p/habits/list");
  await expect(page.getByTestId("habits-list")).toBeVisible();
  await api.reset();
});

// 月曆打卡（HAB-14）：點一下打卡、再點撤銷，未來的日子不能點；今天頁一鍵「做了」（HAB-15）。
test("月曆打卡與今天只有一件事", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  const today = dayOf(new Date());
  const id = await api.add("habits.habit", {
    ...weeklyHabit({ name: "散步", two_min: "換上鞋子" }),
    cadence: "daily",
    start: today,
  });
  await page.goto(`/p/habits/detail?id=${id}`);
  const cell = page.locator(`[data-testid="calendar-day"][data-date="${today}"]`);
  await expect(cell).toBeEnabled();
  await cell.click();
  await expect(cell).toHaveAttribute("data-state", "done", { timeout: 20_000 });
  await cell.click();
  await expect(cell).toHaveAttribute("data-state", "open", { timeout: 20_000 });
  await page.goto("/");
  const one = page.getByTestId("today-one");
  await expect(one).toContainText("散步");
  await expect(one).toContainText("換上鞋子");
  await page.getByTestId("today-one-done").click();
  await expect(page.getByTestId("today-enough")).toBeVisible();
  // 清單卡片的本週七格跟著打勾（HAB-17）
  await page.goto("/p/habits/list");
  const day = page.locator(`[data-testid="habit-week-day"][data-date="${today}"]`);
  await expect(page.getByTestId("habit-week-day")).toHaveCount(7);
  await expect(day).toHaveAttribute("data-state", "done", { timeout: 20_000 });
  await expect(day).toContainText("✓");
  await api.reset();
});

// 今天不做：沒做也能把卡片收起來到明天，不記紀錄（那天照規則自動算跳過或斷了）；重新整理也不再冒出來。
test("今天不做把卡片收到明天", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  const id = await api.add("habits.habit", { ...weeklyHabit({ name: "早睡" }), cadence: "daily" });
  await page.goto("/");
  const one = page.getByTestId("today-one");
  await expect(one).toHaveAttribute("data-habit", id);
  await expect(one).toContainText("不按也沒關係");
  await page.getByTestId("today-one-off").click();
  await expect(page.getByTestId("today-off")).toBeVisible();
  await expect(page.getByTestId("today-one")).toHaveCount(0);
  await page.reload();
  await expect(page.getByTestId("today-off")).toBeVisible();
  await expect(page.getByTestId("today-one")).toHaveCount(0);
  // 沒有寫任何打卡
  await page.goto(`/p/habits/detail?id=${id}`);
  const cell = page.locator(`[data-testid="calendar-day"][data-date="${dayOf(new Date())}"]`);
  await expect(cell).toHaveAttribute("data-state", "open");
  await api.reset();
});

// 待辦累積（HAB-18、DEV-149）：完成「問我做了沒」的待辦就往上加；滿 30 按「換成 1 點」（DEV-156 U-14 改說法）。
test("待辦點數在待辦頁往上加、滿 30 可以兌換", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  const wallet = async () =>
    (await api.get("habits.wallet", "main").catch(() => null))?.fields ?? {};
  const before = await wallet();
  const total0 = Number(before.task_points ?? 0);
  const left0 = total0 - Number(before.task_exchanged ?? 0);
  // 困難的「問我做了沒」一筆 3 點（只有它和「秘書幫我確認」集點）：做到還沒換的至少 31 點
  const need = Math.max(Math.ceil((31 - left0) / 3), 1);
  for (let i = 0; i < need; i++) {
    const r = (await api.post("reminders/create", {
      content: `e2e 點數 ${i}`,
      follow: "confirm",
      at: new Date().toISOString(),
      difficulty: "hard",
    })) as {
      reminder: { id: string };
    };
    await api.post("reminders/done", { id: r.reminder.id });
  }
  await page.goto("/reminders");
  const card = page.getByTestId("todo-points");
  // 畫面上只有一種點數（DEV-156 U-14）：進度條＋一句下一步，滿了才有「換成 1 點」；規則收在「怎麼算」。
  await expect(card.getByTestId("todo-points-next")).toContainText("累積滿了，可以換", {
    timeout: 20_000,
  });
  await expect(card.getByTestId("todo-points-progress")).toHaveAttribute("aria-valuenow", "30");
  await expect(card).not.toContainText("待辦點數");
  await expect(card).not.toContainText("衣服點數");
  await card.getByText("怎麼算").click();
  await expect(card.getByTestId("todo-points-how")).toContainText("簡單 1、普通 2、困難 3");
  await card.getByTestId("todo-points-exchange").click();
  await expect(card).toContainText("換好了");
  await expect
    .poll(async () => Number((await wallet()).task_exchanged ?? 0), { timeout: 20_000 })
    .toBe(Number(before.task_exchanged ?? 0) + 30);
  // 總數只增不減，換掉的那份從進度扣掉。
  expect(Number((await wallet()).task_points ?? 0)).toBe(total0 + need * 3);
});

test("約一個新習慣：選秘書後開聊天並起好頭（DEV-142）", async ({ page, request, baseURL }) => {
  await client(request, baseURL ?? "").reset();
  await page.goto("/p/habits/list");
  await page.getByTestId("habit-new").click();
  const pick = page.getByTestId("habit-new-agent").first();
  await expect(pick).toBeVisible();
  const agent = await pick.getAttribute("data-agent");
  await pick.click();
  await expect(page).toHaveURL(new RegExp(`/chat/${agent}$`));
  await expect(page.getByRole("combobox", { name: /傳訊息給/ })).toHaveValue("我想約一個新習慣：");
  // 抽屜裡的連結走站內路由（不整頁重載），上一頁回得到習慣頁（FEN-29）。
  await page.goBack();
  await expect(page.getByTestId("habits-list")).toBeVisible();
});

// 第一天（DEV-157）：還沒有習慣時，今天頁的習慣卡就有「約一個新習慣」，跟習慣頁同一顆（選秘書、起好頭、開聊天）。
test("今天頁還沒有習慣時按得到「約一個新習慣」", async ({ page, request, baseURL }) => {
  await client(request, baseURL ?? "").reset();
  await page.goto("/");
  const card = page.getByTestId("habits-today");
  await expect(card).toContainText("還沒有約定習慣");
  await card.getByTestId("habit-new").click();
  const pick = page.getByTestId("habit-new-agent").first();
  await expect(pick).toBeVisible();
  const agent = await pick.getAttribute("data-agent");
  await pick.click();
  await expect(page).toHaveURL(new RegExp(`/chat/${agent}$`));
  await expect(page.getByRole("combobox", { name: /傳訊息給/ })).toHaveValue("我想約一個新習慣：");
});

// 只有一位秘書（DEV-157 新家第一天走查④）：「約一個新習慣」不先問跟誰，按了直接開那位的聊天、起好頭。
// 暫時停用 code 讓名冊只剩 life；結束一定開回來（同一個 worker 的別的測試看的是原本的名冊）。
test("只有一位秘書時「約一個新習慣」直接開她的聊天", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  const enable = (value: string) =>
    api.post("agents/set_field", {
      id: "code",
      field: "enabled",
      value,
      by: { name: "owner(web)" },
    });
  await enable("false");
  try {
    await page.goto("/p/habits/list");
    const button = page.getByTestId("habit-new");
    await expect(button).toHaveAttribute("data-agent", "life");
    await button.click();
    await expect(page.getByRole("dialog", { name: "跟誰約？" })).toHaveCount(0);
    await expect(page).toHaveURL(/\/chat\/life$/);
    await expect(page.getByRole("combobox", { name: /傳訊息給/ })).toHaveValue(
      "我想約一個新習慣：",
    );
  } finally {
    await enable("true").catch(() => undefined);
  }
});

// 點數拿去展示間換衣服（DEV-157）：習慣頁的錢包卡連到展示間（站內路由，上一頁回得到習慣頁）。
test("習慣頁的錢包卡點得到展示間", async ({ page }) => {
  await page.goto("/p/habits/list");
  await expect(page.getByTestId("habits-wallet")).toContainText("我的點數");
  await page.getByTestId("wallet-showroom").click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/p/closet/showroom");
  await expect(page.getByTestId("showroom")).toBeVisible();
  await page.goBack();
  await expect(page.getByTestId("habits-list")).toBeVisible();
});

test("自動來源的習慣不放大打卡鈕，只有小的手動補（DEV-142）", async ({
  page,
  request,
  baseURL,
}) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  const id = await api.add("habits.habit", weeklyHabit({ target: 2, sources: ["fitness:重訓"] }));
  await page.goto("/p/habits/list");
  const card = page.locator(`[data-testid="habit-card"][data-habit="${id}"]`);
  await expect(card.getByTestId("habit-done-rule")).toHaveText("每週 2 次・記了重訓就自動算");
  await expect(card.getByTestId("habit-auto")).toHaveText("自動算，不用按");
  await expect(card.getByTestId("habit-checkin")).toHaveText("沒記到？手動補今天");
});
