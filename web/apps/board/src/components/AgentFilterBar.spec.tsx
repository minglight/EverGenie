import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { AgentFilterBar } from "./AgentFilterBar";

const state = vi.hoisted(() => ({
  roster: [] as { id: string; name: string; duty: string; enabled: boolean }[],
  counts: null as { counts: Record<string, number>; total: number } | null,
}));

vi.mock("../data/agents", async (actual) => ({
  ...(await actual<typeof import("../data/agents")>()),
  useAgents: () => ({ list: state.roster, view: () => ({}) }),
  useSiteAgents: () => [{ site: { key: "" }, list: state.roster, view: () => ({}) }],
  presenceOf: () => ({ presence: "idle" }),
}));
vi.mock("../data/pageCounts", () => ({ usePageCounts: () => state.counts }));
vi.mock("../data/queries", () => ({
  threadsQuery: () => ({ queryKey: ["threads"], queryFn: async () => [] }),
}));

function mount() {
  const client = new QueryClient();
  return render(
    <QueryClientProvider client={client}>
      <AgentFilterBar />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
  state.roster = [
    { id: "code", name: "Builder", duty: "寫程式", enabled: true },
    { id: "life", name: "Helper", duty: "生活", enabled: true },
  ];
  state.counts = null;
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("AgentFilterBar", () => {
  // Guards: REQ-155
  test("件數還沒到：沒有徽章，「全部」下面顯示「— 件」", () => {
    const { container } = mount();
    expect(screen.getByText("— 件")).not.toBeNull();
    expect(container.querySelectorAll("[data-count]").length).toBe(2);
    for (const chip of container.querySelectorAll("[data-count]")) {
      expect(chip.getAttribute("data-count")).toBe("0");
      expect(chip.textContent).not.toMatch(/\d/);
    }
  });

  // Guards: REQ-155
  test("件數到了：徽章與總數照 usePageCounts", () => {
    state.counts = { counts: { code: 3 }, total: 5 };
    const { container } = mount();
    expect(screen.getByText("5 件")).not.toBeNull();
    expect(container.querySelector('[data-count="3"]')?.textContent).toContain("3");
  });

  // Guards: REQ-155
  test("名冊還沒到：外框還在、固定高度、畫 5 個骨架加「全部」鈕", () => {
    state.roster = [];
    const { container } = mount();
    const bar = screen.getByRole("region", { name: "秘書篩選" });
    expect(bar.className).toContain("h-[88.5px]");
    expect(bar.className).toContain("box-border");
    expect(container.querySelectorAll('[aria-hidden="true"] > .bg-surface-2').length).toBe(5);
    expect(screen.getByRole("button", { name: /^全部/ })).not.toBeNull();
  });

  // Guards: REQ-155
  test("名冊到了：外框高度跟名冊沒到時一樣", () => {
    state.roster = [];
    const first = mount();
    const before = screen.getByRole("region", { name: "秘書篩選" }).className;
    first.unmount();
    state.roster = [{ id: "code", name: "Builder", duty: "", enabled: true }];
    mount();
    expect(screen.getByRole("region", { name: "秘書篩選" }).className).toBe(before);
  });
});
