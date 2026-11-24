import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { useRef } from "react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { AttachmentSection } from "./AttachmentSection";

const state = vi.hoisted(() => ({ upload: vi.fn(), desktop: true }));
vi.mock("../lib/media", () => ({ useIsDesktop: () => state.desktop }));
vi.mock("@evergenie/sdk-web", async () => {
  const actual = await vi.importActual<typeof import("@evergenie/sdk-web")>("@evergenie/sdk-web");
  return { ...actual, upload: (...a: unknown[]) => state.upload(...a) };
});

const site = { key: "", base: "" };

// 今天頁右欄：旁邊有清單與快速輸入，附件段只收「貼給這張單」的貼上（pasteScope）。
function Pane({ onAdd }: { onAdd: (urls: string[]) => Promise<unknown> }) {
  const pane = useRef<HTMLDivElement>(null);
  return (
    <>
      <input aria-label="旁邊的輸入框" />
      <div data-testid="outside">清單</div>
      <div ref={pane} data-testid="pane">
        <p>這張單</p>
        <AttachmentSection
          attachments={[]}
          site={site}
          open
          onAdd={onAdd}
          onRemove={async () => {}}
          pasteScope={pane}
        />
      </div>
    </>
  );
}

function paste(target: Element | Document) {
  const shot = new File(["x"], "image.png", { type: "image/png" });
  fireEvent.paste(target, { clipboardData: { files: [shot], types: ["Files"] } });
}

beforeEach(() => {
  state.upload.mockReset().mockResolvedValue({ name: "x", url: "/share/p.png", size: 1 });
});
afterEach(cleanup);

describe("AttachmentSection 貼上範圍", () => {
  // TSK-28：右欄只收點過右欄之後、或焦點在右欄裡的貼上；旁邊的輸入框與沒點過右欄時的整頁貼上不收。
  test("TestAttachmentPasteScope：只收貼給這張單的貼上", async () => {
    const onAdd = vi.fn().mockResolvedValue(undefined);
    const view = render(<Pane onAdd={onAdd} />);
    paste(document.body);
    paste(view.getByLabelText("旁邊的輸入框"));
    fireEvent.pointerDown(view.getByTestId("outside"));
    paste(document.body);
    await new Promise((r) => setTimeout(r, 20));
    expect(state.upload).not.toHaveBeenCalled();
    // 點過右欄再貼（焦點不在任何輸入框，事件落在 body）：收。
    fireEvent.pointerDown(view.getByText("這張單"));
    paste(document.body);
    await waitFor(() => expect(onAdd).toHaveBeenCalledWith(["/share/p.png"]));
    expect(state.upload).toHaveBeenCalledTimes(1);
  });
});

describe("AttachmentSection 說明字", () => {
  // DEV-156 U-17：手機沒有鍵盤快速鍵，不寫 ⌘V，改「拍照或選檔案」；桌機照舊。
  test("手機寬度不提 ⌘V，桌機才提", () => {
    state.desktop = false;
    const view = render(<Pane onAdd={async () => {}} />);
    expect(view.getByTestId("attach-hint").textContent).toContain("拍照或選檔案");
    expect(view.getByTestId("attach-hint").textContent).not.toContain("⌘V");
    cleanup();
    state.desktop = true;
    const again = render(<Pane onAdd={async () => {}} />);
    expect(again.getByTestId("attach-hint").textContent).toContain("⌘V");
  });
});
