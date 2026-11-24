import { copyFileSync, readdirSync, readFileSync, rmSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { APIRequestContext, Page } from "@playwright/test";
import { expect, type HgServer, test } from "./fixtures";

// 官方套件 closet 的頁面（P6-4b；DEV-112 起分兩頁：衣櫃只放擁有的、展示間看全部與兌換；設計中的在開發家角色頁的「修改」分頁，closet-edit.spec）：資料經看板的 vault 代理寫進去；person、state 是套件的程式（bin/evergenie-closet，核心啟動）
// 算好寫進 vault 的，所以改了資料之後要等幾秒才會更新；點數來自習慣套件（habits）。
// 同一個 worker 的家是共用的：每個測試用自己的衣服編號（vault 的編號封存後不能再用），開頭與結尾把自己造的資料收掉。
// 圖放在家的 personas/avatars/builder@<衣服>.png（builder 是 code 秘書的人設），素材在 e2e/fixtures/closet/（三張圖樣不同的 256×256）。
// DEV-143 起兩頁上面一排角色頭像、一次看一位，所以網址都帶 ?persona=builder。

const here = path.dirname(fileURLToPath(import.meta.url));
const fx = path.join(here, "fixtures");
const IMG = ["outfit-a.png", "outfit-b.png", "outfit-c.png"].map((n) => path.join(fx, "closet", n));
const rand = () =>
  Math.random()
    .toString(36)
    .slice(2, 8)
    .replace(/[^a-z0-9]/g, "x");

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
  const list = async (kind: string) =>
    ((await post("vault/list", { kind, include_archived: false })) as { records?: Rec[] })
      .records ?? [];
  const get = async (kind: string, id: string): Promise<Rec | null> => {
    const res = await request.post(`${base}/api/vault/get`, { data: { kind, id } });
    return res.ok() ? ((await res.json()) as { record: Rec }).record : null;
  };
  const avatars = path.join(server.home, "personas", "avatars");
  const created: string[] = []; // 這個測試造的衣服編號
  const api = {
    list,
    get,
    /** 把圖放進家（要在加資料之前），idx 指 fixtures 的第幾張；withVideo 另放 persona.mp4 當動態。 */
    async outfit(
      id: string,
      idx: number,
      over: Record<string, unknown> = {},
      withVideo = false,
      persona = "builder",
    ): Promise<string> {
      copyFileSync(IMG[idx] as string, path.join(avatars, `${persona}@${id}.png`));
      if (withVideo)
        copyFileSync(path.join(fx, "persona.mp4"), path.join(avatars, `${persona}@${id}.mp4`));
      const record = `${persona}-${id}`;
      await post("vault/put", {
        record: {
          kind: "closet.outfit",
          id: record,
          fields: {
            persona,
            outfit: id,
            name: `衣服${id.toUpperCase()}`,
            cost: 0,
            state: "active",
            source: "official",
            ...over,
          },
        },
      });
      created.push(id);
      return record;
    },
    /** 收掉這個測試造的東西：衣服封存、輪播清掉、圖刪掉，等套件的程式把公開資料夾與 person 清乾淨。 */
    async cleanup() {
      for (const r of await list("closet.outfit")) {
        await post("vault/archive", { kind: "closet.outfit", id: r.id });
      }
      for (const r of await list("closet.note")) {
        await post("vault/archive", { kind: "closet.note", id: r.id });
      }
      for (const r of await list("closet.request")) {
        if (r.fields.state === "pending")
          await post("vault/archive", { kind: "closet.request", id: r.id });
      }
      if (await get("closet.rotation", "builder")) {
        await post("vault/patch", {
          kind: "closet.rotation",
          id: "builder",
          set: { outfits: [], wear: "", wear_until: "" },
        });
      }
      for (const f of readdirSync(avatars)) {
        if (f.startsWith("builder@") || f.startsWith("helper@"))
          rmSync(path.join(avatars, f), { force: true });
      }
      await expect
        .poll(
          async () => {
            const p = await get("closet.person", "builder");
            return p ? ((p.fields.outfits as unknown[]) ?? []).length : 0;
          },
          { timeout: 20_000 },
        )
        .toBe(0);
    },
    async points(): Promise<number> {
      const s = await get("closet.state", "main");
      return typeof s?.fields.points === "number" ? s.fields.points : 0;
    },
    /**
     * 約一個習慣、補兩週的打卡 → 習慣套件算出 1 點，再等衣櫃的 state 至少多 1 點；做完把習慣收掉（賺到的點數還在）。
     * 家裡第一個習慣另外有起手禮 1 點（HAB-20），所以這個家第一次跑會多 2 點，用「至少」。
     */
    async seedPoint() {
      const before = await api.points();
      const monday = new Date();
      monday.setDate(monday.getDate() - ((monday.getDay() + 6) % 7) - 14);
      const day = (n: number) => {
        const d = new Date(monday);
        d.setDate(d.getDate() + n);
        return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
      };
      const habit = String((await post("vault/next_id", { kind: "habits.habit" })).id);
      await post("vault/put", {
        record: {
          kind: "habits.habit",
          id: habit,
          fields: {
            name: "衣櫃測試用的習慣",
            agent: "code",
            cadence: "weekly",
            target: 1,
            mode: "cumulative",
            start: day(0),
            status: "active",
          },
        },
      });
      const checkins: string[] = [];
      for (const d of [day(0), day(7)]) {
        const cid = String((await post("vault/next_id", { kind: "habits.checkin" })).id);
        await post("vault/put", {
          record: {
            kind: "habits.checkin",
            id: cid,
            fields: { habit, date: d, source: "manual", key: `manual:${habit}:${d}:` },
          },
        });
        checkins.push(cid);
      }
      await expect
        .poll(async () => (await get("habits.status", habit.toLowerCase()))?.fields.earned_points, {
          timeout: 30_000,
        })
        .toBe(1);
      await expect.poll(() => api.points(), { timeout: 30_000 }).toBeGreaterThanOrEqual(before + 1);
      for (const c of checkins) await post("vault/archive", { kind: "habits.checkin", id: c });
      await post("vault/archive", { kind: "habits.habit", id: habit });
    },
    async request(persona: string, outfit: string) {
      const id = String((await post("vault/next_id", { kind: "closet.request" })).id);
      await post("vault/put", {
        record: {
          kind: "closet.request",
          id,
          fields: { type: "unlock", persona, outfit, state: "pending" },
        },
      });
      return id;
    },
  };
  return api;
}

const WARDROBE = "/p/closet/wardrobe?persona=builder";
const SHOWROOM = "/p/closet/showroom?persona=builder";

const tile = (page: Page, record: string) =>
  page.locator(`[data-testid="outfit-tile"][data-outfit="${record}"]`);

// Guards: REQ-125
test("TestLockedOutfitShowsSilhouetteOnly @smoke", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  const free = rand();
  const locked = rand();
  try {
    await api.outfit(free, 0);
    const lockedRecord = await api.outfit(locked, 1, { cost: 2 });
    // 整個頁面載入過程中，不能有任何請求打到沒兌換那套的真圖路徑。
    const seen: string[] = [];
    page.on("request", (r) => seen.push(new URL(r.url()).pathname));

    // 衣櫃只放擁有的（CLO-14）：免費那套在，沒兌換那套不在。
    await page.goto(WARDROBE);
    await expect(page.getByTestId("closet")).toBeVisible();
    await expect(tile(page, `builder-${free}`)).toBeVisible();
    await expect(tile(page, lockedRecord)).toHaveCount(0);

    // 展示間看全部：沒兌換那套是剪影加鎖頭。
    await page.goto(SHOWROOM);
    await expect(page.getByTestId("showroom")).toBeVisible();
    // 正式家沒有篩選那一排（升成官方只在開發家，CLO-29）；等衣服畫出來再看，免得只是還沒載入。
    await expect(tile(page, lockedRecord)).toBeVisible();
    await expect(page.getByTestId("showroom-filter")).toHaveCount(0);
    const t = tile(page, lockedRecord);
    await expect(t).toBeVisible();
    const img = t.getByTestId("outfit-img");
    await expect(img).toHaveAttribute("src", new RegExp(`^/pkg-files/closet/s/builder/${locked}/`));
    await expect(t.getByTestId("lock-badge")).toBeVisible();
    await expect(t.getByTestId("outfit-tags")).toContainText("2 點");
    // 圖真的載入了（剪影），而且免費那套是真圖。
    await expect
      .poll(() => img.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth > 0))
      .toBe(true);
    const freeImg = tile(page, `builder-${free}`).getByTestId("outfit-img");
    await expect(freeImg).toHaveAttribute(
      "src",
      new RegExp(`/pkg-files/closet/o/builder/${free}/1\\.png`),
    );

    // 詳情頁一樣只有剪影；從展示間點進去，上一頁回展示間（CLO-16）。
    await t.locator("a").first().click();
    await expect(page).toHaveURL(new RegExp(`/p/closet/outfit\\?id=${lockedRecord}$`));
    await expect(page.getByTestId("outfit-name")).toContainText(locked.toUpperCase());
    await expect(page.getByTestId("outfit-big")).toHaveAttribute("src", /\/pkg-files\/closet\/s\//);
    await expect(page.getByTestId("outfit-hero")).toHaveAttribute("data-owned", "false");

    await page.getByRole("link", { name: "上一頁" }).click();
    await expect(page.getByTestId("showroom")).toBeVisible();
    expect(seen.filter((p) => p.startsWith(`/pkg-files/closet/o/builder/${locked}/`))).toEqual([]);
    // 直接要真圖：404。
    const direct = await request.get(`${baseURL}/pkg-files/closet/o/builder/${locked}/1.png`);
    expect(direct.status()).toBe(404);
    const sil = await request.get(`${baseURL}/pkg-files/closet/s/builder/${locked}/1.png`);
    expect(sil.status()).toBe(200);
    expect((await sil.body()).equals(readFileSync(IMG[1] as string))).toBe(false);
  } finally {
    await api.cleanup();
  }
});

// Guards: REQ-125
test("TestUnlockSpendsPointAndRevealsImage", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  const locked = rand();
  try {
    const record = await api.outfit(locked, 2, { cost: 1 });
    await api.seedPoint();
    const before = await api.points();
    expect(before).toBeGreaterThanOrEqual(1);

    // 選單有「衣櫃」與「展示間」。
    const menu = JSON.stringify(
      await (await request.post(`${baseURL}/api/web/get_menu`, { data: {} })).json(),
    );
    expect(menu).toContain("衣櫃");
    expect(menu).toContain("展示間");

    await page.goto(SHOWROOM);
    await expect(page.getByTestId("closet-points-value")).toContainText(String(before));
    await page.goto(`/p/closet/outfit?id=${record}`);
    await expect(page.getByTestId("outfit-big")).toHaveAttribute("src", /\/pkg-files\/closet\/s\//);
    // 按一次展開確認，不直接送。
    await page.getByTestId("outfit-unlock").click();
    await expect(page.getByTestId("outfit-unlock-confirm")).toContainText("確定用 1 點兌換嗎？");
    expect((await api.list("closet.request")).length).toBe(0);
    await page.getByTestId("outfit-unlock-yes").click();
    await expect(page.getByTestId("outfit-result")).toHaveText("兌換了");
    // 圖換成真圖。
    await expect(page.getByTestId("outfit-big")).toHaveAttribute(
      "src",
      new RegExp(`/pkg-files/closet/o/builder/${locked}/1\\.png`),
    );
    await expect(page.getByTestId("outfit-hero")).toHaveAttribute("data-owned", "true");
    const real = await request.get(`${baseURL}/pkg-files/closet/o/builder/${locked}/1.png`);
    expect(real.status()).toBe(200);
    expect((await real.body()).equals(readFileSync(IMG[2] as string))).toBe(true);
    // 兌換紀錄由套件的程式寫（by = closet），點數少 1；習慣錢包的 spent 也跟著變。
    const unlock = await api.get("closet.unlock", record);
    expect(unlock?.fields).toMatchObject({ cost: 1, by: "closet", persona: "builder" });
    await expect.poll(() => api.points()).toBe(before - 1);
    await expect
      .poll(async () => (await api.get("habits.wallet", "main"))?.fields.spent as number)
      .toBeGreaterThanOrEqual(1);
    await page.goto(SHOWROOM);
    await expect(page.getByTestId("closet-points-value")).toContainText(String(before - 1));
    await expect(tile(page, record)).toHaveAttribute("data-owned", "true");
    // 兌換了就進衣櫃。
    await page.goto(WARDROBE);
    await expect(tile(page, record)).toHaveAttribute("data-owned", "true");
  } finally {
    await api.cleanup();
  }
});

// Guards: REQ-125
test("TestUnlockRejectedWhenNoPoints", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  const locked = rand();
  try {
    const points = await api.points();
    const cost = points + 1;
    const record = await api.outfit(locked, 0, { cost });
    await page.goto(`/p/closet/outfit?id=${record}`);
    // 點數明顯不夠：按鈕停用並說明。
    await expect(page.getByTestId("outfit-unlock")).toBeDisabled();
    await expect(page.getByTestId("outfit-unlock-hint")).toContainText(
      `點數不夠：要 ${cost} 點，現在 ${points} 點`,
    );

    // 頁面不擋就不是錯：讓頁面以為點數夠，按下去由程式判斷，顯示程式給的原因。
    await page.route("**/api/vault/get", async (route) => {
      const body = route.request().postDataJSON() as { kind?: string };
      if (body.kind === "closet.state") {
        await route.fulfill({
          json: {
            record: {
              kind: "closet.state",
              id: "main",
              fields: { earned: 99, spent: 0, points: 99, wallet: true },
            },
          },
        });
      } else await route.continue();
    });
    await expect(page.getByTestId("outfit-unlock")).toBeEnabled({ timeout: 15_000 });
    await page.getByTestId("outfit-unlock").click();
    await page.getByTestId("outfit-unlock-yes").click();
    const result = page.getByTestId("outfit-result");
    await expect(result).toHaveAttribute("data-kind", "rejected");
    await expect(result).toContainText(`點數不夠：要 ${cost} 點，現在 ${points} 點`);
    expect(await api.get("closet.unlock", record)).toBeNull();
    const direct = await request.get(`${baseURL}/pkg-files/closet/o/builder/${locked}/1.png`);
    expect(direct.status()).toBe(404);
  } finally {
    await api.cleanup();
  }
});

// Guards: REQ-113, REQ-125
test("TestRotationAndAvatarFollowWorn", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  const a = rand();
  const b = rand();
  try {
    // a 有動態（persona.mp4）、b 只有圖；兩套都免費。
    const ra = await api.outfit(a, 0, { order: 1 }, true);
    const rb = await api.outfit(b, 1, { order: 2 });
    await page.goto(WARDROBE);
    const section = page.locator('[data-testid="person-section"][data-persona="builder"]');
    await expect(tile(page, ra)).toHaveAttribute("data-owned", "true");
    // 預設只輪第一套（a）：現在穿 a。
    await expect(section.getByTestId("worn-name")).toContainText(`衣服${a.toUpperCase()}`);
    const avatar = async () => (await request.get(`${baseURL}/avatars/code.png`)).body();
    await expect
      .poll(async () => (await avatar()).equals(readFileSync(IMG[0] as string)))
      .toBe(true);
    const videoStatus = async () => (await request.get(`${baseURL}/avatars/code.mp4`)).status();
    await expect.poll(videoStatus).toBe(200);

    // 不能取消最後一個勾。
    const toggleA = tile(page, ra).getByTestId("rotate-toggle");
    await expect(toggleA).toHaveAttribute("aria-checked", "true");
    await toggleA.click();
    await expect(section.getByRole("alert")).toContainText("至少要留一套輪流穿");
    await expect(toggleA).toHaveAttribute("aria-checked", "true");

    // 先勾 b 再取消 a：只輪 b。
    await tile(page, rb).getByTestId("rotate-toggle").click();
    await expect(tile(page, rb).getByTestId("rotate-toggle")).toHaveAttribute(
      "aria-checked",
      "true",
    );
    await toggleA.click();
    await expect(toggleA).toHaveAttribute("aria-checked", "false");
    expect((await api.get("closet.rotation", "builder"))?.fields.outfits).toEqual([b]);
    await expect(section.getByTestId("worn-name")).toContainText(`衣服${b.toUpperCase()}`);

    // 頭像換成 b 的圖；b 沒有動態，所以影片 404（不退回人設預設影片），角色畫面只剩圖。
    await expect
      .poll(async () => (await avatar()).equals(readFileSync(IMG[1] as string)))
      .toBe(true);
    await expect.poll(videoStatus).toBe(404);
    await page.goto("/chat/code");
    await expect(page.locator("[data-persona-media]").first()).toHaveAttribute(
      "data-persona-media",
      "image",
    );
    await expect(page.locator("[data-persona-media] video")).toHaveCount(0);
    const heroBytes = await page
      .locator('[data-persona-media] img[src="/avatars/code.png"]')
      .first()
      .evaluate(async (el: HTMLImageElement) => {
        const res = await fetch(el.src, { cache: "reload" });
        return (await res.arrayBuffer()).byteLength;
      });
    expect(heroBytes).toBe(readFileSync(IMG[1] as string).byteLength);

    // 改回 a：角色畫面有影片，src 是 /avatars/code.mp4（影片本身就是 persona.mp4）。
    await page.goto(WARDROBE);
    await tile(page, ra).getByTestId("rotate-toggle").click();
    await expect(toggleA).toHaveAttribute("aria-checked", "true");
    await tile(page, rb).getByTestId("rotate-toggle").click();
    await expect(section.getByTestId("worn-name")).toContainText(`衣服${a.toUpperCase()}`);
    await expect.poll(videoStatus).toBe(200);
    await page.goto("/chat/code");
    await expect(page.locator("[data-persona-media] video").first()).toHaveAttribute(
      "src",
      "/avatars/code.mp4",
    );
  } finally {
    await api.cleanup();
  }
});

// 一般家（沒有 edit 能力，只有開發家才有）沒有開發用的東西（CLO-37、CLO-23）：展示間沒有「設計中」（採用、留言、修改都沒有），
// 只有設計中衣服的角色不列；衣櫃與展示間沒有評論、生圖秘書、官方／我的這些字（「我的點數」是主人自己的點數，不是來源標籤）；
// 角色頁沒有「修改」分頁（核心照 show_when 不列；直接打網址的那半在 closet-edit.spec 的 TestEditTabHiddenWithoutEditCapability）。
// 直接打設計中那套的詳情網址：當找不到，不顯示「設計中」（CLO-38）。
// 設計中的採用與評論都在開發家角色頁的「修改」分頁，e2e 在 closet-edit.spec。一般家也沒有上架／下架這些字（作者 11-15）。
test("TestOrdinaryHomeHidesDevOnlyThings", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  const NO_WORDS = [
    /設計中/,
    /留言/,
    /評論/,
    /生圖秘書/,
    /官方/,
    /我的(?!點數)/,
    /還沒上架/,
    /上架/,
    /下架/,
  ];
  const noDevWords = async (root: string) => {
    const text = await page.getByTestId(root).innerText();
    for (const w of NO_WORDS) expect(text, `${root} 不該有 ${w}`).not.toMatch(w);
  };
  try {
    const base = await api.outfit(rand(), 0); // 一套上架的免費衣服（在衣櫃裡）
    const draft = await api.outfit(rand(), 1, {
      state: "draft",
      cost: 1,
      desc: "還在設計的新衣服",
    });
    // 下架又擁有的（免費的那套被收起來）：一般家照常列、不標「下架」（一般家沒有上架／下架這些字）。
    const retired = await api.outfit(rand(), 2, { state: "retired" });
    // 另一位角色只有設計中的衣服：一般家的展示間不列她（不然是空的一段）。
    const helperDraft = await api.outfit(rand(), 2, { state: "draft" }, false, "helper");
    // 等程式把三套都算進 closet.person（之後「沒有」的檢查才算數）。
    const listed = async (p: string) =>
      (((await api.get("closet.person", p))?.fields.outfits as { record?: string }[]) ?? []).map(
        (o) => o.record,
      );
    await expect
      .poll(async () => [...(await listed("builder")), ...(await listed("helper"))], {
        timeout: 30_000,
      })
      .toEqual(expect.arrayContaining([base, draft, retired, helperDraft]));

    await page.goto(SHOWROOM);
    const showroom = page.getByTestId("showroom");
    // 能力讀到了、這個家不能修改衣服，才算數。
    await expect(showroom).toHaveAttribute("data-edit", "false", { timeout: 20_000 });
    await expect(tile(page, base)).toBeVisible({ timeout: 20_000 });
    await expect(tile(page, draft)).toHaveCount(0);
    await expect(showroom).toContainText("我的點數");
    await noDevWords("showroom");
    await expect(page.getByTestId("showroom-to-edit")).toHaveCount(0);
    await expect(page.getByTestId("showroom-filter")).toHaveCount(0);
    await expect(page.getByRole("button", { name: /設計中/ })).toHaveCount(0);
    // 只有設計中衣服的 helper 不在頭像列；網址指定看她也不會畫出她那套設計中的。
    await expect(page.locator('[data-testid="person-pick"][data-persona="helper"]')).toHaveCount(0);
    await page.goto("/p/closet/showroom?persona=helper");
    await expect(page.getByTestId("person-section")).toBeVisible({ timeout: 20_000 });
    await expect(page.getByTestId("person-section")).not.toHaveAttribute("data-persona", "helper");
    await expect(tile(page, helperDraft)).toHaveCount(0);
    await noDevWords("showroom");

    await page.goto(WARDROBE);
    await expect(tile(page, base)).toBeVisible({ timeout: 20_000 });
    await expect(tile(page, retired)).toBeVisible();
    await expect(tile(page, draft)).toHaveCount(0);
    await noDevWords("closet");
    await page.goto(`/p/closet/outfit?id=${retired}`);
    await expect(page.getByTestId("outfit-name")).toBeVisible({ timeout: 20_000 });
    await noDevWords("closet-outfit");

    // 設計中那套的詳情網址：找不到（上架的那套照常打得開）。
    await page.goto(`/p/closet/outfit?id=${draft}`);
    await expect(page.getByTestId("closet-outfit")).toContainText("找不到這套衣服", {
      timeout: 20_000,
    });
    await noDevWords("closet-outfit");
    await page.goto(`/p/closet/outfit?id=${base}`);
    await expect(page.getByTestId("closet-outfit")).not.toContainText("找不到這套衣服", {
      timeout: 20_000,
    });
    await expect(page.getByTestId("closet-outfit")).toContainText(
      `衣服${base.slice("builder-".length).toUpperCase()}`,
    );

    // 角色頁沒有「修改」分頁。
    await page.goto("/personas/builder");
    const tabs = page.getByTestId("page-tabs").getByRole("link");
    await expect(tabs.filter({ hasText: "衣服" })).toBeVisible({ timeout: 15_000 });
    await expect(tabs.filter({ hasText: "修改" })).toHaveCount(0);
  } finally {
    await api.cleanup();
  }
});

// 一般家沒有生圖秘書（作者 2026-11-15，DEV-160；ADM-39）：e2e 的家是一般家（開發工坊 admin 沒開），
// 設定精靈與「職位與秘書」頁（連 ?claim=art 的連結也一樣）都沒有生圖 art，也沒有「選用的職位」那一組。
// 開發家（開發工坊開著）看得到 art、標「選用」的那一半在 admin.spec.ts；衣櫃頁面也沒有生圖秘書的字（TestOrdinaryHomeHidesDevOnlyThings）。
// Guards: ADM-39, WEB-47
test("TestNoArtOnOrdinaryHome", async ({ page }) => {
  await page.goto("/settings/setup");
  const step = page.getByTestId("setup-step-agents");
  // 先等認領卡出來（套件清單讀到了），再看裡面沒有 art；哪個職位不寫死，同一個 worker 的別的測試可能已經認領過某些。
  await expect(step.getByTestId("function-unclaimed").first()).toBeVisible();
  await expect(step.locator('[data-agent="art"]')).toHaveCount(0);
  await expect(step.getByTestId("functions-optional")).toHaveCount(0);
  await expect(step).not.toContainText("生圖");

  await page.goto("/settings/agents?claim=art");
  await expect(page.getByTestId("function-unclaimed").first()).toBeVisible();
  await expect(page.locator('[data-testid="function-unclaimed"][data-agent="art"]')).toHaveCount(0);
  await expect(page.getByTestId("functions-optional")).toHaveCount(0);
  await expect(page.getByTestId("function-optional-badge")).toHaveCount(0);
});

// 一次看一位角色（DEV-143）：上面一排頭像，點了換人、網址換成 ?persona=；這台瀏覽器記得上次看誰（衣櫃與展示間共用）；
// 換人用 replaceState 帶著原本的 history.state，上一頁照樣回前一頁（FEN-29）。
test("TestPickerShowsOnePersonaAndRemembers", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  const a = rand();
  const h = rand();
  try {
    const ra = await api.outfit(a, 0);
    const rh = await api.outfit(h, 1, {}, false, "helper");
    await page.goto(WARDROBE);
    const picker = page.getByTestId("person-picker");
    const pick = (persona: string) =>
      picker.locator(`[data-testid="person-pick"][data-persona="${persona}"]`);
    await expect(tile(page, ra)).toBeVisible({ timeout: 30_000 });
    await expect(pick("builder")).toHaveAttribute("aria-pressed", "true");
    await expect(tile(page, rh)).toHaveCount(0);

    await expect(pick("helper")).toBeVisible({ timeout: 30_000 });
    await pick("helper").click();
    await expect(page).toHaveURL(/persona=helper/);
    await expect(tile(page, rh)).toBeVisible();
    await expect(tile(page, ra)).toHaveCount(0);

    // 網址沒指定：照上次看的（衣櫃與展示間共用）。
    await page.goto("/p/closet/wardrobe");
    await expect(pick("helper")).toHaveAttribute("aria-pressed", "true");
    // 站內點到展示間（不整頁重載，歷史接得上）。
    await page.getByTestId("closet-to-showroom").click();
    await expect(page.getByTestId("showroom")).toBeVisible();
    await expect(pick("helper")).toHaveAttribute("aria-pressed", "true");
    await expect(tile(page, rh)).toBeVisible();

    // 換人之後上一頁回前一頁（衣櫃）。
    await pick("builder").click();
    await expect(page).toHaveURL(/\/p\/closet\/showroom\?persona=builder$/);
    await page.getByRole("link", { name: "上一頁" }).click();
    await expect(page).toHaveURL(/\/p\/closet\/wardrobe$/);
  } finally {
    await api.cleanup();
  }
});

// 看大圖（FEN-30，作者 2026-11-14「圖案關閉應該是大家都要有」）：手機大小。衣服詳情頁點大圖在這頁疊一層看，
// 右上角 ✕ 關掉回到同一頁同一個位置；開著時手機的上一頁是關掉大圖、不是離開頁面；用 ✕ 關的歷史不多一筆。
test.describe("看大圖（手機大小）", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("TestOutfitImageOpensViewerAndClosesInPlace", async ({
    page,
    request,
    baseURL,
    hgServer,
  }) => {
    const api = client(request, baseURL ?? "", hgServer);
    const a = rand();
    try {
      // 免費的一套、兩張輪播照。
      copyFileSync(
        IMG[1] as string,
        path.join(hgServer.home, "personas", "avatars", `builder@${a}~2.png`),
      );
      const record = await api.outfit(a, 0);
      await page.goto(SHOWROOM);
      await expect(tile(page, record)).toHaveAttribute("data-owned", "true", { timeout: 30_000 });
      await tile(page, record).getByRole("link").click();
      await expect(page).toHaveURL(new RegExp(`/p/closet/outfit\\?id=${record}$`));
      await expect(page.getByTestId("outfit-thumbs").getByTestId("outfit-thumb")).toHaveCount(2);
      const here = page.url();
      // 捲到下面一點再點圖：關掉後要在同一個位置。
      await page.getByTestId("outfit-name").scrollIntoViewIfNeeded();
      const zoom = page.getByTestId("outfit-zoom");
      const top = async () => (await zoom.boundingBox())?.y;
      const before = await top();
      await zoom.click();
      const viewer = page.getByTestId("media-viewer");
      await expect(viewer.getByTestId("media-viewer-img")).toHaveAttribute(
        "src",
        `/pkg-files/closet/o/builder/${a}/1.png`,
      );
      // 沒有開新分頁；✕ 至少 44px、在畫面右上角裡面。
      expect(page.context().pages()).toHaveLength(1);
      const close = viewer.getByRole("button", { name: "關閉" });
      const box = await close.boundingBox();
      expect(box?.width).toBeGreaterThanOrEqual(44);
      expect(box?.height).toBeGreaterThanOrEqual(44);
      expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(390);
      // 多張可以左右換。
      await viewer.getByRole("button", { name: "下一張" }).click();
      await expect(viewer.getByTestId("media-viewer-img")).toHaveAttribute(
        "src",
        `/pkg-files/closet/o/builder/${a}/2.png`,
      );
      // ✕：關掉，同一頁、同一個位置。
      await close.click();
      await expect(viewer).toHaveCount(0);
      expect(page.url()).toBe(here);
      await expect(page.getByTestId("outfit-name")).toBeVisible();
      await expect.poll(top).toBe(before);
      // 用 ✕ 關的歷史不多一筆：上一頁直接回展示間。
      await page.goBack();
      await expect(page).toHaveURL(/\/p\/closet\/showroom\?persona=builder$/);
      await expect(tile(page, record)).toBeVisible();

      // 開著時按上一頁（手機的返回手勢）：只關掉大圖，還在衣服詳情。
      await tile(page, record).getByRole("link").click();
      await page.getByTestId("outfit-zoom").click();
      await expect(viewer).toBeVisible();
      await page.goBack();
      await expect(viewer).toHaveCount(0);
      expect(page.url()).toBe(here);
      await expect(page.getByTestId("outfit-name")).toBeVisible();
      // 再按一次才回展示間。
      await page.goBack();
      await expect(page).toHaveURL(/\/p\/closet\/showroom\?persona=builder$/);
    } finally {
      await api.cleanup();
    }
  });
});

// 現在穿這套（CLO-30，DEV-156）：兌換 → 現在穿這套 → 復原 → 換回來；在衣櫃再穿一次 → 今天頁的頭像換了。手機大小。
test.describe("現在穿這套（手機大小）", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("TestWearNowAfterUnlockThenUndo", async ({ page, request, baseURL, hgServer }) => {
    const api = client(request, baseURL ?? "", hgServer);
    const a = rand();
    const b = rand();
    // 指定穿穿到當天結束（測試的服務用真的時鐘、跟這支測試同一台機器同一個時區，沒辦法換成假時鐘）：
    // 離午夜不到 90 秒才先等過了再開始，免得中途剛好跨日、指定穿到期。到期的規則本身由 Go 單元測試守。
    const now = new Date();
    const left = 86400 - (now.getHours() * 3600 + now.getMinutes() * 60 + now.getSeconds());
    if (left <= 90) await page.waitForTimeout((left + 1) * 1000);
    try {
      // a 免費（預設輪流穿的就是它）、b 要 1 點。
      const ra = await api.outfit(a, 0, { order: 1 });
      const rb = await api.outfit(b, 1, { order: 2, cost: 1 });
      await api.seedPoint();
      const avatarIs = async (idx: number) =>
        (await (await request.get(`${baseURL}/avatars/code.png`)).body()).equals(
          readFileSync(IMG[idx] as string),
        );
      await expect.poll(() => avatarIs(0)).toBe(true);

      // 沒兌換的沒有「現在穿這套」；直接寫請求也被程式擋下。
      await page.goto(`/p/closet/outfit?id=${rb}`);
      await expect(page.getByTestId("outfit-unlock")).toBeVisible();
      await expect(page.getByTestId("wear-now")).toHaveCount(0);
      const forged = String(
        (
          (await (
            await request.post(`${baseURL}/api/vault/next_id`, { data: { kind: "closet.request" } })
          ).json()) as { id: string }
        ).id,
      );
      await request.post(`${baseURL}/api/vault/put`, {
        data: {
          record: {
            kind: "closet.request",
            id: forged,
            fields: { type: "wear", persona: "builder", outfit: b, state: "pending" },
          },
        },
      });
      await expect
        .poll(async () => (await api.get("closet.request", forged))?.fields.state)
        .toBe("rejected");
      expect((await api.get("closet.request", forged))?.fields.reason).toContain("要先兌換才能穿");
      expect(await avatarIs(0)).toBe(true);

      // 兌換：成功的地方直接有「現在穿這套」與「到衣櫃看」。
      await page.getByTestId("outfit-unlock").click();
      await page.getByTestId("outfit-unlock-yes").click();
      await expect(page.getByTestId("outfit-result")).toHaveText("兌換了");
      const wearNow = page.getByTestId("wear-now");
      await expect(wearNow).toBeVisible();
      await expect(wearNow).toHaveText("現在穿這套");
      await expect(page.getByTestId("outfit-to-wardrobe")).toHaveAttribute(
        "href",
        "/p/closet/wardrobe?persona=builder",
      );
      await expect(page.getByTestId("rotate-hint")).toContainText(
        "每小時輪流換；指定「現在穿這套」的那天不輪",
      );
      const box = await wearNow.boundingBox();
      expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);

      // 按了馬上換：畫面寫「換好了　今天都穿這套…」，按鈕變成「現在穿著（今天都穿這套）」。
      await wearNow.click();
      await expect(page.getByTestId("wear-result")).toHaveText(
        "換好了　今天都穿這套，明天起照輪流穿的換",
      );
      await expect(page.getByTestId("wearing-now")).toContainText("現在穿著（今天都穿這套）");
      await expect(page.getByTestId("wear-now")).toHaveCount(0);
      const person = await api.get("closet.person", "builder");
      expect(person?.fields).toMatchObject({ worn: b, picked: true });
      // 穿到當天結束：到期時間是今天之後的第一個午夜（服務算的）。
      const until = new Date(String(person?.fields.worn_until));
      expect([until.getHours(), until.getMinutes(), until.getSeconds()]).toEqual([0, 0, 0]);
      expect(until.getTime()).toBeGreaterThan(Date.now());
      expect(until.getTime() - Date.now()).toBeLessThanOrEqual(25 * 3600_000);
      // 輪流穿的清單沒被動：b 沒有被加進去。
      expect(person?.fields.rotation).toEqual([a]);
      await expect(page.getByTestId("rotate-toggle")).toHaveAttribute("aria-checked", "false");
      expect(await avatarIs(1)).toBe(true);

      // 復原：回到剛才那套（照輪流穿的 a），按鈕回來。
      await page.getByTestId("wear-undo").click();
      await expect(page.getByTestId("wear-result")).toHaveText("已復原");
      await expect(page.getByTestId("wear-now")).toBeVisible();
      await expect(page.getByTestId("wear-undo")).toHaveCount(0);
      await expect.poll(() => avatarIs(0)).toBe(true);
      expect((await api.get("closet.person", "builder"))?.fields).toMatchObject({
        worn: a,
        picked: false,
      });

      // 衣櫃：正在穿的那格寫「現在穿著」、另一格是「現在穿這套」；手機寬沒有水平捲動。
      await page.getByTestId("outfit-to-wardrobe").click();
      await expect(page).toHaveURL(/\/p\/closet\/wardrobe\?persona=builder/);
      await expect(tile(page, ra).getByTestId("wearing-now")).toBeVisible();
      await expect(tile(page, ra).getByTestId("wear-now")).toHaveCount(0);
      await expect(tile(page, rb).getByTestId("wear-now")).toBeVisible();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
        ),
      ).toBe(true);
      // 在衣櫃按：上面現穿的名字跟著換。
      await tile(page, rb).getByTestId("wear-now").click();
      await expect(tile(page, rb).getByTestId("wear-result")).toContainText("換好了");
      await expect(page.getByTestId("worn-name")).toContainText(`衣服${b.toUpperCase()}`);
      await expect(tile(page, rb).getByTestId("wearing-now")).toBeVisible();
      await expect(tile(page, ra).getByTestId("wear-now")).toBeVisible();
      await expect.poll(() => avatarIs(1)).toBe(true);

      // 今天頁的頭像換了：頁面上她的頭像抓回來就是剛換的那套的圖。
      await page.goto("/");
      const img = page.locator('img[src*="/avatars/code."]').first();
      await expect(img).toBeVisible();
      const bytes = await img.evaluate(async (el: HTMLImageElement) => {
        const res = await fetch(el.src, { cache: "reload" });
        return (await res.arrayBuffer()).byteLength;
      });
      expect(bytes).toBe(readFileSync(IMG[1] as string).byteLength);
    } finally {
      await api.cleanup();
    }
  });
});
