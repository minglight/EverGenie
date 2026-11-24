import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { NudgeToasts } from "../shell/NudgeToasts";
import { NudgeCard } from "./NudgeCard";

const state = vi.hoisted(() => ({
  nudges: [] as Record<string, unknown>[],
  resolve: vi.fn(async (_id: string, _site?: string) => {}),
  viewed: "",
}));
vi.mock("../data/sites", async () => ({
  ...(await vi.importActual<typeof import("../data/sites")>("../data/sites")),
  useViewedHome: () => state.viewed,
}));
vi.mock("../data/viewing", async () => {
  const actual = await vi.importActual<typeof import("../data/viewing")>("../data/viewing");
  return { ...actual, useNudges: () => state.nudges, resolveNudge: state.resolve };
});
vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    search,
    children,
  }: {
    to: string;
    search?: Record<string, string>;
    children: ReactNode;
  }) => {
    const q = new URLSearchParams(search ?? {}).toString();
    return <a href={q ? `${to}?${q}` : to}>{children}</a>;
  },
}));

const n = (over: Record<string, unknown> = {}) => ({
  id: "nudge-1",
  session: "code-sec-12",
  task: "SEC-12",
  title: "SEC-12 還在等你",
  context: "把通知做安靜",
  todo: "請確認要不要上線",
  url: "/chat/code-sec-12",
  surface: "inline",
  resolved: false,
  site: "",
  ...over,
});

afterEach(() => {
  cleanup();
  state.nudges = [];
  state.viewed = "";
  state.resolve.mockClear();
});

describe("提醒的頁內卡片與站內提示（P7-6、D-34）", () => {
  // Guards: REQ-228
  test("頁內卡片只顯示這個討論串（或這張單）的提醒，按「知道了」收掉", () => {
    state.nudges = [n(), n({ id: "nudge-2", session: "code-sec-99", task: "SEC-99" })];
    render(<NudgeCard session="code-sec-12" />);
    const cards = screen.getAllByTestId("nudge-card");
    expect(cards).toHaveLength(1);
    expect(cards[0]?.textContent).toContain("SEC-12 還在等你");
    expect(cards[0]?.textContent).toContain("請確認要不要上線");
    expect(cards[0]?.textContent).toContain("2 分鐘沒回覆，會改推到手機");
    fireEvent.click(screen.getByRole("button", { name: "知道了" }));
    expect(state.resolve).toHaveBeenCalledWith("nudge-1", "");
  });

  // Guards: REQ-373
  test("綁在對話上的等待（帶 key）不在對話裡再畫一張卡（對話裡已經有確認卡）", () => {
    state.nudges = [n({ id: "nudge-3", task: "", key: "pending:P1", title: "Athena 要你確認" })];
    render(<NudgeCard session="code-sec-12" />);
    expect(screen.queryAllByTestId("nudge-card")).toHaveLength(0);
  });

  // Guards: REQ-228
  test("任務單頁用單號對得上", () => {
    state.nudges = [n()];
    render(<NudgeCard task="SEC-12" />);
    expect(screen.getAllByTestId("nudge-card")).toHaveLength(1);
    cleanup();
    render(<NudgeCard task="SEC-13" />);
    expect(screen.queryAllByTestId("nudge-card")).toHaveLength(0);
  });

  // Guards: REQ-228
  test("已經收掉的不顯示", () => {
    state.nudges = [n({ resolved: true })];
    render(<NudgeCard session="code-sec-12" />);
    expect(screen.queryAllByTestId("nudge-card")).toHaveLength(0);
  });

  // Guards: REQ-228
  test("站內提示：在那一頁就不重複（那裡是卡片），在別頁顯示並可以去看看或收掉", () => {
    state.nudges = [n({ surface: "toast" })];
    const { unmount } = render(<NudgeToasts pathname="/chat/code-sec-12" />);
    expect(screen.queryAllByTestId("nudge-toast")).toHaveLength(0);
    unmount();
    render(<NudgeToasts pathname="/reminders" />);
    expect(screen.getAllByTestId("nudge-toast")).toHaveLength(1);
    expect(screen.getByRole("link", { name: "去看看" }).getAttribute("href")).toBe(
      "/chat/code-sec-12",
    );
    fireEvent.click(screen.getByRole("button", { name: "知道了" }));
    expect(state.resolve).toHaveBeenCalledWith("nudge-1", "");
  });
});

describe("多台的提醒（S3-8）", () => {
  // 兩台都有 code-sec-12：頁內卡片只畫這一頁那台的，別台同名的不算。
  test("頁內卡片只畫這張單／討論串所屬那台的提醒", () => {
    state.nudges = [n({ id: "a" }), n({ id: "b", site: "amy-office" })];
    render(<NudgeCard session="code-sec-12" />);
    expect(screen.getAllByTestId("nudge-card")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "知道了" }));
    expect(state.resolve).toHaveBeenCalledWith("a", "");
    cleanup();
    state.resolve.mockClear();
    render(<NudgeCard session="code-sec-12" site="amy-office" />);
    expect(screen.getAllByTestId("nudge-card")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "知道了" }));
    expect(state.resolve).toHaveBeenCalledWith("b", "amy-office");
  });

  // 頁面沒傳 site 時照網址的 ?home=（跟外殼報在場、站內提示藏不藏同一個來源）：
  // 開 /chat/code-sec-12?home=amy-office 時站內提示藏起來了，卡片要畫那台的，不能兩邊都看不到。
  test("頁內卡片沒給 site：照網址的 ?home= 那台", () => {
    state.nudges = [n({ id: "a" }), n({ id: "b", site: "amy-office" })];
    state.viewed = "amy-office";
    render(<NudgeCard session="code-sec-12" />);
    expect(screen.getAllByTestId("nudge-card")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "知道了" }));
    expect(state.resolve).toHaveBeenCalledWith("b", "amy-office");
  });

  // 別台的提醒：「去看看」帶 ?home=<那台>；在主伺服器同名的討論串頁不算「在那一頁」，照樣顯示提示。
  test("站內提示：別台的「去看看」帶 home，同名的別台頁面不算在那一頁", () => {
    state.nudges = [n({ surface: "toast", site: "amy-office" })];
    const { unmount } = render(<NudgeToasts pathname="/chat/code-sec-12" />);
    expect(screen.getAllByTestId("nudge-toast")).toHaveLength(1);
    expect(screen.getByRole("link", { name: "去看看" }).getAttribute("href")).toBe(
      "/chat/code-sec-12?home=amy-office",
    );
    unmount();
    render(<NudgeToasts pathname="/chat/code-sec-12" viewed="amy-office" />);
    expect(screen.queryAllByTestId("nudge-toast")).toHaveLength(0);
  });
});
