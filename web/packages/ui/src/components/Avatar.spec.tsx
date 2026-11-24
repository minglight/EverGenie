import { cleanup, fireEvent, render } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { Avatar, initialsOf } from "./Avatar";

afterEach(cleanup);

const img = (container: HTMLElement) => container.querySelector("img") as HTMLImageElement;

describe("Avatar", () => {
  // Guards: REQ-155
  test("Avatar remembers loaded src", () => {
    const first = render(<Avatar name="Builder" src="/avatars/remember.png" />);
    expect(img(first.container).className).toContain("opacity-0");
    fireEvent.load(img(first.container));
    expect(img(first.container).className).toContain("opacity-100");
    first.unmount();

    const second = render(<Avatar name="Builder" src="/avatars/remember.png" />);
    expect(img(second.container).className).toContain("opacity-100");
    expect(img(second.container).className).not.toContain("opacity-0");
  });

  // Guards: REQ-155
  // 載入失敗：拿掉圖、退回名字；重新掛上不再抓同一張（不一直打 404，DEV-157）。
  test("Avatar forgets src after error", () => {
    const first = render(<Avatar name="Builder" src="/avatars/forget.png" />);
    fireEvent.load(img(first.container));
    fireEvent.error(img(first.container));
    expect(first.container.querySelector("img")).toBeNull();
    expect(first.container.textContent).toBe("B");
    first.unmount();

    const second = render(<Avatar name="Builder" src="/avatars/forget.png" />);
    expect(second.container.querySelector("img")).toBeNull();
    expect(second.container.textContent).toBe("B");
  });

  // 失敗的記憶只留 5 分鐘：之後重新掛上會再試一次（圖補上了就看得到，DEV-157）。
  test("Avatar retries a failed src after five minutes", () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(new Date("2026-11-15T09:00:00"));
      const first = render(<Avatar name="Builder" src="/avatars/retry.png" />);
      fireEvent.error(img(first.container));
      first.unmount();
      vi.setSystemTime(new Date("2026-11-15T09:04:59"));
      const early = render(<Avatar name="Builder" src="/avatars/retry.png" />);
      expect(early.container.querySelector("img")).toBeNull();
      early.unmount();
      vi.setSystemTime(new Date("2026-11-15T09:05:01"));
      const later = render(<Avatar name="Builder" src="/avatars/retry.png" />);
      expect(later.container.querySelector("img")).not.toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  // 圖載入好就不畫字：透明背景的圖不會透出字（DEV-157）。
  test("Avatar hides the initials once the image loads", () => {
    const view = render(<Avatar name="阿基里斯" src="/avatars/hide-initial.png" size={56} />);
    expect(view.container.textContent).toBe("阿基");
    fireEvent.load(img(view.container));
    expect(view.container.textContent).toBe("");
  });

  // 沒有圖：名字的前兩個字，同一個字開頭的角色分得出來；太小的頭像只放一個字（DEV-157）。
  test("initialsOf", () => {
    expect(initialsOf("阿基里斯")).toBe("阿基");
    expect(initialsOf("阿媞米斯")).toBe("阿媞");
    expect(initialsOf("輝夜")).toBe("輝夜");
    expect(initialsOf("かぐや")).toBe("かぐ");
    expect(initialsOf("Mary Jane")).toBe("MJ");
    expect(initialsOf("builder")).toBe("B");
    expect(initialsOf("阿基里斯", 26)).toBe("阿");
    expect(initialsOf("  ")).toBe("?");
    const view = render(<Avatar name="阿媞米斯" size={56} />);
    expect(view.container.textContent).toBe("阿媞");
  });

  // Guards: REQ-155
  test("Avatar resets when src changes to an unseen image", () => {
    const view = render(<Avatar name="Builder" src="/avatars/a-seen.png" />);
    fireEvent.load(img(view.container));
    view.rerender(<Avatar name="Builder" src="/avatars/b-unseen.png" />);
    expect(img(view.container).className).toContain("opacity-0");
  });
});
