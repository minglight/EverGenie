import type { APIRequestContext, Page } from "@playwright/test";
import { expect, test } from "./fixtures";

const mainNav = (page: Page) => page.getByRole("navigation", { name: "主選單" });

// 選單的 renderer 是存在家裡的設定：每個測試自己設，結束一定清掉（同一個 worker 的下一個測試用預設選單）。
async function setRenderer(request: APIRequestContext, base: string, renderer: string) {
  const res = await request.post(`${base}/api/web/set_menu_prefs`, {
    data: { prefs: { renderer, tree: [] } },
  });
  expect(res.ok(), await res.text()).toBe(true);
}

test.afterEach(async ({ request, baseURL }) => {
  await setRenderer(request, baseURL ?? "", "");
});

// Guards: REQ-106
test("TestPackagePageOpens @smoke", async ({ page, isMobile }) => {
  await page.goto("/");
  await expect(mainNav(page)).toBeVisible();
  // 套件的選單項目在預設樹的「未分類」；手機放不下，收在「更多」裡。
  if (isMobile) {
    await page.getByRole("button", { name: "更多" }).click();
    await page.getByRole("dialog", { name: "更多" }).getByRole("link", { name: "示範頁" }).click();
  } else {
    await mainNav(page).getByRole("link", { name: "示範頁" }).click();
  }
  await expect.poll(() => new URL(page.url()).pathname).toBe("/p/demo/home");
  await expect(page.getByTestId("demo-page")).toBeVisible();
  await expect(page.getByRole("heading", { name: "示範頁" })).toBeVisible();
  expect(await page.title()).toContain("示範頁");
  // 套件頁面能用外殼提供的元件、自己有狀態。
  await page.getByRole("button", { name: /按了 0 次/ }).click();
  await expect(page.getByRole("button", { name: /按了 1 次/ })).toBeVisible();
  // 重新整理還在同一頁，網址的 search 也傳得進去。
  await page.goto("/p/demo/home?x=1");
  await expect(page.getByTestId("demo-search")).toHaveText("search=?x=1");
  await page.reload();
  await expect(page.getByTestId("demo-page")).toBeVisible();
  expect(new URL(page.url()).pathname).toBe("/p/demo/home");
  // 掛在 slot 的頁面不能直接開。
  await page.goto("/p/demo/card");
  await expect(page.getByText("可能已經搬走，或網址打錯了。")).toBeVisible();
});

// 套件頁面用同一組 vault API 讀自己宣告的資料（demo.note）：資料經看板的代理寫進去（只限套件的種類），
// 示範頁用 @evergenie/sdk-web 的 client 列出來；核心的種類（任務）看板不給。
// Guards: REQ-173
test("TestPackagePageReadsData", async ({ page, request, baseURL }) => {
  const text = `筆記-${Math.random().toString(36).slice(2, 10)}`;
  const post = async (route: string, data: unknown) => {
    const res = await request.post(`${baseURL}/api/${route}`, { data });
    return {
      status: res.status(),
      json: (await res.json().catch(() => ({}))) as Record<string, unknown>,
    };
  };
  const next = await post("vault/next_id", { kind: "demo.note" });
  expect(next.status, JSON.stringify(next.json)).toBe(200);
  const id = String(next.json.id);
  expect(id).toMatch(/^N-\d+$/);
  const put = await post("vault/put", {
    record: { kind: "demo.note", id, fields: { text, date: "2026-10-25", score: 4.5 } },
  });
  expect(put.status, JSON.stringify(put.json)).toBe(200);
  // 欄位不符被核心擋下並說明（缺必填的 text）。
  const bad = await post("vault/put", {
    record: { kind: "demo.note", id: "N-9999", fields: { score: 1 } },
  });
  expect(bad.status).toBe(400);
  // 核心的種類看板不給。
  expect((await post("vault/list", { kind: "task" })).status).toBe(404);

  await page.goto("/p/demo/home");
  const notes = page.getByTestId("demo-notes");
  await expect(notes).toBeVisible();
  await expect(notes.getByText(`${id}：${text}`)).toBeVisible();
  // 封存之後（資料沒刪，只是不列）重新整理就沒有了。
  expect((await post("vault/archive", { kind: "demo.note", id })).status).toBe(200);
  await page.reload();
  // 清單載入完成（元素在，可能是空的所以不要求看得見）之後，這一筆不在裡面。
  await expect(page.getByTestId("demo-notes")).toBeAttached();
  await expect(page.getByTestId("demo-notes").getByText(text)).toHaveCount(0);
});

// Guards: REQ-114
test("TestSlotRenders", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("demo-card")).toBeVisible();
  await expect(page.getByText("這張卡片是示範套件掛在今天頁的。")).toBeVisible();
  // 別的頁面沒有這個 slot，不會冒出卡片。
  await page.goto("/messages");
  await expect(page.getByTestId("demo-card")).toHaveCount(0);
});

// Guards: REQ-114
test("TestBrokenPackagePageShowsError", async ({ page }) => {
  await page.goto("/p/demo/broken");
  const alert = page.getByRole("alert");
  await expect(alert).toBeVisible();
  await expect(alert).toContainText("頁面「編不過的頁面」編不過：");
  await expect(alert).toContainText("套件頁面只能 import 套件裡的檔案");
  // 外殼還在，別的套件頁面照常。
  await expect(mainNav(page)).toBeVisible();
  await page.goto("/p/demo/home");
  await expect(page.getByTestId("demo-page")).toBeVisible();
});

// Guards: REQ-140
test("TestBrokenRendererFallsBack", async ({ page, request, baseURL }) => {
  const notice = page.getByTestId("menu-renderer-notice");
  // 編不過：伺服器就說了，直接用預設選單加提醒。
  await setRenderer(request, baseURL ?? "", "demo.badmenu");
  await page.goto("/");
  await expect(mainNav(page)).toBeVisible();
  await expect(page.getByTestId("custom-menu")).toHaveCount(0);
  await expect(notice).toContainText("自訂選單載入失敗，先用預設的選單");
  await expect(notice).toContainText("編不過");
  await expect(notice).toContainText("?menu=default");
  // 提醒可以關掉（只關這次）。
  await notice.getByRole("button", { name: "關閉這則提醒" }).click();
  await expect(notice).toHaveCount(0);
  await expect(mainNav(page)).toBeVisible();

  // 找不到。
  await setRenderer(request, baseURL ?? "", "demo.nope");
  await page.goto("/");
  await expect(notice).toContainText("找不到自訂選單「demo.nope」");
  await expect(mainNav(page)).toBeVisible();

  // 編得過、執行時丟錯：先試著載入，丟錯後換回預設選單加提醒。
  await setRenderer(request, baseURL ?? "", "demo.crashmenu");
  await page.goto("/");
  await expect(notice).toContainText("故意壞掉的選單");
  await expect(mainNav(page)).toBeVisible();
  await expect(page.getByTestId("custom-menu")).toHaveCount(0);
  // 選單仍然完整可用。
  await mainNav(page).getByRole("link", { name: /^待辦/ }).first().click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/reminders");
});

// Guards: REQ-140
test("TestMenuDefaultParam", async ({ page, request, baseURL, isMobile }) => {
  await setRenderer(request, baseURL ?? "", "demo.menu");
  await page.goto("/");
  const custom = page.getByTestId("custom-menu");
  await expect(custom).toBeVisible();
  await expect(custom).toHaveAttribute("data-layout", isMobile ? "mobile" : "desktop");
  await expect(mainNav(page)).toHaveCount(0);
  await expect(page.getByTestId("menu-renderer-notice")).toHaveCount(0);
  // 自訂選單能換頁（navigate），pool 裡有套件的項目。
  await custom.getByRole("button", { name: "權限申請" }).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/policy");
  await expect(custom.getByRole("button", { name: "示範頁" })).toBeVisible();
  // ?menu=default：一律用預設選單，不載入自訂元件，也不顯示提醒。
  await page.goto("/?menu=default");
  await expect(mainNav(page)).toBeVisible();
  await expect(custom).toHaveCount(0);
  await expect(page.getByTestId("menu-renderer-notice")).toHaveCount(0);
  // 在看板裡換頁（網址丟掉參數）也一直用預設。
  await mainNav(page).getByRole("link", { name: /^待辦/ }).first().click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/reminders");
  await expect(mainNav(page)).toBeVisible();
  await expect(custom).toHaveCount(0);
});

// 選用套件（manifest optional = true）列在「設定 → 套件」最上面的「進階套件（選用）」，預設關著，開關送 set_enabled。
// 例子用開發工坊（admin）；closet-mine 2026-11-03 起預設啟用、不再列在這裡（DEV-112）。
// 不真的開 admin（它的行程沒設定 [admin] 時會去讀開發機上真的 repo）：攔下 set_enabled，只驗頁面送了什麼。
// admin 是 dev_only：fixture 是正式家，不列；把 get_mode 回的 env 改成 dev（開發家）才列（作者 2026-11-04）。
// Guards: REQ-176
test("TestPackagesPageOffersOptionalPackage", async ({ page }) => {
  // 先等家的身分（get_mode）回來再驗：不然「還沒讀到就當開發家」寫反時，admin 先出現再消失也會過。
  const mode = page.waitForResponse("**/api/system/get_mode");
  await page.goto("/settings/packages");
  await mode;
  await expect(page.getByTestId("packages-install-jump")).toBeVisible();
  await expect(page.getByTestId("optional-admin")).toHaveCount(0);
  await page.route("**/api/system/get_mode", async (route) => {
    const res = await route.fetch();
    await route.fulfill({ response: res, json: { ...(await res.json()), env: "dev" } });
  });
  const sent: unknown[] = [];
  await page.route("**/api/package/set_enabled", async (route) => {
    const body = route.request().postDataJSON() as { id: string; enabled: boolean };
    sent.push(body);
    await route.fulfill({
      json: { package: { id: body.id, name: "開發工坊", enabled: body.enabled, state: "stopped" } },
    });
  });
  await page.reload();
  const step = page.getByTestId("packages-optional");
  await expect(step).toContainText("進階套件（選用）");
  await expect(step.getByTestId("optional-closet-mine")).toHaveCount(0);
  const toggle = step.getByTestId("optional-admin").getByRole("switch", { name: "啟用 開發工坊" });
  await expect(toggle).toHaveAttribute("aria-checked", "false");
  await toggle.click();
  await expect.poll(() => sent).toEqual([{ id: "admin", enabled: true }]);
});
