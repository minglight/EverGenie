import { describe, expect, test } from "vitest";
import { stateBadge } from "./PackageRow";

describe("套件狀態標籤", () => {
  test("開關開著但程式停著：寫「開著但沒在跑」、提醒色，不跟開著的開關矛盾", () => {
    expect(stateBadge({ state: "stopped", enabled: true })).toEqual({
      text: "settings.packages.state.stoppedOn",
      tone: "attention",
    });
  });

  test("停用的照舊寫「停止」；其他狀態照原樣；沒有狀態不畫", () => {
    expect(stateBadge({ state: "stopped", enabled: false })).toEqual({
      text: "settings.packages.state.stopped",
      tone: "muted",
    });
    expect(stateBadge({ state: "running", enabled: true })?.text).toBe(
      "settings.packages.state.running",
    );
    expect(stateBadge({ state: "failed", enabled: true })?.tone).toBe("danger");
    expect(stateBadge({ enabled: true })).toBeNull();
  });
});
