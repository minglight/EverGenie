import { describe, expect, test } from "vitest";
import { hotkeyIndex } from "./agentHotkeys";

const key = (
  code: string,
  mods: Partial<Record<"alt" | "meta" | "ctrl" | "shift", boolean>> = {},
) => ({
  code,
  altKey: mods.alt ?? true,
  metaKey: mods.meta ?? false,
  ctrlKey: mods.ctrl ?? false,
  shiftKey: mods.shift ?? false,
});

describe("聊天快速換秘書的快捷鍵", () => {
  // Guards: REQ-200
  test("⌥1–⌥9 對到第 1–9 位；看實體鍵（code），Mac 的 ⌥1 打出來的字不是 1", () => {
    expect(hotkeyIndex(key("Digit1"))).toBe(0);
    expect(hotkeyIndex(key("Digit9"))).toBe(8);
  });

  // Guards: REQ-200
  test("沒按 ⌥、多按了 ⌘／Ctrl／Shift、⌥0、其他鍵都不算", () => {
    expect(hotkeyIndex(key("Digit1", { alt: false }))).toBeNull();
    expect(hotkeyIndex(key("Digit1", { meta: true }))).toBeNull();
    expect(hotkeyIndex(key("Digit1", { ctrl: true }))).toBeNull();
    expect(hotkeyIndex(key("Digit1", { shift: true }))).toBeNull();
    expect(hotkeyIndex(key("Digit0"))).toBeNull();
    expect(hotkeyIndex(key("KeyA"))).toBeNull();
  });
});
