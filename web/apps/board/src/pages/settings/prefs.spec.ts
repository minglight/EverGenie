import type { NotifyPrefs } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import {
  agentModeOf,
  agentModesFor,
  agentOverrideCount,
  DEFAULT_TASKS,
  hoursOf,
  isClock,
  joinQuiet,
  modeOf,
  modesFor,
  overrideCount,
  parseHours,
  parseTaskNumber,
  presetRows,
  quietParts,
  revertField,
  tasksOf,
  withAgentMode,
  withHours,
  withMode,
  withPreset,
  withQuiet,
  withTask,
} from "./prefs";

const base: NotifyPrefs = {
  preset: "standard",
  quiet_hours: "23:00-07:00",
  channel_app: true,
  channel_line: false,
  reask_after_minutes: 30,
  reask_max_asks: 3,
  categories: {
    approve: { mode: "instant", digest_hours: 3 },
    done: { mode: "instant", digest_hours: 3 },
    due: { mode: "instant", digest_hours: 3 },
    digest: { mode: "digest", digest_hours: 3 },
    opening: { mode: "board", digest_hours: 3 },
  },
};

describe("通知偏好的欄位", () => {
  // Guards: REQ-26
  test("「要你同意」沒有「關閉」，其他分類四種都有", () => {
    expect(modesFor("approve")).toEqual(["instant", "digest", "board"]);
    expect(modesFor("done")).toEqual(["instant", "digest", "board", "off"]);
  });

  // Guards: REQ-24
  test("改送法只動那一類，其他欄位原封不動", () => {
    const next = withMode(base, "done", "board");
    expect(modeOf(next, "done")).toBe("board");
    expect(modeOf(next, "due")).toBe("instant");
    expect(next.quiet_hours).toBe("23:00-07:00");
    expect(next.channel_app).toBe(true);
    // 不改到原本的物件（React 的 state 不能就地改）。
    expect(modeOf(base, "done")).toBe("instant");
  });

  // Guards: REQ-24
  test("切到合併摘要時間隔一定大於零；沒設過就用 3 小時", () => {
    const zero: NotifyPrefs = {
      ...base,
      categories: { ...base.categories, due: { mode: "board", digest_hours: 0 } },
    };
    const next = withMode(zero, "due", "digest");
    expect(next.categories?.due).toEqual({ mode: "digest", digest_hours: 3 });
    expect(hoursOf(withHours(base, "digest", 6), "digest")).toBe(6);
  });

  // Guards: REQ-24
  test("伺服器拒絕時，把改過的那一格換回伺服器上的值", () => {
    const edited = withHours(withMode(base, "digest", "board"), "digest", 9);
    expect(revertField(edited, base, "mode:digest").categories?.digest?.mode).toBe("digest");
    expect(hoursOf(revertField(edited, base, "mode:digest"), "digest")).toBe(9);
    expect(hoursOf(revertField(edited, base, "hours:digest"), "digest")).toBe(3);
    const quiet = withQuiet(base, "01:00-02:00");
    expect(revertField(quiet, base, "quiet").quiet_hours).toBe("23:00-07:00");
  });
});

describe("安靜時段", () => {
  // Guards: REQ-32
  test("拆成起訖、兩邊都填才能合回去", () => {
    expect(quietParts("23:00-07:00")).toEqual({ from: "23:00", to: "07:00" });
    expect(quietParts("")).toEqual({ from: "", to: "" });
    expect(quietParts(undefined)).toEqual({ from: "", to: "" });
    expect(joinQuiet("23:00", "07:00")).toBe("23:00-07:00");
    expect(joinQuiet("", "")).toBe("");
    expect(joinQuiet("23:00", "")).toBeNull();
    expect(joinQuiet("", "07:00")).toBeNull();
  });

  // Guards: REQ-24
  test("摘要間隔只收 1 以上的整數", () => {
    expect(parseHours("3")).toBe(3);
    expect(parseHours(" 12 ")).toBe(12);
    expect(parseHours("0")).toBeNull();
    expect(parseHours("")).toBeNull();
    expect(parseHours("1.5")).toBeNull();
    expect(parseHours("-2")).toBeNull();
    expect(parseHours("三")).toBeNull();
  });
});

describe("任務單跟進的欄位", () => {
  // Guards: REQ-148
  test("伺服器沒給就顯示預設值 09:00／3／3／3", () => {
    expect(tasksOf(base)).toEqual(DEFAULT_TASKS);
    expect(
      tasksOf({
        ...base,
        tasks: { nudge_at: "", stale_days: 5, nudge_every_days: 3, nudge_max: 0 },
      }),
    ).toEqual({
      nudge_at: "09:00",
      stale_days: 5,
      nudge_every_days: 3,
      nudge_max: 0,
    });
  });

  // Guards: REQ-148
  test("改一格不動別格，也不動別的偏好", () => {
    const next = withTask(base, "stale_days", 7);
    expect(next.tasks).toEqual({ ...DEFAULT_TASKS, stale_days: 7 });
    expect(next.quiet_hours).toBe("23:00-07:00");
    expect(base.tasks).toBeUndefined();
  });

  // Guards: REQ-148
  test("數字只收範圍內的整數", () => {
    expect(parseTaskNumber("stale_days", "30")).toBe(30);
    expect(parseTaskNumber("stale_days", "0")).toBeNull();
    expect(parseTaskNumber("stale_days", "31")).toBeNull();
    expect(parseTaskNumber("nudge_every_days", "1.5")).toBeNull();
    expect(parseTaskNumber("nudge_max", "0")).toBe(0);
    expect(parseTaskNumber("nudge_max", "10")).toBe(10);
    expect(parseTaskNumber("nudge_max", "11")).toBeNull();
    expect(parseTaskNumber("nudge_max", "")).toBeNull();
    expect(isClock("09:00")).toBe(true);
    expect(isClock("9:00")).toBe(false);
    expect(isClock("24:00")).toBe(false);
    expect(isClock("")).toBe(false);
  });

  // Guards: REQ-148
  test("伺服器拒絕時，只把那一格換回伺服器上的值", () => {
    const server = withTask(base, "stale_days", 4);
    const draft = withTask(withTask(server, "stale_days", 9), "nudge_max", 5);
    const back = revertField(draft, server, "tasks:stale_days");
    expect(back.tasks).toMatchObject({ stale_days: 4, nudge_max: 5 });
  });
});

// 核心給的預設表（notify-prefs.md 的預設值表）；畫面不自己算。
const presets: NonNullable<NotifyPrefs["presets"]> = {
  quiet: {
    approve: { mode: "instant", digest_hours: 3 },
    done: { mode: "board", digest_hours: 3 },
    due: { mode: "instant", digest_hours: 3 },
    digest: { mode: "board", digest_hours: 3 },
    opening: { mode: "off", digest_hours: 3 },
  },
  standard: {
    approve: { mode: "instant", digest_hours: 3 },
    done: { mode: "instant", digest_hours: 3 },
    due: { mode: "instant", digest_hours: 3 },
    digest: { mode: "digest", digest_hours: 3 },
    opening: { mode: "board", digest_hours: 3 },
  },
  active: {
    approve: { mode: "instant", digest_hours: 3 },
    done: { mode: "instant", digest_hours: 3 },
    due: { mode: "instant", digest_hours: 3 },
    digest: { mode: "instant", digest_hours: 3 },
    opening: { mode: "instant", digest_hours: 3 },
  },
};
const withPresets: NotifyPrefs = { ...base, presets };

describe("簡單模式：預設組", () => {
  // Guards: REQ-25
  test("預設組展開清單：每類的送法來自核心的預設表，摘要帶間隔", () => {
    const rows = presetRows(withPresets, "standard");
    expect(rows.map((r) => [r.category, r.mode])).toEqual([
      ["approve", "instant"],
      ["done", "instant"],
      ["due", "instant"],
      ["digest", "digest"],
      ["opening", "board"],
    ]);
    expect(rows.find((r) => r.category === "digest")?.hours).toBe(3);
    expect(presetRows(withPresets, "quiet").find((r) => r.category === "opening")?.mode).toBe(
      "off",
    );
  });

  // Guards: REQ-25
  test("「自己調過 N 項」：跟預設組不一樣的分類加上單一秘書另設；跟預設一樣的不算", () => {
    expect(overrideCount(withPresets)).toBe(0);
    const tweaked = withMode(withPresets, "done", "board");
    expect(overrideCount(tweaked)).toBe(1);
    const more = withAgentMode(withMode(tweaked, "due", "off"), "code", "done", "off");
    expect(overrideCount(more)).toBe(3);
    // 摘要的間隔只有「合併摘要」才算不一樣。
    expect(overrideCount(withHours(withPresets, "digest", 6))).toBe(1);
    expect(overrideCount(withHours(withPresets, "done", 6))).toBe(0);
    // 預設表讀不到時不亂報（只算單一秘書另設）。
    expect(overrideCount(withMode(base, "done", "off"))).toBe(0);
  });

  // Guards: REQ-25
  test("選預設組：逐類設定換成那組的預設、單一秘書另設清空，其他欄位不動", () => {
    const messy = withAgentMode(withMode(withPresets, "digest", "off"), "code", "done", "off");
    const next = withPreset(messy, "quiet");
    expect(next.preset).toBe("quiet");
    expect(modeOf(next, "done")).toBe("board");
    expect(modeOf(next, "opening")).toBe("off");
    expect(next.agents).toEqual([]);
    expect(next.quiet_hours).toBe("23:00-07:00");
    expect(overrideCount(next)).toBe(0);
    // 伺服器拒絕時還原整個預設組選擇。
    const back = revertField(next, messy, "preset");
    expect(back.preset).toBe("standard");
    expect(modeOf(back, "digest")).toBe("off");
    expect(agentOverrideCount(back, "code")).toBe(1);
  });
});

describe("完整模式：單一秘書另設", () => {
  // Guards: REQ-25, REQ-26
  test("每個分類可以「跟全域一樣」或另指定；「要你同意」不擺「關閉」", () => {
    expect(agentModesFor("done")).toEqual(["same", "instant", "digest", "board", "off"]);
    expect(agentModesFor("approve")).toEqual(["same", "instant", "digest", "board"]);
  });

  // Guards: REQ-25
  test("另設：指定送法就記下來，改回「跟全域一樣」就拿掉，一項都不剩整位拿掉", () => {
    let p = withAgentMode(base, "code", "done", "board");
    expect(agentModeOf(p, "code", "done")).toBe("board");
    expect(agentModeOf(p, "code", "due")).toBe("same");
    expect(agentModeOf(p, "life", "done")).toBe("same");
    p = withAgentMode(p, "code", "digest", "digest");
    expect(p.agents?.[0]?.categories?.digest).toEqual({ mode: "digest", digest_hours: 3 });
    expect(agentOverrideCount(p, "code")).toBe(2);
    p = withAgentMode(withAgentMode(p, "code", "done", "same"), "code", "digest", "same");
    expect(p.agents).toEqual([]);
    // 不動傳進來的物件。
    expect(base.agents).toBeUndefined();
  });

  // Guards: REQ-25
  test("伺服器拒絕時只還原那位秘書那一類", () => {
    const server = withAgentMode(base, "code", "done", "board");
    const draft = withAgentMode(withAgentMode(server, "code", "done", "off"), "life", "due", "off");
    const back = revertField(draft, server, "agent:code:done");
    expect(agentModeOf(back, "code", "done")).toBe("board");
    expect(agentModeOf(back, "life", "due")).toBe("off");
  });
});
