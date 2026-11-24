import { describe, expect, test } from "vitest";
import { greetingText } from "./greeting";

describe("greetingText", () => {
  test("用家的主人稱呼，還沒讀到就只有問候", () => {
    const morning = new Date(2026, 9, 24, 9, 0);
    expect(greetingText(morning, "小明")).toBe("早安，小明");
    expect(greetingText(morning, "")).toBe("早安");
  });
});
