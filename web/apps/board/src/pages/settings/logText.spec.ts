import { describe, expect, test } from "vitest";
import { groupByDay, kindText, opText, targetOf, whoText } from "./logText";

describe("設定→紀錄的白話對照", () => {
  // Guards: REQ-111
  test("op 的白話表：新增、更新、修改、追加、封存、搬移；不認得的原樣顯示", () => {
    expect(opText("create")).toBe("新增");
    expect(opText("put")).toBe("更新");
    expect(opText("patch")).toBe("修改");
    expect(opText("append")).toBe("追加");
    expect(opText("archive")).toBe("封存");
    expect(opText("move")).toBe("搬移");
    expect(opText("import-archived")).toBe("匯入封存");
    expect(opText("weird")).toBe("weird");
    expect(opText(undefined)).toBe("");
  });

  // Guards: REQ-111
  test("種類的白話名；夜班清單那一筆、套件的資料種類都有白話", () => {
    expect(kindText("task")).toBe("任務單");
    expect(kindText("reminder")).toBe("待辦");
    expect(kindText("goal")).toBe("Goal");
    expect(kindText("policy_request")).toBe("權限申請");
    expect(kindText("note", "system/night-queue")).toBe("夜班清單");
    expect(kindText("note", "code/x")).toBe("筆記");
    expect(kindText("nutrition.meal")).toBe("套件資料（nutrition.meal）");
    expect(kindText("unknown_kind")).toBe("unknown_kind");
  });

  // Guards: REQ-111
  test("誰：owner(…) 是主人，名冊上的職位 id 是她的名字，其他是系統", () => {
    const names: Record<string, string> = { code: "Athena" };
    const nameOf = (id: string) => names[id];
    expect(whoText("owner(web)", nameOf)).toBe("主人");
    expect(whoText("owner(cli)", nameOf)).toBe("主人");
    expect(whoText("code", nameOf)).toBe("Athena");
    expect(whoText("sweeper", nameOf)).toBe("系統");
    expect(whoText(undefined, nameOf)).toBe("系統");
  });

  // Guards: REQ-111
  test("連結對照：任務、待辦、訊息連到詳情；權限申請、秘書、夜班清單連到所在頁；Goal 沒有獨立頁不連", () => {
    expect(targetOf("task", "SEC-9")).toEqual({ to: "/tasks/$id", id: "SEC-9" });
    expect(targetOf("reminder", "R-3")).toEqual({ to: "/reminders/$id", id: "R-3" });
    expect(targetOf("message", "M-4")).toEqual({ to: "/messages/$id", id: "M-4" });
    expect(targetOf("policy_request", "P-1")).toEqual({ to: "/policy" });
    expect(targetOf("agent", "code")).toEqual({ to: "/settings/agents" });
    expect(targetOf("note", "system/night-queue")).toEqual({ to: "/settings/machine" });
    expect(targetOf("note", "code/other")).toBeNull();
    expect(targetOf("goal", "GOAL-1")).toBeNull();
    expect(targetOf("nutrition.meal", "2026-10-25")).toBeNull();
  });

  // Guards: REQ-111
  test("依當地日期分組，保持新到舊；時間壞掉的略過", () => {
    const at = (h: number, d = 25) => new Date(2026, 9, d, h, 0, 0).toISOString();
    const groups = groupByDay([
      { at: at(10), id: "a" },
      { at: at(9), id: "b" },
      { at: "壞掉", id: "x" },
      { at: at(23, 24), id: "c" },
    ]);
    expect(groups.map((g) => [g.day, g.items.map((i) => i.id)])).toEqual([
      ["2026-10-25", ["a", "b"]],
      ["2026-10-24", ["c"]],
    ]);
  });
});
