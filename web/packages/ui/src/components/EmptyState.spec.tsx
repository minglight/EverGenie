import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, test } from "vitest";
import { EmptyState } from "./EmptyState";

afterEach(cleanup);

describe("EmptyState", () => {
  // DEV-156 U-28：找不到、讀不到這類不是好消息的空狀態不用綠色打勾。
  test("預設是綠勾；tone=missing 改灰色問號", () => {
    const { container } = render(<EmptyState title="沒有待辦" />);
    expect(container.querySelector("svg")?.getAttribute("class")).toContain("text-ok");
    expect(screen.queryByTestId("empty-icon-missing")).toBeNull();
    cleanup();
    render(<EmptyState tone="missing" title="找不到這套衣服" />);
    const icon = screen.getByTestId("empty-icon-missing");
    expect(icon.getAttribute("class")).toContain("text-muted");
    expect(icon.getAttribute("class")).not.toContain("text-ok");
  });
});
