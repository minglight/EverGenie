import type { ThreadSummary } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import type { Site } from "../../data/sites";
import { siteUsable } from "../../data/sites";
import { chatCounts, findThread, type SiteThreads } from "./siteThreads";

// 聊天每台各自的討論串（S3-6）：徽章跨台加總、照台找串、不能用的台不算。

const site = (key: string, state: Site["state"] = "ok"): Site =>
  ({
    key,
    base: key ? `https://${key}.example` : "",
    primary: key === "",
    label: key || "amy-mini",
    machine: key || "mini",
    env: "prod",
    version: "1",
    boardApi: 1,
    publicUrl: "",
    color: "",
    state,
    reason: "",
  }) as Site;

const th = (name: string, agent: string, waiting = false): ThreadSummary =>
  ({
    name,
    agent,
    status: waiting ? "waiting_user" : "idle",
    unread: 0,
    waiting: waiting ? ["reply"] : [],
  }) as unknown as ThreadSummary;

const group = (s: Site, data: ThreadSummary[] | undefined): SiteThreads => ({
  site: s,
  usable: siteUsable(s),
  data,
  isPending: false,
  isError: false,
  refetch: () => {},
});

describe("每台的討論串", () => {
  test("TestSiteUsableOnlyPrimaryOrOk：主伺服器永遠可以；別台只有 ok 才讀寫", () => {
    expect(siteUsable(site(""))).toBe(true);
    expect(siteUsable(site("", "down"))).toBe(true);
    expect(siteUsable(site("office"))).toBe(true);
    for (const s of ["down", "denied", "old", "checking"] as const)
      expect(siteUsable(site("office", s))).toBe(false);
  });

  test("TestFindThreadBySite：同名的串照台找，不會拿到另一台的", () => {
    const a = th("code", "code");
    const b = th("code", "code");
    const all = [group(site(""), [a]), group(site("office"), [b])];
    expect(findThread(all, "office", "code")).toBe(b);
    expect(findThread(all, "", "code")).toBe(a);
    expect(findThread(all, "nas", "code")).toBeUndefined();
  });

  test("TestChatCountsSumAcrossSites：總串數加總每台，徽章 key 帶台名；不能用的台的舊快取不算", () => {
    const all = [
      group(site(""), [th("code", "code", true), th("life", "life")]),
      group(site("office"), [th("code", "code", true)]),
      group(site("nas", "down"), [th("code", "code", true), th("x", "x")]),
    ];
    const { counts, total } = chatCounts(all);
    expect(total).toBe(3);
    expect(counts).toEqual({ code: 1, "office/code": 1 });
  });
});
