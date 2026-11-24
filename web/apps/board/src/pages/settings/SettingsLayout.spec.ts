import { describe, expect, test } from "vitest";
import { type SettingsTab, settingsCategories } from "./SettingsLayout";

const all: SettingsTab[] = [
  "notify",
  "display",
  "trust",
  "devices",
  "phone",
  "menu",
  "packages",
  "agents",
  "rules",
  "machine",
  "setup",
  "health",
  "log",
];

describe("設定分頁的分類", () => {
  // Guards: REQ-237
  test("十三個分頁各出現一次（手機連線 DEV-153）（角色庫搬到「角色」頁，WEB-136）；常用的在前面，套件、電腦、系統健康、紀錄收在「進階」", () => {
    const listed = settingsCategories.flatMap((c) => c.tabs);
    expect([...listed].sort()).toEqual([...all].sort());
    expect(settingsCategories.map((c) => c.label)).toEqual([
      "settings.cat.basic",
      "settings.cat.agents",
      "settings.cat.advanced",
    ]);
    expect(settingsCategories[2]?.tabs).toEqual(["packages", "machine", "health", "log"]);
  });
});
