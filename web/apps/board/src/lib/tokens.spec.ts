import css from "@evergenie/ui/tokens.css?raw";
import { describe, expect, test } from "vitest";

// 狀態顏色（WEB-70）：亮色與暗色都要分得出橘（要你回覆）與紅（出錯：沒送到、這一輪沒做完），也分得出藍（處理中）。
// 直接讀 @evergenie/ui 的 tokens.css：亮色是 :root，暗色是 [data-theme="dark"]（跟著系統的那一段值一樣）。

function block(selector: string): string {
  const start = css.indexOf(`${selector} {`);
  if (start < 0) throw new Error(`找不到 ${selector}`);
  return css.slice(start, css.indexOf("}", start));
}

function token(body: string, name: string, fallback?: string): string {
  const m = body.match(new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})`));
  if (m?.[1]) return m[1];
  if (fallback) return fallback;
  throw new Error(`沒有 --${name}`);
}

function hue(hex: string): number {
  const ch = (i: number) => Number.parseInt(hex.slice(i, i + 2), 16) / 255;
  const [r, g, b] = [ch(1), ch(3), ch(5)];
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const d = max - min;
  if (d === 0) return 0;
  let h: number;
  if (max === r) h = ((g - b) / d) % 6;
  else if (max === g) h = (b - r) / d + 2;
  else h = (r - g) / d + 4;
  return (h * 60 + 360) % 360;
}

function hueGap(a: string, b: string): number {
  const d = Math.abs(hue(a) - hue(b));
  return Math.min(d, 360 - d);
}

const light = block(":root");
const dark = block('[data-theme="dark"]');
const darkSystem = block(':root:not([data-theme="light"])');

describe("狀態顏色在亮色、暗色都分得出來（WEB-70、UXC-6）", () => {
  // Guards: REQ-382
  test("暗色跟著系統的那一段與強制暗色的狀態色一樣", () => {
    for (const name of ["attention-fg", "busy", "ok", "danger"]) {
      expect(token(darkSystem, name), name).toBe(token(dark, name));
    }
  });

  // Guards: REQ-382
  test("橘與紅的色相差至少 30 度（點、字，亮色與暗色）", () => {
    for (const [mode, body] of [
      ["light", light],
      ["dark", dark],
    ] as const) {
      const dot = token(body, "attention", token(light, "attention"));
      const fg = token(body, "attention-fg");
      const red = token(body, "danger");
      expect(hueGap(dot, red), `${mode} 點`).toBeGreaterThanOrEqual(30);
      expect(hueGap(fg, red), `${mode} 字`).toBeGreaterThanOrEqual(30);
      // 藍跟橘、紅、綠都差很多。
      const blue = token(body, "busy");
      for (const other of [fg, red, token(body, "ok")]) {
        expect(hueGap(blue, other), `${mode} 藍`).toBeGreaterThanOrEqual(60);
      }
    }
  });
});

// 看板連多台（S3-6）：每台的預設色亮暗都要有，而且不能跟「要你回覆」的橘搞混（WEB-70）。
describe("台的預設色（--site-1…6）", () => {
  test("亮色、暗色（兩段）都有六個，暗色兩段一樣，跟橘至少差 30 度", () => {
    for (let i = 1; i <= 6; i++) {
      const name = `site-${i}`;
      expect(token(darkSystem, name), name).toBe(token(dark, name));
      for (const [mode, body] of [
        ["light", light],
        ["dark", dark],
      ] as const) {
        const orange = token(body, "attention", token(light, "attention"));
        expect(hueGap(token(body, name), orange), `${mode} ${name}`).toBeGreaterThanOrEqual(30);
      }
    }
  });
});

// DEV-156 U-26：三處原本不到 4.5:1 的字（WCAG 1.4.3）——深色的頭像縮寫、分段鈕上數量「0」、停用的推播按鈕。
// 這裡照 tokens.css 的實際顏色算，token 改了對比掉下去就會紅。
function luminance(hex: string): number {
  const ch = (i: number) => {
    const c = Number.parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * ch(1) + 0.7152 * ch(3) + 0.0722 * ch(5);
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

/** color-mix(in srgb, <hex> p%, #000)。 */
function withBlack(hex: string, percent: number): string {
  const ch = (i: number) =>
    Math.round((Number.parseInt(hex.slice(i, i + 2), 16) * percent) / 100)
      .toString(16)
      .padStart(2, "0");
  return `#${ch(1)}${ch(3)}${ch(5)}`;
}

describe("字的對比至少 4.5:1（DEV-156 U-26）", () => {
  test("深色的頭像縮寫：白字在混了黑的底色上（預設色 muted）", () => {
    const m = css.match(/\[data-theme="dark"\] \.hg-agent \{[^}]*?(\d+)%,\s*#000/);
    const percent = Number(m?.[1]);
    expect(percent).toBeGreaterThan(0);
    // 跟著系統的那一段混的比例要一樣。
    expect(css.match(/(\d+)%,\s*#000/g)?.every((x) => x.startsWith(`${percent}%`))).toBe(true);
    const bg = withBlack(token(dark, "muted"), percent);
    expect(contrast("#ffffff", bg)).toBeGreaterThanOrEqual(4.5);
    // 亮色的預設底色本來就夠。
    expect(contrast("#ffffff", token(light, "muted"))).toBeGreaterThanOrEqual(4.5);
  });

  test("分段鈕上的數量 0、停用的推播按鈕：灰字（muted）在分段鈕的兩種底色上", () => {
    for (const [mode, body] of [
      ["light", light],
      ["dark", dark],
    ] as const) {
      const muted = token(body, "muted");
      expect(contrast(muted, token(body, "surface-2")), `${mode} 底`).toBeGreaterThanOrEqual(4.5);
      expect(contrast(muted, token(body, "segment-on")), `${mode} 選到`).toBeGreaterThanOrEqual(
        4.5,
      );
    }
  });
});
