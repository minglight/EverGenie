import { Avatar, cn } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useId, useMemo, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { SiteNotice } from "../../components/SiteNotice";
import { siteName } from "../../components/SiteTag";
import {
  agentKey,
  presenceOf,
  splitAgentKey,
  useAgentSite,
  useAgents,
  useSiteAgents,
} from "../../data/agents";
import { refreshAll } from "../../data/live";
import { chatAgentFor, usePageChat } from "../../data/pageChat";
import { rosterQuery, threadsQuery } from "../../data/queries";
import { apiFor, homeSearch, type Site, siteUsable, useSites } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import { ThreadView } from "./ThreadView";

export interface PageChatProps {
  chatKey: string;
  fallback: string;
  /** 這一頁固定跟這位聊（REQ-236，頁面宣告的 chat_agent 在名冊上）：不給選人，上面是她的小標題。agentKey。 */
  fixed?: string;
  /** aside：寬螢幕右邊一欄；inline：窄螢幕「聊天」分頁裡，高度由外面給。 */
  variant?: "aside" | "inline";
  className?: string;
}

// 套件頁的聊天（REQ-235、236）。頁面宣告了負責的秘書就固定跟她聊；沒有的話上面選跟誰聊
// （每一頁記一位，沒選過用頁面宣告的預設秘書），下面是那位的討論串（跟聊天頁同一個元件）。
// 連多台時選單照電腦分組、別台的秘書也選得到（值是 agentKey，主伺服器就是職位 id，舊的選擇照用）；
// 選的那位在別台、那台現在不能用：照樣選著她、下面寫 SiteNotice，不改跟主伺服器的人聊。
export function PageChat({
  chatKey,
  fallback,
  fixed,
  variant = "aside",
  className,
}: PageChatProps) {
  const client = useQueryClient();
  const rosters = useSiteAgents();
  const sites = useSites();
  const { picks, pick } = usePageChat(chatKey);
  const selectId = useId();
  const multi = sites.length > 1;
  const groups = useMemo(
    () =>
      rosters.map(({ site, list }) => ({
        site,
        options: list
          .filter((a) => a.enabled && a.id)
          .map((a) => ({ key: agentKey(site.key, a.id ?? ""), name: a.name || a.id || "" })),
      })),
    [rosters],
  );
  // 別台的選擇、那台現在不能用（試連中、連不上、被拒）：不算失效，等它回來。
  const waitingSite = (key: string): Site | undefined => {
    const at = splitAgentKey(key).site;
    if (at === "") return undefined;
    const site = sites.find((s) => s.key === at);
    return site && !siteUsable(site) ? site : undefined;
  };
  const picked = chatAgentFor(
    picks,
    chatKey,
    fallback,
    (key) =>
      groups.some((g) => g.options.some((o) => o.key === key)) || waitingSite(key) !== undefined,
  );
  const agent = fixed || picked;
  const waiting = agent ? waitingSite(agent) : undefined;
  // 那台現在不能用：人名用這個分頁上次讀到的那台名冊（定案 11 不露職位 id），沒讀過就寫「秘書」。
  const waitingName = waiting
    ? (client
        .getQueryData(rosterQuery(waiting).queryKey)
        ?.find((a) => a.id === splitAgentKey(agent).id)?.name ?? t("agent.unknown"))
    : "";
  const option = (o: { key: string; name: string }) => (
    <option key={o.key} value={o.key}>
      {o.name}
    </option>
  );

  return (
    <aside
      aria-label={t("pageChat.label")}
      data-testid="page-chat"
      data-variant={variant}
      className={cn(
        "flex min-h-0 flex-col bg-surface",
        variant === "aside"
          ? "w-[420px] shrink-0 border-l border-border"
          : "overflow-hidden rounded-[18px] border border-border",
        className,
      )}
    >
      {fixed ? (
        <FixedHeader agent={fixed} />
      ) : (
        <div className="flex items-center gap-2 border-b border-border px-4 py-2.5">
          <label htmlFor={selectId} className="shrink-0 text-[13px] font-semibold text-muted">
            {t("pageChat.with")}
          </label>
          <select
            id={selectId}
            data-testid="page-chat-agent"
            value={agent}
            onChange={(e) => pick(e.target.value)}
            className="min-h-10 min-w-0 flex-1 rounded-xl border border-border-strong bg-bg px-2.5 text-sm text-fg focus-visible:outline-2 focus-visible:outline-focus"
          >
            {agent === "" ? <option value="">{t("pageChat.pick")}</option> : null}
            {waiting ? (
              <option value={agent} disabled>
                {t("chat.site.who", { name: waitingName, site: siteName(waiting) })}
              </option>
            ) : null}
            {multi
              ? groups.map((g) => (
                  <optgroup key={g.site.key || "primary"} label={siteName(g.site)}>
                    {g.options.map(option)}
                  </optgroup>
                ))
              : (groups[0]?.options ?? []).map(option)}
          </select>
        </div>
      )}
      {agent === "" ? (
        <p className="px-5 py-6 text-sm text-muted">{t("pageChat.none")}</p>
      ) : (
        <PageThread key={agent} agent={agent} canStart={Boolean(fixed)} />
      )}
    </aside>
  );
}

function FixedHeader({ agent }: { agent: string }) {
  const { site, id, usable } = useAgentSite(agent);
  if (!site || !usable) return null;
  return <FixedHeaderOn site={site} id={id} />;
}

function FixedHeaderOn({ site, id }: { site: Site; id: string }) {
  const { view } = useAgents(site);
  const { data: threads } = useQuery(threadsQuery(site));
  const a = view(id);
  const { session } = presenceOf(threads ?? [], id, site.key);
  return (
    <div className="flex min-h-[52px] items-center gap-2.5 border-b border-border px-4 py-2">
      <Avatar name={a.name} color={a.color} colorDark={a.colorDark} src={a.avatarUrl} size={28} />
      <p className="min-w-0 flex-1 truncate text-sm font-semibold">
        {t("pageChat.with.fixed", { name: a.name })}
      </p>
      {session ? (
        <Link
          to="/chat/$session"
          params={{ session }}
          search={homeSearch(site)}
          className="flex min-h-10 shrink-0 items-center rounded-lg px-2 text-[13px] font-semibold text-accent hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus"
        >
          {t("pageChat.open")}
        </Link>
      ) : null}
    </div>
  );
}

// 那位秘書在哪台就讀那台（agentKey）；那台現在不能用只寫一行 SiteNotice。
function PageThread({ agent, canStart }: { agent: string; canStart: boolean }) {
  const { site, id, usable } = useAgentSite(agent);
  if (!site) return <p className="px-5 py-6 text-sm text-muted">{t("pageChat.none")}</p>;
  if (!usable) return <SiteNotice site={site} className="m-4" />;
  return <Thread site={site} agent={id} canStart={canStart} />;
}

// 這位秘書最近的討論串；還沒有的話，固定的秘書就地開一個（就在這頁聊），選來的秘書給一條去聊天頁的路。
function Thread({ site, agent, canStart }: { site: Site; agent: string; canStart: boolean }) {
  const client = useQueryClient();
  const { view } = useAgents(site);
  const { data: threads } = useQuery(threadsQuery(site));
  const [started, setStarted] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const name = view(agent).name;
  const found = presenceOf(threads ?? [], agent, site.key);
  const session = found.session || started;
  const summary = session ? threads?.find((th) => th.name === session) : undefined;

  if (session) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <ThreadView
          key={session}
          name={session}
          summary={summary}
          showBack={false}
          bare
          home={site}
        />
      </div>
    );
  }
  if (!canStart) {
    return (
      <div className="flex flex-col gap-2 px-5 py-6 text-sm text-muted">
        <p>{t("pageChat.noThread", { name })}</p>
        <Link to="/chat" className="font-semibold text-accent">
          {t("pageChat.goChat")}
        </Link>
      </div>
    );
  }
  const start = async () => {
    setBusy(true);
    setError("");
    try {
      const res = await apiFor(site).session.open({ agent });
      setStarted(res.session?.name ?? "");
      await refreshAll(client, site);
    } catch (err) {
      setError(t("pageChat.startFailed", { reason: reasonOf(err) }));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-col items-start gap-3 px-5 py-6 text-sm text-muted">
      <p>{t("pageChat.noThread", { name })}</p>
      <ActionButton size="card" tone="primary" busy={busy} onClick={() => void start()}>
        {busy ? t("pageChat.starting", { name }) : t("pageChat.start", { name })}
      </ActionButton>
      {error ? (
        <p role="alert" className="text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}
