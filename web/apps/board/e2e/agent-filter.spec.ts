import type { Page } from "@playwright/test";
import { expect, type Seed, test } from "./fixtures";

// v2（S3）：主伺服器的秘書存職位 id、別台存「台名/職位 id」；v1 讀到時轉成 v2（vitest agentFilter.spec 守）。
const KEY = "eg.agentFilter.v2";
const rand = () => Math.random().toString(36).slice(2, 8);

// 篩選列：手機是頁標題下面的小卡列，桌機是側欄的「只看這位」；兩邊都是 aria-label「秘書篩選」。
const filterRegion = (page: Page) => page.getByRole("region", { name: "秘書篩選" });
const chip = (page: Page, name: string) =>
  filterRegion(page).getByRole("button", { name: new RegExp(`^${name}`) });
// 清掉選擇：桌機是側欄標題列的「看全部」，手機是小卡列最左邊的「全部」那顆（沒有「只看：…」那一行了）。
const showAll = (page: Page, isMobile: boolean) =>
  isMobile
    ? filterRegion(page).getByRole("button", { name: /^全部/ })
    : page.getByRole("button", { name: "看全部", exact: true }).first();

// 在乾淨的狀態開始：清掉上次的選擇（每個測試的瀏覽器本來就是新的，這裡照契約再清一次）。
async function start(page: Page, path = "/") {
  await page.goto(path);
  await page.evaluate((key) => localStorage.removeItem(key), KEY);
  await page.reload();
  await expect(filterRegion(page)).toBeVisible();
}

// 用主選單換頁（不重新整理），證明五頁共用同一個選擇；手機的任務單在「更多」裡。
async function navTo(page: Page, label: string) {
  const nav = page.getByRole("navigation", { name: "主選單" });
  const link = nav.getByRole("link", { name: new RegExp(`^${label}`) });
  if (await link.count()) {
    await link.first().click();
    return;
  }
  await nav.getByRole("button", { name: /^更多/ }).click();
  await page
    .getByRole("dialog", { name: "更多" })
    .getByRole("link", { name: new RegExp(`^${label}`) })
    .click();
}

interface Data {
  tag: string;
  remCode: string;
  remLife: string;
  taskCode: string;
  taskLife: string;
  noteCode: string;
  noteLife: string;
}

async function seedBoth(seed: Seed): Promise<Data> {
  const tag = rand();
  const at = new Date(Date.now() + 60_000).toISOString();
  const rem = (content: string, owner: string) =>
    seed.api("/api/reminders/create", {
      content,
      owner,
      follow: "remind",
      at,
      by: { name: "owner(web)" },
    });
  const d: Data = {
    tag,
    remCode: `篩選待辦甲 ${tag}`,
    remLife: `篩選待辦乙 ${tag}`,
    taskCode: `篩選任務甲 ${tag}`,
    taskLife: `篩選任務乙 ${tag}`,
    noteCode: `篩選通知甲 ${tag}`,
    noteLife: `篩選通知乙 ${tag}`,
  };
  await rem(d.remCode, "code");
  await rem(d.remLife, "life");
  await seed.reviewTask(d.taskCode, "code");
  await seed.reviewTask(d.taskLife, "life");
  await seed.approve(d.noteCode, "code");
  await seed.approve(d.noteLife, "life");
  return d;
}

// Guards: REQ-155
test("TestAgentFilterAcrossPages @smoke", async ({ page, seed, isMobile }) => {
  const d = await seedBoth(seed);
  await start(page, "/");
  // 今天：兩位的通知都在。
  await expect(page.getByText(d.noteCode).first()).toBeVisible();
  await expect(page.getByText(d.noteLife).first()).toBeVisible();

  // 只選 code（Builder）：life 的看不到，出現「只看：Builder」。
  await chip(page, "Builder").click();
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "true");
  await expect(chip(page, "Helper")).toHaveAttribute("aria-pressed", "false");
  await expect(page.getByText(d.noteCode).first()).toBeVisible();
  await expect(page.getByText(d.noteLife)).toHaveCount(0);
  if (isMobile) {
    // 手機沒有「只看：…／看全部」那一行；選中的狀態只靠小卡的 aria-pressed。
    await expect(filterRegion(page)).not.toContainText("只看：");
    await expect(filterRegion(page).getByRole("button", { name: "看全部" })).toHaveCount(0);
    await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "true");
    await expect(chip(page, "Helper")).toHaveAttribute("aria-pressed", "false");
    await expect(page.getByText(d.remCode).first()).toBeVisible();
    await expect(page.getByText(d.remLife)).toHaveCount(0);
  } else {
    await expect(showAll(page, isMobile)).toBeVisible();
  }

  // 換頁（不重新整理）：待辦、任務單、訊息、聊天，code 都還是選中的，只看到 code 的。
  await navTo(page, "待辦");
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByText(d.remCode).first()).toBeVisible();
  await expect(page.getByText(d.remLife)).toHaveCount(0);

  await navTo(page, "任務單");
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByText(d.taskCode).first()).toBeVisible();
  await expect(page.getByText(d.taskLife)).toHaveCount(0);

  await navTo(page, "訊息");
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByText(d.noteCode).first()).toBeVisible();
  await expect(page.getByText(d.noteLife)).toHaveCount(0);

  await navTo(page, "聊天");
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator('[data-thread="code"]')).toBeVisible();
  await expect(page.locator('[data-thread="life"]')).toHaveCount(0);

  // 一次只看一位（2026-11-02）：點 Helper＝換成只看 Helper。
  await chip(page, "Helper").click();
  await expect(chip(page, "Helper")).toHaveAttribute("aria-pressed", "true");
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "false");
  await expect(page.locator('[data-thread="life"]')).toBeVisible();
  await expect(page.locator('[data-thread="code"]')).toHaveCount(0);

  // 重新整理後選擇還在。
  await page.reload();
  await expect(chip(page, "Helper")).toHaveAttribute("aria-pressed", "true");
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "false");
  expect(await page.evaluate((key) => localStorage.getItem(key), KEY)).toBe('["life"]');

  // 再點已選的那位＝回到全部。
  await chip(page, "Helper").click();
  await expect(chip(page, "Helper")).toHaveAttribute("aria-pressed", "false");
  await expect(page.locator('[data-thread="code"]')).toBeVisible();
  await chip(page, "Helper").click();

  // 「看全部」：全部回來，重新整理後也是全部。
  await showAll(page, isMobile).click();
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "false");
  await expect(chip(page, "Helper")).toHaveAttribute("aria-pressed", "false");
  await page.reload();
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "false");
  await expect(page.locator('[data-thread="code"]')).toBeVisible();
  await expect(page.locator('[data-thread="life"]')).toBeVisible();
  await page.goto("/messages");
  await expect(page.getByText(d.noteCode).first()).toBeVisible();
  await expect(page.getByText(d.noteLife).first()).toBeVisible();
});

// Guards: REQ-155
test("TestTodayBadgeEqualsSubtitle", async ({ page, seed, isMobile }) => {
  await seedBoth(seed);
  await start(page, "/");
  const digits = (text: string | null) => Number(/\d+/.exec(text ?? "")?.[0] ?? Number.NaN);
  // 「等你處理」段落標題的數字：手機在 h2 裡，桌機（寬版）在標題旁邊的 span。
  const heading = page.getByRole("heading", { name: /^等你處理/ }).first();
  const sectionCount = async () =>
    digits(
      isMobile
        ? await heading.textContent()
        : await heading.locator("xpath=following-sibling::span").first().textContent(),
    );
  // 頁首「N 件等你處理」只有手機的頁首有。
  const subtitleCount = async () =>
    digits(
      /(\d+) 件等你處理/.exec(
        (await page
          .getByText(/\d+ 件等你處理/)
          .first()
          .textContent()) ?? "",
      )?.[1] ?? null,
    );

  // 沒選人：全部的數字＝伺服器的 waiting_total＝段落標題（手機再加「全部」下面的 N 與頁首）。
  const total = await seed.waitingTotal();
  expect(total).toBeGreaterThan(0);
  await expect.poll(sectionCount).toBe(total);
  if (isMobile) {
    await expect.poll(subtitleCount).toBe(total);
    const all = filterRegion(page).getByRole("button", { name: /^全部/ });
    await expect.poll(async () => digits(await all.textContent())).toBe(total);
  }

  // 點 code：它的徽章＝頁首（桌機是段落標題）的數字；待辦與要你回覆的聊天不算進來。
  await chip(page, "Builder").click();
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "true");
  const badge = Number(await chip(page, "Builder").getAttribute("data-count"));
  expect(badge).toBeGreaterThan(0);
  await expect.poll(sectionCount).toBe(badge);
  if (isMobile) await expect.poll(subtitleCount).toBe(badge);
  await expect(chip(page, "Builder")).toHaveAttribute("data-count", String(badge));
});

// Guards: REQ-155
test("TestAgentFilterEmptyAndBadge", async ({ page, seed }) => {
  await seedBoth(seed);
  await start(page, "/reminders");

  // 徽章數字＝那位在這一頁目前檢視（未完成）的筆數；API 另外數一遍對照。
  const list = await seed.api<{ reminders?: { owner?: string }[] }>("/api/reminders/list", {
    statuses: ["todo", "doing", "asking", "shelved"],
    include_archived: false,
  });
  const mine = (owner: string) => (list.reminders ?? []).filter((r) => r.owner === owner).length;
  expect(mine("code")).toBeGreaterThan(0);
  await expect(chip(page, "Builder")).toHaveAttribute("data-count", String(mine("code")));
  await expect(chip(page, "Builder")).toContainText(String(mine("code")));
  await expect(chip(page, "Helper")).toHaveAttribute("data-count", String(mine("life")));

  // 任務單頁（看「等你」分頁）：同樣對得上。
  await page.goto("/tasks?view=waiting");
  const waiting = await seed.api<{ tasks?: { task?: { agent?: string } }[] }>(
    "/api/tasks/waiting",
    {},
  );
  const tasksOf = (agent: string) =>
    (waiting.tasks ?? []).filter((w) => w.task?.agent === agent).length;
  expect(tasksOf("code")).toBeGreaterThan(0);
  await expect(chip(page, "Builder")).toHaveAttribute("data-count", String(tasksOf("code")));

  // 選一位在這個檢視沒有東西的秘書：出現「<人名> 在這裡沒有東西」＋「看全部」，不是「最近 7 天沒有做完的待辦」。
  await page.goto("/reminders?view=done");
  await chip(page, "Builder").click();
  const empty = page.getByTestId("filter-empty");
  await expect(empty).toContainText("Builder 在這裡沒有東西");
  await expect(page.getByText("最近 7 天沒有做完的待辦")).toHaveCount(0);
  await empty.getByRole("button", { name: "看全部" }).click();
  await expect(empty).toHaveCount(0);
  await expect(page.getByText("最近 7 天沒有做完的待辦")).toBeVisible();
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "false");
});

// Guards: REQ-155
test("TestAgentFilterQuickAddOwner", async ({ page }) => {
  await start(page, "/reminders");
  // 沒選人：預設負責的是 Helper（life）；剛好選一位 Builder：負責秘書預設帶 Builder。
  await page.getByLabel("新增待辦").fill("篩選預設負責人");
  await expect(page.getByTestId("quick-preview")).toContainText("負責：Helper");
  await chip(page, "Builder").click();
  await expect(page.getByTestId("quick-preview")).toContainText("負責：Builder");
  // 換成 Helper：負責的跟著換。
  await chip(page, "Helper").click();
  await expect(page.getByTestId("quick-preview")).toContainText("負責：Helper");
  // 再點 Helper（回到全部）：回到預設。
  await chip(page, "Helper").click();
  await expect(page.getByTestId("quick-preview")).toContainText("負責：Helper");
});

// Guards: REQ-155
test("TestAgentFilterBadSavedValue", async ({ page }) => {
  await page.goto("/");
  await page.evaluate((key) => localStorage.setItem(key, "{壞掉的 json"), KEY);
  await page.reload();
  // 壞掉的值＝全部：沒有人被選中，畫面照常。
  await expect(filterRegion(page)).toBeVisible();
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "false");
  await expect(chip(page, "Helper")).toHaveAttribute("aria-pressed", "false");
  // 名冊裡沒有的 id 也被丟掉，不會卡在看不見的選擇。
  await page.evaluate((key) => localStorage.setItem(key, '["ghost"]'), KEY);
  await page.reload();
  await expect(filterRegion(page)).toBeVisible();
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "false");
  await expect(page.getByRole("button", { name: "看全部", exact: true })).toHaveCount(0);
});

// 篩選管全部，聊天也一樣（作者 2026-11-02「左邊的 filter for everything，包含聊天」）：開著 Builder 的對話時在側欄點 Helper，
// 對話換成 Helper；之後用換秘書的浮窗換到 Builder，側欄的選擇也換回 Builder。手機的對話頁沒有篩選列，只跑桌機。
test("TestAgentFilterDrivesOpenChat", async ({ page, isMobile }) => {
  test.skip(isMobile, "手機的對話頁沒有篩選列");
  await start(page, "/chat/code");
  const identity = page.getByTestId("thread-identity").getByRole("heading");
  await expect(identity).toContainText("Builder");
  await chip(page, "Helper").click();
  await expect(chip(page, "Helper")).toHaveAttribute("aria-pressed", "true");
  await expect(identity).toContainText("Helper");
  await expect(page).toHaveURL(/\/chat\/life$/);
  // 在看板裡換到 Builder（換秘書的浮窗，不重新載入）：篩選跟著換成她。
  await page.getByRole("button", { name: /^換一位秘書/ }).click();
  await page
    .getByRole("dialog", { name: "換一位秘書" })
    .locator('[data-agent="code"]')
    .first()
    .click();
  await expect(page).toHaveURL(/\/chat\/code$/);
  await expect(identity).toContainText("Builder");
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "true");
  await expect(chip(page, "Helper")).toHaveAttribute("aria-pressed", "false");
  // 看全部之後換對話，篩選照樣是全部。
  await showAll(page, false).click();
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "false");
  await page.goto("/chat/code");
  await expect(identity).toContainText("Builder");
  await expect(chip(page, "Builder")).toHaveAttribute("aria-pressed", "false");
});
