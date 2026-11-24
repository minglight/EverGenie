import { appendFileSync, readFileSync } from "node:fs";
import path from "node:path";
import type { APIRequestContext } from "@playwright/test";
import { expect, test } from "./fixtures";

// 官方選用套件 admin（開發工坊）的兩頁「發版」「開發家」（ADM-27）：頁面只讀 admin 程式寫的 admin.release／admin.devhome，
// 這裡直接把資料寫進 vault（程式下一輪是 5 分鐘後，不會蓋掉）；[admin] repo 指到不存在的資料夾，程式不碰真的 repo。
// 開單鈕開的是一般任務單，給 admin 秘書（測試裡自己造一位）。每個測試結束時停用套件。

async function post(request: APIRequestContext, base: string, route: string, data: unknown) {
  const res = await request.post(`${base}/api/${route}`, { data });
  const json = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  expect(res.status(), `${route} ${JSON.stringify(json)}`).toBeLessThan(300);
  return json;
}

async function putCurrent(
  request: APIRequestContext,
  base: string,
  kind: string,
  fields: Record<string, unknown>,
) {
  const res = await request.post(`${base}/api/vault/get`, { data: { kind, id: "current" } });
  const rev = res.ok()
    ? ((await res.json()) as { record?: { revision?: string } }).record?.revision
    : "";
  await post(request, base, "vault/put", {
    record: { kind, id: "current", fields },
    expected_revision: rev ?? "",
  });
}

const sha = (c: string) => c.repeat(40);

// [admin] repo 指到不存在的資料夾，程式不碰真的 repo。同一個 worker 的家共用，home.toml 只能有一個 [admin]，寫過就不再寫。
function adminConfig(hgServer: { home: string; tmp: string }) {
  const file = path.join(hgServer.home, "home.toml");
  if (/^\[admin\]$/m.test(readFileSync(file, "utf8"))) return;
  appendFileSync(
    file,
    `\n[admin]\nrepo = "${path.join(hgServer.tmp, "no-repo")}"\nrelease_state = "${path.join(hgServer.tmp, "no-state")}"\n`,
  );
}

// 畫面照瀏覽器時區寫時間；資料寫的是台北時間，固定時區才不會在別的機器上變成別的鐘點。
test.use({ timezoneId: "Asia/Taipei" });

test("TestAdminPagesDeployButtonComesBackAfterTaskEnds：發版頁開部署單後顯示在跑，單結束了按鈕回來；頁名不重複、確認句白話", async ({
  page,
  request,
  baseURL,
  hgServer,
  seed,
}) => {
  test.setTimeout(120_000);
  const base = baseURL ?? "";
  adminConfig(hgServer);
  await post(request, base, "package/set_enabled", { id: "admin", enabled: true });
  try {
    await expect
      .poll(
        async () =>
          (
            (await post(request, base, "package/get", { id: "admin" })) as {
              package: { state: string };
            }
          ).package.state,
        { timeout: 60_000 },
      )
      .toBe("running");
    await seed.api("/api/agents/create", {
      id: "admin",
      persona: "helper",
      duty: "開發工坊",
      by: { name: "owner(web)" },
    });
    await seed.api("/api/agents/set_field", {
      id: "admin",
      field: "enabled",
      value: "true",
      by: { name: "owner(web)" },
    });
    // 等程式第一輪寫完（repo 不存在），再蓋上測試要的資料。
    await expect
      .poll(
        async () =>
          (
            await request.post(`${base}/api/vault/get`, {
              data: { kind: "admin.release", id: "current" },
            })
          ).ok(),
        { timeout: 30_000 },
      )
      .toBe(true);
    const main = { commit: sha("d"), short: "ddddddd", subject: "最新的改動", at: "" };
    await putCurrent(request, base, "admin.release", {
      version: "0.1.0-1-gccccccc",
      main,
      pending: [main],
      computed_at: "2026-11-03T02:10:00+08:00",
    });

    await page.goto("/p/admin/release");
    // 頁名只有外殼頁首那一個（ADM-27 補）。
    await expect(page.getByRole("heading", { name: "發版", exact: true })).toHaveCount(1);
    await expect(page.getByTestId("admin-release")).toContainText("11 月 3 日 02:10");
    // 一列的值長到換行時整段靠左，不一行行靠右（ADM-32）。
    const value = page.getByTestId("admin-row-value").first();
    await expect(value).toBeVisible();
    expect(await value.evaluate((el) => getComputedStyle(el).textAlign)).toBe("left");
    // 手機寬：換到下一行的值貼著左邊，不被推到右邊。
    await page.setViewportSize({ width: 390, height: 844 });
    const pushedRight = await page.getByTestId("admin-row-value").evaluateAll(
      (els) =>
        els.filter((el) => {
          const row = el.parentElement?.getBoundingClientRect();
          const label = el.previousElementSibling?.getBoundingClientRect();
          const v = el.getBoundingClientRect();
          return row && label && v.top >= label.bottom - 1 && v.left - row.left > 2;
        }).length,
    );
    expect(pushedRight).toBe(0);
    await page.setViewportSize({ width: 1280, height: 800 });

    await page.getByTestId("admin-deploy").click();
    await expect(page.getByRole("alert")).toContainText("給 Helper，");
    await page.getByRole("button", { name: "開部署單" }).click();
    await expect(page.getByTestId("admin-deploy-running")).toBeVisible();

    const list = (await post(request, base, "tasks/list", {
      agent: "admin",
      statuses: ["doing"],
    })) as { tasks?: { id: string; title: string }[] };
    const dep = (list.tasks ?? []).find((t) => t.title.startsWith("部署 ddddddd"));
    expect(dep, "開了部署單").toBeTruthy();
    await seed.api("/api/tasks/close", {
      id: dep?.id,
      status: "cancelled",
      result: "e2e 取消",
      by: { name: "owner(web)" },
    });
    // 清單 15 秒重讀一次：單結束了按鈕回來，不用重新整理（ADM-30）。
    await expect(page.getByTestId("admin-deploy")).toBeVisible({ timeout: 25_000 });
    await expect(page.getByTestId("admin-deploy-running")).toHaveCount(0);

    await page.goto("/p/admin/devhome");
    await expect(page.getByRole("heading", { name: "開發家", exact: true })).toHaveCount(1);
    // 卡片標題不跟第一列「狀態」重複（ADM-33）。
    await expect(page.getByRole("heading", { name: "這台的開發家", exact: true })).toHaveCount(1);
    await expect(page.getByRole("heading", { name: "狀態", exact: true })).toHaveCount(0);
  } finally {
    await post(request, base, "package/set_enabled", { id: "admin", enabled: false });
    await seed
      .api("/api/agents/set_field", {
        id: "admin",
        field: "enabled",
        value: "false",
        by: { name: "owner(web)" },
      })
      .catch(() => undefined);
  }
});

// 開發家（開發工坊開著）才有生圖 art（ADM-39，DEV-160）：開發工坊帶 art 這個選用職位；還沒人擔任時，
// 「職位與秘書」頁「選用的職位」那一組列出她、標「選用」。一般家沒有 art 的那一半在 closet.spec.ts 的 TestNoArtOnOrdinaryHome。
// 同一個 worker 的家共用：名冊上已經有 art 時（別的測試建過）不出認領卡，畫面上只看得到名冊那一列；
// 套件帶 art、標選用這件事用 package.get 照樣驗。
test("TestArtListedOnDevHome", async ({ page, request, baseURL, hgServer }) => {
  const base = baseURL ?? "";
  adminConfig(hgServer);
  await post(request, base, "package/set_enabled", { id: "admin", enabled: true });
  try {
    const pkg = (await post(request, base, "package/get", { id: "admin" })) as {
      package: { enabled: boolean; agents?: { id: string; optional?: boolean }[] };
    };
    expect(pkg.package.enabled).toBe(true);
    expect(pkg.package.agents).toEqual([expect.objectContaining({ id: "art", optional: true })]);

    const roster = (await post(request, base, "web/get_agents", {})) as {
      agents?: { id: string }[];
    };
    await page.goto("/settings/agents?claim=art");
    if ((roster.agents ?? []).some((a) => a.id === "art")) {
      await expect(page.locator('[data-testid="agent-row"][data-agent="art"]')).toBeVisible();
    } else {
      const art = page
        .getByTestId("functions-optional")
        .locator('[data-testid="function-unclaimed"][data-agent="art"]');
      await expect(art).toContainText("生圖");
      await expect(art).toHaveAttribute("data-optional", "true");
      await expect(art).toHaveAttribute("data-focused", "true");
      await expect(art.getByTestId("function-optional-badge")).toHaveText("選用");
    }
  } finally {
    await post(request, base, "package/set_enabled", { id: "admin", enabled: false });
  }
});
