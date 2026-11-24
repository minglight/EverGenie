import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { ReportProblem } from "./ReportProblem";

const state = vi.hoisted(() => ({ report: vi.fn() }));

vi.mock("@evergenie/sdk-web", () => {
  class ApiError extends Error {
    constructor(
      readonly code: string,
      message: string,
    ) {
      super(message);
    }
  }
  return { ApiError, api: { system: { report: state.report } } };
});

const writeText = vi.fn();

beforeEach(() => {
  state.report.mockReset();
  writeText.mockReset();
  writeText.mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
});
afterEach(cleanup);

describe("回報問題", () => {
  // 按了才產生；內容整段先顯示給主人看，之後才有「開回報頁」「複製內容」。
  test("TestReportShowsContentBeforeAnyAction", async () => {
    state.report.mockResolvedValue({
      title: "問題回報：1.2.3",
      body: "## 環境\n- 版本：1.2.3",
      issue_link: "https://example.invalid/issues/new?title=x&body=y",
    });
    render(<ReportProblem />);
    expect(state.report).not.toHaveBeenCalled();
    expect(screen.queryByTestId("report-preview")).toBeNull();
    expect(screen.queryByTestId("report-open")).toBeNull();
    expect(screen.queryByTestId("report-copy")).toBeNull();

    fireEvent.click(screen.getByTestId("report-make"));
    const preview = await screen.findByTestId("report-preview");
    expect(state.report).toHaveBeenCalledWith({ detail: false });
    expect(preview.textContent).toContain("問題回報：1.2.3");
    expect(preview.textContent).toContain("- 版本：1.2.3");

    const open = screen.getByTestId("report-open");
    expect(open.getAttribute("href")).toBe("https://example.invalid/issues/new?title=x&body=y");
    expect(open.getAttribute("target")).toBe("_blank");
    expect(open.getAttribute("rel")).toBe("noopener noreferrer");
    // 產生完只是顯示：沒有自己開網頁、沒有自己複製。
    expect(writeText).not.toHaveBeenCalled();
  });

  // 回報頁還沒設定（issue_link 空）：沒有開網頁的按鈕，主要動作是「複製內容」。
  test("TestReportWithoutIssueUrlOnlyCopies", async () => {
    state.report.mockResolvedValue({ title: "問題回報：1.2.3", body: "內文" });
    render(<ReportProblem />);
    fireEvent.click(screen.getByTestId("report-make"));
    await screen.findByTestId("report-preview");
    expect(screen.queryByTestId("report-open")).toBeNull();
    fireEvent.click(screen.getByTestId("report-copy"));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("問題回報：1.2.3\n\n內文"));
  });

  // 選「詳細」：還沒產生就只記選擇；已經有內容就照新的選擇重新產生，畫面換成新的內容。
  test("TestReportDetailRegenerates", async () => {
    state.report.mockImplementation(async (req: { detail?: boolean }) => ({
      title: "t",
      body: req.detail ? "詳細內容" : "一般內容",
    }));
    render(<ReportProblem />);
    fireEvent.click(screen.getByRole("button", { name: "詳細" }));
    expect(state.report).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("report-make"));
    await waitFor(() =>
      expect(screen.getByTestId("report-preview").textContent).toContain("詳細內容"),
    );
    expect(state.report).toHaveBeenLastCalledWith({ detail: true });
    fireEvent.click(screen.getByRole("button", { name: "一般" }));
    // 重新整理的那一下：舊的（詳細）內容不留在畫面上，也沒有可以按的「複製內容」。
    expect(screen.queryByTestId("report-preview")).toBeNull();
    expect(screen.queryByTestId("report-copy")).toBeNull();
    expect(screen.getByTestId("report-busy")).toBeTruthy();
    await waitFor(() =>
      expect(screen.getByTestId("report-preview").textContent).toContain("一般內容"),
    );
  });

  test("TestReportFailureShowsReason", async () => {
    state.report.mockRejectedValue(new Error("boom"));
    render(<ReportProblem />);
    fireEvent.click(screen.getByTestId("report-make"));
    expect((await screen.findByRole("alert")).textContent).not.toContain("boom");
    expect(screen.queryByTestId("report-preview")).toBeNull();
  });
});
