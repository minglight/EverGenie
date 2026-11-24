import type { Task } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import type { Site } from "../../data/sites";
import { PRIMARY } from "../../data/sites";
import { filteredViewCount } from "./viewCounts";

const site = { ...PRIMARY } as unknown as Site;
const row = (agent: string) => ({ task: { agent } as Task, waitingDays: 0 });

describe("任務單分段的數字", () => {
  // WEB-96：只看一位秘書時，「等你 N」「進行中 N」跟清單一樣只算她的，不是全部的。
  test("跟清單同一個秘書篩選；沒選人是全部；還沒讀到是 undefined", () => {
    const parts = [{ site, data: [row("code"), row("code"), row("life")] }];
    expect(filteredViewCount(parts, [])).toBe(3);
    expect(filteredViewCount(parts, ["life"])).toBe(1);
    expect(filteredViewCount(parts, ["code"])).toBe(2);
    expect(filteredViewCount([{ site, data: undefined }], ["life"])).toBeUndefined();
  });

  // DEV-107：狀態標籤的數字是從「全部未結」挑出來的，要同時守住秘書篩選。
  test("only 只算挑中的那種狀態，並且跟秘書篩選一起算", () => {
    const withStatus = (agent: string, status: Task["status"]) => ({
      task: { agent, status } as Task,
      waitingDays: 0,
    });
    const parts = [
      {
        site,
        data: [
          withStatus("code", "doing"),
          withStatus("code", "idea"),
          withStatus("life", "doing"),
          withStatus("life", "waiting_external"),
        ],
      },
    ];
    const doing = (r: { task: Task }) => r.task.status === "doing";
    expect(filteredViewCount(parts, [], doing)).toBe(2);
    expect(filteredViewCount(parts, ["life"], doing)).toBe(1);
    expect(filteredViewCount(parts, ["code"], (r) => r.task.status === "waiting_external")).toBe(0);
  });
});
