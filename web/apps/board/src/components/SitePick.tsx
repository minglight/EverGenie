import { AgentPicker } from "@evergenie/ui";
import { useMemo, useState } from "react";
import { agentKey, type SiteAgents, splitAgentKey, useSiteAgents } from "../data/agents";
import { type Site, siteUsable, useSites } from "../data/sites";
import { SiteTag, siteName } from "./SiteTag";

// 新東西建在哪台（S3-7b；計畫設計決定 4）：建在那位秘書所在的台，沒指定秘書就建在主伺服器，膠囊可以改。
// 秘書一律用 agentKey（主伺服器＝職位 id，別台＝「台名/職位 id」），台跟著秘書走，不會把 B 的秘書寫進 A。

/**
 * 一位秘書（agentKey）解開成：哪台、那台的職位 id、名字。那台現在不能用（不在能用的台裡），
 * 或那台沒有這個職位、職位停用了（篩選裡留著的舊選擇）時回 undefined。
 */
export interface ResolvedAgent {
  site: Site;
  id: string;
  name: string;
}

export function resolveAgentKey(
  agents: readonly SiteAgents[],
  key: string,
): ResolvedAgent | undefined {
  if (!key) return undefined;
  const { site, id } = splitAgentKey(key);
  const found = agents.find((a) => a.site.key === site);
  if (!found?.list.some((r) => r.enabled && r.id === id)) return undefined;
  return { site: found.site, id, name: found.view(id).name };
}

/** 每台啟用中的秘書的 agentKey（照台、再照名冊順序）。 */
export function enabledAgentKeys(agents: readonly SiteAgents[]): string[] {
  return agents.flatMap((a) =>
    a.list.filter((r) => r.enabled).map((r) => agentKey(a.site.key, r.id ?? "")),
  );
}

export interface SiteTarget {
  /** 能建的台（主伺服器＋ok 的別台）與每台的秘書。 */
  agents: SiteAgents[];
  sites: Site[];
  /** 選的秘書（agentKey）；沒選或那台用不了是 ""。 */
  agent: string;
  /** 那位秘書在那台的職位 id（送給伺服器的）；沒選是 ""。 */
  agentId: string;
  agentName: string;
  /**
   * 要建在哪台：有秘書＝秘書所在的台；沒有＝自己選的台，沒選過是主伺服器。
   * 選的（或預設的）秘書在別台、那台現在用不了（試連中、連不上、被拒）時就是那台——不退回主伺服器，`ready` 是 false。
   */
  site: Site;
  /** 那台現在能建（主伺服器，或 ok 的別台）；false 時送出鈕要停用。 */
  ready: boolean;
  /** 自己點過秘書或換過台（之後不再跟著 fallback 走）。 */
  picked: boolean;
  setAgent: (key: string) => void;
  /** 換台：選了秘書的話換成那台同一個職位的秘書（那台沒有就不選）。 */
  setSite: (key: string) => void;
}

/**
 * 開單、加待辦、開 Goal 共用的「建給誰、建在哪台」。`fallback` 是沒自己點過時的預設秘書（agentKey 或 ""）。
 * 秘書所在的台現在不能用（試連中、連不上、被拒）時，目標還是那台、`ready` 是 false，不會偷偷建到主伺服器；
 * 台不在清單裡（設定拿掉了）的舊選擇當成沒選。
 */
export function useSiteTarget(
  fallback: string | ((agents: readonly SiteAgents[]) => string),
): SiteTarget {
  const agents = useSiteAgents();
  const all = useSites();
  const [agentPick, setAgentPick] = useState<string | null>(null);
  const [sitePick, setSitePick] = useState<string | null>(null);
  const sites = useMemo(() => agents.map((a) => a.site), [agents]);
  const raw = agentPick ?? (typeof fallback === "function" ? fallback(agents) : fallback);
  const resolved = resolveAgentKey(agents, raw);
  const primary = sites[0] as Site;
  // 秘書指到別台、那台現在不在能用的台裡：目標停在那台（等它好），不退回主伺服器。
  const rawSite = raw ? splitAgentKey(raw).site : "";
  const waiting =
    !resolved && rawSite !== "" && !sites.some((s) => s.key === rawSite)
      ? all.find((s) => s.key === rawSite)
      : undefined;
  const site =
    resolved?.site ??
    waiting ??
    sites.find((s) => s.key === sitePick) ??
    all.find((s) => s.key === sitePick) ??
    primary;
  const ready = siteUsable(site);
  return {
    agents,
    sites,
    agent: resolved ? raw : "",
    agentId: resolved?.id ?? "",
    agentName: resolved?.name ?? "",
    site,
    ready,
    picked: agentPick !== null,
    setAgent: (key) => setAgentPick(key),
    setSite: (key) => {
      setSitePick(key);
      if (!resolved) {
        // 原本卡在用不了的台的預設秘書：換台就不再跟著它。
        if (waiting) setAgentPick("");
        return;
      }
      if (resolved.site.key === key) return;
      const there = agents.find((a) => a.site.key === key);
      const same = there?.list.some((r) => r.enabled && r.id === resolved.id);
      setAgentPick(same ? agentKey(key, resolved.id) : "");
    },
  };
}

/**
 * 選秘書：只有一台時就是原本的一排頭像（值是職位 id，跟 S3 之前一樣）；
 * 好幾台時每台一排、前面標台名，值是 agentKey。
 */
export function SiteAgentPicker({
  agents,
  value,
  onChange,
  label,
}: {
  agents: readonly SiteAgents[];
  value: string;
  onChange: (key: string) => void;
  label: string;
}) {
  const groups = agents.map((a) => ({
    site: a.site,
    options: a.list
      .filter((r) => r.enabled)
      .map((r) => ({
        id: agentKey(a.site.key, r.id ?? ""),
        name: r.name ?? "",
        color: r.color,
        colorDark: r.color_dark,
        avatarUrl: a.view(r.id).avatarUrl,
      })),
  }));
  if (groups.length <= 1) {
    return (
      <AgentPicker
        label={label}
        value={value}
        agents={groups[0]?.options ?? []}
        onChange={onChange}
      />
    );
  }
  return (
    <div className="flex flex-col gap-2">
      {groups.map((g) => (
        <div
          key={g.site.key}
          data-testid="site-agents"
          data-site={g.site.key}
          className="flex flex-col gap-1"
        >
          <SiteTag site={g.site} className="self-start" />
          <AgentPicker
            label={`${label}（${siteName(g.site)}）`}
            value={value}
            agents={g.options}
            onChange={onChange}
          />
        </div>
      ))}
    </div>
  );
}
