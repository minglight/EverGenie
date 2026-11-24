import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { Composer } from "./Composer";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function stubPointer(coarse: boolean) {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: query.includes("coarse") ? coarse : false,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
}

function setup(value: string) {
  const onSend = vi.fn();
  render(
    <Composer
      value={value}
      onChange={() => {}}
      onSend={onSend}
      label="傳訊息給 Athena"
      sendLabel="送出"
    />,
  );
  return { onSend, box: screen.getByLabelText("傳訊息給 Athena") };
}

describe("Composer", () => {
  // Guards: REQ-142
  test("滑鼠與鍵盤的裝置：Enter 送出、Shift+Enter 換行", () => {
    stubPointer(false);
    const { onSend, box } = setup("哈囉");
    fireEvent.keyDown(box, { key: "Enter", shiftKey: true });
    expect(onSend).not.toHaveBeenCalled();
    fireEvent.keyDown(box, { key: "Enter" });
    expect(onSend).toHaveBeenCalledTimes(1);
  });

  // Guards: REQ-142
  test("觸控裝置：Enter 換行、只靠送出鈕", () => {
    stubPointer(true);
    const { onSend, box } = setup("哈囉");
    fireEvent.keyDown(box, { key: "Enter" });
    expect(onSend).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "送出" }));
    expect(onSend).toHaveBeenCalledTimes(1);
  });

  // Guards: REQ-142
  test("輸入法選字中的 Enter 不送出", () => {
    stubPointer(false);
    const { onSend, box } = setup("你好");
    fireEvent.keyDown(box, { key: "Enter", isComposing: true });
    fireEvent.keyDown(box, { key: "Enter", keyCode: 229 });
    expect(onSend).not.toHaveBeenCalled();
  });

  // Guards: REQ-142
  test("沒有字（或只有空白）時送出鈕停用、Enter 也不送", () => {
    stubPointer(false);
    const { onSend, box } = setup("  \n ");
    expect((screen.getByRole("button", { name: "送出" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.keyDown(box, { key: "Enter" });
    expect(onSend).not.toHaveBeenCalled();
  });

  test("外殼先拿到按鍵：preventDefault 了 Enter 就不送", () => {
    stubPointer(false);
    const onSend = vi.fn();
    const onKeyDown = vi.fn((e: { key: string; preventDefault: () => void }) => {
      if (e.key === "Enter") e.preventDefault();
    });
    render(
      <Composer
        value="/clear"
        onChange={() => {}}
        onSend={onSend}
        onKeyDown={onKeyDown}
        inputProps={{ role: "combobox", "aria-expanded": true }}
        label="傳訊息給 Athena"
        sendLabel="送出"
      />,
    );
    const box = screen.getByRole("combobox", { name: "傳訊息給 Athena" });
    fireEvent.keyDown(box, { key: "Enter" });
    expect(onKeyDown).toHaveBeenCalledTimes(1);
    expect(onSend).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "送出" }));
    expect(onSend).toHaveBeenCalledWith("/clear", { raw: "/clear", attachments: 0 });
  });
});

// 附件（REQ-82）：迴紋針選檔 → chip → 送出時組成 Markdown 行。
describe("Composer 附件", () => {
  function attachSetup(
    value: string,
    onUpload: (f: File) => Promise<{ url: string; name: string }>,
  ) {
    const onSend = vi.fn();
    const { container } = render(
      <Composer
        value={value}
        onChange={() => {}}
        onSend={onSend}
        onUpload={onUpload}
        label="傳訊息給 Athena"
        sendLabel="送出"
      />,
    );
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    return { onSend, input };
  }
  const send = () => screen.getByRole("button", { name: "送出" }) as HTMLButtonElement;
  const pick = (input: HTMLInputElement, files: File[]) =>
    fireEvent.change(input, { target: { files } });

  // Guards: REQ-82
  test("沒有 onUpload 就沒有迴紋針；有的話 input 不設 accept／capture、可多選", () => {
    stubPointer(false);
    const { unmount } = render(
      <Composer value="" onChange={() => {}} onSend={() => {}} label="x" sendLabel="送出" />,
    );
    expect(screen.queryByRole("button", { name: "附加檔案" })).toBeNull();
    unmount();
    const { input } = attachSetup("", async () => ({ url: "/share/x", name: "x" }));
    expect(screen.getByRole("button", { name: "附加檔案" })).toBeTruthy();
    expect(input.multiple).toBe(true);
    expect(input.hasAttribute("accept")).toBe(false);
    expect(input.hasAttribute("capture")).toBe(false);
  });

  // Guards: REQ-82
  test("上傳中送出鈕停用；傳完後只有附件也能送，圖片與其他檔案各一行接在文字後面", async () => {
    stubPointer(false);
    const resolvers: ((r: { url: string; name: string }) => void)[] = [];
    const onUpload = vi.fn(
      (f: File) =>
        new Promise<{ url: string; name: string }>((resolve) => {
          resolvers.push((r) => resolve({ ...r, name: f.name }));
        }),
    );
    const { onSend, input } = attachSetup("", onUpload);
    pick(input, [
      new File(["x"], "午餐.png", { type: "image/png" }),
      new File(["y"], "報告.pdf", { type: "application/pdf" }),
    ]);
    // 逐一上傳：第一個還沒結束，第二個不會開始。
    await waitFor(() => expect(onUpload).toHaveBeenCalledTimes(1));
    expect(send().disabled).toBe(true);
    resolvers[0]?.({ url: "/share/20261025-120000-ab12-午餐.png", name: "" });
    await waitFor(() => expect(onUpload).toHaveBeenCalledTimes(2));
    expect(send().disabled).toBe(true);
    resolvers[1]?.({ url: "/share/20261025-120001-cd34-報告.pdf", name: "" });
    await waitFor(() => expect(send().disabled).toBe(false));
    fireEvent.click(send());
    expect(onSend).toHaveBeenCalledWith(
      "![午餐.png](/share/20261025-120000-ab12-午餐.png)\n[報告.pdf](/share/20261025-120001-cd34-報告.pdf)",
      { raw: "", attachments: 2 },
    );
    // 送出後 chip 清掉。
    expect(screen.queryByText("午餐.png")).toBeNull();
  });

  // Guards: REQ-82
  test("有文字時文字在前；失敗的附件可重試，拿掉後才能送", async () => {
    stubPointer(false);
    let calls = 0;
    const onUpload = vi.fn(async (f: File) => {
      calls++;
      if (calls === 1) throw new Error("檔案太大（上限 20 MB）");
      return { url: `/share/ok-${f.name}`, name: f.name };
    });
    const { onSend, input } = attachSetup("看看這個", onUpload);
    pick(input, [new File(["x"], "a.txt", { type: "text/plain" })]);
    await screen.findByText(/上傳失敗：檔案太大/);
    expect(send().disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "重試 a.txt" }));
    await waitFor(() => expect(send().disabled).toBe(false));
    fireEvent.click(send());
    expect(onSend).toHaveBeenCalledWith("看看這個\n[a.txt](/share/ok-a.txt)", {
      raw: "看看這個",
      attachments: 1,
    });

    // 拿掉：失敗的 chip 移除後恢復成只看文字。
    calls = 0;
    pick(input, [new File(["x"], "b.txt", { type: "text/plain" })]);
    await screen.findByText(/上傳失敗/);
    fireEvent.click(screen.getByRole("button", { name: "移除 b.txt" }));
    expect(screen.queryByText("b.txt")).toBeNull();
    expect(send().disabled).toBe(false);
  });
  // Guards: REQ-82
  test("貼上截圖：當附件上傳，不貼進文字", async () => {
    const onUpload = vi.fn(async (f: File) => ({ url: "/share/x.png", name: f.name }));
    render(
      <Composer
        value=""
        onChange={() => {}}
        onSend={() => {}}
        onUpload={onUpload}
        label="傳訊息"
        sendLabel="送出"
      />,
    );
    const png = new File(["x"], "image.png", { type: "image/png" });
    fireEvent.paste(screen.getByLabelText("傳訊息"), { clipboardData: { files: [png] } });
    await waitFor(() => expect(onUpload).toHaveBeenCalledTimes(1));
    expect(onUpload.mock.calls[0]?.[0].name).toMatch(/^paste-\d+\.png$/);
  });
});
