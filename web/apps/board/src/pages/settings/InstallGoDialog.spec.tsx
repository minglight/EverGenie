import { ApiError } from "@evergenie/sdk-web";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { InstallGoDialog } from "./InstallGoDialog";

const state = vi.hoisted(() => ({
  installGo: vi.fn(),
}));

vi.mock("@evergenie/sdk-web", () => ({
  api: { package: { installGo: state.installGo } },
  ApiError: class ApiError extends Error {
    constructor(
      readonly code: string,
      message: string,
    ) {
      super(message);
    }
  },
}));

beforeEach(() => {
  state.installGo.mockReset();
  state.installGo.mockResolvedValue({
    command: "下載 Go go1.27.4 到程式自己的資料夾",
    version: "go1.27.4",
    packages: [{ id: "fitness", name: "健身" }],
  });
});
afterEach(() => cleanup());

describe("InstallGoDialog", () => {
  // Guards: REQ-171
  test("TestSkipSendsNothing：按「先不要」不送任何請求，只關閉", () => {
    const onClose = vi.fn();
    render(<InstallGoDialog open count={2} onClose={onClose} />);
    expect(screen.getByText(/有 2 個套件要先裝編譯工具（Go）才能用/)).not.toBeNull();
    expect(screen.getByText(/程式自己的資料夾/)).not.toBeNull();
    expect(screen.queryByText(/Homebrew/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "先不要" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(state.installGo).not.toHaveBeenCalled();
  });

  // Guards: REQ-171
  test("TestInstallSendsConfirmedOnce：按「安裝」只送一次，而且 confirmed 是 true", async () => {
    const onInstalled = vi.fn();
    render(<InstallGoDialog open count={1} onClose={() => {}} onInstalled={onInstalled} />);
    const button = screen.getByRole("button", { name: "安裝" });
    fireEvent.click(button);
    // 送出中按鈕停用，連點不會送第二次。
    fireEvent.click(button);
    await waitFor(() => expect(screen.getByText(/重新編譯了：健身/)).not.toBeNull());
    expect(state.installGo).toHaveBeenCalledTimes(1);
    expect(state.installGo).toHaveBeenCalledWith({ confirmed: true });
    expect(onInstalled).toHaveBeenCalledTimes(1);
  });

  // Guards: REQ-171
  test("失敗時顯示白話原因，不關掉對話框", async () => {
    state.installGo.mockRejectedValue(
      new ApiError("unavailable", "下載不到 Go（可能是網路不通），沒有裝；等網路正常後再試一次"),
    );
    render(<InstallGoDialog open count={1} onClose={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "安裝" }));
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toBe(
        "安裝 Go：下載不到 Go（可能是網路不通），沒有裝；等網路正常後再試一次",
      ),
    );
    expect(screen.getByRole("button", { name: "完成" })).not.toBeNull();
  });
});
