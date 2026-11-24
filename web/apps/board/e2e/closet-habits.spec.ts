import { copyFileSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { APIRequestContext, Page } from "@playwright/test";
import { expect, type HgServer, freshHomeTest as test } from "./fixtures";

// 衣櫃與習慣連起來的第二輪頁面（DEV-157）。用沒人用過的新家（freshHomeTest，自己一個 worker），測試照順序共用這個家：
//   1. 一套衣服都沒有時的說明（CLO-25）——要趁家裡還沒有衣服先跑；
//   2. 新家約好第一個習慣 → 起手禮 1 點（HAB-20）→ 展示間兌換一套、真的換到；
//   3. 展示間的點數處連到習慣頁、「再 N 點可以換下一套」（CLO-24）；習慣套件關掉就不連；
//   4. 衣櫃套件關掉的家，賺到點的通知連回習慣頁（HAB-9）。
// 圖是 e2e/fixtures/closet/ 的測試圖樣，不是真的角色。

const here = path.dirname(fileURLToPath(import.meta.url));
const IMG = ["outfit-a.png", "outfit-b.png", "outfit-c.png"].map((n) =>
  path.join(here, "fixtures", "closet", n),
);
const rand = () =>
  Math.random()
    .toString(36)
    .slice(2, 8)
    .replace(/[^a-z0-9]/g, "x");
const pad = (n: number) => String(n).padStart(2, "0");
const dayOf = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

interface Rec {
  id: string;
  fields: Record<string, unknown>;
}

function client(request: APIRequestContext, base: string, server: HgServer) {
  const post = async (route: string, data: unknown) => {
    const res = await request.post(`${base}/api/${route}`, { data });
    const json = (await res.json().catch(() => ({}))) as Record<string, unknown>;
    expect(res.status(), `${route} ${JSON.stringify(json)}`).toBeLessThan(300);
    return json;
  };
  const get = async (kind: string, id: string): Promise<Rec | null> => {
    const res = await request.post(`${base}/api/vault/get`, { data: { kind, id } });
    return res.ok() ? ((await res.json()) as { record: Rec }).record : null;
  };
  const add = async (kind: string, fields: Record<string, unknown>) => {
    const id = String((await post("vault/next_id", { kind })).id);
    await post("vault/put", { record: { kind, id, fields } });
    return id;
  };
  const api = {
    post,
    get,
    add,
    async points(): Promise<number | undefined> {
      const s = await get("closet.state", "main");
      return typeof s?.fields.points === "number" ? s.fields.points : undefined;
    },
    /** builder 的一套官方衣服（圖先放進家），回 closet.outfit 的編號。 */
    async outfit(id: string, idx: number, cost: number): Promise<string> {
      copyFileSync(
        IMG[idx] as string,
        path.join(server.home, "personas", "avatars", `builder@${id}.png`),
      );
      await post("vault/put", {
        record: {
          kind: "closet.outfit",
          id: `builder-${id}`,
          fields: {
            persona: "builder",
            outfit: id,
            name: `衣服${id.toUpperCase()}`,
            cost,
            state: "active",
            source: "official",
          },
        },
      });
      await expect
        .poll(
          async () =>
            (
              ((await get("closet.person", "builder"))?.fields.outfits as { record: string }[]) ??
              []
            ).some((o) => o.record === `builder-${id}`),
          { timeout: 30_000 },
        )
        .toBe(true);
      return `builder-${id}`;
    },
    async setEnabled(id: string, enabled: boolean) {
      await post("package/set_enabled", { id, enabled });
      await expect
        .poll(
          async () =>
            ((await post("package/get", { id })) as { package: { state: string } }).package.state,
          { timeout: 30_000 },
        )
        .toBe(enabled ? "running" : "stopped");
    },
    async notifications(): Promise<Notice[]> {
      return ((await post("notify/list", {})) as { notifications?: Notice[] }).notifications ?? [];
    },
  };
  return api;
}

/** 通知裡這幾個測試要看的欄位；link 是訊息上的站內按鈕（NTF-66、DEV-157）。 */
interface Notice {
  id?: string;
  title?: string;
  context?: string;
  url?: string;
  link?: { label?: string; url?: string };
}

const SHOWROOM = "/p/closet/showroom?persona=builder";
const tile = (page: Page, record: string) =>
  page.locator(`[data-testid="outfit-tile"][data-outfit="${record}"]`);

/** 把 personas.pack_status 換成指定的回應（只換這一頁看到的；服務照舊）。 */
async function fakePackStatus(page: Page, body: Record<string, unknown>) {
  await page.unroute("**/api/personas/pack_status");
  await page.route("**/api/personas/pack_status", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) }),
  );
}

// Guards: CLO-25
test("TestEmptyClosetExplainsPackState", async ({ page }) => {
  const empty = page.getByTestId("closet-empty");
  // 一、這一版沒有官方衣服包（測試家沒有角色包的指標，這是服務真的回的狀態）。
  await page.goto("/p/closet/showroom");
  await expect(empty).toHaveAttribute("data-kind", "none", { timeout: 30_000 });
  // 白話說「衣服包還沒裝」，給「到聊天請秘書幫忙」：開聊天清單，不替主人送話（DEV-156 U-09）。
  await expect(empty).toContainText("衣服包還沒裝");
  await expect(empty.getByTestId("closet-empty-ask")).toHaveAttribute("href", "/chat");
  await expect(page.getByTestId("closet-pack-retry")).toHaveCount(0);
  // 自己上傳的入口照樣在。
  await expect(empty.getByTestId("closet-empty-upload")).toHaveAttribute("href", "/personas?new=1");
  // 衣櫃頁同一套說明。
  await page.goto("/p/closet/wardrobe");
  await expect(empty).toHaveAttribute("data-kind", "none", { timeout: 30_000 });

  // 二、上次沒成功：說原因、會自動再試，另有「現在再試」——按了真的請服務再同步一次，不帶指標、不強制。
  await fakePackStatus(page, {
    source: false,
    latest: "2026.11.1",
    pending: { version: "2026.11.1", reason: "下載不到角色包（沒有網路）" },
    last: {
      status: "failed",
      reason: "下載不到角色包（沒有網路）",
      message: "角色包沒有同步成功：下載不到角色包（沒有網路）（什麼都沒換；有網路時會自動再試）",
      installed: 0,
      unchanged: 0,
    },
  });
  await page.goto("/p/closet/showroom");
  await expect(empty).toHaveAttribute("data-kind", "pending", { timeout: 30_000 });
  await expect(page.getByTestId("closet-empty-hint")).toHaveText(
    "上次沒成功：下載不到角色包（沒有網路）。會自動再試。",
  );
  const sent = page.waitForRequest("**/api/personas/pack_sync");
  await page.getByTestId("closet-pack-retry").click();
  expect((await sent).postDataJSON()).toEqual({});
  // 服務回的那一行照實顯示（這個家沒有角色包，回的是它自己的說明；重點是有回、不是錯誤）。
  await expect(page.getByTestId("closet-pack-result")).toHaveAttribute("role", "status");
  await expect(page.getByTestId("closet-pack-result")).not.toHaveText("");
  await expect(page.getByTestId("closet-pack-retry")).toBeEnabled();
  // 正在下載（還沒同步過、沒有失敗）：不說「上次沒成功」；一般家看不到「官方」字樣（CLO-37）。
  await fakePackStatus(page, { source: false, latest: "2026.11.1" });
  await page.goto("/p/closet/showroom");
  await expect(page.getByTestId("closet-empty-hint")).toHaveText(
    "正在下載衣服，好了這裡就會出現。會自動再試。",
  );
  await expect(page.getByTestId("closet-pack-retry")).toBeVisible();

  // 三、已經同步好了，但這位沒有衣服。
  await fakePackStatus(page, {
    source: false,
    latest: "2026.11.1",
    registered: "2026.11.1",
    last: { status: "synced", message: "角色包 2026.11.1 已經同步", installed: 3, unchanged: 0 },
  });
  await page.goto("/p/closet/showroom?persona=builder");
  await expect(empty).toHaveAttribute("data-kind", "synced", { timeout: 30_000 });
  await expect(empty).toContainText("這位角色還沒有衣服");
  await expect(page.getByTestId("closet-pack-retry")).toHaveCount(0);
  await expect(empty.getByTestId("closet-empty-upload")).toHaveAttribute(
    "href",
    "/personas/builder?tab=closet-mine.outfits",
  );
});

// Guards: HAB-20, CLO-N4
test("TestFirstHabitGiftRedeemsOutfit @smoke", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  // 新家：還沒有任何點數。
  expect((await api.points()) ?? 0).toBe(0);
  const id = rand();
  const record = await api.outfit(id, 0, 1);
  // 約好第一個習慣（秘書在聊天裡約好時寫的就是這一筆）。
  await api.add("habits.habit", {
    name: "每天散步",
    agent: "code",
    cadence: "weekly",
    target: 3,
    mode: "cumulative",
    start: dayOf(new Date()),
    status: "active",
  });
  // 起手禮：錢包記送過、點數換得到衣服（outfits_ready，HAB-23）、衣櫃看到 1 點；
  // 通知說可以換一套、帶「到展示間」的按鈕（DEV-157）。
  await expect
    .poll(async () => (await api.get("habits.wallet", "main"))?.fields.starter, { timeout: 30_000 })
    .toBe("given");
  expect((await api.get("habits.wallet", "main"))?.fields.outfits_ready).toBe(true);
  await expect.poll(() => api.points(), { timeout: 30_000 }).toBe(1);
  let gift: Notice | undefined;
  await expect
    .poll(
      async () => {
        gift = (await api.notifications()).find((n) => n.title === "送你 1 點");
        return gift?.link?.url;
      },
      { timeout: 30_000 },
    )
    .toBe("/p/closet/showroom");
  expect(gift?.context ?? "").toContain("可以到展示間換一套");
  // 訊息頁畫出按鈕，點下去到展示間（法律第 5 條「點下去到哪」）。
  await page.goto(`/messages/${gift?.id}`);
  const button = page.getByTestId("message-link-button");
  await expect(button).toHaveText("到展示間", { timeout: 30_000 });
  await button.click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/p/closet/showroom");

  // 展示間：1 點、可以換 Builder 的一套（正在看這位，照這位的衣服算，DEV-157）；那套還是剪影。
  await page.goto(SHOWROOM);
  await expect(page.getByTestId("closet-points-value")).toContainText("1", { timeout: 30_000 });
  await expect(page.getByTestId("closet-next-hint")).toHaveText("可以換Builder的一套了");
  await expect(tile(page, record)).toHaveAttribute("data-owned", "false");
  await tile(page, record).click();
  await expect(page).toHaveURL(new RegExp(`/p/closet/outfit\\?id=${record}`));
  await page.getByTestId("outfit-unlock").click();
  await expect(page.getByTestId("outfit-unlock-confirm")).toContainText("確定用 1 點兌換嗎？");
  await page.getByTestId("outfit-unlock-yes").click();
  await expect(page.getByTestId("outfit-result")).toHaveText("兌換了", { timeout: 30_000 });
  // 真的換到：真圖拿得到、內容就是那張圖；點數用掉。
  await expect(page.getByTestId("outfit-big")).toHaveAttribute(
    "src",
    new RegExp(`/pkg-files/closet/o/builder/${id}/1\\.png`),
  );
  const real = await request.get(`${baseURL}/pkg-files/closet/o/builder/${id}/1.png`);
  expect(real.status()).toBe(200);
  expect((await real.body()).equals(readFileSync(IMG[0] as string))).toBe(true);
  await expect.poll(() => api.points(), { timeout: 30_000 }).toBe(0);
  // 進衣櫃了。
  await page.goto("/p/closet/wardrobe?persona=builder");
  await expect(tile(page, record)).toHaveAttribute("data-owned", "true", { timeout: 30_000 });
});

// Guards: CLO-24
test("TestShowroomLinksHabitsAndShowsNextOutfit", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  // 接上一個測試：0 點、唯一一套已經兌換 → 沒有可換的衣服，不顯示那一句。
  await page.goto(SHOWROOM);
  await expect(page.getByTestId("closet-points-value")).toContainText("0", { timeout: 30_000 });
  await expect(page.getByTestId("closet-next-hint")).toHaveCount(0);
  // 多一套要 2 點的：還差 2 點。
  await api.outfit(rand(), 1, 2);
  await expect(page.getByTestId("closet-next-hint")).toHaveText("再 2 點可以換Builder的下一套", {
    timeout: 30_000,
  });
  // 點數處連到習慣頁（點數在那裡賺）。
  const link = page.getByTestId("closet-to-habits");
  await expect(link).toBeVisible();
  const box = await link.boundingBox();
  expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
  await link.click();
  await expect(page).toHaveURL(/\/p\/habits\/list/);
  await expect(page.getByTestId("habits-wallet")).toBeVisible({ timeout: 30_000 });
  // 習慣套件關掉：不連（那一句照樣在，點數是衣櫃自己算好的）。
  try {
    await api.setEnabled("habits", false);
    await page.goto(SHOWROOM);
    await expect(page.getByTestId("closet-points")).toBeVisible({ timeout: 30_000 });
    await expect(page.getByTestId("showroom")).toBeVisible();
    await expect(page.getByTestId("closet-to-habits")).toHaveCount(0);
  } finally {
    await api.setEnabled("habits", true);
  }
});

// Guards: HAB-9
test("TestPointNotificationLinksHabitsWhenClosetOff", async ({
  page,
  request,
  baseURL,
  hgServer,
}) => {
  const api = client(request, baseURL ?? "", hgServer);
  try {
    await api.setEnabled("closet", false);
    // 一個每週 1 次的習慣、補兩週的打卡 → 賺到 1 點。
    const monday = new Date();
    monday.setDate(monday.getDate() - ((monday.getDay() + 6) % 7) - 14);
    const day = (n: number) => {
      const d = new Date(monday);
      d.setDate(d.getDate() + n);
      return dayOf(d);
    };
    const name = `關衣櫃的習慣${rand()}`;
    const habit = await api.add("habits.habit", {
      name,
      agent: "code",
      cadence: "weekly",
      target: 1,
      mode: "cumulative",
      start: day(0),
      status: "active",
    });
    for (const d of [day(0), day(7)]) {
      await api.add("habits.checkin", {
        habit,
        date: d,
        source: "manual",
        key: `manual:${habit}:${d}:`,
      });
    }
    let found: Notice | undefined;
    await expect
      .poll(
        async () => {
          found = (await api.notifications()).find((n) => (n.context ?? "").includes(name));
          return found?.link?.url;
        },
        { timeout: 30_000 },
      )
      .toBe("/p/habits/list");
    // 內文不提「換衣服」（這個家沒有展示間）。
    expect(found?.context ?? "").not.toContain("換衣服");
    // 訊息頁的按鈕「到習慣頁」點得到東西（DEV-157）。
    await page.goto(`/messages/${found?.id}`);
    const button = page.getByTestId("message-link-button");
    await expect(button).toHaveText("到習慣頁", { timeout: 30_000 });
    await button.click();
    await expect(page.getByTestId("habits-wallet")).toBeVisible({ timeout: 30_000 });
  } finally {
    await api.setEnabled("closet", true);
  }
});
