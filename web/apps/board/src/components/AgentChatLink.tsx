import { Avatar, cn } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { presenceOf, useAgents } from "../data/agents";
import { threadsQuery } from "../data/queries";
import { homeSearch, PRIMARY, type SiteRef } from "../data/sites";
import { t } from "../i18n/zh-TW";

// 一列資料上的秘書頭像：點了進她的討論串，沒有串就到聊天清單（補充契約 S8 第 5 條）。
// 命中範圍用 ::after 補到 44×44（FEN-7）。
export function AgentChatLink({
  agent: id,
  size = 26,
  className,
  site = PRIMARY,
}: {
  agent: string;
  size?: number;
  className?: string;
  /** 這位秘書在哪一台（預設主伺服器）：頭像、狀態、討論串都看那台的。 */
  site?: SiteRef;
}) {
  const { view } = useAgents(site);
  const { data: threads } = useQuery(threadsQuery(site));
  const agent = view(id);
  const { presence, session } = presenceOf(threads ?? [], id, site.key);
  const cls = cn(
    "relative inline-flex shrink-0 rounded-full after:absolute after:top-1/2 after:left-1/2 after:size-11 after:-translate-x-1/2 after:-translate-y-1/2 after:content-[''] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
    className,
  );
  const avatar = (
    <Avatar
      name={agent.name}
      color={agent.color}
      colorDark={agent.colorDark}
      src={agent.avatarUrl}
      size={size}
      pulse={presence === "working"}
    />
  );
  const label = t("agent.openChat", { name: agent.name });
  return session ? (
    <Link
      to="/chat/$session"
      params={{ session }}
      search={homeSearch(site)}
      aria-label={label}
      className={cls}
    >
      {avatar}
    </Link>
  ) : (
    <Link to="/chat" aria-label={label} className={cls}>
      {avatar}
    </Link>
  );
}
