import { existsSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import type { Page } from "@playwright/test";
import { expect, test } from "./fixtures";

// 「角色」頁的介紹（P6-5a，REQ-130、D-31；DEV-117 從設定搬到 /personas）：官方卡唯讀、我的卡可編輯，新卡先是設計中、檢查過才上線；
// 說話層強度 A–D 與自己的；設計中的角色不能指派。DEV-143 起一位角色一頁：清單只放小卡，點進去是 /personas/<代號>，
// 上面分頁（介紹、套件掛的分頁，記在 ?tab=），擔任哪個職位、匯出也在介紹裡。同一個 worker 的家是共用的：每個測試用自己的代號（隨機字尾）。
// 自己的角色代號一律 my_<名字>（DEV-154、PRF-1）：API 打名字或 my_<名字> 都收，回來的、檔名、網址都是 my_ 開頭。
const rand = () =>
  Math.random()
    .toString(36)
    .slice(2, 8)
    .replace(/[^a-z0-9]/g, "x");

const owner = { name: "owner(web)" };

const filledBody = [
  "## 個性",
  "- 一句話：安靜又可靠的夜班同事。",
  "- 性格：慢熱，但很準。",
  "",
  "## 說話",
  "- 語氣：平穩，句尾偶爾帶一點笑意。",
].join("\n");

const row = (page: Page, slug: string) =>
  page.locator(`[data-testid="persona-row"][data-slug="${slug}"]`);
const style = (page: Page, level: string) =>
  page.locator(`[data-testid="persona-style-option"][data-level="${level}"]`);

// 手機一打開角色庫就看得到角色，說話層強度排在角色清單下面（WEB-108）。
test("TestPersonaLibraryCardsBeforeStyleLevels", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/personas");
  const first = row(page, "builder");
  await expect(first).toBeVisible();
  await expect(first).toBeInViewport();
  const rowBox = await first.boundingBox();
  const styleBox = await page.locator("#persona-style-title").boundingBox();
  expect(rowBox && styleBox && rowBox.y < styleBox.y).toBe(true);
});

// Guards: REQ-130
test("TestPersonaLibraryCreateEditLive @smoke", async ({ page, hgServer }) => {
  // 主人在代號那格只打名字，伺服器記成 my_<名字>（DEV-154、PRF-1）。
  const typed = `e2e${rand()}`;
  const slug = `my_${typed}`;
  const name = `夜班${rand()}`;
  const card = path.join(hgServer.home, "personas", "mine", `${slug}.md`);

  await page.goto("/personas");
  await expect(row(page, "builder")).toBeVisible();

  // 新增：建好直接進她的頁，是設計中、內文是範本。有套件掛新增方式（closet-mine 的「上傳半身照」）時先選「只寫角色卡」。
  // 「新增角色」收在清單右上的「⋯」（DEV-156 U-15）。
  await page.getByTestId("persona-list-menu").click();
  await page.getByTestId("persona-new-open").click();
  const cardOnly = page.getByRole("dialog").getByRole("button", { name: "只寫角色卡" });
  // 套件的新增方式是晚一步才掛上來的：等一下它，不然剛好還沒出現就往下走，之後畫面換成「上傳半身照」，游標不在表單裡。
  await cardOnly.waitFor({ timeout: 3_000 }).catch(() => {});
  if (await cardOnly.count()) await cardOnly.click();
  // 對話框打開後才把游標放到第一格；先等它落定再填，不然負載高時後面幾格的字會被打進代號（DEV-11）。
  // 第一格是代號（SEC-6 拿掉了「放在哪裡」）。
  await expect(page.getByRole("dialog").locator(":focus")).toHaveCount(1);
  await page.getByLabel("代號").fill(typed);
  await page.getByLabel("名字").fill(name);
  await page.getByLabel("別名").fill("小夜, nightly");
  await page.getByRole("button", { name: "建立", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/personas/${slug}$`));
  const detail = page.getByTestId("persona-detail");
  await expect(detail.getByText("設計中", { exact: true })).toBeVisible();
  await expect(detail.getByLabel("內文")).toHaveValue(/## 個性/);
  expect(existsSync(card)).toBe(true);

  // 範本還沒填：檢查會說還有空格；設計中可以直接上線的前提是沒有撞名與格式問題，這裡先填好再上線。
  await detail.getByRole("button", { name: "檢查這張卡" }).click();
  await expect(page.getByTestId("persona-check-result")).toContainText("還沒填完");

  // 改內文、存檔；重新整理後還在。
  await detail.getByLabel("內文").fill(filledBody);
  await detail.getByRole("button", { name: "存檔" }).click();
  await expect(detail.getByText("已儲存", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByTestId("persona-detail").getByLabel("內文")).toHaveValue(filledBody);
  expect(readFileSync(card, "utf8")).toContain("安靜又可靠的夜班同事");

  await detail.getByRole("button", { name: "檢查這張卡" }).click();
  await expect(page.getByTestId("persona-check-result")).toContainText("沒有問題");

  // 有還沒存的修改時不能上線；存好以後按一次展開確認，確定才上線。
  await detail.getByLabel("內文").fill(`${filledBody}\n- 小癖好：喝熱茶。`);
  await expect(detail.getByTestId("persona-go-live")).toBeDisabled();
  await detail.getByRole("button", { name: "存檔" }).click();
  await expect(detail.getByText("已儲存", { exact: true })).toBeVisible();
  await detail.getByTestId("persona-go-live").click();
  await expect(detail.getByText("上線後就能指派給秘書了", { exact: false })).toBeVisible();
  await detail.getByRole("button", { name: "取消" }).click();
  await expect(detail.getByText("設計中", { exact: true })).toBeVisible();
  await detail.getByTestId("persona-go-live").click();
  await detail.getByRole("button", { name: "確定上線" }).click();
  await expect(detail.getByText("設計中", { exact: true })).toHaveCount(0);
  await expect(detail.getByTestId("persona-go-design")).toBeVisible();

  // 頁頂：別名。
  await expect(detail).toContainText("別名：小夜、nightly");
  // 清單：在「我的」那一區、上線了（沒有設計中的小標籤）；點小卡回到她的頁。
  await page.getByRole("link", { name: "上一頁" }).click();
  await expect(page).toHaveURL(/\/personas$/);
  const mineGroup = page.locator('[data-testid="persona-group"][data-group="mine"]');
  await expect(mineGroup.locator(`[data-testid="persona-row"][data-slug="${slug}"]`)).toContainText(
    name,
  );
  await expect(row(page, slug)).not.toContainText("設計中");
  await row(page, slug).click();
  await expect(page).toHaveURL(new RegExp(`/personas/${slug}$`));
});

// Guards: REQ-130
test("TestPersonaEditConflictAsksReload", async ({ page, hgServer, seed }) => {
  const slug = `my_e2e${rand()}`;
  await seed.api("/api/personas/create", { slug, name: `衝突${rand()}`, by: owner });
  const card = path.join(hgServer.home, "personas", "mine", `${slug}.md`);
  await page.goto(`/personas/${slug}`);
  const detail = page.getByTestId("persona-detail");
  await expect(detail.getByLabel("內文")).toHaveValue(/## 個性/);

  // 我開著編輯的時候，卡片檔被別人改了。
  const raw = readFileSync(card, "utf8");
  await new Promise((r) => setTimeout(r, 30));
  writeFileSync(card, `${raw}\n- 別人加的一行`);
  await detail.getByLabel("內文").fill("我的修改");
  await detail.getByRole("button", { name: "存檔" }).click();
  const alert = detail.getByRole("alert");
  await expect(alert).toContainText("被別人改過");
  expect(readFileSync(card, "utf8")).toContain("別人加的一行");
  await alert.getByRole("button", { name: "重新載入" }).click();
  await expect(detail.getByLabel("內文")).toHaveValue(/別人加的一行/);
  await expect(detail.getByRole("alert")).toHaveCount(0);
});

// Guards: REQ-130
test("TestOfficialPersonaReadOnlyAndCopy", async ({ page, hgServer }) => {
  const official = path.join(hgServer.home, "personas", "builder.md");
  const before = readFileSync(official, "utf8");
  // 主人在代號那格只打名字，伺服器記成 my_<名字>（DEV-154、PRF-1）。
  const typed = `e2e${rand()}`;
  const slug = `my_${typed}`;

  await page.goto("/personas/builder");
  const detail = page.getByTestId("persona-detail");
  await expect(detail.getByText("官方", { exact: true })).toBeVisible();
  await expect(detail.getByText("官方角色是唯讀的")).toBeVisible();
  await expect(detail.getByTestId("persona-body-readonly")).toContainText("人造角色");
  await expect(detail.getByRole("textbox")).toHaveCount(0);
  await expect(detail.getByRole("button", { name: "存檔" })).toHaveCount(0);

  // 複製成我的：名字照抄會撞名，寫在名字欄位下；換個名字就建好、進到新卡的編輯。
  await detail.getByRole("button", { name: "複製成我的" }).click();
  // 對話框打開後才把游標放到第一格；先等它落定再填，不然負載高時後面幾格的字會被打進代號（DEV-11）。
  // 第一格是代號（SEC-6 拿掉了「放在哪裡」）。
  await expect(page.getByRole("dialog").locator(":focus")).toHaveCount(1);
  await page.getByLabel("代號").fill(typed);
  await page.getByLabel("名字").fill("Builder");
  await page.getByRole("button", { name: "建立", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "撞名" })).toBeVisible();
  await page.getByLabel("名字").fill(`副本${rand()}`);
  await page.getByRole("button", { name: "建立", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/personas/${slug}$`));
  const copy = page.getByTestId("persona-detail");
  await expect(copy.getByText("設計中", { exact: true })).toBeVisible();
  await expect(copy.getByLabel("內文")).toHaveValue(/人造角色/);

  expect(readFileSync(official, "utf8")).toBe(before);
});

// Guards: REQ-130
test("TestStyleLevelSwitch", async ({ page, hgServer, seed }) => {
  const custom = path.join(hgServer.home, "personas", "_style.md");
  rmSync(custom, { force: true });
  await seed.api("/api/personas/set_style", { level: "c", by: owner });

  await page.goto("/personas");
  await expect(style(page, "c").getByRole("button", { pressed: true })).toBeVisible();
  // 自己的：沒有 personas/_style.md 時停用並說明。
  await expect(style(page, "custom").getByText("還沒有 personas/_style.md")).toBeVisible();
  await expect(style(page, "custom").getByRole("button", { name: /自己的/ })).toBeDisabled();

  // 選 D：馬上換，重新整理還在，存進 home.toml。
  await style(page, "d")
    .getByRole("button", { name: /D\s全開/ })
    .click();
  await expect(style(page, "d").getByRole("button", { pressed: true })).toBeVisible();
  await page.reload();
  await expect(style(page, "d").getByRole("button", { pressed: true })).toBeVisible();
  expect(readFileSync(path.join(hgServer.home, "home.toml"), "utf8")).toMatch(/style\s*=\s*"d"/);

  // 看全文。
  await style(page, "d").getByRole("button", { name: "看全文" }).click();
  await expect(style(page, "d").getByTestId("persona-style-text")).toContainText("說話層（強度 D");
  await style(page, "d").getByRole("button", { name: "收起全文" }).click();
  await expect(style(page, "d").getByTestId("persona-style-text")).toHaveCount(0);

  // 家裡有了 personas/_style.md，「自己的」才能選。
  writeFileSync(custom, "# 自己的說話層\n\n用我自己的口吻。\n");
  try {
    await page.reload();
    const mine = style(page, "custom").getByRole("button", { name: /自己的/ });
    await expect(mine).toBeEnabled();
    await mine.click();
    await expect(style(page, "custom").getByRole("button", { pressed: true })).toBeVisible();
    await style(page, "custom").getByRole("button", { name: "看全文" }).click();
    await expect(style(page, "custom").getByTestId("persona-style-text")).toContainText(
      "用我自己的口吻",
    );
  } finally {
    rmSync(custom, { force: true });
    await seed.api("/api/personas/set_style", { level: "c", by: owner });
  }
});

// Guards: REQ-130
test("TestDesignPersonaHiddenFromAssign", async ({ page, seed }) => {
  const slug = `my_e2e${rand()}`;
  const name = `設計${rand()}`;
  const id = `ds${Date.now().toString(36)}`;
  await seed.api("/api/personas/create", { slug, name, by: owner });
  // 用一位自己造的秘書來換角色（不動 code、life）。
  await seed.api("/api/agents/create", { id, persona: "helper", duty: "e2e 測試用", by: owner });

  await page.goto("/settings/agents");
  const select = page.locator(`[data-agent="${id}"]`).getByTestId("agent-persona");
  await expect(select).toBeVisible();
  const options = select.locator("option");
  await expect(options.filter({ hasText: "Builder" })).toHaveCount(1);
  await expect(options.filter({ hasText: name })).toHaveCount(0);

  // 角色庫的詳情裡上線後，下拉就有了。
  await seed.api("/api/personas/set_status", { slug, status: "live", by: owner });
  await page.reload();
  await expect(select.locator("option").filter({ hasText: name })).toHaveCount(1);

  // 選了就換：名冊的人設變成這張卡；角色清單的小卡寫她擔任哪個職位。
  await select.selectOption(slug);
  await expect
    .poll(async () => {
      const r = await seed.api<{ record: { fields: { persona: string } } }>("/api/vault/get", {
        kind: "agent",
        id,
      });
      return r.record.fields.persona;
    })
    .toBe(slug);
  await page.goto("/personas");
  // 寫職位的名字（職責第一段），不寫秘書的顯示名（那就是這張卡自己的名字，WEB-107）。
  await expect(row(page, slug)).toContainText("擔任 e2e 測試用");
  await expect(row(page, slug)).not.toContainText(`擔任 ${name}`);
  // 坐著的時候不能移回設計中（按鈕停用並說明）。
  await page.goto(`/personas/${slug}`);
  const detail = page.getByTestId("persona-detail");
  await expect(detail.getByTestId("persona-go-design")).toBeDisabled();
  await expect(detail.getByText("要先換成別的角色")).toBeVisible();
  await expect(detail.getByText("現在有秘書坐著（e2e 測試用）")).toBeVisible();
});

// 說話測試（P6-5b）：選座位、按「測口吻」（假引擎）→ 出現結果 → 再跑一次（換說話層等級）→ 勾兩份並排比較。
// Guards: REQ-130
test("TestPersonaToneTestRunsAndCompares", async ({ page, seed }) => {
  const slug = `my_e2e${rand()}`;
  const name = `口吻${rand()}`;
  await seed.api("/api/personas/create", { slug, name, by: owner });
  await seed
    .api("/api/personas/set_status", { slug, status: "live", by: owner })
    .catch(() => undefined);

  await page.goto(`/personas/${slug}`);
  const tone = page.getByTestId("persona-tone-test");
  await expect(tone).toBeVisible();
  await expect(tone.getByTestId("tone-draft-note")).toHaveCount(0);
  await expect(tone.getByText("還沒測過")).toBeVisible();

  // 編輯中有未存的修改：提示會用畫面上的內容測。
  await page.getByTestId("persona-detail").getByLabel("內文").fill(filledBody);
  await expect(tone.getByTestId("tone-draft-note")).toContainText("還沒存的內容");

  // 沒有秘書坐這個角色，不指定座位會被擋；選 code 的位置就能測。
  await tone.getByTestId("tone-run").click();
  await expect(tone.getByRole("alert")).toContainText("還沒坐任何秘書");
  await tone.getByTestId("tone-seat").selectOption("code");
  await tone.getByTestId("tone-run").click();
  const rows = tone.getByTestId("tone-run-row");
  await expect(rows).toHaveCount(1);
  // 跑完自動展開：六個內建情境（原本三個，加梗本的三個：DEV-164），假引擎回 "[fake] <訊息>"，{name} 是這個角色的名字。
  const result = tone.getByTestId("tone-result");
  await expect(result).toBeVisible();
  await expect(result.locator("section")).toHaveCount(6);
  await expect(result).toContainText("[fake]");
  await expect(result).toContainText(name);
  await expect(rows.first()).toHaveAttribute("data-status", "done");

  // 第二次：說話層 D。
  await expect(tone.getByTestId("tone-run")).toBeEnabled();
  await tone.getByTestId("tone-style").selectOption("d");
  await tone.getByTestId("tone-run").click();
  await expect(rows).toHaveCount(2);
  await expect(rows.first()).toContainText("等級 D");
  await expect(rows.first()).toHaveAttribute("data-status", "done");

  // 勾兩份 → 並排比較，每個情境一列，左右各一份回覆。
  await rows.nth(0).getByTestId("tone-pick").check();
  await rows.nth(1).getByTestId("tone-pick").check();
  await tone.getByTestId("tone-compare-open").click();
  const compare = tone.getByTestId("tone-compare");
  await expect(compare).toBeVisible();
  const compareRows = compare.getByTestId("tone-compare-row");
  await expect(compareRows).toHaveCount(6);
  await expect(compareRows.first().locator('[data-side="left"]')).toContainText("[fake]");
  await expect(compareRows.first().locator('[data-side="right"]')).toContainText("[fake]");
  await compare.getByRole("button", { name: "關閉比較" }).click();
  await expect(compare).toHaveCount(0);
});

// 一位角色一頁（DEV-143）：分頁記在 ?tab=、換分頁不多疊歷史（上一頁回清單）；介紹裡換職位（選了再按「換」）、匯出這位。
// 舊網址 /personas?card=<代號> 轉到 /personas/<代號>。
test("TestPersonaPageTabsAssignAndExport", async ({ page, seed }) => {
  const slug = `my_e2e${rand()}`;
  const name = `分頁${rand()}`;
  const id = `pt${Date.now().toString(36)}`;
  await seed.api("/api/personas/create", { slug, name, by: owner });
  await seed.api("/api/personas/set_status", { slug, status: "live", by: owner });
  await seed.api("/api/agents/create", { id, persona: "helper", duty: "分頁測試用", by: owner });
  await seed.api("/api/agents/set_field", { id, field: "enabled", value: "true", by: owner });
  // 停用的職位不能換角色，不在選單裡。
  const off = `po${Date.now().toString(36)}`;
  await seed.api("/api/agents/create", { id: off, persona: "helper", duty: "停用的", by: owner });

  await page.goto(`/personas?card=${slug}`);
  await expect(page).toHaveURL(new RegExp(`/personas/${slug}$`));
  const detail = page.getByTestId("persona-detail");
  await expect(detail.getByTestId("persona-tab-intro")).toBeVisible();

  // 套件掛的「衣服」分頁（closet-mine）：網址記 ?tab=，重新整理還在；上一頁直接回清單。
  await page.goto("/personas");
  await row(page, slug).click();
  const tabs = page.getByRole("navigation", { name: `${name} 的分頁` });
  await tabs.getByRole("link", { name: "衣服" }).click();
  await expect(page).toHaveURL(new RegExp(`/personas/${slug}\\?tab=closet-mine\\.outfits$`));
  await expect(
    detail.locator('[data-testid="persona-tab"][data-tab="closet-mine.outfits"]'),
  ).toBeVisible();
  await page.reload();
  await expect(page.getByTestId("mine-outfits")).toBeAttached();
  await tabs.getByRole("link", { name: "介紹" }).click();
  await expect(page).toHaveURL(new RegExp(`/personas/${slug}$`));
  await page.getByRole("link", { name: "上一頁" }).click();
  await expect(page).toHaveURL(/\/personas$/);

  // 擔任哪個職位：選了先問，按「換」才換。
  await page.goto(`/personas/${slug}`);
  const seat = page.getByTestId("persona-seat");
  await expect(seat.getByTestId("persona-seat-now")).toHaveText("還沒擔任職位");
  await expect(seat.locator(`option[value="${id}"]`)).toHaveCount(1);
  await expect(seat.locator(`option[value="${off}"]`)).toHaveCount(0);
  await seat.getByTestId("persona-seat-pick").selectOption(id);
  await expect(seat).toContainText(`把「分頁測試用」從`);
  await seat.getByTestId("persona-seat-yes").click();
  await expect(seat.getByTestId("persona-seat-now")).toHaveText("擔任 分頁測試用", {
    timeout: 15_000,
  });
  const r = await seed.api<{ record: { fields: { persona: string } } }>("/api/vault/get", {
    kind: "agent",
    id,
  });
  expect(r.record.fields.persona).toBe(slug);

  // 匯出這位：對話框一打開就勾好她。
  await detail.getByTestId("persona-export-one").click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("checkbox", { name: new RegExp(name) })).toBeChecked();
});

// 介紹分頁有還沒存的角色卡編輯時，換分頁先在畫面裡問「要放棄嗎」（FEN-23、FEN-N6）：繼續編輯就留著，放棄才換。
test("TestPersonaPageAsksBeforeDroppingUnsavedEdit", async ({ page, seed }) => {
  const slug = `my_e2e${rand()}`;
  const name = `未存${rand()}`;
  await seed.api("/api/personas/create", { slug, name, by: owner });
  await page.goto(`/personas/${slug}`);
  const detail = page.getByTestId("persona-detail");
  await expect(detail.getByLabel("內文")).toHaveValue(/## 個性/);
  await detail.getByLabel("內文").fill("改了還沒存");

  const tabs = page.getByRole("navigation", { name: `${name} 的分頁` });
  await tabs.getByRole("link", { name: "衣服" }).click();
  const ask = page.getByRole("dialog", { name: "還沒存，要放棄嗎？" });
  await expect(ask).toBeVisible();
  await ask.getByRole("button", { name: "繼續編輯" }).click();
  await expect(ask).toBeHidden();
  await expect(page).toHaveURL(new RegExp(`/personas/${slug}$`));
  await expect(detail.getByLabel("內文")).toHaveValue("改了還沒存");

  await tabs.getByRole("link", { name: "衣服" }).click();
  await ask.getByRole("button", { name: "放棄修改" }).click();
  await expect(page).toHaveURL(new RegExp(`/personas/${slug}\\?tab=closet-mine\\.outfits$`));
  // 放棄後回到介紹，看到的是伺服器上的內容，換分頁不再問。
  await tabs.getByRole("link", { name: "介紹" }).click();
  await expect(page).toHaveURL(new RegExp(`/personas/${slug}$`));
  await expect(detail.getByLabel("內文")).toHaveValue(/## 個性/);
  await expect(ask).toHaveCount(0);
});

// 手機寬度優先（FEN-7、DEV-143）：角色清單、角色頁的分頁、衣櫃與展示間的頭像列都不能讓整頁橫向捲動。
test("TestPersonaPagesFitPhoneWidth", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const fits = () =>
    page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1);
  for (const [url, testId] of [
    ["/personas", "persona-row"],
    ["/personas/builder", "persona-tab-intro"],
    ["/personas/builder?tab=closet-mine.outfits", "mine-outfits"],
    ["/p/closet/wardrobe", "closet"],
    ["/p/closet/showroom", "showroom"],
  ] as const) {
    await page.goto(url);
    await expect(page.getByTestId(testId).first()).toBeVisible({ timeout: 30_000 });
    expect(await fits(), url).toBe(true);
  }
  // 分頁的觸控目標至少 44px。
  await page.goto("/personas/builder");
  const tab = page
    .getByRole("navigation", { name: /的分頁$/ })
    .getByRole("link")
    .first();
  const box = await tab.boundingBox();
  expect(box && box.height >= 44).toBe(true);
});

// DEV-156 U-18：找不到角色要有返回鈕（跟任務單、待辦、訊息的找不到頁一樣）、不用綠色打勾、不說「代號」。
test("TestPersonaMissingHasBackButton", async ({ page }) => {
  await page.goto("/personas/no-such-persona-xyz");
  const main = page.getByRole("main");
  await expect(main.getByText("找不到這個角色")).toBeVisible();
  await expect(main).toContainText("可能已經被刪掉或改了名字");
  await expect(main).not.toContainText("代號");
  await expect(main.locator("svg.text-ok")).toHaveCount(0);
  await main.getByRole("link", { name: "回到角色" }).click();
  await expect(page).toHaveURL(/\/personas$/);
});
