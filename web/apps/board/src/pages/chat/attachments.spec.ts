import { describe, expect, test } from "vitest";
import { splitAttachments } from "./attachments";

describe("splitAttachments", () => {
  // Guards: REQ-82
  test("文字之外的附件行被抽出來，順序不變", () => {
    const got = splitAttachments(
      "這是午餐\n第二行\n![a.png](/share/1-a.png)\n[報告.pdf](/share/2-報告.pdf)",
    );
    expect(got.text).toBe("這是午餐\n第二行");
    expect(got.attachments).toEqual(["![a.png](/share/1-a.png)", "[報告.pdf](/share/2-報告.pdf)"]);
  });

  // Guards: REQ-82
  test("只有附件、或夾在句子裡的連結：前者全部抽出，後者留在文字", () => {
    expect(splitAttachments("![a.png](/share/a.png)")).toEqual({
      text: "",
      attachments: ["![a.png](/share/a.png)"],
    });
    const inline = "看這個 [a](/share/a.png) 好嗎";
    expect(splitAttachments(inline)).toEqual({ text: inline, attachments: [] });
    const external = "![x](https://example.com/x.png)";
    expect(splitAttachments(external)).toEqual({ text: external, attachments: [] });
  });
});
