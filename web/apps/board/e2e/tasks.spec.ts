import { existsSync } from "node:fs";
import path from "node:path";
import type { Locator, Page } from "@playwright/test";
import { expect, test } from "./fixtures";

const rand = () => Math.random().toString(36).slice(2, 8);
const PNG_1X1 = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

// 模擬 ⌘V 貼上一張截圖：剪貼簿只有一個沒名字的 image.png（跟瀏覽器貼截圖一樣）。
async function pasteImage(target: Locator) {
  await target.evaluate((el, b64) => {
    const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
    const dt = new DataTransfer();
    dt.items.add(new File([bytes], "image.png", { type: "image/png" }));
    el.dispatchEvent(
      new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }),
    );
  }, PNG_1X1.toString("base64"));
}

interface TaskRecord {
  id: string;
  status: string;
  review_note?: string;
  links?: string[];
  attachments?: string[];
  title?: string;
  body?: string;
  criteria?: string;
  agent?: string;
}

async function taskOf(seed: import("./fixtures").Seed, id: string): Promise<TaskRecord> {
  const res = await seed.api<{ task: TaskRecord }>("/api/tasks/get", { id });
  return res.task;
}

// Guards: REQ-49
test("TestTaskAcceptReject @smoke", async ({ page, seed }) => {
  const title = `退回測試 ${rand()}`;
  const id = await seed.reviewTask(title);
  await page.goto(`/tasks/${id}`);
  await expect(page.getByRole("heading", { name: title })).toBeVisible();
  // 最上面一張驗收卡（DEV-156 U-16）：〈名字〉做完了＋結果、怎麼確認的＋驗證方式、〈名字〉建議：結案，驗收／退回在卡裡。
  await expect(page.getByText("要你驗收").first()).toBeVisible();
  const card = page.getByTestId("task-review-card");
  await expect(card.getByRole("heading", { name: /做完了$/ })).toBeVisible();
  await expect(card.getByRole("heading", { name: /^她做完了/ })).toHaveCount(0);
  await expect(card).toContainText(`${title} 做完了`);
  await expect(card.getByRole("heading", { name: "怎麼確認的" })).toBeVisible();
  await expect(card).toContainText("跑過測試");
  await expect(card).toContainText("已驗證");
  await expect(card.getByTestId("task-review-proposed")).toHaveText(/建議：結案$/);
  await expect(card.getByRole("button", { name: "驗收", exact: true })).toBeVisible();
  // 其他欄位在「詳細」裡：e2e 的家是完整模式，預設展開；收起來就看不到需求，再按一次回來。
  const details = page.getByTestId("task-details-toggle");
  await expect(details).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByText(`${title} 的內容`)).toBeVisible();
  await details.click();
  await expect(page.getByText(`${title} 的內容`)).toHaveCount(0);
  await details.click();
  await expect(page.getByText(`${title} 的內容`)).toBeVisible();

  // 「開專屬對話」是看得出來的按鈕：高 ≥ 44，跟「驗收」「退回」同一列，左緣不比「驗收」更左。
  const accept = page.getByRole("button", { name: "驗收", exact: true });
  const room = page.getByRole("button", { name: "開專屬對話" });
  const acceptBox = await accept.boundingBox();
  const roomBox = await room.boundingBox();
  expect(roomBox?.height).toBeGreaterThanOrEqual(44);
  expect(roomBox?.x).toBeGreaterThanOrEqual((acceptBox?.x ?? Number.POSITIVE_INFINITY) - 0.5);

  // 退回要寫理由：沒填，「送出退回」是停用的、什麼都不會送。
  await page.getByRole("button", { name: "退回", exact: true }).click();
  const send = page.getByRole("button", { name: "送出退回" });
  await expect(send).toBeDisabled();
  expect((await taskOf(seed, id)).status).toBe("waiting_user");
  // 填了才能送；送出後狀態回到進行中，理由記在單上。
  await page.getByLabel("退回的理由").fill("缺少手機寬度的截圖");
  await expect(send).toBeEnabled();
  await send.click();
  await expect(page.getByRole("status").filter({ hasText: `已退回 · ${id}` })).toBeVisible();
  await expect.poll(async () => (await taskOf(seed, id)).status).toBe("doing");
  expect((await taskOf(seed, id)).review_note).toBe("缺少手機寬度的截圖");
});

// Guards: REQ-49
test("TestTaskAccept", async ({ page, seed }) => {
  const title = `驗收測試 ${rand()}`;
  const id = await seed.reviewTask(title);
  await page.goto(`/tasks/${id}`);
  await page.getByRole("button", { name: "驗收", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: `已驗收 · ${id}` })).toBeVisible();
  await expect.poll(async () => (await taskOf(seed, id)).status).toBe("done");
});

// Guards: REQ-56
test("TestOpenTaskRoom", async ({ page, seed }) => {
  const title = `專屬對話 ${rand()}`;
  // 家是同一個 worker 共用的：前面的測試（退回、chat-work 的排隊…）可能留著開著的工作 session 佔滿 work_max，
  // 這張單的對話就會被排隊、單號那一句不會送出。先收乾淨。
  await seed.closeAllWork();
  const id = await seed.doingTask(title, "code");
  await page.goto(`/tasks/${id}`);
  await page.getByRole("button", { name: "開專屬對話" }).click();
  // 轉到 /chat/<回傳的對話名稱>；第一句是單號，秘書先讀單。
  const num = id.replace("SEC-", "");
  await expect.poll(() => new URL(page.url()).pathname).toBe(`/chat/code-sec-${num}`);
  await expect(page.getByRole("log", { name: "對話" })).toContainText(id, { timeout: 20_000 });
});

// 「去聊」是次要的、只開對話（DEV-156 U-05）：這張單沒有開著的對話時到負責秘書的主要討論串，輸入框先起個頭，
// 不替主人送任何話、不開工作 session。
// Guards: REQ-56
test("TestWaitingTaskShowsAskAndGoChat", async ({ page, seed }) => {
  await seed.closeAllWork();
  const title = `等你決定 ${rand()}`;
  const ask = `要用方案 A 還是 B？${rand()}`;
  const id = await seed.askTask(title, ask, "code");
  await page.goto(`/tasks/${id}`);
  await expect(page.getByText("秘書在問你")).toBeVisible();
  await expect(page.getByText(ask)).toBeVisible();
  // 問句下面就有回答框；「去聊」退成次要（不是實心的主要鈕）。
  await expect(page.getByTestId("task-answer").getByLabel("你的回答")).toBeVisible();
  const go = page.getByTestId("task-go-chat");
  await expect(go).toHaveAttribute("data-tone", "secondary");
  await go.click();
  await expect(page).toHaveURL(/\/chat\/code$/);
  await expect(page.locator("#hg-composer")).toHaveValue(`關於「${title}」：`);
  // 沒有替主人開這張單的工作 session（開了的話第一句會是單號，看起來像主人說的）。
  const threads = await seed.api<{ threads?: { name?: string }[] }>("/api/session/list_threads", {
    include_stopped: true,
  });
  const num = id.replace("SEC-", "");
  expect((threads.threads ?? []).map((t) => t.name)).not.toContain(`code-sec-${num}`);
  expect((await taskOf(seed, id)).status).toBe("waiting_user");
});

// 秘書在問你：問句下面直接打字回答（DEV-156 U-05），走「回覆這則通知」同一條路；回完最後一則，單回到進行中（DEV-26）。
// 今天頁的卡也有同一個回答框。
// Guards: REQ-56
test("TestWaitingTaskInlineAnswer", async ({ page, seed, isMobile }) => {
  const title = `就地回答 ${rand()}`;
  const ask = `要日式還是義式？${rand()}`;
  const id = await seed.askTask(title, ask, "code");
  if (isMobile) {
    // 手機的今天頁：卡上就有回答框。
    await page.goto("/");
    const card = page.locator(`[data-card="${id}"]`);
    await expect(card.getByTestId("task-answer").getByLabel("你的回答")).toBeVisible();
    await expect(card.getByTestId("task-go-chat")).toBeVisible();
  }
  await page.goto(`/tasks/${id}`);
  const box = page.getByTestId("task-answer");
  await box.getByLabel("你的回答").fill("義式");
  await box.getByRole("button", { name: "送出" }).click();
  await expect(page.getByRole("status").filter({ hasText: `已回「義式」· ${id}` })).toBeVisible();
  await expect.poll(async () => (await taskOf(seed, id)).status).toBe("doing");
  const list = await seed.api<{
    notifications?: { task?: string; state?: string; answer?: { text?: string } }[];
  }>("/api/notify/list", {});
  const answered = (list.notifications ?? []).find((n) => n.task === id);
  expect(answered?.answer?.text).toBe("義式");
});

// 同一句問句有兩則（舊資料以前催一次就多一則、秘書另外發的同一句，DEV-156 U-05 review）：卡上與詳情頁都只有一組回答，
// 拿的是只收打字的那則（TSK-6 推的，回答會轉回秘書，NTF-48）——不是比較新、帶按鈕、答案不會轉回秘書的那則。
// 回了它，另一則後端一起收掉（TSK-30），單回到進行中，秘書收到主人的回答。
test("TestWaitingTaskDuplicateAskOneBox", async ({ page, seed, isMobile }) => {
  const title = `催過的問句 ${rand()}`;
  const ask = `要不要改週四？${rand()}`;
  const id = await seed.askTask(title, ask, "code");
  await seed.api("/api/notify/send", {
    agent: "code",
    category: "approve",
    title,
    todo: ask,
    task: id,
  });
  if (isMobile) {
    await page.goto("/");
    const card = page.locator(`[data-card="${id}"]`).getByTestId("task-answer");
    await expect(card.getByRole("textbox")).toHaveCount(1);
    await expect(card.getByRole("button", { name: "好" })).toHaveCount(0);
  }
  await page.goto(`/tasks/${id}`);
  const box = page.getByTestId("task-answer");
  await expect(box.getByRole("textbox")).toHaveCount(1);
  await expect(box.getByRole("button", { name: "好" })).toHaveCount(0);
  await box.getByRole("textbox").fill("改週四");
  await box.getByRole("button", { name: "送出" }).click();
  await expect.poll(async () => (await taskOf(seed, id)).status).toBe("doing");
  const list = await seed.api<{
    notifications?: { id?: string; task?: string; typed?: boolean; answer?: { text?: string } }[];
  }>("/api/notify/list", {});
  const mine = (list.notifications ?? []).filter((n) => n.task === id);
  expect(mine.map((n) => n.answer?.text)).toEqual(["改週四"]);
  expect(mine[0]?.typed).toBe(true);
  // 答案轉回秘書：送了一封帶這則通知編號的回答，給秘書本人或那張單的工作 session（SES-11）。
  await expect
    .poll(async () => {
      const r = await seed.api<{ envelopes?: { kind?: string; notice?: string; body?: string }[] }>(
        "/api/bus/list",
        {},
      );
      return (r.envelopes ?? []).some(
        (e) => e.kind === "answer" && e.notice === mine[0]?.id && e.body === "改週四",
      );
    })
    .toBe(true);
});

// DEV-107（作者 2026-11-03 18:22）：分頁只有「未結／已結束」；未結一條清單照狀態排（等你 → 進行中 → 等外部 → 待做），
// 上面一排狀態標籤可複選篩選，記在網址；舊的 ?view=waiting 開進來是「未結」＋「等你」。
// Guards: REQ-55
test("TestTasksListViews", async ({ page, seed, isMobile }) => {
  const waitingTitle = `清單等你 ${rand()}`;
  const doingTitle = `清單進行中 ${rand()}`;
  const ideaTitle = `清單待做 ${rand()}`;
  const extTitle = `清單等外部 ${rand()}`;
  const waitingId = await seed.reviewTask(waitingTitle);
  const doingId = await seed.doingTask(doingTitle);
  await seed.statusTask(ideaTitle, "idea");
  await seed.statusTask(extTitle, "waiting_external");
  await page.goto("/tasks");
  if (!isMobile) await expect(page.getByRole("heading", { name: "任務單" })).toBeVisible();
  const tabs = page.getByRole("group", { name: "任務單篩選" });
  const chips = page.getByRole("group", { name: "依狀態篩選" });
  const list = page.getByRole("list", { name: "任務單清單", exact: true });
  const link = (title: string) => list.getByRole("link", { name: new RegExp(title) });
  const order = async () => {
    const names = await list.getByRole("link").allTextContents();
    return [waitingTitle, doingTitle, extTitle, ideaTitle].map((t) =>
      names.findIndex((n) => n.includes(t)),
    );
  };

  // 預設「未結」：只有兩個分頁；四種狀態都在同一張清單，照 等你 → 進行中 → 等外部 → 待做 排，待做在最下面。
  await expect(tabs.getByRole("button")).toHaveCount(2);
  await expect(tabs.getByRole("button", { name: /^未結/ })).toHaveAttribute("aria-pressed", "true");
  await expect(link(ideaTitle)).toBeVisible();
  await expect
    .poll(async () => {
      const [w, d, e, i] = await order();
      return w >= 0 && w < d && d < e && e < i;
    })
    .toBe(true);
  // 每列左邊有狀態小標籤；送驗收的直接寫「要你驗收」（DEV-156 U-10），等了多久前端自己算。
  const row = link(waitingTitle);
  await expect(row).toContainText(waitingId);
  await expect(row.getByTestId("task-status")).toHaveText("要你驗收");
  await expect(row).toContainText("等了");
  await expect(link(ideaTitle).getByTestId("task-status")).toHaveText("待做");

  // 狀態標籤：寫張數；點「待做」只剩待做，再點「等你」兩種都顯示；網址帶著，重新整理還在。
  await expect(chips.getByRole("button", { name: /^待做\s*\d+/ })).toBeVisible();
  await chips.getByRole("button", { name: /^待做/ }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get("status")).toBe("idea");
  await expect(link(ideaTitle)).toBeVisible();
  await expect(link(doingTitle)).toHaveCount(0);
  await chips.getByRole("button", { name: /^等你處理/ }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get("status")).toBe("waiting_user,idea");
  await expect(link(waitingTitle)).toBeVisible();
  await expect(link(ideaTitle)).toBeVisible();
  await expect(link(extTitle)).toHaveCount(0);
  await page.reload();
  await expect(chips.getByRole("button", { name: /^等你處理/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await expect(chips.getByRole("button", { name: /^待做/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await expect(link(doingTitle)).toHaveCount(0);
  // 再點取消：一個都沒選＝全部。
  await chips.getByRole("button", { name: /^等你處理/ }).click();
  await chips.getByRole("button", { name: /^待做/ }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get("status")).toBeNull();
  await expect(link(doingTitle)).toBeVisible();

  // 篩到沒有符合的：說「沒有符合的單」，「清除篩選」回到全部。
  await page.getByRole("searchbox", { name: "搜尋任務單" }).fill(doingTitle);
  await chips.getByRole("button", { name: /^待做/ }).click();
  await expect(page.getByText("沒有符合")).toBeVisible();
  await page.getByTestId("tasks-clear-filters").click();
  await expect(link(waitingTitle)).toBeVisible();
  await expect.poll(() => new URL(page.url()).searchParams.get("status")).toBeNull();

  // 舊網址：?view=waiting 是「未結」＋「等你」篩選。
  await page.goto("/tasks?view=waiting");
  await expect(tabs.getByRole("button", { name: /^未結/ })).toHaveAttribute("aria-pressed", "true");
  await expect(chips.getByRole("button", { name: /^等你處理/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await expect(link(waitingTitle)).toBeVisible();
  await expect(link(doingTitle)).toHaveCount(0);

  // 已結束：沒有狀態標籤；點進詳細頁。
  await tabs.getByRole("button", { name: "已結束" }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get("view")).toBe("closed");
  await expect(chips).toHaveCount(0);
  await expect(link(doingTitle)).toHaveCount(0);
  await tabs.getByRole("button", { name: /^未結/ }).click();
  await link(doingTitle).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe(`/tasks/${doingId}`);
});

// DEV-107：搜尋框找得到編號、標題；沒找到時一鍵去搜已結束的。
test("TestTasksSearch", async ({ page, seed }) => {
  const tag = rand();
  const aTitle = `搜尋甲 ${tag}`;
  const bTitle = `搜尋乙 ${tag}`;
  await seed.doingTask(aTitle);
  const bId = await seed.statusTask(bTitle, "idea");
  await page.goto("/tasks");
  const search = page.getByRole("searchbox", { name: "搜尋任務單" });
  await search.fill(`甲 ${tag}`);
  await expect(page.getByRole("link", { name: new RegExp(aTitle) })).toBeVisible();
  await expect(page.getByRole("link", { name: new RegExp(bTitle) })).toHaveCount(0);
  await search.fill(bId);
  await expect(page.getByRole("link", { name: new RegExp(bTitle) })).toBeVisible();
  await expect(page.getByRole("link", { name: new RegExp(aTitle) })).toHaveCount(0);
  // 沒有符合的：說沒找到，並給「搜尋已結束的單」。
  await search.fill(`不存在的字 ${tag}`);
  await expect(page.getByText("沒有符合")).toBeVisible();
  await page.getByTestId("tasks-search-closed").click();
  await expect.poll(() => new URL(page.url()).searchParams.get("view")).toBe("closed");
  await page.getByRole("button", { name: "清除搜尋" }).click();
  await expect(search).toHaveValue("");
});

// DEV-107：手機寬度下狀態標籤換行，整頁沒有橫向捲軸。
test("TestTaskStatusChipsFitPhone", async ({ page, seed }) => {
  await seed.statusTask(`手機寬度 ${rand()}`, "idea");
  await page.setViewportSize({ width: 360, height: 780 });
  await page.goto("/tasks");
  const chips = page.getByRole("group", { name: "依狀態篩選" });
  for (const name of [/^等你處理/, /^進行中/, /^等外部/, /^待做/])
    await expect(chips.getByRole("button", { name })).toBeInViewport({ ratio: 1 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
});

// Guards: REQ-55
test("TestTaskAddLink", async ({ page, seed }) => {
  const id = await seed.doingTask(`加連結 ${rand()}`);
  await page.goto(`/tasks/${id}`);
  const url = `https://example.com/pr/${rand()}`;
  const box = page.getByLabel("加連結");
  await box.fill(url);
  await box.press("Enter");
  // 按 Enter 就存：chip 出現、輸入框清空、經 API 讀得到。
  await expect(page.getByRole("link", { name: url })).toBeVisible();
  await expect(box).toHaveValue("");
  await expect.poll(async () => (await taskOf(seed, id)).links ?? []).toContain(url);
});

// DEV-104／TSK-28：開好的單可以再加附件（選檔、貼截圖）；圖片直接顯示、其他是下載連結；拿掉只從單上拿掉，檔還在。
test("TestTaskAddAttachment", async ({ page, seed, hgServer }) => {
  const id = await seed.doingTask(`加附件 ${rand()}`);
  await page.goto(`/tasks/${id}`);
  const section = page.getByRole("region", { name: /^附件/ });
  await expect(section.getByText("還沒有附件。")).toBeVisible();
  const shot = `畫面-${rand()}.png`;
  await page.getByTestId("attach-file").setInputFiles({
    name: shot,
    mimeType: "image/png",
    buffer: PNG_1X1,
  });
  const img = section.getByRole("img", { name: shot });
  await expect(img).toBeVisible();
  await expect
    .poll(() => img.evaluate((el: HTMLImageElement) => el.naturalWidth))
    .toBeGreaterThan(0);
  // 點縮圖在這頁看大圖（WEB-132，不開新分頁），X 關掉。
  await section.getByRole("button", { name: `看大圖：${shot}` }).click();
  const viewer = page.getByTestId("media-viewer");
  await expect(viewer.getByRole("img", { name: shot })).toBeVisible();
  await viewer.getByRole("button", { name: "關閉" }).click();
  await expect(viewer).toHaveCount(0);
  // 不在輸入框裡按 ⌘V：整頁的貼上也算貼給這張單。
  await pasteImage(page.locator("body"));
  await expect(section.getByRole("img", { name: /^paste-\d+\.png$/ })).toBeVisible();
  const note = `說明-${rand()}.txt`;
  await page.getByTestId("attach-file").setInputFiles({
    name: note,
    mimeType: "text/plain",
    buffer: Buffer.from("hello"),
  });
  await expect(section.getByRole("link", { name: note })).toBeVisible();
  await expect.poll(async () => (await taskOf(seed, id)).attachments?.length ?? 0).toBe(3);
  const atts = (await taskOf(seed, id)).attachments ?? [];
  for (const url of atts) {
    expect(url).toMatch(/^\/share\//);
    expect(
      existsSync(path.join(hgServer.home, "runtime", "share", url.slice("/share/".length))),
    ).toBe(true);
  }
  // 拿掉圖片：單上少一個，檔案還在分享資料夾。
  const gone = atts.find((u) => u.endsWith(`-${shot}`)) ?? "";
  await section.getByRole("button", { name: `拿掉附件 ${shot}` }).click();
  await expect(img).toHaveCount(0);
  await expect.poll(async () => (await taskOf(seed, id)).attachments ?? []).not.toContain(gone);
  expect(
    existsSync(path.join(hgServer.home, "runtime", "share", gone.slice("/share/".length))),
  ).toBe(true);
});

// Guards: REQ-107
test("TestTaskNotFound", async ({ page }) => {
  await page.goto("/tasks/SEC-999999");
  await expect(page.getByRole("heading", { name: "找不到這張任務單" })).toBeVisible();
  await page.getByRole("link", { name: "回到任務單清單" }).last().click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/tasks");
});

// Guards: REQ-109
test("TestWideTaskPaneActions", async ({ page, seed, isMobile }, info) => {
  info.skip(isMobile, "右欄與鍵盤快捷鍵只在桌機寬版");
  const title = `右欄驗收 ${rand()}`;
  const id = await seed.reviewTask(title);
  await page.goto(`/?sel=${id}`);
  // 右欄是 /tasks/$id 的內容（同一個元件）＋動作列，鍵帽 A＝驗收、R＝退回。
  await expect(page.getByRole("heading", { name: title })).toBeVisible();
  await expect(page.getByRole("paragraph").filter({ hasText: `${title} 做完了` })).toBeVisible();
  const bar = page.getByRole("toolbar", { name: "任務單" });
  await expect(bar.getByRole("button", { name: /^驗收/ })).toContainText("A");
  // R 展開退回的理由（必填）；焦點在輸入框裡打 A 不會驗收。
  await page.locator(`[data-id="${id}"]`).focus();
  await page.keyboard.press("r");
  const reason = page.getByLabel("退回的理由");
  await expect(reason).toBeVisible();
  await reason.fill("aaa");
  expect((await taskOf(seed, id)).status).toBe("waiting_user");
  await reason.fill("");
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await page.locator(`[data-id="${id}"]`).focus();
  await page.keyboard.press("a");
  await expect.poll(async () => (await taskOf(seed, id)).status).toBe("done");
});

// WEB-129：用滑鼠在右欄按過東西（展開退回、再按取消）之後，焦點不在清單，按 A 照樣驗收；不用先點回清單。
test("TestWidePaneKeysAfterMouse", async ({ page, seed, isMobile }, info) => {
  info.skip(isMobile, "右欄與鍵盤快捷鍵只在桌機寬版");
  const title = `滑鼠之後 ${rand()}`;
  const id = await seed.reviewTask(title);
  await page.goto(`/?sel=${id}`);
  await expect(page.getByRole("heading", { name: title })).toBeVisible();
  const bar = page.getByRole("toolbar", { name: "任務單" });
  await bar.getByRole("button", { name: /^退回/ }).click();
  const reason = page.getByLabel("退回的理由");
  await expect(reason).toBeVisible();
  // 在理由裡打 A 不算驗收。
  await reason.fill("a");
  await page.keyboard.press("a");
  expect((await taskOf(seed, id)).status).toBe("waiting_user");
  await reason.fill("");
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await expect(reason).toHaveCount(0);
  await page.keyboard.press("a");
  await expect.poll(async () => (await taskOf(seed, id)).status).toBe("done");
});

// ── 看板開任務單（REQ-186）──────────────────────────────────────────────────

interface TaskFull extends TaskRecord {
  title: string;
  agent: string;
  type: string;
  body?: string;
  by?: string;
  created_at?: string;
  updated_at?: string;
}

async function findTask(
  seed: import("./fixtures").Seed,
  title: string,
): Promise<TaskFull | undefined> {
  const res = await seed.api<{ tasks?: TaskFull[] }>("/api/tasks/list", {});
  return (res.tasks ?? []).find((t) => t.title === title);
}

const newInput = (page: Page) => page.getByRole("textbox", { name: "交代秘書做什麼" });

// 選負責秘書：桌機在輸入框下方展開、手機開底部抽屜，兩邊都是「負責秘書」那一組頭像按鈕。
async function pickAgent(page: Page, name: string) {
  await page.getByRole("button", { name: /^(選一位秘書|換負責秘書)/ }).click();
  await page.getByRole("group", { name: "負責秘書" }).getByRole("button", { name }).click();
}

// 送出：手機在抽屜按「開單」，桌機在輸入框按 Enter。
async function sendNew(page: Page, isMobile: boolean) {
  if (isMobile) {
    await page
      .getByRole("dialog", { name: "交代秘書" })
      .getByRole("button", { name: "開單" })
      .click();
  } else {
    await newInput(page).press("Enter");
  }
}

// Guards: REQ-186
test("TestBoardCreatesTask @smoke", async ({ page, seed, isMobile }) => {
  const title = `看板開的單 ${rand()}`;
  await page.goto("/tasks?new=true");
  // 搜尋框有別的字也一樣：開完清掉搜尋字，剛開的那張看得到；網址的 new 拿掉。
  await page.getByRole("searchbox", { name: "搜尋任務單" }).fill(`不相干 ${rand()}`);
  await newInput(page).fill(title);
  await pickAgent(page, "Builder");
  await sendNew(page, isMobile);
  // toast 說開給誰（編號旁邊有白話），預設的「未結」立刻看得到，不用切分頁。
  await expect(page.getByText(/已開任務單 SEC-\d+ 給Builder/)).toBeVisible();
  expect(new URL(page.url()).searchParams.get("view")).toBeNull();
  await expect(page.getByRole("searchbox", { name: "搜尋任務單" })).toHaveValue("");
  await expect.poll(() => new URL(page.url()).searchParams.get("new")).toBeNull();
  const list = page.getByRole("list", { name: "任務單清單", exact: true });
  await expect(list.getByRole("link", { name: new RegExp(title) })).toBeVisible();
  // 單是待做、負責秘書是選的那位、開單的人是看板；還沒叫出專屬對話（DEV-124）。
  const task = await findTask(seed, title);
  expect(task?.status).toBe("idea");
  expect(task?.agent).toBe("code");
  expect(task?.type).toBe("task");
  expect(task?.by).toBe("owner(web)");
  const num = (task?.id ?? "").replace("SEC-", "");
  const sessions = await seed.api<{ sessions?: { name: string }[] }>("/api/session/list", {});
  expect((sessions.sessions ?? []).map((s) => s.name)).not.toContain(`code-sec-${num}`);
  // 輸入框清空；草稿不留。
  await expect(newInput(page)).toHaveValue("");

  // 詳情頁：按「開始做」才進行中、才開工作 session；之後才有「開專屬對話」。
  await list.getByRole("link", { name: new RegExp(title) }).click();
  await expect(page.getByRole("button", { name: "開專屬對話" })).toHaveCount(0);
  await page.getByRole("button", { name: "開始做" }).click();
  await expect(page.getByRole("button", { name: "開專屬對話" })).toBeVisible();
  await expect.poll(async () => (await findTask(seed, title))?.status).toBe("doing");
  await expect
    .poll(async () => {
      const r = await seed.api<{ sessions?: { name: string }[] }>("/api/session/list", {});
      return (r.sessions ?? []).map((s) => s.name);
    })
    .toContain(`code-sec-${num}`);
});

// Guards: REQ-186
test("TestBoardCreateTaskMoreOptions", async ({ page, seed, isMobile }) => {
  const title = `問一下 ${rand()}`;
  const body = `背景說明 ${rand()}`;
  await page.goto("/tasks");
  await newInput(page).fill(title);
  await pickAgent(page, "Helper");
  // 桌機要再展開「更多選項」；手機的抽屜本來就有。
  if (!isMobile) await page.getByRole("button", { name: "更多選項" }).click();
  await page.getByRole("button", { name: "問一下", exact: true }).click();
  await page.getByLabel("詳細說明").fill(body);
  await sendNew(page, isMobile);
  await expect(page.getByText(/已開任務單 SEC-\d+ 給Helper/)).toBeVisible();
  const task = await findTask(seed, title);
  expect(task?.agent).toBe("life");
  expect(task?.type).toBe("ask");
  expect(task?.body).toBe(body);
  expect(task?.status).toBe("idea");
});

// DEV-104／TSK-28：開單時貼截圖、選檔案當附件；傳好才能開單，開完單上就有、輸入框下的附件清掉。
test("TestBoardCreateTaskWithAttachments", async ({ page, seed, isMobile }) => {
  const title = `附圖開單 ${rand()}`;
  await page.goto("/tasks");
  await newInput(page).fill(title);
  await pasteImage(newInput(page));
  const doc = `規格-${rand()}.pdf`;
  await page.getByTestId("task-new-file").setInputFiles({
    name: doc,
    mimeType: "application/pdf",
    buffer: Buffer.from("%PDF-1.4"),
  });
  const list = page.getByRole("list", { name: "要附上的檔案" }).first();
  await expect(list.locator('[data-attachment="done"]')).toHaveCount(2);
  await expect(list.getByText(doc)).toBeVisible();
  // 貼圖不會把任何字貼進標題。
  await expect(newInput(page)).toHaveValue(title);
  await pickAgent(page, "Builder");
  await sendNew(page, isMobile);
  await expect(page.getByText(/已開任務單 SEC-\d+ 給Builder/)).toBeVisible();
  const task = await findTask(seed, title);
  expect(task?.attachments ?? []).toHaveLength(2);
  expect(task?.attachments?.[0]).toMatch(/^\/share\/.+-paste-\d+\.png$/);
  expect(task?.attachments?.[1]).toMatch(new RegExp(`^/share/.+-${doc.replace(".", "\\.")}$`));
  await expect(page.locator("[data-attachment]")).toHaveCount(0);
});

// Guards: REQ-186
test("TestBoardCreateTaskNeedsAgent", async ({ page, seed, isMobile }) => {
  const title = `沒選秘書 ${rand()}`;
  await page.goto("/tasks");
  await newInput(page).fill(title);
  if (isMobile) {
    // 手機：按送出鈕會開抽屜；抽屜裡「開單」停用，欄位下寫「選一位秘書」。
    await page.getByRole("button", { name: "開單", exact: true }).click();
    const sheet = page.getByRole("dialog", { name: "交代秘書" });
    await expect(sheet.getByRole("button", { name: "開單" })).toBeDisabled();
    await expect(sheet.getByText("選一位秘書", { exact: true })).toBeVisible();
  } else {
    await newInput(page).press("Enter");
    await expect(page.getByRole("alert").filter({ hasText: "選一位秘書" })).toBeVisible();
  }
  expect(await findTask(seed, title)).toBeUndefined();
});

// Guards: REQ-186
test("TestBoardCreateTaskDraftKept", async ({ page }) => {
  const title = `草稿 ${rand()}`;
  await page.goto("/tasks");
  await newInput(page).fill(title);
  await page.reload();
  await expect(newInput(page)).toHaveValue(title);
});

test("TestTaskDetailEditContent", async ({ page, seed }) => {
  const id = await seed.doingTask(`改內容 ${rand()}`);
  await page.goto(`/tasks/${id}`);
  const next = `新標題 ${rand()}`;
  await page.getByRole("button", { name: "改標題" }).click();
  const titleBox = page.getByLabel("標題");
  await titleBox.fill(next);
  await titleBox.press("Enter");
  await expect(page.getByRole("heading", { level: 1, name: next })).toBeVisible();
  await expect.poll(async () => (await taskOf(seed, id)).title).toBe(next);

  // 標題清空存不了，原因寫在欄位下；Esc 還原。
  await page.getByRole("button", { name: "改標題" }).click();
  await page.getByLabel("標題").fill("");
  await page.getByLabel("標題").press("Enter");
  await expect(page.getByRole("alert").filter({ hasText: "標題不能空白" })).toBeVisible();
  await page.getByLabel("標題").press("Escape");
  await expect(page.getByRole("heading", { level: 1, name: next })).toBeVisible();

  // 多行有「存」「取消」：取消不存。
  await page.getByRole("button", { name: "改需求" }).click();
  await page.getByLabel("需求").fill("不要的內容");
  await page.getByRole("button", { name: "取消" }).click();
  await expect(page.getByLabel("需求")).toHaveCount(0);
  await expect.poll(async () => (await taskOf(seed, id)).body).not.toBe("不要的內容");

  await page.getByRole("button", { name: "改需求" }).click();
  await page.getByLabel("需求").fill("第一行\n第二行");
  await page.getByRole("button", { name: "存", exact: true }).click();
  await expect(page.getByText("第一行")).toBeVisible();
  await expect.poll(async () => (await taskOf(seed, id)).body).toBe("第一行\n第二行");

  // 秘書寫的條件，主人改了就算主人的。
  await expect(page.getByText(/^秘書寫的條件/)).toBeVisible();
  const rule = `打開看得到 ${rand()}`;
  await page.getByRole("button", { name: "改驗收條件" }).click();
  await page.getByLabel("驗收條件").fill(rule);
  await page.getByRole("button", { name: "存", exact: true }).click();
  await expect(page.getByText(rule)).toBeVisible();
  await expect.poll(async () => (await taskOf(seed, id)).criteria).toBe(rule);
  await expect(page.getByText(/^你給的條件/)).toBeVisible();

  // 重新整理還在。
  await page.reload();
  await expect(page.getByRole("heading", { level: 1, name: next })).toBeVisible();
  await expect(page.getByText(rule)).toBeVisible();
});

// WEB-161：右上角「編輯」打開整張表單，一次改好幾個欄位、儲存一次；標題空白擋下、表單不關。
test("TestTaskDetailEditForm", async ({ page, seed }) => {
  const id = await seed.doingTask(`整張改 ${rand()}`);
  await page.goto(`/tasks/${id}`);
  await page.getByRole("button", { name: "編輯", exact: true }).click();
  const form = page.getByRole("dialog", { name: "編輯任務單" });
  await expect(form).toBeVisible();

  // 標題清空：擋下、原因寫在欄位下、表單不關。
  await form.getByLabel("標題").fill("  ");
  await form.getByRole("button", { name: "儲存", exact: true }).click();
  await expect(form.getByRole("alert").filter({ hasText: "標題不能空白" })).toBeVisible();
  await expect(form).toBeVisible();

  const next = `整張改過 ${rand()}`;
  const rule = `打開看得到 ${rand()}`;
  await form.getByLabel("標題").fill(next);
  // 換一位負責秘書（原本是 code）。
  const other = form
    .getByRole("group", { name: "負責秘書" })
    .getByRole("button", { pressed: false });
  // 按鈕裡是頭像（可能是名字的第一個字）＋名字：取最後一行當名字。
  const otherName = (await other.first().innerText()).trim().split("\n").pop()?.trim() ?? "";
  await other.first().click();
  await form.getByLabel("驗收條件").fill(rule);
  await form.getByRole("button", { name: "儲存", exact: true }).click();
  await expect(form).toHaveCount(0);

  await expect(page.getByRole("heading", { level: 1, name: next })).toBeVisible();
  await expect(page.getByText(rule)).toBeVisible();
  await expect(page.getByText(/^你給的條件/)).toBeVisible();
  await expect(page.getByText(otherName, { exact: true }).first()).toBeVisible();
  const saved = await taskOf(seed, id);
  expect(saved.title).toBe(next);
  expect(saved.criteria).toBe(rule);
  expect(saved.agent).not.toBe("code");

  // 取消不動。
  await page.getByRole("button", { name: "編輯", exact: true }).click();
  await page.getByRole("dialog", { name: "編輯任務單" }).getByLabel("標題").fill("不要的標題");
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: next })).toBeVisible();
  expect((await taskOf(seed, id)).title).toBe(next);
});
