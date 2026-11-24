// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { focusFirstInvalid } from "./focusInvalid";

describe("focusFirstInvalid", () => {
  // GOL-14：新增 Goal 必填沒填就按「建立」，焦點跳到第一個有錯的欄位（手機上錯誤可能在捲出去的地方）。
  it("moves focus to the first field marked invalid", () => {
    const form = document.createElement("form");
    form.innerHTML =
      '<input id="title" aria-invalid="true"><textarea id="text"></textarea><textarea id="criteria" aria-invalid="true"></textarea>';
    document.body.append(form);
    (form.querySelector("#text") as HTMLElement).focus();
    expect(focusFirstInvalid(form)).toBe(true);
    expect(document.activeElement?.id).toBe("title");
    form.remove();
  });

  it("does nothing when every field is fine", () => {
    const form = document.createElement("form");
    form.innerHTML = '<input id="title">';
    expect(focusFirstInvalid(form)).toBe(false);
    expect(focusFirstInvalid(null)).toBe(false);
  });
});
