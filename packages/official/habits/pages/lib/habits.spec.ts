import { describe, expect, test } from "vitest";
import {
  checkinKey,
  doneLine,
  doneToday,
  fieldsFromInput,
  type Habit,
  type HabitInput,
  type HabitStatus,
  howDone,
  monthCells,
  nextFormValue,
  nextPointText,
  notDoneCount,
  parseSub,
  pickOneThing,
  pointLine,
  progressText,
  ruleSections,
  soleAgent,
  sortHabits,
  sourceText,
  statusId,
  subLabel,
  taskLeft,
  todayCardState,
  todoHowLines,
  todoNextLine,
  todoProgress,
  toHabit,
  toStatus,
  toWallet,
  validateForm,
  validateHabit,
  weekCells,
  withPendingMark,
  withPendingMarks,
} from "./habits";

const ok = (): HabitInput => ({
  name: "重訓",
  cadence: "weekly",
  target: 3,
  missDays: 1,
  weekSkips: 2,
  mode: "cumulative",
  grace: 1,
  status: "active",
  sources: [],
  subtasks: [],
  allowMiss: 0,
  identity: "",
  cue: "",
  twoMin: "",
  bonusWeeks: 4,
});

describe("習慣的純函式", () => {
  // Guards: REQ-124
  test("TestHabitInputValidation", () => {
    expect(validateHabit(ok())).toBeNull();
    expect(validateHabit({ ...ok(), name: "  " })).toContain("名字");
    expect(validateHabit({ ...ok(), target: 0 })).toContain("1 到 7");
    expect(validateHabit({ ...ok(), target: 8 })).toContain("1 到 7");
    expect(validateHabit({ ...ok(), target: 2.5 })).toContain("1 到 7");
    // 每天型不看 target。
    expect(validateHabit({ ...ok(), cadence: "daily", target: 0 })).toBeNull();
    expect(validateHabit({ ...ok(), missDays: 7 })).toContain("0 到 6");
    expect(validateHabit({ ...ok(), grace: -1 })).toContain("0 到 6");
    expect(validateHabit({ ...ok(), cadence: "monthly" })).toContain("頻率");
    expect(validateHabit({ ...ok(), mode: "x" })).toContain("模式");
    expect(validateHabit({ ...ok(), status: "x" })).toContain("狀態");
    // 自動來源。
    for (const good of ["fitness:重訓", "fitness:跑步", "fitness:*", "reminder:R-3"])
      expect(validateHabit({ ...ok(), sources: [good] })).toBeNull();
    for (const bad of ["gmail:1", "fitness:", "reminder"])
      expect(validateHabit({ ...ok(), sources: [bad] })).toContain("看不懂");
  });

  // Guards: REQ-124
  test("TestSubtaskRules", () => {
    expect(parseSub("拉筋")).toEqual({ name: "拉筋", source: "", min: 1 });
    expect(parseSub("量體重=body:weight")).toEqual({
      name: "量體重",
      source: "body:weight",
      min: 1,
    });
    expect(parseSub("吃飯=nutrition:meals>=3")).toEqual({
      name: "吃飯",
      source: "nutrition:meals",
      min: 3,
    });
    expect(parseSub("吃飯=nutrition:meals>=x")).toBeNull();
    const withSubs = (subtasks: string[], allowMiss = 0): HabitInput => ({
      ...ok(),
      cadence: "daily",
      subtasks,
      allowMiss,
    });
    expect(validateHabit(withSubs(["a", "b=body:weight"], 1))).toBeNull();
    expect(validateHabit(withSubs(["a", "a"]))).toContain("不能重複");
    expect(validateHabit(withSubs(["=body:weight"]))).toContain("不能是空的");
    expect(validateHabit(withSubs(["a=gmail:x"]))).toContain("自動來源");
    expect(validateHabit(withSubs(["a=nutrition:meals>=0"]))).toContain("1 以上");
    expect(validateHabit(withSubs(["a=nutrition:meals>=z"]))).toContain("整數");
    expect(validateHabit(withSubs(["a", "b"], 2))).toContain("0 到 1");
    expect(validateHabit(withSubs([], 1))).toContain("只能是 0");
    expect(subLabel("量體重=body:weight")).toBe("量體重（有體重記錄就算）");
    expect(subLabel("吃飯=nutrition:meals>=2")).toBe("吃飯（一天記滿 2 餐就算）");
    expect(subLabel("拉筋")).toBe("拉筋");
  });

  // Guards: REQ-124
  test("TestHabitRecordsAndFields", () => {
    const h = toHabit({
      id: "H-3",
      fields: {
        name: "拉筋",
        agent: "fitness",
        cadence: "daily",
        mode: "streak",
        start: "2026-10-05",
        status: "ended",
        ended: "2026-10-20",
        sources: ["fitness:*"],
        subtasks: ["a", "b"],
        allow_miss: 1,
      },
      body: "約定時聊的",
    });
    expect(h).toMatchObject({
      id: "H-3",
      cadence: "daily",
      target: 7,
      missDays: 1,
      grace: 1,
      ended: "2026-10-20",
      allowMiss: 1,
      body: "約定時聊的",
    });
    // 存檔欄位：weekly 才帶 target，名稱去空白。
    expect(fieldsFromInput({ ...ok(), name: " 重訓 " })).toMatchObject({ name: "重訓", target: 3 });
    expect(fieldsFromInput({ ...ok(), cadence: "daily" })).not.toHaveProperty("target");
    expect(statusId("H-12")).toBe("h-12");
    expect(checkinKey("H-1", "2026-10-25", "量體重")).toBe("manual:H-1:2026-10-25:量體重");
    expect(checkinKey("H-1", "2026-10-25")).toBe("manual:H-1:2026-10-25:");
    const sorted = sortHabits([
      { ...h, id: "H-10" },
      { ...h, id: "H-2" },
    ]);
    expect(sorted.map((x) => x.id)).toEqual(["H-2", "H-10"]);
  });

  // Guards: REQ-124
  test("TestStatusAndProgressText", () => {
    const st: HabitStatus = toStatus({
      id: "h-1",
      fields: {
        habit: "H-1",
        streak_weeks: 3,
        achieved_weeks: 5,
        earned_points: 1,
        next_need_weeks: 1,
        this_week: { done: 2, need: 3, days: ["2026-10-26"] },
        weeks: [{ week: "2026-10-19", count: 3, result: "hit" }],
        today_subs: [{ name: "a", done: true, how: "auto" }],
        today_manual: 1,
        computed_at: "2026-10-26T10:00:00+08:00",
      },
    });
    expect(st).toMatchObject({ streakWeeks: 3, earnedPoints: 1, done: 2, need: 3, todayManual: 1 });
    expect(st.weeks[0]).toEqual({ week: "2026-10-19", count: 3, result: "hit" });
    expect(progressText("weekly", st)).toBe("本週 2 / 3 次");
    expect(progressText("daily", { ...st, need: 7, done: 5 })).toBe("本週 5 / 7 天");
    expect(progressText("weekly", undefined)).toBe("還在算");
    expect(nextPointText(st)).toBe("再達標 1 週就拿到 1 點");
    expect(nextPointText({ ...st, nextNeedWeeks: 0 })).toBe("這週達標就拿到 1 點");
    expect(doneToday(st, "2026-10-26")).toBe(true);
    expect(doneToday(st, "2026-10-27")).toBe(false);
    expect(doneToday(undefined, "2026-10-26")).toBe(false);
    expect(toWallet({ fields: { earned: 3, spent: 1, points: 2 } })).toEqual({
      earned: 3,
      spent: 1,
      points: 2,
      taskPoints: 0,
      taskExchanged: 0,
      outfitsReady: false,
    });
    expect(toWallet(null)).toEqual({
      earned: 0,
      spent: 0,
      points: 0,
      taskPoints: 0,
      taskExchanged: 0,
      outfitsReady: false,
    });
    // 點數換不換得到衣服照程式算好的 outfits_ready（HAB-23）；不是 true 一律當換不到。
    expect(toWallet({ fields: { outfits_ready: true } }).outfitsReady).toBe(true);
    expect(toWallet({ fields: { outfits_ready: "true" } }).outfitsReady).toBe(false);
  });

  test("只有一位秘書才不問跟誰約（HAB-22，DEV-157）", () => {
    expect(soleAgent({ life: "露娜" })).toBe("life");
    expect(soleAgent({ life: "露娜", code: "雅典娜" })).toBe("");
    expect(soleAgent({})).toBe("");
    expect(soleAgent(undefined)).toBe("");
  });

  // Guards: REQ-124, REQ-114
  test("TestNotDoneCount", () => {
    const base = toHabit({
      id: "H-1",
      fields: { name: "a", agent: "x", cadence: "weekly", start: "2026-10-01", status: "active" },
    });
    const mk = (id: string, over: Partial<Habit>): Habit => ({ ...base, id, ...over });
    const habits = [
      mk("H-1", {}),
      mk("H-2", {}),
      mk("H-3", { status: "ended" }),
      mk("H-4", { start: "2026-11-01" }),
    ];
    const done = toStatus({ fields: { this_week: { done: 1, need: 1, days: ["2026-10-26"] } } });
    expect(notDoneCount(habits, { "h-1": done }, "2026-10-26")).toBe(1);
    expect(notDoneCount(habits, {}, "2026-10-26")).toBe(2);
  });
});

// 即時更新重讀時，主人正在改、還沒存的習慣表單不被洗掉；沒動過的就換成新存好的（PKG-49 補）。
describe("TestUnsavedHabitEditsSurviveLiveReload", () => {
  const base = { name: "喝水", cadence: "daily" } as unknown as Parameters<typeof nextFormValue>[0];
  const next = { name: "喝水 2 杯", cadence: "daily" } as unknown as Parameters<
    typeof nextFormValue
  >[0];
  test("沒動過：換成新的", () => {
    expect(nextFormValue({ ...base }, base, next)).toEqual(next);
  });
  test("改了還沒存：留著主人改的", () => {
    const edited = { ...base, name: "喝水（改）" };
    expect(nextFormValue(edited, base, next)).toBe(edited);
  });
});

// 今天頁的習慣卡：打卡狀態讀不到時要顯示錯誤（給再試一次），不能一直轉圈（HAB-12）。
test("TestTodayCardShowsErrorWhenStatusFails", () => {
  expect(
    todayCardState([
      { data: [], error: "" },
      { data: undefined, error: "連不上" },
    ]),
  ).toBe("error");
  expect(
    todayCardState([
      { data: undefined, error: "" },
      { data: {}, error: "" },
    ]),
  ).toBe("loading");
  expect(
    todayCardState([
      { data: [], error: "" },
      { data: {}, error: "" },
    ]),
  ).toBe("ready");
});

describe("pickOneThing", () => {
  const today = "2026-11-10";
  const yesterday = "2026-11-09";
  const habit = (id: string, f: Record<string, unknown> = {}) =>
    toHabit({ id, fields: { name: id, cadence: "daily", start: "2026-11-01", ...f } });
  const status = (f: Record<string, unknown>) => toStatus({ fields: f });
  const doneYesterday = { done_days: [yesterday] };

  test("快連兩次漏的先", () => {
    const hs = [habit("H-1"), habit("H-2")];
    const st = { "h-1": status(doneYesterday), "h-2": status({}) };
    expect(pickOneThing(hs, st, today, yesterday)?.id).toBe("H-2");
  });
  test("今天做過一件就不再給一般的，但快斷的還是給", () => {
    const doneToday = { this_week: { days: [today] }, done_days: [yesterday, today] };
    const hs = [habit("H-1"), habit("H-2")];
    expect(
      pickOneThing(
        hs,
        { "h-1": status(doneToday), "h-2": status(doneYesterday) },
        today,
        yesterday,
      ),
    ).toBeNull();
    expect(
      pickOneThing(hs, { "h-1": status(doneToday), "h-2": status({}) }, today, yesterday)?.id,
    ).toBe("H-2");
  });
  test("每週型本週滿了、還沒開始的不挑", () => {
    const hs = [
      habit("H-1", { cadence: "weekly", target: 1 }),
      habit("H-2", { start: "2026-12-01" }),
    ];
    const st = { "h-1": status({ this_week: { done: 1, need: 1 } }) };
    expect(pickOneThing(hs, st, today, yesterday)).toBeNull();
  });
});

describe("monthCells", () => {
  test("週一開頭，未來和開始前不能點，手動打的才能撤銷", () => {
    const h = { start: "2026-11-03", status: "active", ended: "" };
    const st = {
      doneDays: ["2026-11-04", "2026-11-05"],
      manualDays: ["2026-11-04"],
      skippedDays: ["2026-11-06"],
      brokenDays: [],
    };
    const cells = monthCells(2026, 11, h, st, "2026-11-07");
    expect(cells.slice(0, 6)).toEqual([null, null, null, null, null, null]); // 11/1 是週日
    const at = (d: string) => cells.find((c) => c?.date === d);
    expect(at("2026-11-02")?.clickable).toBe(false);
    expect(at("2026-11-04")).toMatchObject({ state: "done", clickable: true, canUndo: true });
    expect(at("2026-11-05")).toMatchObject({ state: "done", clickable: false, canUndo: false });
    expect(at("2026-11-06")).toMatchObject({ state: "skipped", clickable: true });
    expect(at("2026-11-07")).toMatchObject({ state: "open", clickable: true });
    expect(at("2026-11-08")?.clickable).toBe(false);
  });
});

describe("weekCells", () => {
  test("本週週一到週日，做了、跳過、未來分得清楚（週日當天也算同一週）", () => {
    const h = { start: "2026-11-03", status: "active", ended: "" };
    const st = {
      doneDays: ["2026-11-04"],
      manualDays: [],
      skippedDays: ["2026-11-05"],
      brokenDays: [],
    };
    const sat = weekCells(h, st, "2026-11-07");
    expect(sat.map((c) => c.date)).toEqual([
      "2026-11-02",
      "2026-11-03",
      "2026-11-04",
      "2026-11-05",
      "2026-11-06",
      "2026-11-07",
      "2026-11-08",
    ]);
    expect(sat.map((c) => c.state)).toEqual([
      "none",
      "open",
      "done",
      "skipped",
      "open",
      "open",
      "none",
    ]);
    expect(weekCells(h, st, "2026-11-08")[0]?.date).toBe("2026-11-02");
    expect(weekCells(h, st, "2026-11-09")[0]?.date).toBe("2026-11-09");
    // 跨年、斷了、結束的習慣
    const y = weekCells(
      { start: "2026-12-01", status: "ended", ended: "2026-12-31" },
      { doneDays: [], manualDays: [], skippedDays: [], brokenDays: ["2026-12-30"] },
      "2027-01-02",
    );
    expect(y.map((c) => c.date)[0]).toBe("2026-12-28");
    expect(y.map((c) => c.state)).toEqual([
      "open",
      "open",
      "broken",
      "open",
      "none",
      "none",
      "none",
    ]);
  });
});

describe("withPendingMark（DEV-156 U-20）", () => {
  const base = {
    done: 1,
    need: 3,
    days: ["2026-11-12"],
    doneDays: ["2026-11-12"],
    todaySubs: [],
    todayManual: 0,
  } as unknown as Parameters<typeof withPendingMark>[1];
  const today = "2026-11-14";
  const plain = { subtasks: [], allowMiss: 0 };

  test("同一份進度底下連續勾兩個子項目：兩個都留著，前一個不會彈回沒勾（頁面每次都從伺服器那份重套）", () => {
    const two = {
      subtasks: [{ name: "甲" }, { name: "乙" }],
      allowMiss: 0,
    } as unknown as Parameters<typeof withPendingMarks>[0];
    const after = withPendingMarks(two, base, today, [
      { kind: "checkin", sub: "甲" },
      { kind: "checkin", sub: "乙" },
    ]);
    expect(after.todaySubs.map((s) => s.name).sort()).toEqual(["乙", "甲"]);
    expect(after.days).toContain(today);
    expect(after.done).toBe(2);
    // 勾了又撤掉甲：只剩乙、今天不算做了。
    const back = withPendingMarks(two, base, today, [
      { kind: "checkin", sub: "甲" },
      { kind: "checkin", sub: "乙" },
      { kind: "undo", sub: "甲" },
    ]);
    expect(back.todaySubs.map((s) => s.name)).toEqual(["乙"]);
    expect(back.days).not.toContain(today);
    expect(back.done).toBe(1);
  });

  test("打卡：數字馬上加一、今天算做了、可以撤銷；撤銷就退回去", () => {
    const after = withPendingMark(plain, base, today, { kind: "checkin" });
    expect(after.done).toBe(2);
    expect(after.days).toContain(today);
    expect(after.todayManual).toBe(1);
    const back = withPendingMark(plain, after, today, { kind: "undo" });
    expect(back.done).toBe(1);
    expect(back.days).not.toContain(today);
    expect(back.todayManual).toBe(0);
    // 已經算做了的再打一次不會多加。
    expect(withPendingMark(plain, after, today, { kind: "checkin" }).done).toBe(2);
    // 原本那份不被改動。
    expect(base.days).toEqual(["2026-11-12"]);
  });

  test("有子項目：勾到夠的項數今天才算做了；撤銷掉到不夠就不算", () => {
    const h = { subtasks: ["伸展", "深蹲", "平板"], allowMiss: 1 };
    const one = withPendingMark(h, base, today, { kind: "checkin", sub: "伸展" });
    expect(one.todaySubs).toEqual([{ name: "伸展", done: true, how: "manual" }]);
    expect(one.done).toBe(1);
    const two = withPendingMark(h, one, today, { kind: "checkin", sub: "深蹲" });
    expect(two.done).toBe(2);
    expect(two.days).toContain(today);
    const less = withPendingMark(h, two, today, { kind: "undo", sub: "深蹲" });
    expect(less.done).toBe(1);
    expect(less.days).not.toContain(today);
    expect(less.todaySubs.map((s) => s.name)).toEqual(["伸展"]);
  });
});

describe("taskLeft", () => {
  test("總點數一直往上加，還沒換的滿 30 才能兌換（DEV-149）", () => {
    const w = {
      earned: 0,
      spent: 0,
      points: 0,
      taskPoints: 67,
      taskExchanged: 20,
      outfitsReady: false,
    };
    expect(taskLeft(w)).toEqual({ left: 47, canExchange: true });
    expect(taskLeft({ ...w, taskExchanged: 40 })).toEqual({ left: 27, canExchange: false });
  });
});

describe("規則說明（DEV-142）", () => {
  const habit = (f: Record<string, unknown>): Habit =>
    toHabit({
      id: "H-1",
      fields: { name: "x", cadence: "weekly", target: 2, start: "2026-10-05", ...f },
    });

  test("沒寫 bonus_weeks 預設 4；存檔只寫表單管的欄位", () => {
    expect(habit({}).bonusWeeks).toBe(4);
    expect(habit({ bonus_weeks: 0 }).bonusWeeks).toBe(0);
    const f = fieldsFromInput(ok());
    for (const k of [
      "bonus_weeks",
      "mode",
      "miss_days",
      "week_skips",
      "grace",
      "sources",
      "subtasks",
      "allow_miss",
      "identity",
    ])
      expect(f).not.toHaveProperty(k);
  });

  test("連續加分週數要在 0–12", () => {
    expect(validateHabit({ ...ok(), bonusWeeks: 13 })).toMatch(/2 到 12/);
    expect(validateHabit({ ...ok(), bonusWeeks: 1 })).toMatch(/2 到 12/);
    // 頁面表單只檢查表單上的欄位：秘書寫壞的子項目不擋主人改名字。
    expect(validateForm({ ...ok(), allowMiss: 9, missDays: 99 })).toBeNull();
    expect(validateForm({ ...ok(), name: " " })).toBe("習慣要有名字");
    expect(validateHabit({ ...ok(), bonusWeeks: 0 })).toBeNull();
  });

  test("自動、自己按、部分自動分得清楚", () => {
    expect(howDone(habit({ sources: ["fitness:重訓"] }))).toBe("auto");
    expect(howDone(habit({}))).toBe("manual");
    expect(
      howDone(habit({ subtasks: ["量體重=body:weight", "記錄吃飯=nutrition:meals>=1"] })),
    ).toBe("auto");
    expect(howDone(habit({ subtasks: ["量體重=body:weight", "拉筋"] }))).toBe("mixed");
  });

  test("完成那一行念得出來", () => {
    expect(sourceText("fitness:重訓")).toBe("記了重訓");
    expect(doneLine(habit({ sources: ["fitness:重訓"] }))).toBe("每週 2 次・記了重訓就自動算");
    expect(doneLine(habit({ cadence: "daily" }))).toBe("每天・做完自己按打卡");
    expect(
      doneLine(
        habit({
          cadence: "daily",
          subtasks: ["量體重=body:weight", "記錄吃飯=nutrition:meals>=1"],
        }),
      ),
    ).toBe("每天・量體重＋記錄吃飯都做到才算（自動算）");
  });

  // DEV-156 U-14：卡片只寫下一步，完整規則在「怎麼算」。
  test("點數那一行只寫下一步", () => {
    expect(pointLine({ nextNeedWeeks: 1 })).toBe("再達標 1 週就拿到 1 點");
    expect(pointLine({ nextNeedWeeks: 3 })).toBe("再達標 3 週就拿到 1 點");
    expect(pointLine({ nextNeedWeeks: 0 })).toBe("這週達標就拿到 1 點");
    expect(pointLine(undefined)).toBe("還在算");
  });

  test("待辦那張卡：只說累積還差多少、滿了換幾點；規則照 RMD-26 寫，換不到衣服不提衣服", () => {
    const w = (taskPoints: number, taskExchanged = 0) => ({
      earned: 0,
      spent: 0,
      points: 0,
      taskPoints,
      taskExchanged,
      outfitsReady: false,
    });
    expect(todoNextLine(w(12))).toBe("再累積 18 就多 1 點");
    expect(todoProgress(w(12))).toBe(12);
    expect(todoNextLine(w(65, 30))).toBe("累積滿了，可以換 1 點");
    expect(todoProgress(w(65, 30))).toBe(30);
    expect(todoNextLine(w(60))).toBe("累積滿了，可以換 2 點");
    const how = todoHowLines(false).join("");
    expect(how).toContain("簡單 1、普通 2、困難 3");
    expect(how).toContain("問我做了沒");
    expect(how).not.toContain("件");
    expect(how).not.toContain("衣服");
    expect(todoHowLines(true).join("")).toContain("展示間換衣服");
  });

  test("規則說明分三段，每段各自的欄位", () => {
    const st = toStatus({
      fields: {
        streak_weeks: 5,
        achieved_weeks: 5,
        earned_points: 2,
        next_need_weeks: 1,
        bonus_weeks: 4,
        bonus_points: 1,
        next_bonus_weeks: 3,
      },
    });
    const secs = ruleSections(habit({ sources: ["fitness:重訓"] }), st);
    expect(secs.map((s) => s.title)).toEqual(["怎樣算完成", "怎樣拿點", "現在進度"]);
    const now = Object.fromEntries(secs[2]?.rows ?? []);
    expect(now.連續達標).toBe("5 週");
    expect(now.離連續加分).toBe("再連續 3 週");
    expect(now.這個習慣賺到).toBe("2 點（含連續加分 1 點）");
    expect(ruleSections(habit({}), undefined)[2]?.rows).toEqual([["狀態", "還在算"]]);
  });
});

describe("skipNo", () => {
  test("同一週第二次跳過起 skipNo 是 2，下週重算（HAB-17）", () => {
    const h = { start: "2026-11-01", status: "active", ended: "" };
    const st = {
      doneDays: [],
      manualDays: [],
      skippedDays: ["2026-11-03", "2026-11-05", "2026-11-10"],
      brokenDays: [],
    };
    const cells = monthCells(2026, 11, h, st, "2026-11-12");
    const at = (d: string) => cells.find((c) => c?.date === d);
    expect(at("2026-11-03")?.skipNo).toBe(1);
    expect(at("2026-11-05")?.skipNo).toBe(2);
    expect(at("2026-11-10")?.skipNo).toBe(1);
    expect(at("2026-11-04")?.skipNo).toBe(0);
    // 同一天也在做了裡面：畫成做了、不算跳過次數
    const both = monthCells(2026, 11, h, { ...st, doneDays: ["2026-11-03"] }, "2026-11-12");
    expect(both.find((c) => c?.date === "2026-11-05")?.skipNo).toBe(1);
  });
});
