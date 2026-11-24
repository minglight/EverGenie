import { describe, expect, it } from "vitest";
import { canCollapse, clampY, nextHidden } from "./hideOnScroll";

describe("nextHidden（手機篩選列往下捲收起）", () => {
  it("往下捲夠多就收起、往上捲夠多就出現", () => {
    expect(nextHidden(false, 100, 140)).toBe(true);
    expect(nextHidden(true, 140, 100)).toBe(false);
  });
  it("抖動幾 px 不換", () => {
    expect(nextHidden(true, 200, 204)).toBe(true);
    expect(nextHidden(false, 200, 196)).toBe(false);
  });
  it("回到頂端附近一律出現", () => {
    expect(nextHidden(true, 30, 10)).toBe(false);
    expect(nextHidden(true, 0, 20)).toBe(false);
  });
});

describe("canCollapse（收起後範圍還夠才收，DEV-111）", () => {
  it("內容只多一點點（範圍 150、頭像列 90）不收：收了 scrollTop 會被夾回去", () => {
    expect(canCollapse(150, 100, 90)).toBe(false);
  });
  it("收起後位置還在新範圍內、留一點空才收", () => {
    expect(canCollapse(400, 100, 90)).toBe(true);
    expect(canCollapse(400, 300, 90)).toBe(false);
  });
});

describe("clampY（回彈不算往上捲）", () => {
  it("超出頂端、底部的 scrollTop 夾回範圍內", () => {
    expect(clampY(-30, 200)).toBe(0);
    expect(clampY(240, 200)).toBe(200);
    expect(clampY(80, 200)).toBe(80);
  });
});
