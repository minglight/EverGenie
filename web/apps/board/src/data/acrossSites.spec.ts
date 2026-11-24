import { describe, expect, test } from "vitest";
import {
  failedSites,
  flattenParts,
  itemKey,
  mergeCounts,
  type SitePart,
  sumParts,
  usableSites,
} from "./acrossSites";
import type { Site } from "./sites";

const site = (key: string, over: Partial<Site> = {}): Site => ({
  key,
  base: key ? `https://${key}.example.ts.net` : "",
  primary: key === "",
  label: key,
  machine: "",
  env: "prod",
  version: "",
  boardApi: 1,
  publicUrl: "",
  color: "",
  state: "ok",
  reason: "",
  ...over,
});

const part = <T>(s: Site, data: T | undefined, isError = false): SitePart<T> => ({
  site: s,
  data,
  isError,
  refetch: () => {},
});

describe("合併好幾台的清單", () => {
  test("同一個單號在兩台不撞：主伺服器就是單號，別台加台名", () => {
    expect(itemKey(site(""), "SEC-1")).toBe("SEC-1");
    expect(itemKey(site("amy-office"), "SEC-1")).toBe("amy-office/SEC-1");
  });

  test("只抓主伺服器和連得上的別台：連不上、被拒、太舊、試連中的都不抓", () => {
    const sites = [
      site(""),
      site("ok"),
      site("down", { state: "down" }),
      site("denied", { state: "denied" }),
      site("old", { state: "old" }),
      site("checking", { state: "checking" }),
    ];
    expect(usableSites(sites).map((s) => s.key)).toEqual(["", "ok"]);
    // 主伺服器自己的狀態不看（它讀不到照舊由頁面顯示載入失敗）。
    expect(usableSites([site("", { state: "down" })]).map((s) => s.key)).toEqual([""]);
  });

  test("接成一份時每筆記得自己是哪台的；還沒抓到的台略過", () => {
    const rows = flattenParts([
      part(site(""), ["x", "y"]),
      part(site("b"), ["x"]),
      part<string[]>(site("c"), undefined),
    ]);
    expect(rows.map((r) => `${r.site.key}:${r.item}`)).toEqual([":x", ":y", "b:x"]);
  });

  test("數字加總抓到的台；一台都還沒抓到時是 undefined（分段上不畫數字）", () => {
    expect(sumParts([part(site(""), [1, 2]), part(site("b"), [3])], (d) => d.length)).toBe(3);
    expect(
      sumParts([part(site(""), [1]), part<number[]>(site("b"), undefined)], (d) => d.length),
    ).toBe(1);
    expect(sumParts([part<number[]>(site(""), undefined)], (d) => d.length)).toBeUndefined();
  });

  test("每台的秘書計數合在一起（key 已帶台名，同名職位不會加在一起）", () => {
    expect(mergeCounts([{ code: 2 }, { "b/code": 1 }, { code: 1 }])).toEqual({
      code: 3,
      "b/code": 1,
    });
  });
});

describe("failedSites：哪幾台連得上但資料讀不到", () => {
  test("只算別台、而且讀失敗又沒有舊資料的；主伺服器自己讀不到由頁面顯示", () => {
    const parts = [
      part(site(""), undefined, true),
      part(site("amy-office"), undefined, true),
      part(site("family"), [1], true),
      part(site("dev"), [1]),
    ];
    expect(failedSites(parts)).toEqual(["amy-office"]);
  });
});
