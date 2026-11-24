import type { Page } from "@playwright/test";
import { expect, test } from "./fixtures";

const rand = () => Math.random().toString(36).slice(2, 8);

interface Notice {
  id: string;
  state: string;
  answer?: { text: string; via: string };
}

// 造一則要你同意的通知，選項自己指定（seed.approve 的選項是「同意」「不要」）。
async function approveWith(
  seed: import("./fixtures").Seed,
  title: string,
  options: string[] | undefined,
): Promise<string> {
  const res = await seed.api<{ notification: { id: string } }>("/api/notify/send", {
    agent: "code",
    category: "approve",
    title,
    context: `${title} 的說明`,
    todo: "要不要同意這件事？",
    ...(options ? { options } : {}),
  });
  return res.notification.id;
}

async function noticeOf(seed: import("./fixtures").Seed, id: string): Promise<Notice> {
  const res = await seed.api<{ notification: Notice }>("/api/notify/get", { id });
  return res.notification;
}

const mainNav = (page: Page) => page.getByRole("navigation", { name: "主選單" });

// Guards: REQ-138
test("TestApproveLinkShowsButtons @smoke", async ({ page, seed }) => {
  const title = `要不要訂位 ${rand()}`;
  const id = await approveWith(seed, title, ["好", "不要"]);
  await page.goto(`/messages/${id}`);

  // 選項按鈕在落地畫面的第一屏：不用捲動（手機 390×844，按鈕底邊要在 844 以內）。
  const yes = page.getByRole("button", { name: "好", exact: true });
  await expect(yes).toBeVisible();
  const box = await yes.boundingBox();
  const height = page.viewportSize()?.height ?? 0;
  expect(box).not.toBeNull();
  expect((box?.y ?? 0) + (box?.height ?? 0)).toBeLessThanOrEqual(height);
  // 第一個是實心的主要按鈕；「或打字回答」一行也在。
  await expect(page.getByRole("button", { name: "不要", exact: true })).toBeVisible();
  await expect(page.getByLabel("或打字回答")).toBeVisible();

  // 按下立刻顯示「已回」（不等伺服器），按鈕收起。
  await yes.click();
  await expect(page.getByRole("status").filter({ hasText: "已回：好" })).toBeVisible();
  await expect(page.getByRole("button", { name: "不要", exact: true })).toHaveCount(0);
  await expect.poll(async () => (await noticeOf(seed, id)).state).toBe("answered");
  const n = await noticeOf(seed, id);
  expect(n.answer?.text).toBe("好");
  expect(n.answer?.via).toBe("button");
});

// Guards: REQ-139
test("TestTypedAnswerCounts", async ({ page, seed }) => {
  const title = `打字也算 ${rand()}`;
  const id = await approveWith(seed, title, ["同意", "不要"]);
  await page.goto(`/messages/${id}`);
  const typed = `改週日 ${rand()}`;
  await page.getByLabel("或打字回答").fill(typed);
  await page.getByLabel("或打字回答").press("Enter");
  await expect(page.getByText(`已回：${typed}`)).toBeVisible();
  // 打字回答也算數：通知變 answered，答案是打的字，via 是 chat。
  await expect.poll(async () => (await noticeOf(seed, id)).state).toBe("answered");
  const n = await noticeOf(seed, id);
  expect(n.answer?.text).toBe(typed);
  expect(n.answer?.via).toBe("chat");
});

// Guards: REQ-138
test("TestApproveWithoutOptionsUsesYesNo", async ({ page, seed }) => {
  const id = await approveWith(seed, `沒有選項 ${rand()}`, undefined);
  await page.goto(`/messages/${id}`);
  await expect(page.getByRole("button", { name: "好", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "不要", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "不要", exact: true }).click();
  await expect.poll(async () => (await noticeOf(seed, id)).answer?.text).toBe("不要");
});

// Guards: REQ-143
test("TestOpenMessageMarksRead @smoke", async ({ page, seed }) => {
  const title = `打開就已讀 ${rand()}`;
  const id = await seed.api<{ notification: { id: string } }>("/api/notify/send", {
    agent: "life",
    category: "done",
    title,
    context: "這只是通知",
  });
  const nid = id.notification.id;
  expect((await noticeOf(seed, nid)).state).toBe("unread");
  await page.goto(`/messages/${nid}`);
  await expect(page.getByRole("heading", { name: title })).toBeVisible();
  await expect(page.getByText("這則只是通知，不用回答。")).toBeVisible();
  await expect.poll(async () => (await noticeOf(seed, nid)).state).toBe("read");
});

// Guards: REQ-107
test("TestMessagesListFilters", async ({ page, seed, isMobile }) => {
  const approveTitle = `清單要同意 ${rand()}`;
  const doneTitle = `清單只是通知 ${rand()}`;
  const approveId = await approveWith(seed, approveTitle, ["好", "不要"]);
  await seed.api("/api/notify/send", {
    agent: "life",
    category: "done",
    title: doneTitle,
    context: "做完了",
  });
  await page.goto("/messages");
  if (!isMobile) await expect(page.getByRole("heading", { name: "訊息" })).toBeVisible();
  // 每一列：分類白話、編號（等寬字）、標題。
  const list = page.getByRole("list", { name: "訊息清單" });
  const approveRow = list.getByRole("link", { name: new RegExp(approveTitle) });
  await expect(approveRow).toBeVisible();
  await expect(approveRow).toContainText("要你同意");
  await expect(approveRow).toContainText(approveId);
  await expect(list.getByRole("link", { name: new RegExp(doneTitle) })).toContainText("做完了");

  // 分段：要你同意只剩還沒回答的；選擇寫在網址（重新整理還在）。
  await page.getByRole("button", { name: /^要你同意/ }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get("show")).toBe("approve");
  await expect(list.getByRole("link", { name: new RegExp(approveTitle) })).toBeVisible();
  await expect(list.getByRole("link", { name: new RegExp(doneTitle) })).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole("button", { name: /^要你同意/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );

  // 回答之後，它從「要你同意」消失，在「全部」裡寫著「已回」。
  await page.goto(`/messages/${approveId}`);
  await page.getByRole("button", { name: "好", exact: true }).click();
  await expect.poll(async () => (await noticeOf(seed, approveId)).state).toBe("answered");
  await page.goto("/messages?show=approve");
  await expect(page.getByRole("button", { name: /^要你同意/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await expect(list.getByRole("link", { name: new RegExp(approveTitle) })).toHaveCount(0);
  await page.goto("/messages?show=all");
  await expect(list.getByRole("link", { name: new RegExp(approveTitle) })).toContainText(
    "已回「好」",
  );
});

// Guards: REQ-231
test("TestMessagesUnreadFirst", async ({ page, seed }) => {
  const title = `未讀在前 ${rand()}`;
  const res = await seed.api<{ notification: { id: string } }>("/api/notify/send", {
    agent: "life",
    category: "done",
    title,
    context: "看過就不在未讀",
  });
  // 分段第一個是「未讀」，沒帶 ?show= 就是它。
  await page.goto("/messages");
  const tabs = page.getByRole("group", { name: "篩選訊息" }).getByRole("button");
  await expect(tabs.first()).toHaveText(/^未讀/);
  await expect(tabs.first()).toHaveAttribute("aria-pressed", "true");
  const list = page.getByRole("list", { name: "訊息清單" });
  await expect(list.getByRole("link", { name: new RegExp(title) })).toBeVisible();
  // 讀過之後只在「全部」裡。
  await seed.api("/api/notify/mark_read", { ids: [res.notification.id] });
  await page.goto("/messages");
  await expect(list.getByRole("link", { name: new RegExp(title) })).toHaveCount(0);
  await page.goto("/messages?show=all");
  await expect(list.getByRole("link", { name: new RegExp(title) })).toBeVisible();
});

// Guards: REQ-230, REQ-267
test("TestMessageDeleteInPlace", async ({ page, seed }) => {
  const title = `原地刪除 ${rand()}`;
  const done = await seed.api<{ notification: { id: string } }>("/api/notify/send", {
    agent: "life",
    category: "done",
    title,
    context: "刪掉也沒關係",
  });
  const pendingTitle = `還在等的 ${rand()}`;
  const pendingId = await approveWith(seed, pendingTitle, ["好", "不要"]);
  await page.goto("/messages?show=all");
  const list = page.getByRole("list", { name: "訊息清單" });
  await expect(list.getByRole("link", { name: new RegExp(title) })).toBeVisible();
  // 還沒回答的「要你同意」也有刪除鈕（REQ-267：主人說刪就刪）。
  await expect(list.getByRole("button", { name: `刪除 ${pendingId}` })).toBeVisible();
  // 兩段式：第一下變「確定刪除？」，再按才刪；不開彈出視窗。
  await list.getByRole("button", { name: `刪除 ${done.notification.id}` }).click();
  await list.getByRole("button", { name: "確定刪除？" }).click();
  await expect(list.getByRole("link", { name: new RegExp(title) })).toHaveCount(0);
  await expect
    .poll(async () => {
      const res = await seed.api<{ notifications?: { id: string }[] }>("/api/notify/list", {});
      return (res.notifications ?? []).some((n) => n.id === done.notification.id);
    })
    .toBe(false);
  await expect(list.getByRole("link", { name: new RegExp(pendingTitle) })).toBeVisible();
});

// Guards: REQ-231
test("TestMessagesWideTwoPane", async ({ page, seed, isMobile }, info) => {
  info.skip(isMobile, "兩欄只在 ≥ 1024px");
  const title = `兩欄打開 ${rand()}`;
  const res = await seed.api<{ notification: { id: string } }>("/api/notify/send", {
    agent: "life",
    category: "done",
    title,
    context: "從通知連結進來",
  });
  const id = res.notification.id;
  // 推播點進來的網址 /messages/<編號>：左邊是清單（這一則被選取），右邊是內容。
  await page.goto(`/messages/${id}`);
  const list = page.getByRole("list", { name: "訊息清單" });
  await expect(list.getByRole("link", { name: new RegExp(title) })).toHaveAttribute(
    "aria-current",
    "page",
  );
  await expect(page.getByRole("heading", { name: title })).toBeVisible();
  // 打開就已讀，但還留在「未讀」清單裡（不會一點就消失）。
  await expect.poll(async () => (await noticeOf(seed, id)).state).toBe("read");
  await expect(list.getByRole("link", { name: new RegExp(title) })).toBeVisible();
});

// Guards: REQ-107
test("TestMessageNotFound", async ({ page }) => {
  await page.goto("/messages/M-999999");
  await expect(page.getByRole("heading", { name: "找不到這則訊息" })).toBeVisible();
  // 桌機的詳細頁上方還有一個一樣的返回連結；點畫面中央那個。
  await page.getByRole("link", { name: "回到訊息清單" }).last().click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/messages");
});

// Guards: REQ-139
test("TestTodayCardAnswerCollapses", async ({ page, seed, isMobile }, info) => {
  info.skip(!isMobile, "卡片上的就地回答是單欄版；桌機寬版在右欄");
  const title = `卡片同意 ${rand()}`;
  const id = await approveWith(seed, title, ["好", "不要"]);
  await page.goto("/");
  const card = page.locator(`[data-card="${id}"]`);
  await expect(card).toBeVisible();
  // 手機的卡片只放選項按鈕和一個「打字回答」連結。
  await expect(card.getByRole("link", { name: "打字回答" })).toHaveAttribute(
    "href",
    `/messages/${id}`,
  );
  await card.getByRole("button", { name: "好", exact: true }).click();
  // 按下立刻收成綠色一列，不給復原。
  await expect(page.locator(`[data-card="${id}"]`)).toContainText(`已回「好」· ${id} ${title}`);
  await expect(page.locator(`[data-card="${id}"]`).getByRole("button")).toHaveCount(0);
  await expect.poll(async () => (await noticeOf(seed, id)).answer?.text).toBe("好");
  // 之後伺服器的資料更新，這一列就不在清單裡了。
  await expect(page.locator(`[data-card="${id}"]`)).toHaveCount(0, { timeout: 15_000 });
});

// Guards: REQ-139
test("TestCardAnswerFailureRestores", async ({ page, seed, isMobile }, info) => {
  info.skip(!isMobile, "卡片上的就地回答是單欄版");
  const title = `卡片失敗 ${rand()}`;
  const id = await approveWith(seed, title, ["好", "不要"]);
  await page.route("**/api/notify/answer", (route) =>
    route.fulfill({
      status: 409,
      contentType: "application/json",
      body: JSON.stringify({ code: "conflict", message: "這則已經在別處回答過了" }),
    }),
  );
  await page.goto("/");
  const card = page.locator(`[data-card="${id}"]`);
  await card.getByRole("button", { name: "好", exact: true }).click();
  // 送出失敗：卡片還原，錯誤寫在按鈕下，按鈕還能再按。
  await expect(card.getByRole("alert")).toContainText("這則已經在別處回答過了");
  await expect(card.getByRole("button", { name: "好", exact: true })).toBeVisible();
  expect((await noticeOf(seed, id)).state).not.toBe("answered");
});

// Guards: REQ-139
test("TestWidePaneKeyboardAnswer", async ({ page, seed, isMobile }, info) => {
  info.skip(isMobile, "右欄與鍵盤快捷鍵只在桌機寬版");
  const a = `鍵盤甲 ${rand()}`;
  const b = `鍵盤乙 ${rand()}`;
  const idA = await approveWith(seed, a, ["好", "不要"]);
  const idB = await approveWith(seed, b, ["好", "不要"]);
  await page.goto(`/?sel=${idA}`);
  // 右欄是完整內容＋固定在底部的動作列，按鈕旁有鍵帽 A／R。
  const pane = page.getByRole("toolbar", { name: "選項" });
  await expect(page.getByRole("heading", { name: a })).toBeVisible();
  await expect(pane.getByRole("button", { name: /^好/ })).toContainText("A");
  await expect(pane.getByRole("button", { name: /^不要/ })).toContainText("R");
  await expect(page.getByRole("paragraph").filter({ hasText: `${a} 的說明` })).toBeVisible();

  // 焦點在清單時，A 就是主要動作（第一個選項）；做完選取跳到下一件，不是留在原地。
  await page.locator(`[data-id="${idA}"]`).focus();
  await page.keyboard.press("a");
  await expect.poll(async () => (await noticeOf(seed, idA)).answer?.text).toBe("好");
  await expect.poll(() => new URL(page.url()).searchParams.get("sel")).not.toBe(idA);

  // 焦點在輸入框時，A／R 照常打字，不會觸發動作。
  await page.goto(`/?sel=${idB}`);
  const typed = page.getByLabel("或打字回答");
  await typed.fill("ar");
  await expect(typed).toHaveValue("ar");
  expect((await noticeOf(seed, idB)).state).not.toBe("answered");
  // 收尾：R＝第二個選項。
  await page.locator(`[data-id="${idB}"]`).focus();
  await page.keyboard.press("r");
  await expect.poll(async () => (await noticeOf(seed, idB)).answer?.text).toBe("不要");
  await expect(mainNav(page)).toBeVisible();
});

// 訊息分「秘書｜系統」（D-47、WEB-86）：自動報告進系統分頁、同一種收成一列 ×N，刪整組；秘書分頁不被洗版。
test("TestMessagesSystemLaneCollapses", async ({ page, seed }) => {
  const tag = rand().replace(/\d/g, "x");
  const agentTitle = `秘書說的話 ${tag}`;
  await seed.api("/api/notify/send", { agent: "life", category: "done", title: agentTitle });
  const ids: string[] = [];
  for (const hash of ["8c4e971f", "c703aef8", "6c27669e"]) {
    const res = await seed.api<{ notification: { id: string; lane?: string } }>(
      "/api/notify/send",
      {
        agent: "admin",
        category: "done",
        lane: "system",
        title: `夜測沒過 ${tag}：${hash}（false）`,
      },
    );
    expect(res.notification.lane).toBe("system");
    ids.push(res.notification.id);
  }
  await page.goto("/messages");
  const list = page.getByRole("list", { name: "訊息清單" });
  const lanes = page.getByRole("navigation", { name: "訊息分類" });
  const agentTab = lanes.getByRole("link", { name: /^秘書/ });
  const systemTab = lanes.getByRole("link", { name: /^系統/ });
  // 兩個分頁都連 /messages：只有目前這一道是 aria-current（TanStack 不能把兩個都標成目前這頁）。
  await expect(agentTab).toHaveAttribute("aria-current", "page");
  await expect(systemTab).not.toHaveAttribute("aria-current", "page");
  await expect(list.getByRole("link", { name: new RegExp(agentTitle) })).toBeVisible();
  await expect(list.getByText(new RegExp(`夜測沒過 ${tag}`))).toHaveCount(0);

  await systemTab.click();
  await expect.poll(() => new URL(page.url()).searchParams.get("lane")).toBe("system");
  await expect(systemTab).toHaveAttribute("aria-current", "page");
  await expect(agentTab).not.toHaveAttribute("aria-current", "page");
  const group = list.getByTestId("notice-group").filter({ hasText: `夜測沒過 ${tag}` });
  await expect(group).toHaveCount(1);
  await expect(group).toHaveAttribute("data-count", "3");
  await expect(group).toContainText("×3");
  await expect(list.getByRole("link", { name: new RegExp(agentTitle) })).toHaveCount(0);
  // 展開看每一則，再刪整組（兩段式），三則都封存。
  await group.getByRole("button", { name: /收起來的 3 則/ }).click();
  await expect(group.getByRole("list", { name: "這 3 則" }).getByRole("link")).toHaveCount(3);
  await group.getByRole("button", { name: "刪除這 3 則" }).click();
  await group.getByRole("button", { name: "確定刪除？" }).click();
  await expect(list.getByText(new RegExp(`夜測沒過 ${tag}`))).toHaveCount(0);
  await expect
    .poll(async () => {
      const res = await seed.api<{ notifications?: { id: string }[] }>("/api/notify/list", {});
      return (res.notifications ?? []).filter((n) => ids.includes(n.id)).length;
    })
    .toBe(0);
});

// 通知沒有專屬的對話時：「跟 <她> 聊這則」到她的主要討論串，輸入框先起個頭「關於「標題」：」。
test("TestNoticeWithoutRoomOpensChatWithDraft", async ({ page, seed }) => {
  const title = `幫我看報表 ${rand()}`;
  const res = await seed.api<{ notification: { id: string } }>("/api/notify/send", {
    agent: "code",
    category: "done",
    title,
  });
  await page.addInitScript(() => {
    try {
      localStorage.removeItem("eg.draft.chat.code");
    } catch {
      // 沒有 localStorage 就算了
    }
  });
  await page.goto(`/messages/${res.notification.id}`);
  const link = page.getByTestId("message-chat-link");
  await expect(link).toContainText("聊這則");
  await link.click();
  await expect(page).toHaveURL(/\/chat\/code$/);
  await expect(page.locator("#hg-composer")).toHaveValue(`關於「${title}」：`);
});
