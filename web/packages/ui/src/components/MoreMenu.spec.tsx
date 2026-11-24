import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { MoreMenu } from "./MoreMenu";

afterEach(cleanup);

// 「⋯」選單（DEV-156 U-15、U-19）：手機（測試環境沒有 matchMedia＝手機）是底部抽屜。
describe("MoreMenu", () => {
  test("鈕的讀屏名字寫明裡面有什麼；抽屜標題用 title，選了先關再做", async () => {
    const onSelect = vi.fn();
    render(
      <MoreMenu
        label="更多：新增角色、匯入"
        title="角色"
        testId="menu"
        items={[
          { key: "new", label: "新增角色", onSelect, testId: "menu-new" },
          { key: "import", label: "匯入", href: "/personas?import=1", testId: "menu-import" },
        ]}
      />,
    );
    const trigger = screen.getByTestId("menu");
    expect(trigger.getAttribute("aria-label")).toBe("更多：新增角色、匯入");
    fireEvent.click(trigger);
    const sheet = await screen.findByRole("dialog");
    expect(sheet.textContent).toContain("角色");
    expect(screen.getByTestId("menu-import").getAttribute("href")).toBe("/personas?import=1");
    fireEvent.click(screen.getByTestId("menu-new"));
    expect(onSelect).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByTestId("menu-new")).toBeNull());
  });

  test("沒有項目就不畫鈕", () => {
    render(<MoreMenu label="更多" items={[]} testId="menu" />);
    expect(screen.queryByTestId("menu")).toBeNull();
  });
});
