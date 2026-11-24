import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { NightQueue } from "./NightQueue";

const state = vi.hoisted(() => ({
  record: null as null | { body: string; fields: Record<string, unknown>; revision: string },
  get: vi.fn(),
  put: vi.fn(),
}));

vi.mock("@evergenie/sdk-web", () => {
  class ApiError extends Error {
    readonly code: string;
    constructor(code: string, message: string) {
      super(message);
      this.code = code;
    }
  }
  return { ApiError, api: { vault: { get: state.get, put: state.put } } };
});

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children: React.ReactNode; to: string }) => (
    <a href={to}>{children}</a>
  ),
}));

const shown = `# 夜班清單

## 待做
- [ ] 整理 A｜加入 2026-10-20
- [ ] 補測試 B｜加入 2026-10-21

## 要主人

## 做完（留最近 14 天）
`;

// 別處（CLI 或另一個分頁）剛在最前面加了一項：畫面上的「第 2 項」在新版是別的事。
const newer = shown.replace("## 待做\n", "## 待做\n- [ ] 新加進來的 C｜加入 2026-10-26\n");

function renderQueue() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <NightQueue />
    </QueryClientProvider>,
  );
}

beforeEach(async () => {
  const { ApiError } = await import("@evergenie/sdk-web");
  state.record = { body: shown, fields: {}, revision: "r1" };
  state.get.mockReset().mockImplementation(async () => ({ record: state.record }));
  state.put.mockReset().mockImplementation(async () => {
    // 送出前別處已經改成 r2：vault 以 revision 不符拒絕。
    state.record = { body: newer, fields: {}, revision: "r2" };
    throw new ApiError("conflict", "revision mismatch");
  });
});
afterEach(() => cleanup());

describe("夜班清單：寫入帶畫面上那一版的 revision", () => {
  // Guards: REQ-111
  test("TestNightQueueConflictReloads：版本不符不重試，重新載入並白話提示再按一次", async () => {
    renderQueue();
    await screen.findByText("補測試 B");
    fireEvent.click(screen.getByRole("button", { name: "刪除「補測試 B」" }));
    fireEvent.click(screen.getByRole("button", { name: "確定刪除" }));

    expect((await screen.findByRole("alert")).textContent).toBe(
      "清單剛被改過，已重新載入，請再按一次。",
    );
    // 只送一次，而且是畫面上那一版（r1）改出來的內容；沒有偷偷重讀再寫。
    expect(state.put).toHaveBeenCalledTimes(1);
    const sent = state.put.mock.calls[0]?.[0];
    expect(sent.expected_revision).toBe("r1");
    expect(sent.record.body).toContain("整理 A");
    expect(sent.record.body).not.toContain("補測試 B");
    // 重新載入後畫面是新版，舊的那項還在。
    await screen.findByText("新加進來的 C");
    expect(screen.getByText("補測試 B")).not.toBeNull();
  });

  // Guards: REQ-111
  test("新增撞到也一樣：保留輸入的字，提示再按一次", async () => {
    renderQueue();
    await screen.findByText("補測試 B");
    fireEvent.click(screen.getByRole("button", { name: "新增一項" }));
    const input = screen.getByTestId("night-add") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "寫 D" } });
    fireEvent.click(screen.getByRole("button", { name: /^新增$/ }));

    expect((await screen.findByRole("alert")).textContent).toBe(
      "清單剛被改過，已重新載入，請再按一次。",
    );
    expect(state.put).toHaveBeenCalledTimes(1);
    expect(state.put.mock.calls[0]?.[0].expected_revision).toBe("r1");
    await screen.findByText("新加進來的 C");
    expect(input.value).toBe("寫 D");
  });

  // Guards: REQ-111
  test("沒撞到：照畫面上那一版寫，不先重讀", async () => {
    state.put.mockReset().mockImplementation(async () => ({}));
    renderQueue();
    await screen.findByText("補測試 B");
    const gets = state.get.mock.calls.length;
    fireEvent.click(screen.getByRole("button", { name: "刪除「整理 A」" }));
    fireEvent.click(screen.getByRole("button", { name: "確定刪除" }));
    await waitFor(() => expect(state.put).toHaveBeenCalledTimes(1));
    // 送出前沒有多讀一次（送出後的重新載入可以有）。
    expect(state.get.mock.invocationCallOrder.slice(gets)[0] ?? Infinity).toBeGreaterThan(
      state.put.mock.invocationCallOrder[0] ?? 0,
    );
    expect(state.put.mock.calls[0]?.[0].expected_revision).toBe("r1");
    expect(screen.queryByRole("alert")).toBeNull();
  });

  // Guards: REQ-19, REQ-111
  test("表單：標題加補充欄位，寫成一項加縮排說明行", async () => {
    state.put.mockReset().mockImplementation(async () => ({}));
    renderQueue();
    await screen.findByText("補測試 B");
    fireEvent.click(screen.getByRole("button", { name: "新增一項" }));
    fireEvent.change(screen.getByTestId("night-add"), { target: { value: "review PR" } });
    fireEvent.change(screen.getByTestId("night-out"), { target: { value: "docs/r.md" } });
    fireEvent.click(screen.getByRole("button", { name: /^新增$/ }));
    await waitFor(() => expect(state.put).toHaveBeenCalledTimes(1));
    expect(state.put.mock.calls[0]?.[0].record.body).toMatch(
      /- \[ \] review PR｜加入 \d{4}-\d{2}-\d{2}\n {2}產出：docs\/r\.md/,
    );
  });
});
