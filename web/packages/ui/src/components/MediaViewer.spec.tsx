import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { StrictMode, useState } from "react";
import { afterEach, beforeEach, describe, expect, test } from "vitest";
import { ImageViewer } from "./ImageViewer";
import { isVideoItem, type MediaItem, MediaViewer } from "./MediaViewer";

const ITEMS: MediaItem[] = [
  { src: "/a/1.png", name: "第一張" },
  { src: "/a/1.mp4", name: "動畫" },
  { src: "/a/2.png", name: "第二張" },
];

function Page({ items = ITEMS }: { items?: MediaItem[] }) {
  const [at, setAt] = useState<number | null>(null);
  return (
    <>
      <button type="button" onClick={() => setAt(0)}>
        開
      </button>
      <span data-testid="at">{at === null ? "關" : String(at)}</span>
      <MediaViewer items={items} index={at} onIndex={setAt} />
    </>
  );
}

const marked = () => (window.history.state as { hgMediaViewer?: boolean } | null)?.hgMediaViewer;
const gone = () => waitFor(() => expect(screen.queryByTestId("media-viewer")).toBeNull());
/** 等「用 ✕ 關掉之後把多疊的那筆歷史退掉」跑完。 */
const settled = () => waitFor(() => expect(marked()).toBeFalsy());

beforeEach(() => {
  window.history.replaceState({ __TSR_index: 3, __TSR_key: "k1" }, "", "/p/closet/outfit?id=a");
});

afterEach(async () => {
  cleanup();
  await settled();
});

describe("看大圖（MediaViewer，FEN-30）", () => {
  test("右上角 ✕ 關掉，按鈕至少 44px", async () => {
    render(<Page />);
    expect(screen.queryByTestId("media-viewer")).toBeNull();
    fireEvent.click(screen.getByText("開"));
    const viewer = await screen.findByTestId("media-viewer");
    expect(within(viewer).getByTestId("media-viewer-img").getAttribute("src")).toBe("/a/1.png");
    const close = within(viewer).getByRole("button", { name: "關閉" });
    expect(close.className).toContain("size-11");
    fireEvent.click(close);
    await gone();
    expect(screen.getByTestId("at").textContent).toBe("關");
  });

  test("點大圖本身不關，點背景關掉", async () => {
    render(<Page />);
    fireEvent.click(screen.getByText("開"));
    const viewer = await screen.findByTestId("media-viewer");
    fireEvent.click(within(viewer).getByTestId("media-viewer-img"));
    expect(screen.queryByTestId("media-viewer")).not.toBeNull();
    fireEvent.click(viewer);
    await gone();
  });

  test("Esc 關掉", async () => {
    render(<Page />);
    fireEvent.click(screen.getByText("開"));
    const viewer = await screen.findByTestId("media-viewer");
    fireEvent.keyDown(viewer, { key: "Escape" });
    await gone();
  });

  // 手機的上一頁／返回手勢：開著時多疊一筆同網址的歷史，退一筆＝關掉大圖，網址不變、沒有離開頁面。
  test("開著時上一頁是關掉大圖，不是離開頁面", async () => {
    render(<Page />);
    fireEvent.click(screen.getByText("開"));
    await screen.findByTestId("media-viewer");
    // 多疊的那筆帶著原本的 history.state（路由的鍵不變，套件頁不重掛），第幾筆加一。
    expect(window.history.state).toEqual({ __TSR_index: 4, __TSR_key: "k1", hgMediaViewer: true });
    expect(window.location.pathname + window.location.search).toBe("/p/closet/outfit?id=a");
    act(() => window.history.back());
    await gone();
    expect(screen.getByTestId("at").textContent).toBe("關");
    expect(window.history.state).toEqual({ __TSR_index: 3, __TSR_key: "k1" });
    expect(window.location.pathname + window.location.search).toBe("/p/closet/outfit?id=a");
  });

  // 用 ✕ 關的：多疊的那筆自己退掉，之後按上一頁才會真的回前一頁（不用多按一次）。
  test("用 ✕ 關掉後歷史不多一筆", async () => {
    render(<Page />);
    fireEvent.click(screen.getByText("開"));
    const viewer = await screen.findByTestId("media-viewer");
    expect(marked()).toBe(true);
    fireEvent.click(within(viewer).getByRole("button", { name: "關閉" }));
    await gone();
    await settled();
    expect(window.history.state).toEqual({ __TSR_index: 3, __TSR_key: "k1" });
  });

  // 開發模式的 StrictMode 會把 effect 收掉再跑一次：不能因此多疊一筆，也不能自己關掉。
  test("StrictMode 下只疊一筆、不會自己關掉", async () => {
    render(
      <StrictMode>
        <Page />
      </StrictMode>,
    );
    fireEvent.click(screen.getByText("開"));
    await screen.findByTestId("media-viewer");
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByTestId("media-viewer")).not.toBeNull();
    expect(window.history.state).toEqual({ __TSR_index: 4, __TSR_key: "k1", hgMediaViewer: true });
  });

  test("多張可以左右換（按鈕與方向鍵），影片用 video 播", async () => {
    render(<Page />);
    fireEvent.click(screen.getByText("開"));
    const viewer = await screen.findByTestId("media-viewer");
    expect(within(viewer).getByTestId("media-viewer-count").textContent).toBe("1 / 3");
    fireEvent.click(within(viewer).getByRole("button", { name: "下一張" }));
    const video = within(viewer).getByTestId("media-viewer-video");
    expect(video.getAttribute("src")).toBe("/a/1.mp4");
    expect(video.hasAttribute("controls")).toBe(true);
    expect(video.hasAttribute("playsinline")).toBe(true);
    fireEvent.keyDown(viewer, { key: "ArrowRight" });
    expect(within(viewer).getByTestId("media-viewer-img").getAttribute("src")).toBe("/a/2.png");
    // 最後一張再往右回到第一張；第一張往左到最後一張。
    fireEvent.keyDown(viewer, { key: "ArrowRight" });
    expect(screen.getByTestId("at").textContent).toBe("0");
    fireEvent.click(within(viewer).getByRole("button", { name: "上一張" }));
    expect(screen.getByTestId("at").textContent).toBe("2");
    // 換張不多疊歷史。
    expect(window.history.state).toEqual({ __TSR_index: 4, __TSR_key: "k1", hgMediaViewer: true });
  });

  test("只有一張沒有左右換的按鈕", async () => {
    render(<Page items={[{ src: "/a/1.png", name: "一張" }]} />);
    fireEvent.click(screen.getByText("開"));
    const viewer = await screen.findByTestId("media-viewer");
    expect(within(viewer).queryByTestId("media-viewer-next")).toBeNull();
    expect(within(viewer).queryByTestId("media-viewer-count")).toBeNull();
  });

  test("看副檔名分影片與圖，有寫 type 照 type", () => {
    expect(isVideoItem({ src: "/x/a.mp4" })).toBe(true);
    expect(isVideoItem({ src: "/x/a.MOV?v=2" })).toBe(true);
    expect(isVideoItem({ src: "/x/a.png" })).toBe(false);
    expect(isVideoItem({ src: "/x/a", type: "video" })).toBe(true);
    expect(isVideoItem({ src: "/x/a.mp4", type: "image" })).toBe(false);
  });

  test("ImageViewer：一張縮圖點了就是看大圖", async () => {
    render(<ImageViewer src="/a/9.png" name="九" />);
    fireEvent.click(screen.getByRole("button", { name: "看大圖：九" }));
    const viewer = await screen.findByTestId("media-viewer");
    expect(within(viewer).getByRole("img", { name: "九" }).getAttribute("src")).toBe("/a/9.png");
    act(() => window.history.back());
    await gone();
  });
});
