import { describe, expect, test } from "vitest";
import { collapseDiff, diffCounts, splitItems } from "./rulesText";

describe("規矩頁", () => {
  test("文件照最高一級的標題切成一條一條，小標題跟著那一條", () => {
    const items = splitItems(
      "開頭說明\n\n### 第 1 條 甲\n\n內容一\n\n#### 1.1 細項\n\n細節\n\n### 第 2 條 乙\n\n內容二",
    );
    expect(items.map((i) => i.heading)).toEqual(["", "第 1 條 甲", "第 2 條 乙"]);
    expect(items[1]?.body).toContain("#### 1.1 細項");
    expect(items[2]?.body).toBe("內容二");
  });

  test("程式碼區塊裡的 # 不算標題", () => {
    const items = splitItems("## A\n\n```\n## 不是\n```\n\n## B\n\nb");
    expect(items.map((i) => i.heading)).toEqual(["A", "B"]);
  });

  test("差異只留改動附近，其他收成「幾行沒變」", () => {
    const same = (n: number) =>
      Array.from({ length: n }, (_, i) => ({ op: "same" as const, text: `s${i}` }));
    const diff = [
      ...same(10),
      { op: "del" as const, text: "舊" },
      { op: "add" as const, text: "新" },
      ...same(10),
    ];
    const out = collapseDiff(diff, 2);
    expect(out[0]).toEqual({ kind: "skip", count: 8, key: 8 });
    expect(out.filter((s) => s.kind === "line")).toHaveLength(6);
    expect(out.at(-1)).toMatchObject({ kind: "skip", count: 8 });
    expect(diffCounts(diff)).toEqual({ add: 1, del: 1 });
  });
});
