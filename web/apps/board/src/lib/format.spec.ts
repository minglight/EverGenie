import { describe, expect, test } from "vitest";
import {
  bytesText,
  clockText,
  dateLabel,
  dayGroupLabel,
  describeWhen,
  greetingPart,
  parseIso,
  preview,
  relativeAgo,
  timeLabel,
  timeMark,
  waitedFor,
  whenInputText,
} from "./format";

const now = new Date(2026, 9, 24, 15, 30, 0);
const ago = (ms: number) => new Date(now.getTime() - ms).toISOString();

describe("format", () => {
  // Guards: REQ-109
  test("relativeAgo 把時間差說成白話", () => {
    expect(relativeAgo(ago(20_000), now)).toBe("剛剛");
    expect(relativeAgo(ago(3 * 60_000), now)).toBe("3 分鐘前");
    expect(relativeAgo(ago(2 * 3600_000), now)).toBe("2 小時前");
    expect(relativeAgo(ago(3 * 86400_000), now)).toBe("3 天前");
    expect(relativeAgo(undefined, now)).toBe("");
  });

  // Guards: REQ-109
  test("waitedFor 說「等了多久」，沒有開始時間就用伺服器給的天數", () => {
    expect(waitedFor(ago(2 * 3600_000), now)).toBe("等了 2 小時");
    expect(waitedFor(undefined, now, 3)).toBe("等了 3 天");
    expect(waitedFor(undefined, now)).toBe("");
  });

  // Guards: REQ-109
  test("greetingPart 依時間分早安午安晚安", () => {
    expect(greetingPart(new Date(2026, 9, 24, 5, 0))).toBe("morning");
    expect(greetingPart(new Date(2026, 9, 24, 10, 59))).toBe("morning");
    expect(greetingPart(new Date(2026, 9, 24, 11, 0))).toBe("afternoon");
    expect(greetingPart(new Date(2026, 9, 24, 17, 59))).toBe("afternoon");
    expect(greetingPart(new Date(2026, 9, 24, 18, 0))).toBe("evening");
    expect(greetingPart(new Date(2026, 9, 24, 4, 59))).toBe("evening");
  });

  // Guards: REQ-109
  test("dateLabel 與 clockText 用瀏覽器時區", () => {
    expect(dateLabel(now)).toBe("10 月 24 日（六）");
    expect(clockText(new Date(2026, 9, 24, 9, 5).toISOString())).toBe("09:05");
    expect(clockText(undefined)).toBe("");
  });

  // Guards: REQ-109
  test("preview 截前 60 字", () => {
    expect(preview("短")).toBe("短");
    expect(preview("字".repeat(70))).toBe(`${"字".repeat(60)}…`);
  });

  // Guards: REQ-59
  test("伺服器的「沒有時間」（Go 的零值）當作沒有", () => {
    const zero = "0001-01-01T00:00:00Z";
    expect(parseIso(zero)).toBeNull();
    expect(clockText(zero)).toBe("");
    expect(relativeAgo(zero, now)).toBe("");
    expect(timeLabel(zero, now)).toBe("");
  });

  // Guards: REQ-59
  test("timeLabel：今天只寫時間，明天、一週內、更遠的日期各有寫法", () => {
    const at = (d: number, h: number, mi = 0) => new Date(2026, 9, d, h, mi).toISOString();
    expect(timeLabel(at(24, 9, 5), now)).toBe("09:05");
    expect(timeLabel(at(25, 9), now)).toBe("明天 09:00");
    expect(timeLabel(at(23, 18), now)).toBe("昨天 18:00");
    expect(timeLabel(at(28, 19), now)).toBe("週三 19:00");
    expect(timeLabel(at(31, 8), now)).toBe("10/31 08:00");
  });

  // Guards: REQ-59
  test("dayGroupLabel 與 describeWhen", () => {
    expect(dayGroupLabel(new Date(2026, 9, 24, 8), now)).toBe("今天");
    expect(dayGroupLabel(new Date(2026, 9, 25, 8), now)).toBe("明天");
    expect(dayGroupLabel(new Date(2026, 9, 28, 8), now)).toBe("10/28（三）");
    expect(describeWhen(new Date(2026, 9, 25, 9), now)).toBe("→ 10/25（日）09:00");
    expect(whenInputText(new Date(2026, 9, 5, 9, 3))).toBe("2026/10/5 09:03");
  });

  // Guards: REQ-157
  test("TestPreviewStripsMarkdown：預覽不露 Markdown 符號", () => {
    expect(preview("看 ![圖](https://x/a.png) 這張")).toBe("看 [圖片] 這張");
    expect(preview("見 [設計文件](https://x/doc) 了嗎")).toBe("見 設計文件 了嗎");
    expect(preview("跑 `make ci` 看看")).toBe("跑 make ci 看看");
    expect(preview("```ts\nconst a = 1\n```")).toBe("const a = 1");
    expect(preview("**重點** 和 __另一個__ 和 ~~刪掉~~")).toBe("重點 和 另一個 和 刪掉");
    expect(preview("這是 *強調* 和 _斜體_")).toBe("這是 強調 和 斜體");
    expect(preview("# 標題\n> 引用\n- 項目\n* 星號項目\n1. 第一點")).toBe(
      "標題 引用 項目 星號項目 第一點",
    );
    expect(preview("  多   個\n\n空白  ")).toBe("多 個 空白");
    expect(preview("3 * 4 = 12")).toBe("3 * 4 = 12");
    expect(preview("snake_case_name 不變")).toBe("snake_case_name 不變");
    expect(preview("收到：幫我整理 **這週的重")).toBe("收到：幫我整理 這週的重");
    expect(preview("**很長很長的重點**", 4)).toBe("很長很長…");
  });

  // Guards: REQ-157
  test("TestTimeMarkFormats：同一天、昨天、7 天內、同一年、更早各一種寫法", () => {
    const at = (y: number, m: number, d: number, h: number, mi = 0) =>
      new Date(y, m - 1, d, h, mi).toISOString();
    expect(timeMark(at(2026, 10, 24, 9, 5), now)).toBe("09:05");
    expect(timeMark(at(2026, 10, 23, 18), now)).toBe("昨天 18:00");
    expect(timeMark(at(2026, 10, 20, 9), now)).toBe("週二 09:00");
    expect(timeMark(at(2026, 10, 17, 9), now)).toBe("10/17 09:00");
    expect(timeMark(at(2026, 3, 2, 7, 30), now)).toBe("3/2 07:30");
    expect(timeMark(at(2025, 10, 2, 0, 0), now)).toBe("2025/10/2 00:00");
    expect(timeMark(undefined, now)).toBe("");
  });
});

describe("bytesText", () => {
  // Guards: REQ-111
  test("備份大小：B、KB、MB、GB", () => {
    expect(bytesText(0)).toBe("0 B");
    expect(bytesText(512)).toBe("512 B");
    expect(bytesText(1536)).toBe("1.5 KB");
    expect(bytesText(3.4 * 1024 * 1024)).toBe("3.4 MB");
    expect(bytesText(250 * 1024 * 1024)).toBe("250 MB");
    expect(bytesText(5 * 1024 ** 3)).toBe("5.0 GB");
  });
});
