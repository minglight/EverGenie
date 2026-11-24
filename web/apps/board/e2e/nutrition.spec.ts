import type { APIRequestContext } from "@playwright/test";
import { expect, test } from "./fixtures";

// 官方套件 nutrition 的頁面（P6-3b）：資料經看板的 vault 代理寫進去（只限套件的種類），頁面用同一組 API 讀。
// 同一個 worker 的家是共用的：每個測試用自己的日期或事後封存，結果不互相影響。

const PNG_1X1 = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

const pad = (n: number) => String(n).padStart(2, "0");
const dayOf = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const daysAgo = (n: number) => {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return d;
};

interface Api {
  post: (route: string, data: unknown) => Promise<Record<string, unknown>>;
  meal: (fields: Record<string, unknown>) => Promise<string>;
  fav: (fields: Record<string, unknown>) => Promise<string>;
  goal: (fields: Record<string, unknown>) => Promise<void>;
  archive: (kind: string, id: string) => Promise<void>;
  upload: (name: string) => Promise<string>;
}

function client(request: APIRequestContext, base: string): Api {
  const post = async (route: string, data: unknown) => {
    const res = await request.post(`${base}/api/${route}`, { data });
    const json = (await res.json().catch(() => ({}))) as Record<string, unknown>;
    expect(res.status(), `${route} ${JSON.stringify(json)}`).toBeLessThan(300);
    return json;
  };
  const add = async (kind: string, fields: Record<string, unknown>) => {
    const next = await post("vault/next_id", { kind });
    const id = String(next.id);
    await post("vault/put", { record: { kind, id, fields } });
    return id;
  };
  return {
    post,
    meal: (fields) =>
      add("nutrition.meal", {
        at: `${String(fields.date)} 12:30`,
        meal: "午餐",
        carb: 60,
        fat: 20,
        ...fields,
      }),
    fav: (fields) => add("nutrition.fav", { carb: 50, fat: 15, ...fields }),
    goal: async (fields) => {
      const res = await request.post(`${base}/api/vault/get`, {
        data: { kind: "nutrition.goal", id: "main" },
      });
      if (res.ok()) await post("vault/patch", { kind: "nutrition.goal", id: "main", set: fields });
      else
        await post("vault/put", {
          record: {
            kind: "nutrition.goal",
            id: "main",
            fields: { mode: "維持", carb: 0, fat: 0, ...fields },
          },
        });
    },
    archive: async (kind, id) => {
      await post("vault/archive", { kind, id });
    },
    upload: async (name) => {
      const res = await request.post(`${base}/api/web/upload`, {
        headers: {
          "X-Filename": encodeURIComponent(name),
          "X-Requested-With": "evergenie",
          "Content-Type": "application/octet-stream",
        },
        data: PNG_1X1,
      });
      expect(res.ok(), await res.text()).toBe(true);
      return ((await res.json()) as { url: string }).url;
    },
  };
}

// Guards: REQ-128
test("TestMealLogWithPhoto @smoke", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  const date = "2021-03-14";
  await api.goal({ kcal: 2000, protein: 100 });
  const photo = await api.upload(`午餐-${Math.random().toString(36).slice(2, 8)}.png`);
  const id = await api.meal({
    date,
    kcal: 700,
    protein: 40,
    conf: "中",
    photos: [photo],
    items: [{ name: "雞腿便當", qty: "1 份", kcal: 700, protein: 40, carb: 60, fat: 20 }],
  });
  // 欄位不符被擋：缺必填的 date。
  const bad = await request.post(`${baseURL}/api/vault/put`, {
    data: { record: { kind: "nutrition.meal", id: "M-9999", fields: { kcal: 1 } } },
  });
  expect(bad.status()).toBe(400);

  await page.goto(`/p/nutrition/log?date=${date}`);
  const card = page.locator(`[data-testid="nutrition-meal"][data-id="${id}"]`);
  await expect(card).toBeVisible();
  await expect(card).toContainText("午餐");
  await expect(card).toContainText("700 大卡");
  await expect(card).toContainText("把握中");
  await card.getByText(/^品項/).click();
  await expect(card.getByText("雞腿便當")).toBeVisible();
  // 當天合計對目標。
  const kcal = page.getByTestId("nutrition-kcal");
  await expect(kcal).toContainText("700 / 2000 大卡");
  await expect(kcal).toHaveAttribute("data-over", "false");
  await expect(page.getByTestId("nutrition-protein")).toContainText("40 / 100 公克");
  // 縮圖真的載入，點了開大圖，可以關。
  const thumb = card.getByTestId("nutrition-photo").getByRole("img");
  await expect(thumb).toBeVisible();
  await expect
    .poll(() => thumb.evaluate((el: HTMLImageElement) => el.naturalWidth))
    .toBeGreaterThan(0);
  await card.getByTestId("nutrition-photo").click();
  const box = page.getByTestId("media-viewer");
  await expect(box).toBeVisible();
  await box.getByRole("button", { name: "關閉" }).click();
  await expect(box).toHaveCount(0);
  // 前一天沒有記錄。
  await page.getByRole("button", { name: "前一天" }).click();
  // 日期只寫一次：在日期選擇器裡（DEV-156 U-29）。
  await expect(page.getByTestId("nutrition-datepick")).toHaveValue("2021-03-13");
  await expect(page.getByTestId("nutrition-datenav")).not.toContainText("2021-03-13");
  await expect(page.getByText("這天還沒有記錄")).toBeVisible();
  await page.getByRole("button", { name: "後一天" }).click();
  await expect(card).toBeVisible();
  // 重新整理回到同一天（日期在網址裡）。
  await page.reload();
  await expect(page.getByTestId("nutrition-datepick")).toHaveValue(date);
  await expect(card).toBeVisible();
  // 刪除要確認；確認後消失（只是封存）。
  await card.getByRole("button", { name: `刪除 ${id}` }).click();
  await page.getByTestId("nutrition-confirm").getByRole("button", { name: "取消" }).click();
  await expect(card).toBeVisible();
  await card.getByRole("button", { name: `刪除 ${id}` }).click();
  await page.getByTestId("nutrition-confirm").getByRole("button", { name: "刪除" }).click();
  await expect(card).toHaveCount(0);
  const rec = (await api.post("vault/get", { kind: "nutrition.meal", id })) as {
    record: { archived: boolean };
  };
  expect(rec.record.archived).toBe(true);
});

// Guards: REQ-128
test("TestFavLogsMeal", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  const name = `常吃-${Math.random().toString(36).slice(2, 8)}`;
  const favId = await api.fav({ name, kcal: 333, protein: 22 });
  await page.goto("/p/nutrition/log");
  // 今天是最後一天：「後一天」按不下去（NUT-8）。
  await expect(page.getByRole("button", { name: "後一天" })).toBeDisabled();
  const chip = page.getByTestId("nutrition-fav").filter({ hasText: name });
  await expect(chip).toBeVisible();
  await chip.click();
  // 新的一餐出現在今天，餐別依現在時間猜（五種之一），並記下來源。
  const card = page.getByTestId("nutrition-meal").filter({ hasText: "333 大卡" });
  await expect(card).toBeVisible();
  await expect(card).toContainText(/早餐|午餐|點心|晚餐|宵夜/);
  const list = (await api.post("vault/list", {
    kind: "nutrition.meal",
    include_archived: false,
  })) as {
    records: { id: string; fields: Record<string, unknown> }[];
  };
  const made = list.records.filter((r) => r.fields.fav === favId);
  expect(made).toHaveLength(1);
  expect(made[0]?.fields.date).toBe(dayOf(new Date()));
  // 把這一餐存成常吃。
  await card.getByRole("button", { name: "存成常吃" }).click();
  await card.getByTestId("nutrition-fav-name").fill(`${name}-複製`);
  await card.getByRole("button", { name: "存", exact: true }).click();
  await expect(card.getByText("已存成常吃")).toBeVisible();
  await expect(page.getByTestId("nutrition-fav").filter({ hasText: `${name}-複製` })).toBeVisible();
  // 清掉這個測試造的資料。
  for (const r of made) await api.archive("nutrition.meal", r.id);
  await api.archive("nutrition.fav", favId);
  const favs = (await api.post("vault/list", {
    kind: "nutrition.fav",
    include_archived: false,
  })) as {
    records: { id: string; fields: Record<string, unknown> }[];
  };
  for (const f of favs.records.filter((x) => String(x.fields.name).startsWith(name))) {
    await api.archive("nutrition.fav", f.id);
  }
});

// 秘書在旁邊的聊天裡幫你記了一餐（從頁面外面寫進 vault）：頁面開著就自己出現，不用重新整理（PKG-49）。
test("TestMealFromSecretaryShowsUpWithoutReload", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  const date = "2021-05-20";
  await page.goto(`/p/nutrition/log?date=${date}`);
  await expect(page.getByText("這天還沒有記錄")).toBeVisible();
  const id = await api.meal({ date, kcal: 612, protein: 31 });
  const card = page.locator(`[data-testid="nutrition-meal"][data-id="${id}"]`);
  await expect(card).toBeVisible({ timeout: 3000 });
  await expect(card).toContainText("612 大卡");
  await api.archive("nutrition.meal", id);
  await expect(card).toHaveCount(0, { timeout: 3000 });
});

// Guards: REQ-128
test("TestTrendAndGoal", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.goal({ kcal: 2000, protein: 100, mode: "維持" });
  const made = [
    await api.meal({ date: dayOf(daysAgo(0)), kcal: 1800, protein: 90 }),
    await api.meal({ date: dayOf(daysAgo(2)), kcal: 2300, protein: 110 }),
  ];
  await page.goto("/p/nutrition/trend");
  const days = page.getByTestId("trend-day");
  await expect(days).toHaveCount(14);
  // 有記錄的日子畫長條，其餘留空（不畫 0）。
  await expect(page.locator('[data-testid="trend-day"][data-empty="false"]').first()).toBeVisible();
  await expect(days.last()).toHaveAttribute("data-date", dayOf(new Date()));
  await expect(days.last()).toHaveAttribute("data-empty", "false");
  await expect(days.nth(11)).toHaveAttribute("data-empty", "false");
  await expect(days.nth(12)).toHaveAttribute("data-empty", "true");
  await expect(page.getByTestId("trend-goal-line")).toBeVisible();
  await expect(page.getByTestId("trend-average")).toContainText("近 7 天平均");
  await expect(page.getByTestId("trend-average")).toContainText(/熱量 \d+ 大卡/);

  // 改目標、存檔、重新整理還在。
  const editor = page.getByTestId("goal-editor");
  await expect(editor.getByLabel("熱量（大卡）")).toHaveValue("2000");
  await editor.getByRole("button", { name: "增肌" }).click();
  await editor.getByLabel("熱量（大卡）").fill("2400");
  await editor.getByLabel("蛋白質（公克）").fill("150");
  await editor.getByRole("button", { name: "存目標" }).click();
  await expect(editor.getByText("已儲存")).toBeVisible();
  await page.reload();
  await expect(page.getByTestId("goal-editor").getByLabel("熱量（大卡）")).toHaveValue("2400");
  await expect(page.getByTestId("goal-editor").getByLabel("蛋白質（公克）")).toHaveValue("150");
  await expect(
    page.getByTestId("goal-editor").getByRole("button", { name: "增肌" }),
  ).toHaveAttribute("aria-pressed", "true");
  const g = (await api.post("vault/get", { kind: "nutrition.goal", id: "main" })) as {
    record: { fields: Record<string, unknown> };
  };
  expect(g.record.fields).toMatchObject({ mode: "增肌", kcal: 2400, protein: 150 });
  // 不合格的數字在欄位旁說明，不會存。
  const again = page.getByTestId("goal-editor");
  await again.getByLabel("熱量（大卡）").fill("很多");
  await again.getByRole("button", { name: "存目標" }).click();
  await expect(again.getByRole("alert")).toContainText("熱量要填 0 以上的數字");
  for (const id of made) await api.archive("nutrition.meal", id);
});

// Guards: REQ-114, REQ-128
test("TestNutritionTodayCard", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.goal({ kcal: 2000, protein: 100, mode: "維持" });
  const id = await api.meal({ date: dayOf(new Date()), kcal: 640, protein: 35 });
  await page.goto("/");
  const card = page.getByTestId("nutrition-today");
  await expect(card).toBeVisible();
  await expect(card).toContainText("今天吃了什麼");
  await expect(card.getByTestId("today-kcal")).toContainText("/ 2000 大卡");
  await expect(card.getByTestId("today-protein")).toContainText("/ 100 公克");
  await expect(card).toContainText(/\d+ 餐/);
  // 別的頁面沒有這個 slot，不會冒出卡片。
  await page.goto("/messages");
  await expect(page.getByTestId("nutrition-today")).toHaveCount(0);
  await page.goto("/");
  await page.getByTestId("nutrition-today").click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/p/nutrition/log");
  await expect(page.getByTestId("nutrition-log")).toBeVisible();
  await api.archive("nutrition.meal", id);
});

// package.toml 的 route 是別名：打開 /nutrition 轉到正式網址 /p/nutrition/log（DEV-91）。
test("TestPageRouteAliasRedirects", async ({ page }) => {
  await page.goto("/nutrition");
  await expect.poll(() => new URL(page.url()).pathname).toBe("/p/nutrition/log");
  await expect(page.getByRole("heading", { name: "找不到這一頁" })).toHaveCount(0);
});

// 還沒設飲食目標是常態：頁面讀目標不打會回 404 的 vault/get，主控台不會每開一次就記一筆錯（DEV-157 新家第一天走查⑤）。
test("TestGoalLoadNeverAsks404", async ({ page }) => {
  const bad: string[] = [];
  page.on("response", (res) => {
    if (res.url().includes("/api/") && res.status() === 404) bad.push(res.url());
  });
  const asked: string[] = [];
  page.on("request", (req) => {
    if (req.url().endsWith("/api/vault/get") && (req.postData() ?? "").includes("nutrition.goal"))
      asked.push(req.postData() ?? "");
  });
  for (const at of ["/", "/p/nutrition/log", "/p/nutrition/trend"]) {
    // 等到這一頁真的讀過目標（用 list）才往下，免得還沒讀就判定「沒打 get」。
    const listed = page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/vault/list") &&
        (r.request().postData() ?? "").includes("nutrition.goal"),
    );
    await page.goto(at);
    expect((await listed).status(), at).toBe(200);
  }
  expect(asked).toEqual([]);
  expect(bad).toEqual([]);
});
