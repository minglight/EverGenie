import type { RosterAgent } from "@evergenie/sdk-web";
import { act, renderHook } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import type { SiteAgents } from "../data/agents";
import type { Site, SiteState } from "../data/sites";
import { enabledAgentKeys, resolveAgentKey, useSiteTarget } from "./SitePick";

const state = vi.hoisted(() => ({
  agents: [] as SiteAgents[],
  sites: [] as Site[],
}));
vi.mock("../data/agents", async () => ({
  ...(await vi.importActual<typeof import("../data/agents")>("../data/agents")),
  useSiteAgents: () => state.agents,
}));
vi.mock("../data/sites", async () => ({
  ...(await vi.importActual<typeof import("../data/sites")>("../data/sites")),
  useSites: () => state.sites,
}));

const site = (key: string, st: SiteState = "ok"): Site => ({
  key,
  base: key ? `https://${key}.example.ts.net` : "",
  primary: key === "",
  label: key || "amy-mini",
  machine: "",
  env: "prod",
  version: "",
  boardApi: 1,
  publicUrl: "",
  color: "",
  state: st,
  reason: "",
});

const roster = (s: Site, agents: RosterAgent[]): SiteAgents => ({
  site: s,
  list: agents,
  all: agents,
  view: (id) => ({
    id: id ?? "",
    key: s.key ? `${s.key}/${id}` : (id ?? ""),
    name: agents.find((a) => a.id === id)?.name ?? "?",
    duty: "",
  }),
});

const mini = roster(site(""), [
  { id: "life", name: "維納斯", enabled: true },
  { id: "code", name: "雅典娜", enabled: true },
]);
const office = roster(site("amy-office"), [
  { id: "work", name: "阿媞米斯", enabled: true },
  { id: "code", name: "雅典娜", enabled: false },
]);

describe("新東西建在哪台", () => {
  test("選別台的秘書（台名/職位 id）＝建在那台、送那台的職位 id", () => {
    const got = resolveAgentKey([mini, office], "amy-office/work");
    expect(got?.site.key).toBe("amy-office");
    expect(got?.id).toBe("work");
    expect(got?.name).toBe("阿媞米斯");
  });

  test("選主伺服器的秘書（只有職位 id）＝建在主伺服器", () => {
    const got = resolveAgentKey([mini, office], "code");
    expect(got?.site.key).toBe("");
    expect(got?.id).toBe("code");
  });

  test("秘書那台現在用不了，或那台沒有這位、停用了，解不開（undefined）", () => {
    expect(resolveAgentKey([mini], "amy-office/work")).toBeUndefined();
    expect(resolveAgentKey([mini, office], "")).toBeUndefined();
    expect(resolveAgentKey([mini, office], "amy-office/code")).toBeUndefined();
    expect(resolveAgentKey([mini, office], "amy-office/nope")).toBeUndefined();
  });

  test("可選的秘書每台各列啟用中的，別台的帶台名", () => {
    expect(enabledAgentKeys([mini, office])).toEqual(["life", "code", "amy-office/work"]);
  });
});

describe("useSiteTarget：建給誰、建在哪台", () => {
  const use = (fallback: string, sites: Site[], agents: SiteAgents[]) => {
    state.sites = sites;
    state.agents = agents;
    return renderHook(() => useSiteTarget(fallback));
  };

  test("預設秘書在別台：建在那台；沒選秘書：主伺服器", () => {
    const both = [mini.site, office.site];
    expect(use("amy-office/work", both, [mini, office]).result.current.site.key).toBe("amy-office");
    const none = use("", both, [mini, office]).result.current;
    expect(none.site.key).toBe("");
    expect(none.ready).toBe(true);
  });

  test("預設秘書那台還在試連或連不上：目標停在那台、不能送，不退回主伺服器", () => {
    const down = site("amy-office", "down");
    const got = use("amy-office/work", [mini.site, down], [mini]).result.current;
    expect(got.site.key).toBe("amy-office");
    expect(got.ready).toBe(false);
    expect(got.agent).toBe("");
  });

  test("換台：那台有同一個職位就換成那台的，沒有（或停用）就清掉", () => {
    const other = roster(site("family"), [{ id: "life", name: "維納斯", enabled: true }]);
    const { result } = use("life", [mini.site, office.site, other.site], [mini, office, other]);
    act(() => result.current.setSite("family"));
    expect(result.current.agent).toBe("family/life");
    expect(result.current.site.key).toBe("family");
    act(() => result.current.setSite("amy-office"));
    expect(result.current.agent).toBe("");
    expect(result.current.site.key).toBe("amy-office");
  });
});
