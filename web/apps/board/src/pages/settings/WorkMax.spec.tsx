import { ApiError } from "@evergenie/sdk-web";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { WorkMaxSetting } from "./WorkMax";

const state = vi.hoisted(() => ({
  value: { work_max: 3, set: false, default: 3, min: 1, max: 10 } as Record<string, unknown>,
  get: vi.fn(),
  set: vi.fn(),
}));

vi.mock("@evergenie/sdk-web", () => {
  class ApiError extends Error {
    readonly code: string;
    constructor(code: string, message: string) {
      super(message);
      this.code = code;
    }
  }
  return { ApiError, api: { system: { getWorkMax: state.get, setWorkMax: state.set } } };
});

function renderIt() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <WorkMaxSetting />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.value = { work_max: 3, set: false, default: 3, min: 1, max: 10 };
  state.get.mockImplementation(async () => state.value);
  state.set.mockImplementation(async ({ work_max }: { work_max: number }) => {
    state.value = { ...state.value, work_max, set: true };
    return { work_max };
  });
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

// 設定 → 職位與秘書 → 同時進行的對話（WEB-186）。
describe("同時進行的對話上限", () => {
  test("沒寫：顯示 3 個（預設），說明照主人的意思寫，說「對話」不說 session", async () => {
    renderIt();
    expect((await screen.findByTestId("work-max-value")).textContent).toContain("3 個（預設）");
    const text = document.body.textContent ?? "";
    expect(text).toContain("每張「進行中」的任務單會各開一個對話同時做");
    expect(text).toContain("AI 額度和電腦的記憶體");
    expect(text).toContain("超過上限的單會排隊");
    expect(text).toContain("預設 3 個");
    expect(text).not.toMatch(/session/i);
  });

  test("按「多一個」就存 4、不再寫預設；到上限「多一個」不能按", async () => {
    renderIt();
    await screen.findByTestId("work-max-value");
    fireEvent.click(screen.getByRole("button", { name: "多一個" }));
    await waitFor(() => expect(state.set).toHaveBeenCalledWith({ work_max: 4 }));
    await waitFor(() => expect(screen.getByTestId("work-max-value").textContent).toContain("4 個"));
    expect(screen.getByTestId("work-max-value").textContent).not.toContain("預設");

    cleanup();
    state.value = { ...state.value, work_max: 10, set: true };
    renderIt();
    await screen.findByText("10 個");
    expect((screen.getByRole("button", { name: "多一個" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect((screen.getByRole("button", { name: "少一個" }) as HTMLButtonElement).disabled).toBe(
      false,
    );
  });

  test("到下限「少一個」不能按", async () => {
    state.value = { ...state.value, work_max: 1, set: true };
    renderIt();
    await screen.findByText("1 個");
    expect((screen.getByRole("button", { name: "少一個" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
  });

  test("伺服器拒絕：寫原因、退回原本的值", async () => {
    state.set.mockRejectedValueOnce(
      new (ApiError as unknown as new (c: string, m: string) => Error)(
        "forbidden",
        "只有主人能改同時進行的對話上限",
      ),
    );
    renderIt();
    await screen.findByTestId("work-max-value");
    fireEvent.click(screen.getByRole("button", { name: "少一個" }));
    expect((await screen.findByRole("alert")).textContent).toContain("沒改成：只有主人能改");
    await waitFor(() => expect(screen.getByTestId("work-max-value").textContent).toContain("3 個"));
  });
});
