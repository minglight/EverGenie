import { copyFileSync, existsSync, readdirSync, readFileSync, rmSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { APIRequestContext } from "@playwright/test";
import { devTest, expect, type HgServer, test } from "./fixtures";

// 官方套件 closet-mine「上傳衣服與角色」（P6-4c，REQ-126；DEV-112 起預設啟用、自己不在選單上）。DEV-143 起沒有自己的入口頁：
// 衣服的上傳是核心角色頁的「衣服」分頁（slot core.persona.tab），新增角色在角色清單的「新增角色」（slot core.personas.new），
// 頁面上傳檔案到 runtime/share/、寫 closet-mine.request，程式（bin/evergenie-closet-mine，核心啟動）驗證後複製進 personas/mine/ 並登記到衣櫃。
// 同一個 worker 的家是共用的：每個測試用自己的角色代號（my + 隨機字尾），開頭把自己之前留下的資料收掉；
// 檔案結束時把套件開回預設（啟用）。素材沿用 e2e/fixtures/closet/ 的 PNG 與 persona.mp4。

const here = path.dirname(fileURLToPath(import.meta.url));
const fx = path.join(here, "fixtures");
const IMG = ["outfit-a.png", "outfit-b.png", "outfit-c.png"].map((n) => path.join(fx, "closet", n));
const MP4 = path.join(fx, "persona.mp4");
const rand = () =>
  Math.random()
    .toString(36)
    .slice(2, 8)
    .replace(/[^a-z0-9]/g, "x");

interface Rec {
  id: string;
  fields: Record<string, unknown>;
  archived?: boolean;
}

function client(request: APIRequestContext, base: string, server: HgServer) {
  const post = async (route: string, data: unknown) => {
    const res = await request.post(`${base}/api/${route}`, { data });
    const json = (await res.json().catch(() => ({}))) as Record<string, unknown>;
    expect(res.status(), `${route} ${JSON.stringify(json)}`).toBeLessThan(300);
    return json;
  };
  const list = async (kind: string, archived = false) =>
    ((await post("vault/list", { kind, include_archived: archived })) as { records?: Rec[] })
      .records ?? [];
  const get = async (kind: string, id: string): Promise<Rec | null> => {
    const res = await request.post(`${base}/api/vault/get`, { data: { kind, id } });
    return res.ok() ? ((await res.json()) as { record: Rec }).record : null;
  };
  const mine = path.join(server.home, "personas", "mine");
  const api = {
    post,
    list,
    get,
    mine,
    async setEnabled(enabled: boolean) {
      await post("package/set_enabled", { id: "closet-mine", enabled });
      await expect
        .poll(
          async () =>
            ((await post("package/get", { id: "closet-mine" })) as { package: { state: string } })
              .package.state,
          { timeout: 30_000 },
        )
        .toBe(enabled ? "running" : "stopped");
    },
    /** 開頭收掉之前留下的：pending 的請求封存、我的衣服封存、personas/mine 清空，等 person 清乾淨。 */
    async reset() {
      await api.setEnabled(true);
      for (const r of await list("closet-mine.request")) {
        await post("vault/archive", { kind: "closet-mine.request", id: r.id });
      }
      for (const r of await list("closet.outfit")) {
        if (r.fields.source === "mine")
          await post("vault/archive", { kind: "closet.outfit", id: r.id });
      }
      if (existsSync(mine)) {
        for (const f of readdirSync(mine))
          rmSync(path.join(mine, f), { recursive: true, force: true });
      }
      await expect
        .poll(
          async () => {
            const people = await list("closet.person");
            return people.filter((p) =>
              ((p.fields.outfits as { source?: string }[]) ?? []).some((o) => o.source === "mine"),
            ).length;
          },
          { timeout: 30_000 },
        )
        .toBe(0);
    },
    /** 上傳檔案（跟頁面同一個路由），回 /share/<名>。 */
    async upload(file: string, name?: string): Promise<string> {
      const res = await request.post(`${base}/api/web/upload`, {
        headers: {
          "Content-Type": "application/octet-stream",
          "X-Requested-With": "evergenie",
          "X-Filename": encodeURIComponent(name ?? path.basename(file)),
        },
        data: readFileSync(file),
      });
      expect(res.status()).toBe(200);
      return ((await res.json()) as { url: string }).url;
    },
    /** 寫一筆請求並等程式處理完，回請求。 */
    async send(fields: Record<string, unknown>): Promise<Rec> {
      const id = String((await post("vault/next_id", { kind: "closet-mine.request" })).id);
      await post("vault/put", {
        record: { kind: "closet-mine.request", id, fields: { ...fields, state: "pending" } },
      });
      let rec: Rec | null = null;
      await expect
        .poll(
          async () => {
            rec = await get("closet-mine.request", id);
            return rec?.fields.state;
          },
          { timeout: 30_000 },
        )
        .not.toBe("pending");
      return rec as unknown as Rec;
    },
    /** 用 API 建一個角色（代號 code），等它出現在 closet.person。 */
    async character(code: string, img = 0) {
      const url = await api.upload(IMG[img] as string);
      const r = await api.send({
        type: "character",
        upload: url,
        name: `角色${code}`,
        persona: code,
      });
      expect(r.fields.state, JSON.stringify(r.fields)).toBe("done");
      await expect
        .poll(
          async () => ((await get("closet.person", code))?.fields.outfits as unknown[])?.length,
          {
            timeout: 30_000,
          },
        )
        .toBe(1);
    },
  };
  return api;
}

const charPage = (code: string) => `/personas/${code}?tab=closet-mine.outfits`;

// 一般家的「衣服」分頁不該有的字（DEV-160 S5c，CMI-24）：設計中、評論、留言、生圖秘書、官方（含升成官方）、隱藏版
// （隱藏版開關關著時）；一般家沒有上架／下架（作者 11-15），上架、下架、還沒上架也不該有。「先放進設計中」勾選另外看 testid。
const NO_ORDINARY_WORDS = [
  /設計中/,
  /評論/,
  /留言/,
  /生圖秘書/,
  /官方/,
  /隱藏版/,
  /還沒上架/,
  /上架/,
  /下架/,
];
async function noOrdinaryWords(page: import("@playwright/test").Page, what: string) {
  const text = await page.getByTestId("mine-outfits").innerText();
  for (const w of NO_ORDINARY_WORDS) expect(text, `${what} 不該有 ${w}`).not.toMatch(w);
  await expect(page.getByTestId("mine-outfit-draft")).toHaveCount(0);
}

/** 打開一套的面板（總覽點那一列）。 */
async function openOutfit(page: import("@playwright/test").Page, record: string) {
  await page.getByTestId(`mine-row-${record}`).click({ timeout: 30_000 });
  await expect(page.getByTestId(`mine-outfit-${record}`)).toBeVisible();
}

const photoRows = (page: import("@playwright/test").Page, record: string, type = "half") =>
  page
    .getByTestId(`mine-photos-${record}`)
    .locator(`[data-testid="mine-photo"][data-type="${type}"]`);

/** 角色清單打開「新增角色」，選「上傳半身照」（closet-mine 掛的新增方式）。 */
async function openNewCharacter(page: import("@playwright/test").Page) {
  await page.goto("/personas?new=1");
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByTestId("persona-new-slot")).toHaveAttribute(
    "data-page",
    "closet-mine.new-character",
    { timeout: 30_000 },
  );
  return dialog.getByTestId("mine-new");
}

test.afterAll(async ({ hgServer }) => {
  // 開回預設（啟用）；用 fetch 直接呼叫，不需要 test 層級的 fixture。
  await fetch(`${hgServer.base}/api/package/set_enabled`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ id: "closet-mine", enabled: true }),
  });
});

// 預設啟用、不是選用（作者 2026-11-03）；入口是展示間的按鈕，關掉時展示間不顯示這些按鈕（CMI-9、CLO-15）。
test("TestShowroomOpensAddPages @smoke", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  await api.setEnabled(true);
  // 選單有「展示間」，closet-mine 自己沒有選單項目。
  const menu = JSON.stringify(await api.post("web/get_menu", {}));
  expect(menu).toContain("展示間");
  expect(menu).not.toContain("closet-mine");
  // 設定 → 套件的「進階套件（選用）」不列它。
  await page.goto("/settings/packages");
  await expect(page.getByTestId("optional-closet-mine")).toHaveCount(0);

  // 展示間的「新增角色」開到角色清單、一進來就打開「新增角色」（上傳半身照）；上一頁回展示間（DEV-143）。
  await page.goto("/p/closet/showroom?persona=builder");
  // 兩個入口收在展示間右上的「⋯」（DEV-156 U-15）。
  await page.getByTestId("showroom-add").click();
  await page.getByTestId("showroom-new-character").click();
  await expect(page).toHaveURL(/\/personas\?new=1$/);
  await expect(page.getByRole("dialog").getByTestId("mine-new")).toBeVisible();
  await page.getByRole("dialog").getByRole("button", { name: "先不要" }).click();
  await expect(page).toHaveURL(/\/personas$/);
  await page.getByRole("link", { name: "上一頁" }).click();
  await expect(page.getByTestId("showroom")).toBeVisible();
  // 「加一套衣服」開到現在看的那位角色的「衣服」分頁（展示間還沒有角色時到角色清單）。
  await page.getByTestId("showroom-add").click();
  await page.getByTestId("showroom-add-outfit").click();
  await expect(page).toHaveURL(/\/personas(\/[^/?]+\?tab=closet-mine\.outfits)?$/);

  // 關掉：展示間不顯示新增的按鈕；開回來又有。
  await api.setEnabled(false);
  await page.goto("/p/closet/showroom");
  await expect(page.getByTestId("showroom")).toHaveAttribute("data-mine", "false");
  await expect(page.getByTestId("showroom-add")).toHaveCount(0);
  await api.setEnabled(true);
  await page.goto("/p/closet/showroom");
  await expect(page.getByTestId("showroom-add")).toBeVisible();
});

// Guards: REQ-126
test("TestCreateCharacterAppearsInWardrobe @smoke", async ({
  page,
  request,
  baseURL,
  hgServer,
}) => {
  const api = client(request, baseURL ?? "", hgServer);
  await api.reset();
  const code = `my${rand()}`;
  const form = await openNewCharacter(page);
  await form.getByTestId("mine-new-name").fill("小狐狸");
  await form.getByTestId("mine-new-desc").fill("一隻愛搗蛋的狐狸");
  await form.getByTestId("mine-new-file").setInputFiles(IMG[0] as string);
  await form.getByText("進階：自訂代號").click();
  await form.getByTestId("mine-new-code").fill(code);
  await form.getByTestId("mine-new-submit").click();
  // 送出後對話框顯示「處理中」；程式建好就關掉對話框、開到她的頁。
  await expect(page.getByTestId("mine-result")).toHaveAttribute("data-kind", "waiting");
  // 建好就開到她的頁（還沒擔任職位）；「衣服」分頁有她的基本款，只有一張半身照，缺全身、輪播、動畫一眼看得到。
  await expect(page).toHaveURL(new RegExp(`/personas/${code}$`), { timeout: 30_000 });
  await expect(page.getByTestId("persona-detail")).toContainText("小狐狸");
  await expect(page.getByTestId("persona-seat-now")).toHaveText("還沒擔任職位");
  await page.goto(charPage(code));
  // 總覽一套一列（DEV-143）：列上的小標已看得到缺全身、缺動畫；點進去是那套的面板，每張照片一列（CMI-24）。
  const row = page.getByTestId(`mine-row-${code}-base`);
  await expect(row).toContainText("基本款", { timeout: 30_000 });
  await expect(row.locator('[data-chip="full"]')).toHaveText("缺全身");
  await expect(row.locator('[data-chip="video"]')).toHaveText("缺動畫");
  await row.click();
  await expect(page.getByTestId(`mine-outfit-${code}-base`)).toContainText("基本款");
  await expect(photoRows(page, `${code}-base`)).toHaveCount(1);
  await expect(photoRows(page, `${code}-base`, "full")).toHaveCount(0);
  // 只剩一張半身照：不能單獨刪（衣服要有封面），沒有「刪掉」。
  await expect(photoRows(page, `${code}-base`).getByTestId("mine-photo-remove")).toHaveCount(0);
  const parts = page.getByTestId(`mine-parts-${code}-base`);
  await expect(parts.locator('[data-part="half"]')).toHaveAttribute("data-ok", "true");
  await expect(parts.locator('[data-part="full"]')).toHaveText("缺全身");
  await expect(parts.locator('[data-part="rotation"]')).toHaveText("缺輪播照");
  await expect(parts.locator('[data-part="video"]')).toHaveText("缺動畫");
  // 衣櫃裡有她，基本款是擁有的真圖，位元組等於上傳檔；角色卡用 YAML 檔頭。
  await page.goto(`/p/closet/wardrobe?persona=${code}`);
  await expect(
    page.locator(`[data-testid="outfit-tile"][data-outfit="${code}-base"]`),
  ).toHaveAttribute("data-owned", "true", { timeout: 30_000 });
  const real = await request.get(`${baseURL}/pkg-files/closet/o/${code}/base/1.png`);
  expect(real.status()).toBe(200);
  expect((await real.body()).equals(readFileSync(IMG[0] as string))).toBe(true);
  const card = readFileSync(path.join(api.mine, `${code}.md`), "utf8");
  expect(card.startsWith('---\nschema: 1\nname: "小狐狸"\n---\n一隻愛搗蛋的狐狸')).toBe(true);
  // 角色本身的頭像在角色卡的慣例路徑 avatars/<代號>.png（複製角色進套件只認它），基本款第一張的照片紀錄指著它（DEV-160 S3）。
  expect(
    readFileSync(path.join(api.mine, "avatars", `${code}.png`)).equals(
      readFileSync(IMG[0] as string),
    ),
  ).toBe(true);
  const first = (await api.list("closet.photo")).filter((r) => r.fields.outfit === `${code}-base`);
  expect(first.map((r) => r.fields.file)).toEqual([`avatars/${code}.png`]);
  expect((await api.get("closet.person", code))?.fields.name).toBe("小狐狸");
});

// Guards: REQ-125, REQ-126
test("TestAddPaidOutfitShowsSilhouette", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  await api.reset();
  const code = `my${rand()}`;
  await api.character(code);
  await page.goto(charPage(code));
  await page.getByRole("button", { name: "＋ 加一套衣服" }).click({ timeout: 30_000 });
  const form = page.getByTestId("mine-outfit");
  await expect(form).toBeVisible();
  await form.getByTestId("mine-outfit-name").fill("海邊");
  await form.getByTestId("mine-outfit-desc").fill("夏天的衣服");
  // 「要用點數換」預設關（一上傳就擁有）；打開才出現幾點，預設 1。
  await expect(form.getByTestId("mine-outfit-cost")).toHaveCount(0);
  await form.getByTestId("mine-outfit-paid").check();
  await expect(form.getByTestId("mine-outfit-cost")).toHaveValue("1");
  await form.getByTestId("mine-outfit-file").setInputFiles(IMG[1] as string);
  await form.getByTestId("mine-outfit-submit").click();
  await expect(page.getByTestId("mine-result")).toHaveAttribute("data-kind", "done", {
    timeout: 30_000,
  });
  const outfits = (await api.list("closet.outfit")).filter(
    (r) => r.fields.persona === code && r.fields.outfit !== "base",
  );
  expect(outfits).toHaveLength(1);
  const rec = outfits[0] as Rec;
  expect(rec.fields).toMatchObject({ source: "mine", cost: 1, state: "active", name: "海邊" });
  // 一般家的頁面不帶 draft（上傳直接生效）。
  const reqs = await api.list("closet-mine.request");
  expect(reqs.find((r) => r.fields.type === "outfit")?.fields).not.toHaveProperty("draft");
  const outfitId = String(rec.fields.outfit);
  // 沒兌換：不在衣櫃；展示間只看到剪影＋鎖頭，真圖 404、剪影不等於上傳的圖。
  await page.goto(`/p/closet/showroom?persona=${code}`);
  const tile = page.locator(`[data-testid="outfit-tile"][data-outfit="${rec.id}"]`);
  await expect(tile).toBeVisible({ timeout: 30_000 });
  await expect(tile.getByTestId("outfit-img")).toHaveAttribute(
    "src",
    new RegExp(`^/pkg-files/closet/s/${code}/${outfitId}/`),
  );
  await expect(tile.getByTestId("lock-badge")).toBeVisible();
  await expect(tile.getByTestId("outfit-tags")).toContainText("1 點");
  expect(
    (await request.get(`${baseURL}/pkg-files/closet/o/${code}/${outfitId}/1.png`)).status(),
  ).toBe(404);
  const sil = await request.get(`${baseURL}/pkg-files/closet/s/${code}/${outfitId}/1.png`);
  expect(sil.status()).toBe(200);
  expect((await sil.body()).equals(readFileSync(IMG[1] as string))).toBe(false);
  // 圖確實寫進 personas/mine/photos/<照片編號>.png（不是官方資料夾），照片紀錄指著它（DEV-160 S3）。
  const photos = (await api.list("closet.photo")).filter((r) => r.fields.outfit === rec.id);
  expect(photos).toHaveLength(1);
  const photo = photos[0] as Rec;
  expect(photo.fields).toMatchObject({
    source: "mine",
    hidden: false,
    adopted: true,
    file: `photos/${photo.id}.png`,
  });
  expect(
    readFileSync(path.join(api.mine, String(photo.fields.file))).equals(
      readFileSync(IMG[1] as string),
    ),
  ).toBe(true);
  expect(
    existsSync(path.join(hgServer.home, "personas", "avatars", `${code}@${outfitId}.png`)),
  ).toBe(false);
});

// Guards: REQ-126
test("TestAddRotationPhotoAndFull", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  await api.reset();
  const code = `my${rand()}`;
  await api.character(code);
  await page.goto(charPage(code));
  await page.getByTestId(`mine-row-${code}-base`).click({ timeout: 30_000 });
  const card = page.getByTestId(`mine-outfit-${code}-base`);
  const part = (key: string) =>
    page.getByTestId(`mine-parts-${code}-base`).locator(`[data-part="${key}"]`);
  await expect(part("rotation")).toHaveText("缺輪播照", { timeout: 30_000 });

  // 加一張輪播照。
  await page.getByTestId(`mine-photo-input-${code}-base`).setInputFiles(IMG[1] as string);
  await expect(page.getByTestId("mine-result")).toHaveAttribute("data-kind", "done", {
    timeout: 30_000,
  });
  await expect(part("rotation")).toHaveText("輪播 2 張", { timeout: 30_000 });
  await expect
    .poll(
      async () => (await request.get(`${baseURL}/pkg-files/closet/o/${code}/base/2.png`)).status(),
      {
        timeout: 30_000,
      },
    )
    .toBe(200);

  // 放全身照，詳情頁多一個半身／全身切換。
  await page.getByTestId(`mine-full-input-${code}-base`).setInputFiles(IMG[2] as string);
  await expect(part("full")).toHaveAttribute("data-ok", "true", { timeout: 30_000 });
  await expect(card).toBeVisible();
  const full = await request.get(`${baseURL}/pkg-files/closet/f/${code}/base/1.png`);
  expect(full.status()).toBe(200);
  expect((await full.body()).equals(readFileSync(IMG[2] as string))).toBe(true);
  await page.goto(`/p/closet/outfit?id=${code}-base`);
  await page.getByRole("button", { name: "全身" }).click();
  const big = page.getByTestId("outfit-full");
  await expect(big).toHaveAttribute("src", `/pkg-files/closet/f/${code}/base/1.png`);
  await expect
    .poll(() => big.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth > 0))
    .toBe(true);
  await page.getByRole("button", { name: "半身" }).click();
  await expect(page.getByTestId("outfit-full")).toHaveCount(0);

  // 放動畫（第 1 張的「加動畫」）：公開資料夾有同張的 mp4；那張底下多一列動畫。
  await page.goto(charPage(code));
  await openOutfit(page, `${code}-base`);
  await expect(photoRows(page, `${code}-base`)).toHaveCount(2, { timeout: 30_000 });
  const half1 = String(await photoRows(page, `${code}-base`).first().getAttribute("data-photo"));
  await page.getByTestId(`mine-clip-input-${half1}`).setInputFiles(MP4);
  await expect(page.getByTestId("mine-result")).toHaveAttribute("data-kind", "done", {
    timeout: 30_000,
  });
  await expect
    .poll(
      async () => (await request.get(`${baseURL}/pkg-files/closet/o/${code}/base/1.mp4`)).status(),
      {
        timeout: 30_000,
      },
    )
    .toBe(200);
  await expect(photoRows(page, `${code}-base`, "video")).toHaveCount(1, { timeout: 30_000 });
  // 四個檔各有一筆照片紀錄指著（DEV-160 S3）：第一張是角色本身的頭像 avatars/<代號>.png，其他三個在 photos/、檔名是照片編號。
  const files = (await api.list("closet.photo"))
    .filter((r) => r.fields.outfit === `${code}-base`)
    .map((r) => String(r.fields.file))
    .sort();
  expect(files).toHaveLength(4);
  expect(
    [
      ...readdirSync(path.join(api.mine, "avatars")).map((f) => `avatars/${f}`),
      ...readdirSync(path.join(api.mine, "photos")).map((f) => `photos/${f}`),
    ].sort(),
  ).toEqual(files);
});

// Guards: REQ-126
test("TestRejectsNonImageUpload", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  await api.reset();
  const code = `my${rand()}`;
  const form = await openNewCharacter(page);
  await form.getByTestId("mine-new-name").fill("壞檔案");
  await form.getByText("進階：自訂代號").click();
  await form.getByTestId("mine-new-code").fill(code);

  // 本機先擋：文字檔、太大的檔案不會送出（沒有請求產生）。
  await form.getByTestId("mine-new-file").setInputFiles({
    name: "notes.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("hello"),
  });
  await form.getByTestId("mine-new-submit").click();
  await expect(form).toContainText("只收 PNG、JPEG、WEBP 圖片");
  expect(await api.list("closet-mine.request")).toHaveLength(0);

  // 偽裝成 png 的文字檔：本機看副檔名放行，程式看檔案開頭拒絕，原因直接顯示，什麼都沒寫。
  await form.getByTestId("mine-new-file").setInputFiles({
    name: "fake.png",
    mimeType: "image/png",
    buffer: Buffer.from("this is not a picture"),
  });
  await form.getByTestId("mine-new-submit").click();
  const result = page.getByTestId("mine-result");
  await expect(result).toHaveAttribute("data-kind", "rejected", { timeout: 30_000 });
  await expect(result).toContainText("只收 PNG、JPEG、WEBP 圖片");
  const reqs = await api.list("closet-mine.request");
  expect(reqs).toHaveLength(1);
  expect(reqs[0]?.fields).toMatchObject({ state: "rejected" });
  expect(existsSync(path.join(api.mine, `${code}.md`))).toBe(false);
  expect(existsSync(path.join(api.mine, "avatars"))).toBe(false);
  expect(existsSync(path.join(api.mine, "photos"))).toBe(false);
  expect(await api.get("closet.outfit", `${code}-base`)).toBeNull();
});

// Guards: REQ-233
// 指派搬到核心角色頁的「介紹」（DEV-143，personas.spec 的 TestPersonaPageTabsAssignAndExport）；這裡只驗衣服分頁加一套
// 「要用點數換」關著的衣服：直接擁有、不用點數，分頁裡看得到。
test("TestOutfitsTabAddOwnedOutfit", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  await api.reset();
  const code = `my${rand()}`;
  await api.character(code);
  await page.goto(charPage(code));
  await page.getByRole("button", { name: "＋ 加一套衣服" }).click({ timeout: 30_000 });
  const form = page.getByTestId("mine-outfit");
  await expect(form).toBeVisible();
  await form.getByTestId("mine-outfit-name").fill("自己的");
  await form.getByTestId("mine-outfit-file").setInputFiles(IMG[2] as string);
  await form.getByTestId("mine-outfit-submit").click();
  await expect(page.getByTestId("mine-result")).toHaveAttribute("data-kind", "done", {
    timeout: 30_000,
  });
  const mineOutfits = (await api.list("closet.outfit")).filter(
    (r) => r.fields.persona === code && r.fields.outfit !== "base",
  );
  expect(mineOutfits).toHaveLength(1);
  expect(mineOutfits[0]?.fields).toMatchObject({
    source: "mine",
    cost: 0,
    state: "active",
    name: "自己的",
  });
  await expect(page.getByTestId(`mine-row-${mineOutfits[0]?.id}`)).toContainText("自己的", {
    timeout: 30_000,
  });
});

// 自己的隱藏版開關（closet 的 closet.adult）關著時，衣服分頁完全沒有「放進自己的隱藏版」；開了幾秒內出現（CLO-18、DEV-143）。
test("TestOutfitsTabHidesAdultWhenSwitchOff", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  await api.reset();
  const code = `my${rand()}`;
  await api.character(code);
  const setAdult = (value: string) =>
    api.post("package/set_extension", {
      package: "closet",
      extension: "adult",
      value: value ? { value } : {},
    });
  try {
    await setAdult("off");
    await page.goto(charPage(code));
    await page.getByTestId(`mine-row-${code}-base`).click({ timeout: 30_000 });
    await expect(page.getByTestId(`mine-outfit-${code}-base`)).toBeVisible();
    await expect(page.locator('[data-testid^="mine-adult-"]')).toHaveCount(0);
    // 關著時加照片：請求不帶 adult。
    await page.getByTestId(`mine-photo-input-${code}-base`).setInputFiles(IMG[1] as string);
    await expect(page.getByTestId("mine-result")).toHaveAttribute("data-kind", "done", {
      timeout: 30_000,
    });
    const photoReqs = async () =>
      (await api.list("closet-mine.request")).filter((r) => r.fields.type === "photo");
    expect((await photoReqs())[0]?.fields).not.toHaveProperty("adult");
    await setAdult("on");
    const adult = page.getByTestId(`mine-adult-${code}-base`);
    await expect(adult).toBeAttached({ timeout: 15_000 });
    // 勾選有出現但沒勾：明送 adult = false，照片是一般版（DEV-160 S3 的規則：算不算隱藏版只看 adult）。
    await expect(adult).not.toBeChecked();
    await page.getByTestId(`mine-photo-input-${code}-base`).setInputFiles(IMG[2] as string);
    await expect.poll(async () => (await photoReqs()).length, { timeout: 30_000 }).toBe(2);
    await expect
      .poll(async () => (await photoReqs()).map((r) => r.fields.state), { timeout: 30_000 })
      .toEqual(["done", "done"]);
    const second = (await photoReqs()).find((r) => "adult" in r.fields);
    expect(second?.fields.adult).toBe(false);
    const hidden = (await api.list("closet.photo"))
      .filter((r) => r.fields.outfit === `${code}-base`)
      .map((r) => r.fields.hidden);
    expect(hidden).toEqual([false, false, false]);
  } finally {
    await setAdult("");
  }
});

// 官方角色的「衣服」分頁也沒有開發用的字（CMI-24）：官方衣服不標「官方」，自己的衣服沒有「升成官方」
// （升成官方只在開發家的「修改」分頁，CMI-16；流程在 closet-promote.spec）。
test("TestOfficialPersonaOutfitsTabHasNoDevWords", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  await api.reset();
  // 一套官方衣服（官方角色 helper 的），看一般家的這頁沒有「官方」字樣（CMI-24）；結束時收掉。
  const offId = `off${rand()}`;
  const offRecord = `helper-${offId}`;
  const offFile = path.join(hgServer.home, "personas", "avatars", `helper@${offId}.png`);
  copyFileSync(IMG[0] as string, offFile);
  await api.post("vault/put", {
    record: {
      kind: "closet.outfit",
      id: offRecord,
      fields: {
        persona: "helper",
        outfit: offId,
        name: "海邊外套",
        cost: 0,
        state: "active",
        source: "official",
      },
    },
  });
  try {
    const r = await api.send({
      type: "outfit",
      upload: await api.upload(IMG[1] as string),
      persona: "helper",
      name: "正式家的衣服",
      cost: 0,
    });
    expect(r.fields.state, JSON.stringify(r.fields)).toBe("done");
    const record = String(r.fields.result);
    await page.goto(charPage("helper"));
    await expect(page.getByTestId(`mine-row-${record}`)).toBeVisible({ timeout: 30_000 });
    // 官方角色的這頁也沒有「官方」那些字（CMI-24）：總覽與官方衣服的卡都看。
    await expect(page.getByTestId(`mine-row-${offRecord}`)).toBeVisible({ timeout: 30_000 });
    await noOrdinaryWords(page, "官方角色的總覽");
    await openOutfit(page, offRecord);
    await noOrdinaryWords(page, "官方衣服的卡");
    await page.getByRole("button", { name: /全部衣服/ }).click();
    await page.getByTestId(`mine-row-${record}`).click();
    await expect(page.getByTestId(`mine-outfit-${record}`)).toBeVisible();
    // 面板畫好了（「照片」在按鈕前面、「刪掉這套」在最後），沒有「升成官方」。
    await expect(page.getByRole("heading", { name: "照片", exact: true })).toBeVisible();
    await expect(page.getByTestId(`mine-remove-outfit-${record}`)).toBeVisible();
    await expect(page.getByRole("button", { name: /升成官方/ })).toHaveCount(0);
    await noOrdinaryWords(page, "自己的衣服的面板");
  } finally {
    await api.post("vault/archive", { kind: "closet.outfit", id: offRecord });
    rmSync(offFile, { force: true });
    await api.reset();
  }
});

// closet-mine 關掉後，開它的分頁網址回到「介紹」，網址的 ?tab= 用 replace 拿掉（DEV-143）。
test("TestPersonaTabFallsBackWhenPackageOff", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  try {
    await api.setEnabled(false);
    await page.goto("/personas/builder?tab=closet-mine.outfits");
    await expect(page.getByTestId("persona-tab-intro")).toBeVisible({ timeout: 15_000 });
    await expect(page).toHaveURL(/\/personas\/builder$/);
    await expect(page.getByTestId("persona-tab")).toHaveCount(0);
  } finally {
    await api.setEnabled(true);
  }
});

// 一般家的「衣服」分頁（DEV-160 S5c，CMI-24）：加照片、刪一張與拿回、改點數、刪一套與拿回，都在這頁；
// 刪掉的列在總覽最下面的「刪掉的」。整頁沒有設計中、評論、留言、生圖秘書、官方、隱藏版、上架、下架這些字，也沒有「先放進設計中」。
test("TestOrdinaryOutfitsTabRemoveRestoreAndCost", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  await api.reset();
  const code = `my${rand()}`;
  await api.character(code);
  await page.goto(charPage(code));
  const tab = page.getByTestId("mine-outfits");
  await expect(tab).toHaveAttribute("data-edit", "false", { timeout: 30_000 });
  await page.getByRole("button", { name: "＋ 加一套衣服" }).click();
  await expect(page.getByTestId("mine-outfit")).toBeVisible();
  await noOrdinaryWords(page, "總覽與加一套");

  // 加一套（要用點數換關著）、再加一張半身照。
  const form = page.getByTestId("mine-outfit");
  await form.getByTestId("mine-outfit-name").fill("睡衣");
  await form.getByTestId("mine-outfit-desc").fill("在家穿的");
  await form.getByTestId("mine-outfit-file").setInputFiles(IMG[1] as string);
  await form.getByTestId("mine-outfit-submit").click();
  await expect(page.getByTestId("mine-result")).toHaveAttribute("data-kind", "done", {
    timeout: 30_000,
  });
  const rec = (await api.list("closet.outfit")).find(
    (r) => r.fields.persona === code && r.fields.outfit !== "base",
  ) as Rec;
  expect(rec.fields).toMatchObject({ cost: 0, state: "active" });
  await openOutfit(page, rec.id);
  await page.getByTestId(`mine-photo-input-${rec.id}`).setInputFiles(IMG[2] as string);
  await expect(photoRows(page, rec.id)).toHaveCount(2, { timeout: 30_000 });
  await noOrdinaryWords(page, "一套的面板");

  // 換第 1 張：請求帶那張的 photo_id（同一筆紀錄換新檔）；結果顯示在那一列（UXC-9）。
  const first = photoRows(page, rec.id).nth(0);
  const firstId = String(await first.getAttribute("data-photo"));
  await page.getByTestId(`mine-replace-input-${firstId}`).setInputFiles(IMG[0] as string);
  await expect(first.getByTestId("mine-result")).toHaveAttribute("data-kind", "done", {
    timeout: 30_000,
  });
  const replaced = (await api.list("closet-mine.request")).filter(
    (r) => r.fields.photo_id === firstId && r.fields.type === "photo",
  );
  expect(replaced.map((r) => r.fields.state)).toEqual(["done"]);
  await expect(photoRows(page, rec.id)).toHaveCount(2);

  // 刪第 2 張：先問一次，確定了才送；那一列不見。
  const second = photoRows(page, rec.id).nth(1);
  const secondId = String(await second.getAttribute("data-photo"));
  await second.getByTestId("mine-photo-remove").click();
  await expect(page.getByTestId("mine-photo-confirm")).toContainText("第 2 張");
  await page.getByTestId("mine-photo-confirm-yes").click();
  await expect(page.getByTestId("mine-result")).toContainText("刪掉了", { timeout: 30_000 });
  await expect(photoRows(page, rec.id)).toHaveCount(1, { timeout: 30_000 });
  expect((await api.get("closet.photo", secondId))?.archived).toBe(true);

  // 改點數：打開「要用點數換」，預設 1 點，「改好了」送 set_cost。
  await page.getByTestId(`mine-cost-paid-${rec.id}`).check();
  await expect(page.getByTestId(`mine-cost-points-${rec.id}`)).toHaveValue("1");
  await page.getByTestId(`mine-cost-points-${rec.id}`).fill("3");
  await page.getByTestId(`mine-cost-save-${rec.id}`).click();
  await expect
    .poll(async () => (await api.get("closet.outfit", rec.id))?.fields.cost, { timeout: 30_000 })
    .toBe(3);

  // 回總覽：「刪掉的」有那張，拿回來。
  await page.getByRole("button", { name: /全部衣服/ }).click();
  const deleted = page.getByTestId("mine-deleted");
  const photoItem = deleted.locator(`[data-testid="mine-deleted-item"][data-id="${secondId}"]`);
  await expect(photoItem).toContainText("睡衣", { timeout: 30_000 });
  await noOrdinaryWords(page, "刪掉的清單");
  await photoItem.getByTestId("mine-deleted-restore").click();
  await expect(page.getByTestId("mine-result")).toContainText("拿回來了", { timeout: 30_000 });
  await expect(photoItem).toHaveCount(0, { timeout: 30_000 });
  await expect
    .poll(async () => (await api.get("closet.photo", secondId))?.archived, { timeout: 30_000 })
    .toBe(false);

  // 刪一整套：回到總覽、那一列不見，「刪掉的」有它；拿回來又出現。
  await openOutfit(page, rec.id);
  await page.getByTestId(`mine-remove-outfit-${rec.id}`).click();
  await page.getByTestId("mine-remove-outfit-confirm-yes").click();
  await expect(page.getByTestId("mine-outfit-panel")).toHaveCount(0, { timeout: 30_000 });
  await expect(page.getByTestId(`mine-row-${rec.id}`)).toHaveCount(0, { timeout: 30_000 });
  const outfitItem = deleted.locator(
    `[data-testid="mine-deleted-item"][data-kind="outfit"][data-id="${rec.fields.outfit}"]`,
  );
  await expect(outfitItem).toContainText("睡衣", { timeout: 30_000 });
  await outfitItem.getByTestId("mine-deleted-restore").click();
  await expect(page.getByTestId(`mine-row-${rec.id}`)).toBeVisible({ timeout: 30_000 });
  await expect(outfitItem).toHaveCount(0, { timeout: 30_000 });
  await noOrdinaryWords(page, "拿回之後");
});

// 一般家沒有上架／下架（作者 11-15，CMI-23 作廢、CLO-38）：秘書照開發家的做法送了 draft = true、或以前留下的設計中衣服，
// 衣櫃每一輪自動改成上架、照片標採用，衣櫃看得到；列上沒有「還沒上架」也沒有「上架」鈕。
// 自己的衣服被改成下架（state = retired，秘書或以前留下的）：一般家把它列在「刪掉的」，「拿回來」＝改回上架（CMI-20）。
test("TestOrdinaryHomeHasNoListing", async ({ page, request, baseURL, hgServer }) => {
  const api = client(request, baseURL ?? "", hgServer);
  await api.reset();
  const code = `my${rand()}`;
  await api.character(code);
  const r = await api.send({
    type: "outfit",
    upload: await api.upload(IMG[1] as string),
    persona: code,
    name: "舊的草稿",
    free: true,
    draft: true,
  });
  expect(r.fields.state, JSON.stringify(r.fields)).toBe("done");
  const record = String(r.fields.result);
  await expect
    .poll(async () => (await api.get("closet.outfit", record))?.fields.state, { timeout: 30_000 })
    .toBe("active");
  await expect
    .poll(
      async () =>
        (await api.list("closet.photo"))
          .filter((p) => p.fields.outfit === record)
          .map((p) => p.fields.adopted),
      { timeout: 30_000 },
    )
    .toEqual([true]);
  await page.goto(charPage(code));
  await expect(page.getByTestId(`mine-row-${record}`)).toBeVisible({ timeout: 30_000 });
  await noOrdinaryWords(page, "自動上架後的總覽");
  await openOutfit(page, record);
  await noOrdinaryWords(page, "自動上架後的面板");
  await expect(page.getByTestId(`mine-publish-${record}`)).toHaveCount(0);
  await page.goto(`/p/closet/wardrobe?persona=${code}`);
  await expect(page.locator(`[data-testid="outfit-tile"][data-outfit="${record}"]`)).toBeVisible({
    timeout: 30_000,
  });

  // 改成下架：總覽沒有這一列，「刪掉的」有它；拿回來＝改回上架。
  await api.post("vault/patch", { kind: "closet.outfit", id: record, set: { state: "retired" } });
  await page.goto(charPage(code));
  const deleted = page.getByTestId("mine-deleted");
  const outfitId = String((await api.get("closet.outfit", record))?.fields.outfit);
  const item = deleted.locator(
    `[data-testid="mine-deleted-item"][data-kind="outfit"][data-id="${outfitId}"]`,
  );
  await expect(item).toContainText("舊的草稿", { timeout: 30_000 });
  await expect(page.getByTestId(`mine-row-${record}`)).toHaveCount(0);
  await noOrdinaryWords(page, "下架的那套在刪掉的");
  await item.getByTestId("mine-deleted-restore").click();
  await expect(page.getByTestId("mine-result")).toContainText("拿回來了", { timeout: 30_000 });
  await expect
    .poll(async () => (await api.get("closet.outfit", record))?.fields.state, { timeout: 30_000 })
    .toBe("active");
  await expect(page.getByTestId(`mine-row-${record}`)).toBeVisible({ timeout: 30_000 });
  await expect(item).toHaveCount(0, { timeout: 30_000 });
});

// 開發家（這個家支援 edit）的「衣服」分頁跟一般家長得一樣（DEV-160 審查 S1，CMI-24）：沒有「先放進設計中」、
// 官方衣服不標「官方」、沒有「升成官方」、沒有到「修改」分頁的入口（「修改」分頁就在旁邊）；頁面送的請求不帶 draft。
// 升成官方在「修改」分頁那一套的塊裡（closet-promote.spec），放回開發中也在那裡（closet-edit.spec）。
devTest.describe("開發家的衣服分頁", () => {
  devTest("TestDevHomeOutfitsTabSameAsOrdinary", async ({ page, request, baseURL, hgServer }) => {
    const api = client(request, baseURL ?? "", hgServer);
    await api.reset();
    try {
      // 先有一套，「加一套衣服」才是收起來的按鈕（沒有衣服時表單直接展開，送出後又收起來）。
      const first = await api.send({
        type: "outfit",
        upload: await api.upload(IMG[1] as string),
        persona: "helper",
        name: "開發家的第一套",
        cost: 0,
      });
      expect(first.fields.state, JSON.stringify(first.fields)).toBe("done");
      await page.goto(charPage("helper"));
      await expect(page.getByTestId("mine-outfits")).toHaveAttribute("data-edit", "true", {
        timeout: 30_000,
      });
      await expect(page.getByTestId(`mine-row-${String(first.fields.result)}`)).toBeVisible({
        timeout: 30_000,
      });
      await page.getByRole("button", { name: "＋ 加一套衣服" }).click();
      const form = page.getByTestId("mine-outfit");
      await expect(form).toBeVisible();
      await expect(page.getByTestId("mine-outfit-draft")).toHaveCount(0);
      await expect(form).not.toContainText("設計中");
      await form.getByTestId("mine-outfit-name").fill("開發家的衣服");
      await form.getByTestId("mine-outfit-file").setInputFiles(IMG[2] as string);
      await form.getByTestId("mine-outfit-submit").click();
      await expect(form.getByTestId("mine-result")).toHaveAttribute("data-kind", "done", {
        timeout: 30_000,
      });
      const req = (await api.list("closet-mine.request")).find(
        (q) => q.fields.type === "outfit" && q.fields.name === "開發家的衣服",
      );
      expect(req?.fields).not.toHaveProperty("draft");
      const rec = (await api.list("closet.outfit")).find(
        (o) => o.fields.persona === "helper" && o.fields.name === "開發家的衣服",
      ) as Rec;
      expect(rec.fields.state).toBe("active");
      await expect(page.getByTestId(`mine-row-${rec.id}`)).toBeVisible({ timeout: 30_000 });
      const text = await page.getByTestId("mine-outfits").innerText();
      expect(text).not.toMatch(/官方/);
      await openOutfit(page, rec.id);
      await expect(page.getByRole("heading", { name: "照片", exact: true })).toBeVisible();
      await expect(page.getByRole("button", { name: /升成官方/ })).toHaveCount(0);
      await expect(page.getByTestId(`mine-to-edit-${rec.id}`)).toHaveCount(0);
      expect(await page.getByTestId("mine-outfits").innerText()).not.toMatch(/官方|設計中/);
    } finally {
      await api.reset();
    }
  });
});
