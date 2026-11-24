import type { Task } from "@evergenie/sdk-web";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { TaskStatus } from "./TaskStatus";

const state = vi.hoisted(() => ({ update: vi.fn(), close: vi.fn() }));

vi.mock("@evergenie/sdk-web", () => ({
  ApiError: class extends Error {},
  api: { tasks: { update: state.update, close: state.close } },
}));
vi.mock("@evergenie/ui", () => ({
  cn: (...xs: unknown[]) => xs.filter(Boolean).join(" "),
  showToast: vi.fn(),
}));
vi.mock("../data/live", () => ({ refreshAll: vi.fn() }));

const task = (over: Partial<Task> = {}): Task =>
  ({ id: "SEC-9", title: "換枕頭", type: "task", status: "doing", ...over }) as Task;

function mount(t: Task) {
  const client = new QueryClient();
  return render(
    <QueryClientProvider client={client}>
      <TaskStatus task={t} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.update.mockReset().mockResolvedValue({});
  state.close.mockReset().mockResolvedValue({});
});
afterEach(() => cleanup());

describe("主人手動改任務狀態（TSK-19）", () => {
  // Guards: REQ-267
  test("TestTaskStatusOpenStatesUpdate：待做／進行中／等外部直接改，等你不給手動選", async () => {
    mount(task());
    expect(
      screen.getByRole("button", { name: "改成「進行中」" }).getAttribute("aria-pressed"),
    ).toBe("true");
    expect(screen.queryByRole("button", { name: "改成「等你」" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "改成「待做」" }));
    await waitFor(() => expect(state.update).toHaveBeenCalledWith({ id: "SEC-9", status: "idea" }));
    expect(state.close).not.toHaveBeenCalled();
  });

  // Guards: REQ-267
  test("TestTaskStatusCloseAsksTwiceAndFillsResult：完成要按兩下，沒寫過結果就記成主人手動結單", async () => {
    mount(task());
    fireEvent.click(screen.getByRole("button", { name: "改成「完成」" }));
    expect(state.close).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "確定完成？" }));
    await waitFor(() =>
      expect(state.close).toHaveBeenCalledWith({
        id: "SEC-9",
        status: "done",
        result: "主人在看板手動結單。",
        verify: "unverifiable",
        verify_how: "主人手動結單，沒有另外驗證。",
      }),
    );
    cleanup();
    mount(task({ result: "秘書寫的結果" }));
    fireEvent.click(screen.getByRole("button", { name: "改成「不做了」" }));
    fireEvent.click(screen.getByRole("button", { name: "確定不做了？" }));
    await waitFor(() =>
      expect(state.close).toHaveBeenLastCalledWith({
        id: "SEC-9",
        status: "cancelled",
        result: "秘書寫的結果",
        verify: undefined,
        verify_how: undefined,
      }),
    );
  });
});
