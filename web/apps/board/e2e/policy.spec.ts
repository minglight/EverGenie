import { expect, shot, test } from "./fixtures";

const rand = () => Math.random().toString(36).slice(2, 8);

// 權限申請頁（REQ-169）：秘書送的申請出現在待審，主人核准後移到已授權、可以撤銷；過寬的有紅字警語。
// Guards: REQ-169
test("TestPolicyPageApproveAndRevoke @smoke", async ({ page, seed, isMobile }) => {
  const scope = `~/Workspace/amy/e2e-${rand()}`;
  await seed.api("/api/policy/request", {
    kind: "rule",
    action: "write",
    scope,
    why: "改檔案被擋了兩次",
    risk: "只限這個資料夾",
    by: { name: "code" },
  });
  await seed.api("/api/policy/request", {
    kind: "rule",
    action: "read",
    why: `要讀別的地方 ${rand()}`,
    by: { name: "code" },
  });
  await page.goto("/policy");
  const card = page.getByTestId("policy-request").filter({ hasText: scope });
  await expect(card).toBeVisible();
  await expect(card.getByText("改檔案被擋了兩次")).toBeVisible();
  await expect(page.getByText("範圍很寬").first()).toBeVisible();
  await shot(page, `policy-pending-${isMobile ? "mobile" : "desktop"}`);

  await card.getByRole("button", { name: "核准", exact: true }).click();
  await expect(card).toHaveCount(0);
  // POL-14：卡片離開待審時有一句結果。
  await expect(page.getByText(/已核准 .+，在「已授權」看得到/)).toBeVisible();
  await page.getByRole("button", { name: "已授權", exact: true }).click();
  const applied = page.getByTestId("policy-request").filter({ hasText: scope });
  await expect(applied.getByText("已授權", { exact: true })).toBeVisible();
  await expect(applied.getByText("一直有效", { exact: true })).toBeVisible();
  await applied.getByRole("button", { name: "撤銷這個授權" }).click();
  await applied.getByRole("button", { name: "確定撤銷" }).click();
  await expect(applied).toHaveCount(0);
});

// POL-15：按核准時那張已經在別處被決定了——送出失敗、清單重抓、卡片離開待審，原因要用 toast 留著，不能跟著卡片一起消失。
test("TestPolicyDecideFailureShowsToast", async ({ page, seed }) => {
  const scope = `~/Workspace/amy/e2e-${rand()}`;
  const res = await seed.api<{ request: { id: string } }>("/api/policy/request", {
    kind: "rule",
    action: "write",
    scope,
    why: "要改檔案",
    by: { name: "code" },
  });
  await page.goto("/policy");
  const card = page.getByTestId("policy-request").filter({ hasText: scope });
  await expect(card).toBeVisible();
  // 畫面送出核准之前，先在別處把它拒絕掉（像另一台裝置剛按了）。
  await page.route("**/api/policy/decide", async (route) => {
    await seed.api("/api/policy/decide", {
      id: res.request.id,
      approve: false,
      by: { name: "owner(web)" },
    });
    await route.continue();
  });
  await card.getByRole("button", { name: "核准", exact: true }).click();
  await expect(card).toHaveCount(0);
  await expect(page.getByText(/沒送出去：這張申請已經是「.+」，不能再審/)).toBeVisible();
});
