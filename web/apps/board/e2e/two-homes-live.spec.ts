import type { Page } from "@playwright/test";
import { expect, type Seed, type ServeProxy, twoHomesTest as test } from "./fixtures";

// S3-8 即時、在場、推播多台：A 的看板對每台能用的台各開一組串流、各報一次在場（data/live.ts、data/viewing.ts）。
// 「B 改一張單，A 的畫面 3 秒內更新」用 B 的「還在等你」提醒驗：B 的單一移動，B 收回提醒、通知串流送出變動，
// A 的看板重報 B 的在場、拿到新清單，站內提示跟著消失——全程沒有輪詢（在場每 30 秒，遠大於 3 秒）。
// 只跑桌機（沒有手機專屬內容，不列進 mobileSpecs）。兩個家整個 spec 共用，資料帶亂數（TST-13）。

const rand = () => Math.random().toString(36).slice(2, 8);
const num = (id: string) => id.replace("SEC-", "");

// 在 B 開一張單的工作 session（同 chat-nudge.spec.ts 的 workThread，作用在 B）。
async function workThread(seed: Seed) {
  await seed.closeAllWork();
  const task = await seed.doingTask(`多台提醒 ${rand()}`);
  await seed.api("/api/session/open", { agent: "code", topic: task, by: { name: "owner(web)" } });
  return { task, session: `code-sec-${num(task)}` };
}

const nudge = (seed: Seed, session: string, task: string) =>
  seed.api<{ nudge: { id: string; surface: string } }>("/api/notify/nudge", {
    agent: "code",
    session,
    task,
    title: `${task} 還在等你`,
    todo: "請確認要不要上線",
    by: { name: "code" },
  });

// A 的看板（origin＝A）打到某台的 POST。
const fromBoard = (proxy: ServeProxy, origin: string, path: string) =>
  proxy.forwarded.filter(
    (f) => f.method === "POST" && f.path === path && f.headers.origin === origin,
  );

// A 的看板報給 B 的在場（瀏覽器端看得到內容）。
function presenceTo(page: Page, base: string) {
  const bodies: { path?: string; thread?: string; away?: boolean }[] = [];
  page.on("request", (r) => {
    if (r.method() === "POST" && r.url() === `${base}/api/notify/presence`) {
      try {
        bodies.push(r.postDataJSON());
      } catch {
        // 不是 JSON：不算。
      }
    }
  });
  return bodies;
}

test("TestHomeBTaskChangeUpdatesBoardAWithin3s", async ({ page, homes }) => {
  test.setTimeout(90_000);
  const b = homes.b.seed;
  const { task, session } = await workThread(b);
  // 單要先在「等你回覆」，離開它才算回答（提醒才會被收回）。
  await b.api("/api/tasks/update", {
    id: task,
    status: "waiting_user",
    ask: "要不要上線？",
    by: { name: "code" },
  });
  const toB = presenceTo(page, homes.b.url);
  await page.goto("/reminders");
  // A 的看板也報給 B：B 才知道主人在看板前（提醒出在看板、不推手機）。看的不是 B 的東西，所以報 ELSEWHERE。
  await expect.poll(() => toB.length, { timeout: 20_000 }).toBeGreaterThan(0);
  expect(toB[0]).toMatchObject({ path: "/", thread: "", away: false });

  const res = await nudge(b, session, task);
  expect(res.nudge.surface).toBe("toast");
  const toast = page.getByTestId("nudge-toast");
  await expect(toast).toBeVisible({ timeout: 3_000 });
  await expect(toast).toContainText(`${task} 還在等你`);
  // 別台的提醒：去看看帶 ?home=<B>。
  const href = await toast.getByRole("link", { name: "去看看" }).getAttribute("href");
  const url = new URL(href ?? "", homes.a.url);
  expect(url.pathname).toBe(`/chat/${session}`);
  expect(url.searchParams.get("home")).toBe(homes.b.label);

  // B 改單：B 收回提醒，A 的畫面 3 秒內跟著收掉。
  await b.api("/api/tasks/update", { id: task, status: "doing", by: { name: "code" } });
  await expect(toast).toHaveCount(0, { timeout: 3_000 });
  await b.closeAllWork();
});

// 在場送給正在看的那筆所屬的台：開 B 的對話（?home=B），B 收到真的路徑與討論串，提醒是頁內卡片（inline），
// 主伺服器拿 ELSEWHERE；「知道了」送 B。
test("TestViewingHomeBThreadReportsToB", async ({ page, homes }) => {
  test.setTimeout(90_000);
  const b = homes.b.seed;
  const { task, session } = await workThread(b);
  await b.api("/api/tasks/update", {
    id: task,
    status: "waiting_user",
    ask: "要不要上線？",
    by: { name: "code" },
  });
  const toA = presenceTo(page, homes.a.url);
  const toB = presenceTo(page, homes.b.url);
  await page.goto(`/chat/${session}?home=${encodeURIComponent(homes.b.label)}`);
  await expect
    .poll(() => toB.some((x) => x.path === `/chat/${session}` && x.thread === session), {
      timeout: 20_000,
    })
    .toBe(true);
  await expect
    .poll(() => toA.some((x) => x.path === "/" && x.thread === ""), {
      timeout: 20_000,
    })
    .toBe(true);
  expect(toA.some((x) => x.path === `/chat/${session}`)).toBe(false);

  const res = await nudge(b, session, task);
  expect(res.nudge.surface).toBe("inline");
  const card = page.getByTestId("nudge-card");
  await expect(card).toBeVisible({ timeout: 3_000 });
  await expect(page.getByTestId("nudge-toast")).toHaveCount(0);
  const resolved = page.waitForRequest(
    (r) => r.method() === "POST" && r.url() === `${homes.b.url}/api/notify/resolve_nudge`,
  );
  await card.getByRole("button", { name: "知道了" }).click();
  await resolved;
  await expect(card).toHaveCount(0, { timeout: 3_000 });
  await b.closeAllWork();
});

test("TestHomeBDownKeepsBoardALiveAndReconnects", async ({ page, homes, seed }) => {
  test.setTimeout(90_000);
  const b = homes.b.seed;
  const { task, session } = await workThread(b);
  await page.goto("/");
  const watchA = () => fromBoard(homes.a.proxy, homes.a.url, "/api/notify/watch").length;
  const watchB = () => fromBoard(homes.b.proxy, homes.a.url, "/api/notify/watch").length;
  // A 的看板對兩台都開了通知串流。
  await expect.poll(watchB, { timeout: 20_000 }).toBeGreaterThan(0);
  await expect.poll(watchA, { timeout: 20_000 }).toBeGreaterThan(0);

  // B 整台不在：A 自己的即時照常（A 新的通知馬上出現），A 的串流沒有跟著重開。
  const before = watchA();
  homes.b.proxy.mode = "reset";
  const title = `B 斷線時的即時 ${rand()}`;
  await seed.approve(title);
  await expect(page.getByText(title).first()).toBeVisible({ timeout: 3_000 });
  expect(watchA()).toBe(before);
  // 別台連不上不是「看板連不上」：頁首不出現那條（那台另有自己的說明，FEN-28）。
  await expect(page.getByTestId("offline-banner")).toHaveCount(0);

  // B 回來：A 的看板自己接回 B 的串流（不用重新整理），B 的變動又能即時到 A。
  homes.b.proxy.forwarded.length = 0;
  homes.b.proxy.mode = "up";
  await expect.poll(watchB, { timeout: 20_000 }).toBeGreaterThan(0);
  await expect
    .poll(() => fromBoard(homes.b.proxy, homes.a.url, "/api/notify/presence").length, {
      timeout: 20_000,
    })
    .toBeGreaterThan(0);
  await b.api("/api/tasks/update", {
    id: task,
    status: "waiting_user",
    ask: "要不要上線？",
    by: { name: "code" },
  });
  const res = await nudge(b, session, task);
  expect(res.nudge.surface).toBe("toast");
  await expect(page.getByTestId("nudge-toast")).toBeVisible({ timeout: 3_000 });
  await b.api("/api/tasks/update", { id: task, status: "doing", by: { name: "code" } });
  await expect(page.getByTestId("nudge-toast")).toHaveCount(0, { timeout: 3_000 });
  await b.closeAllWork();
});

// 看板服務（A，頁面自己這台）連不上：頁首一條淡色橫條「現在連不上，顯示的是剛才的內容」，連回來自動消失
// （DEV-156 U-21）。只看即時更新的串流（本來就在重連），不另外輪詢；正常時一直沒有這條。
test("TestBoardDownShowsOfflineBanner", async ({ page, homes }) => {
  test.setTimeout(60_000);
  await page.goto("/");
  const banner = page.getByTestId("offline-banner");
  await expect
    .poll(() => fromBoard(homes.a.proxy, homes.a.url, "/api/notify/watch").length, {
      timeout: 20_000,
    })
    .toBeGreaterThan(0);
  // 正常時沒有（連著好幾秒都不會閃出來）。
  for (let i = 0; i < 6; i++) {
    await expect(banner).toHaveCount(0);
    await page.waitForTimeout(500);
  }
  homes.a.proxy.mode = "bad-gateway";
  await expect(banner).toBeVisible({ timeout: 15_000 });
  await expect(banner).toHaveText("現在連不上，顯示的是剛才的內容");
  await expect(banner).toHaveAttribute("role", "status");
  homes.a.proxy.mode = "up";
  await expect(banner).toHaveCount(0, { timeout: 20_000 });
});
