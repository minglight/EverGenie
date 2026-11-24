import type { APIRequestContext } from "@playwright/test";
import { expect, test } from "./fixtures";

// 官方套件 fitness 的頁面（P6-3c）：資料經看板的 vault 代理寫進去（只限套件的種類），頁面用同一組 API 讀。
// 同一個 worker 的家是共用的：每個測試開頭把自己用的種類（訓練、器材、筆記）全部封存，從乾淨的狀態開始；
// 身體數據的編號就是日期；封存後同一天可以再記（DEV-151），但正本還在時不能重複新增，所以每個測試結束前把自己記的封存掉。

const pad = (n: number) => String(n).padStart(2, "0");
const dayOf = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const atOf = (d: Date) => `${dayOf(d)} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
const daysAgo = (n: number) => {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return d;
};

interface Rec {
  id: string;
  archived: boolean;
  body?: string;
  fields: Record<string, unknown>;
}

interface Api {
  post: (route: string, data: unknown) => Promise<Record<string, unknown>>;
  list: (kind: string) => Promise<Rec[]>;
  get: (kind: string, id: string) => Promise<Rec>;
  reset: () => Promise<void>;
  add: (kind: string, fields: Record<string, unknown>, body?: string) => Promise<string>;
  put: (kind: string, id: string, fields: Record<string, unknown>) => Promise<void>;
  archive: (kind: string, id: string) => Promise<void>;
}

function client(request: APIRequestContext, base: string): Api {
  const post = async (route: string, data: unknown) => {
    const res = await request.post(`${base}/api/${route}`, { data });
    const json = (await res.json().catch(() => ({}))) as Record<string, unknown>;
    expect(res.status(), `${route} ${JSON.stringify(json)}`).toBeLessThan(300);
    return json;
  };
  const list = async (kind: string) =>
    (
      (await post("vault/list", { kind, include_archived: false })) as {
        records?: Rec[];
      }
    ).records ?? [];
  const archive = async (kind: string, id: string) => {
    await post("vault/archive", { kind, id });
  };
  return {
    post,
    list,
    get: async (kind, id) => ((await post("vault/get", { kind, id })) as { record: Rec }).record,
    reset: async () => {
      for (const kind of ["fitness.workout", "fitness.equipment", "fitness.note"]) {
        for (const r of await list(kind)) await archive(kind, r.id);
      }
    },
    add: async (kind, fields, body) => {
      const id = String((await post("vault/next_id", { kind })).id);
      await post("vault/put", { record: { kind, id, fields, ...(body ? { body } : {}) } });
      return id;
    },
    put: async (kind, id, fields) => {
      await post("vault/put", { record: { kind, id, fields } });
    },
    archive,
  };
}

// Guards: REQ-127
test("TestWorkoutLogAndPlan @smoke", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  await api.add("fitness.equipment", {
    name: "啞鈴彎舉",
    primary: ["二頭肌"],
    secondary: [],
    seat: "座位 4",
    cues: ["肩膀往下壓、不聳肩", "手肘不往前滑"],
  });
  await api.add("fitness.equipment", {
    name: "棒式",
    primary: ["核心"],
    secondary: [],
    type: "徒手",
    unit: "秒",
  });
  // 前一天忘了收操的重訓（沒有任何組、10 小時前開始）：記新的一組時要被標成完成。
  const ten = new Date(Date.now() - 10 * 3600 * 1000);
  const forgotten = await api.add("fitness.workout", {
    type: "重訓",
    date: dayOf(ten),
    started: atOf(ten),
    status: "進行中",
    sets: [],
  });

  await page.goto("/p/fitness/log");
  await expect(page.getByTestId("fitness-log")).toBeVisible();
  await expect(page.getByTestId("active-workout")).toContainText("進行中：0 組");
  await page.getByRole("button", { name: "啞鈴彎舉", exact: true }).click();
  // 選到器材就先看到座位調整與注意事項（還沒記第一組）。
  await expect(page.getByTestId("coach-seat")).toContainText("座位 4");
  await expect(page.getByTestId("coach-cues")).toContainText("肩膀往下壓、不聳肩");
  const form = page.getByTestId("set-form");
  await form.getByLabel("重量（kg）").fill("10");
  await form.getByLabel("次數").fill("10");
  await form.getByRole("button", { name: "記這組" }).click();
  // 第一組：沒有可比較的，教練說第一次記錄；本週練到的肌群。
  const fb = page.getByTestId("coach-feedback");
  await expect(fb).toContainText("這台第一次記錄，之後拿這組當比較基準。");
  await expect(fb).toContainText("本週二頭肌第 1 次（共 1 組）。");
  await expect(page.getByTestId("active-workout")).toContainText("進行中：1 組");
  // 舊的那筆被標完成、備註加一句；新的一筆開始。
  await expect
    .poll(async () => (await api.get("fitness.workout", forgotten)).fields.status)
    .toBe("完成");
  const old = await api.get("fitness.workout", forgotten);
  expect(old.fields.note).toBe("忘了收操");
  expect(old.fields.review_by).toBe("規則");

  // 第二組：次數多 2 下，建議句出現。
  await form.getByLabel("次數").fill("12");
  await form.getByRole("button", { name: "記這組" }).click();
  await expect(page.getByTestId("coach-feedback")).toContainText(
    "次數比上一組多 2 下，有進步，維持 10 kg。",
  );
  await expect(page.getByTestId("coach-feedback")).toContainText("本週二頭肌第 1 次（共 2 組）。");
  await expect(page.getByTestId("coach-cues")).toContainText("手肘不往前滑");
  await expect(page.getByTestId("active-workout")).toContainText("進行中：2 組");

  // 徒手動作沒有重量欄；多記一組再刪掉最後一組。
  await page.getByRole("button", { name: "棒式", exact: true }).click();
  await expect(form.getByLabel("重量（kg）")).toHaveCount(0);
  await form.getByLabel("秒數").fill("30");
  await form.getByRole("button", { name: "記這組" }).click();
  await expect(page.getByTestId("active-workout")).toContainText("進行中：3 組");
  await page.getByRole("button", { name: "刪掉最後一組" }).click();
  await expect(page.getByTestId("active-workout")).toContainText("進行中：2 組");

  // 收操：選感覺、備註；檢討用規則算。
  await page.getByRole("button", { name: "收操", exact: true }).click();
  const finish = page.getByTestId("finish-form");
  await finish.getByRole("button", { name: "吃力" }).click();
  await finish.getByLabel("備註").fill("腿有點痠");
  await finish.getByRole("button", { name: "確定收操" }).click();
  const review = page.getByTestId("workout-review");
  await expect(review).toContainText("練了：二頭肌 2 組。");
  await expect(review).toContainText("整體吃力，下次重量先不加");
  await expect(page.getByTestId("active-workout")).toHaveCount(0);
  const done = (await api.list("fitness.workout")).filter((r) => r.fields.status === "完成");
  const mine = done.find((r) => r.id !== forgotten);
  expect(mine?.fields).toMatchObject({ feeling: "吃力", note: "腿有點痠", review_by: "規則" });

  // 歷史：新到舊、檢討可展開；選器材看每次最大重量。
  await page.getByRole("button", { name: "歷史", exact: true }).click();
  await expect(page).toHaveURL(/view=history/);
  const item = page.locator(`[data-testid="workout-item"][data-id="${mine?.id}"]`);
  await item.getByText(/重訓/).first().click();
  await expect(item.getByTestId("workout-review-text")).toContainText("練了：二頭肌 2 組。");
  // 趨勢：沒選器材是每週總量；選了器材看最大重量、估算 1RM、總量（時間軸）。
  await expect(page.getByTestId("week-volume")).toBeVisible();
  await page.getByTestId("history-equipment").selectOption("啞鈴彎舉");
  await expect(page.getByTestId("max-chart")).toBeVisible();
  await expect(page.getByTestId("max-chart-readout")).toContainText("最大重量");
  await page.getByRole("button", { name: "估算 1RM", exact: true }).click();
  await expect(page.getByTestId("max-chart-readout")).toContainText("估算 1RM");
  await page.reload();
  await expect(page.getByTestId("fitness-history")).toBeVisible();

  // 訓練計畫：用 API 種一篇釘選的筆記和一篇一般筆記。
  const body = "## 週一\n\n- 胸推 3 組\n- 飛鳥 3 組";
  await api.add("fitness.note", { title: "十月計畫", date: dayOf(new Date()), pinned: true }, body);
  await api.add("fitness.note", { title: "週報 10/20", date: dayOf(daysAgo(5)) }, "這週練了三次");
  await page.goto("/p/fitness/plan");
  const pinned = page.getByTestId("plan-pinned");
  await expect(pinned).toContainText("十月計畫");
  await expect(pinned).toContainText("胸推 3 組");
  await expect(page.getByTestId("plan-note")).toContainText("週報 10/20");
  // 從運動頁連得到計畫。
  await page.goto("/p/fitness/log");
  await page.getByRole("link", { name: "訓練計畫" }).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/p/fitness/plan");
});

// 手機寬度、器材名稱很長：進行中那張卡右邊的「60 kg × 8 · 很硬」固定一行，不被切成兩行（FIT-14）。
test("TestActiveSetDetailStaysOnOneLine", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  const now = new Date();
  const long = "史密斯機器深蹲（窄站距、腳放前面一點的版本）";
  await api.add("fitness.workout", {
    type: "重訓",
    date: dayOf(now),
    started: atOf(now),
    status: "進行中",
    sets: [{ equipment: long, weight: 60, reps: 8, feel: "很硬" }],
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/p/fitness/log");
  const detail = page.getByTestId("active-set-detail");
  await expect(detail).toHaveText("60 kg × 8 · 很硬");
  // 量文字本身佔幾行（span 會被 flex 撐高到跟左邊兩行的名稱一樣，不能量 span 的高）。
  const { textHeight, line } = await detail.evaluate((el) => {
    const r = document.createRange();
    r.selectNodeContents(el);
    return {
      textHeight: r.getBoundingClientRect().height,
      line: Number.parseFloat(getComputedStyle(el).lineHeight),
    };
  });
  expect(textHeight).toBeLessThan(line * 1.5);
  await api.reset();
});

// 重量忘了填就按「記這組」：不記成 0 kg（FIT-13），錯誤就在按鈕下面、手機上看得到（FIT-12）。
test("TestEmptyWeightErrorShowsByButton", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  await api.add("fitness.equipment", { name: "槓鈴臥推", primary: ["胸"], secondary: [] });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/p/fitness/log");
  await page.getByRole("button", { name: "槓鈴臥推", exact: true }).click();
  const form = page.getByTestId("set-form");
  await form.getByLabel("次數").fill("8");
  await form.getByRole("button", { name: "記這組" }).click();
  const alert = form.getByRole("alert");
  await expect(alert).toHaveText("重量要填");
  await expect(alert).toBeInViewport();
  expect(await api.list("fitness.workout")).toHaveLength(0);
  await api.reset();
});

// Guards: REQ-127
test("TestEquipmentDrawer", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  await page.goto("/p/fitness/log");
  await expect(page.getByText("還沒有器材")).toBeVisible();
  await page.getByRole("button", { name: "管理器材", exact: true }).click();
  const drawer = page.getByTestId("equipment-drawer");
  // 新增：關鍵字推得出肌群就帶上，推不出來提示請教練補。
  await drawer.getByTestId("equipment-name").fill("高位下拉");
  await drawer.getByRole("button", { name: "新增", exact: true }).click();
  await expect(drawer.getByRole("status")).toContainText("主練肌群：背闊肌");
  await drawer.getByTestId("equipment-name").fill("神祕訓練");
  await drawer.getByRole("button", { name: "新增", exact: true }).click();
  await expect(drawer.getByRole("status")).toContainText("請教練補");
  // 不重名。
  await drawer.getByTestId("equipment-name").fill("高位下拉");
  await drawer.getByRole("button", { name: "新增", exact: true }).click();
  await expect(drawer.getByRole("alert")).toContainText("已經有「高位下拉」了");
  // 改肌群：名單外的名字被擋，合格的存起來。
  const row = drawer.locator('[data-testid="equipment-row"][data-name="神祕訓練"]');
  await expect(row).toContainText("肌群待教練補");
  await row.getByRole("button", { name: "改 神祕訓練 的肌群" }).click();
  await row.getByLabel("主練肌群").fill("火星肌");
  await row.getByRole("button", { name: "存肌群" }).click();
  await expect(drawer.getByRole("alert")).toContainText("不認得的肌群：火星肌");
  await row.getByLabel("主練肌群").fill("臀大肌、核心");
  await row.getByLabel("輔助肌群").fill("大腿後側");
  await row.getByRole("button", { name: "存肌群" }).click();
  await expect(row).toContainText("臀大肌、核心");
  const saved = (await api.list("fitness.equipment")).find((r) => r.fields.name === "神祕訓練");
  expect(saved?.fields.primary).toEqual(["臀大肌", "核心"]);
  expect(saved?.fields.secondary).toEqual(["大腿後側"]);
  // 座位與注意事項：一行一條，空行不算。
  await row.getByRole("button", { name: "改 神祕訓練 的座位與注意事項" }).click();
  await row.getByTestId("equipment-seat").fill("座位 3");
  await row.getByTestId("equipment-cues").fill("核心鎖緊\n\n慢速回放");
  await row.getByRole("button", { name: "存", exact: true }).click();
  await expect(row.getByTestId("equipment-setting")).toContainText("座位 3");
  await expect
    .poll(async () => {
      const r = (await api.list("fitness.equipment")).find((x) => x.fields.name === "神祕訓練");
      return [r?.fields.seat, r?.fields.cues];
    })
    .toEqual(["座位 3", ["核心鎖緊", "慢速回放"]]);
  // 搜尋肌群找得到器材；移除要確認，只是封存。
  await drawer.getByRole("button", { name: "關閉" }).click();
  await page.getByTestId("equipment-search").fill("背闊");
  await expect(page.getByTestId("equipment-results")).toContainText("高位下拉");
  await page.getByRole("button", { name: "管理器材", exact: true }).click();
  await drawer.getByRole("button", { name: "移除 神祕訓練" }).click();
  await drawer.getByTestId("equipment-confirm").getByRole("button", { name: "取消" }).click();
  await expect(row).toBeVisible();
  await drawer.getByRole("button", { name: "移除 神祕訓練" }).click();
  await drawer.getByTestId("equipment-confirm").getByRole("button", { name: "移除" }).click();
  await expect(row).toHaveCount(0);
  expect((await api.list("fitness.equipment")).map((r) => r.fields.name)).toEqual(["高位下拉"]);
});

// 挑器材（DEV-146）：預選剛才在用的、先挑部位再挑器材、切到有氧或重新整理都還在、版面由上往下是記一組→挑器材→已經做的。
// Guards: FIT-17, FIT-18, FIT-19
test("TestEquipmentPickerRemembersAndParts", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  await api.add("fitness.equipment", {
    name: "坐姿划船機",
    primary: ["菱形肌", "背闊肌"],
    secondary: [],
  });
  await api.add("fitness.equipment", { name: "腿伸直機", primary: ["股四頭"], secondary: [] });
  await api.add("fitness.equipment", {
    name: "反向飛鳥機",
    primary: ["後三角", "菱形肌"],
    secondary: [],
  });
  await api.add("fitness.equipment", { name: "二頭彎舉機", primary: ["二頭肌"], secondary: [] });
  const now = new Date(Date.now() - 20 * 60 * 1000);
  await api.add("fitness.workout", {
    type: "重訓",
    date: dayOf(now),
    started: atOf(now),
    status: "進行中",
    sets: [
      { equipment: "坐姿划船機", weight: 30, reps: 10, feel: "剛好" },
      { equipment: "腿伸直機", weight: 25, reps: 12, feel: "輕鬆" },
    ],
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/p/fitness/log");
  const form = page.getByTestId("set-form");
  // 一進來就預選最後一組的器材，重量次數帶上一組。
  await expect(form.getByRole("heading")).toHaveText("腿伸直機");
  await expect(form.getByLabel("重量（kg）")).toHaveValue("25");
  await expect(page.getByTestId("equipment-recent").getByRole("button")).toHaveText([
    "腿伸直機",
    "坐姿划船機",
  ]);
  // 由上往下：記一組 → 挑器材 → 進行中（已經做的放最下面）。
  const top = async (id: string) => (await page.getByTestId(id).boundingBox())?.y ?? -1;
  expect(await top("set-form")).toBeLessThan(await top("equipment-search"));
  expect(await top("equipment-search")).toBeLessThan(await top("active-workout"));
  // 先挑部位再挑器材：一台可以在兩個部位。
  await page.getByRole("button", { name: /^部位：背/ }).click();
  const inPart = page.getByTestId("equipment-in-part");
  await expect(inPart.getByRole("button")).toHaveText(["坐姿划船機", "反向飛鳥機"]);
  await inPart.getByRole("button", { name: "反向飛鳥機" }).click();
  await expect(form.getByRole("heading")).toHaveText("反向飛鳥機");
  await expect(form).toBeInViewport();
  // −／＋：空的重量按「減」不會變成 0 kg（FIT-13），按「加」從 5 開始。
  await form.getByRole("button", { name: "減 5 公斤" }).click();
  await expect(form.getByLabel("重量（kg）")).toHaveValue("");
  await form.getByRole("button", { name: "加 5 公斤" }).click();
  await expect(form.getByLabel("重量（kg）")).toHaveValue("5");
  // 切到有氧再切回來、重新整理：選的器材和打開的部位都還在。
  await page.getByRole("button", { name: "有氧", exact: true }).click();
  await page.getByRole("button", { name: "重訓", exact: true }).click();
  await expect(form.getByRole("heading")).toHaveText("反向飛鳥機");
  await page.reload();
  await expect(form.getByRole("heading")).toHaveText("反向飛鳥機");
  await expect(page.getByRole("button", { name: /^部位：背/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await api.reset();
});

// Guards: REQ-304
test("TestAddEquipmentFromSearch", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  await page.goto("/p/fitness/log");
  await page.getByRole("button", { name: "管理器材", exact: true }).click();
  const drawer = page.getByTestId("equipment-drawer");
  await drawer.getByTestId("equipment-name").fill("高位下拉");
  await drawer.getByRole("button", { name: "新增", exact: true }).click();
  await drawer.getByRole("button", { name: "關閉" }).click();
  // 搜尋找不到：直接用搜尋的字新增並選好。
  await page.getByTestId("equipment-search").fill("二頭彎舉訓練機");
  await expect(page.getByText("找不到「二頭彎舉訓練機」")).toBeVisible();
  await page.getByTestId("equipment-add-from-search").click();
  await expect(page.getByTestId("equipment-search")).toHaveValue("");
  await expect(page.getByRole("button", { name: "二頭彎舉訓練機", pressed: true })).toBeVisible();
  const names = (await api.list("fitness.equipment")).map((r) => r.fields.name);
  expect(names).toContain("二頭彎舉訓練機");
});

// Guards: REQ-127
test("TestCardioLog", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  await page.goto("/p/fitness/log");
  await page.getByRole("button", { name: "有氧", exact: true }).click();
  await page.getByRole("button", { name: "跑步", exact: true }).click();
  const form = page.getByTestId("cardio-form");
  await expect(form).toHaveAttribute("data-sport", "跑步");
  await form.getByLabel("距離（km）").fill("5");
  await form.getByLabel("時間（分鐘）").fill("30");
  await form.getByLabel("平均心率（bpm）").fill("150");
  await form.getByLabel("最大心率（bpm）").fill("170");
  await form.getByLabel("步頻（步/分）").fill("172");
  await form.getByText("更多指數").click();
  await form.getByLabel("爬升（m）").fill("40");
  await form.getByRole("button", { name: "記這次跑步" }).click();
  const review = page.getByTestId("cardio-review");
  await expect(review).toContainText("這次：5 km、30 分，配速 6:00/km。");
  await expect(review).toContainText("心率：平均 150、最大 170 bpm。");
  await expect(review).toContainText("這是第一筆跑步");
  // 不合理的輸入在旁邊說明，不會存。
  await form.getByLabel("距離（km）").fill("0");
  await form.getByLabel("時間（分鐘）").fill("30");
  await form.getByRole("button", { name: "記這次跑步" }).click();
  await expect(page.getByRole("alert")).toContainText("距離 0.1–100 km");
  await form.getByLabel("距離（km）").fill("5");
  await form.getByLabel("平均心率（bpm）").fill("180");
  await form.getByLabel("最大心率（bpm）").fill("170");
  await form.getByRole("button", { name: "記這次跑步" }).click();
  await expect(page.getByRole("alert")).toContainText("最大心率不能比平均心率低");
  // 第二筆：跟上次比。
  await form.getByLabel("距離（km）").fill("5.5");
  await form.getByLabel("時間（分鐘）").fill("32.5");
  await form.getByLabel("平均心率（bpm）").fill("146");
  await form.getByLabel("最大心率（bpm）").fill("");
  await form.getByRole("button", { name: "輕鬆" }).click();
  await form.getByRole("button", { name: "記這次跑步" }).click();
  await expect(review).toContainText("距離多 0.5 km、配速快 5 秒/km、平均心率低 4");
  await expect(review).toContainText("心肺有進步");
  await expect(review).toContainText("可以多跑 0.5 km");
  const runs = (await api.list("fitness.workout")).filter((r) => r.fields.type === "跑步");
  expect(runs).toHaveLength(2);
  expect(runs.find((r) => r.fields.distance_km === 5)?.fields).toMatchObject({
    avg_hr: 150,
    max_hr: 170,
    cadence_spm: 172,
    stride_m: 0.97,
    elevation_m: 40,
  });
  expect(runs.find((r) => r.fields.distance_km === 5.5)?.fields).toMatchObject({
    status: "完成",
    pace: "5:55",
    distance_km: 5.5,
    duration_min: 32.5,
    avg_hr: 146,
    source: "web",
    review_by: "規則",
    feeling: "輕鬆",
  });

  // 划船：距離用公尺，配速每 500 m，有槳頻、功率。
  await page.getByRole("button", { name: "划船", exact: true }).click();
  await expect(form).toHaveAttribute("data-sport", "划船");
  await form.getByLabel("距離（公尺）").fill("2000");
  await form.getByLabel("時間（分鐘）").fill("8");
  await form.getByLabel("槳頻（下/分）").fill("26");
  await form.getByLabel("平均功率（W）").fill("180");
  await form.getByRole("button", { name: "記這次划船" }).click();
  await expect(review).toContainText("這次：2000 m、8 分，配速 2:00/500m。");

  // 游泳：池長 × 趟數算距離，配速每 100 m，泳姿、SWOLF。
  await page.getByRole("button", { name: "游泳", exact: true }).click();
  await form.getByLabel("趟數").fill("40");
  await expect(form.getByTestId("swim-distance")).toContainText("1000 m");
  await form.getByLabel("時間（分鐘）").fill("25");
  await form.getByLabel("SWOLF").fill("38");
  await form.getByLabel("主要泳姿").selectOption("自由式");
  await form.getByRole("button", { name: "記這次游泳" }).click();
  await expect(review).toContainText("這次：1000 m、25 分，配速 2:30/100m。");

  // 騎車：看均速，踏頻、室內。
  await page.getByRole("button", { name: "騎車", exact: true }).click();
  await form.getByLabel("距離（km）").fill("20");
  await form.getByLabel("時間（分鐘）").fill("48");
  await form.getByLabel("踏頻（轉/分）").fill("85");
  await form.getByLabel(/室內/).check();
  await form.getByRole("button", { name: "記這次騎車" }).click();
  await expect(review).toContainText("這次：20 km、48 分，均速 25 km/h。");

  const all = await api.list("fitness.workout");
  expect(all.find((r) => r.fields.type === "划船")?.fields).toMatchObject({
    distance_km: 2,
    pace: "2:00",
    stroke_rate_spm: 26,
    avg_power_w: 180,
  });
  expect(all.find((r) => r.fields.type === "游泳")?.fields).toMatchObject({
    distance_km: 1,
    pool_m: 25,
    laps: 40,
    swolf: 38,
    stroke_style: "自由式",
    pace: "2:30",
  });
  expect(all.find((r) => r.fields.type === "騎車")?.fields).toMatchObject({
    avg_speed_kmh: 25,
    cadence_rpm: 85,
    indoor: true,
  });

  // 歷史：清單有摘要與指數；趨勢圖畫每週分鐘與某項目的指標。
  await page.getByRole("button", { name: "歷史", exact: true }).click();
  await expect(page.getByTestId("workout-item")).toHaveCount(5);
  const swimItem = page.getByTestId("workout-item").filter({ hasText: "游泳" });
  await expect(swimItem).toContainText("1000 m · 25 分 · 2:30/100m");
  await swimItem.getByText(/游泳/).first().click();
  await expect(swimItem.getByTestId("workout-cardio-details")).toContainText("池長 25 m × 40 趟");
  const trends = page.getByTestId("fitness-trends");
  await expect(trends.getByTestId("week-cardio")).toBeVisible();
  await expect(trends.getByTestId("week-cardio-readout")).toContainText("本週");
  await trends.getByTestId("history-sport").selectOption("跑步");
  await trends.getByTestId("history-metric").selectOption("avg_hr");
  await expect(trends.getByTestId("cardio-chart-point")).toHaveCount(2);
  await expect(trends.getByTestId("cardio-chart-readout")).toContainText("跑步 平均心率 146 bpm");
  // 點圖的最左邊＝挑最舊的那次（整張圖都是點擊範圍）。
  await trends.getByTestId("cardio-chart-hit").click({ position: { x: 2, y: 20 } });
  await expect(trends.getByTestId("cardio-chart-readout")).toContainText("150 bpm");
});

test("TestBodyMetrics", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  const today = dayOf(new Date());
  const yesterday = dayOf(daysAgo(1));
  const earlier = dayOf(daysAgo(8));
  // 前一週的一筆（跟這週比差多少）。
  await api.put("body.entry", earlier, { date: earlier, weight: 74 });

  await page.goto("/p/body/log");
  const form = page.getByTestId("body-form");
  // 體重必填。
  await form.getByRole("button", { name: "記下來" }).click();
  await expect(form.getByRole("alert")).toContainText("體重要填");
  await form.getByLabel("體重（kg）").fill("500");
  await form.getByRole("button", { name: "記下來" }).click();
  await expect(form.getByRole("alert")).toContainText("體重要在 20–300");
  // 記昨天、今天。
  await form.getByLabel("日期").fill(yesterday);
  await form.getByLabel("體重（kg）").fill("72");
  await form.getByLabel("體脂（%）").fill("20");
  await form.getByRole("button", { name: "記下來" }).click();
  await expect(form.getByRole("status")).toContainText(`已記 ${yesterday}`);
  await form.getByLabel("日期").fill(today);
  await form.getByLabel("體重（kg）").fill("71");
  await form.getByRole("button", { name: "記下來" }).click();
  await expect(form.getByRole("status")).toContainText(`已記 ${today}`);
  // 近 7 天平均 71.5，比前 7 天（74）少 2.5。
  await expect(page.getByTestId("body-avg-weight")).toContainText("71.5");
  await expect(page.getByTestId("body-avg-weight")).toContainText("比前 7 天少 2.5");
  await expect(page.getByTestId("body-avg-fat")).toContainText("20");
  await expect(page.getByTestId("body-chart-weight")).toHaveAttribute("data-points", "3");
  await expect(page.getByTestId("body-entry")).toHaveCount(3);
  // 同一天再記就是覆蓋：今天改成 70.6，平均變 71.3。
  await form.getByLabel("日期").fill(today);
  await form.getByLabel("體重（kg）").fill("70.6");
  await form.getByRole("button", { name: "更新這天" }).click();
  await expect(form.getByRole("status")).toContainText(`已更新 ${today}`);
  await expect(page.getByTestId("body-avg-weight")).toContainText("71.3");
  await expect(page.getByTestId("body-entry")).toHaveCount(3);
  const rec = await api.get("body.entry", today);
  expect(rec.fields.weight).toBe(70.6);
  // 刪一筆（昨天）要確認，只是封存。
  const row = page.locator(`[data-testid="body-entry"][data-date="${yesterday}"]`);
  await row.getByRole("button", { name: `刪除 ${yesterday}` }).click();
  await row.getByTestId("body-confirm").getByRole("button", { name: "取消" }).click();
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: `刪除 ${yesterday}` }).click();
  await row.getByTestId("body-confirm").getByRole("button", { name: "刪除" }).click();
  await expect(row).toHaveCount(0);
  expect((await api.get("body.entry", yesterday)).archived).toBe(true);
  await expect(page.getByTestId("body-avg-weight")).toContainText("70.6");
  // 刪掉的那天可以再記一次（DEV-151）：新的一筆回到清單與平均，沒有錯誤；舊的留在封存裡。
  await form.getByLabel("日期").fill(yesterday);
  await form.getByLabel("體重（kg）").fill("72.4");
  await form.getByRole("button", { name: "記下來" }).click();
  await expect(form.getByRole("status")).toContainText(`已記 ${yesterday}`);
  await expect(form.getByRole("alert")).toHaveCount(0);
  await expect(row).toBeVisible();
  await expect(page.getByTestId("body-entry")).toHaveCount(3);
  await expect(page.getByTestId("body-avg-weight")).toContainText("71.5");
  const again = await api.get("body.entry", yesterday);
  expect(again.archived).toBe(false);
  expect(again.fields.weight).toBe(72.4);
  // 畫面還沒跟上、其實那天已經有資料（例如秘書剛幫忙記了）：照「同一天再記就是覆蓋」改掉，不報錯。
  await row.getByRole("button", { name: `刪除 ${yesterday}` }).click();
  await row.getByTestId("body-confirm").getByRole("button", { name: "刪除" }).click();
  await expect(row).toHaveCount(0);
  // 先把頁面重讀清單的請求擋著，再從外面記一筆：頁面以為那天沒資料。
  let release = () => {};
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/vault/list", async (r) => {
    await held;
    await r.continue();
  });
  await api.put("body.entry", yesterday, { date: yesterday, weight: 73 });
  await form.getByLabel("日期").fill(yesterday);
  await form.getByLabel("體重（kg）").fill("72.8");
  await form.getByRole("button", { name: "記下來" }).click();
  await expect(form.getByRole("status")).toContainText(`已記 ${yesterday}`);
  expect((await api.get("body.entry", yesterday)).fields.weight).toBe(72.8);
  release();
  await expect(row).toContainText("72.8");
  await page.unroute("**/api/vault/list");
  // 清掉這個測試的資料（今天卡片的測試要乾淨的起點）。
  await api.archive("body.entry", yesterday);
  await api.archive("body.entry", today);
  await api.archive("body.entry", earlier);
});

// Guards: REQ-114, REQ-127, REQ-236
test("TestFitnessTodayCard", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  const now = new Date();
  await api.add("fitness.workout", {
    type: "重訓",
    date: dayOf(now),
    started: atOf(new Date(now.getTime() - 3600 * 1000)),
    finished: atOf(now),
    status: "完成",
    feeling: "剛好",
    sets: [{ equipment: "啞鈴彎舉", weight: 10, reps: 10, feel: "剛好" }],
  });
  await api.add("fitness.workout", {
    type: "重訓",
    date: dayOf(now),
    started: atOf(now),
    status: "進行中",
    sets: [
      { equipment: "啞鈴彎舉", weight: 10, reps: 10, feel: "剛好" },
      { equipment: "啞鈴彎舉", weight: 10, reps: 11, feel: "剛好" },
    ],
  });
  const d = dayOf(daysAgo(2));
  await api.put("body.entry", d, { date: d, weight: 70 });

  await page.goto("/");
  const card = page.getByTestId("fitness-today");
  await expect(card).toBeVisible();
  await expect(card).toContainText("今天的運動");
  await expect(card.getByTestId("today-week")).toContainText("本週完成重訓 1 次 · 有氧 0 次");
  await expect(card.getByTestId("today-active")).toContainText("進行中：2 組");
  await expect(card.getByTestId("today-weight")).toHaveCount(0);
  // 體重在身體套件自己的卡片（REQ-236）。
  const body = page.getByTestId("body-today");
  await expect(body.getByTestId("today-weight")).toContainText("體重 70");
  await expect(body.getByTestId("today-weight")).toContainText("7 天平均 70");
  // 那筆是兩天前量的：卡片要標日期，不能看起來像今天的體重（BOD-5）。
  await expect(body.getByTestId("today-weight-when")).toContainText(
    `${daysAgo(2).getMonth() + 1}/${daysAgo(2).getDate()}`,
  );
  // 別的頁面沒有這個 slot，不會冒出卡片。
  await page.goto("/messages");
  await expect(page.getByTestId("fitness-today")).toHaveCount(0);
  await page.goto("/");
  await page.getByTestId("fitness-today").click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/p/fitness/log");
  await expect(page.getByTestId("fitness-log")).toBeVisible();
  await api.reset();
  await api.archive("body.entry", d);
});

// 忘了收操的訓練：今天頁不再一直寫「進行中」，改說上次的還沒收操（FIT-15）。
test("TestForgottenWorkoutTodayCardSaysNotFinished", async ({ page, request, baseURL }) => {
  const api = client(request, baseURL ?? "");
  await api.reset();
  const then = daysAgo(1);
  await api.add("fitness.workout", {
    type: "重訓",
    date: dayOf(then),
    started: atOf(then),
    status: "進行中",
    sets: [],
  });
  await page.goto("/");
  const card = page.getByTestId("fitness-today");
  await expect(card.getByTestId("today-stale")).toContainText("上次開了訓練還沒收操");
  await expect(card.getByTestId("today-active")).toHaveCount(0);
  await api.reset();
});

// Guards: REQ-235, REQ-236
test("TestHealthTabsAndSideChat", async ({ page, isMobile }) => {
  await page.goto("/p/body/log");
  // 飲食、運動、身體是同一頁「健康」的分頁：上方三個分頁，目前是身體。
  const tabs = page.getByRole("navigation", { name: "健康的分頁" });
  await expect(tabs.getByRole("link")).toHaveText(["飲食", "運動", "身體"]);
  await expect(tabs.getByRole("link", { name: "身體" })).toHaveAttribute("aria-current", "page");
  await tabs.getByRole("link", { name: "飲食" }).click();
  await expect.poll(() => new URL(page.url()).pathname).toBe("/p/nutrition/log");
  if (isMobile) {
    // 窄螢幕：分頁最後是「聊天」，切過去聊天就在這頁、記錄藏起來；點回分頁就回到記錄。
    await expect(page.getByTestId("page-chat")).toHaveCount(0);
    await tabs.getByTestId("page-chat-tab").click();
    await expect(page.getByTestId("page-chat")).toBeVisible();
    await expect(page.getByTestId("nutrition-datenav")).toBeHidden();
    await tabs.getByRole("link", { name: "飲食" }).click();
    await expect(page.getByTestId("page-chat")).toHaveCount(0);
    await expect(page.getByTestId("nutrition-datenav")).toBeVisible();
    return;
  }
  // 桌機：選單只有一個「健康」，在任何一個分頁都亮著；寬螢幕旁邊就是聊天。
  const nav = page.getByRole("navigation", { name: "主選單" });
  await expect(nav.getByRole("link", { name: /^健康/ })).toHaveAttribute("aria-current", "page");
  await expect(nav.getByRole("link", { name: /^運動/ })).toHaveCount(0);
  await expect(page.getByRole("heading", { level: 1, name: "健康" })).toBeVisible();
  await expect(page.getByTestId("page-chat")).toBeVisible();
});

// Guards: REQ-236
test("TestHealthPartsCanBeTurnedOff", async ({ page, request, baseURL }) => {
  const setEnabled = (id: string, enabled: boolean) =>
    request.post(`${baseURL}/api/package/set_enabled`, { data: { id, enabled } });
  try {
    await page.goto("/p/body/log");
    const tabs = page.getByRole("navigation", { name: "健康的分頁" });
    // 「顯示設定」列出三項，都開著；關掉正在看的「身體」：分頁不見，換到還開著的第一頁。
    await tabs.getByTestId("section-toggles").click();
    const panel = page.getByTestId("section-toggles-panel");
    await expect(panel.getByRole("switch")).toHaveCount(3);
    const body = panel.getByRole("switch", { name: "記錄 身體" });
    await expect(body).toHaveAttribute("aria-checked", "true");
    await body.click();
    await expect.poll(() => new URL(page.url()).pathname).toBe("/p/nutrition/log");
    await expect(tabs.getByRole("link")).toHaveText(["飲食", "運動"]);
    // 關掉的還在清單裡，打開就回來。
    await tabs.getByTestId("section-toggles").click();
    await panel.getByRole("switch", { name: "記錄 身體" }).click();
    await expect(tabs.getByRole("link")).toHaveText(["飲食", "運動", "身體"]);
  } finally {
    await setEnabled("body", true);
  }
});
