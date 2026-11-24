import type { Locator, Page } from "@playwright/test";
import { expect, TEST_OWNER_LOGIN, twoHomesTest as test } from "./fixtures";

// S3-9 設定 → 電腦：「看板連的電腦」與「誰可以連進這台」，兩個家（A 的看板看 B）。
// 兩個家整個 spec 檔共用：每個測試改了 A 的台清單或 B 的信任設定，結束時都改回 fixture 的樣子（A 看 B；B 允許 A、owner_login＝TEST_OWNER_LOGIN）。
// 只跑桌機（沒有手機專屬內容，不列進 mobileSpecs）。

const OWNER = { name: "owner(cli)" };

interface SitesResp {
  owner_login?: string;
  allow_boards?: string[];
  sites?: { url?: string; color?: string }[];
}

const siteRow = (page: Page, base: string): Locator =>
  page.getByTestId("site-row").filter({ has: page.getByText(base, { exact: true }) });

function pagePost(page: Page, url: string, body: unknown) {
  return page.evaluate(
    async ({ url, body }) => {
      try {
        const res = await fetch(url, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
        return { status: res.status, json: (await res.json().catch(() => null)) as unknown };
      } catch (err) {
        return { status: 0, json: String(err) as unknown };
      }
    },
    { url, body },
  );
}

test("TestMachineSitesRemoveThenAddShowsOkAtOnce", async ({ page, homes, seed }) => {
  const b = homes.b;
  try {
    await page.addInitScript(() => {
      // 篩選裡選了 B 的秘書與 A 自己的；拿掉 B 之後只留 A 的。
      if (!sessionStorage.getItem("seeded")) {
        localStorage.setItem("eg.agentFilter.v2", JSON.stringify(["code", "amy-e2e-b/code"]));
        sessionStorage.setItem("seeded", "1");
      }
    });
    await page.goto("/settings/machine");
    const row = siteRow(page, b.url);
    await expect(row).toHaveAttribute("data-state", "ok");
    await expect(row).toContainText(b.label);
    await expect(row.getByTestId("site-version")).toBeVisible();
    // 別台的信任設定只顯示，旁邊寫「到那台的看板改」。
    const remote = row.getByTestId("site-remote-trust");
    await expect(remote).toContainText(TEST_OWNER_LOGIN);
    await expect(remote).toContainText(homes.a.url);
    await expect(remote.getByRole("link", { name: "到那台的看板改" })).toHaveAttribute(
      "href",
      `${b.url}/settings/machine`,
    );

    await row.getByRole("button", { name: `拿掉「${b.label}」` }).click();
    await row.getByRole("button", { name: "確定拿掉" }).click();
    await expect(siteRow(page, b.url)).toHaveCount(0);
    expect((await seed.api<SitesResp>("/api/web/get_sites", {})).sites ?? []).toEqual([]);
    expect(await page.evaluate(() => localStorage.getItem("eg.agentFilter.v2"))).toBe(
      JSON.stringify(["code"]),
    );

    const form = page.getByTestId("site-add");
    await form.getByLabel("那台看板的網址").fill(b.url);
    await form.getByText("綠", { exact: true }).click();
    // 拿掉前試連過的 ok 還在快取裡；清掉 B 收到的請求，才驗得到加回去時真的重新試連了。
    homes.b.proxy.forwarded.length = 0;
    await form.getByRole("button", { name: "加入" }).click();
    await expect(siteRow(page, b.url)).toHaveAttribute("data-state", "ok");
    await expect
      .poll(() => homes.b.proxy.forwarded.filter((f) => f.path === "/api/system/get_mode").length)
      .toBeGreaterThan(0);
    await expect(siteRow(page, b.url)).toContainText(b.label);
    await expect(form.getByLabel("那台看板的網址")).toHaveValue("");
    expect((await seed.api<SitesResp>("/api/web/get_sites", {})).sites).toEqual([
      { url: b.url, color: "#22c55e" },
    ]);
  } finally {
    await seed.api("/api/web/set_sites", { sites: [{ url: b.url }], by: OWNER });
  }
});

test("TestMachineSitesShowsDeniedWithServerReason", async ({ page, homes }) => {
  const b = homes.b;
  try {
    await b.seed.api("/api/web/set_owner_login", { owner_login: "someone@else.test", by: OWNER });
    await page.goto("/settings/machine");
    const row = siteRow(page, b.url);
    await expect(row).toHaveAttribute("data-state", "denied");
    await expect(row.getByTestId("site-state")).toHaveText("被拒");
    await expect(row.getByTestId("site-notice")).toContainText("這台只接受主人的裝置");
    await expect(row).toContainText(`把這個看板（${homes.a.url}）加進「誰可以連進這台」`);
    await expect(row.getByTestId("site-remote-trust")).toHaveCount(0);

    // 改回來，按「立刻試連」就回到正常（不用等 30 秒的自動重試）。
    await b.seed.api("/api/web/set_owner_login", { owner_login: TEST_OWNER_LOGIN, by: OWNER });
    await page.getByRole("button", { name: "立刻試連" }).click();
    await expect(row).toHaveAttribute("data-state", "ok");
  } finally {
    await b.seed.api("/api/web/set_owner_login", { owner_login: TEST_OWNER_LOGIN, by: OWNER });
  }
});

// 改「誰能連進來」只能在那台自己的看板做（WEB-79）：A 的頁面跨站呼叫 B 的 set_allow_boards／set_owner_login 一律 403、B 的設定沒變；
// 台清單（set_sites）跨站可以改。
test("TestCrossSiteCannotChangeTrustSettingsOfOtherHome", async ({ page, homes }) => {
  const b = homes.b;
  const before = await b.seed.api<SitesResp>("/api/web/get_sites", {});
  await page.goto("/settings/machine");
  for (const [route, body] of [
    ["set_allow_boards", { allow_boards: ["https://evil.example"] }],
    ["set_owner_login", { owner_login: "evil@example.com" }],
  ] as const) {
    const r = await pagePost(page, `${b.url}/api/web/${route}`, body);
    expect(r.status, route).toBe(403);
    expect(r.json).toMatchObject({ code: "forbidden", message: "這個設定只能在那台自己的看板改" });
  }
  const after = await b.seed.api<SitesResp>("/api/web/get_sites", {});
  expect(after.allow_boards).toEqual(before.allow_boards);
  expect(after.owner_login).toBe(TEST_OWNER_LOGIN);
  expect(
    (await pagePost(page, `${b.url}/api/web/set_sites`, { sites: before.sites ?? [] })).status,
  ).toBe(200);

  // A 自己的信任設定在 A 的看板改得了（同源）。
  const local = page.getByTestId("local-trust");
  await expect(local).toContainText("還沒有允許任何別台的看板。");
  try {
    await local.getByLabel("要允許的看板網址").fill(b.url);
    await local.getByRole("button", { name: "允許" }).click();
    await expect(local.getByTestId("allow-board")).toHaveText([`${b.url}拿掉`]);
  } finally {
    await homes.a.seed.api("/api/web/set_allow_boards", { allow_boards: [], by: OWNER });
  }
});
