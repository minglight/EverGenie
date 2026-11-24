import { copyFileSync, mkdirSync, readdirSync, readFileSync, rmSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { APIRequestContext, Locator, Page } from "@playwright/test";
import { devTest, expect, type HgServer, shot, test } from "./fixtures";

// 角色頁的「修改」分頁（DEV-160 S6，衣櫃卡 CLO-35）：只有這個家支援 edit（開發家，devTest：home.toml [runtime] env = "dev"）才有這個分頁，
// 正式家（test）沒有。每個測試在 code 秘書的人設 builder 身上造兩套衣服（官方的開發中一套、我的上架一套）與它們的照片紀錄
// （closet.photo：測試自己寫，編號跟衣櫃自己補登記的一樣，所以不會多出第二筆），圖是 e2e/fixtures/closet/ 的替代圖樣，不是真的角色。
// 同一個 worker 的家是共用的：每個測試用自己的衣服編號（封存的編號不能再用），結尾把自己造的東西收掉。
// 隱藏版開關預設關著：隱藏版的照片整張不列、沒有「顯示隱藏版」（作者 11-15，S3a）；要看隱藏版的測試先 openHidden()。

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
  archived?: boolean;
}

/** 測試造的一位角色的兩套衣服與照片：a＝官方、開發中；b＝我的、上架中、每張都採用了。 */
interface Seed {
  a: string;
  b: string;
  half1: string;
  half2: string;
  full: string;
  video: string;
  hidden: string;
  mine: string;
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
  const put = (kind: string, id: string, fields: Record<string, unknown>) =>
    post("vault/put", { record: { kind, id, fields } });
  const avatars = path.join(server.home, "personas", "avatars");
  const mineAvatars = path.join(server.home, "personas", "mine", "avatars");
  const files: string[] = [];
  const copy = (src: string, dst: string) => {
    mkdirSync(path.dirname(dst), { recursive: true });
    copyFileSync(src, dst);
    files.push(dst);
  };
  const made: { kind: string; id: string }[] = [];
  /** 收尾要還原的：別人留下的紀錄先改的欄位（quiet）、打開的「自己的隱藏版」開關（openHidden）。 */
  const undo: (() => Promise<unknown>)[] = [];
  const setAdult = (value: string) =>
    post("package/set_extension", {
      package: "closet",
      extension: "adult",
      value: value ? { value } : {},
    });
  const api = {
    post,
    list,
    get,
    put,
    /** 上傳檔案（跟頁面同一個路由），回 /share/<名>。 */
    async upload(file: string): Promise<string> {
      const res = await request.post(`${base}/api/web/upload`, {
        headers: {
          "Content-Type": "application/octet-stream",
          "X-Requested-With": "evergenie",
          "X-Filename": encodeURIComponent(path.basename(file)),
        },
        data: readFileSync(file),
      });
      expect(res.status()).toBe(200);
      return ((await res.json()) as { url: string }).url;
    },
    /** 寫一筆 closet-mine.request 並等程式處理完，回請求。 */
    async send(fields: Record<string, unknown>): Promise<Rec> {
      const id = String((await post("vault/next_id", { kind: "closet-mine.request" })).id);
      await put("closet-mine.request", id, { ...fields, state: "pending" });
      made.push({ kind: "closet-mine.request", id });
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
    /** 換圖請求寫出來的檔（新檔與舊檔的備份）收尾一起刪；file 是那張紀錄現在的 file（相對 personas/）。 */
    forgetReplaced(file: string, oldFile: string) {
      files.push(path.join(server.home, "personas", file));
      const dir = path.join(server.home, "personas", path.dirname(oldFile));
      const name = path.basename(oldFile);
      for (const f of readdirSync(dir)) {
        if (f.startsWith(`${name}.bak-`)) files.push(path.join(dir, f));
      }
    },
    /**
     * 打開隱藏版：一筆開著的隱藏版設定（closet.variant，on = true）＋「自己的隱藏版」開關（種子的隱藏版那張在 adult/ 底下，
     * 算自己的隱藏版）；等衣櫃這一輪的檢視不再把它列在 hidden_off。收尾封存設定、開關改回沒設。
     */
    async openHidden(s: Seed) {
      const v = `v${rand()}`;
      await put("closet.variant", v, { name: "隱藏版", dir: "avatars/hidden", on: true });
      made.push({ kind: "closet.variant", id: v });
      await setAdult("on");
      undo.push(() => setAdult(""));
      await expect
        .poll(
          async () =>
            ((await get("closet.person", "builder"))?.fields.hidden_off as string[] | undefined) ??
            [],
          { timeout: 30_000 },
        )
        .not.toContain(s.hidden);
    },
    /**
     * 讓 builder 身上只有這個測試造的東西算數（不依賴別的測試有沒有收乾淨）：別人留下還沒採用的照片先標採用、
     * 開發中的整套先改成上架；收尾改回原樣。
     */
    async quiet(s: Seed) {
      const ours = new Set([s.a, s.b]);
      for (const r of await list("closet.photo")) {
        if (r.fields.persona !== "builder" || ours.has(String(r.fields.outfit))) continue;
        if (r.fields.adopted === true) continue;
        await post("vault/patch", { kind: "closet.photo", id: r.id, set: { adopted: true } });
        undo.push(() =>
          post("vault/patch", {
            kind: "closet.photo",
            id: r.id,
            set: { adopted: r.fields.adopted ?? false },
          }),
        );
      }
      for (const r of await list("closet.outfit")) {
        if (r.fields.persona !== "builder" || ours.has(r.id) || r.fields.state !== "draft")
          continue;
        await post("vault/patch", { kind: "closet.outfit", id: r.id, set: { state: "active" } });
        undo.push(() =>
          post("vault/patch", { kind: "closet.outfit", id: r.id, set: { state: "draft" } }),
        );
      }
    },
    /** 造兩套衣服、放圖、寫照片紀錄，等衣櫃的程式把檢視算好（照片網址從檢視來）。 */
    async seed(): Promise<Seed> {
      const id = `x${rand()}`;
      const mineId = `my-${rand()}`;
      const a = `builder-${id}`;
      const b = `builder-${mineId}`;
      copy(IMG[0] as string, path.join(avatars, `builder@${id}.png`));
      copy(IMG[1] as string, path.join(avatars, `builder@${id}~2.png`));
      copy(IMG[2] as string, path.join(avatars, "full", `builder@${id}.png`));
      copy(path.join(fx, "persona.mp4"), path.join(avatars, `builder@${id}.mp4`));
      copy(IMG[2] as string, path.join(mineAvatars, `builder@${mineId}.png`));
      const outfit = (rec: string, o: string, f: Record<string, unknown>) =>
        put("closet.outfit", rec, { persona: "builder", outfit: o, cost: 0, ...f });
      await outfit(a, id, { name: `開發中${id}`, state: "draft", source: "official", order: 1 });
      await outfit(b, mineId, { name: `我的${mineId}`, state: "active", source: "mine", order: 2 });
      made.push({ kind: "closet.outfit", id: a }, { kind: "closet.outfit", id: b });
      const s: Seed = {
        a,
        b,
        half1: `${a}--half1`,
        half2: `${a}--half2`,
        full: `${a}--full1`,
        video: `${a}--video1-c1`,
        hidden: `${a}--half1-h`,
        mine: `${b}--half1`,
      };
      const photo = async (pid: string, f: Record<string, unknown>) => {
        await put("closet.photo", pid, {
          persona: "builder",
          outfit: a,
          type: "half",
          order: 1,
          hidden: false,
          source: "official",
          file: `avatars/builder@${id}.png`,
          adopted: false,
          ...f,
        });
        made.push({ kind: "closet.photo", id: pid });
      };
      await photo(s.half1, {});
      await photo(s.half2, { order: 2, file: `avatars/builder@${id}~2.png`, adopted: true });
      await photo(s.full, { type: "full", file: `avatars/full/builder@${id}.png`, adopted: true });
      await photo(s.video, {
        type: "video",
        of: s.half1,
        clip: 1,
        file: `avatars/builder@${id}.mp4`,
      });
      await photo(s.hidden, { hidden: true, file: `adult/builder@${id}.png` });
      await photo(s.mine, {
        outfit: b,
        source: "mine",
        file: `avatars/builder@${mineId}.png`,
        adopted: true,
      });
      // 檢視算好（兩套都在、開發中那套有兩張真圖與全身照、隱藏版那張因為開關關著列在 hidden_off）才看得到縮圖；
      // 也讓之後 openHidden 等「不在 hidden_off」時，等到的是衣櫃真的處理過開關，不是還沒算進這張的舊檢視。
      await expect
        .poll(
          async () => {
            const p = await get("closet.person", "builder");
            const outs = (p?.fields.outfits as Record<string, unknown>[] | undefined) ?? [];
            const oa = outs.find((o) => o.record === a);
            const ob = outs.find((o) => o.record === b);
            const off = (p?.fields.hidden_off as string[] | undefined) ?? [];
            return [
              ((oa?.photos as unknown[]) ?? []).length,
              !!oa?.full,
              !!ob,
              off.includes(s.hidden),
            ];
          },
          { timeout: 30_000 },
        )
        .toEqual([2, true, true, true]);
      return s;
    },
    /** 收掉這個測試造的：先刪圖（不然衣櫃會在封存與刪檔之間把還在的圖用新編號補登記回來），再封存衣服、照片紀錄與評論。 */
    async cleanup(extra: { kind: string; id: string }[] = []) {
      for (const f of files) rmSync(f, { force: true });
      for (const u of undo.splice(0).reverse()) await u().catch(() => undefined);
      for (const r of [...made, ...extra]) {
        const cur = await get(r.kind, r.id);
        if (cur && !cur.archived) await post("vault/archive", { kind: r.kind, id: r.id });
      }
      for (const n of await list("closet.note")) {
        if (made.some((m) => m.id === n.fields.record))
          await post("vault/archive", { kind: "closet.note", id: n.id });
      }
    },
  };
  return api;
}

const tabsOf = (page: Page) => page.getByTestId("page-tabs").getByRole("link");
const outfitBlock = (page: Page, record: string) =>
  page.locator(`[data-testid="edit-outfit"][data-record="${record}"]`);
const photoCard = (scope: Page | Locator, id: string) =>
  scope.locator(`[data-testid="edit-photo"][data-photo="${id}"]`);
const filterBtn = (page: Page, key: string) =>
  page.locator(`[data-testid="edit-filter"][data-filter="${key}"]`);

async function openEdit(page: Page) {
  await page.goto("/personas/builder");
  await expect(tabsOf(page).last()).toHaveText("修改", { timeout: 20_000 });
  await tabsOf(page).last().click();
  await expect(page).toHaveURL(/\/personas\/builder\?tab=closet\.edit$/);
  await expect(page.getByTestId("closet-edit")).toBeVisible();
}

test("TestEditTabHiddenWithoutEditCapability", async ({ page, request, baseURL }) => {
  // 正式家：衣櫃說這個家只有 mine，沒有 edit，核心不列「修改」分頁（頁面自己不判斷是哪種家）。
  await expect
    .poll(
      async () => {
        const r = await request.post(`${baseURL}/api/vault/get`, {
          data: { kind: "closet.capabilities", id: "main" },
        });
        return r.ok() ? ((await r.json()) as { record: Rec }).record.fields.supports : null;
      },
      { timeout: 20_000 },
    )
    .toEqual(["mine"]);
  await page.goto("/personas/builder");
  await expect(tabsOf(page).filter({ hasText: "衣服" })).toBeVisible({ timeout: 15_000 });
  await expect(tabsOf(page).filter({ hasText: "修改" })).toHaveCount(0);
  // 直接打網址也不會出現：分頁不在就拿掉 ?tab=，回到「介紹」。
  await page.goto("/personas/builder?tab=closet.edit");
  await expect(page).toHaveURL(/\/personas\/builder$/);
  await expect(page.getByTestId("closet-edit")).toHaveCount(0);
});

devTest.describe("開發家的「修改」分頁", () => {
  devTest("TestEditTabTogglesIndependent", async ({ page, request, baseURL, hgServer }) => {
    const c = client(request, baseURL ?? "", hgServer);
    const s = await c.seed();
    try {
      await openEdit(page);
      // 「修改」是最後一個分頁。
      await expect(tabsOf(page)).toHaveText(["介紹", "衣服", "修改"]);
      const a = outfitBlock(page, s.a);
      const b = outfitBlock(page, s.b);
      await expect(a).toBeVisible({ timeout: 20_000 });
      await expect(b).toBeVisible();
      // 塊頭：狀態與官方／我的；不顯示擁有、輪流穿。
      await expect(a.getByTestId("edit-outfit-state")).toHaveText("開發中");
      await expect(a.getByTestId("edit-outfit-source")).toHaveText("官方");
      await expect(b.getByTestId("edit-outfit-state")).toHaveText("上架中");
      await expect(b.getByTestId("edit-outfit-source")).toHaveText("我的");
      await expect(page.getByTestId("closet-edit")).not.toContainText("輪流穿");
      await expect(page.getByTestId("closet-edit")).not.toContainText("擁有");
      // 照片排序：半身（order）→ 全身 → 影片。隱藏版開關關著：隱藏版那張整張不列（不留框）、
      // 不說「還有幾張隱藏版沒列出來」，也沒有「顯示隱藏版」（S3a）。
      await expect(a.getByTestId("edit-photo")).toHaveCount(4);
      const ids = await a
        .getByTestId("edit-photo")
        .evaluateAll((els) => els.map((e) => e.getAttribute("data-photo")));
      expect(ids).toEqual([s.half1, s.half2, s.full, s.video]);
      await expect(a.getByTestId("edit-outfit-hidden-out")).toHaveCount(0);
      await expect(filterBtn(page, "hidden")).toHaveCount(0);
      await expect(page.getByTestId("closet-edit")).not.toContainText("顯示隱藏版");
      // 縮圖是衣櫃公開資料夾的網址；點了在這頁看大圖（不開新分頁，FEN-30）。
      await expect(photoCard(a, s.half2).getByTestId("edit-photo-img")).toHaveAttribute(
        "src",
        new RegExp(`/pkg-files/closet/o/builder/${s.a.slice("builder-".length)}/2\\.`),
      );
      await photoCard(a, s.half2).getByTestId("edit-photo-zoom").click();
      await expect(page.getByRole("dialog")).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(page.getByRole("dialog")).toHaveCount(0);

      // 切換鈕預設都關，各自獨立開關。
      for (const k of ["official", "wip"]) {
        await expect(filterBtn(page, k)).toHaveAttribute("aria-pressed", "false");
      }
      // 官方：只看官方的衣服。
      await filterBtn(page, "official").click();
      await expect(page).toHaveURL(/item=official/);
      await expect(b).toHaveCount(0);
      await expect(a).toBeVisible();
      await expect(filterBtn(page, "wip")).toHaveAttribute("aria-pressed", "false");
      // ＋開發中：只看沒採用的照片。
      await filterBtn(page, "wip").click();
      await expect(a.getByTestId("edit-photo")).toHaveCount(2);
      await expect(photoCard(a, s.half1)).toBeVisible();
      await expect(photoCard(a, s.video)).toBeVisible();
      // 關掉官方、開發中照開：我的那套每張都採用了，整套不列。
      await filterBtn(page, "official").click();
      await expect(filterBtn(page, "official")).toHaveAttribute("aria-pressed", "false");
      await expect(filterBtn(page, "wip")).toHaveAttribute("aria-pressed", "true");
      await expect(b).toHaveCount(0);
      // 隱藏版開關打開：「顯示隱藏版」出現（預設關），塊裡說還有一張沒列出來。
      await c.openHidden(s);
      await page.reload();
      await expect(filterBtn(page, "hidden")).toHaveAttribute("aria-pressed", "false", {
        timeout: 20_000,
      });
      await expect(a.getByTestId("edit-outfit-hidden-out")).toBeVisible();
      // 顯示隱藏版：跟開發中一起成立（交集），隱藏版那張也是沒採用的。
      await filterBtn(page, "hidden").click();
      await expect(a.getByTestId("edit-photo")).toHaveCount(3);
      await expect(photoCard(a, s.hidden)).toBeVisible();
      // 開關開著、但對不到網址的那張（檔不在）照舊顯示佔位（類型＋檔名），不會壞掉。
      await expect(photoCard(a, s.hidden).getByTestId("edit-photo-placeholder")).toContainText(
        "半身",
      );
      // 篩選在網址裡：重新整理還在。
      await expect(page).toHaveURL(/item=wip%2Chidden|item=wip,hidden/);
      await page.reload();
      await expect(filterBtn(page, "wip")).toHaveAttribute("aria-pressed", "true");
      await expect(filterBtn(page, "hidden")).toHaveAttribute("aria-pressed", "true");
      await expect(filterBtn(page, "official")).toHaveAttribute("aria-pressed", "false");
      await expect(outfitBlock(page, s.a).getByTestId("edit-photo")).toHaveCount(3);
      // 都關回去：兩套都在。
      await filterBtn(page, "wip").click();
      await filterBtn(page, "hidden").click();
      await expect(outfitBlock(page, s.b)).toBeVisible();
      await expect(page).toHaveURL(/\/personas\/builder\?tab=closet\.edit$/);

      // 所有角色：一位一段。
      await page.getByRole("button", { name: "所有角色" }).click();
      await expect(
        page.locator('[data-testid="edit-persona"][data-persona="builder"]'),
      ).toBeVisible();
      await expect(page).toHaveURL(/item=all/);
    } finally {
      await c.cleanup();
    }
  });

  devTest("TestEditPhotoStatesChangeRecords", async ({ page, request, baseURL, hgServer }) => {
    const c = client(request, baseURL ?? "", hgServer);
    const s = await c.seed();
    const field = async (id: string, k: string) => (await c.get("closet.photo", id))?.fields[k];
    try {
      // 這個測試要改隱藏版：先打開隱藏版開關（關著時隱藏版不列，S3a）。
      await c.openHidden(s);
      await openEdit(page);
      const a = outfitBlock(page, s.a);
      await expect(a).toBeVisible({ timeout: 20_000 });
      // 開發中的那套還有沒採用的：「上架」按不下去。
      const toggle = a.getByTestId("edit-outfit-state-toggle");
      await expect(toggle).toHaveText("上架");
      await expect(toggle).toBeDisabled();
      await expect(a.getByTestId("edit-outfit-publish-need")).toContainText("還有 3 張沒採用");

      // 採用／取消採用：改 closet.photo 的 adopted。
      const h1 = photoCard(a, s.half1);
      await h1.getByTestId("edit-photo-adopt").click();
      await expect.poll(() => field(s.half1, "adopted")).toBe(true);
      await expect(h1).toHaveAttribute("data-adopted", "true");
      await expect(h1.getByTestId("edit-photo-adopt")).toHaveText("取消採用");
      await h1.getByTestId("edit-photo-adopt").click();
      await expect.poll(() => field(s.half1, "adopted")).toBe(false);
      await expect(h1.getByTestId("edit-photo-adopt")).toHaveText("採用");

      // 一般／隱藏：只改 hidden（不搬檔）；「顯示隱藏版」沒打開時那張就不列。
      const h2 = photoCard(a, s.half2);
      await h2.getByRole("button", { name: "隱藏版", exact: true }).click();
      await expect.poll(() => field(s.half2, "hidden")).toBe(true);
      await expect(h2).toHaveCount(0);
      await filterBtn(page, "hidden").click();
      await expect(photoCard(a, s.half2)).toHaveAttribute("data-hidden", "true");
      // 自己的隱藏版（這張的檔在 adult/ 底下，衣櫃算進 owner_hidden）：不給改成一般版，只寫一行說明。
      await expect(photoCard(a, s.hidden).getByTestId("edit-photo-owner-hidden")).toBeVisible({
        timeout: 20_000,
      });
      await expect(
        photoCard(a, s.hidden).getByRole("button", { name: "一般版", exact: true }),
      ).toHaveCount(0);
      // 自己的一般版照片：先說改成隱藏版後這頁就改不回來。
      await expect(
        photoCard(outfitBlock(page, s.b), s.mine).getByTestId("edit-photo-owner-hint"),
      ).toBeVisible();
      // 影片跟配的半身不同版（上次寫到一半）：影片卡給「跟上半身」，按了影片回到半身那一版。
      await c.post("vault/patch", {
        kind: "closet.photo",
        id: s.video,
        set: { hidden: true },
        unset: [],
      });
      const clip = photoCard(a, s.video);
      await clip.getByTestId("edit-photo-level-sync").click();
      await expect.poll(() => field(s.video, "hidden")).toBe(false);
      await expect(clip.getByTestId("edit-photo-level-sync")).toHaveCount(0);
      await expect(clip.getByTestId("edit-photo-level-follows")).toBeVisible();
      await photoCard(a, s.half2).getByRole("button", { name: "一般版", exact: true }).click();
      await expect.poll(() => field(s.half2, "hidden")).toBe(false);
      // 改回一般版：衣櫃下一輪重新發佈後，照編號（closet.person 的 photo_urls）又對得到圖，不是佔位。
      await expect(photoCard(a, s.half2).getByTestId("edit-photo-img")).toHaveAttribute(
        "src",
        new RegExp(`/pkg-files/closet/o/builder/${s.a.slice("builder-".length)}/2\\.`),
        { timeout: 20_000 },
      );

      // 類型：一般版已經有全身照時，半身不給改成全身（一版只公開一張，改了就永遠是佔位）。
      const pub = (k: "o" | "f", n: number) =>
        new RegExp(`/pkg-files/closet/${k}/builder/${s.a.slice("builder-".length)}/${n}\\.`);
      await expect(photoCard(a, s.half2).getByTestId("edit-photo-full-taken")).toBeVisible();
      await expect(
        photoCard(a, s.half2).getByRole("button", { name: "全身", exact: true }),
      ).toHaveCount(0);
      // 先把原本的全身照改成半身（放到半身最後一張），再把第 2 張半身改成全身；衣櫃下一輪重新發佈後兩張都對得到圖。
      await photoCard(a, s.full).getByRole("button", { name: "半身", exact: true }).click();
      await expect.poll(() => field(s.full, "type")).toBe("half");
      expect(await field(s.full, "order")).toBe(3);
      await photoCard(a, s.half2).getByRole("button", { name: "全身", exact: true }).click();
      await expect.poll(() => field(s.half2, "type")).toBe("full");
      expect(await field(s.half2, "order")).toBe(1);
      await expect(photoCard(a, s.half2).getByTestId("edit-photo-img")).toHaveAttribute(
        "src",
        pub("f", 1),
        { timeout: 20_000 },
      );
      await expect(photoCard(a, s.full).getByTestId("edit-photo-img")).toHaveAttribute(
        "src",
        pub("o", 3),
        { timeout: 20_000 },
      );
      // 影片不能改類型。
      await expect(
        photoCard(a, s.video).getByRole("button", { name: "全身", exact: true }),
      ).toHaveCount(0);
      // 配了影片的半身（half1 有一支影片）也不能改成全身：只寫一行說明，沒有切換鈕。
      await expect(photoCard(a, s.half1).getByTestId("edit-photo-paired")).toBeVisible();
      await expect(
        photoCard(a, s.half1).getByRole("button", { name: "全身", exact: true }),
      ).toHaveCount(0);

      // 每張都採用了才能上架：採用剩下的，按「上架」→ state 改 active；再按「下架」→ retired。
      for (const id of [s.half1, s.video, s.hidden]) {
        await photoCard(a, id).getByTestId("edit-photo-adopt").click();
        await expect.poll(() => field(id, "adopted")).toBe(true);
      }
      await expect(toggle).toBeEnabled();
      await toggle.click();
      await expect
        .poll(async () => (await c.get("closet.outfit", s.a))?.fields.state)
        .toBe("active");
      await expect(a.getByTestId("edit-outfit-state")).toHaveText("上架中");
      await expect(toggle).toHaveText("下架");
      await toggle.click();
      await expect
        .poll(async () => (await c.get("closet.outfit", s.a))?.fields.state)
        .toBe("retired");
      await expect(a.getByTestId("edit-outfit-state")).toHaveText("已下架");
      // 官方的衣服沒有「放回開發中」（狀態跟著角色包）。
      await expect(a.getByTestId("edit-outfit-to-draft")).toHaveCount(0);

      // 放回開發中（DEV-160 審查 S1）：自己的衣服（不是基本款）多一顆，按了 state = draft，照片的採用不動；
      // 之後照片都採用了，「上架」又按得下去。
      const b = outfitBlock(page, s.b);
      await expect(b.getByTestId("edit-outfit-state")).toHaveText("上架中");
      await b.getByTestId("edit-outfit-to-draft").click();
      await expect
        .poll(async () => (await c.get("closet.outfit", s.b))?.fields.state)
        .toBe("draft");
      await expect(b.getByTestId("edit-outfit-state")).toHaveText("開發中");
      await expect(b.getByTestId("edit-outfit-to-draft")).toHaveCount(0);
      expect(await field(s.mine, "adopted")).toBe(true);
      const bToggle = b.getByTestId("edit-outfit-state-toggle");
      await expect(bToggle).toHaveText("上架");
      await expect(bToggle).toBeEnabled();
      await bToggle.click();
      await expect
        .poll(async () => (await c.get("closet.outfit", s.b))?.fields.state)
        .toBe("active");
      await expect(b.getByTestId("edit-outfit-to-draft")).toBeVisible();
    } finally {
      await c.cleanup();
    }
  });

  // 展示間到「修改」的入口（CLO-37）：開發家的展示間沒有「設計中」區塊，那位角色的一段裡只有一行「N 張照片還沒採用」連到「修改」分頁；
  // N 跟「修改」分頁打開「開發中」看到的一樣（都讀 closet.photo 的 adopted，CLO-34、CLO-35）。
  // 種子：開發中那套的第 1 張半身、它的影片、隱藏版那張還沒採用；隱藏版開關關著，隱藏版那張「修改」分頁不列，入口也不算（S3a）＝2 張，
  // 連結只打開「開發中」。在「修改」採用一張，展示間跟著少一張；全部採用了（隱藏版那張也是）就不寫張數，但那套還是開發中（等上架），
  // 入口照樣在、寫「1 套等上架」（CLO-38），連過去不打開「開發中」（它只列沒採用的，等上架的那套會被篩掉）；按了上架整行不見。
  // 不依賴別的測試有沒有收乾淨：builder 身上別人留下的先擺平（quiet），所以張數寫死。
  devTest("TestEditShowroomLinkCountsUnadopted", async ({ page, request, baseURL, hgServer }) => {
    const c = client(request, baseURL ?? "", hgServer);
    const s = await c.seed();
    const toEdit = page.getByTestId("showroom-to-edit");
    try {
      await c.quiet(s);
      await page.goto("/p/closet/showroom?persona=builder");
      await expect(page.getByTestId("showroom")).toHaveAttribute("data-edit", "true", {
        timeout: 20_000,
      });
      await expect(toEdit).toHaveAttribute("data-count", "2", { timeout: 20_000 });
      await expect(toEdit).toHaveAttribute("data-ready", "0");
      await expect(toEdit).toContainText("2 張照片還沒採用");
      await expect(toEdit).toHaveAttribute("href", "/personas/builder?tab=closet.edit&item=wip");
      // 舊的「看角色｜設計中」切換沒有了。
      await expect(page.getByRole("button", { name: /設計中/ })).toHaveCount(0);

      // 點進去：「修改」分頁、開發中打開，看到的就是那 2 張；沒有「顯示隱藏版」。
      await toEdit.click();
      await expect(page).toHaveURL(/\/personas\/builder\?tab=closet\.edit&item=wip$/);
      await expect(filterBtn(page, "wip")).toHaveAttribute("aria-pressed", "true");
      await expect(filterBtn(page, "hidden")).toHaveCount(0);
      const a = outfitBlock(page, s.a);
      await expect(a.getByTestId("edit-photo")).toHaveCount(2, { timeout: 20_000 });
      // 隱藏版那張也沒採用：上架的提示說其中 1 張是開關關著的隱藏版。
      await filterBtn(page, "wip").click();
      await expect(a.getByTestId("edit-outfit-publish-need")).toContainText("還有 3 張沒採用");
      await expect(a.getByTestId("edit-outfit-publish-need")).toContainText("其中 1 張是隱藏版");
      // 在這裡採用一張：展示間跟著少一張。
      await photoCard(a, s.half1).getByTestId("edit-photo-adopt").click();
      await expect
        .poll(async () => (await c.get("closet.photo", s.half1))?.fields.adopted)
        .toBe(true);
      await page.goto("/p/closet/showroom?persona=builder");
      await expect(toEdit).toHaveAttribute("data-count", "1", { timeout: 20_000 });
      // 其餘的也採用了：沒有沒採用的照片；那套照片都採用了、還沒上架，「1 套等上架」（展示間每幾秒重讀）。
      for (const id of [s.video, s.hidden]) {
        await c.post("vault/patch", { kind: "closet.photo", id, set: { adopted: true } });
      }
      await expect(toEdit).toHaveAttribute("data-count", "0", { timeout: 20_000 });
      await expect(toEdit).toHaveAttribute("data-ready", "1");
      await expect(toEdit).toContainText("1 套等上架");
      await expect(toEdit).not.toContainText("張照片還沒採用");
      await expect(toEdit).toHaveAttribute("href", "/personas/builder?tab=closet.edit");
      // 上架了：不再等上架，沒有別的，整行不見。
      await c.post("vault/patch", { kind: "closet.outfit", id: s.a, set: { state: "active" } });
      await expect(toEdit).toHaveCount(0, { timeout: 20_000 });
      await expect(page.getByTestId("person-section")).toBeVisible();
    } finally {
      await c.cleanup();
    }
  });

  devTest("TestEditCommentWritesNote", async ({ page, request, baseURL, hgServer }) => {
    const c = client(request, baseURL ?? "", hgServer);
    const s = await c.seed();
    const notesOf = async (record: string) =>
      (await c.list("closet.note")).filter((n) => n.fields.record === record);
    try {
      await openEdit(page);
      const a = outfitBlock(page, s.a);
      await expect(a).toBeVisible({ timeout: 20_000 });
      // 單張的評論：closet.note 的 scope = photo、photo＝照片編號；評論本身不改採用。
      const h2 = photoCard(a, s.half2);
      await h2.getByTestId("edit-photo-comment").click();
      await h2.getByTestId("edit-photo-comment-text").fill("臉再柔和一點");
      await h2.getByTestId("edit-photo-comment-send").click();
      await expect.poll(async () => (await notesOf(s.a)).length).toBe(1);
      const [note] = await notesOf(s.a);
      expect(note?.fields).toMatchObject({
        scope: "photo",
        photo: s.half2,
        persona: "builder",
        outfit: s.a.slice("builder-".length),
        record: s.a,
        piece: "half-2",
        type: "half",
        comment: "臉再柔和一點",
        state: "open",
        intent: "comment",
      });
      expect((await c.get("closet.photo", s.half2))?.fields.adopted).toBe(true);
      // 評論紀錄：這張底下看得到，處理狀態「等秘書處理」。
      const history = h2.getByTestId("edit-photo-notes");
      await expect(history.getByTestId("edit-note")).toHaveCount(1);
      await expect(history).toContainText("臉再柔和一點");
      await expect(history).toContainText("等秘書處理");

      // 整套的評論：scope = outfit，piece、type 照固定寫法，不寫 photo。
      await a.getByTestId("edit-outfit-comment").click();
      await a.getByTestId("edit-outfit-comment-text").fill("整套換成藍色系");
      await a.getByTestId("edit-outfit-comment-send").click();
      await expect.poll(async () => (await notesOf(s.a)).length).toBe(2);
      const outfitNote = (await notesOf(s.a)).find((n) => n.fields.scope === "outfit");
      expect(outfitNote?.fields).toMatchObject({
        piece: "outfit",
        type: "half",
        intent: "comment",
        state: "open",
      });
      expect(outfitNote?.fields.photo).toBeUndefined();
      await expect(a.getByTestId("edit-outfit-notes")).toContainText("整套換成藍色系");

      // 秘書照 README 做：送換圖請求（closet-mine.request 帶 photo_id）換了新圖 → 系統自動把那張改回還沒採用（M4，
      // 官方的照片在開發家也走這條，CMI-25），秘書回填評論的 state 與 result；頁面跟著顯示。
      const before = await c.get("closet.photo", s.half2);
      const replaced = await c.send({
        type: "photo",
        persona: "builder",
        outfit: s.a.slice("builder-".length),
        photo_id: s.half2,
        upload: await c.upload(IMG[0] as string),
      });
      const after = await c.get("closet.photo", s.half2);
      c.forgetReplaced(String(after?.fields.file), String(before?.fields.file));
      expect(replaced.fields.state, JSON.stringify(replaced.fields)).toBe("done");
      expect(after?.fields.adopted).toBe(false);
      expect(after?.fields.file).not.toBe(before?.fields.file);
      await c.post("vault/patch", {
        kind: "closet.note",
        id: note?.id,
        set: { state: "redone", result: "換好了，臉比較柔和" },
      });
      await expect(history).toContainText("已重做", { timeout: 15_000 });
      await expect(history.getByTestId("edit-note-result")).toContainText("換好了，臉比較柔和");
      await expect(h2).toHaveAttribute("data-adopted", "false");
    } finally {
      await c.cleanup();
    }
  });

  devTest("TestEditPluginActionAndFilter", async ({ page, request, baseURL, hgServer }) => {
    const c = client(request, baseURL ?? "", hgServer);
    const s = await c.seed();
    const action = `demo-mark${rand()}`;
    const marks: { kind: string; id: string }[] = [{ kind: "closet.action", id: action }];
    try {
      // 使用者自己的套件（demo）登記一個單張的動作：按下去寫 demo.mark，狀態也看它；有篩選鈕。
      await c.put("closet.action", action, {
        package: "demo",
        label: "示範記號",
        scope: "photo",
        needs: "edit",
        request_kind: "demo.mark",
        request_fields: ["persona", "outfit", "photo"],
        request_set: { state: "wanted" },
        status_kind: "demo.mark",
        status_field: "state",
        status_labels: { wanted: "要了" },
        filter_label: "示範篩選",
        filter_values: ["wanted"],
      });
      await expect
        .poll(
          async () =>
            (
              ((await c.get("closet.capabilities", "main"))?.fields.actions as
                | { id: string }[]
                | undefined) ?? []
            ).some((x) => x.id === action),
          { timeout: 20_000 },
        )
        .toBe(true);
      await openEdit(page);
      const a = outfitBlock(page, s.a);
      await expect(a).toBeVisible({ timeout: 20_000 });
      const btn = photoCard(a, s.half2).locator(
        `[data-testid="edit-action"][data-action="${action}"]`,
      );
      await expect(btn).toHaveText("示範記號");
      await expect(filterBtn(page, `a:${action}`)).toHaveText("示範篩選");
      await expect(filterBtn(page, `a:${action}`)).toHaveAttribute("aria-pressed", "false");

      // 按下去：寫一筆 demo.mark（照登記的欄位），狀態字跟著出現。
      await btn.click();
      await expect
        .poll(
          async () => (await c.list("demo.mark")).filter((m) => m.fields.photo === s.half2).length,
        )
        .toBe(1);
      const [mark] = (await c.list("demo.mark")).filter((m) => m.fields.photo === s.half2);
      marks.push({ kind: "demo.mark", id: mark?.id ?? "" });
      expect(mark?.fields).toMatchObject({
        persona: "builder",
        outfit: s.a,
        photo: s.half2,
        state: "wanted",
      });
      await expect(
        photoCard(a, s.half2).locator(
          `[data-testid="edit-action-status"][data-action="${action}"]`,
        ),
      ).toHaveText("要了", { timeout: 15_000 });

      // 外掛篩選鈕：只留狀態是 wanted 的那張；沒有符合的那套（我的）不列。
      await filterBtn(page, `a:${action}`).click();
      await expect(a.getByTestId("edit-photo")).toHaveCount(1);
      await expect(photoCard(a, s.half2)).toBeVisible();
      await expect(outfitBlock(page, s.b)).toHaveCount(0);
      // 跟「開發中」交集：那張已採用，沒有符合的。
      await filterBtn(page, "wip").click();
      await expect(a).toHaveCount(0);
    } finally {
      await c.cleanup(marks);
    }
  });

  devTest("TestEditMobileFilterRowScrolls", async ({ page, request, baseURL, hgServer }) => {
    const c = client(request, baseURL ?? "", hgServer);
    const s = await c.seed();
    // 再登記三個名字很長的外掛篩選，手機上一排一定放不下。
    const ids = [1, 2, 3].map((n) => `demo-long${n}${rand()}`);
    const extra = ids.map((id) => ({ kind: "closet.action", id }));
    try {
      for (const [i, id] of ids.entries()) {
        await c.put("closet.action", id, {
          package: "demo",
          label: `長名字的動作${i + 1}`,
          scope: "photo",
          needs: "edit",
          request_kind: "demo.mark",
          request_fields: ["persona", "outfit", "photo"],
          status_kind: "demo.mark",
          status_field: "state",
          status_labels: { wanted: "要了" },
          filter_label: `很長很長的篩選名字${i + 1}`,
        });
      }
      for (const width of [390, 320]) {
        await page.setViewportSize({ width, height: 844 });
        await openEdit(page);
        await expect(outfitBlock(page, s.a)).toBeVisible({ timeout: 20_000 });
        // 官方、開發中＋三個外掛（隱藏版開關關著，沒有「顯示隱藏版」）。
        await expect(page.getByTestId("edit-filter")).toHaveCount(5, { timeout: 20_000 });
        // 切換鈕一排：不折成兩行，放不下就橫向捲、右邊淡出提示還有；按鈕夠大（至少 44 高）。
        const tops = await page
          .getByTestId("edit-filter")
          .evaluateAll((els) => els.map((e) => Math.round(e.getBoundingClientRect().top)));
        expect(new Set(tops).size).toBe(1);
        const heights = await page
          .getByTestId("edit-filter")
          .evaluateAll((els) => els.map((e) => e.getBoundingClientRect().height));
        for (const h of heights) expect(h).toBeGreaterThanOrEqual(44);
        const row = page.getByTestId("edit-filter-row");
        expect(await row.evaluate((el) => el.scrollWidth > el.clientWidth)).toBe(true);
        await expect(row).toHaveAttribute("data-more", "");
        // 捲到最右邊：最後一顆看得到、淡出拿掉。
        await row.evaluate((el) => {
          el.scrollLeft = el.scrollWidth;
        });
        await expect(row).not.toHaveAttribute("data-more", "");
        await expect(page.getByTestId("edit-filter").last()).toBeInViewport();
        // 整頁不橫向捲（看板是內層 main 捲動，兩個都量）。
        const overflow = await page.evaluate(() =>
          [document.documentElement, document.querySelector("main")].map((el) =>
            el ? el.scrollWidth - el.clientWidth : 0,
          ),
        );
        expect(overflow).toEqual([0, 0]);
      }
    } finally {
      await c.cleanup(extra);
    }
  });

  // 截圖：手機 390 與桌機 1280、亮色與深色（make shots，或直接 --grep @shots；存到 $EVERGENIE_SHOTS_DIR）。
  devTest("TestEditTabShots @shots", async ({ page, request, baseURL, hgServer }) => {
    const c = client(request, baseURL ?? "", hgServer);
    const s = await c.seed();
    try {
      for (const [w, h, tag] of [
        [390, 844, "mobile"],
        [1280, 900, "desktop"],
      ] as const) {
        for (const scheme of ["light", "dark"] as const) {
          await page.setViewportSize({ width: w, height: h });
          await page.emulateMedia({ colorScheme: scheme });
          await openEdit(page);
          await expect(outfitBlock(page, s.a).getByTestId("edit-photo-img").first()).toBeVisible({
            timeout: 20_000,
          });
          await page.waitForTimeout(300);
          await shot(page, `closet-edit-${tag}-${scheme}`);
        }
      }
      // 整頁（手機寬、亮色）：隱藏版開關打開、顯示隱藏版、寫一則評論，看得到佔位、評論紀錄與「我的」那套。
      await page.setViewportSize({ width: 390, height: 844 });
      await page.emulateMedia({ colorScheme: "light" });
      await c.openHidden(s);
      await page.reload();
      await filterBtn(page, "hidden").click({ timeout: 20_000 });
      const h1 = photoCard(outfitBlock(page, s.a), s.half1);
      await h1.getByTestId("edit-photo-comment").click();
      await h1.getByTestId("edit-photo-comment-text").fill("裙子短一點");
      await h1.getByTestId("edit-photo-comment-send").click();
      await expect(h1.getByTestId("edit-note")).toHaveCount(1);
      // 看板是內層捲動，fullPage 照不到下面：把視窗拉高一次照完。
      await page.setViewportSize({ width: 390, height: 3200 });
      await page.waitForTimeout(300);
      await shot(page, "closet-edit-mobile-full");
    } finally {
      await c.cleanup();
    }
  });

  // 沒有生圖秘書上班時（CLO-23）：開發家的 art 是選用的，測試家沒有（開發工坊 admin 沒開）。打開「評論」馬上看到白話說明與
  // 到「職位與秘書」的連結，送出後評論照樣記下、不顯示「請跟她說一聲」；名冊上有一位啟用中的 art 之後不再說明、直接傳給她。
  // 這裡的 art 用 agents.create 直接建（像主人自己建的職位），不走認領。排在最後：收尾只停用、不會消失，
  // 之後同一個 worker 的家裡 art 是「停用中」（data-art="off"），要「家裡沒有 art」的測試請排在它前面。
  devTest(
    "TestEditCommentWithoutArtExplains",
    async ({ page, request, baseURL, hgServer, seed }) => {
      const c = client(request, baseURL ?? "", hgServer);
      const s = await c.seed();
      const comments = async () =>
        (await c.list("closet.note"))
          .filter((n) => n.fields.record === s.a)
          .map((n) => n.fields.comment)
          .sort();
      try {
        await openEdit(page);
        const h1 = photoCard(outfitBlock(page, s.a), s.half1);
        await expect(h1).toBeVisible({ timeout: 20_000 });
        await h1.getByTestId("edit-photo-comment").click();
        const hint = h1.getByTestId("edit-no-art");
        await expect(hint).toContainText("這個家沒有啟用中的生圖秘書");
        await expect(hint).toHaveAttribute("data-art", /^(none|off)$/);
        await expect(hint.getByRole("link")).toHaveAttribute("href", "/settings/agents");
        await h1.getByTestId("edit-photo-comment-text").fill("臉再小一點");
        await h1.getByTestId("edit-photo-comment-send").click();
        await expect.poll(comments).toEqual(["臉再小一點"]);
        await expect(hint).toBeVisible();
        await expect(h1.getByRole("alert")).toHaveCount(0);

        // 名冊上有一位啟用中的 art。
        await seed.api("/api/agents/create", {
          id: "art",
          persona: "helper",
          duty: "生圖",
          by: { name: "owner(web)" },
        });
        await seed.api("/api/agents/set_field", {
          id: "art",
          field: "enabled",
          value: "true",
          by: { name: "owner(web)" },
        });

        // 有生圖秘書了：再評論不再說明，送出傳得到（沒有任何提醒）。
        await openEdit(page);
        await expect(h1).toBeVisible({ timeout: 20_000 });
        await h1.getByTestId("edit-photo-comment").click();
        await h1.getByTestId("edit-photo-comment-text").fill("再亮一點");
        await h1.getByTestId("edit-photo-comment-send").click();
        await expect.poll(comments).toEqual(["再亮一點", "臉再小一點"].sort());
        await expect(h1.getByTestId("edit-note")).toHaveCount(2);
        await expect(h1.getByTestId("edit-no-art")).toHaveCount(0);
        await expect(h1.getByRole("alert")).toHaveCount(0);
      } finally {
        // 收尾：art 停用（同一個 worker 的別的測試看的是原本的名冊），衣服、照片與評論收掉。
        await seed
          .api("/api/agents/set_field", {
            id: "art",
            field: "enabled",
            value: "false",
            by: { name: "owner(web)" },
          })
          .catch(() => undefined);
        await c.cleanup();
      }
    },
  );
});
