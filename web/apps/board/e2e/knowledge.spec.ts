import { expect, shot, test } from "./fixtures";

// 設定 → 職位與秘書：每個職位一塊「她帶的知識」（DEV-161）。
// 第一支用假的回應（可以精準驗勾選的版面與送出的內容）；第二支接真的 /api/agents/knowledge、set_knowledge。

const blocks = [
  { id: "parenting", title: "育兒", summary: "依孩子的年齡陪你想", default: true },
  {
    id: "balance",
    title: "工作與生活的平衡",
    summary: "工作吃掉家庭、睡眠與自己的時間時，提醒一句",
    default: false,
  },
];

// Guards: DEV-161
test("TestSettingsAgentKnowledgeChecklist", async ({ page }, info) => {
  let enabled = ["parenting"];
  const saves: string[][] = [];
  await page.route("**/api/agents/knowledge", async (route) => {
    const agent = (route.request().postDataJSON() as { agent: string }).agent;
    await route.fulfill({
      json: {
        agent,
        blocks:
          agent === "life" ? blocks.map((b) => ({ ...b, enabled: enabled.includes(b.id) })) : [],
      },
    });
  });
  await page.route("**/api/agents/set_knowledge", async (route) => {
    const body = route.request().postDataJSON() as { agent: string; enabled: string[] };
    enabled = body.enabled;
    saves.push(body.enabled);
    await route.fulfill({
      json: {
        agent: body.agent,
        blocks: blocks.map((b) => ({ ...b, enabled: enabled.includes(b.id) })),
      },
    });
  });

  await page.goto("/settings/agents");
  const card = page.locator('[data-testid="agent-row"][data-agent="life"]');
  const picker = card.getByTestId("knowledge");
  await expect(picker).toBeVisible();
  await expect(picker).toContainText("她帶的知識");
  await expect(picker).toContainText("第一天上班她也會跟你聊一下再幫你調。");
  // 沒有可選知識的職位（code）不出這塊。
  await expect(
    page.locator('[data-testid="agent-row"][data-agent="code"]').getByTestId("knowledge"),
  ).toHaveCount(0);

  const balance = picker.locator('[data-block="balance"]');
  await expect(picker.locator('[data-block="parenting"]')).toBeChecked();
  await expect(balance).not.toBeChecked();
  await balance.check();
  await expect.poll(() => saves.at(-1)).toEqual(["parenting", "balance"]);
  await expect(
    page.locator('[data-testid="agent-row"][data-agent="life"]').getByTestId("knowledge-saved"),
  ).toContainText("已存。下次開對話才生效。");
  await expect(balance).toBeChecked();

  // 每一列至少 44px、頁面不左右捲（手機 390 寬也是）。
  for (const row of await picker.locator("label").all()) {
    expect((await row.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(44);
  }
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
  await picker.scrollIntoViewIfNeeded();
  await shot(page, `settings-knowledge-${info.project.name}`);
});

// 真 API 的往返：勾了存進家裡，重新整理還在。
test("TestAgentKnowledgeRoundTripWithRealApi", async ({ page, seed }) => {
  const before = await seed.api<{ blocks: { id: string; enabled: boolean }[] }>(
    "/api/agents/knowledge",
    { agent: "life" },
  );
  expect(before.blocks.length).toBeGreaterThan(0);
  await page.goto("/settings/agents");
  const picker = page
    .locator('[data-testid="agent-row"][data-agent="life"]')
    .getByTestId("knowledge");
  const first = picker.locator("input[type=checkbox]").first();
  const was = await first.isChecked();
  await first.setChecked(!was);
  await expect(
    page.locator('[data-testid="agent-row"][data-agent="life"]').getByTestId("knowledge-saved"),
  ).toContainText("已存。下次開對話才生效。");
  await page.reload();
  await expect(
    page
      .locator('[data-testid="agent-row"][data-agent="life"]')
      .getByTestId("knowledge")
      .locator("input[type=checkbox]")
      .first(),
  ).toBeChecked({ checked: !was });
});
