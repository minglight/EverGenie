import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, test, vi } from "vitest";
import type { FlatItem } from "./flatten";
import { MobileBar } from "./MobileBar";

vi.mock("./NavLink", () => ({
  NavLink: ({ route, children, ...rest }: { route: string; children: ReactNode }) => (
    <a href={route} {...rest}>
      {children}
    </a>
  ),
}));

afterEach(() => cleanup());

const flat = (id: string, group?: string): FlatItem =>
  ({
    id,
    title: id,
    route: `/${id}`,
    routes: [`/${id}`],
    badge: 0,
    group: group ? { id: group, title: group } : undefined,
  }) as unknown as FlatItem;

describe("MobileBar 更多", () => {
  test("群組後面接的頂層項目（設定）有分隔線，不會看起來像在「我的套件」群組裡", () => {
    const items = [
      flat("a"),
      flat("b"),
      flat("c"),
      flat("d"),
      flat("e"),
      flat("mine1", "我的套件"),
      flat("mine2", "我的套件"),
      flat("settings"),
    ];
    render(<MobileBar items={items} pathname="/" />);
    fireEvent.click(screen.getByRole("button", { name: /更多/ }));
    const sections = screen.getAllByTestId("more-section");
    expect(sections).toHaveLength(2);
    expect(within(sections[0] as HTMLElement).getByText("我的套件")).toBeTruthy();
    expect(sections[0]?.getAttribute("data-separated")).toBeNull();
    expect(within(sections[1] as HTMLElement).getByText("settings")).toBeTruthy();
    expect(sections[1]?.getAttribute("data-separated")).toBe("true");
  });

  test("最上面那段沒有標題時不畫分隔線", () => {
    const items = ["a", "b", "c", "d", "e", "f", "g"].map((id) => flat(id));
    render(<MobileBar items={items} pathname="/" />);
    fireEvent.click(screen.getByRole("button", { name: /更多/ }));
    const sections = screen.getAllByTestId("more-section");
    expect(sections).toHaveLength(1);
    expect(sections[0]?.getAttribute("data-separated")).toBeNull();
  });
});
