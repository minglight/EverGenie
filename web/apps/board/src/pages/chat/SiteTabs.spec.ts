import { describe, expect, test } from "vitest";
import type { Site } from "../../data/sites";
import type { ChatAgent } from "./chatAgents";
import { siteTabsFor } from "./SiteTabs";

const site = (key: string, state: Site["state"] = "ok") =>
  ({ key, primary: key === "", state }) as Site;
const agent = (s: Site, id: string, enabled = true) =>
  ({ id, key: s.key ? `${s.key}/${id}` : id, site: s, enabled, presence: "idle" }) as ChatAgent;

describe("切電腦的分頁（WEB-34）", () => {
  const a = site("");
  const b = site("office");

  test("她在兩台：兩個分頁，照台的順序", () => {
    const tabs = siteTabsFor(
      [agent(a, "code"), agent(b, "code"), agent(b, "work")],
      [a, b],
      "code",
    );
    expect(tabs.map((x) => [x.kind, x.site.key])).toEqual([
      ["agent", ""],
      ["agent", "office"],
    ]);
  });

  test("她只在一台、別台都能用：不出現", () => {
    expect(siteTabsFor([agent(a, "code"), agent(b, "work")], [a, b], "code")).toEqual([]);
  });

  test("停用的不算", () => {
    expect(siteTabsFor([agent(a, "code"), agent(b, "code", false)], [a, b], "code")).toEqual([]);
  });

  test("別台連不上：列成不能點的分頁，不讓那台無聲消失", () => {
    const down = site("office", "down");
    const tabs = siteTabsFor([agent(a, "code")], [a, down], "code");
    expect(tabs.map((x) => [x.kind, x.site.key])).toEqual([
      ["agent", ""],
      ["unavailable", "office"],
    ]);
  });
});
