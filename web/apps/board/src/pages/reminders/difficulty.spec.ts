import { describe, expect, test } from "vitest";
import { difficultyOf, isLate } from "./reminderText";

describe("difficultyOf", () => {
  test("沒寫或不認得的難度都當普通（WEB-162）", () => {
    expect(difficultyOf({})).toBe("normal");
    expect(difficultyOf({ difficulty: "easy" })).toBe("easy");
    expect(difficultyOf({ difficulty: "hard" })).toBe("hard");
    expect(difficultyOf({ difficulty: "extreme" })).toBe("normal");
  });
});

describe("isLate", () => {
  // DEV-156 U-17：時間過了還沒做完的，詳細頁寫「過期了」而不是「等你回覆」。
  test("時間過了、還沒做完也沒擱置才算過期；沒設時間、結束的、擱置的不算", () => {
    const now = new Date("2026-11-14T12:00:00+08:00");
    const past = "2026-11-13T09:00:00+08:00";
    const future = "2026-11-15T09:00:00+08:00";
    for (const s of ["todo", "doing", "asking"]) expect(isLate(s, past, now)).toBe(true);
    for (const s of ["done", "cancelled", "shelved"]) expect(isLate(s, past, now)).toBe(false);
    expect(isLate("asking", future, now)).toBe(false);
    expect(isLate("todo", undefined, now)).toBe(false);
    expect(isLate("todo", "", now)).toBe(false);
  });
});
