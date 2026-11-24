import { beforeAll, describe, expect, test } from "vitest";
import { parseWhen } from "./when";

// 時區固定在 Asia/Taipei（補充契約 S5 第 6 點）；一定要在建立任何 Date 之前設。
// 沒有裝 @types/node（FEN-1 的套件清單裡沒有）：只宣告這一個用到的全域。
declare const process: { env: Record<string, string | undefined> };
process.env.TZ = "Asia/Taipei";

// 2026-10-24（六）15:30，跟計畫裡的範例同一天。
const now = new Date(2026, 9, 24, 15, 30, 0);

const at = (m: number, d: number, h: number, mi = 0, y = 2026) => new Date(y, m - 1, d, h, mi);

function read(input: string) {
  const out = parseWhen(input, now);
  return out ? { date: out.date.getTime(), rest: out.rest } : null;
}

beforeAll(() => {
  expect(new Date(2026, 0, 1).getTimezoneOffset()).toBe(-480);
});

describe("parseWhen：日期詞", () => {
  // Guards: REQ-59
  test("「明天 9 點 打給牙醫」：時間拿掉，剩下是內容", () => {
    expect(read("明天 9 點 打給牙醫")).toEqual({ date: at(10, 25, 9).getTime(), rest: "打給牙醫" });
  });

  // Guards: REQ-59
  test("沒有空白也行：「明天9點打給牙醫」", () => {
    expect(read("明天9點打給牙醫")).toEqual({ date: at(10, 25, 9).getTime(), rest: "打給牙醫" });
  });

  // Guards: REQ-59
  test("「明天 9」：日期詞後面的整數是幾點", () => {
    expect(read("明天 9 打給牙醫")).toEqual({ date: at(10, 25, 9).getTime(), rest: "打給牙醫" });
  });

  // Guards: REQ-59
  test("今天、後天、大後天", () => {
    expect(read("今天 18:00 取貨")?.date).toBe(at(10, 24, 18).getTime());
    expect(read("後天 8 點")?.date).toBe(at(10, 26, 8).getTime());
    expect(read("大後天 繳費")?.date).toBe(at(10, 27, 9).getTime());
  });

  // RMD-20：「昨天 3 點」是昨天，不是今天（或明天）的 03:00；「昨天」兩個字也從內容拿掉。
  test("昨天、前天、昨晚：照說的那天", () => {
    expect(read("昨天 3 點 繳費")).toEqual({ date: at(10, 23, 3).getTime(), rest: "繳費" });
    expect(read("昨天下午 3 點 繳費")?.date).toBe(at(10, 23, 15).getTime());
    expect(read("前天 繳費")?.date).toBe(at(10, 22, 9).getTime());
    expect(read("昨晚 倒垃圾")?.date).toBe(at(10, 23, 20).getTime());
    expect(read("查目前天氣")).toBeNull();
  });

  // Guards: REQ-59
  test("「今晚」沒說幾點是 20:00；「今晚 9 點半」是 21:30", () => {
    expect(read("今晚 看球")?.date).toBe(at(10, 24, 20).getTime());
    expect(read("今晚 9 點半 看球")?.date).toBe(at(10, 24, 21, 30).getTime());
    expect(read("今晚 9 看球")?.date).toBe(at(10, 24, 21).getTime());
  });

  // Guards: REQ-59
  test("「下週一」是下一個週一；「週三」是最近的週三；沒說時間是 09:00", () => {
    expect(read("下週一 10:30 開會")).toEqual({ date: at(10, 26, 10, 30).getTime(), rest: "開會" });
    expect(read("下週日 爬山")?.date).toBe(at(11, 1, 9).getTime());
    expect(read("週三 晚上 7 點 健身房")).toEqual({
      date: at(10, 28, 19).getTime(),
      rest: "健身房",
    });
    expect(read("星期五 聚餐")?.date).toBe(at(10, 30, 9).getTime());
  });

  // Guards: REQ-59
  test("說的是今天、沒說時間：9 點過了就用 20:00", () => {
    expect(read("週六 買菜")?.date).toBe(at(10, 24, 20).getTime());
    expect(read("今天 買菜")?.date).toBe(at(10, 24, 20).getTime());
  });
});

describe("parseWhen：時間與時段", () => {
  // Guards: REQ-59
  test("「9 點」「9:30」「9 點半」「9 點 30 分」過了就是明天", () => {
    expect(read("9 點 開會")?.date).toBe(at(10, 25, 9).getTime());
    expect(read("9:30 站會")).toEqual({ date: at(10, 25, 9, 30).getTime(), rest: "站會" });
    expect(read("9 點半 站會")?.date).toBe(at(10, 25, 9, 30).getTime());
    expect(read("9 點 30 分 站會")?.date).toBe(at(10, 25, 9, 30).getTime());
    expect(read("17:45 下班")?.date).toBe(at(10, 24, 17, 45).getTime());
  });

  // Guards: REQ-59
  test("早上／下午／晚上／中午會調整 12 小時制", () => {
    expect(read("明天 早上 8 點")?.date).toBe(at(10, 25, 8).getTime());
    expect(read("明天 下午 3 點")?.date).toBe(at(10, 25, 15).getTime());
    expect(read("明天晚上 7 點")?.date).toBe(at(10, 25, 19).getTime());
    expect(read("明天 中午 1 點")?.date).toBe(at(10, 25, 13).getTime());
    expect(read("下午 4 點 接小孩")).toEqual({ date: at(10, 24, 16).getTime(), rest: "接小孩" });
  });

  // Guards: REQ-59
  test("只說時段：早上 09:00、下午 15:00、晚上 20:00", () => {
    expect(read("明天早上")?.date).toBe(at(10, 25, 9).getTime());
    expect(read("明天下午 開會")?.date).toBe(at(10, 25, 15).getTime());
    expect(read("晚上 看電影")?.date).toBe(at(10, 24, 20).getTime());
    expect(read("早上 運動")?.date).toBe(at(10, 25, 9).getTime());
  });
});

describe("parseWhen：相對時間", () => {
  // Guards: REQ-59
  test("「3h」「3 小時後」「30m」「30 分鐘後」「2 天後」", () => {
    expect(read("3h 取包裹")).toEqual({ date: at(10, 24, 18, 30).getTime(), rest: "取包裹" });
    expect(read("3 小時後 取包裹")).toEqual({ date: at(10, 24, 18, 30).getTime(), rest: "取包裹" });
    expect(read("取包裹 30m")).toEqual({ date: at(10, 24, 16).getTime(), rest: "取包裹" });
    expect(read("30 分鐘後 關火")).toEqual({ date: at(10, 24, 16).getTime(), rest: "關火" });
    expect(read("2 天後 退租")?.date).toBe(at(10, 26, 15, 30).getTime());
    expect(read("半小時後 關火")?.date).toBe(at(10, 24, 16).getTime());
  });
});

describe("parseWhen：月／日", () => {
  // Guards: REQ-59
  test("「10/28」「10/28 14:00」「10月28日 9點」", () => {
    expect(read("10/28 打給牙醫")).toEqual({ date: at(10, 28, 9).getTime(), rest: "打給牙醫" });
    expect(read("10/28 14:00 開會")).toEqual({ date: at(10, 28, 14).getTime(), rest: "開會" });
    expect(read("10月28日 9點 報稅")).toEqual({ date: at(10, 28, 9).getTime(), rest: "報稅" });
  });

  // Guards: REQ-59
  test("今年已經過的日子是明年；完整日期照寫", () => {
    expect(read("1/5 繳費")?.date).toBe(at(1, 5, 9, 0, 2027).getTime());
    expect(read("2026/11/3 14:00 面談")?.date).toBe(at(11, 3, 14).getTime());
  });
});

describe("parseWhen：解析不到就不動句子", () => {
  // Guards: REQ-59
  test("沒有時間詞、數量、不合理的數字都回 null", () => {
    expect(parseWhen("買牛奶", now)).toBeNull();
    expect(parseWhen("買 3 顆蛋", now)).toBeNull();
    expect(parseWhen("下午茶訂位", now)).toBeNull();
    expect(parseWhen("25 點 開會", now)).toBeNull();
    expect(parseWhen("13/45 開會", now)).toBeNull();
    expect(parseWhen("   ", now)).toBeNull();
  });

  // Guards: REQ-59
  test("夾在英數字中間的 h／m 不是時間（例：編號 6h0joc、834m9n）", () => {
    expect(parseWhen("買牛奶 6h0joc", now)).toBeNull();
    expect(parseWhen("買牛奶 834m9n", now)).toBeNull();
    expect(parseWhen("型號 4h1q8e", now)).toBeNull();
    expect(parseWhen("3h 後打給他", now)?.matched).toBe("3h");
  });

  // Guards: REQ-59
  test("日期詞後面的數字如果是數量，不當成幾點", () => {
    expect(read("明天 2 個人吃飯")).toEqual({ date: at(10, 25, 9).getTime(), rest: "2 個人吃飯" });
  });
});
