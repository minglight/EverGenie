import { describe, expect, test } from "vitest";
import { claimFollowUp, claimOf } from "./claim";

describe("認領通知", () => {
  // Guards: REQ-346
  test("「換一個」與打別的字開設定頁那張卡；「好」與「之後再說」不開；不是認領通知不開", () => {
    const n = { source: "package", claim: "garden" };
    expect(claimFollowUp(n, "換一個")).toEqual({
      to: "/settings/agents",
      search: { claim: "garden" },
    });
    expect(claimFollowUp(n, "我想用別人")).toEqual({
      to: "/settings/agents",
      search: { claim: "garden" },
    });
    expect(claimFollowUp(n, "好，用 花仙子")).toBeNull();
    expect(claimFollowUp(n, "之後再說")).toBeNull();
    expect(claimFollowUp({ source: "policy", claim: "garden" }, "換一個")).toBeNull();
    expect(claimFollowUp({ claim: "garden" }, "換一個")).toBeNull();
    expect(claimOf({ source: "package", claim: "" })).toBe("");
  });
});
