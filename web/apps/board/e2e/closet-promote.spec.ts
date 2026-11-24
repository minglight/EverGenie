import { copyFileSync, existsSync, readFileSync, rmSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { APIRequestContext } from "@playwright/test";
import { expect, devTest as test } from "./fixtures";

// 衣服的「升成官方（公開給所有人）」按鈕（DEV-157，CMI-16；程式是 CMI-14）：只在開發家出現，所以這支用開發家（devTest，
// home.toml [runtime] env = "dev"，自己一個 worker）。DEV-160 審查 S1 起在角色頁「修改」分頁那一套的塊裡（「衣服」分頁開發家與一般家一樣，
// 沒有這顆，closet-mine.spec）。流程：官方角色 helper 身上加一套自己的衣服 → 角色頁「修改」分頁那一套的塊 → 按「升成官方」→ 改新編號
// → 預演（什麼都沒動）→ 看白話說明與「收不回」→ 確定 → 變成官方的那筆、圖在官方的位置。素材是 e2e/fixtures/closet/ 的測試圖樣，不是真的角色。

const here = path.dirname(fileURLToPath(import.meta.url));
const IMG = ["outfit-a.png", "outfit-b.png"].map((n) => path.join(here, "fixtures", "closet", n));
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

const EDIT_TAB = (persona: string) => `/personas/${persona}?tab=closet.edit`;

function client(request: APIRequestContext, base: string) {
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
  /** addOutfit 加的那張照片的照片紀錄編號：<衣服編號>--u<請求編號>（DEV-160 S3，檔在 personas/mine/photos/<照片編號>.png）。 */
  const photos = new Map<string, string>();
  const photoOf = (record: string): string => photos.get(record) ?? "";
  /** 在官方角色（預設 helper）身上加一套自己的衣服（預設免費、直接擁有；cost 給 1 以上就要兌換），回 closet.outfit 的編號（helper-my-N）。 */
  const addOutfit = async (
    name: string,
    img: number,
    cost = 0,
    persona = "helper",
  ): Promise<string> => {
    const up = await request.post(`${base}/api/web/upload`, {
      headers: {
        "Content-Type": "application/octet-stream",
        "X-Requested-With": "evergenie",
        "X-Filename": encodeURIComponent(path.basename(IMG[img] as string)),
      },
      data: readFileSync(IMG[img] as string),
    });
    expect(up.status()).toBe(200);
    const id = String((await post("vault/next_id", { kind: "closet-mine.request" })).id);
    await post("vault/put", {
      record: {
        kind: "closet-mine.request",
        id,
        fields: {
          type: "outfit",
          upload: ((await up.json()) as { url: string }).url,
          persona,
          name,
          cost,
          state: "pending",
        },
      },
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
      .toBe("done");
    const result = String((rec as unknown as Rec).fields.result);
    photos.set(result, `${result}--u${id.replace(/^U-/, "")}`);
    return result;
  };
  return { post, get, addOutfit, photoOf };
}

// Guards: CMI-16, CMI-14
test("TestPromoteOutfitFromEditTab @smoke", async ({ page, request, baseURL, hgServer }) => {
  const base = baseURL ?? "";
  const api = client(request, base);
  // 這個家真的是開發家。
  expect(((await api.post("system/get_mode", {})) as { env?: string }).env).toBe("dev");
  const newId = `sea${rand()}`;
  const official = path.join(hgServer.home, "personas", "avatars");
  const first = await api.addOutfit("海邊的衣服", 0);
  const second = await api.addOutfit("山上的衣服", 1);
  const oldId = first.replace(/^helper-/, "");
  expect(oldId).toMatch(/^my-/);
  // 新上傳的檔名是照片編號，升成官方搬到官方的 avatars/photos/（檔名不改）；照片紀錄換到新的衣服編號。
  const oldPhoto = api.photoOf(first);
  const newPhoto = oldPhoto.replace(first, `helper-${newId}`);
  const officialImg = path.join(official, "photos", `${oldPhoto}.png`);
  try {
    await page.goto(EDIT_TAB("helper"));
    const block = page.locator(`[data-testid="edit-outfit"][data-record="${first}"]`);
    const open = block.getByTestId("edit-promote-open");
    await expect(open).toHaveText("升成官方（公開給所有人）", { timeout: 30_000 });
    await open.click();
    const box = page.locator(`[data-testid="edit-promote"][data-record="${first}"]`);
    const input = box.getByTestId("edit-promote-id");
    // 建議值：不是 my- 開頭、可以直接用；主人可以改。
    await expect(input).not.toHaveValue(/^my-/);
    await expect(input).not.toHaveValue("");
    // 不合格的新編號：頁面先說，不送。
    await input.fill("my-1");
    await box.getByTestId("edit-promote-preview-btn").click();
    await expect(box.getByTestId("edit-promote-error")).toContainText("不能用 my- 開頭");
    await input.fill(newId);
    await box.getByTestId("edit-promote-preview-btn").click();
    // 預演的結果用白話說：變成什麼編號、搬幾個檔、舊的怎麼處理；寫明收不回。
    const preview = box.getByTestId("edit-promote-preview");
    await expect(preview).toBeVisible({ timeout: 30_000 });
    await expect(preview).toContainText(`編號 helper-${newId}`);
    await expect(preview).toContainText("會把 1 個檔");
    await expect(preview).toContainText("舊的怎麼處理");
    await expect(box.getByTestId("edit-promote-warn")).toHaveText(
      "升成官方＝可以公開給所有人：下一次建角色包時會帶出去（這位角色要在公開名單裡），收不回。",
    );
    await expect(box.getByTestId("edit-promote-detail")).toContainText("預演（什麼都沒動）");
    // 預演什麼都沒動：還是自己的衣服，官方位置沒有圖。
    expect((await api.get("closet.outfit", first))?.fields.source).toBe("mine");
    expect(await api.get("closet.outfit", `helper-${newId}`)).toBeNull();
    expect(existsSync(officialImg)).toBe(false);
    // 改了新編號：那次預演不算，要再預演才有「確定」。
    await input.fill(`${newId}x`);
    await expect(box.getByTestId("edit-promote-confirm")).toHaveCount(0);
    await input.fill(newId);
    await box.getByTestId("edit-promote-confirm").click();
    const done = page.getByTestId("edit-promote-done");
    await expect(done).toContainText(`已經升成官方了，編號 helper-${newId}`, { timeout: 30_000 });
    // 真的升了：新的那筆是官方的、舊的封存、圖在官方的位置（photos/ 的檔搬到 avatars/photos/、檔名不改，照片紀錄跟著換到新編號，DEV-160）。
    await expect
      .poll(async () => (await api.get("closet.outfit", `helper-${newId}`))?.fields.source)
      .toBe("official");
    expect(readFileSync(officialImg).equals(readFileSync(IMG[0] as string))).toBe(true);
    const photo = await api.get("closet.photo", newPhoto);
    expect(photo?.fields).toMatchObject({
      outfit: `helper-${newId}`,
      source: "official",
      file: `avatars/photos/${oldPhoto}.png`,
    });
    // 那一塊換成官方的那筆（官方的衣服沒有「升成官方」），結果那行留在分頁最上面。
    const promoted = page.locator(`[data-testid="edit-outfit"][data-record="helper-${newId}"]`);
    await expect(promoted).toBeVisible({ timeout: 30_000 });
    await expect(promoted.getByTestId("edit-outfit-source")).toHaveText("官方");
    await expect(promoted.getByTestId("edit-promote-open")).toHaveCount(0);
    await expect(block).toHaveCount(0);
    await expect(done).toBeVisible();

    // 有照片沒採用（秘書換了新圖會這樣）：預演就被拒，說哪幾張（CMI-14），什麼都沒動。
    const block2 = page.locator(`[data-testid="edit-outfit"][data-record="${second}"]`);
    const secondPhoto = api.photoOf(second);
    await api.post("vault/patch", {
      kind: "closet.photo",
      id: secondPhoto,
      set: { adopted: false },
    });
    await expect(
      block2.locator(`[data-testid="edit-photo"][data-photo="${secondPhoto}"]`),
    ).toHaveAttribute("data-adopted", "false", { timeout: 30_000 });
    await block2.getByTestId("edit-promote-open").click();
    const box2 = page.locator(`[data-testid="edit-promote"][data-record="${second}"]`);
    await box2.getByTestId("edit-promote-id").fill(`${newId}y`);
    await box2.getByTestId("edit-promote-preview-btn").click();
    await expect(box2.getByTestId("edit-promote-error")).toContainText("沒採用", {
      timeout: 30_000,
    });
    await expect(box2.getByTestId("edit-promote-error")).toContainText(secondPhoto);
    await expect(box2.getByTestId("edit-promote-confirm")).toHaveCount(0);

    // 被拒：另一套想用同一個新編號（已經有了），照實顯示程式說的原因，什麼都沒動。
    await block2
      .locator(`[data-testid="edit-photo"][data-photo="${secondPhoto}"]`)
      .getByTestId("edit-photo-adopt")
      .click();
    await expect
      .poll(async () => (await api.get("closet.photo", secondPhoto))?.fields.adopted)
      .toBe(true);
    await box2.getByTestId("edit-promote-id").fill(newId);
    await box2.getByTestId("edit-promote-preview-btn").click();
    await expect(box2.getByTestId("edit-promote-error")).toContainText("沒有升成官方：", {
      timeout: 30_000,
    });
    await expect(box2.getByTestId("edit-promote-error")).not.toContainText("沒採用");
    await expect(box2.getByTestId("edit-promote-confirm")).toHaveCount(0);
    expect((await api.get("closet.outfit", second))?.fields.source).toBe("mine");
  } finally {
    for (const id of [first, second, `helper-${newId}`]) {
      const rec = await api.get("closet.outfit", id);
      if (rec && !rec.archived)
        await request.post(`${base}/api/vault/archive`, { data: { kind: "closet.outfit", id } });
    }
    rmSync(officialImg, { force: true });
    for (const id of [newPhoto, oldPhoto, api.photoOf(second)]) {
      const rec = await api.get("closet.photo", id);
      if (rec && !rec.archived)
        await request.post(`${base}/api/vault/archive`, { data: { kind: "closet.photo", id } });
    }
  }
});

// 開發家不遮（DEV-157，CLO-27）＋升成官方找得到（CMI-16）：手機大小。主人在開發家用手機看：沒兌換的衣服要看得到真圖，
// 才知道要升成官方的是哪一套；衣服詳情頁有連結到角色頁那一套（加照片），可以升的另有一條到「修改」分頁、那一套的塊裡有「升成官方」。
// 正式家沒兌換的只有剪影在 closet.spec 的 TestLockedOutfitShowsSilhouetteOnly；「衣服」分頁沒有「升成官方」在 closet-mine.spec。
test.describe("開發家的展示間（手機大小）", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("TestDevHomeShowsUnredeemedAndLinksToPromote", async ({ page, request, baseURL }) => {
    const base = baseURL ?? "";
    const api = client(request, base);
    expect(((await api.post("system/get_mode", {})) as { env?: string }).env).toBe("dev");
    // 要 1 點的自己的衣服：這個家沒有點數，所以是沒兌換的。
    const record = await api.addOutfit("還沒兌換的衣服", 0, 1);
    const outfit = record.replace(/^helper-/, "");
    try {
      // 衣櫃的程式算好了：沒擁有、但給真圖（preview）。
      await expect
        .poll(
          async () => {
            const p = await api.get("closet.person", "helper");
            const o = ((p?.fields.outfits ?? []) as Record<string, unknown>[]).find(
              (x) => x.record === record,
            );
            return o ? `${o.owned}/${o.preview}` : "";
          },
          { timeout: 30_000 },
        )
        .toBe("false/true");
      // 真圖拿得到、跟上傳的一樣；沒有剪影。
      const real = await request.get(`${base}/pkg-files/closet/o/helper/${outfit}/1.png`);
      expect(real.status()).toBe(200);
      expect((await real.body()).equals(readFileSync(IMG[0] as string))).toBe(true);
      expect(
        (await request.get(`${base}/pkg-files/closet/s/helper/${outfit}/1.png`)).status(),
      ).toBe(404);

      // 展示間：那一格是真圖、沒有鎖頭，照樣標「未兌換」；說明不再說只看得到剪影。
      await page.goto("/p/closet/showroom?persona=helper");
      const tile = page.locator(`[data-testid="outfit-tile"][data-outfit="${record}"]`);
      await expect(tile).toBeVisible({ timeout: 30_000 });
      await expect(tile).toHaveAttribute("data-owned", "false");
      await expect(tile.getByTestId("outfit-img")).toHaveAttribute(
        "src",
        `/pkg-files/closet/o/helper/${outfit}/1.png`,
      );
      await expect(tile.getByTestId("lock-badge")).toHaveCount(0);
      await expect(tile.getByTestId("outfit-tags")).toContainText("未兌換");
      await expect(page.getByTestId("showroom-hint")).toContainText("這裡是開發家");
      // 手機寬度不橫向捲動。
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
      ).toBe(true);

      // 衣服詳情：真圖、標「未兌換（開發家看得到全部）」、兌換按鈕照舊在（沒有點數所以按不了）。
      await tile.getByRole("link").click();
      await expect(page.getByTestId("outfit-big")).toHaveAttribute(
        "src",
        `/pkg-files/closet/o/helper/${outfit}/1.png`,
      );
      await expect(page.getByTestId("outfit-locked-hint")).toContainText(
        "未兌換（開發家看得到全部）",
      );
      await expect(page.getByTestId("outfit-unlock")).toBeDisabled();
      // 到「修改」分頁：那一套的塊裡有「升成官方」；手機寬度不橫向捲動。
      const toEdit = page.getByTestId("outfit-to-edit");
      await expect(toEdit).toHaveText(/到「修改」分頁升成官方/);
      await expect(toEdit).toHaveAttribute("href", EDIT_TAB("helper"));
      // 到角色頁那一套（加照片）：不再說升成官方。
      const link = page.getByTestId("outfit-to-mine");
      await expect(link).toHaveText(/到角色頁管理這套（加照片）/);
      await expect(link).not.toContainText("升成官方");
      await toEdit.click();
      await expect(page).toHaveURL(/\/personas\/helper\?tab=closet\.edit$/);
      const block = page.locator(`[data-testid="edit-outfit"][data-record="${record}"]`);
      await expect(block.getByTestId("edit-promote-open")).toBeVisible({ timeout: 30_000 });
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
      ).toBe(true);
      // 角色頁「衣服」分頁那一套：真圖、標「未兌換（開發家看得到全部）」，沒有「升成官方」也沒有「可以升成官方」的小字。
      await page.goto(`/personas/helper?tab=closet-mine.outfits&item=${record}`);
      await expect(page.getByTestId(`mine-outfit-${record}`)).toBeVisible({ timeout: 30_000 });
      await expect(page.getByRole("button", { name: /升成官方/ })).toHaveCount(0);
      await page.getByRole("button", { name: "全部衣服" }).click();
      const row = page.getByTestId(`mine-row-${record}`);
      await expect(row).toContainText("未兌換（開發家看得到全部）");
      await expect(row).not.toContainText("升成官方");
      await expect(row.locator("img")).toHaveAttribute(
        "src",
        `/pkg-files/closet/o/helper/${outfit}/1.png`,
      );
    } finally {
      const rec = await api.get("closet.outfit", record);
      if (rec && !rec.archived)
        await request.post(`${base}/api/vault/archive`, {
          data: { kind: "closet.outfit", id: record },
        });
    }
  });
  // 開發家展示間的篩選（CLO-29，作者 2026-11-14「是不是要加一些 filter，不然我怎麼知道哪一些可以按」）：
  // 選「可以升成官方」→ 跨角色列出、每格標角色名、按鈕帶數量 → 點一套到那位角色的「修改」分頁，那一套的塊裡有「升成官方」。
  // 正式家沒有這排在 closet.spec 的 TestLockedOutfitShowsSilhouetteOnly。
  test("TestShowroomFilterListsPromotableAcrossPersonas", async ({
    page,
    request,
    baseURL,
    hgServer,
  }) => {
    const base = baseURL ?? "";
    const api = client(request, base);
    // 一套本來就是官方的（圖放在官方的頭像資料夾、紀錄 source = official）。
    const offId = `filter${Date.now().toString(36)}`;
    const off = `builder-${offId}`;
    const offImg = path.join(hgServer.home, "personas", "avatars", `builder@${offId}.png`);
    copyFileSync(IMG[1] as string, offImg);
    await api.post("vault/put", {
      record: {
        kind: "closet.outfit",
        id: off,
        fields: {
          persona: "builder",
          outfit: offId,
          name: "本來就是官方的",
          cost: 0,
          state: "active",
          source: "official",
        },
      },
    });
    // 兩位官方角色各一套自己的衣服；照片全是隱藏版的那套不能升（照照片紀錄的 hidden，DEV-160，不再照名字猜）。
    const a = await api.addOutfit("篩選用的衣服甲", 0, 0, "helper");
    const b = await api.addOutfit("篩選用的衣服乙", 1, 0, "builder");
    const hidden = await api.addOutfit("篩選用的隱藏版", 0, 0, "helper");
    await api.post("vault/patch", {
      kind: "closet.photo",
      id: api.photoOf(hidden),
      set: { hidden: true },
    });
    try {
      await expect
        .poll(
          async () => {
            const flags: string[] = [];
            for (const [persona, record] of [
              ["helper", a],
              ["builder", b],
              ["helper", hidden],
            ] as const) {
              const p = await api.get("closet.person", persona);
              const o = ((p?.fields.outfits ?? []) as Record<string, unknown>[]).find(
                (x) => x.record === record,
              );
              flags.push(o ? String(o.promotable === true) : "?");
            }
            return flags.join(",");
          },
          { timeout: 30_000 },
        )
        .toBe("true,true,false");

      await page.goto("/p/closet/showroom?persona=helper");
      const row = page.getByTestId("showroom-filter");
      await expect(row).toBeVisible({ timeout: 30_000 });
      await expect(row.getByRole("button")).toHaveText([
        /^全部\d+$/,
        /^可以升成官方\d+$/,
        /^已經是官方\d+$/,
        /^只在開發家（我自己的）\d+$/,
      ]);
      // 篩選那排緊接在點數卡下面、角色頭像列上面（一打開就看得到，DEV-156）；
      // 新增的鈕收在點數卡右邊的「⋯」（DEV-156 U-15），不再佔一排。
      const yOf = async (testId: string) =>
        (await page.getByTestId(testId).first().boundingBox())?.y ?? Number.NaN;
      const rowY = await yOf("showroom-filter");
      expect(rowY).toBeGreaterThan(await yOf("closet-points"));
      if ((await page.getByTestId("showroom-add").count()) > 0)
        expect(await yOf("showroom-add")).toBeLessThan(rowY);
      expect(rowY).toBeLessThan(await yOf("person-picker"));
      await expect(page.getByTestId("showroom-filter-all")).toHaveAttribute("aria-pressed", "true");
      // 沒選篩選：照舊一位一位看。
      await expect(page.getByTestId("person-section")).toHaveCount(1);

      const pick = page.getByTestId("showroom-filter-promotable");
      await pick.click();
      await expect(pick).toHaveAttribute("aria-pressed", "true");
      await expect(page).toHaveURL(/[?&]filter=promotable/);
      const listed = page.getByTestId("showroom-filtered");
      await expect(listed).toHaveAttribute("data-filter", "promotable");
      await expect(page.getByTestId("person-section")).toHaveCount(0);
      const tiles = listed.getByTestId("outfit-tile");
      const tileOf = (record: string) => listed.locator(`[data-outfit="${record}"]`);
      // 跨角色：兩位的都在、各標角色名；照片全是隱藏版的不在。
      await expect(tileOf(a)).toBeVisible();
      await expect(tileOf(b)).toBeVisible();
      await expect(tileOf(hidden)).toHaveCount(0);
      const ownerA = await tileOf(a).getByTestId("outfit-owner").innerText();
      const ownerB = await tileOf(b).getByTestId("outfit-owner").innerText();
      expect(ownerA).not.toBe("");
      expect(ownerB).not.toBe("");
      expect(ownerA).not.toBe(ownerB);
      // 按鈕上的數量＝列出來的套數。
      const count = Number(await pick.getByTestId("showroom-filter-count").innerText());
      expect(count).toBeGreaterThanOrEqual(2);
      await expect(tiles).toHaveCount(count);
      // 手機寬度：頁面不橫向捲動（那一排自己捲）。
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
      ).toBe(true);
      // 重新整理回到同一個篩選（UXC-3）。
      await page.reload();
      await expect(page.getByTestId("showroom-filter-promotable")).toHaveAttribute(
        "aria-pressed",
        "true",
        { timeout: 30_000 },
      );
      await expect(tileOf(b)).toBeVisible();

      // 「只在開發家」：照片全是隱藏版的那套在這裡；「已經是官方」沒有自己加的。
      await page.getByTestId("showroom-filter-mine").click();
      await expect(tileOf(hidden)).toBeVisible();
      await expect(tileOf(a)).toHaveCount(0);
      await page.getByTestId("showroom-filter-official").click();
      await expect(listed).toHaveAttribute("data-filter", "official");
      await expect(tileOf(off)).toBeVisible();
      await expect(tileOf(a)).toHaveCount(0);
      await expect(tileOf(hidden)).toHaveCount(0);

      // 點一套：到那位角色的「修改」分頁，那一套的塊裡有「升成官方」；本來就是官方的那套沒有。
      await page.getByTestId("showroom-filter-promotable").click();
      await tileOf(b).getByRole("link").click();
      await expect(page).toHaveURL(/\/personas\/builder\?tab=closet\.edit$/);
      const blockOf = (record: string) =>
        page.locator(`[data-testid="edit-outfit"][data-record="${record}"]`);
      await expect(blockOf(b).getByTestId("edit-promote-open")).toBeVisible({ timeout: 30_000 });
      await expect(blockOf(off)).toBeVisible();
      await expect(blockOf(off).getByTestId("edit-promote-open")).toHaveCount(0);
      // 上一頁回到展示間、同一個篩選。
      await page.goBack();
      await expect(page).toHaveURL(/\/p\/closet\/showroom\?.*filter=promotable/);
      await expect(page.getByTestId("showroom-filter-promotable")).toHaveAttribute(
        "aria-pressed",
        "true",
      );
    } finally {
      rmSync(offImg, { force: true });
      for (const id of [a, b, hidden, off]) {
        const rec = await api.get("closet.outfit", id);
        if (rec && !rec.archived)
          await request.post(`${base}/api/vault/archive`, { data: { kind: "closet.outfit", id } });
      }
    }
  });
});
