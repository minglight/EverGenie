import type { APIRequestContext, Page } from "@playwright/test";
import { expect, test } from "./fixtures";

// P6-2b：設定頁補齊的分頁（P6-5a 起十個，多了角色庫；P6-6 第十一個「紀錄」；F4 加過「排程」，2026-10-26 搬到任務單，回到十一個；
// 角色庫 2026-11-04 搬到「角色」頁 /personas，WEB-136）。示範套件 demo 由 fixtures.ts 複製進暫存家（packages/mine/demo/，有一個 greeting 擴充點）。
// 這些測試會改家裡的設定（選單、權限、套件開關），每個都在結束時改回原樣，同一個 worker 的下一個測試才不受影響。

const mainNav = (page: Page) => page.getByRole("navigation", { name: "主選單" });
const settingsTabs = (page: Page) => page.getByRole("navigation", { name: "設定分頁" });

async function call<T = Record<string, unknown>>(
  request: APIRequestContext,
  base: string | undefined,
  route: string,
  data: unknown = {},
): Promise<{ status: number; json: T }> {
  const res = await request.post(`${base}/api/${route}`, { data });
  return { status: res.status(), json: (await res.json().catch(() => ({}))) as T };
}

interface MenuEntryJSON {
  id?: string;
  title?: string;
  children?: MenuEntryJSON[];
}

async function menuTree(request: APIRequestContext, base: string | undefined) {
  const { json } = await call<{ menu: { tree?: MenuEntryJSON[]; pool?: { id: string }[] } }>(
    request,
    base,
    "web/get_menu",
  );
  return json.menu;
}

const tabs: { path: string; label: string; marker: (p: Page) => ReturnType<Page["getByText"]> }[] =
  [
    { path: "notify", label: "通知", marker: (p) => p.getByText("各種通知怎麼送") },
    { path: "display", label: "顯示", marker: (p) => p.getByText("對話的顯示方式").first() },
    { path: "trust", label: "權限", marker: (p) => p.getByText("每位秘書的等級", { exact: true }) },
    { path: "devices", label: "裝置推播", marker: (p) => p.getByText("這台裝置").first() },
    { path: "phone", label: "手機連線", marker: (p) => p.getByTestId("phone-step-1") },
    { path: "menu", label: "選單", marker: (p) => p.getByText("目前的選單", { exact: true }) },
    { path: "packages", label: "套件", marker: (p) => p.getByText("示範套件").first() },
    {
      path: "agents",
      label: "職位與秘書",
      marker: (p) => p.getByText("改帳號、模型、思考力度後，下次開對話才生效。"),
    },
    { path: "machine", label: "電腦", marker: (p) => p.getByText("這台的環境", { exact: true }) },
    {
      path: "setup",
      label: "設定精靈",
      marker: (p) => p.getByText("1. 選職位，每個職位請一位角色擔任"),
    },
    {
      path: "health",
      label: "系統健康",
      marker: (p) => p.getByText("看板", { exact: true }).first(),
    },
    {
      path: "log",
      label: "紀錄",
      marker: (p) => p.getByText("保留最近 30 天，更早的會自動清掉。"),
    },
  ];

// Guards: REQ-111
test("TestSettingsTabsReachable @smoke", async ({ page, isMobile }) => {
  await page.goto("/settings");
  await expect(page).toHaveURL(isMobile ? /\/settings$/ : /\/settings\/notify$/);
  // 桌機：左邊的分頁清單標出這一頁；手機：沒有分頁列，頁首只寫這一頁的名字（DEV-156 U-12）。
  const current = async (label: string) => {
    if (isMobile) {
      await expect(page.locator("header").first()).toContainText(label);
      await expect(page.locator("header").first()).not.toContainText("設定 ·");
      await expect(settingsTabs(page)).toHaveCount(0);
    } else {
      await expect(settingsTabs(page).locator('[aria-current="page"]')).toHaveText(label);
    }
  };
  for (const tab of tabs) {
    await page.goto(`/settings/${tab.path}`);
    await expect(tab.marker(page)).toBeVisible();
    await current(tab.label);
    // 頁首與分頁標題用的名字跟分頁上寫的一樣（以前「裝置推播」的頁首寫「設定 · 裝置」，WEB-104）。
    await expect(page).toHaveTitle(new RegExp(`設定 · ${tab.label} · `));
    // 重新整理，畫面不變。
    await page.reload();
    expect(new URL(page.url()).pathname).toBe(`/settings/${tab.path}`);
    await expect(tab.marker(page)).toBeVisible();
    await current(tab.label);
  }
});

// 手機：設定首頁是一張清單（常用／秘書／進階三組），點一列進那一頁，頁首只寫那頁的名字、上一頁回清單；
// 收到設定的頁面（權限申請）在「進階」裡，不在每一頁都出現；整頁沒有水平捲動（DEV-156 U-12）。
// 桌機照舊是左邊的分頁清單。
// Guards: REQ-111
test("TestMobileSettingsList", async ({ page, isMobile }) => {
  if (!isMobile) {
    await page.goto("/settings/setup");
    const strip = settingsTabs(page);
    // 桌機是左側的清單，十三個分頁都看得到（S7 加了「規矩」；角色庫 2026-11-04 搬到「角色」頁，WEB-136；DEV-153 加了「手機連線」）。
    await expect(strip.getByRole("link")).toHaveCount(13);
    // 分成「常用／秘書／進階」三類（REQ-237）。
    for (const label of ["常用", "秘書", "進階"]) {
      await expect(strip.getByText(label, { exact: true })).toBeVisible();
    }
    return;
  }
  await page.goto("/settings");
  const home = page.getByTestId("settings-home");
  const group = (name: string) => home.getByRole("region", { name, exact: true });
  await expect(home.getByRole("heading", { level: 2 })).toHaveText(["常用", "秘書", "進階"]);
  await expect(group("常用").getByRole("link")).toHaveText([
    "設定精靈",
    "通知",
    "顯示",
    "手機連線",
    "裝置推播",
    "選單",
  ]);
  await expect(group("秘書").getByRole("link")).toHaveText(["職位與秘書", "權限", "規矩"]);
  // 預設收起來的「權限申請」排在進階最後（MNU-10）。
  await expect(group("進階").getByRole("link")).toHaveText([
    "套件",
    "電腦",
    "系統健康",
    "紀錄",
    "權限申請",
  ]);
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(overflow).toBe(false);
  // 每一列至少 44 高（UXC-5）。
  const box = await group("秘書").getByRole("link", { name: "職位與秘書" }).boundingBox();
  expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);

  await group("秘書").getByRole("link", { name: "職位與秘書" }).click();
  await expect(page).toHaveURL(/\/settings\/agents$/);
  const header = page.locator("header").first();
  await expect(header).toContainText("職位與秘書");
  await expect(header).not.toContainText("設定");
  // 內容前面沒有分類、分頁、收起來的頁面那幾層。
  await expect(page.getByRole("navigation", { name: "設定分類" })).toHaveCount(0);
  await expect(settingsTabs(page)).toHaveCount(0);
  await expect(page.getByTestId("settings-tucked")).toHaveCount(0);
  await page.getByTestId("back-link").click();
  await expect(page).toHaveURL(/\/settings$/);

  // 從通知、書籤直接開某一頁（沒有站內歷史）：上一頁也回清單。
  await page.goto("/settings/devices");
  await expect(header).toContainText("裝置推播");
  await page.getByTestId("back-link").click();
  await expect(page).toHaveURL(/\/settings$/);
  await expect(home).toBeVisible();
});

// Guards: REQ-111, REQ-237
test("TestAgentsTabToggle", async ({ page, request, baseURL, seed }) => {
  // 用一位自己造的秘書來開關（不動 code、life，同一個 worker 的別的測試還要用）。造好時是停用的。
  // 畫面上顯示的是現任人設的人名（helper 這個人設叫 Helper），不是 display_name。
  const id = `tg${Date.now().toString(36)}`;
  // 卡片標題是職位（職責的第一段），不是人名（REQ-237）。
  const name = "e2e 測試用";
  await seed.api("/api/agents/create", {
    id,
    persona: "helper",
    duty: "e2e 測試用",
    by: { name: "owner(web)" },
  });
  await page.goto("/settings/agents");
  const row = page.locator(`[data-agent="${id}"]`);
  await expect(row).toBeVisible();
  await expect(row).toContainText("已停用");
  await expect(row.getByRole("heading", { name: new RegExp(`^${name}`) })).toContainText(
    `職位 ${id}`,
  );
  const toggle = row.getByRole("switch", { name: `啟用「${name}」` });
  await expect(toggle).toHaveAttribute("aria-checked", "false");
  // 開啟：直接送，不用確認。
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-checked", "true");
  const enabled = async () =>
    (
      await call<{ agents: { id: string; enabled: boolean }[] }>(request, baseURL, "web/get_agents")
    ).json.agents.find((a) => a.id === id)?.enabled;
  await expect.poll(enabled).toBe(true);
  // 名冊（頭像、篩選用）也跟著有這位。
  await expect
    .poll(async () => {
      const r = await call<{ agents: { id: string }[] }>(request, baseURL, "web/get_roster");
      return r.json.agents.some((a) => a.id === id);
    })
    .toBe(true);
  // 關閉：先確認；「先不要」什麼都不做。
  await toggle.click();
  const dialog = page.getByRole("dialog", { name: `停用「${name}」？` });
  await expect(dialog).toContainText("停用後不會再收訊息、排程也停，資料都留著。");
  await dialog.getByRole("button", { name: "先不要" }).click();
  await expect(dialog).toHaveCount(0);
  await expect(toggle).toHaveAttribute("aria-checked", "true");
  expect(await enabled()).toBe(true);
  await toggle.click();
  await page
    .getByRole("dialog", { name: `停用「${name}」？` })
    .getByRole("button", { name: "停用" })
    .click();
  await expect(toggle).toHaveAttribute("aria-checked", "false");
  await expect.poll(enabled).toBe(false);
  // 由誰負責、帳號、預設模型、思考力度、角色提示都在；模型與思考力度改了就存（REQ-237）。
  await expect(row.getByRole("combobox", { name: `「${name}」用的帳號` })).toHaveValue("main");
  await expect(row.getByRole("combobox", { name: `「${name}」由誰負責` })).toHaveValue("helper");
  await expect(row).toContainText("換角色時記憶和職責都不動");
  await expect(page.getByText("改帳號、模型、思考力度後，下次開對話才生效。")).toBeVisible();
  const field = async (f: "model" | "effort") =>
    (
      await call<{ agents: { id: string; model?: string; effort?: string }[] }>(
        request,
        baseURL,
        "web/get_agents",
      )
    ).json.agents.find((a) => a.id === id)?.[f];
  await row.getByTestId("agent-model").selectOption("opus");
  await expect.poll(() => field("model")).toBe("opus");
  // 存好要有反應（WEB-103）：手機上頁尾那行要捲很遠，改了當場說一聲。
  await expect(page.getByText("已存。下次開對話才生效。").first()).toBeVisible();
  await row.getByTestId("agent-effort").selectOption("high");
  await expect.poll(() => field("effort")).toBe("high");
  await row.getByTestId("agent-effort").selectOption("");
  await expect.poll(() => field("effort")).toBeUndefined();
});

// Guards: REQ-111
test("TestMachineTabShowsChecks", async ({ page }) => {
  // 真的核心：假引擎沒有要檢查的東西，帳號有一筆還沒有資料的用量，沒有備份。
  await page.goto("/settings/machine");
  await expect(page.getByText("這台的環境", { exact: true })).toBeVisible();
  await expect(page.getByText("還沒有檢查結果。")).toBeVisible();
  // 沒有用量資料：用量那一區整個收起來（DEV-153），不放空的用量條或「還沒量到」。
  await expect(page.getByTestId("account-row")).toHaveCount(1);
  await expect(page.getByText("AI 帳號用量", { exact: true })).toHaveCount(0);
  await expect(page.getByTestId("usage-row")).toHaveCount(0);
  // 只有一個帳號：清單上不列引擎與登入資料夾；編輯時這些收在「進階」，按了才展開。
  const account = page.getByTestId("account-row");
  await expect(account).toContainText("main");
  await expect(account).not.toContainText("/");
  await account.getByRole("button", { name: "編輯" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("帳號名稱")).toBeVisible();
  await expect(dialog.getByText("登入資料夾")).toHaveCount(0);
  await expect(dialog.getByText("用量暖機")).toHaveCount(0);
  const advanced = dialog.getByTestId("account-advanced");
  expect((await advanced.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(44);
  await advanced.click();
  await expect(dialog.getByText("登入資料夾")).toBeVisible();
  await expect(dialog.getByText("環境檔（選填）")).toBeVisible();
  await expect(dialog.getByText("引擎", { exact: true })).toBeVisible();
  await expect(dialog.getByText("用量暖機")).toBeVisible();
  await dialog.getByRole("button", { name: "取消" }).click();
  await expect(dialog).toHaveCount(0);
  await expect(
    page.getByText("要清理備份，跟任何一位秘書說「幫我清理舊的設定備份」。"),
  ).toBeVisible();
  // 按鈕只有：最上面「看板連的電腦」的「加入」、「誰可以連進這台」的「儲存」「允許」（S3-9；只有一台、沒填東西時都還不能按），
  // 帳號的「編輯」「新增帳號」、夜班清單的「新增一項」（夜班清單是空的，沒有「刪除」），
  // 備份的「立刻備份」「從備份還原」「搬到新電腦」（DEV-153；備份只放在這台電腦，沒有別的設定），
  // 備份掃描的「現在掃一次」（DEV-163；報告讀得到才只有這一顆，讀不到會多一顆「再試一次」）。
  const main = page.getByRole("main");
  await expect(main.getByRole("button")).toHaveCount(10);
  for (const name of ["加入", "儲存", "允許"]) {
    await expect(main.getByRole("button", { name, exact: true })).toBeDisabled();
  }
  for (const name of [
    "編輯",
    "新增帳號",
    "新增一項",
    "立刻備份",
    "從備份還原",
    "搬到新電腦",
    "現在掃一次",
  ]) {
    await expect(main.getByRole("button", { name, exact: true })).toBeEnabled();
  }
});

// 環境檢查一項一列：用假的回應驗版面（✓／✗、名稱、說明；不正常的另一行講怎麼修）。
// Guards: REQ-111
test("TestMachineTabRendersCheckRows", async ({ page }) => {
  await page.route("**/api/system/preflight", (route) =>
    route.fulfill({
      json: {
        checks: [
          { id: "brew", name: "Homebrew", ok: true, detail: "已安裝 4.5" },
          {
            id: "go",
            name: "Go",
            ok: false,
            detail: "找不到 go",
            fix: "請電腦秘書用 Homebrew 安裝",
          },
        ],
      },
    }),
  );
  await page.route("**/api/system/backups", (route) =>
    route.fulfill({
      json: {
        total: 3_500_000,
        files: [
          { path: "trust/a", group: "trust", size: 1_000_000, mod_time: "2026-10-20T08:00:00Z" },
          { path: "trust/b", group: "trust", size: 2_000_000, mod_time: "2026-10-24T08:00:00Z" },
          { path: "home/c", group: "home", size: 500_000, mod_time: "2026-10-22T08:00:00Z" },
        ],
      },
    }),
  );
  // 用量：只列量得到的帳號（DEV-153）；量不到的那個不佔一列。
  await page.route("**/api/system/usage", (route) =>
    route.fulfill({
      json: {
        accounts: [
          { account: "main", used_ratio: 0.42, measured_at: "2026-10-24T08:00:00Z" },
          { account: "second", used_ratio: 0 },
        ],
      },
    }),
  );
  await page.goto("/settings/machine");
  await expect(page.getByText("AI 帳號用量", { exact: true })).toBeVisible();
  const usage = page.getByTestId("usage-row");
  await expect(usage).toHaveCount(1);
  await expect(usage).toContainText("main");
  await expect(usage).toContainText("已用 42%");
  const checks = page.getByTestId("preflight-check");
  await expect(checks).toHaveCount(2);
  await expect(checks.first()).toContainText("Homebrew");
  await expect(checks.first()).toContainText("已安裝 4.5");
  await expect(checks.last()).toContainText("找不到 go");
  await expect(checks.last()).toContainText("請電腦秘書用 Homebrew 安裝");
  await expect(checks.first()).not.toContainText("請電腦秘書");
  await expect(
    page.getByText("要修的話，跟任何一位秘書說「幫我看這台電腦的環境」。", { exact: false }),
  ).toBeVisible();
  await expect(page.getByTestId("backups-total")).toContainText("3.3 MB");
  await expect(page.getByTestId("backups-total")).toContainText("共 3 份");
  const groups = page.getByTestId("backup-group");
  await expect(groups).toHaveCount(2);
  await expect(groups.first()).toContainText("trust：2 份");
  await expect(groups.last()).toContainText("home：1 份");
});

// Guards: REQ-93, REQ-94
test("TestPackagesTabToggleAndExtension", async ({ page, request, baseURL, isMobile }) => {
  const inPool = async () =>
    ((await menuTree(request, baseURL)).pool ?? []).some((i) => i.id === "demo.home");
  expect(await inPool()).toBe(true);
  try {
    await page.goto("/settings/packages");
    const row = page.getByTestId("package-demo");
    await expect(row).toContainText("示範套件");
    await expect(row).toContainText("執行中");
    await expect(row).toContainText("版本 1.0.0");
    await expect(page.getByText("自己的", { exact: true })).toBeVisible();
    if (!isMobile) await expect(mainNav(page).getByRole("link", { name: "示範頁" })).toBeVisible();

    // 擴充點：改了、離開欄位就存，重新整理還在；清空＝拿掉。
    const greeting = page.getByRole("textbox", { name: "問候語" });
    await expect(greeting).toHaveValue("");
    await expect(greeting).toHaveAttribute("placeholder", "預設：哈囉");
    const text = `早安 ${Date.now()}`;
    await greeting.fill(text);
    await greeting.blur();
    await expect(page.getByText("已儲存")).toBeVisible();
    await page.reload();
    await expect(page.getByRole("textbox", { name: "問候語" })).toHaveValue(text);
    const values = await call<{ values: Record<string, string> }>(
      request,
      baseURL,
      "package/list_extensions",
    );
    expect(values.json.values["demo.greeting"]).toBe(text);
    await page.getByRole("textbox", { name: "問候語" }).fill("");
    await page.getByRole("textbox", { name: "問候語" }).blur();
    await expect
      .poll(async () => {
        const r = await call<{ values?: Record<string, string> }>(
          request,
          baseURL,
          "package/list_extensions",
        );
        return r.json.values?.["demo.greeting"] ?? "";
      })
      .toBe("");

    // 停用：先確認；「先不要」不做；停用後選單項目馬上消失（不用重新整理）。
    const toggle = page.getByRole("switch", { name: "啟用 示範套件" });
    await toggle.click();
    const dialog = page.getByRole("dialog", { name: "停用「示範套件」？" });
    await expect(dialog).toContainText("停用後它的頁面和選單會消失，附程式的會停掉；資料保留。");
    await dialog.getByRole("button", { name: "先不要" }).click();
    await expect(toggle).toHaveAttribute("aria-checked", "true");
    expect(await inPool()).toBe(true);
    await toggle.click();
    await page
      .getByRole("dialog", { name: "停用「示範套件」？" })
      .getByRole("button", { name: "停用" })
      .click();
    await expect(toggle).toHaveAttribute("aria-checked", "false");
    await expect(row).toContainText("停止");
    await expect.poll(inPool).toBe(false);
    if (!isMobile) await expect(mainNav(page).getByRole("link", { name: "示範頁" })).toHaveCount(0);
    // 再開起來：直接送，選單項目回來。
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-checked", "true");
    await expect.poll(inPool).toBe(true);
    if (!isMobile) await expect(mainNav(page).getByRole("link", { name: "示範頁" })).toBeVisible();
  } finally {
    await call(request, baseURL, "package/set_enabled", { id: "demo", enabled: true });
    await call(request, baseURL, "package/set_extension", {
      package: "demo",
      extension: "greeting",
      value: {},
    });
  }
});

// 從資料夾安裝：路徑不對時錯誤顯示在表單下面（白話），表單不關。
// Guards: REQ-93
test("TestPackagesFolderInstallShowsError", async ({ page }) => {
  await page.goto("/settings/packages");
  await page.getByRole("button", { name: "從資料夾安裝" }).click();
  const submit = page.getByRole("button", { name: "安裝", exact: true });
  await expect(submit).toBeDisabled();
  await page.getByLabel("資料夾的完整路徑").fill("/tmp/hg-e2e-no-such-package-folder");
  await submit.click();
  await expect(page.getByRole("alert").filter({ hasText: "沒做成功" })).toBeVisible();
  await expect(page.getByLabel("資料夾的完整路徑")).toBeVisible();
  await page.getByRole("button", { name: "取消" }).click();
  await expect(page.getByLabel("資料夾的完整路徑")).toHaveCount(0);
});

// 補裝 Go 的橫幅與對話框：沒有套件在等 Go 時看不到橫幅；有的話按「先不要」不送請求，按「安裝」才送 confirmed:true。
// 不真的下載 Go：整個 package.* 用假的回應。
// Guards: REQ-171
test("TestInstallGoBannerAndDialog", async ({ page }) => {
  const installs: unknown[] = [];
  const pkg = (id: string, name: string, remedy?: string) => ({
    id,
    name,
    version: "0.1.0",
    kind: "program",
    tier: "mine",
    state: remedy ? "failed" : "running",
    enabled: true,
    error: remedy
      ? "這台電腦還沒有編譯工具（Go）。這個套件要先裝編譯工具（Go，約 70MB，裝在程式自己的資料夾裡），要我裝嗎？"
      : undefined,
    remedy,
  });
  await page.route("**/api/package/list", (route) =>
    route.fulfill({
      json: {
        packages: [
          pkg("fit", "健身", "install_go"),
          pkg("eat", "飲食", "install_go"),
          pkg("ok", "好的"),
        ],
      },
    }),
  );
  await page.route("**/api/package/install_go", async (route) => {
    installs.push(route.request().postDataJSON());
    await route.fulfill({
      json: {
        command: "下載 Go go1.27.4 到程式自己的資料夾",
        version: "go1.27.4",
        packages: [
          { id: "fit", name: "健身" },
          { id: "eat", name: "飲食" },
        ],
      },
    });
  });
  await page.goto("/settings/packages");
  await expect(page.getByTestId("install-go-banner")).toContainText(
    "有 2 個套件要先裝編譯工具（Go）才能用",
  );
  await expect(page.getByTestId("package-fit")).toContainText("這台電腦還沒有編譯工具（Go）");
  await expect(page.getByTestId("package-fit")).toContainText("有問題");
  // 失敗的附程式套件有「重新載入」。
  await expect(
    page.getByTestId("package-fit").getByRole("button", { name: "重新載入" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "安裝 Go" }).click();
  const dialog = page.getByRole("dialog", { name: "安裝 Go" });
  await expect(dialog).toContainText("程式自己的資料夾");
  await expect(dialog).toContainText("不需要密碼");
  await expect(dialog).not.toContainText("Homebrew");
  await dialog.getByRole("button", { name: "先不要" }).click();
  await expect(dialog).toHaveCount(0);
  expect(installs).toHaveLength(0);
  await page.getByRole("button", { name: "安裝 Go" }).click();
  await page
    .getByRole("dialog", { name: "安裝 Go" })
    .getByRole("button", { name: "安裝", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toContainText("重新編譯了：健身、飲食");
  // 瀏覽器只送 confirmed；by 是看板代理換上去的。
  expect(installs).toEqual([{ confirmed: true }]);
});

async function resetMenu(request: APIRequestContext, base: string | undefined) {
  const res = await call(request, base, "web/set_menu_prefs", {
    prefs: { tree: [], renderer: "" },
  });
  expect(res.status).toBe(200);
}

// Guards: REQ-141
test("TestMenuEditorSavesLive", async ({ page, request, baseURL, isMobile }) => {
  await resetMenu(request, baseURL);
  try {
    await page.goto("/settings/menu");
    // 一打開就是畫面上的樣子：預設的兩個群組，加上自己的套件（示範頁，沒有 hint）放在預設的「我的套件」群組（REQ-349）。
    await expect(page.locator('[data-group="daily"]')).toBeVisible();
    await expect(page.locator('[data-group="work"]')).toBeVisible();
    await expect(page.locator('[data-group="mine"] [data-item="demo.home"]')).toBeVisible();
    await expect(page.getByRole("button", { name: "儲存" })).toBeDisabled();

    // 新增群組、改名、把「聊天」放進去、把「示範頁」也放進去。
    const groupName = `常用${Date.now().toString(36)}`;
    await page.getByRole("button", { name: "新增群組" }).click();
    await expect(page.getByText("有未儲存的變更")).toBeVisible();
    const group = page.locator('[data-group="group1"]');
    const nameInput = group.getByRole("textbox", { name: "群組名稱" });
    await nameInput.fill(groupName);
    await nameInput.press("Enter");
    await page
      .getByRole("combobox", { name: "把 聊天 放進群組" })
      .selectOption({ label: groupName });
    await expect(group.locator('[data-item="core.chat"]')).toBeVisible();
    await page
      .getByRole("combobox", { name: "把 示範頁 放進群組" })
      .selectOption({ label: groupName });
    await expect(group.locator('[data-item="demo.home"]')).toBeVisible();
    // 上移：示範頁排到聊天前面。
    await group.getByRole("button", { name: "上移 示範頁" }).click();
    await expect(group.locator("[data-item]").first()).toHaveAttribute("data-item", "demo.home");
    // 換圖示。
    await group.getByRole("button", { name: `${groupName}的圖示` }).click();
    await page.getByRole("button", { name: "太陽" }).click();

    // 沒存之前選單還是舊的。
    const before = await menuTree(request, baseURL);
    expect(before.tree?.some((e) => e.title === groupName)).toBe(false);
    await page.getByRole("button", { name: "儲存" }).click();
    await expect(page.getByText("有未儲存的變更")).toHaveCount(0);

    // 存了：伺服器的選單變了。
    await expect
      .poll(async () =>
        (await menuTree(request, baseURL)).tree
          ?.find((e) => e.title === groupName)
          ?.children?.map((c) => c.id),
      )
      .toEqual(["demo.home", "core.chat"]);
    // 桌機的側邊欄不用重新整理就換成新的群組。
    if (!isMobile) {
      await expect(mainNav(page).getByRole("button", { name: `收合 ${groupName}` })).toBeVisible();
      await expect(mainNav(page).getByRole("link", { name: "示範頁" })).toBeVisible();
    }
    // 重新整理，編輯器讀回剛存的。
    await page.reload();
    await expect(
      page.locator('[data-group="group1"]').getByRole("textbox", { name: "群組名稱" }),
    ).toHaveValue(groupName);
    await expect(page.getByRole("button", { name: "儲存" })).toBeDisabled();

    // 刪群組：底下的項目回到「沒放進選單」。
    await page
      .locator('[data-group="group1"]')
      .getByRole("button", { name: `刪除群組：${groupName}` })
      .click();
    await expect(page.getByTestId("menu-unplaced-row").filter({ hasText: "聊天" })).toBeVisible();
    // 還原成預設：選單回到預設的樣子。
    await page.getByRole("button", { name: "還原成預設" }).click();
    await expect
      .poll(async () => (await menuTree(request, baseURL)).tree?.some((e) => e.title === groupName))
      .toBe(false);
    await expect(page.locator('[data-group="daily"]')).toContainText("聊天");
  } finally {
    await resetMenu(request, baseURL);
  }
});

// Guards: REQ-141
test("TestEditorBlocksThirdLevel", async ({ page, request, baseURL }) => {
  await resetMenu(request, baseURL);
  try {
    await page.goto("/settings/menu");
    const work = page.locator('[data-group="work"]');
    await expect(work).toBeVisible();
    // 群組那一列不給「放進群組…」（一定會失敗的選項不出現，WEB-105）；群組裡的項目照樣有。
    await expect(page.getByRole("combobox", { name: "把 工作 放進群組" })).toHaveCount(0);
    await expect(page.getByRole("combobox", { name: "把 日常 放進群組" })).toHaveCount(0);
    await expect(
      page.locator('[data-group="daily"] [data-item="core.tasks"]').getByRole("combobox"),
    ).toHaveCount(1);
    await expect(page.getByText("有未儲存的變更")).toHaveCount(0);
    await expect(page.locator('[data-group="daily"] [data-group]')).toHaveCount(0);
    // 樹不變：工作群組還在頂層，任務單照預設在「日常」（F6 起）。
    await expect(work).toBeVisible();
    await expect(page.locator('[data-group="daily"] [data-item="core.tasks"]')).toBeVisible();
    await expect(page.getByRole("button", { name: "儲存" })).toBeDisabled();
    // core.settings 沒有放進群組與移出的控制。
    const settings = page.locator('[data-item="core.settings"]');
    await expect(settings).toContainText("固定在選單裡，不能移除");
    await expect(settings.getByRole("combobox")).toHaveCount(0);
    await expect(settings.getByRole("button", { name: /移出群組/ })).toHaveCount(0);
    // 伺服器也擋：第三層的偏好存不進去。
    const bad = await call<{ code?: string; field?: string }>(
      request,
      baseURL,
      "web/set_menu_prefs",
      {
        prefs: {
          tree: [
            { group: "a", title: "甲", children: ["b"] },
            { group: "b", title: "乙", children: [] },
          ],
        },
      },
    );
    expect(bad.status).toBe(400);
    expect(bad.json.code).toBe("invalid");
  } finally {
    await resetMenu(request, baseURL);
  }
});

// 桌機可以拖拉：把項目拖到群組標題上＝放進去；把群組拖到群組標題上＝被擋下。
// Guards: REQ-141
test("TestEditorDragDropDesktop", async ({ page, request, baseURL, isMobile }) => {
  await resetMenu(request, baseURL);
  try {
    await page.goto("/settings/menu");
    if (isMobile) {
      // 手機沒有拖拉（只有上移、下移與下拉）。
      await expect(page.getByTestId("menu-group").first()).toBeVisible();
      await expect(page.locator("[draggable]")).toHaveCount(0);
      return;
    }
    const work = page.locator('[data-group="work"]');
    await expect(work).toBeVisible();
    const header = work.locator("div[draggable]").first();
    await page.locator('[data-item="core.messages"]').dragTo(header);
    await expect(work.locator('[data-item="core.messages"]')).toBeVisible();
    await expect(page.getByText("有未儲存的變更")).toBeVisible();
    // 群組拖進群組：擋下。
    await page
      .locator('[data-group="work"] div[draggable]')
      .first()
      .dragTo(page.locator('[data-group="daily"] div[draggable]').first());
    await expect(page.getByText("選單最多兩層：群組裡不能再放群組")).toBeVisible();
    await expect(page.locator('[data-group="daily"] [data-group]')).toHaveCount(0);
  } finally {
    await resetMenu(request, baseURL);
  }
});

// 自訂選單元件那一行：預設時寫「預設」且沒有「改回預設選單」；用了自己的元件時有，按了就改回預設。
// Guards: REQ-141
test("TestMenuRendererLine", async ({ page, request, baseURL }) => {
  await resetMenu(request, baseURL);
  try {
    await page.goto("/settings/menu");
    await expect(page.getByTestId("menu-renderer")).toContainText("預設");
    await expect(page.getByRole("button", { name: "改回預設選單" })).toHaveCount(0);
    await call(request, baseURL, "web/set_menu_prefs", {
      prefs: { tree: [], renderer: "demo.menu" },
    });
    await page.goto("/settings/menu?menu=default");
    await expect(page.getByTestId("menu-renderer")).toContainText("demo.menu");
    await page.getByRole("button", { name: "改回預設選單" }).click();
    await expect(page.getByTestId("menu-renderer")).toContainText("預設");
    const prefs = await call<{ prefs: { renderer?: string } }>(
      request,
      baseURL,
      "web/get_menu_prefs",
    );
    expect(prefs.json.prefs.renderer ?? "").toBe("");
  } finally {
    await resetMenu(request, baseURL);
  }
});

async function defaultLevel(request: APIRequestContext, base: string | undefined) {
  const { json } = await call<{ default_level: string }>(request, base, "system/trust");
  return json.default_level;
}

// 設定精靈兩步（REQ-238）：①選職位、每個職位選角色（只有負責的人與開關）；②開始用（推播只是一行選用）。
// Guards: REQ-111, REQ-238
test("TestSetupWizardTwoSteps", async ({ page }) => {
  await page.goto("/settings/setup");
  for (const id of ["agents", "start"]) {
    await expect(page.getByTestId(`setup-step-${id}`)).toBeVisible();
  }
  for (const id of ["permission", "packages", "optional", "push", "mode"]) {
    await expect(page.getByTestId(`setup-step-${id}`)).toHaveCount(0);
  }
  // code、life 都啟用：第一步完成，第二步有「到今天」。
  await expect(page.getByTestId("setup-step-agents")).toContainText("完成");
  await expect(
    page.getByTestId("setup-step-agents").getByTestId("agent-model").first(),
  ).toBeHidden();
  await expect(page.getByTestId("setup-push")).toContainText("選用");
  await page.getByTestId("setup-step-start").getByRole("link", { name: "到今天" }).click();
  await expect(page).toHaveURL(/\/$/);
});

// 套件帶來、還沒人負責的職位（secretaries 的 work、system…）：選角色、按「請〈角色名字〉上班」→ 名冊多一位啟用的秘書（REQ-238）。
// Guards: REQ-238
test("TestClaimFunctionFromTemplate", async ({ page, request, baseURL, seed }) => {
  await page.goto("/settings/agents");
  const card = page.locator('[data-testid="function-unclaimed"][data-agent="system"]');
  await expect(card).toContainText("電腦");
  await expect(card).toContainText("職位 system");
  await card.getByTestId("claim-persona").selectOption("helper");
  await card.getByRole("button", { name: "請Helper上班" }).click();
  const row = page.locator('[data-testid="agent-row"][data-agent="system"]');
  await expect(row).toBeVisible();
  await expect(row.getByRole("switch")).toHaveAttribute("aria-checked", "true");
  await expect(card).toHaveCount(0);
  const agent = async () =>
    (
      await call<{ agents: { id: string; enabled: boolean; persona?: string; duty?: string }[] }>(
        request,
        baseURL,
        "web/get_agents",
      )
    ).json.agents.find((a) => a.id === "system");
  await expect.poll(async () => (await agent())?.persona).toBe("helper");
  expect((await agent())?.duty).toContain("電腦");
  // 收尾：停用（同一個 worker 的別的測試看的是原本的名冊）。
  await seed.api("/api/agents/set_field", {
    id: "system",
    field: "enabled",
    value: "false",
    by: { name: "owner(web)" },
  });
});

// 整個家的預設權限在「權限」分頁改（2026-10-28 從設定精靈搬來）；三個選項逐字照命令列精靈。
// Guards: REQ-110, REQ-238
test("TestTrustSetsDefaultLevel", async ({ page, request, baseURL }) => {
  const original = await defaultLevel(request, baseURL);
  try {
    await page.goto("/settings/trust");
    const choices = page.getByTestId("trust-default-choices");
    await expect(choices).toContainText("全部讓 AI 幫我做");
    await expect(choices).toContainText(
      "秘書可以自己改檔案、跑指令、改系統設定，不會先問你。最省事，但做錯事時你事後才會知道。",
    );
    await expect(choices).toContainText("做大事先問我");
    await expect(choices).toContainText("只讓 AI 看不讓它改");
    const target = original === "readonly" ? "standard" : "readonly";
    const card = choices.locator(`[data-level="${target}"]`);
    await card.click();
    await expect(card).toHaveAttribute("aria-pressed", "true");
    await expect.poll(() => defaultLevel(request, baseURL)).toBe(target);
    await page.reload();
    await expect(
      page.getByTestId("trust-default-choices").locator(`[data-level="${target}"]`),
    ).toHaveAttribute("aria-pressed", "true");
  } finally {
    await call(request, baseURL, "system/set_permission_levels", { default_level: original });
  }
});

// Guards: REQ-110
test("TestTrustShowsLevels", async ({ page, request, baseURL }) => {
  const agentLevel = async (id: string) =>
    (
      await call<{ agents: { agent: string; level: string; source: string }[] }>(
        request,
        baseURL,
        "system/trust",
      )
    ).json.agents.find((a) => a.agent === id);
  try {
    await page.goto("/settings/trust");
    await expect(page.getByText("每位秘書的等級", { exact: true })).toBeVisible();
    await expect(page.getByTestId("trust-default-level")).toContainText("整個家的預設：");
    const rows = page.getByTestId("trust-level-row");
    // 名冊上的秘書一位一列（同一個 worker 的別的測試可能多造了幾位，所以不算死數量）。
    await expect.poll(() => rows.count()).toBeGreaterThanOrEqual(2);
    await expect(rows.filter({ hasText: "Helper" }).first()).toBeVisible();
    const row = rows.filter({ hasText: "Builder" });
    await expect(row.getByRole("button", { name: "跟預設" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    // 改成「唯讀」：馬上存，這位的單獨設定出現。
    await row.getByRole("button", { name: "唯讀" }).click();
    await expect(row.getByRole("button", { name: "唯讀" })).toHaveAttribute("aria-pressed", "true");
    await expect.poll(async () => (await agentLevel("code"))?.source).toBe("agent");
    expect((await agentLevel("code"))?.level).toBe("readonly");
    await page.reload();
    await expect(
      page
        .getByTestId("trust-level-row")
        .filter({ hasText: "Builder" })
        .getByRole("button", { name: "唯讀" }),
    ).toHaveAttribute("aria-pressed", "true");
    // 改回「跟預設」：拿掉單獨設定。
    await page
      .getByTestId("trust-level-row")
      .filter({ hasText: "Builder" })
      .getByRole("button", { name: "跟預設" })
      .click();
    await expect.poll(async () => (await agentLevel("code"))?.source).not.toBe("agent");
    await expect(page.getByText("改了之後，秘書下次開對話才生效。")).toBeVisible();
    // 原本的唯讀內容還在。
    await expect(page.getByTestId("trust-repo")).toHaveCount(2);
  } finally {
    await call(request, baseURL, "system/set_permission_levels", {
      levels: [{ agent: "code", level: "" }],
    });
  }
});

// MNU-10：預設收起權限申請，設定頁點得進去；逐項切回來、再收一項也存得住；今天與設定不能收。
test("TestMenuTuckToSettings", async ({ page, request, baseURL, isMobile }) => {
  await resetMenu(request, baseURL);
  try {
    await page.goto("/settings/menu");
    await expect(
      page.locator('[data-testid="menu-tucked-row"][data-item="core.policy"]'),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "把 今天 收到設定" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "把 設定 收到設定" })).toHaveCount(0);

    // 桌機在左邊的分頁清單下面；手機在設定首頁清單的「進階」裡（DEV-156 U-12）。
    const tucked = isMobile
      ? page.getByTestId("settings-home").getByRole("region", { name: "進階", exact: true })
      : page.getByTestId("settings-tucked");
    if (isMobile) await page.goto("/settings");
    await tucked.getByRole("link", { name: /權限申請/ }).click();
    await expect.poll(() => new URL(page.url()).pathname).toBe("/policy");

    await page.goto("/settings/menu");
    await page.getByRole("button", { name: "把 訊息 收到設定" }).click();
    await page.getByRole("button", { name: "把 權限申請 放回選單" }).click();
    await page.getByRole("button", { name: "儲存" }).click();
    await expect(page.getByRole("button", { name: "儲存" })).toBeDisabled();
    await expect
      .poll(async () => {
        const flat = JSON.stringify((await menuTree(request, baseURL)).tree);
        return flat.includes("core.policy") && !flat.includes("core.messages");
      })
      .toBe(true);
    if (isMobile) await page.goto("/settings");
    await expect(tucked.getByRole("link", { name: /訊息/ })).toBeVisible();
  } finally {
    await resetMenu(request, baseURL);
  }
});
