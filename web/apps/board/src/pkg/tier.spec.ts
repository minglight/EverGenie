import { describe, expect, test } from "vitest";
import { PACKAGE_ROUTE, packagePageRoute } from "./routes";
import { shortSource, tierLabel } from "./TierBadge";

describe("套件層級的標籤（tier）", () => {
  // Guards: REQ-350
  test("TestTierLabel：官方不標、自己的「我的」、分享的有來源寫來源", () => {
    expect(tierLabel("official")).toBe("");
    expect(tierLabel(undefined)).toBe("");
    expect(tierLabel("mine")).toBe("我的");
    expect(tierLabel("installed")).toBe("分享的");
    expect(tierLabel("installed", "https://github.com/friend/pond.git")).toBe(
      "分享自 github.com/friend/pond",
    );
  });

  // Guards: REQ-350
  test("TestShortSource：去掉協定、帳號、.git 與結尾斜線", () => {
    expect(shortSource("https://github.com/friend/pond.git")).toBe("github.com/friend/pond");
    expect(shortSource("git@github.com:friend/pond.git")).toBe("github.com/friend/pond");
    expect(shortSource("ssh://git@example.com:2222/a/b/")).toBe("example.com:2222/a/b");
    expect(shortSource("file:///Users/x/garden")).toBe("/Users/x/garden");
  });
});

// 前端原始碼（不含測試）只有 pkg/routes.ts 能寫套件網址的字串；其他地方一律呼叫 packagePageRoute（REQ-351）。
const sources = import.meta.glob<string>(["../**/*.{ts,tsx}", "!../**/*.spec.{ts,tsx}"], {
  query: "?raw",
  import: "default",
  eager: true,
});

describe("套件網址只在一個地方組（tier）", () => {
  // Guards: REQ-351
  test("TestPackageRouteOnlyInRoutes：其他檔案沒有手寫 /p/ 網址", () => {
    expect(Object.keys(sources).length).toBeGreaterThan(20);
    const literal = /["'`]\/p\//;
    const comment = /^\s*(\/\/|\/?\*)/;
    // glob 的鍵相對這個檔案：同資料夾的 routes.ts 是 "./routes.ts"。
    const offenders = Object.entries(sources)
      .filter(([path]) => path !== "./routes.ts")
      .filter(([, text]) =>
        text.split("\n").some((line) => line.search(comment) < 0 && line.search(literal) >= 0),
      )
      .map(([path]) => path);
    expect(offenders).toEqual([]);
  });

  // Guards: REQ-351
  test("TestPackagePageRoute：跟核心同一條規則 /p/<套件>/<頁面>", () => {
    expect(packagePageRoute("garden", "index")).toBe("/p/garden/index");
    expect(PACKAGE_ROUTE).toBe("/p/$pkg/$page");
  });
});
