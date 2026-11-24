import type { BackupSweepReport } from "@evergenie/sdk-web";
import { describe, expect, it } from "vitest";
import { type SweepGroupKey, sweepCount, sweepDetail, sweepGroups } from "./backupSweepView";

const rep: BackupSweepReport = {
  at: "2026-11-22T04:30:00+08:00",
  took_ms: 1200,
  current: 4,
  current_bytes: 2048,
  pruned: 0,
  pruned_bytes: 0,
  expired: [
    {
      path: "~/a.bak-20261001-x",
      size: 1,
      mod_time: "",
      state: "expired",
      checked: "查過：沒有開機服務、執行中的程式、終端機視窗用到它",
    },
    {
      path: "~/svc.bak-20260901-move",
      size: 1,
      mod_time: "",
      state: "in_use",
      used_by: "開機服務：x.plist（指到上層資料夾）",
    },
    { path: "~/c.bak-20260901-y", size: 1, mod_time: "", state: "unchecked", checked: "沒查完" },
  ],
  misnamed: [
    {
      path: "~/n.bak",
      size: 1,
      mod_time: "",
      problem: "沒有日期",
      suggest: "改名成 n.bak-20261022-<原因>",
    },
  ],
  skipped: [{ path: "~/repo", size: 0, mod_time: "", reason: "git 管的" }],
};

describe("備份掃描報告：畫面的純函式", () => {
  it("已到期照伺服器給的狀態分成可刪、在用、沒查完；在用的不會出現在可刪", () => {
    const groups = sweepGroups(rep);
    expect(groups.map((g) => g.key)).toEqual([
      "deletable",
      "inUse",
      "unchecked",
      "misnamed",
      "skipped",
    ]);
    expect(groups[0]?.items.map((it) => it.path)).toEqual(["~/a.bak-20261001-x"]);
    expect(groups[1]?.items.map((it) => it.path)).toEqual(["~/svc.bak-20260901-move"]);
    expect(sweepCount(groups, "unchecked")).toBe(1);
    expect(sweepCount(groups, "misnamed")).toBe(1);
  });

  it("還沒掃過是五個空組", () => {
    expect(sweepGroups(undefined).every((g) => g.items.length === 0)).toBe(true);
  });

  it("細節那一行：可刪寫查過什麼、在用寫誰指到、不合規寫問題與建議", () => {
    const first = (key: SweepGroupKey) => {
      const it = sweepGroups(rep).find((g) => g.key === key)?.items[0];
      if (!it) throw new Error(`${key} 沒有東西`);
      return sweepDetail(key, it);
    };
    expect(first("deletable")).toContain("開機服務、執行中的程式、終端機視窗");
    expect(first("inUse")).toContain("上層資料夾");
    expect(first("misnamed")).toBe("沒有日期；改名成 n.bak-20261022-<原因>");
    expect(first("skipped")).toBe("git 管的");
  });
});
