import { expect, type Seed, shot, test } from "./fixtures";

// 不吵的通知的截圖（P7-6）：討論串裡的頁內卡片（標題欄有 hot cache 倒數）、別頁的站內提示。手機亮／暗、桌機亮。

const rand = () => Math.random().toString(36).slice(2, 8);
const num = (id: string) => id.replace("SEC-", "");

async function nudgeFor(seed: Seed, session: string, task: string) {
  await seed.api("/api/notify/nudge", {
    agent: "code",
    session,
    task,
    title: `${task} 還在等你`,
    context: "整理三月報表（工作 session 再 10 分鐘會關掉，之後接回來會比較慢）",
    todo: "請確認要不要上線",
    by: { name: "code" },
  });
}

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-225, REQ-228
  test(`TestShotsNudge @shots ${scheme}`, async ({ page, seed, isMobile }, info) => {
    const d = isMobile ? "mobile" : "desktop";
    info.skip(d === "desktop" && scheme === "dark", "桌機只拍亮色");
    test.setTimeout(90_000);
    await page.emulateMedia({ colorScheme: scheme });
    // 好幾個視窗同時開著時，被蓋住的視窗會變成「背景分頁」（看板會回報不在了、提醒就不顯示）：截圖固定當前景。
    await page.addInitScript(() => {
      Object.defineProperty(document, "visibilityState", { get: () => "visible" });
      Object.defineProperty(document, "hidden", { get: () => false });
    });
    await seed.closeAllWork();
    const task = await seed.doingTask(`整理三月報表 ${rand()}`);
    const session = `code-sec-${num(task)}`;
    await seed.api("/api/session/open", { agent: "code", topic: task, by: { name: "owner(web)" } });
    await seed.api("/api/bus/send", {
      from: "owner(web)",
      to: session,
      kind: "chat",
      body: "開工",
    });

    // 站內提示：看板開著、在別頁。
    const away = page.waitForResponse(
      (r) =>
        r.url().includes("/api/notify/presence") &&
        r.request().postData()?.includes('"/reminders"') === true,
    );
    await page.goto("/reminders");
    await away;
    await nudgeFor(seed, session, task);
    await expect(page.getByTestId("nudge-toast")).toBeVisible({ timeout: 20_000 });
    await page.waitForTimeout(400);
    await shot(page, `nudge-toast-${d}-${scheme}`);

    // 頁內卡片：去看看之後，同一則提示變成討論串裡的卡片。
    await page.getByTestId("nudge-toast").getByRole("link", { name: "去看看" }).click();
    await expect(page.getByTestId("nudge-card")).toBeVisible({ timeout: 20_000 });
    await expect(page.getByTestId("thread-status")).toContainText("快取", { timeout: 20_000 });
    await page.waitForTimeout(1500); // 對話記錄的空狀態緩衝
    await shot(page, `nudge-card-${d}-${scheme}`);

    // 離開這個討論串，讓伺服器收掉頁內卡片那則提示（同一個 worker 的下一個測試才不會多出一則舊的站內提示）。
    // 完整版 Chrome 關分頁時會送「不在了」（pagehide）；headless shell 不會，所以這裡明確離開一次。
    const gone = page.waitForRequest(
      (r) =>
        r.url().includes("/api/notify/presence") && r.postData()?.includes('"away":true') === true,
    );
    await page.goto("about:blank");
    await gone;
  });
}
