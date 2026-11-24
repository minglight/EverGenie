import { describe, expect, test } from "vitest";
import { remindersQuery, reminderViews } from "./queries";

describe("待辦的清單不輪詢、改推送（S8）", () => {
  // 沒有超過 3 秒的輪詢：待辦一變 vault.changed 就重抓（data/live.ts）。
  // Guards: REQ-389
  test("每個檢視的 query 沒有超過 3 秒的 refetchInterval", () => {
    for (const o of reminderViews.map((v) => remindersQuery(v))) {
      const every = (o as { refetchInterval?: unknown }).refetchInterval;
      expect(
        every === undefined || every === false || (typeof every === "number" && every <= 3_000),
        `${JSON.stringify(o.queryKey)} refetchInterval=${String(every)}`,
      ).toBe(true);
    }
  });
});
