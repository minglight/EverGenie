import { copyFileSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { Page } from "@playwright/test";
import type { Seed } from "./fixtures";
import { expect, type HgServer, shot, test } from "./fixtures";

// 截圖用的資料：標題固定，已經有了就不再造（手機與桌機兩個 project 共用同一個家）。
const approvals = [
  { title: "更新開發環境的小版本", agent: "code" },
  { title: "要不要訂這週的健身課", agent: "life" },
];
const reviewTitle = "看板外殼做完了，請驗收";
// 秘書在問你的單：問句下面就地回答（DEV-156 U-05）。
const askTitle = "週會時間要不要改";
const waitingChat = "【權限】幫我建立一個檔案";
// 討論串截圖要同時有：user 泡泡、assistant 的 Markdown、收起的工具膠囊、等你同意的卡片。
const markdownChat =
  "幫我整理 **這週的重點**：\n\n- 看板外殼做完了\n- 聊天頁開始做\n\n`make ci` 都過了嗎？";
const doneChat = "晚一點再看也可以";

async function seedShotData(
  seed: Seed,
  request: import("@playwright/test").APIRequestContext,
  base: string,
) {
  const list = await seed.api<{ notifications?: { title: string }[] }>("/api/notify/list", {});
  const have = new Set((list.notifications ?? []).map((n) => n.title));
  for (const a of approvals) {
    if (!have.has(a.title)) await seed.approve(a.title, a.agent);
  }
  const tasks = await seed.api<{ tasks?: { title: string }[] }>("/api/tasks/list", {});
  if (!(tasks.tasks ?? []).some((t) => t.title === reviewTitle))
    await seed.reviewTask(reviewTitle, "code");
  // 讓 life 有一個「要你回覆」的討論串（假引擎收到【權限】會停下來等回答）。
  const threads = await request.post(`${base}/api/session/list_threads`, {
    data: { include_stopped: false },
  });
  const body = (await threads.json()) as { threads?: { agent: string; status: string }[] };
  if (!(body.threads ?? []).some((t) => t.agent === "life" && t.status === "waiting_user")) {
    // 先來一輪普通的對話（有 Markdown 的回覆和一個工具膠囊），再停在要你同意。
    await request.post(`${base}/api/bus/send`, {
      data: { to: "life", body: markdownChat, idempotency_key: "shots-markdown" },
    });
    await expect
      .poll(
        async () => {
          const r = await request.post(`${base}/api/session/list_threads`, {
            data: { include_stopped: false },
          });
          const j = (await r.json()) as { threads?: { agent: string; last_preview?: string }[] };
          return (j.threads ?? []).some(
            (t) => t.agent === "life" && (t.last_preview ?? "").includes("看板外殼做完了"),
          );
        },
        { timeout: 20_000 },
      )
      .toBe(true);
    await request.post(`${base}/api/bus/send`, {
      data: { to: "life", body: waitingChat, idempotency_key: "shots-wait" },
    });
    await expect
      .poll(
        async () => {
          const r = await request.post(`${base}/api/session/list_threads`, {
            data: { include_stopped: false },
          });
          const j = (await r.json()) as { threads?: { agent: string; status: string }[] };
          return (j.threads ?? []).some((t) => t.agent === "life" && t.status === "waiting_user");
        },
        { timeout: 20_000 },
      )
      .toBe(true);
  }
}

// 清單截圖要有「完成了還沒看」的一列：code 回完一輪，之後沒有人打開過。
async function seedDoneThread(request: import("@playwright/test").APIRequestContext, base: string) {
  const read = async () => {
    const r = await request.post(`${base}/api/session/list_threads`, {
      data: { include_stopped: false },
    });
    const j = (await r.json()) as { threads?: { name: string; status: string; unread: number }[] };
    return (j.threads ?? []).find((t) => t.name === "code");
  };
  if ((await read())?.status === "done") return;
  await request.post(`${base}/api/bus/send`, {
    // 前面的截圖（角色聊天）打開過 code 的討論串，「完成還沒看」被清掉了：每次都要能再造一次，所以 key 不固定。
    data: { to: "code", body: doneChat, idempotency_key: `shots-done-${Date.now()}` },
  });
  await expect.poll(async () => (await read())?.status, { timeout: 20_000 }).toBe("done");
}

async function settled(page: Page) {
  await expect(page.getByRole("navigation", { name: "主選單" })).toBeVisible();
  // 長連線（SSE）一直開著，不能等 networkidle；讓 hg-rise 進場動畫跑完，截圖才穩定。
  await page.waitForTimeout(600);
}

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-115
  test(`TestShotsOverview @shots ${scheme}`, async ({ page, seed, request, baseURL, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedShotData(seed, request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/");
    await settled(page);
    await shot(page, `overview-${d}-${scheme}`);

    if (d === "desktop") {
      // 選取第一件的樣子（補充契約 S3 第 14 點）。
      await page.getByRole("button", { name: /更新開發環境的小版本/ }).click();
      await expect(page.getByRole("heading", { name: "更新開發環境的小版本" })).toBeVisible();
      // S5：右欄是完整內容＋固定在底部的動作列（鍵帽 A／R）。
      await expect(
        page.getByRole("toolbar", { name: "選項" }).getByRole("button", { name: /^同意/ }),
      ).toContainText("A");
      await page.waitForTimeout(300);
      await shot(page, `overview-desktop-1280-${scheme}`);
    } else {
      // 「更多」打開的樣子。
      await page.getByRole("button", { name: /更多/ }).click();
      await expect(page.getByRole("dialog", { name: "更多" })).toBeVisible();
      await page.waitForTimeout(400);
      await shot(page, `more-sheet-mobile-${scheme}`);
    }
  });

  // Guards: REQ-115
  test(`TestShotsNotFound @shots ${scheme}`, async ({ page, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/no-such-page");
    await expect(page.getByRole("heading", { name: "找不到這一頁" })).toBeVisible();
    await settled(page);
    await shot(page, `not-found-${d}-${scheme}`);
  });

  // Guards: REQ-115
  test(`TestShotsPalette @shots ${scheme}`, async ({ page, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/");
    await settled(page);
    await page.keyboard.press("Control+k");
    await expect(page.getByRole("dialog", { name: "搜尋或下指令" })).toBeVisible();
    await page.waitForTimeout(300);
    await shot(page, `palette-${d}-${scheme}`);
  });
}

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-115
  test(`TestShotsChatList @shots ${scheme}`, async ({ page, seed, request, baseURL, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedShotData(seed, request, baseURL ?? "");
    await seedDoneThread(request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/chat");
    await expect(page.locator("[data-thread]").first()).toBeVisible();
    await expect(page.locator('[data-thread="life"] [data-dot="waiting"]')).toBeVisible();
    await settled(page);
    await shot(page, `chat-list-${d}-${scheme}`);
  });

  // Guards: REQ-115
  test(`TestShotsChatThread @shots ${scheme}`, async ({
    page,
    seed,
    request,
    baseURL,
    isMobile,
  }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedShotData(seed, request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/chat/life");
    await expect(page.getByRole("region", { name: /想執行/ })).toBeVisible();
    await expect(page.getByText("這週的重點").first()).toBeVisible();
    await expect(page.getByRole("button", { name: /用了 \d+ 個工具/ }).first()).toBeVisible();
    await page.waitForTimeout(600); // 手機的討論串是全頁，沒有選單可等；讓進場動畫跑完
    await shot(page, `chat-thread-${d}-${scheme}`);
  });
}

// ── S5：訊息、任務單、主人待辦 ──────────────────────────────────────────────

// 「繳水費」是昨天的：不管幾點拍，都有一筆「過期了」（紅色，DEV-156 U-17）。
const lateReminder = "繳水費";
const reminderSeeds = [
  { content: lateReminder, at: [9, 0], days: -1 },
  { content: "回覆房東的信", at: [10, 0] },
  { content: "跟團隊 1:1", at: [14, 0] },
  { content: "補維他命 D", at: [20, 0] },
];

// 待辦、任務單、訊息的截圖資料：標題固定，已經有了就不再造。
async function seedS5(seed: Seed) {
  const reminders = await seed.api<{ reminders?: { content: string }[] }>("/api/reminders/list", {
    include_archived: false,
  });
  const have = new Set((reminders.reminders ?? []).map((r) => r.content));
  const now = new Date();
  for (const r of reminderSeeds) {
    if (have.has(r.content)) continue;
    const [h, m] = r.at as [number, number];
    await seed.reminder(
      r.content,
      new Date(now.getFullYear(), now.getMonth(), now.getDate() + (r.days ?? 0), h, m),
    );
  }
  const list = await seed.api<{ notifications?: { title: string }[] }>("/api/notify/list", {});
  const titles = new Set((list.notifications ?? []).map((n) => n.title));
  for (const a of approvals) {
    if (!titles.has(a.title)) await seed.approve(a.title, a.agent);
  }
  if (!titles.has("週報做好了")) {
    await seed.api("/api/notify/send", {
      agent: "life",
      category: "done",
      title: "週報做好了",
      context: "這週的重點都整理在附件裡。",
    });
  }
  const tasks = await seed.api<{ tasks?: { title: string }[] }>("/api/tasks/list", {});
  if (!(tasks.tasks ?? []).some((t) => t.title === reviewTitle)) {
    await seed.reviewTask(reviewTitle, "code");
  }
  if (!(tasks.tasks ?? []).some((t) => t.title === "整理這週的行程")) {
    await seed.doingTask("整理這週的行程", "life");
  }
  if (!(tasks.tasks ?? []).some((t) => t.title === askTitle)) {
    await seed.askTask(askTitle, "週會要改到週四下午嗎？還是維持週三早上？", "life");
  }
}

async function idOfTitle(seed: Seed, kind: "notify" | "tasks", title: string): Promise<string> {
  if (kind === "notify") {
    const res = await seed.api<{ notifications?: { id: string; title: string }[] }>(
      "/api/notify/list",
      {},
    );
    return (res.notifications ?? []).find((n) => n.title === title)?.id ?? "";
  }
  const res = await seed.api<{ tasks?: { id: string; title: string }[] }>("/api/tasks/list", {});
  return (res.tasks ?? []).find((n) => n.title === title)?.id ?? "";
}

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-115
  test(`TestShotsMessages @shots ${scheme}`, async ({ page, seed, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedS5(seed);
    await page.emulateMedia({ colorScheme: scheme });
    // 拍「全部」：預設的「未讀」分頁會因為上一輪（亮色）打開過這則而看不到它（REQ-231）。
    await page.goto("/messages?show=all");
    await expect(page.getByRole("list", { name: "訊息清單" })).toBeVisible();
    await expect(page.getByText("要不要訂這週的健身課").first()).toBeVisible();
    await settled(page);
    await shot(page, `messages-list-${d}-${scheme}`);

    const id = await idOfTitle(seed, "notify", "要不要訂這週的健身課");
    await page.goto(`/messages/${id}`);
    await expect(page.getByRole("button", { name: "同意", exact: true })).toBeVisible();
    await settled(page);
    await shot(page, `message-detail-${d}-${scheme}`);
  });

  // Guards: REQ-115
  test(`TestShotsTasks @shots ${scheme}`, async ({ page, seed, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedS5(seed);
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/tasks");
    await expect(page.getByRole("link", { name: new RegExp(reviewTitle) })).toBeVisible();
    await settled(page);
    await shot(page, `tasks-list-${d}-${scheme}`);

    const id = await idOfTitle(seed, "tasks", reviewTitle);
    await page.goto(`/tasks/${id}`);
    await expect(page.getByRole("button", { name: "驗收", exact: true })).toBeVisible();
    await settled(page);
    await shot(page, `task-detail-${d}-${scheme}`);

    // 秘書在問你：問句下面有回答框，「去聊」是次要的（DEV-156 U-05）。
    const ask = await idOfTitle(seed, "tasks", askTitle);
    await page.goto(`/tasks/${ask}`);
    await expect(page.getByTestId("task-answer")).toBeVisible();
    await settled(page);
    await shot(page, `task-ask-${d}-${scheme}`);
  });

  // Guards: REQ-115
  test(`TestShotsReminders @shots ${scheme}`, async ({ page, seed, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedS5(seed);
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/reminders");
    await expect(page.getByText("回覆房東的信").first()).toBeVisible();
    // 萬用輸入框打了一句話：預覽列（意圖、時間、內容、負責秘書）出現。
    await page.getByLabel("新增待辦").fill("明天 9 點 打給牙醫");
    await expect(page.getByTestId("quick-preview")).toContainText("09:00");
    await settled(page);
    await shot(page, `reminders-list-${d}-${scheme}`);

    await page.goto("/reminders");
    await page.getByText("回覆房東的信").first().click();
    await expect(page.getByRole("button", { name: "做了", exact: true })).toBeVisible();
    await settled(page);
    await shot(page, `reminder-detail-${d}-${scheme}`);

    // 過期的那一筆：狀態寫「過期了」，是紅色的膠囊（跟「做了」的綠、「等你回覆」的黃分得出來）。
    await page.goto("/reminders");
    await page.getByText(lateReminder).first().click();
    const status = page.getByTestId("reminder-status");
    await expect(status).toHaveText("過期了");
    const [red, green] = await status.evaluate((el) => {
      const [r = 0, g = 0] = (getComputedStyle(el).color.match(/[\d.]+/g) ?? []).map(Number);
      return [r, g];
    });
    expect(red, "「過期了」要是紅色").toBeGreaterThan(green);
    // 下面「狀態」欄也寫「過期了」，跟膠囊同一個字（DEV-156 U-17 尾巴）。
    await expect(page.getByTestId("reminder-status-field")).toHaveText("過期了");
    await settled(page);
    await shot(page, `reminder-late-${d}-${scheme}`);
  });

  // Guards: REQ-115
  test(`TestShotsOverviewActions @shots ${scheme}`, async ({ page, seed, isMobile }, info) => {
    info.skip(!isMobile, "桌機的右欄（完整內容＋動作列）在 overview-desktop-1280 那張");
    await seedS5(seed);
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/");
    await settled(page);
    // 今天頁的卡片：選項按鈕、驗收／退回；萬用輸入框打了一句話。
    await expect(page.locator("[data-card]").first()).toBeVisible();
    await page.getByLabel("快速輸入").fill("明天 9 點 打給牙醫");
    await expect(page.getByTestId("quick-preview")).toContainText("09:00");
    await page.waitForTimeout(300);
    await shot(page, `overview-actions-mobile-${scheme}`);
  });
}

// Guards: REQ-115
test("TestShotsToastUndo @shots", async ({ page, seed, isMobile }, info) => {
  info.skip(!isMobile, "toast 的截圖只拍手機（補充契約 S5 第 7 點）");
  await seedS5(seed);
  const content = "toast 截圖用的待辦";
  await seed.reminder(content, new Date(new Date().setHours(21, 0, 0, 0)));
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/reminders");
  const row = page.locator("ul > li").filter({ hasText: content });
  await row.getByRole("button", { name: "標成做完" }).click();
  await expect(page.getByText(`做完了：${content}`)).toBeVisible();
  await page.waitForTimeout(500);
  await shot(page, "toast-undo-mobile-light");
  await page.getByRole("button", { name: "復原", exact: true }).click();
  await expect(row).toBeVisible();
});

// Guards: REQ-115
test("TestShotsRemindersTablet @shots", async ({ page, seed, isMobile }, info) => {
  info.skip(isMobile, "768–1023px 的缺口只在桌機 project 用改視窗寬度的方式拍");
  await seedS5(seed);
  await page.setViewportSize({ width: 820, height: 1180 });
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/reminders");
  await expect(page.getByText("回覆房東的信").first()).toBeVisible();
  await page.getByLabel("新增待辦").fill("明天 9 點 打給牙醫");
  await expect(page.getByTestId("quick-preview")).toContainText("09:00");
  await settled(page);
  await shot(page, "reminders-list-tablet-light");
});

// ── S6：設定的四個分頁 ──────────────────────────────────────────────────────

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-115
  test(`TestShotsSettings @shots ${scheme}`, async ({ page, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await page.emulateMedia({ colorScheme: scheme });

    await page.goto("/settings/notify");
    await expect(page.getByRole("group", { name: "摘要的送法" })).toBeVisible();
    await settled(page);
    await shot(page, `settings-notify-${d}-${scheme}`);

    await page.goto("/settings/trust");
    await expect(page.getByTestId("trust-repo").first()).toBeVisible();
    await settled(page);
    await shot(page, `settings-trust-${d}-${scheme}`);

    await page.goto("/settings/devices");
    await expect(page.getByText("推播還沒準備好").first()).toBeVisible();
    await settled(page);
    await shot(page, `settings-devices-${d}-${scheme}`);

    await page.goto("/settings/health");
    await expect(page.getByTestId("health-item").first()).toBeVisible();
    await settled(page);
    await shot(page, `settings-health-${d}-${scheme}`);
  });
}

// ── P6-2b：設定的新分頁（選單、套件、職位與秘書、電腦、設定精靈）──────────────────────

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-115
  test(`TestShotsSettingsP62b @shots ${scheme}`, async ({ page, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await page.emulateMedia({ colorScheme: scheme });

    await page.goto("/settings/menu");
    await expect(page.getByTestId("menu-group").first()).toBeVisible();
    await settled(page);
    await shot(page, `settings-menu-${d}-${scheme}`);

    await page.goto("/settings/packages");
    await expect(page.getByTestId("package-demo")).toBeVisible();
    await expect(page.getByRole("textbox", { name: "問候語" })).toBeVisible();
    await settled(page);
    await shot(page, `settings-packages-${d}-${scheme}`);

    await page.goto("/settings/agents");
    await expect(page.getByTestId("agent-row").first()).toBeVisible();
    await settled(page);
    await shot(page, `settings-agents-${d}-${scheme}`);

    await page.goto("/settings/machine");
    await expect(page.getByTestId("account-row").first()).toBeVisible();
    await settled(page);
    await shot(page, `settings-machine-${d}-${scheme}`);

    await page.goto("/settings/setup");
    await expect(page.getByTestId("setup-step-agents")).toBeVisible();
    await settled(page);
    await shot(page, `settings-setup-${d}-${scheme}`);

    // 權限分頁多了「每位秘書的等級」。
    await page.goto("/settings/trust");
    await expect(page.getByTestId("trust-level-row").first()).toBeVisible();
    await settled(page);
    await shot(page, `settings-trust-levels-${d}-${scheme}`);
  });
}

// ── P6-5a：角色頁的介紹（DEV-117 從設定搬來）（說話層強度＋角色清單；清單裡有官方卡、一張設計中的我的卡）──────────────

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-115, REQ-130
  test(`TestShotsPersonas @shots ${scheme}`, async ({ page, isMobile, seed }) => {
    const d = isMobile ? "mobile" : "desktop";
    await page.emulateMedia({ colorScheme: scheme });
    // 乾淨的家只有官方的兩張卡：造一張設計中的我的卡，清單才看得到「我的」「設計中」的標籤（造過就沿用）。
    await seed
      .api("/api/personas/create", {
        slug: "shotsdraft",
        name: "試作角色",
        aka: ["小試"],
        by: { name: "owner(web)" },
      })
      .catch(() => undefined);
    await page.goto("/personas");
    await expect(page.getByTestId("persona-row").first()).toBeVisible();
    await expect(page.getByTestId("persona-style-option").first()).toBeVisible();
    // 畫面對準角色清單（上面的說話層強度在清單上方一屏）。
    await page
      .locator("#persona-list-title")
      .evaluate((el) => el.scrollIntoView({ block: "start" }));
    await settled(page);
    await shot(page, `settings-personas-${d}-${scheme}`);
  });
}

for (const scheme of ["light", "dark"] as const) {
  // 說話測試（P6-5b）：角色庫詳情頁，兩份結果並排比較的狀態。
  // Guards: REQ-115, REQ-130
  test(`TestShotsPersonaTest @shots ${scheme}`, async ({ page, isMobile, seed }) => {
    const d = isMobile ? "mobile" : "desktop";
    await page.emulateMedia({ colorScheme: scheme });
    const owner = { name: "owner(web)" };
    await seed
      .api("/api/personas/create", { slug: "my_shotstone", name: "試口吻", by: owner })
      .catch(() => undefined);
    const runsDone = async (n: number) => {
      await expect
        .poll(async () => {
          const r = await seed.api<{ runs?: { status: string }[] }>("/api/personas/test_results", {
            slug: "my_shotstone",
            limit: 10,
          });
          const runs = r.runs ?? [];
          return runs.length >= n && runs.every((x) => x.status !== "running");
        })
        .toBe(true);
    };
    const before =
      (
        await seed.api<{ runs?: unknown[] }>("/api/personas/test_results", {
          slug: "my_shotstone",
          limit: 10,
        })
      ).runs?.length ?? 0;
    await seed.api("/api/personas/test", { slug: "my_shotstone", seat: "code" });
    await runsDone(before + 1);
    await seed.api("/api/personas/test", { slug: "my_shotstone", seat: "code", style: "d" });
    await runsDone(before + 2);
    await page.goto("/personas/my_shotstone");
    const tone = page.getByTestId("persona-tone-test");
    const rows = tone.getByTestId("tone-run-row");
    await expect(rows.nth(1)).toBeVisible();
    await rows.nth(0).getByTestId("tone-pick").check();
    await rows.nth(1).getByTestId("tone-pick").check();
    await tone.getByTestId("tone-compare-open").click();
    await expect(tone.getByTestId("tone-compare")).toBeVisible();
    await tone.getByTestId("tone-compare").evaluate((el) => el.scrollIntoView({ block: "start" }));
    await settled(page);
    await shot(page, `settings-persona-test-${d}-${scheme}`);
  });
}

// ── P6-6：通知的簡單模式、設定→紀錄、夜班清單 ──────────────────────────────────

const shotOwner = { name: "owner(test)" };

for (const scheme of ["light", "dark"] as const) {
  // 簡單模式的通知：三張預設組卡片（標準被選中、展開「每類會怎麼送」）＋安靜時段。
  // Guards: REQ-115, REQ-25
  test(`TestShotsNotifySimple @shots ${scheme}`, async ({ page, isMobile, seed }) => {
    const d = isMobile ? "mobile" : "desktop";
    await page.emulateMedia({ colorScheme: scheme });
    await seed.api("/api/system/set_mode", { mode: "simple", by: shotOwner });
    try {
      await page.goto("/settings/notify");
      await expect(page.getByTestId("preset-standard")).toBeVisible();
      await page
        .getByTestId("preset-standard")
        .getByRole("button", { name: "看每類會怎麼送" })
        .click();
      await expect(page.getByTestId("preset-standard-detail")).toBeVisible();
      await settled(page);
      await shot(page, `settings-notify-simple-${d}-${scheme}`);
    } finally {
      await seed.api("/api/system/set_mode", { mode: "full", by: shotOwner });
    }
  });
}

for (const scheme of ["light", "dark"] as const) {
  // 設定 → 紀錄：主人與秘書做的事，依日期分組；上面是篩選。
  // Guards: REQ-115, REQ-111
  test(`TestShotsLog @shots ${scheme}`, async ({ page, isMobile, seed }) => {
    const d = isMobile ? "mobile" : "desktop";
    await page.emulateMedia({ colorScheme: scheme });
    const taskId = await seed.doingTask("整理這週的報表", "code");
    await seed.api("/api/tasks/update", { id: taskId, result: "做了一半", by: { name: "code" } });
    await seed.reminder("下週一帶保單去銀行");
    await seed.reminder("訂週五的健身課");
    await page.goto("/settings/log");
    await expect(page.getByTestId("log-row").first()).toBeVisible();
    await expect(page.getByTestId("log-row").filter({ hasText: "任務單" }).first()).toBeVisible();
    await settled(page);
    await shot(page, `settings-log-${d}-${scheme}`);
  });
}

for (const scheme of ["light", "dark"] as const) {
  // 設定 → 電腦 → 夜班清單：待做（可刪除、可新增）、要主人、做完。
  // Guards: REQ-115, REQ-111
  test(`TestShotsNightQueue @shots ${scheme}`, async ({ page, isMobile, seed }) => {
    const d = isMobile ? "mobile" : "desktop";
    await page.emulateMedia({ colorScheme: scheme });
    const body = [
      "# 夜班清單",
      "",
      "## 待做",
      "- [ ] 把 ai_docs 的決策整理成一頁摘要，放 notes/decisions-summary.md｜加入 2026-10-24",
      "- [ ] 補 internal/cli 的測試，做到每個指令都有一個｜加入 2026-10-25",
      "",
      "## 要主人",
      "- [ ] 升級 Go 到最新版｜加入 2026-10-22｜卡住：要 sudo",
      "",
      "## 做完（留最近 14 天）",
      "- [x] 整理 README 的安裝步驟｜加入 2026-10-20｜結果：已更新，在 README.md｜完成 2026-10-24",
      "",
    ].join("\n");
    const cur = await seed
      .api<{ record?: { revision?: string; fields?: Record<string, unknown> } }>("/api/vault/get", {
        kind: "note",
        id: "system/night-queue",
      })
      .catch(() => ({ record: undefined }));
    await seed.api("/api/vault/put", {
      record: {
        kind: "note",
        id: "system/night-queue",
        fields: cur.record?.fields ?? {},
        body,
        archived: false,
      },
      expected_revision: cur.record?.revision ?? "",
      by: shotOwner,
    });
    await page.goto("/settings/machine");
    await expect(page.getByTestId("night-todo")).toContainText("ai_docs 的決策");
    await expect(page.getByTestId("night-done")).toContainText("README");
    await page.getByTestId("night-todo").evaluate((el) => el.scrollIntoView({ block: "center" }));
    await settled(page);
    await shot(page, `settings-night-queue-${d}-${scheme}`);
  });
}

// ── S8：秘書篩選（選 code 一位的狀態；標準截圖照舊是「全部」）──────────────────────

const codeReminderSeeds = [
  { content: "整理 CI 的失敗紀錄", at: [11, 0] },
  { content: "確認看板的 UX 對照", at: [15, 30] },
];

// 篩選截圖要 code 在待辦也有東西（共用的 seed.reminder 都是 life 的）。
async function seedFilterShots(
  seed: Seed,
  request: import("@playwright/test").APIRequestContext,
  base: string,
) {
  await seedShotData(seed, request, base);
  await seedS5(seed);
  const reminders = await seed.api<{ reminders?: { content: string }[] }>("/api/reminders/list", {
    include_archived: false,
  });
  const have = new Set((reminders.reminders ?? []).map((r) => r.content));
  const now = new Date();
  for (const r of codeReminderSeeds) {
    if (have.has(r.content)) continue;
    const [h, m] = r.at as [number, number];
    await seed.api("/api/reminders/create", {
      content: r.content,
      owner: "code",
      follow: "remind",
      at: new Date(now.getFullYear(), now.getMonth(), now.getDate(), h, m).toISOString(),
      by: { name: "owner(web)" },
    });
  }
}

const filterScreens = [
  { name: "today", path: "/" },
  { name: "chat-list", path: "/chat" },
  { name: "reminders", path: "/reminders" },
  { name: "tasks", path: "/tasks" },
  { name: "messages", path: "/messages" },
] as const;

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-155
  test(`TestShotsFilter @shots ${scheme}`, async ({ page, seed, request, baseURL, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedFilterShots(seed, request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    const code = page
      .getByRole("region", { name: "秘書篩選" })
      .getByRole("button", { name: /^Builder/ });
    for (const [i, screen] of filterScreens.entries()) {
      await page.goto(screen.path);
      await expect(code).toBeVisible();
      // 第一頁點選 code；後面幾頁靠「記住上次的選擇」自己就是選中的。
      if (i === 0) await code.click();
      await expect(code).toHaveAttribute("aria-pressed", "true");
      if (screen.name === "chat-list") {
        await expect(page.locator('[data-thread="code"]')).toBeVisible();
        await expect(page.locator('[data-thread="life"]')).toHaveCount(0);
      }
      await settled(page);
      await shot(page, `filter-${screen.name}-${d}-${scheme}`);
    }
  });
}

// ── S9：角色聊天（影片、收合、角色欄）──────────────────────────────────────

const personaChat = "幫我看看今天的進度，**重點**放前面";

async function seedPersonaChat(
  request: import("@playwright/test").APIRequestContext,
  base: string,
) {
  const res = await request.post(`${base}/api/bus/send`, {
    data: { to: "code", body: personaChat, idempotency_key: "shots-persona" },
  });
  expect(res.ok()).toBe(true);
}

// 影片停在第 0 格，截圖才穩定。
async function freezeVideos(page: Page) {
  await page.evaluate(async () => {
    for (const v of document.querySelectorAll("video")) {
      v.pause();
      v.currentTime = 0;
      if (v.seeking) await new Promise((r) => v.addEventListener("seeked", r, { once: true }));
    }
  });
}

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-156
  test(`TestShotsPersona @shots ${scheme}`, async ({ page, seed, request, baseURL, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedShotData(seed, request, baseURL ?? "");
    await seedPersonaChat(request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/chat/code");
    await expect(page.getByRole("log", { name: "對話" }).getByText("重點").first()).toBeVisible();
    await expect(page.locator('[data-persona-media="video"] video').first()).toBeVisible();
    await page.waitForTimeout(800); // 進場動畫與影片第一格
    await freezeVideos(page);
    await shot(page, `persona-thread-${d}-${scheme}`);

    if (isMobile) {
      // 聚焦輸入框：頂端收成小頭像。
      await page.locator("#hg-composer").focus();
      await expect(page.locator("header[data-hero]")).toHaveAttribute("data-hero", "collapsed");
      await page.waitForTimeout(500);
      await shot(page, `persona-thread-collapsed-mobile-${scheme}`);
    }
  });
}

// ── P6-2a：套件頁與自訂選單失敗提醒 ───────────────────────────────────────────────

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-115
  test(`TestShotsPackagePage @shots ${scheme}`, async ({ page, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/p/demo/home");
    await expect(page.getByTestId("demo-page")).toBeVisible();
    await settled(page);
    await shot(page, `package-page-${d}-${scheme}`);
  });

  // Guards: REQ-115
  test(`TestShotsMenuRendererFailed @shots ${scheme}`, async ({
    page,
    request,
    baseURL,
    isMobile,
  }) => {
    const d = isMobile ? "mobile" : "desktop";
    const set = (renderer: string) =>
      request.post(`${baseURL}/api/web/set_menu_prefs`, {
        data: { prefs: { renderer, tree: [] } },
      });
    await page.emulateMedia({ colorScheme: scheme });
    // 會在執行時丟錯的選單元件：換回預設選單，內容區最上面一行提醒。
    await set("demo.crashmenu");
    try {
      await page.goto("/");
      await expect(page.getByTestId("menu-renderer-notice")).toBeVisible();
      await expect(page.getByRole("navigation", { name: "主選單" })).toBeVisible();
      await settled(page);
      await shot(page, `menu-renderer-failed-${d}-${scheme}`);
    } finally {
      await set("");
    }
  });
}

// ── P6-3b：官方套件 nutrition（飲食、飲食趨勢）與聊天附件 chip ─────────────────────

const shotDay = (ago: number) => {
  const d = new Date();
  d.setDate(d.getDate() - ago);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
};

// 手機與桌機共用同一個家：已經造過（有備註「截圖資料」的餐點）就不再造。
async function seedNutritionShots(
  request: import("@playwright/test").APIRequestContext,
  base: string,
) {
  const post = async (route: string, data: unknown) => {
    const res = await request.post(`${base}/api/${route}`, { data });
    expect(res.status(), `${route} ${await res.text()}`).toBeLessThan(300);
    return (await res.json()) as Record<string, unknown>;
  };
  const listed = (await post("vault/list", {
    kind: "nutrition.meal",
    include_archived: false,
  })) as {
    records?: { fields?: { note?: string } }[];
  };
  if ((listed.records ?? []).some((r) => r.fields?.note === "截圖資料")) return;
  const upload = await request.post(`${base}/api/web/upload`, {
    headers: {
      "X-Filename": encodeURIComponent("午餐.png"),
      "X-Requested-With": "evergenie",
      "Content-Type": "application/octet-stream",
    },
    data: readFileSync(new URL("./fixtures/persona.png", import.meta.url)),
  });
  expect(upload.ok(), await upload.text()).toBe(true);
  const photo = ((await upload.json()) as { url: string }).url;
  await post("vault/put", {
    record: {
      kind: "nutrition.goal",
      id: "main",
      fields: { mode: "減脂", kcal: 1900, protein: 120, carb: 0, fat: 0 },
    },
  });
  const add = async (fields: Record<string, unknown>) => {
    const id = String((await post("vault/next_id", { kind: "nutrition.meal" })).id);
    await post("vault/put", {
      record: {
        kind: "nutrition.meal",
        id,
        fields: { carb: 50, fat: 15, note: "截圖資料", ...fields },
      },
    });
  };
  const today = shotDay(0);
  await add({
    date: today,
    at: `${today} 08:10`,
    meal: "早餐",
    kcal: 420,
    protein: 24,
    conf: "高",
    items: [
      { name: "全麥吐司", qty: "2 片", kcal: 180, protein: 7 },
      { name: "荷包蛋", qty: "2 顆", kcal: 180, protein: 13 },
      { name: "無糖豆漿", qty: "300 ml", kcal: 60, protein: 4 },
    ],
  });
  await add({
    date: today,
    at: `${today} 12:25`,
    meal: "午餐",
    kcal: 720,
    protein: 46,
    conf: "中",
    photos: [photo],
    items: [
      { name: "雞腿便當", qty: "1 份", kcal: 650, protein: 40 },
      { name: "燙青菜", qty: "1 份", kcal: 70, protein: 6 },
    ],
  });
  await post("vault/put", {
    record: {
      kind: "nutrition.fav",
      id: "F-1",
      fields: { name: "雞胸沙拉", kcal: 380, protein: 38, carb: 20, fat: 12 },
    },
  });
  // 過去 12 天：有些天沒記錄（趨勢圖留空）。
  const kcals: Record<number, [number, number]> = {
    1: [1850, 118],
    2: [2100, 95],
    3: [1720, 110],
    5: [1990, 105],
    6: [2250, 88],
    8: [1680, 125],
    9: [1900, 112],
    11: [2050, 100],
  };
  for (const [ago, [kcal, protein]] of Object.entries(kcals)) {
    const date = shotDay(Number(ago));
    await add({ date, at: `${date} 19:00`, meal: "晚餐", kcal, protein });
  }
}

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-115
  test(`TestShotsNutritionLog @shots ${scheme}`, async ({ page, request, baseURL, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedNutritionShots(request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/p/nutrition/log");
    await expect(page.getByTestId("nutrition-meal").first()).toBeVisible();
    await expect(page.getByTestId("nutrition-photo").getByRole("img")).toBeVisible();
    await settled(page);
    await shot(page, `nutrition-log-${d}-${scheme}`);
  });

  // Guards: REQ-115
  test(`TestShotsNutritionTrend @shots ${scheme}`, async ({ page, request, baseURL, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedNutritionShots(request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/p/nutrition/trend");
    await expect(page.getByTestId("trend-day")).toHaveCount(14);
    await expect(page.getByTestId("goal-editor").getByLabel("熱量（大卡）")).toHaveValue("1900");
    await settled(page);
    await shot(page, `nutrition-trend-${d}-${scheme}`);
  });

  // Guards: REQ-115
  test(`TestShotsChatAttachment @shots ${scheme}`, async ({ page, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/chat/code");
    await page.getByTestId("composer-file").setInputFiles({
      name: "午餐.png",
      mimeType: "image/png",
      buffer: readFileSync(new URL("./fixtures/persona.png", import.meta.url)),
    });
    await expect(page.locator('[data-attachment="done"]')).toBeVisible();
    await page.locator("#hg-composer").fill("這一餐大概多少熱量？");
    // 手機的對話頁沒有主選單，不能用 settled()。
    await page.waitForTimeout(600);
    await shot(page, `chat-attachment-chip-${d}-${scheme}`);
  });
}

// ── P6-3c：官方套件 fitness（運動記錄、運動歷史、身體）──────────────────────────

// 手機與桌機共用同一個家：已經造過（有器材）就不再造。
async function seedFitnessShots(
  request: import("@playwright/test").APIRequestContext,
  base: string,
) {
  const post = async (route: string, data: unknown) => {
    const res = await request.post(`${base}/api/${route}`, { data });
    expect(res.status(), `${route} ${await res.text()}`).toBeLessThan(300);
    return (await res.json()) as Record<string, unknown>;
  };
  const listed = (await post("vault/list", {
    kind: "fitness.equipment",
    include_archived: false,
  })) as {
    records?: unknown[];
  };
  if ((listed.records ?? []).length > 0) return;
  const add = async (kind: string, fields: Record<string, unknown>, body?: string) => {
    const id = String((await post("vault/next_id", { kind })).id);
    await post("vault/put", { record: { kind, id, fields, ...(body ? { body } : {}) } });
    return id;
  };
  const equipment: [string, string[], string[]][] = [
    ["啞鈴彎舉", ["二頭肌"], []],
    ["腿推舉機", ["股四頭"], ["臀大肌", "大腿後側"]],
    ["高位下拉", ["背闊肌"], ["二頭肌"]],
    ["坐姿胸推", ["胸"], ["三頭", "肩"]],
    ["肩推機", ["肩"], ["三頭"]],
  ];
  for (const [name, primary, secondary] of equipment) {
    await add("fitness.equipment", { name, primary, secondary });
  }
  await add("fitness.equipment", {
    name: "棒式",
    primary: ["核心"],
    secondary: [],
    type: "徒手",
    unit: "秒",
  });
  const sets = (w: number) => [
    { equipment: "啞鈴彎舉", weight: 10 + w, reps: 12, feel: "剛好" },
    { equipment: "啞鈴彎舉", weight: 10 + w, reps: 10, feel: "吃力" },
    { equipment: "腿推舉機", weight: 40 + w * 2, reps: 12, feel: "輕鬆" },
    { equipment: "高位下拉", weight: 35 + w, reps: 10, feel: "剛好" },
  ];
  const review = (w: number) =>
    `練了：二頭肌 2 組、股四頭 1 組、背闊肌 1 組。\n進步：${w > 0 ? "啞鈴彎舉 最大重量 10→12 kg" : "這次沒有新的最大重量，穩住也算數"}。\n偏弱：沒有明顯落後的地方。\n下次：照這次的重量，順的話再往上加。`;
  for (const [ago, w] of [
    [9, 0],
    [5, 1],
    [2, 2],
  ] as const) {
    const date = shotDay(ago);
    await add("fitness.workout", {
      type: "重訓",
      date,
      started: `${date} 18:30`,
      finished: `${date} 19:35`,
      status: "完成",
      feeling: ago === 2 ? "剛好" : "輕鬆",
      sets: sets(w),
      review: review(w),
      review_by: "規則",
    });
  }
  const run = shotDay(3);
  await add("fitness.workout", {
    type: "跑步",
    date: run,
    started: `${run} 07:10`,
    finished: `${run} 07:40`,
    status: "完成",
    feeling: "剛好",
    distance_km: 5,
    duration_min: 30,
    pace: "6:00",
    avg_hr: 152,
    max_hr: 171,
    cadence_spm: 168,
    source: "watch",
    review:
      "這次：5 km、30 分，配速 6:00/km。\n這是第一筆跑步，之後拿它當比較基準。\n下次：照這次的強度，穩定了再加距離。",
    review_by: "規則",
  });
  // 更早的兩次跑步與一次游泳：有氧趨勢圖要有幾個點。
  for (const [ago, km, min, pace, hr] of [
    [10, 4, 25, "6:15", 158],
    [7, 4.5, 28, "6:13", 155],
  ] as const) {
    const day = shotDay(ago);
    await add("fitness.workout", {
      type: "跑步",
      date: day,
      started: `${day} 07:00`,
      status: "完成",
      feeling: "剛好",
      distance_km: km,
      duration_min: min,
      pace,
      avg_hr: hr,
      source: "watch",
    });
  }
  const swim = shotDay(5);
  await add("fitness.workout", {
    type: "游泳",
    date: swim,
    started: `${swim} 20:00`,
    status: "完成",
    feeling: "輕鬆",
    distance_km: 1,
    duration_min: 25,
    pace: "2:30",
    pool_m: 25,
    laps: 40,
    swolf: 38,
    stroke_style: "自由式",
    avg_hr: 138,
  });
  await add(
    "fitness.note",
    { title: "十月訓練計畫", date: shotDay(6), pinned: true },
    "## 每週三練\n\n- 週一：胸、肩、三頭\n- 週三：背、二頭\n- 週五：腿、核心",
  );
  // 過去 12 天的體重與體脂：有些天沒量（趨勢圖留空）。
  const weights: Record<number, [number, number]> = {
    0: [72.1, 18.4],
    1: [72.6, 18.6],
    2: [72.3, 18.5],
    4: [73.0, 18.9],
    5: [72.8, 18.8],
    7: [73.4, 19.0],
    8: [73.9, 19.2],
    9: [73.5, 19.1],
    11: [74.2, 19.4],
  };
  for (const [ago, [weight, fat]] of Object.entries(weights)) {
    const date = shotDay(Number(ago));
    await post("vault/put", {
      record: { kind: "body.entry", id: date, fields: { date, weight, fat } },
    });
  }
}

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-115
  test(`TestShotsFitnessLog @shots ${scheme}`, async ({ page, request, baseURL, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedFitnessShots(request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/p/fitness/log");
    await page.getByRole("button", { name: "啞鈴彎舉", exact: true }).click();
    const form = page.getByTestId("set-form");
    await form.getByLabel("重量（kg）").fill("12");
    await form.getByLabel("次數").fill("12");
    await form.getByRole("button", { name: "記這組" }).click();
    await expect(page.getByTestId("coach-feedback")).toBeVisible();
    await expect(page.getByTestId("active-workout")).toBeVisible();
    await settled(page);
    await shot(page, `fitness-log-${d}-${scheme}`);
  });

  // Guards: REQ-115
  test(`TestShotsFitnessHistory @shots ${scheme}`, async ({ page, request, baseURL, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedFitnessShots(request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/p/fitness/log?view=history");
    await expect(page.getByTestId("workout-item").first()).toBeVisible();
    await page.getByTestId("history-equipment").selectOption("腿推舉機");
    await expect(page.getByTestId("max-chart")).toBeVisible();
    await page.getByTestId("workout-item").nth(1).locator("summary").click();
    await settled(page);
    await shot(page, `fitness-history-${d}-${scheme}`);
  });

  test(`TestShotsFitnessCardio @shots ${scheme}`, async ({ page, request, baseURL, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedFitnessShots(request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/p/fitness/log");
    await page.getByRole("button", { name: "有氧", exact: true }).click();
    await expect(page.getByTestId("cardio-form")).toBeVisible();
    await settled(page);
    await shot(page, `fitness-cardio-${d}-${scheme}`);
    await page.goto("/p/fitness/log?view=history");
    const trends = page.getByTestId("fitness-trends");
    await trends.getByRole("button", { name: "有氧", exact: true }).click();
    await trends.getByTestId("history-sport").selectOption("跑步");
    await trends.getByTestId("history-metric").selectOption("pace");
    await expect(trends.getByTestId("cardio-chart")).toBeVisible();
    await settled(page);
    await shot(page, `fitness-cardio-trend-${d}-${scheme}`);
  });

  // Guards: REQ-115
  test(`TestShotsFitnessBody @shots ${scheme}`, async ({ page, request, baseURL, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedFitnessShots(request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/p/body/log");
    await expect(page.getByTestId("body-chart-weight")).toBeVisible();
    await expect(page.getByTestId("body-avg-weight")).toBeVisible();
    await settled(page);
    await shot(page, `fitness-body-${d}-${scheme}`);
  });
}

// ── P6-4a：官方套件 habits（習慣、習慣詳情）──────────────────────────────────────

const dayStr = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

// 手機與桌機共用同一個家：已經造過（有習慣）就不再造。回第一個習慣的編號。
async function seedHabitShots(
  request: import("@playwright/test").APIRequestContext,
  base: string,
): Promise<string> {
  const post = async (route: string, data: unknown) => {
    const res = await request.post(`${base}/api/${route}`, { data });
    expect(res.status(), `${route} ${await res.text()}`).toBeLessThan(300);
    return (await res.json()) as Record<string, unknown>;
  };
  const listed = (await post("vault/list", { kind: "habits.habit", include_archived: false })) as {
    records?: { id: string }[];
  };
  const first = listed.records?.[0]?.id;
  if (first) return first;
  const add = async (kind: string, fields: Record<string, unknown>, body?: string) => {
    const id = String((await post("vault/next_id", { kind })).id);
    await post("vault/put", { record: { kind, id, fields, ...(body ? { body } : {}) } });
    return id;
  };
  const now = new Date();
  const monday = new Date(now);
  monday.setDate(now.getDate() - ((now.getDay() + 6) % 7));
  const addDays = (d: Date, n: number) => {
    const x = new Date(d);
    x.setDate(d.getDate() + n);
    return x;
  };
  const lift = await add(
    "habits.habit",
    {
      name: "每週重訓三次",
      agent: "life",
      cadence: "weekly",
      target: 3,
      mode: "cumulative",
      start: dayStr(addDays(monday, -63)),
      status: "active",
      identity: "會照顧自己身體的人",
      cue: "下班之後先去健身房，再回家",
      two_min: "換上運動服，做兩下暖身",
    },
    "約定時聊到：週一、三、五是固定的練習日，沒去就隔天補。",
  );
  // 每週打幾次卡（最舊到這週）：有一週漏掉。
  const perWeek = [3, 3, 3, 2, 3, 3, 0, 3, 1];
  for (const [i, n] of perWeek.entries()) {
    for (let j = 0; j < n; j++) {
      const d = addDays(monday, (i - 8) * 7 + j * 2);
      if (d > now) continue;
      await add("habits.checkin", {
        habit: lift,
        date: dayStr(d),
        source: "manual",
        key: `manual:${lift}:${dayStr(d)}:`,
      });
    }
  }
  const stretch = await add("habits.habit", {
    name: "睡前拉筋",
    agent: "code",
    cadence: "daily",
    mode: "streak",
    start: dayStr(addDays(monday, -28)),
    status: "active",
  });
  for (let k = 28; k >= 1; k--) {
    if (k % 6 === 0) continue;
    const d = addDays(monday, -k);
    await add("habits.checkin", {
      habit: stretch,
      date: dayStr(d),
      source: "manual",
      key: `manual:${stretch}:${dayStr(d)}:`,
    });
  }
  await add("habits.habit", {
    name: "照顧自己的身體",
    agent: "life",
    cadence: "daily",
    mode: "cumulative",
    start: dayStr(addDays(monday, -7)),
    status: "active",
    subtasks: ["量體重=body:weight", "記錄吃飯=nutrition:meals>=2", "拉筋"],
    allow_miss: 1,
  });
  // 等套件的程式算好（每個習慣都有 status、錢包有點數）。
  await expect
    .poll(
      async () => {
        const s = (await post("vault/list", {
          kind: "habits.status",
          include_archived: false,
        })) as {
          records?: unknown[];
        };
        const w = await request.post(`${base}/api/vault/get`, {
          data: { kind: "habits.wallet", id: "main" },
        });
        return (s.records ?? []).length >= 3 && w.ok();
      },
      { timeout: 30_000 },
    )
    .toBe(true);
  return lift;
}

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-115
  test(`TestShotsHabits @shots ${scheme}`, async ({ page, request, baseURL, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedHabitShots(request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/p/habits/list");
    await expect(page.getByTestId("habit-card").first()).toBeVisible();
    await expect(page.getByTestId("habit-progress-text").first()).toContainText("本週");
    await expect(page.getByTestId("wallet-points")).toContainText("點");
    await settled(page);
    await shot(page, `habits-${d}-${scheme}`);
    // 「怎麼算」跳出來的三段說明（DEV-142、DEV-156 U-14）。
    await page.getByTestId("habit-rules").first().click();
    await expect(page.getByTestId("rules-now")).toBeVisible();
    // 抽屜打開時主選單被遮住，settled() 等不到它；等抽屜滑完再拍。
    await page.waitForTimeout(400);
    await shot(page, `habit-rules-${d}-${scheme}`);
  });

  // Guards: REQ-115
  test(`TestShotsHabitDetail @shots ${scheme}`, async ({ page, request, baseURL, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    const id = await seedHabitShots(request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto(`/p/habits/detail?id=${id}`);
    await expect(page.getByTestId("week-cell").first()).toBeVisible();
    await expect(page.getByTestId("habit-notes")).toContainText("會照顧自己身體的人");
    await settled(page);
    await shot(page, `habit-detail-${d}-${scheme}`);
  });
}

// ---- 衣櫃（P6-4b）----

const closetFixtures = path.join(path.dirname(fileURLToPath(import.meta.url)), "fixtures");

// 衣櫃的截圖資料：builder（code 秘書的人設）一套預設、一套免費、兩套要點數（沒兌換，剪影）、一套設計中草稿（一般家的展示間不列它，CLO-37）；已經有了就不再造。
async function seedClosetShots(
  request: import("@playwright/test").APIRequestContext,
  base: string,
  server: HgServer,
) {
  await seedHabitShots(request, base); // 習慣套件的錢包有點數
  const post = async (route: string, data: unknown) => {
    const res = await request.post(`${base}/api/${route}`, { data });
    expect(res.status(), `${route} ${await res.text()}`).toBeLessThan(300);
    return (await res.json()) as Record<string, unknown>;
  };
  const listed = (await post("vault/list", { kind: "closet.outfit", include_archived: false })) as {
    records?: { id: string }[];
  };
  if (!(listed.records ?? []).some((r) => r.id === "builder-night")) {
    const avatars = path.join(server.home, "personas", "avatars");
    const outfits: [string, string, number, string, number, string][] = [
      // outfit, 名稱, 點數, 狀態, 排序, 圖
      ["base", "原本的樣子", 0, "active", 1, ""],
      ["sunday", "週日散步", 0, "active", 2, "outfit-a.png"],
      ["night", "夜景外套", 1, "active", 3, "outfit-b.png"],
      ["sea", "海邊洋裝", 2, "active", 4, "outfit-c.png"],
      ["wip", "冬季新款（設計中）", 1, "draft", 5, "outfit-a.png"],
    ];
    for (const [outfit, , , , , img] of outfits) {
      if (img)
        copyFileSync(
          path.join(closetFixtures, "closet", img),
          path.join(avatars, `builder@${outfit}.png`),
        );
    }
    for (const [outfit, name, cost, state, order] of outfits) {
      await post("vault/put", {
        record: {
          kind: "closet.outfit",
          id: `builder-${outfit}`,
          fields: {
            persona: "builder",
            outfit,
            name,
            cost,
            state,
            source: "official",
            order,
            ...(state === "draft" ? { desc: "還在試的新衣服，滿意再採用" } : {}),
          },
        },
      });
    }
  }
  await expect
    .poll(
      async () => {
        const person = await request.post(`${base}/api/vault/get`, {
          data: { kind: "closet.person", id: "builder" },
        });
        const state = await request.post(`${base}/api/vault/get`, {
          data: { kind: "closet.state", id: "main" },
        });
        if (!person.ok() || !state.ok()) return false;
        const p = (await person.json()) as { record: { fields: { outfits?: unknown[] } } };
        const st = (await state.json()) as { record: { fields: { points?: number } } };
        return (p.record.fields.outfits ?? []).length === 5 && (st.record.fields.points ?? 0) >= 1;
      },
      { timeout: 30_000 },
    )
    .toBe(true);
}

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-115, REQ-125
  test(`TestShotsCloset @shots ${scheme}`, async ({
    page,
    request,
    baseURL,
    isMobile,
    hgServer,
  }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedClosetShots(request, baseURL ?? "", hgServer);
    await page.emulateMedia({ colorScheme: scheme });
    const imagesLoaded = () =>
      page
        .locator('[data-testid="outfit-img"]:not([data-placeholder])')
        .evaluateAll((els) =>
          els.every(
            (e) => (e as HTMLImageElement).complete && (e as HTMLImageElement).naturalWidth > 0,
          ),
        );
    // 衣櫃：只有擁有的衣服與輪流穿（CLO-14）。
    await page.goto("/p/closet/wardrobe");
    await expect(page.getByTestId("outfit-tile").first()).toBeVisible();
    await expect.poll(imagesLoaded).toBe(true);
    await settled(page);
    await shot(page, `closet-${d}-${scheme}`);
    // 展示間：點數、全部衣服（剪影加鎖頭）、新增的「⋯」（CLO-15）；一般家沒有「設計中」（CLO-37）。圖都載入了（含剪影）再截圖。
    await page.goto("/p/closet/showroom");
    await expect(page.getByTestId("showroom")).toHaveAttribute("data-edit", "false");
    await expect(page.getByTestId("outfit-tile").first()).toBeVisible();
    await expect(page.getByTestId("closet-points-value")).toContainText("點");
    await expect.poll(imagesLoaded).toBe(true);
    await settled(page);
    await shot(page, `closet-showroom-${d}-${scheme}`);
  });

  // 沒兌換的衣服詳情：剪影加鎖頭、兌換按鈕。
  // Guards: REQ-115, REQ-125
  test(`TestShotsClosetOutfit @shots ${scheme}`, async ({
    page,
    request,
    baseURL,
    isMobile,
    hgServer,
  }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedClosetShots(request, baseURL ?? "", hgServer);
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/p/closet/outfit?id=builder-night");
    await expect(page.getByTestId("outfit-name")).toContainText("夜景外套");
    await expect(page.getByTestId("outfit-big")).toHaveAttribute("src", /\/pkg-files\/closet\/s\//);
    await expect(page.getByTestId("outfit-unlock")).toBeVisible();
    await expect
      .poll(() =>
        page
          .getByTestId("outfit-big")
          .evaluate((e: HTMLImageElement) => e.complete && e.naturalWidth > 0),
      )
      .toBe(true);
    await settled(page);
    await shot(page, `closet-outfit-${d}-${scheme}`);
  });
}

// ---- 上傳衣服與角色（P6-4c，closet-mine；DEV-112 起預設啟用、不在選單上）----

// 截圖資料：確認 closet-mine 在跑、建一位我的角色（半身、一張輪播照、一套要點數的衣服、一套設計中）；已經有了就不再造。
async function seedClosetMineShots(
  request: import("@playwright/test").APIRequestContext,
  base: string,
) {
  const post = async (route: string, data: unknown) => {
    const res = await request.post(`${base}/api/${route}`, { data });
    expect(res.status(), `${route} ${await res.text()}`).toBeLessThan(300);
    return (await res.json()) as Record<string, unknown>;
  };
  await post("package/set_enabled", { id: "closet-mine", enabled: true });
  await expect
    .poll(
      async () =>
        ((await post("package/get", { id: "closet-mine" })) as { package: { state: string } })
          .package.state,
      { timeout: 30_000 },
    )
    .toBe("running");
  const person = await request.post(`${base}/api/vault/get`, {
    data: { kind: "closet.person", id: "myshots" },
  });
  const have = person.ok()
    ? ((await person.json()) as { record: { fields: { outfits?: unknown[] } } }).record.fields
        .outfits?.length
    : 0;
  if (!have) {
    const upload = async (img: string) => {
      const res = await request.post(`${base}/api/web/upload`, {
        headers: {
          "Content-Type": "application/octet-stream",
          "X-Requested-With": "evergenie",
          "X-Filename": encodeURIComponent(img),
        },
        data: readFileSync(path.join(closetFixtures, "closet", img)),
      });
      expect(res.status()).toBe(200);
      return ((await res.json()) as { url: string }).url;
    };
    const send = async (fields: Record<string, unknown>) => {
      const id = String((await post("vault/next_id", { kind: "closet-mine.request" })).id);
      await post("vault/put", {
        record: { kind: "closet-mine.request", id, fields: { ...fields, state: "pending" } },
      });
      await expect
        .poll(
          async () => {
            const r = (await post("vault/get", { kind: "closet-mine.request", id })) as {
              record: { fields: { state: string } };
            };
            return r.record.fields.state;
          },
          { timeout: 30_000 },
        )
        .toBe("done");
    };
    await send({
      type: "character",
      upload: await upload("outfit-a.png"),
      name: "小狐狸",
      desc: "一隻愛搗蛋的狐狸",
      persona: "myshots",
    });
    await send({
      type: "photo",
      upload: await upload("outfit-b.png"),
      persona: "myshots",
      outfit: "base",
    });
    await send({
      type: "outfit",
      upload: await upload("outfit-b.png"),
      persona: "myshots",
      name: "海邊洋裝",
      desc: "夏天的衣服",
    });
    await send({
      type: "outfit",
      upload: await upload("outfit-c.png"),
      persona: "myshots",
      name: "冬季新款（設計中）",
      free: true,
      draft: true,
    });
  }
  await expect
    .poll(
      async () => {
        const r = await request.post(`${base}/api/vault/get`, {
          data: { kind: "closet.person", id: "myshots" },
        });
        if (!r.ok()) return 0;
        const p = (await r.json()) as { record: { fields: { outfits?: unknown[] } } };
        return p.record.fields.outfits?.length ?? 0;
      },
      { timeout: 30_000 },
    )
    .toBe(3);
}

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-115, REQ-126
  test(`TestShotsClosetMine @shots ${scheme}`, async ({ page, request, baseURL, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    try {
      await seedClosetMineShots(request, baseURL ?? "");
      await page.emulateMedia({ colorScheme: scheme });
      // DEV-143：衣服在角色頁的「衣服」分頁（closet-mine 掛的）；新增角色在角色清單的「新增角色」。
      await page.goto("/personas?new=1");
      await expect(page.getByTestId("persona-new-slot")).toBeVisible({ timeout: 30_000 });
      await expect(page.getByTestId("mine-new")).toBeVisible();
      // 對話框打開時主選單被遮住，settled() 等不到它；等對話框進場動畫跑完再拍。
      await page.waitForTimeout(600);
      await shot(page, `closet-mine-new-${d}-${scheme}`);
      await page.goto("/personas/myshots?tab=closet-mine.outfits");
      await expect(page.getByTestId("mine-outfits")).toBeVisible({ timeout: 30_000 });
      await expect(page.getByTestId("mine-row-myshots-base")).toContainText("照片 2 張");
      await expect
        .poll(() =>
          page
            .locator('[data-testid="mine-thumb"]')
            .evaluateAll((els) =>
              els.every(
                (e) => (e as HTMLImageElement).complete && (e as HTMLImageElement).naturalWidth > 0,
              ),
            ),
        )
        .toBe(true);
      await settled(page);
      await shot(page, `closet-mine-${d}-${scheme}`);
      // 點一套：那套的面板與每張照片一列（DEV-143、CMI-24）。
      await page.getByTestId("mine-row-myshots-base").click();
      await expect(page.getByTestId("mine-photos-myshots-base")).toBeVisible();
      await settled(page);
      await shot(page, `closet-mine-outfit-${d}-${scheme}`);
    } finally {
      // 開回預設（啟用），免得前面哪一步關了它。
      await request.post(`${baseURL}/api/package/set_enabled`, {
        data: { id: "closet-mine", enabled: true },
      });
    }
  });
}

// ── P6 收尾（F5）：UX 逐條對照時，原本的截圖沒拍到的區塊 ─────────────────────────────
// 套件的擴充點、通知的單一秘書另設、套件頁的「進階套件」、訓練計畫、今天頁的套件卡片。

async function centerOn(page: Page, testId: string) {
  const el = page.getByTestId(testId).first();
  await expect(el).toBeVisible();
  await el.evaluate((e) => e.scrollIntoView({ block: "center" }));
}

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-115
  test(`TestShotsP6Finish @shots ${scheme}`, async ({ page, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await page.emulateMedia({ colorScheme: scheme });

    await page.goto("/settings/packages");
    const greet = page.getByRole("textbox", { name: "問候語" });
    await expect(greet).toBeVisible();
    await greet.evaluate((e) => e.scrollIntoView({ block: "center" }));
    await settled(page);
    await shot(page, `p6f5-packages-extensions-${d}-${scheme}`);

    await page.goto("/settings/notify");
    await centerOn(page, "agent-override-pick");
    await page.getByTestId("agent-override-pick").selectOption("code");
    await settled(page);
    await centerOn(page, "agent-override-pick");
    await shot(page, `p6f5-notify-agent-${d}-${scheme}`);

    // 進階套件區現在只有開發工坊（dev_only，PKG-56）：fixture 是正式家，把 get_mode 的 env 改成 dev 才看得到。
    await page.route("**/api/system/get_mode", async (route) => {
      const res = await route.fetch();
      await route.fulfill({ response: res, json: { ...(await res.json()), env: "dev" } });
    });
    await page.goto("/settings/packages");
    await centerOn(page, "packages-optional");
    await settled(page);
    await shot(page, `p6f5-setup-optional-${d}-${scheme}`);

    await page.goto("/p/fitness/plan");
    await expect(page.getByTestId("fitness-plan")).toBeVisible();
    await settled(page);
    await shot(page, `p6f5-fitness-plan-${d}-${scheme}`);

    await page.goto("/");
    await centerOn(page, "habits-today");
    await settled(page);
    await shot(page, `p6f5-today-cards-${d}-${scheme}`);
  });
}

// ── F4：看板開任務單與排程頁 ────────────────────────────────────────────────────

const jobShots = [
  {
    id: "code/shots-nightly",
    agent: "code",
    name: "夜間整理",
    cron: ["0 3 * * *"],
    source: "migrate",
    body: "整理今天的記憶與筆記",
  },
  {
    id: "code/shots-weekly",
    agent: "code",
    name: "每週一三五回顧進度",
    cron: ["0 15 * * 1,3,5"],
    source: "user",
    body: "回顧這週做了什麼，列出卡住的事",
  },
  {
    id: "life/shots-morning",
    agent: "life",
    name: "早安問候",
    cron: ["30 8 * * *"],
    source: "user",
    body: "用輕鬆的口氣說早安，提醒今天的行程",
  },
  {
    id: "life/shots-chat",
    agent: "life",
    name: "主動聊天",
    windows: ["12:15-13:15", "15:30-17:00"],
    per_day: 2,
    source: "migrate",
    body: "主動開個話題",
  },
  {
    id: "shots-warmup",
    agent: "",
    name: "每天早上額度暖機",
    cron: ["0 6 * * *"],
    source: "migrate",
    body: "額度暖機",
  },
] as const;

async function seedJobShots(seed: Seed) {
  const have = await seed.api<{ jobs?: { id: string }[] }>("/api/scheduler/list_jobs", {});
  const ids = new Set((have.jobs ?? []).map((j) => j.id));
  for (const j of jobShots) {
    if (ids.has(j.id)) continue;
    await seed.api("/api/scheduler/save_job", {
      job: { ...j, ...(j.agent ? {} : { agent: undefined }), enabled: true },
      by: { name: "owner(web)" },
    });
  }
}

// Guards: REQ-186
test("TestShotsTaskNewDrawer @shots", async ({ page, seed, isMobile }) => {
  const d = isMobile ? "mobile" : "desktop";
  await seedS5(seed);
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/tasks");
  await page.getByRole("textbox", { name: "交代秘書做什麼" }).fill("把這週的開支整理成表格");
  await page.getByRole("button", { name: "選一位秘書" }).click();
  await page
    .getByRole("group", { name: "負責秘書" })
    .getByRole("button", { name: "Helper" })
    .click();
  if (isMobile) {
    await expect(page.getByRole("dialog", { name: "交代秘書" })).toBeVisible();
  } else {
    await page.getByRole("button", { name: "更多選項" }).click();
  }
  await page.getByLabel("詳細說明").fill("來源是銀行的 CSV，分成餐飲、交通、其他三類。");
  // 抽屜裡的欄位捲回最上面（填說明時會捲動），截圖才看得到「要做什麼」。
  if (isMobile) {
    await page
      .getByRole("dialog", { name: "交代秘書" })
      .locator("div.overflow-y-auto")
      .evaluate((el) => {
        el.scrollTop = 0;
      });
  }
  await page.waitForTimeout(700);
  await shot(page, `tasks-new-drawer-${d}-light`);
});

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-18
  test(`TestShotsJobs @shots ${scheme}`, async ({ page, seed, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedJobShots(seed);
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/tasks/jobs");
    await expect(page.getByText("每週一三五回顧進度")).toBeVisible();
    await expect(page.getByText("每天早上額度暖機")).toBeVisible();
    await settled(page);
    await shot(page, `jobs-list-${d}-${scheme}`);

    await page.getByRole("link", { name: "查看排程 每週一三五回顧進度" }).click();
    await expect(page.getByRole("button", { name: "現在跑一次" })).toBeVisible();
    await settled(page);
    await shot(page, `job-detail-${d}-${scheme}`);

    // 時間編輯器（每週幾）展開。
    await page.getByRole("button", { name: /每週一三五 15:00/ }).click();
    await expect(page.getByTestId("schedule-preview")).toBeVisible();
    await page.waitForTimeout(300);
    await shot(page, `job-edit-time-${d}-${scheme}`);
  });
}

// Guards: REQ-105
test("TestShotsBottomBarSix @shots", async ({ page, seed, request, baseURL, isMobile }, info) => {
  info.skip(!isMobile, "底部列只在手機寬度");
  await seedS5(seed);
  const setTree = async (tree: { item: string }[]) => {
    const res = await request.post(`${baseURL}/api/web/set_menu_prefs`, {
      data: { prefs: { tree, renderer: "" } },
    });
    expect(res.ok(), await res.text()).toBe(true);
  };
  // 選單是今天、聊天、待辦、訊息、任務單、設定：底部列前 5 個＋「更多」，共 6 格，任務單是第 5 格。
  await setTree(
    [
      "core.overview",
      "core.chat",
      "core.reminders",
      "core.messages",
      "core.tasks",
      "core.settings",
    ].map((item) => ({ item })),
  );
  try {
    await page.emulateMedia({ colorScheme: "light" });
    await page.goto("/tasks");
    await expect(page.getByRole("navigation", { name: "主選單" }).locator("a, button")).toHaveCount(
      6,
    );
    await settled(page);
    await shot(page, "bottom-bar-six-mobile-light");
    await page.emulateMedia({ colorScheme: "dark" });
    await page.waitForTimeout(300);
    await shot(page, "bottom-bar-six-mobile-dark");
  } finally {
    await setTree([]);
  }
});

// 聊天標題欄切秘書（REQ-200）：打開「換一位秘書」的樣子。手機是底部抽屜、桌機是標題下的小浮窗。
for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-200
  test(`TestShotsChatSwitch @shots ${scheme}`, async ({
    page,
    seed,
    request,
    baseURL,
    isMobile,
  }) => {
    const d = isMobile ? "mobile" : "desktop";
    await seedShotData(seed, request, baseURL ?? "");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/chat/code");
    await settled(page).catch(() => undefined);
    await page.getByRole("button", { name: /^換一位秘書/ }).click();
    await expect(page.getByRole("dialog", { name: "換一位秘書" })).toBeVisible();
    await page.waitForTimeout(500);
    await shot(page, `chat-switch-${d}-${scheme}`);
  });
}

// 預設選單樹的手機底部列（REQ-201）：沒有自己的 menu.toml，任務單仍是第 5 格。
// Guards: REQ-201
test("TestShotsBottomBarDefault @shots", async ({ page, seed, isMobile }, info) => {
  info.skip(!isMobile, "底部列只在手機寬度");
  await seedS5(seed);
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/tasks");
    await settled(page);
    await shot(page, `bottom-bar-default-mobile-${scheme}`);
  }
});

// Goal 清單、詳情與新增抽屜（REQ-203）。
async function seedGoalShots(seed: Seed) {
  const list = await seed.api<{ goals?: { title: string }[] }>("/api/goals/list", {});
  const have = new Set((list.goals ?? []).map((g) => g.title));
  const owner = { name: "owner(shots)" };
  const goals = [
    {
      title: "整理三月的報表",
      text: "把三月的所有報表整理成一份，讓我月底前可以交給會計。",
    },
    {
      title: "維持每週三次的運動",
      text: "我想連續三個月每週運動三次，飲食也要跟著調整，不要讓我一直被問。",
    },
  ];
  for (const g of goals) {
    if (!have.has(g.title))
      await seed.api("/api/goals/create", {
        title: g.title,
        text: g.text,
        criteria: "測試驗收條件",
        by: owner,
      });
  }
  if (!have.has("學日文的長期計畫"))
    await seed.api("/api/goals/propose", {
      title: "學日文的長期計畫",
      text: "我想在年底前能看懂日文新聞，請幫我排每天的練習。",
      criteria: "年底前隨機挑三篇 NHK 新聞，看得懂大意",
      agent: "life",
      by: { name: "life" },
    });
  return (
    await seed.api<{ goals: { id: string; title: string; state: string }[] }>("/api/goals/list", {})
  ).goals;
}

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-203
  test(`TestShotsGoals @shots ${scheme}`, async ({ page, seed, isMobile }) => {
    const d = isMobile ? "mobile" : "desktop";
    const goals = await seedGoalShots(seed);
    const first = goals.find((g) => g.title === "整理三月的報表");
    const draft = goals.find((g) => g.title === "學日文的長期計畫");
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/goals");
    await expect(page.getByText("整理三月的報表")).toBeVisible();
    await settled(page);
    await shot(page, `goals-list-${d}-${scheme}`);

    await page.goto(`/goals/${first?.id}`);
    await expect(page.getByTestId("goal-text")).toBeVisible();
    await settled(page);
    await shot(page, `goal-detail-${d}-${scheme}`);

    await page.goto(`/goals/${draft?.id}`);
    await expect(page.getByTestId("goal-draft")).toBeVisible();
    await settled(page);
    await shot(page, `goal-draft-${d}-${scheme}`);

    await page.goto("/goals");
    await page.getByRole("button", { name: "新增 Goal" }).click();
    const dialog = page.getByRole("dialog", { name: "新增 Goal" });
    await dialog.getByLabel("標題", { exact: true }).fill("把這季的簡報做完");
    await dialog
      .getByLabel("要做到什麼（完成的樣子）")
      .fill("下週三之前，把這一季的營運簡報做完，圖表要用最新的數字。");
    await dialog
      .getByLabel("驗收條件（怎樣算達成）")
      .fill("簡報放進共用資料夾、圖表的數字是這週的。");
    // 抽屜裡的欄位捲回最上面，截圖才看得到標題欄。
    await dialog
      .locator("div.overflow-y-auto")
      .first()
      .evaluate((el) => {
        el.scrollTop = 0;
      });
    await page.waitForTimeout(700);
    await shot(page, `goal-new-${d}-${scheme}`);
  });
}
