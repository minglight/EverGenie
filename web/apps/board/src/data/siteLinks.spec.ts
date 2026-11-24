import { describe, expect, test } from "vitest";
import { canLinkDetail } from "./siteLinks";

describe("別台的一筆能不能連到詳情頁", () => {
  test("TestOtherSiteLinksOnlyToPagesThatReadHome：主伺服器都連；別台只連認得 ?home 的詳情頁（訊息、聊天、任務單、待辦、Goal），其他不連", () => {
    const primary = { key: "" };
    const other = { key: "amy-office" };
    const readsHome = [
      "/messages/$id",
      "/chat/$session",
      "/tasks/$id",
      "/reminders/$id",
      "/goals/$id",
    ];
    for (const to of [...readsHome, "/settings"]) {
      expect(canLinkDetail(primary, to)).toBe(true);
    }
    for (const to of readsHome) {
      expect(canLinkDetail(other, to)).toBe(true);
    }
    expect(canLinkDetail(other, "/settings")).toBe(false);
  });
});
