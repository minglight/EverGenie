import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, test } from "vitest";
import { Markdown, shareHref } from "./Markdown";

afterEach(cleanup);

describe("Markdown 的 /share 連結", () => {
  // Guards: REQ-34
  test("shareHref：相對與絕對網址都認，別的路徑不認", () => {
    expect(shareHref("/share/01ABC-a.png")).toBe("/share/01ABC-a.png");
    expect(shareHref("https://hub.example.ts.net/share/01ABC-a.png")).toBe("/share/01ABC-a.png");
    expect(shareHref("/share/")).toBeNull();
    expect(shareHref("/other/share/a.png")).toBeNull();
    expect(shareHref("https://example.com/a.png")).toBeNull();
    expect(shareHref(undefined)).toBeNull();
  });

  // Guards: REQ-34
  test("圖片連結（Markdown 連結、圖片語法、裸網址）都顯示縮圖，點了開新分頁", () => {
    render(
      <Markdown>
        {
          "[圖一](/share/01AAAAAAAAAAAAAAAAAAAAAAAA-one.png)\n\n![圖二](/share/two.JPG)\n\n看這張 /share/three.webp 好了"
        }
      </Markdown>,
    );
    const images = screen.getAllByRole("img");
    expect(images.map((i) => i.getAttribute("src"))).toEqual([
      "/share/01AAAAAAAAAAAAAAAAAAAAAAAA-one.png",
      "/share/two.JPG",
      "/share/three.webp",
    ]);
    for (const img of images) {
      expect(img.getAttribute("loading")).toBe("lazy");
      // 不開新分頁（主畫面的看板沒有分頁列，開了退不回來）：縮圖是按鈕，點了在這頁看大圖。
      expect(img.closest("a")).toBeNull();
      expect(img.closest("button")).not.toBeNull();
    }
  });

  // 點縮圖在這頁疊一層看大圖，右上角 X 關掉（WEB-132）。
  test("點分享的圖開大圖，按關閉回到原頁", async () => {
    render(<Markdown>{"![圖](/share/01AAAAAAAAAAAAAAAAAAAAAAAA-one.png)"}</Markdown>);
    expect(screen.queryByTestId("media-viewer")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "看大圖：one.png" }));
    const viewer = await screen.findByTestId("media-viewer");
    expect(within(viewer).getByRole("img").getAttribute("src")).toBe(
      "/share/01AAAAAAAAAAAAAAAAAAAAAAAA-one.png",
    );
    fireEvent.click(within(viewer).getByRole("button", { name: "關閉" }));
    await waitFor(() => expect(screen.queryByTestId("media-viewer")).toBeNull());
  });

  // 點大圖本身不關；點圖外面的暗處關掉（WEB-132）。
  test("點大圖本身不關，點背景關掉", async () => {
    render(<Markdown>{"![圖](/share/two.png)"}</Markdown>);
    fireEvent.click(screen.getByRole("button", { name: "看大圖：two.png" }));
    const viewer = await screen.findByTestId("media-viewer");
    fireEvent.click(within(viewer).getByRole("img"));
    expect(screen.queryByTestId("media-viewer")).not.toBeNull();
    fireEvent.click(viewer);
    await waitFor(() => expect(screen.queryByTestId("media-viewer")).toBeNull());
  });

  // Guards: REQ-34
  test("svg 與其他檔案一律是下載 chip（顯示原檔名，不顯示縮圖）", () => {
    render(
      <Markdown>{"[報告](/share/01AAAAAAAAAAAAAAAAAAAAAAAA-report.pdf)\n\n/share/x.svg"}</Markdown>,
    );
    expect(screen.queryByRole("img")).toBeNull();
    const pdf = screen.getByRole("link", { name: "report.pdf" });
    expect(pdf.getAttribute("href")).toBe("/share/01AAAAAAAAAAAAAAAAAAAAAAAA-report.pdf");
    expect(screen.getByRole("link", { name: "x.svg" })).toBeTruthy();
  });

  // Guards: REQ-34
  test("程式碼裡的 /share 不變成連結；一般連結開新分頁；raw HTML 不執行", () => {
    const { container } = render(
      <Markdown>
        {"`/share/code.png`\n\n[外站](https://example.com)\n\n<script>window.x=1</script><b>粗</b>"}
      </Markdown>,
    );
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.getByText("/share/code.png").closest("a")).toBeNull();
    const out = screen.getByRole("link", { name: "外站" });
    expect(out.getAttribute("target")).toBe("_blank");
    expect(out.getAttribute("rel")).toBe("noopener noreferrer");
    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector("b")).toBeNull();
  });

  // Guards: REQ-156
  test("分享的影片（圖片語法與裸網址）直接變 <video>，下面的原檔名點了在這頁放大看；外部網址的 mp4 不載入", async () => {
    const { container } = render(
      <Markdown>
        {
          "![片](/share/01AAAAAAAAAAAAAAAAAAAAAAAA-clip.mp4)\n\n看 /share/b.webm 和 /share/c.MOV\n\n![外](https://example.com/x.mp4) [外連](https://example.com/y.mp4)"
        }
      </Markdown>,
    );
    const videos = [...container.querySelectorAll("video")];
    expect(videos.map((v) => v.getAttribute("src"))).toEqual([
      "/share/01AAAAAAAAAAAAAAAAAAAAAAAA-clip.mp4",
      "/share/b.webm",
      "/share/c.MOV",
    ]);
    for (const v of videos) {
      expect(v.hasAttribute("controls")).toBe(true);
      expect(v.getAttribute("preload")).toBe("metadata");
      expect(v.hasAttribute("playsinline")).toBe(true);
    }
    // 外部網址：不載入影片，只留一般連結（開新分頁）。
    expect(container.querySelector('video[src*="example.com"]')).toBeNull();
    expect(screen.getByRole("link", { name: "外連" }).getAttribute("href")).toBe(
      "https://example.com/y.mp4",
    );
    // 不開新分頁（FEN-30）：原檔名是按鈕，點了在這頁放大看那支影片。
    expect(screen.queryByRole("link", { name: "clip.mp4" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "放大看：clip.mp4" }));
    const viewer = await screen.findByTestId("media-viewer");
    expect(within(viewer).getByTestId("media-viewer-video").getAttribute("src")).toBe(
      "/share/01AAAAAAAAAAAAAAAAAAAAAAAA-clip.mp4",
    );
  });

  // 看板連多台（S3-6）：別台對話裡的附件要開到那台，不是這個看板自己的 /share。
  test("給了 shareBase：縮圖、影片、下載 chip 都補成那台的網址；不給維持相對網址", () => {
    const text =
      "![圖](/share/a.png)\n\n/share/b.mp4\n\n[檔](/share/c.txt)\n\n[外連](https://example.com/x)";
    const { unmount } = render(<Markdown shareBase="https://office.example">{text}</Markdown>);
    expect(screen.getByRole("img", { name: "a.png" }).getAttribute("src")).toBe(
      "https://office.example/share/a.png",
    );
    expect(screen.getByLabelText("b.mp4").getAttribute("src")).toBe(
      "https://office.example/share/b.mp4",
    );
    expect(screen.getByRole("link", { name: "c.txt" }).getAttribute("href")).toBe(
      "https://office.example/share/c.txt",
    );
    expect(screen.getByRole("link", { name: "外連" }).getAttribute("href")).toBe(
      "https://example.com/x",
    );
    unmount();
    render(<Markdown>{text}</Markdown>);
    expect(screen.getByRole("img", { name: "a.png" }).getAttribute("src")).toBe("/share/a.png");
  });
});

// WEB-120：手機上表格旁邊有一欄很長時，短欄位不被擠成一字一行——每格都有最小寬度，表格本身可以橫向捲。
describe("Markdown 的表格", () => {
  test("每一格都有最小寬度，表格包在可以橫向捲的框裡", () => {
    render(<Markdown>{"| 項目 | 備註 |\n|---|---|\n| 蘋果 | 很長很長的備註 |"}</Markdown>);
    const cells = [...screen.getAllByRole("columnheader"), ...screen.getAllByRole("cell")];
    expect(cells).toHaveLength(4);
    for (const c of cells) expect(c.className).toContain("min-w-[4.5em]");
    expect(screen.getByRole("table").parentElement?.className).toContain("overflow-x-auto");
  });
});
