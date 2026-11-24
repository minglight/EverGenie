import type { Task } from "@evergenie/sdk-web";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, test, vi } from "vitest";
import type { SiteRef } from "../../data/sites";
import { TaskBanner } from "./TaskBanner";

const state = vi.hoisted(() => ({ task: null as unknown }));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, ...props }: { children: ReactNode; [k: string]: unknown }) => (
    <a
      href="#task"
      data-testid={props["data-testid"] as string}
      data-search={JSON.stringify(props.search ?? {})}
    >
      {children}
    </a>
  ),
}));
vi.mock("../../data/queries", () => ({
  taskQuery: (id: string) => ({ queryKey: ["task", id], queryFn: async () => state.task }),
  // 完整模式：清單列照舊顯示單號（簡單模式不顯示，DEV-156 U-22）。
  useShowCodes: () => true,
}));

function mount(id: string, site?: SiteRef) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TaskBanner id={id} site={site} />
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe("工作對話頂端的任務列", () => {
  // Guards: REQ-286
  test("TestTaskBannerShowsStatusAndAsk：單號、標題、狀態，要你回答時帶秘書的提問", async () => {
    state.task = {
      id: "SEC-318",
      title: "大改架構",
      status: "waiting_user",
      ask: "要先做哪一件？",
    } as Task;
    mount("SEC-318");
    expect((await screen.findByTestId("task-banner-status")).textContent).toBe("要你回答");
    expect(screen.getByTestId("task-banner").textContent).toContain("SEC-318");
    expect(screen.getByTestId("task-banner").textContent).toContain("大改架構");
    expect(screen.getByTestId("task-banner").textContent).toContain("要先做哪一件？");
  });

  // Guards: REQ-286
  test("TestTaskBannerReviewShowsResult：要你驗收時帶結果", async () => {
    state.task = {
      id: "SEC-1",
      title: "修好",
      status: "waiting_user",
      proposed: "done",
      ask: "請驗收",
      result: "已修",
    } as Task;
    mount("SEC-1");
    expect((await screen.findByTestId("task-banner-status")).textContent).toBe("要你驗收");
    expect(screen.getByTestId("task-banner").textContent).toContain("已修");
  });

  test("TestTaskBannerOtherSiteLinksWithHome：別台的單也連，網址帶 ?home=那台（主伺服器不帶）", async () => {
    state.task = { id: "SEC-2", title: "別台的單", status: "doing" } as Task;
    mount("SEC-2", { key: "amy-office", base: "https://office.example" });
    const banner = await screen.findByTestId("task-banner");
    expect(banner.tagName).toBe("A");
    expect(banner.getAttribute("data-search")).toBe(JSON.stringify({ home: "amy-office" }));
    cleanup();
    mount("SEC-2");
    expect((await screen.findByTestId("task-banner")).getAttribute("data-search")).toBe("{}");
  });

  // Guards: REQ-286
  test("TestTaskBannerMissingTask：查不到任務單就不顯示", async () => {
    state.task = null;
    mount("SEC-9");
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByTestId("task-banner")).toBeNull();
  });
});
