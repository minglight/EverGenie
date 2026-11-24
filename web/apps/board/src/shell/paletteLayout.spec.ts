import { describe, expect, it } from "vitest";
import { paletteListMaxHeight, paletteTop } from "./paletteLayout";

describe("paletteListMaxHeight", () => {
  // WEB-124：手機鍵盤打開時（390×844 只剩約 470px 看得到），清單底不能落到鍵盤後面。
  it("fits the result list above the phone keyboard", () => {
    const visible = 470;
    const max = paletteListMaxHeight(visible) ?? 0;
    expect(12 + 50 + 16 + max).toBeLessThanOrEqual(visible);
  });

  it("keeps the 60% cap when the keyboard is closed", () => {
    expect(paletteListMaxHeight(844)).toBe(506);
  });

  it("still shows about three rows on a very short screen", () => {
    expect(paletteListMaxHeight(200)).toBe(132);
  });

  it("falls back to CSS when the height is unknown", () => {
    expect(paletteListMaxHeight(0)).toBeUndefined();
  });
});

describe("paletteTop", () => {
  // WEB-128：iOS 打開鍵盤把畫面往上推 300px 時，面板頂端跟著往下 300px，不跑出看得到的那塊。
  it("follows the visual viewport down when iOS pushes the page up", () => {
    expect(paletteTop(300)).toBe(312);
    expect(paletteTop(120.4)).toBe(132);
  });

  it("leaves the CSS top alone when the page is not pushed", () => {
    expect(paletteTop(0)).toBeUndefined();
    expect(paletteTop(Number.NaN)).toBeUndefined();
  });
});
