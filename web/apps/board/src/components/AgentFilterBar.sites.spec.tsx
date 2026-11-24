import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import type { FilterSite } from "../data/filterSites";
import type { Site } from "../data/sites";
import { AgentFilterBar } from "./AgentFilterBar";

// 手機的秘書小卡列連多台：兩台同一個職位合成一張（作者 2026-11-02），件數加總；別台不能用時小卡後面一格灰字。
const state = vi.hoisted(() => ({
  sections: [] as FilterSite[],
  counts: null as { counts: Record<string, number>; total: number } | null,
}));

vi.mock("../data/filterSites", async (actual) => ({
  ...(await actual<typeof import("../data/filterSites")>()),
  useFilterSites: () => state.sections,
}));
vi.mock("../data/agentFilter", () => ({
  useAgentFilter: () => ({ picked: [], isOn: () => false, toggle: vi.fn(), clear: vi.fn() }),
}));
vi.mock("../data/pageCounts", () => ({ usePageCounts: () => state.counts }));
vi.mock("../lib/media", () => ({ useIsWide: () => false }));

const site = (over: Partial<Site>): Site => ({
  key: "",
  base: "",
  primary: true,
  label: "amy-mini",
  machine: "mini",
  env: "prod",
  version: "1",
  boardApi: 1,
  publicUrl: "",
  color: "",
  state: "ok",
  reason: "",
  ...over,
});
const mini = site({});
const office = site({
  key: "amy-office",
  base: "https://office.example",
  primary: false,
  label: "amy-office",
  machine: "office",
});
const agent = (siteKey: string, id: string, name: string) => ({
  key: siteKey ? `${siteKey}/${id}` : id,
  id,
  name,
  duty: "",
  presence: "idle" as const,
});

beforeEach(() => {
  state.counts = null;
});
afterEach(cleanup);

describe("AgentFilterBar 多台", () => {
  test("只有一台：不分段、小卡的 key 就是職位 id", () => {
    state.sections = [{ site: mini, agents: [agent("", "code", "Builder")] }];
    const { container } = render(<AgentFilterBar />);
    expect(screen.queryAllByTestId("filter-site")).toHaveLength(0);
    expect(screen.queryAllByTestId("site-tag")).toHaveLength(0);
    expect(container.querySelector('[data-agent="code"]')?.getAttribute("aria-label")).toBe(
      "Builder",
    );
  });

  test("兩台：同一個職位合成一張小卡，名字寫兩台、件數加總；只有一台有的照樣一張", () => {
    state.sections = [
      { site: mini, agents: [agent("", "code", "Builder"), agent("", "life", "Helper")] },
      {
        site: office,
        agents: [agent("amy-office", "code", "Builder"), agent("amy-office", "work", "Worker")],
      },
    ];
    state.counts = { counts: { code: 1, "amy-office/code": 4, "amy-office/work": 2 }, total: 7 };
    render(<AgentFilterBar />);
    expect(screen.queryAllByTestId("filter-site")).toHaveLength(0);
    const chip = screen.getByRole("button", { name: "Builder，在 amy-mini、amy-office" });
    expect(chip.dataset.agent).toBe("code");
    expect(chip.dataset.agents).toBe("code amy-office/code");
    expect(chip.dataset.count).toBe("5");
    const work = screen.getByRole("button", { name: "Worker，在 amy-office" });
    expect(work.dataset.agent).toBe("amy-office/work");
    expect(work.dataset.count).toBe("2");
    // 「全部」＋三位。
    expect(screen.getAllByRole("button")).toHaveLength(4);
  });

  // DEV-156 U-23：秘書列只放一格小標記（不放一句會被切掉的長字）；整句話在頁面內文講一次，這裡收在 title 與讀屏名稱。
  test("別台連不到：小卡後面一格小標記，這台照常", () => {
    state.sections = [
      { site: mini, agents: [agent("", "code", "Builder")] },
      { site: { ...office, state: "down" }, agents: [] },
    ];
    render(<AgentFilterBar />);
    const notice = screen.getByTestId("filter-site-notice");
    expect(notice.dataset.site).toBe("amy-office");
    expect(notice.textContent).toBe("有一台連不上");
    expect(notice.getAttribute("aria-label")).toContain("現在連不到");
    expect(notice.getAttribute("title")).toContain("現在連不到");
    expect(screen.queryByTestId("site-notice")).toBeNull();
    expect(screen.getByRole("button", { name: "Builder，在 amy-mini" })).toBeTruthy();
    const bar = screen.getByRole("region", { name: "秘書篩選" });
    expect(bar.className).toContain("h-[88.5px]");
  });
});
