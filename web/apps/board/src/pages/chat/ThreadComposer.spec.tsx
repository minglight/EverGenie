import type { Api } from "@evergenie/sdk-web";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { t } from "../../i18n/zh-TW";
import { ThreadComposer } from "./ThreadComposer";

const toast = vi.hoisted(() => vi.fn());
vi.mock("@evergenie/ui", async (orig) => ({
  ...(await orig<typeof import("@evergenie/ui")>()),
  showToast: toast,
}));

afterEach(() => {
  cleanup();
  toast.mockClear();
  localStorage.clear();
});

function setup(command = vi.fn(async (_req: unknown) => ({ note: {} }))) {
  const onSend = vi.fn();
  const onCommand = vi.fn();
  const api = { session: { command } } as unknown as Api;
  render(
    <ThreadComposer
      name="code"
      session="code"
      api={api}
      base=""
      agentName="雅典娜"
      onSend={onSend}
      onCommand={onCommand}
    />,
  );
  const box = screen.getByRole("combobox", { name: t("chat.compose", { name: "雅典娜" }) });
  const type = (v: string) => fireEvent.change(box, { target: { value: v } });
  return { command, onSend, onCommand, box, type };
}

describe("輸入框的「/」指令（WEB-83）", () => {
  test("打 / 跳出選單，每列有指令與說明；Esc 關掉", () => {
    const { box, type } = setup();
    type("/");
    const menu = screen.getByRole("listbox", { name: t("slash.menu") });
    expect(menu.textContent).toContain("/clear");
    expect(menu.textContent).toContain(t("slash.compact"));
    expect(box.getAttribute("aria-expanded")).toBe("true");
    fireEvent.keyDown(box, { key: "Escape" });
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  test("點 /compact：直接送 session.command，不當訊息送，toast 已送出、留一行紀錄", async () => {
    const { command, onSend, onCommand, type } = setup();
    type("/");
    fireEvent.click(screen.getByRole("option", { name: /\/compact/ }));
    await waitFor(() => expect(command).toHaveBeenCalledTimes(1));
    expect(command.mock.calls[0]?.[0]).toEqual({ session: "code", command: "compact", arg: "" });
    expect(onSend).not.toHaveBeenCalled();
    await waitFor(() => expect(toast).toHaveBeenCalledWith(t("slash.sent", { cmd: "/compact" })));
    expect(onCommand).toHaveBeenCalledWith("/compact");
  });

  test("/clear 先確認，按了清掉才送", async () => {
    const { command, box, type } = setup();
    type("/cl");
    fireEvent.keyDown(box, { key: "Enter" });
    expect(command).not.toHaveBeenCalled();
    expect(screen.getByText(t("slash.clear.body"))).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: t("slash.clear.confirm") }));
    await waitFor(() => expect(command).toHaveBeenCalledTimes(1));
    expect(command.mock.calls[0]?.[0]).toMatchObject({ command: "clear" });
  });

  test("/model 選了出第二層，方向鍵＋Enter 選 opus", async () => {
    const { command, box, type } = setup();
    type("/mo");
    fireEvent.keyDown(box, { key: "Enter" });
    expect((box as HTMLTextAreaElement).value).toBe("/model ");
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual([
      expect.stringContaining("sonnet"),
      expect.stringContaining("opus"),
      expect.stringContaining("fable"),
      expect.stringContaining("haiku"),
    ]);
    fireEvent.keyDown(box, { key: "ArrowDown" });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(command).toHaveBeenCalledTimes(1));
    expect(command.mock.calls[0]?.[0]).toMatchObject({ command: "model", arg: "opus" });
  });

  test("/effort 選了等級要先確認（會變成這個帳號的預設）", async () => {
    const { command, type } = setup();
    type("/effort ");
    fireEvent.click(screen.getByRole("option", { name: /^high/ }));
    expect(screen.getByText(t("slash.effort.body"))).toBeTruthy();
    expect(command).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: t("slash.effort.confirm") }));
    await waitFor(() => expect(command).toHaveBeenCalledTimes(1));
    expect(command.mock.calls[0]?.[0]).toMatchObject({ command: "effort", arg: "high" });
  });

  test("不認得的 /xxx 按送出：提示看板支援哪些，不送；前面加空白就當一般訊息", () => {
    const { command, onSend, type } = setup();
    type("/help");
    fireEvent.click(screen.getByRole("button", { name: t("chat.send") }));
    expect(screen.getByTestId("slash-hint").textContent).toBe(t("slash.unknown"));
    expect(onSend).not.toHaveBeenCalled();
    expect(command).not.toHaveBeenCalled();
    type(" /help 是什麼");
    fireEvent.click(screen.getByRole("button", { name: t("chat.send") }));
    expect(onSend).toHaveBeenCalledWith("/help 是什麼");
    type("/tmp 為什麼滿了");
    fireEvent.click(screen.getByRole("button", { name: t("chat.send") }));
    expect(onSend).toHaveBeenLastCalledWith("/tmp 為什麼滿了");
    expect(command).not.toHaveBeenCalled();
  });

  test("指令送出中：送出鈕停用、轉圈，回來才放開", async () => {
    let finish = () => {};
    const command = vi.fn(
      () =>
        new Promise((resolve) => {
          finish = () => resolve({ note: {} });
        }),
    );
    const { box, type } = setup(command as never);
    type("/compact");
    fireEvent.keyDown(box, { key: "Enter" });
    const send = screen.getByRole("button", { name: t("chat.send") });
    await waitFor(() => expect(send.getAttribute("aria-busy")).toBe("true"));
    expect((send as HTMLButtonElement).disabled).toBe(true);
    finish();
    await waitFor(() => expect(send.getAttribute("aria-busy")).toBeNull());
  });

  test("伺服器拒絕（她在忙）：原因寫在輸入框上面，字留著", async () => {
    const { ApiError } = await import("@evergenie/sdk-web");
    const command = vi.fn(async () => {
      throw new ApiError("conflict", "還在忙，等這一輪做完或先按停止再下指令");
    });
    const { box, type } = setup(command as never);
    type("/compact");
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("還在忙"));
    expect((box as HTMLTextAreaElement).value).toBe("/compact");
    expect(toast).not.toHaveBeenCalled();
  });
});
