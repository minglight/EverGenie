import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { Segmented } from "./Segmented";

afterEach(cleanup);

describe("Segmented", () => {
  const options = [
    { value: "all", label: "全部 3" },
    { value: "approve", label: "同意 2" },
  ] as const;

  // Guards: REQ-115
  test("目前選的那顆標成 pressed，點另一顆會回報它的值", () => {
    const onChange = vi.fn();
    render(<Segmented options={[...options]} value="all" onChange={onChange} ariaLabel="篩選" />);
    expect(screen.getByRole("button", { name: "全部 3" }).getAttribute("aria-pressed")).toBe(
      "true",
    );
    expect(screen.getByRole("button", { name: "同意 2" }).getAttribute("aria-pressed")).toBe(
      "false",
    );
    fireEvent.click(screen.getByRole("button", { name: "同意 2" }));
    expect(onChange).toHaveBeenCalledWith("approve");
  });

  // Guards: REQ-115
  test("群組有可見名稱給讀屏軟體", () => {
    render(
      <Segmented
        options={[...options]}
        value="all"
        onChange={() => {}}
        ariaLabel="篩選等你處理的事"
      />,
    );
    expect(screen.getByRole("group", { name: "篩選等你處理的事" })).toBeTruthy();
  });

  // Guards: REQ-185
  test("按鈕高 44（觸控目標），緊湊 30", () => {
    const height = (props: { compact?: boolean }) => {
      const { unmount } = render(
        <Segmented
          options={[...options]}
          value="all"
          onChange={() => {}}
          ariaLabel="篩選"
          {...props}
        />,
      );
      const cls = screen.getByRole("button", { name: "全部 3" }).className;
      unmount();
      return cls;
    };
    expect(height({})).toContain("h-11");
    expect(height({ compact: true })).toContain("h-[30px]");
  });

  // DEV-156 U-26：數量 0 不用半透明（對比不到 4.5:1），用正常的灰字、不加粗。
  test("數量 0 用灰字、不用半透明", () => {
    render(
      <Segmented
        options={[
          { value: "a", label: "等你", count: 0 },
          { value: "b", label: "全部", count: 3 },
        ]}
        value="b"
        onChange={() => {}}
        ariaLabel="篩選"
      />,
    );
    const zero = screen.getByRole("button", { name: "等你 0" }).querySelector("span");
    expect(zero?.className).toContain("text-muted");
    expect(zero?.className).not.toContain("opacity");
  });
});
