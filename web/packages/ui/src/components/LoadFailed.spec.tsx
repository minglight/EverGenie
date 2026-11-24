import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { LoadFailed, LoadFailedGroup } from "./LoadFailed";

afterEach(cleanup);

describe("LoadFailed", () => {
  test("白話說什麼載不進來、給「再試一次」；會自己再試的才寫自動重試", () => {
    const retry = vi.fn();
    const { rerender } = render(<LoadFailed what="習慣" onRetry={retry} />);
    const box = screen.getByRole("alert");
    expect(box.textContent).toContain("習慣暫時載不進來");
    expect(box.textContent).not.toContain("自動重試");
    fireEvent.click(screen.getByRole("button", { name: "再試一次" }));
    expect(retry).toHaveBeenCalledTimes(1);
    rerender(<LoadFailed what="習慣" onRetry={retry} auto />);
    expect(screen.getByRole("alert").textContent).toContain("稍後會自動重試");
  });

  test("一行的版本（卡片裡）一樣有「再試一次」", () => {
    const retry = vi.fn();
    render(<LoadFailed what="今天的身體" onRetry={retry} variant="inline" />);
    expect(screen.getByRole("alert").textContent).toContain("今天的身體暫時載不進來");
    fireEvent.click(screen.getByRole("button", { name: "再試一次" }));
    expect(retry).toHaveBeenCalledTimes(1);
  });

  test("同一組裡好幾個同時失敗只出現一條，按「再試一次」每個都重試", () => {
    const a = vi.fn();
    const b = vi.fn();
    render(
      <LoadFailedGroup>
        <LoadFailed what="今天的習慣" onRetry={a} variant="inline" />
        <p>正常的卡片</p>
        <LoadFailed what="今天的身體" onRetry={b} variant="inline" />
      </LoadFailedGroup>,
    );
    const alerts = screen.getAllByRole("alert");
    expect(alerts).toHaveLength(1);
    expect(alerts[0]?.textContent).toContain("今天的習慣、今天的身體暫時載不進來");
    expect(screen.getByText("正常的卡片")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "再試一次" }));
    expect(a).toHaveBeenCalledTimes(1);
    expect(b).toHaveBeenCalledTimes(1);
  });

  test("一組裡沒有失敗的：不多畫任何提示；失敗的好了那一條就消失", () => {
    const { rerender } = render(
      <LoadFailedGroup>
        <LoadFailed what="今天的習慣" onRetry={() => {}} />
      </LoadFailedGroup>,
    );
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    rerender(
      <LoadFailedGroup>
        <p>好了</p>
      </LoadFailedGroup>,
    );
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
