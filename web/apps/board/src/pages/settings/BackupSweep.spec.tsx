import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { BackupSweep } from "./BackupSweep";

const state = vi.hoisted(() => ({ sweep: vi.fn() }));

vi.mock("@evergenie/sdk-web", () => {
  class ApiError extends Error {
    readonly code: string;
    constructor(code: string, message: string) {
      super(message);
      this.code = code;
    }
  }
  return { ApiError, api: { system: { backupSweep: state.sweep } } };
});

const report = {
  at: "2026-11-22T04:30:00+08:00",
  took_ms: 900,
  by: "system",
  current: 3,
  current_bytes: 2048,
  pruned: 2,
  pruned_bytes: 1024,
  expired: [
    {
      path: "~/Workspace/a.bak-20261001-x",
      size: 10,
      mod_time: "",
      state: "expired",
      checked: "查過：沒有開機服務、執行中的程式、終端機視窗用到它",
    },
    {
      path: "~/Workspace/svc.bak-20260901-move",
      size: 10,
      mod_time: "",
      state: "in_use",
      used_by: "開機服務：x.plist（指到上層資料夾）",
    },
  ],
  misnamed: [
    {
      path: "~/Workspace/n.bak",
      size: 1,
      mod_time: "",
      problem: "沒有日期",
      suggest: "改名成 n.bak-20261022-<原因>",
    },
  ],
  skipped: [],
};

function renderSweep() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <BackupSweep />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.sweep.mockReset();
});
afterEach(cleanup);

describe("備份掃描", () => {
  // 還沒掃過：說會自動掃、可以按「現在掃一次」；按了才真的掃（run: true），畫出報告。
  test("TestSweepEmptyThenRunShowsReport", async () => {
    state.sweep.mockImplementation(async (req: { run?: boolean }) =>
      req.run ? { report, running: false } : { running: false },
    );
    renderSweep();
    expect(await screen.findByText(/還沒掃過/)).toBeTruthy();
    expect(state.sweep).toHaveBeenCalledWith({});
    fireEvent.click(screen.getByTestId("sweep-run"));
    const summary = await screen.findByTestId("sweep-summary");
    expect(state.sweep).toHaveBeenCalledWith({ run: true });
    expect(summary.textContent).toContain("可刪 1、在用 1、沒查完 0");
    expect(summary.textContent).toContain("名字不合規 1");
    expect(summary.textContent).toContain("期限內 3 個");
    // 在用的只在「在用」那組，不在可刪那組。
    expect(screen.getByTestId("sweep-group-deletable").textContent).not.toContain("svc.bak");
    expect(screen.getByTestId("sweep-group-inUse").textContent).toContain("上層資料夾");
    expect(screen.getByTestId("sweep-group-misnamed").textContent).toContain(
      "n.bak-20261022-<原因>",
    );
    expect(screen.queryByTestId("sweep-group-skipped")).toBeNull();
  });

  // 報告有出錯：紅框列出來（伺服器已經通知主人）。
  test("TestSweepShowsErrors", async () => {
    state.sweep.mockResolvedValue({
      report: { ...report, errors: ["「在用」檢查（執行中的程式）沒做成"] },
      running: false,
    });
    renderSweep();
    const alert = await screen.findByTestId("sweep-errors");
    expect(alert.textContent).toContain("「在用」檢查（執行中的程式）沒做成");
  });

  // 掃描失敗：按鈕下面寫白話原因，報告不動。
  test("TestSweepRunFailureShowsReason", async () => {
    const { ApiError } = await import("@evergenie/sdk-web");
    state.sweep.mockImplementation(async (req: { run?: boolean }) => {
      if (req.run) throw new ApiError("unavailable", "還不知道家在哪裡");
      return { report, running: false };
    });
    renderSweep();
    await screen.findByTestId("sweep-summary");
    fireEvent.click(screen.getByTestId("sweep-run"));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("沒掃成"));
    expect(screen.getByTestId("sweep-summary")).toBeTruthy();
  });
});
