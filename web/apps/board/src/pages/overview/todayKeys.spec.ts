import { describe, expect, it } from "vitest";
import { todayKeyAction, todayPaneKeyAction } from "./todayKeys";

describe("todayKeyAction", () => {
  it("maps J/K/A/R and arrows", () => {
    expect(todayKeyAction({ key: "j" })).toBe("down");
    expect(todayKeyAction({ key: "ArrowDown" })).toBe("down");
    expect(todayKeyAction({ key: "k" })).toBe("up");
    expect(todayKeyAction({ key: "ArrowUp" })).toBe("up");
    expect(todayKeyAction({ key: "a" })).toBe("primary");
    expect(todayKeyAction({ key: "A" })).toBe("primary");
    expect(todayKeyAction({ key: "r" })).toBe("secondary");
    expect(todayKeyAction({ key: "x" })).toBeNull();
  });

  // WEB-123：按住 A／R 不放的連發不能一路核准／拒絕下一件。
  it("ignores auto-repeat of A and R so holding the key acts on one item only", () => {
    expect(todayKeyAction({ key: "a", repeat: true })).toBeNull();
    expect(todayKeyAction({ key: "R", repeat: true })).toBeNull();
    expect(todayKeyAction({ key: "j", repeat: true })).toBe("down");
  });

  // WEB-129：用滑鼠在右欄按過東西之後，焦點不在清單也要能用字母鍵；在輸入框裡打字不攔，↑↓ 留給右欄捲動。
  it("pane keys work outside the list except while typing, and leave arrows alone", () => {
    const button = document.createElement("button");
    const input = document.createElement("input");
    const area = document.createElement("textarea");
    const editable = document.createElement("div");
    editable.setAttribute("contenteditable", "true");
    const inner = document.createElement("span");
    editable.append(inner);
    expect(todayPaneKeyAction({ key: "a", target: button })).toBe("primary");
    expect(todayPaneKeyAction({ key: "j", target: document.body })).toBe("down");
    expect(todayPaneKeyAction({ key: "a", target: input })).toBeNull();
    expect(todayPaneKeyAction({ key: "r", target: area })).toBeNull();
    expect(todayPaneKeyAction({ key: "a", target: inner })).toBeNull();
    expect(todayPaneKeyAction({ key: "ArrowDown", target: button })).toBeNull();
    expect(todayPaneKeyAction({ key: "a", repeat: true, target: button })).toBeNull();
  });

  it("leaves modifier combos (⌘K, Ctrl+R) to the browser and the palette", () => {
    expect(todayKeyAction({ key: "k", metaKey: true })).toBeNull();
    expect(todayKeyAction({ key: "r", ctrlKey: true })).toBeNull();
    expect(todayKeyAction({ key: "a", altKey: true })).toBeNull();
  });
});
