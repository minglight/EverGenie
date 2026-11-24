import { Popover } from "@base-ui/react/popover";
import { Avatar, cn, Sheet } from "@evergenie/ui";
import { Check } from "lucide-react";
import { type ReactNode, useState } from "react";
import { SiteNotice } from "../../components/SiteNotice";
import { SiteRing, SiteTag, siteName } from "../../components/SiteTag";
import { presenceText } from "../../data/agents";
import { t } from "../../i18n/zh-TW";
import { useIsDesktop } from "../../lib/media";
import { statusTone, toneText } from "../../lib/statusTone";
import { type ChatAgent, useChatAgents, useGoToAgent } from "./chatAgents";

// 「換一位秘書」的清單：名冊裡的秘書，頭像、名字、狀態（等你／工作中／有空，跟側欄同一個算法），目前這位打勾。
// 選了就導到那位的主要討論串（跟聊天清單點那位、側欄點那位同一個目的地）；沒有串就到聊天清單。
// 連多台時照電腦分段（同一位角色在兩台是兩列），每段上面一個台名標籤（S3-6）；不能用的台那段寫一行 SiteNotice。
function Choices({ currentKey, onPicked }: { currentKey: string; onPicked: () => void }) {
  const { agents, multi, sites } = useChatAgents();
  const go = useGoToAgent();
  const item = (a: ChatAgent) => {
    const current = a.key === currentKey;
    const state = t(presenceText[a.presence]);
    const who = multi ? t("chat.site.who", { name: a.name, site: siteName(a.site) }) : a.name;
    return (
      <li key={a.key}>
        <button
          type="button"
          aria-current={current ? "true" : undefined}
          aria-label={t("chat.switch.item", { name: who, state })}
          data-agent={a.id}
          data-site={a.site.key}
          onClick={() => {
            onPicked();
            if (!current) go(a);
          }}
          className={cn(
            "flex min-h-12 w-full items-center gap-3 rounded-xl px-2.5 text-left hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus",
            current && "bg-selected",
          )}
        >
          <SiteRing site={a.site} on={multi}>
            <Avatar
              name={a.name}
              color={a.color}
              colorDark={a.colorDark}
              src={a.avatarUrl}
              size={36}
              pulse={a.presence === "working"}
              waiting={a.presence === "waiting"}
            />
          </SiteRing>
          <span className="min-w-0 flex-1 truncate text-[15px] font-semibold">{a.name}</span>
          <span className={cn("shrink-0 text-xs", toneText[statusTone(a.presence)])}>{state}</span>
          {current ? (
            <Check
              className="size-4 shrink-0 text-accent"
              strokeWidth={2.2}
              role="img"
              aria-label={t("chat.switch.current")}
            />
          ) : (
            <span className="size-4 shrink-0" aria-hidden="true" />
          )}
        </button>
      </li>
    );
  };
  if (!multi) {
    return (
      <ul aria-label={t("chat.switch.list")} className="m-0 flex list-none flex-col gap-0.5 p-0">
        {agents.map(item)}
      </ul>
    );
  }
  return (
    <div className="flex flex-col gap-2">
      {sites.map((site) => {
        const mine = agents.filter((a) => a.site.key === site.key);
        return (
          <section
            key={site.key || "primary"}
            aria-label={siteName(site)}
            data-site={site.key}
            className="flex flex-col gap-0.5"
          >
            <h3 className="flex px-2.5 pt-1">
              <SiteTag site={site} />
            </h3>
            <SiteNotice site={site} className="mx-1 px-3 py-2" />
            {mine.length > 0 ? (
              <ul
                aria-label={t("chat.switch.list")}
                className="m-0 flex list-none flex-col gap-0.5 p-0"
              >
                {mine.map(item)}
              </ul>
            ) : null}
          </section>
        );
      })}
    </div>
  );
}

export interface AgentSwitcherProps {
  /** 現在這一串的秘書（agentKey：主伺服器就是職位 id，別台是「台名/職位 id」）。 */
  agentKey: string;
  agentName: string;
  /** 觸發鈕的樣式（位置與大小由標題欄決定）。 */
  className?: string;
  children: ReactNode;
  /** 由外面控制開關（標題欄「⋯」選單裡的「換一位秘書」也打開同一個，DEV-156 U-19）；不給就自己管。 */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}

// 聊天標題欄的秘書切換（REQ-200）：children 是觸發鈕的內容（頭像、名字、chevron…）。
// 手機是底部抽屜，桌機是標題下方的小浮窗；鍵盤可操作、Esc 關閉、關閉後焦點回到觸發鈕。
export function AgentSwitcher({
  agentKey,
  agentName,
  className,
  children,
  open: controlled,
  onOpenChange,
}: AgentSwitcherProps) {
  const desktop = useIsDesktop();
  const [own, setOwn] = useState(false);
  const open = controlled ?? own;
  const setOpen = (next: boolean) => {
    setOwn(next);
    onOpenChange?.(next);
  };
  const label = t("chat.switch.label", { name: agentName });
  const title = t("chat.switch.title");
  if (!desktop) {
    return (
      <>
        <button
          type="button"
          aria-label={label}
          aria-haspopup="dialog"
          aria-expanded={open}
          onClick={() => setOpen(true)}
          className={className}
        >
          {children}
        </button>
        <Sheet open={open} onOpenChange={setOpen} title={title}>
          <div className="max-h-[calc(var(--hg-visible-h)*0.6)] overflow-y-auto px-3 pb-2">
            <Choices currentKey={agentKey} onPicked={() => setOpen(false)} />
          </div>
        </Sheet>
      </>
    );
  }
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger aria-label={label} className={className}>
        {children}
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner align="start" sideOffset={6} className="z-50">
          <Popover.Popup
            aria-label={title}
            className="hg-pop w-[300px] rounded-2xl border border-border bg-surface p-1.5 shadow-[var(--shadow-pop)] outline-none"
          >
            <Choices currentKey={agentKey} onPicked={() => setOpen(false)} />
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
}

/** 沒有觸發鈕的「換一位秘書」底部抽屜：手機角色畫面標題欄的「⋯」選單打開它（DEV-156 U-19）。 */
export function AgentSwitcherSheet({
  agentKey,
  open,
  onOpenChange,
}: {
  agentKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title={t("chat.switch.title")}>
      <div className="max-h-[calc(var(--hg-visible-h)*0.6)] overflow-y-auto px-3 pb-2">
        <Choices currentKey={agentKey} onPicked={() => onOpenChange(false)} />
      </div>
    </Sheet>
  );
}
