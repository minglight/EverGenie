import { beforeEach, describe, expect, it } from "vitest";
import { recordTrail, resetTrail, stepsBack } from "./backTrail";

describe("stepsBack", () => {
  beforeEach(resetTrail);

  it("前一筆不一樣就退一步", () => {
    recordTrail(0, "/");
    recordTrail(1, "/tasks");
    expect(stepsBack(1, "/tasks")).toBe(1);
  });

  it("刪除後疊出兩筆一樣的清單：一次跳過", () => {
    recordTrail(0, "/");
    recordTrail(1, "/tasks");
    recordTrail(2, "/tasks");
    expect(stepsBack(2, "/tasks")).toBe(2);
  });

  it("再往前都是同一個網址：沒有可退的", () => {
    recordTrail(0, "/tasks");
    recordTrail(1, "/tasks");
    expect(stepsBack(1, "/tasks")).toBeNull();
    expect(stepsBack(0, "/tasks")).toBeNull();
  });

  it("重新整理後認不得的筆當成不同", () => {
    recordTrail(3, "/tasks");
    expect(stepsBack(3, "/tasks")).toBe(1);
  });
});
