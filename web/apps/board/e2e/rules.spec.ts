import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { expect, test } from "./fixtures";

const rand = () => Math.random().toString(36).slice(2, 8);

// 規矩頁（S7、D-44）：家裡的憲法、宗旨、法律一條一條呈現；主人編輯存檔會留版本、歷史看得到差異；
// 秘書送的修改提案出現在最上面，主人採用後文件跟著改。
async function seedDocs(home: string, tag: string) {
  const docs = path.join(home, "docs");
  await mkdir(path.join(docs, "projects"), { recursive: true });
  await writeFile(
    path.join(docs, "constitution.md"),
    `# 憲法\n\n## 第 1 條 先問主人 ${tag}\n\n做不能退回的事之前先問。\n\n## 第 2 條 留紀錄\n\n每件事都要留紀錄。\n`,
  );
  await writeFile(
    path.join(docs, "projects", "evergenie.md"),
    `# EverGenie\n\n## 宗旨\n\n讓主人少操心 ${tag}。\n\n## 現在在跑的\n\n- 看板\n\n## 法律\n\n### 第 1 條 一件事做到底\n\n不要丟給別人。\n`,
  );
  return docs;
}

test("TestRulesPageEditAndHistory", async ({ page, seed, hgServer, isMobile }) => {
  test.skip(isMobile, "手機只驗 360 寬讀得了（下一個測試）");
  const tag = rand();
  await seedDocs(hgServer.home, tag);

  await page.goto("/settings/rules");
  await expect(page.getByRole("button", { name: /^憲法/ })).toBeVisible();
  await expect(page.getByText(`第 1 條 先問主人 ${tag}`)).toBeVisible();
  await expect(page.getByText("每件事都要留紀錄。")).toBeVisible();

  // 法律：編輯、存檔，內容跟著變。
  await page.getByRole("button", { name: /^法律/ }).click();
  await expect(page.getByText("第 1 條 一件事做到底")).toBeVisible();
  await page.getByRole("button", { name: "編輯" }).click();
  const box = page.getByLabel("內容（Markdown）");
  await expect(box).toHaveValue(/不要丟給別人。/);
  await box.fill(
    `### 第 1 條 一件事做到底\n\n不要丟給別人。\n\n### 第 2 條 新加的 ${tag}\n\n驗收條件要寫清楚。`,
  );
  await page.getByLabel("改了什麼（選填）").fill(`加第 2 條 ${tag}`);
  await page.getByRole("button", { name: "存檔" }).click();
  await expect(page.getByText(`第 2 條 新加的 ${tag}`)).toBeVisible();

  // 歷史：改之前的樣子＋這次存檔兩版；打開最新一版看得到加上的那行。
  await page.getByRole("button", { name: "歷史" }).click();
  const history = page.getByRole("list", { name: "歷史" });
  await expect(history.getByRole("listitem")).toHaveCount(2);
  await expect(history.getByText("改之前的樣子")).toBeVisible();
  await history.getByRole("button", { name: new RegExp(`加第 2 條 ${tag}`) }).click();
  const diff = page.getByRole("region", { name: /跟前一版的差別/ });
  await expect(
    diff.locator('[data-op="add"]').filter({ hasText: `第 2 條 新加的 ${tag}` }),
  ).toBeVisible();
  await expect(history.getByText("這一版跟現在的文件一樣。")).toBeVisible();

  // 秘書的提案：出現在最上面，採用後宗旨換成新內容。
  await seed.api("/api/rules/propose", {
    doc: "purpose",
    text: `讓主人少操心，也少花錢 ${tag}。`,
    why: `主人說過要省錢 ${tag}`,
    by: { name: "code" },
  });
  await page.goto("/settings/rules?doc=purpose");
  const card = page.getByRole("group", { name: /^修改提案 RP-/ }).filter({ hasText: tag });
  await expect(card.getByText(`主人說過要省錢 ${tag}`)).toBeVisible();
  await card.getByRole("button", { name: "採用", exact: true }).click();
  await expect(card).toHaveCount(0);
  await expect(page.getByText(`讓主人少操心，也少花錢 ${tag}。`)).toBeVisible();
});

// 編輯到一半文件在別處被改了（這裡直接改檔）：存檔回衝突、不蓋掉；「重新開始」丟掉草稿、拿最新的內容。
test("TestRulesSaveConflictsWhenDocChangedWhileEditing", async ({ page, hgServer, isMobile }) => {
  test.skip(isMobile, "桌機驗就好");
  const tag = rand();
  const docs = await seedDocs(hgServer.home, tag);
  await page.goto("/settings/rules?doc=constitution");
  await page.getByRole("button", { name: "編輯" }).click();
  await page.getByLabel("內容（Markdown）").fill(`# 憲法\n\n## 第 1 條 我的版本 ${tag}`);
  const outside = `# 憲法\n\n## 第 1 條 別處改的 ${tag}\n`;
  await writeFile(path.join(docs, "constitution.md"), outside);
  await page.getByRole("button", { name: "存檔" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "改過了" })).toBeVisible();
  expect(await readFile(path.join(docs, "constitution.md"), "utf8")).toBe(outside);
  await page.getByRole("button", { name: "重新開始" }).click();
  await page.getByRole("button", { name: "丟掉", exact: true }).click();
  await expect(page.getByText(`第 1 條 別處改的 ${tag}`)).toBeVisible();
});

// 手機 360 寬：看得到條文、按得到編輯，整頁不能左右捲。
test("TestRulesPageReadableAt360", async ({ page, hgServer, isMobile }) => {
  test.skip(!isMobile, "手機專屬");
  const tag = rand();
  await seedDocs(hgServer.home, tag);
  await page.setViewportSize({ width: 360, height: 780 });
  await page.goto("/settings/rules");
  await expect(page.getByText(`第 1 條 先問主人 ${tag}`)).toBeVisible();
  await expect(page.getByRole("button", { name: "編輯" })).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
    ),
  ).toBe(false);
});
