import type { ThreadSummary } from "@evergenie/sdk-web";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { ThreadList } from "./ThreadList";

const state = vi.hoisted(() => ({
  threads: [] as unknown[],
  picked: [] as string[],
  calls: 0,
}));

vi.mock("../../lib/chatDisplay", () => ({ useToolDetails: () => false }));
vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    ...props
  }: {
    children: ReactNode;
    params?: { session?: string };
    [k: string]: unknown;
  }) => (
    <a
      href={`/chat/${props.params?.session ?? ""}`}
      aria-current={props["aria-current"] as "true" | undefined}
      data-thread={props["data-thread"] as string}
      data-thread-status={props["data-thread-status"] as string | undefined}
      data-work-task={props["data-work-task"] as string}
      className={props.className as string | undefined}
    >
      {children}
    </a>
  ),
}));
vi.mock("../../data/queries", () => ({
  threadsQuery: () => ({
    queryKey: ["threads"],
    queryFn: async () => {
      state.calls++;
      return state.threads;
    },
  }),
  // 完整模式：清單列照舊顯示單號（簡單模式不顯示，DEV-156 U-22）。
  useShowCodes: () => true,
}));
vi.mock("../../data/agentFilter", () => ({
  useAgentFilter: () => ({ picked: state.picked, clear: () => {} }),
  usePickedNames: () => "",
  matchAgent: (picked: string[], id?: string) => picked.length === 0 || picked.includes(id ?? ""),
}));
vi.mock("../../data/agents", () => ({
  useAgents: () => ({
    list: [],
    view: (id: string) => ({ id, name: id === "code" ? "Builder" : "Helper", duty: "" }),
  }),
}));

const thread = (name: string, agent: string): ThreadSummary =>
  ({ name, agent, status: "idle", unread: 0, title: name, last_preview: "" }) as ThreadSummary;

function mount(props: Parameters<typeof ThreadList>[0] = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ThreadList {...props} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.calls = 0;
  state.threads = [thread("code", "code"), thread("life", "life")];
  state.picked = [];
});
afterEach(() => cleanup());

describe("討論串清單：看板的秘書篩選", () => {
  // Guards: REQ-155
  test("TestThreadListBoardFilter：照看板的秘書篩選", async () => {
    state.picked = ["life"];
    mount();
    await screen.findByText("Helper");
    expect(document.querySelector('[data-thread="code"]')).toBeNull();
  });
});

describe("討論串清單：工作 session 收在秘書底下（P7-5、D-33）", () => {
  const workThread = (
    name: string,
    task: string,
    over: Partial<ThreadSummary> = {},
  ): ThreadSummary =>
    ({
      ...thread(name, "code"),
      kind: "work",
      task,
      title: `${task} 整理報表`,
      ...over,
    }) as ThreadSummary;

  // Guards: REQ-204
  test("TestThreadListNestsWorkSessions：單號、單的標題、排隊與 hot cache 倒數都在秘書底下", async () => {
    const ended = new Date(Date.now() - 20 * 60_000).toISOString();
    state.threads = [
      thread("code", "code"),
      thread("life", "life"),
      workThread("code-sec-7", "SEC-7", { last_turn_end_at: ended }),
      workThread("code-sec-9", "SEC-9", { status: "queued", waiting: ["work_queue"] }),
    ];
    mount();
    const list = await screen.findByRole("list", { name: "Builder 的工作 session" });
    expect(list.querySelectorAll("[data-work-task]").length).toBe(2);
    expect(list.querySelector('[data-work-task="SEC-7"]')?.textContent).toContain("整理報表");
    expect(
      list.querySelector('[data-work-task="SEC-7"] [data-testid="work-cache"]')?.textContent,
    ).toMatch(/快取剩 \d+ 分/);
    expect(
      list.querySelector('[data-work-task="SEC-9"] [data-testid="work-status"]')?.textContent,
    ).toBe("排隊等名額");
    // 工作 session 不另外佔頂層的一格。
    const topLevel = document.querySelector("ul")?.children;
    expect(topLevel?.length).toBe(2);
  });

  // Guards: REQ-204
  test("TestThreadListKeepsOrphanWorkSessions：她的主要討論串被篩掉時，工作 session 留在頂層", async () => {
    state.threads = [thread("life", "life"), workThread("code-sec-7", "SEC-7")];
    mount();
    await screen.findByText("Helper");
    expect(document.querySelector('[data-thread="code-sec-7"]')).not.toBeNull();
    expect(screen.queryByRole("list", { name: /的工作 session/ })).toBeNull();
  });
});
