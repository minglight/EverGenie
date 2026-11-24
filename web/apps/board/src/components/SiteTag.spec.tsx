import { describe, expect, test } from "vitest";
import { SITE_TONES, siteColor, siteDetail, siteName, siteToneIndex } from "./SiteTag";

// 看板連多台的台名標籤（S3-6；計畫設計決定 9：每台顏色可選，沒選照電腦代號給預設色）。

const ref = (over: Partial<{ machine: string; key: string; base: string; color: string }>) => ({
  machine: "",
  key: "",
  base: "",
  color: "",
  ...over,
});

describe("台的顏色與名字", () => {
  test("TestSiteDefaultColorFollowsMachine：沒設顏色時照電腦代號挑預設色，同一台每次都一樣", () => {
    const office = siteToneIndex(ref({ machine: "office" }));
    expect(office).toBeGreaterThanOrEqual(1);
    expect(office).toBeLessThanOrEqual(SITE_TONES);
    expect(siteToneIndex(ref({ machine: "office", key: "別的 key" }))).toBe(office);
    expect(siteColor(ref({ machine: "office" }))).toBe(`var(--site-${office})`);
    // mini 與 office 兩台的預設色不一樣。
    expect(siteToneIndex(ref({ machine: "mini" }))).not.toBe(office);
  });

  test("TestSiteConfiguredColorWins：[[web.sites]] 設了 color 就用它", () => {
    expect(siteColor(ref({ machine: "office", color: "#7c5cff" }))).toBe("#7c5cff");
  });

  test("TestSiteNameFallsBackToHost：讀不到台名時用網址的主機名", () => {
    expect(siteName({ label: "amy-office", base: "https://x.example" })).toBe("amy-office");
    expect(siteName({ label: "", base: "https://office.example:8443" })).toBe(
      "office.example:8443",
    );
  });

  test("主伺服器還沒讀到台名時叫「這台」（S3-7b 的清單標籤）", () => {
    expect(siteName({ label: "", base: "", primary: true })).toBe("這台");
    expect(siteName({ label: "amy-mini", base: "", primary: true })).toBe("amy-mini");
  });

  test("TestSiteDetailEnvAndVersion：正式／測試與版本，讀不到的那段不寫", () => {
    expect(siteDetail({ env: "prod", version: "1.2.3" })).toBe("正式 · 版本 1.2.3");
    expect(siteDetail({ env: "dev", version: "" })).toBe("測試");
    expect(siteDetail({ env: "", version: "" })).toBe("");
  });
});
