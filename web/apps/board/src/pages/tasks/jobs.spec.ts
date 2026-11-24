import type { Job } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import { groupJobs, newJobId, sourceKind } from "./jobs";

const job = (id: string, name: string, extra: Partial<Job> = {}): Job => ({
  id,
  name,
  agent: id.includes("/") ? id.split("/")[0] : undefined,
  enabled: true,
  archived: false,
  ...extra,
});

describe("排程清單分組", () => {
  // Guards: REQ-18
  test("依名冊順序分組，系統級最後，已封存的不列", () => {
    const groups = groupJobs(
      [
        job("usage-warmup", "額度暖機"),
        job("life/b", "b-job"),
        job("code/nightly", "nightly"),
        job("code/a", "a-first"),
        job("code/old", "舊的", { archived: true }),
        job("ghost/x", "沒在名冊的秘書"),
      ],
      ["code", "life"],
    );
    expect(groups.map((g) => g.agent)).toEqual(["code", "life", "ghost", ""]);
    expect(groups[0]?.jobs.map((j) => j.id)).toEqual(["code/a", "code/nightly"]);
    expect(groups.flatMap((g) => g.jobs).some((j) => j.id === "code/old")).toBe(false);
  });

  // Guards: REQ-18
  test("沒有排程時沒有任何一組", () => {
    expect(groupJobs([], ["code"])).toEqual([]);
  });
});

describe("新排程的 id", () => {
  // Guards: REQ-18
  test("英數字的名稱轉成小寫減號；純中文用時間戳", () => {
    expect(newJobId("code", "Morning  Report!", 1)).toBe("code/morning-report");
    expect(newJobId("life", "每週整理", 1700)).toBe("life/job-1700");
    expect(newJobId("life", "週報 weekly", 1)).toBe("life/weekly");
  });

  // Guards: REQ-18
  test("來源的三種說法", () => {
    expect(sourceKind("user")).toBe("user");
    expect(sourceKind("migrate")).toBe("migrate");
    expect(sourceKind("closet")).toBe("builtin");
    expect(sourceKind(undefined)).toBe("builtin");
  });
});
