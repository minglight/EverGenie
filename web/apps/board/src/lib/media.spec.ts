import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, test } from "vitest";
import { useVisibleHeightVar, useVisibleTop, VISIBLE_HEIGHT_VAR } from "./media";

// 假的 visualViewport：jsdom 沒有，測試自己放一個能發 resize／scroll 的。
class FakeViewport extends EventTarget {
  height = 844;
  offsetTop = 0;
}

function install(): FakeViewport {
  const vv = new FakeViewport();
  Object.defineProperty(window, "visualViewport", { value: vv, configurable: true });
  return vv;
}

afterEach(() => {
  cleanup();
  Object.defineProperty(window, "visualViewport", { value: undefined, configurable: true });
  document.documentElement.style.removeProperty(VISIBLE_HEIGHT_VAR);
});

describe("鍵盤打開時看得到的那塊（WEB-128、D-59）", () => {
  // dvh 在 iOS 不扣鍵盤：「最高 55% 畫面」的上限要跟著 visualViewport 變矮，卡片和輸入框才都在鍵盤上面。
  test("看得到的高度寫進 --hg-visible-h，鍵盤打開就變矮，離開外殼就拿掉", () => {
    const vv = install();
    const { unmount } = renderHook(() => useVisibleHeightVar());
    const root = document.documentElement.style;
    expect(root.getPropertyValue(VISIBLE_HEIGHT_VAR)).toBe("844px");
    act(() => {
      vv.height = 470;
      vv.dispatchEvent(new Event("resize"));
    });
    expect(root.getPropertyValue(VISIBLE_HEIGHT_VAR)).toBe("470px");
    unmount();
    expect(root.getPropertyValue(VISIBLE_HEIGHT_VAR)).toBe("");
  });

  // iOS 把畫面往上推只發 scroll：貼頂的浮層要跟著 offsetTop 走。
  test("畫面被往上推（只發 scroll）時 offsetTop 跟著更新", () => {
    const vv = install();
    const { result } = renderHook(() => useVisibleTop());
    expect(result.current).toBe(0);
    act(() => {
      vv.offsetTop = 280;
      vv.dispatchEvent(new Event("scroll"));
    });
    expect(result.current).toBe(280);
  });
});
