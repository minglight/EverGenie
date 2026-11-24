import { describe, expect, test } from "vitest";
import { attachmentName, isImageName, namedFile } from "./attachments";

describe("任務單附件的名字（TSK-28）", () => {
  test("去掉看板上傳與 evergenie share 加的字首，留原檔名", () => {
    expect(attachmentName("/share/20261103-101530-a1b2-截圖.png")).toBe("截圖.png");
    expect(attachmentName("/share/01JC2X7Q3N4M5P6R7S8T9V0W1X-report.pdf")).toBe("report.pdf");
    expect(attachmentName("https://x.example.ts.net/share/20261103-101530-a1b2-a%20b.png")).toBe(
      "a b.png",
    );
  });

  test("沒有字首或解不開的照原字", () => {
    expect(attachmentName("/share/plain.txt")).toBe("plain.txt");
    expect(attachmentName("/share/bad%E0.txt")).toBe("bad%E0.txt");
    // 只有字首、沒有原檔名：不要變成空字串
    expect(attachmentName("/share/20261103-101530-a1b2-")).toBe("20261103-101530-a1b2-");
  });

  test("照副檔名判斷圖片", () => {
    expect(isImageName("a.PNG")).toBe(true);
    expect(isImageName("b.jpeg")).toBe(true);
    expect(isImageName("c.webp")).toBe(true);
    expect(isImageName("d.pdf")).toBe(false);
    // 伺服器不當圖片送的格式（瀏覽器拿到下載檔）不畫縮圖。
    expect(isImageName("e.heic")).toBe(false);
    expect(isImageName("png")).toBe(false);
  });

  test("剪貼簿的 image.png 或沒名字的檔改名成 paste-<時間>", () => {
    const pasted = namedFile(new File(["x"], "image.png", { type: "image/png" }), 42);
    expect(pasted.name).toBe("paste-42.png");
    expect(pasted.type).toBe("image/png");
    expect(namedFile(new File(["x"], "", { type: "image/jpeg" }), 7).name).toBe("paste-7.jpeg");
    const named = new File(["x"], "截圖.png", { type: "image/png" });
    expect(namedFile(named)).toBe(named);
  });
});
