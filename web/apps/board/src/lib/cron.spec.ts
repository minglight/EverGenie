import { describe, expect, test } from "vitest";
import {
  buildCron,
  describeCron,
  describeCrons,
  describeJobTime,
  modeOf,
  parseSimple,
} from "./cron";

describe("cron 與白話", () => {
  // Guards: REQ-18
  test("每天：幾點幾分補成兩位數", () => {
    expect(describeCron("0 8 * * *")).toBe("每天 08:00");
    expect(describeCron("40 20 * * *")).toBe("每天 20:40");
    expect(describeCron("5 9,15 * * *")).toBe("每天 09:05、15:05");
  });

  // Guards: REQ-18
  test("每週幾：逗號、範圍、週日（0 或 7）都認得，依週一到週日排", () => {
    expect(describeCron("0 15 * * 1,3,5")).toBe("每週一三五 15:00");
    expect(describeCron("30 9 * * 1-5")).toBe("每週一二三四五 09:30");
    expect(describeCron("0 10 * * 0,6")).toBe("每週六日 10:00");
    expect(describeCron("0 10 * * 7")).toBe("每週日 10:00");
    expect(describeCron("0 10 * * 0-6")).toBe("每天 10:00");
  });

  // WEB-95：每隔一段時間的排程寫白話，不要把「* * * * *」原樣丟給主人。
  test("每隔一段時間：每分鐘、每 N 分鐘、每小時、每 N 小時寫白話；其他看不懂的原樣", () => {
    expect(describeCron("* * * * *")).toBe("每分鐘");
    expect(describeCron("*/30 * * * *")).toBe("每 30 分鐘");
    expect(describeCron("5 * * * *")).toBe("每小時的 05 分");
    expect(describeCron("0 */2 * * *")).toBe("每 2 小時的 00 分");
    expect(describeCron("*/0 * * * *")).toBe("*/0 * * * *");
    // 不能整除的間隔跨整點、跨午夜會變短，寫「每 N」不對：原樣。
    expect(describeCron("*/7 * * * *")).toBe("*/7 * * * *");
    expect(describeCron("0 */5 * * *")).toBe("0 */5 * * *");
    expect(describeCron("*/5 * * * 1-5")).toBe("*/5 * * * 1-5");
    expect(describeCron("0 9 1 * *")).toBe("0 9 1 * *");
  });

  // Guards: REQ-18
  test("多組：用「；」連起來", () => {
    expect(describeCrons(["0 9 * * 1-5", "30 13 * * 6"])).toBe(
      "每週一二三四五 09:00；每週六 13:30",
    );
    expect(describeCrons([])).toBe("");
  });

  // Guards: REQ-18
  test("看不懂的原樣顯示", () => {
    for (const raw of ["0 9 1 * *", "0 9 * 1 *", "0 25 * * *", "0 9 * * 1-9", "每天"]) {
      expect(describeCron(raw)).toBe(raw);
    }
  });

  // Guards: REQ-18
  test("時段型：時段與每天最多幾次；週末時段另外寫", () => {
    expect(describeJobTime({ windows: ["10:00-12:00"], per_day: 2 })).toBe(
      "時段 10:00–12:00、每天最多 2 次",
    );
    expect(describeJobTime({ windows: ["12:15-13:15", "15:30-17:00"] })).toBe(
      "時段 12:15–13:15、15:30–17:00、每天最多 1 次",
    );
    expect(
      describeJobTime({ windows: ["12:00-13:00"], weekend_windows: ["10:00-11:00"], per_day: 1 }),
    ).toBe("時段 12:00–13:00、每天最多 1 次（週末 10:00–11:00）");
    expect(describeJobTime({ cron: ["0 8 * * *"] })).toBe("每天 08:00");
  });

  // Guards: REQ-18
  test("編輯器用的轉換：建出來的 cron 讀得回同樣的內容", () => {
    expect(buildCron([], "08:05")).toBe("5 8 * * *");
    expect(buildCron([3, 1], "09:30")).toBe("30 9 * * 1,3");
    expect(parseSimple(buildCron([0, 6], "21:00"))).toEqual({ days: [0, 6], times: ["21:00"] });
  });

  // Guards: REQ-18
  test("目前的 cron 用哪個分頁開", () => {
    expect(modeOf(["0 8 * * *"])).toBe("daily");
    expect(modeOf(["0 8 * * 1,3"])).toBe("weekly");
    expect(modeOf(["0 8,12 * * *"])).toBe("advanced");
    expect(modeOf(["0 8 * * *", "0 9 * * 6"])).toBe("advanced");
    expect(modeOf(["*/5 * * * *"])).toBe("advanced");
    expect(modeOf([])).toBe("daily");
  });
});
