import { describe, expect, test } from "vitest";
import {
  alignRuns,
  draftCard,
  isUnsupportedReason,
  parseToneMarkdown,
  togglePick,
  toneButton,
} from "./toneLogic";

const run = (replies: Record<string, string>) =>
  `# 名字 說話測試（2026-10-26 10:00）\n\n- 卡片：x\n\n${Object.entries(replies)
    .map(([id, r]) => `## ${id}｜標題${id}\n\n> 你好\n> 第二行\n\n${r}\n`)
    .join("\n")}`;

describe("parseToneMarkdown", () => {
  // Guards: REQ-130
  test("TestToneMarkdownSections：拆出檔頭、情境、引用的訊息與回覆", () => {
    const { head, sections } = parseToneMarkdown(
      run({ a: "回覆甲\n多行", b: "（這則沒跑成：逾時）" }),
    );
    expect(head).toContain("說話測試");
    expect(sections.map((s) => s.id)).toEqual(["a", "b"]);
    expect(sections[0]).toMatchObject({
      title: "標題a",
      quote: "你好\n第二行",
      reply: "回覆甲\n多行",
    });
    expect(sections[1]?.reply).toBe("（這則沒跑成：逾時）");
  });
  // Guards: REQ-130
  test("TestToneMarkdownRunning：沒有情境的檔（測試中）只有檔頭", () => {
    const { sections } = parseToneMarkdown("# x\n\n（測試進行中，跑完會出現在這裡）");
    expect(sections).toEqual([]);
  });
});

describe("alignRuns", () => {
  // Guards: REQ-130
  test("TestToneCompareAlign：依情境 id 對齊，缺的那邊是 undefined，只有右邊有的接在後面", () => {
    const l = parseToneMarkdown(run({ a: "左a", b: "左b" })).sections;
    const r = parseToneMarkdown(run({ b: "右b", c: "右c" })).sections;
    const rows = alignRuns(l, r);
    expect(rows.map((x) => [x.id, x.left, x.right])).toEqual([
      ["a", "左a", undefined],
      ["b", "左b", "右b"],
      ["c", undefined, "右c"],
    ]);
    expect(rows[0]?.quote).toBe("你好\n第二行");
  });
});

describe("helpers", () => {
  // Guards: REQ-130
  test("TestTonePicks：最多勾兩份，第三份換掉最早勾的，再點一次取消", () => {
    expect(togglePick([], "1")).toEqual(["1"]);
    expect(togglePick(["1", "2"], "3")).toEqual(["2", "3"]);
    expect(togglePick(["1", "2"], "1")).toEqual(["2"]);
  });
  // Guards: REQ-130
  test("TestToneButtonState：不支援停用、在跑顯示測試中、其他可按", () => {
    expect(toneButton({ runs: [], starting: false, unsupported: false })).toBe("ready");
    expect(toneButton({ runs: [{ status: "done" }], starting: false, unsupported: false })).toBe(
      "ready",
    );
    expect(toneButton({ runs: [{ status: "running" }], starting: false, unsupported: false })).toBe(
      "running",
    );
    expect(toneButton({ runs: [], starting: true, unsupported: false })).toBe("running");
    expect(toneButton({ runs: [], starting: false, unsupported: true })).toBe("unsupported");
    expect(isUnsupportedReason("這個引擎不支援一次性對話")).toBe(true);
    expect(isUnsupportedReason("回覆逾時")).toBe(false);
  });
  // Guards: REQ-130
  test("TestToneDraftCard：草稿帶檔頭，名字用 JSON 字串引號", () => {
    expect(draftCard(' 新"名 ', "內文")).toBe('---\nname: "新\\"名"\n---\n內文');
  });
});
