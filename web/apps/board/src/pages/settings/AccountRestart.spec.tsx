import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { AccountRestart } from "./AccountRestart";

afterEach(() => cleanup());

// 換帳號之後的「重開她的對話」（DEV-106）：先說清楚會中斷、會從頭開始，再讓人按。
describe("設定頁換帳號後的重開列（DEV-106）", () => {
  test("提示說明會中斷她正在做的事、她會從頭開始；按鈕呼叫 onRestart", () => {
    const onRestart = vi.fn();
    render(<AccountRestart phase="offer" name="阿媞米斯" onRestart={onRestart} />);
    const note = screen.getByRole("status").textContent ?? "";
    expect(note).toContain("阿媞米斯 開著的對話");
    expect(note).toContain("中斷正在做的事");
    expect(note).toContain("從頭開始");
    fireEvent.click(screen.getByRole("button", { name: "重開對話" }));
    expect(onRestart).toHaveBeenCalledTimes(1);
  });

  test("重開中：按鈕停用、寫「正在重開」", () => {
    render(<AccountRestart phase="working" name="阿媞米斯" onRestart={() => {}} />);
    const btn = screen.getByRole("button", { name: "正在重開…" }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  test("重開完：寫「已重開」、不再有按鈕", () => {
    render(<AccountRestart phase="done" name="阿媞米斯" onRestart={() => {}} />);
    expect(screen.getByRole("status").textContent).toContain("已重開 阿媞米斯 的對話");
    expect(screen.queryByRole("button")).toBeNull();
  });
});
