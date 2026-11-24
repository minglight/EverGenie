import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { expect, shot, test } from "./fixtures";

// 工作 session 的截圖（P7-5）：秘書底下展開、超過上限的討論串內提問。手機亮／暗、桌機亮。

const rand = () => Math.random().toString(36).slice(2, 8);
const num = (id: string) => id.replace("SEC-", "");

for (const scheme of ["light", "dark"] as const) {
  // Guards: REQ-204, REQ-220
  test(`TestShotsWorkSessions @shots ${scheme}`, async ({
    page,
    seed,
    hgServer,
    isMobile,
  }, info) => {
    const d = isMobile ? "mobile" : "desktop";
    info.skip(d === "desktop" && scheme === "dark", "桌機只拍亮色");
    test.setTimeout(90_000);
    const settings = path.join(hgServer.home, "settings", "sessions.toml");
    mkdirSync(path.dirname(settings), { recursive: true });
    await seed.closeAllWork();
    writeFileSync(settings, "work_max = 2\n");
    try {
      await page.addInitScript(() => {
        try {
          localStorage.setItem("eg.chatPane.v1", "list");
        } catch {
          // 沒有 localStorage 就用預設
        }
      });
      await page.emulateMedia({ colorScheme: scheme });
      const ids: string[] = [];
      for (const title of ["整理三月報表", "修好登入頁的錯誤", "寫週報草稿"]) {
        ids.push(await seed.doingTask(`${title} ${rand()}`));
      }
      for (const id of ids) {
        await seed.api("/api/session/open", {
          agent: "code",
          topic: id,
          by: { name: "owner(web)" },
        });
      }
      await page.goto("/chat");
      await expect(page.locator(`[data-work-task="${ids[0]}"]`)).toBeVisible({ timeout: 20_000 });
      await expect(page.locator(`[data-work-task="${ids[2]}"]`)).toContainText("要你同意", {
        timeout: 20_000,
      });
      await page.waitForTimeout(500);
      await shot(page, `work-list-${d}-${scheme}`);

      await page.goto(`/chat/code-sec-${num(ids[2] ?? "")}`);
      await expect(page.getByTestId("work-ask")).toBeVisible({ timeout: 20_000 });
      await page.waitForTimeout(1500); // 對話記錄的空狀態要等一下才出現（useChatData 的 grace），不拍轉圈
      await shot(page, `work-ask-${d}-${scheme}`);
    } finally {
      rmSync(settings, { force: true });
      await seed.closeAllWork();
    }
  });
}
