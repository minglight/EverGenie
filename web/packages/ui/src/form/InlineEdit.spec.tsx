import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { ChipsInput } from "./ChipsInput";
import { InlineEdit } from "./InlineEdit";

afterEach(cleanup);

function setup(
  onSave: (v: string) => Promise<void> | void,
  extra: { multiline?: boolean; actions?: { save: string; cancel: string } } = {},
) {
  render(
    <InlineEdit
      label="內容"
      value="打給牙醫"
      onSave={onSave}
      emptyText="（沒有內容）"
      savedText="已儲存"
      requiredText="內容不能空白"
      {...extra}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: /打給牙醫/ }));
  return screen.getByLabelText("內容") as HTMLInputElement;
}

describe("InlineEdit", () => {
  // Guards: REQ-116
  test("點文字變輸入框，Enter 存，旁邊顯示「已儲存」", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const box = setup(onSave);
    fireEvent.change(box, { target: { value: "  改約下週  " } });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(onSave).toHaveBeenCalledWith("改約下週"));
    expect(await screen.findByText("已儲存")).toBeTruthy();
  });

  // Guards: REQ-116
  test("Esc 還原：不存，也不會因為失焦而存", () => {
    const onSave = vi.fn();
    const box = setup(onSave);
    fireEvent.change(box, { target: { value: "亂打的" } });
    fireEvent.keyDown(box, { key: "Escape" });
    fireEvent.blur(box);
    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: /打給牙醫/ })).toBeTruthy();
  });

  // Guards: REQ-116
  test("失焦也會存；內容沒改就不送", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const box = setup(onSave);
    fireEvent.blur(box);
    expect(onSave).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: /打給牙醫/ }));
    const again = screen.getByLabelText("內容");
    fireEvent.change(again, { target: { value: "改約" } });
    fireEvent.blur(again);
    await waitFor(() => expect(onSave).toHaveBeenCalledWith("改約"));
  });

  // Guards: REQ-116
  test("伺服器拒絕：原因寫在欄位下，留在編輯狀態", async () => {
    const onSave = vi.fn().mockRejectedValue(new Error("這個狀態不能修改"));
    const box = setup(onSave);
    fireEvent.change(box, { target: { value: "新的內容" } });
    fireEvent.keyDown(box, { key: "Enter" });
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("這個狀態不能修改");
    expect(screen.getByLabelText("內容")).toBeTruthy();
    expect(screen.queryByText("已儲存")).toBeNull();
  });

  // Guards: REQ-116
  test("必填：空白存不了，不呼叫伺服器", async () => {
    const onSave = vi.fn();
    const box = setup(onSave);
    fireEvent.change(box, { target: { value: "   " } });
    fireEvent.keyDown(box, { key: "Enter" });
    expect((await screen.findByRole("alert")).textContent).toBe("內容不能空白");
    expect(onSave).not.toHaveBeenCalled();
  });

  // Guards: REQ-116
  test("多行：Enter 換行，⌘+Enter 才存", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const box = setup(onSave, { multiline: true });
    fireEvent.change(box, { target: { value: "第一行\n第二行" } });
    fireEvent.keyDown(box, { key: "Enter" });
    expect(onSave).not.toHaveBeenCalled();
    fireEvent.keyDown(box, { key: "Enter", metaKey: true });
    await waitFor(() => expect(onSave).toHaveBeenCalledWith("第一行\n第二行"));
  });

  // Guards: REQ-116
  test("多行給 actions：失焦不存，按存才存，按取消還原", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const box = setup(onSave, { multiline: true, actions: { save: "存", cancel: "取消" } });
    fireEvent.change(box, { target: { value: "寫到一半" } });
    fireEvent.blur(box);
    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByLabelText("內容")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.queryByLabelText("內容")).toBeNull();
    expect(onSave).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: /打給牙醫/ }));
    fireEvent.change(screen.getByLabelText("內容"), { target: { value: "寫好了" } });
    fireEvent.click(screen.getByRole("button", { name: "存" }));
    await waitFor(() => expect(onSave).toHaveBeenCalledWith("寫好了"));
  });

  test("沒改就離開：別人這段時間改了內容，也不把舊的蓋回去", () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const props = {
      label: "內容",
      onSave,
      emptyText: "（沒有內容）",
      savedText: "已儲存",
    };
    const { rerender } = render(<InlineEdit {...props} value="舊內容" />);
    fireEvent.click(screen.getByRole("button", { name: /舊內容/ }));
    rerender(<InlineEdit {...props} value="別人改的" />);
    fireEvent.blur(screen.getByLabelText("內容"));
    expect(onSave).not.toHaveBeenCalled();
  });

  test("輸入法選字中的 Enter 不存", () => {
    const onSave = vi.fn();
    const box = setup(onSave);
    fireEvent.change(box, { target: { value: "你好" } });
    fireEvent.keyDown(box, { key: "Enter", keyCode: 229 });
    expect(onSave).not.toHaveBeenCalled();
  });

  // RMD-23：Markdown 描述裡有連結與圖：內容不包在按鈕裡，點連結不會進編輯；按標籤旁的「改描述」才編輯。
  test("有 editLabel 時內容在按鈕外，按編輯鈕才編輯；沒有值照舊點空白字", () => {
    const view = render(
      <InlineEdit
        label="描述"
        value="看 [這裡](https://example.com)"
        onSave={() => {}}
        emptyText="（沒有描述）"
        savedText="已儲存"
        editLabel="改描述"
        multiline
        display={<a href="#here">這裡</a>}
      />,
    );
    const link = screen.getByRole("link", { name: "這裡" });
    expect(link.closest("button")).toBeNull();
    fireEvent.click(link);
    expect(screen.queryByRole("textbox")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "改描述" }));
    expect((screen.getByLabelText("描述") as HTMLTextAreaElement).value).toBe(
      "看 [這裡](https://example.com)",
    );
    view.unmount();
    render(
      <InlineEdit
        label="描述"
        value=""
        onSave={() => {}}
        emptyText="（沒有描述）"
        savedText="已儲存"
        editLabel="改描述"
      />,
    );
    expect(screen.queryByRole("button", { name: "改描述" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /沒有描述/ }));
    expect(screen.getByLabelText("描述")).toBeTruthy();
  });
});

describe("ChipsInput", () => {
  // Guards: REQ-116
  test("Enter 或逗號成一顆；重複的不加；失敗時原因寫在欄位下", async () => {
    const onAdd = vi
      .fn()
      .mockResolvedValueOnce(undefined)
      .mockRejectedValueOnce(new Error("連結不正確"));
    render(<ChipsInput label="連結" values={["A"]} onAdd={onAdd} />);
    const box = screen.getByLabelText("連結");
    fireEvent.change(box, { target: { value: "A" } });
    fireEvent.keyDown(box, { key: "Enter" });
    expect(onAdd).not.toHaveBeenCalled();
    fireEvent.change(box, { target: { value: "B" } });
    await act(async () => {
      fireEvent.keyDown(box, { key: "," });
    });
    expect(onAdd).toHaveBeenCalledWith("B");
    fireEvent.change(box, { target: { value: "C" } });
    await act(async () => {
      fireEvent.keyDown(box, { key: "Enter" });
    });
    expect((await screen.findByRole("alert")).textContent).toBe("連結不正確");
  });

  // Guards: REQ-116
  test("可刪的時候：空白輸入框按退格刪最後一顆", async () => {
    const onRemove = vi.fn().mockResolvedValue(undefined);
    render(<ChipsInput label="標籤" values={["甲", "乙"]} onAdd={() => {}} onRemove={onRemove} />);
    fireEvent.keyDown(screen.getByLabelText("標籤"), { key: "Backspace" });
    await waitFor(() => expect(onRemove).toHaveBeenCalledWith("乙"));
  });
});
