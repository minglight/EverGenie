import { Avatar, cn, MoreMenu, type MoreMenuItem, PersonaMedia } from "@evergenie/ui";
import { ChevronDown, ChevronLeft, Cpu, PanelLeft } from "lucide-react";
import { useState } from "react";
import { BackLink } from "../../components/BackLink";
import { PageTitle } from "../../components/PageTitle";
import { SiteRing, SiteTag, siteDetail } from "../../components/SiteTag";
import { type AgentView, type Presence, presenceText } from "../../data/agents";
import type { Site } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { statusTone, toneDot } from "../../lib/statusTone";
import { useDocumentVisible } from "../../lib/visible";
import { AgentSwitcher, AgentSwitcherSheet } from "./AgentSwitcher";
import type { ModelLine } from "./modelLine";
import { type StatusView, toneClass } from "./threads";

/** 標題欄的「現在用的模型 · 思考力度」（WEB-84）；點了打開「/model」選單。 */
export interface ModelChip {
  line: ModelLine;
  onPick: () => void;
}

export interface PaneToggle {
  /** 現在是不是「清單」模式（按鈕按下去的樣子）。 */
  pressed: boolean;
  onToggle: () => void;
}

interface Props {
  agent: AgentView;
  /** 連多台時這一串在哪台：頂端寫「哪台 · 正式／測試 · 版本」、頭像外框用那台的顏色；只有一台時不給。 */
  site?: Site;
  /** 這位秘書現在的樣子（工作中／等你／有空）。 */
  presence: Presence;
  /** 這一串的狀態字（「要你回答」、正在做的事…），標題欄那一行。 */
  status: StatusView;
  working: boolean;
  waiting: boolean;
  showBack: boolean;
  /** 手機（< 1024）：頂端是角色影片，可以收合。 */
  hero: boolean;
  collapsed: boolean;
  onExpand: () => void;
  /** 桌機 ≥ 1280：「對話清單」切換鈕。 */
  paneToggle?: PaneToggle;
  /** 這段對話現在用的模型與力度；不知道就不給（不畫）。 */
  model?: ModelChip;
  /** 「重開對話」鈕（DEV-106）；不給＝不畫（session 還沒讀到）。 */
  restart?: RestartButtonProps;
}

export interface RestartButtonProps {
  /** 說明列現在開著嗎（按鈕按下去的樣子）。 */
  open: boolean;
  onToggle: () => void;
  agentName: string;
}

// 標題欄右上的「⋯」（DEV-156 U-19）：「換一位秘書」「重開對話」用字寫在選單裡。以前是 ⇅、↻ 兩個圖示，
// ↻ 很容易被當成重新整理。「重開對話」只打開說明列，不直接重開（RestartBar）。
function ThreadMenu({
  agent,
  restart,
  onSwitch,
  onMedia,
}: {
  agent: AgentView;
  restart?: RestartButtonProps;
  onSwitch: () => void;
  onMedia?: boolean;
}) {
  const items: MoreMenuItem[] = [
    {
      key: "switch",
      label: t("chat.switch.title"),
      onSelect: onSwitch,
      testId: "thread-menu-switch",
    },
  ];
  if (restart) {
    items.push({
      key: "restart",
      label: t("chat.restart.button"),
      onSelect: restart.onToggle,
      testId: "thread-restart",
    });
  }
  return (
    <MoreMenu
      // 讀屏名字照實際有的選項寫：沒有「重開對話」時不提它（DEV-156 review）。
      label={restart ? t("chat.menu.label") : t("chat.menu.label.switchOnly")}
      title={agent.name}
      items={items}
      testId="thread-menu"
      className={
        onMedia
          ? "bg-media-btn text-on-media"
          : restart?.open
            ? "bg-selected text-accent"
            : "text-muted hover:bg-hover hover:text-fg"
      }
    />
  );
}

function ThreadBack({ onMedia }: { onMedia: boolean }) {
  return (
    <BackLink
      className={cn(
        "flex size-11 shrink-0 items-center justify-center rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-focus",
        onMedia ? "bg-media-btn text-on-media" : "text-fg",
      )}
    >
      <ChevronLeft className="size-6" strokeWidth={1.8} aria-hidden="true" />
    </BackLink>
  );
}

// 討論串頂端。手機：角色影片（展開 min(340px, 40vh)）往上捲或聚焦輸入框就收成 60px 的標題欄；
// 桌機：60px 標題欄，≥ 1280 右邊一顆「對話清單」鈕（換人用人名鈕或側欄的篩選）。
export function ThreadHeader(props: Props) {
  const { agent, site, status, working, waiting, hero, paneToggle, model, restart } = props;
  // 「⋯」裡的「換一位秘書」打開名字旁邊那個同一個切換（桌機是名字下方的小浮窗）。
  const [switching, setSwitching] = useState(false);
  if (hero) return <HeroHeader {...props} />;
  return (
    <header className="flex h-[60px] shrink-0 items-center gap-1.5 border-b border-border bg-surface pr-3 pl-1">
      {props.showBack ? (
        <ThreadBack onMedia={false} />
      ) : (
        <span className="w-3 shrink-0" aria-hidden="true" />
      )}
      <MiniIdentity
        agent={agent}
        site={site}
        status={status}
        working={working}
        waiting={waiting}
        switchable
        switching={switching}
        onSwitching={setSwitching}
      />
      <div className="ml-auto flex min-w-0 items-center gap-1 pl-3">
        {model ? <ModelButton model={model} /> : null}
        <ThreadMenu agent={agent} restart={restart} onSwitch={() => setSwitching(true)} />
        {paneToggle ? (
          <button
            type="button"
            aria-pressed={paneToggle.pressed}
            onClick={paneToggle.onToggle}
            className={cn(
              "flex h-11 shrink-0 items-center gap-1.5 rounded-xl px-3 text-[13px] font-semibold focus-visible:outline-2 focus-visible:outline-focus",
              paneToggle.pressed ? "bg-selected text-accent" : "text-muted hover:bg-hover",
            )}
          >
            <PanelLeft className="size-[18px]" strokeWidth={1.8} aria-hidden="true" />
            {t("chat.pane.toggle")}
          </button>
        ) : null}
      </div>
    </header>
  );
}

// 「X 9 · 思考 medium」小鈕：點了在輸入框打開「/model」選單（WEB-84）。
function ModelButton({ model, onMedia }: { model: ModelChip; onMedia?: boolean }) {
  return (
    <button
      type="button"
      data-testid="thread-model"
      title={model.line.title}
      onClick={model.onPick}
      className={cn(
        "flex h-9 min-w-0 shrink items-center gap-1.5 rounded-full px-3 text-xs font-medium focus-visible:outline-2 focus-visible:outline-focus",
        onMedia
          ? "bg-media-btn text-on-media"
          : "border border-border text-muted hover:bg-hover hover:text-fg",
      )}
    >
      <Cpu className="size-3.5 shrink-0" strokeWidth={1.8} aria-hidden="true" />
      <span className="truncate">{model.line.text}</span>
    </button>
  );
}

// 名字旁邊的小 chevron 鈕外觀：觸控範圍用 ::after 撐滿整個身分區塊（頭像、名字、狀態都點得到），至少 44px 高（FEN-7）。
const switchTrigger =
  "flex max-w-full items-center gap-0.5 rounded-md text-left after:absolute after:inset-0 after:content-[''] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus";

function MiniIdentity({
  agent,
  site,
  status,
  working,
  waiting,
  switchable,
  switching,
  onSwitching,
  modelText,
}: {
  agent: AgentView;
  site?: Site;
  status: StatusView;
  working: boolean;
  waiting: boolean;
  /** 手機收合時狀態後面接一小段「模型 · 力度」（不能點，整塊是展開鈕）。 */
  modelText?: string;
  /** 名字變成「換一位秘書」的鈕（桌機標題欄）；手機收合時身分是「展開」鈕，換人在右上的「⋯」。 */
  switchable?: boolean;
  /** 切換開著沒（「⋯」選單也能打開它）。 */
  switching?: boolean;
  onSwitching?: (open: boolean) => void;
}) {
  const name = <span className="truncate">{agent.name}</span>;
  return (
    <div className="relative flex min-h-11 min-w-0 items-center" data-testid="thread-identity">
      {site ? (
        <SiteRing site={site} on>
          <Avatar
            name={agent.name}
            color={agent.color}
            colorDark={agent.colorDark}
            src={agent.avatarUrl}
            size={36}
            pulse={working}
            waiting={waiting}
          />
        </SiteRing>
      ) : (
        <Avatar
          name={agent.name}
          color={agent.color}
          colorDark={agent.colorDark}
          src={agent.avatarUrl}
          size={36}
          pulse={working}
          waiting={waiting}
        />
      )}
      <div className="flex min-w-0 flex-col pl-3 text-left">
        <PageTitle className="truncate text-base leading-tight font-semibold">
          {switchable ? (
            <AgentSwitcher
              agentKey={agent.key}
              agentName={agent.name}
              className={switchTrigger}
              open={switching}
              onOpenChange={onSwitching}
            >
              {name}
              <ChevronDown
                className="size-4 shrink-0 text-muted"
                strokeWidth={2}
                aria-hidden="true"
              />
            </AgentSwitcher>
          ) : (
            agent.name
          )}
        </PageTitle>
        {modelText ? (
          // 狀態字與「模型 · 力度」分開（狀態字的內容不混進別的東西）。
          <p className="truncate text-xs">
            <span className={toneClass[status.tone]} data-testid="thread-status">
              {status.text || t("chat.status.idle")}
            </span>
            <span className="text-muted" data-testid="thread-model-inline">
              {" · "}
              {modelText}
            </span>
          </p>
        ) : (
          <p className={cn("truncate text-xs", toneClass[status.tone])} data-testid="thread-status">
            {status.text || t("chat.status.idle")}
          </p>
        )}
      </div>
      {site ? <ThreadSite site={site} className="ml-2" /> : null}
    </div>
  );
}

// 對話頂端的「哪台 · 正式／測試 · 版本」（誰＝旁邊的人名）：只在連多台時畫。
function ThreadSite({ site, className }: { site: Site; className?: string }) {
  return (
    <span data-testid="thread-site" className={cn("flex min-w-0 shrink", className)}>
      <SiteTag site={site} detail={siteDetail(site)} />
    </span>
  );
}

function HeroHeader({
  agent,
  site,
  presence,
  status,
  working,
  waiting,
  collapsed,
  onExpand,
  model,
  restart,
}: Props) {
  const visible = useDocumentVisible();
  const [switching, setSwitching] = useState(false);
  const state = t(presenceText[presence]);
  const line = agent.duty ? t("chat.hero.status", { state, duty: agent.duty }) : state;
  return (
    <header
      data-hero={collapsed ? "collapsed" : "expanded"}
      className={cn(
        "relative shrink-0 overflow-hidden border-b bg-surface motion-safe:transition-[height] motion-safe:duration-200",
        collapsed ? "h-[60px] border-border" : "h-[min(340px,40vh)] border-transparent",
      )}
    >
      {/* 展開：角色影片鋪滿，左下人名與狀態；收合時淡出 */}
      <PersonaMedia
        name={agent.name}
        src={agent.avatarUrl}
        video={agent.videoUrl}
        videos={agent.videoUrls}
        color={agent.color}
        colorDark={agent.colorDark}
        paused={collapsed || !visible}
        className={cn(
          "absolute inset-0 motion-safe:transition-opacity motion-safe:duration-200",
          collapsed && "pointer-events-none opacity-0",
        )}
      >
        <div
          className="absolute right-5 bottom-4 left-5 text-on-media"
          aria-hidden={collapsed}
          inert={collapsed}
        >
          <PageTitle className="text-[26px] leading-tight font-bold tracking-[-0.01em]">
            <AgentSwitcher
              agentKey={agent.key}
              agentName={agent.name}
              className="relative flex max-w-full items-center gap-1 rounded-md text-left after:absolute after:-inset-y-2 after:-inset-x-1 after:content-[''] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-on-media"
            >
              <span className="truncate">{agent.name}</span>
              <ChevronDown className="size-5 shrink-0" strokeWidth={2.2} aria-hidden="true" />
            </AgentSwitcher>
          </PageTitle>
          <p
            className="mt-1 flex items-center gap-2 text-[13px] text-on-media-soft"
            data-testid="persona-status"
          >
            <span className={cn("size-2 shrink-0 rounded-full", toneDot[statusTone(presence)])} />
            <span className="truncate">{line}</span>
          </p>
          {model ? (
            <p className="mt-2 flex items-center gap-2">
              <ModelButton model={model} onMedia />
            </p>
          ) : null}
          {site ? (
            <p className="mt-1.5 flex">
              <SiteTag
                site={site}
                detail={siteDetail(site)}
                className="bg-media-btn text-on-media"
              />
            </p>
          ) : null}
        </div>
      </PersonaMedia>
      {/* 上面一條 60px：返回鈕；收合後多出頭像、人名與狀態（點了再展開） */}
      <div className="absolute inset-x-0 top-0 flex h-[60px] items-center gap-1.5 px-2">
        <ThreadBack onMedia={!collapsed} />
        <div
          className={cn(
            "relative flex h-11 min-w-0 items-center gap-1.5 pr-3 motion-safe:transition-opacity motion-safe:duration-200",
            collapsed ? "opacity-100" : "opacity-0",
          )}
          aria-hidden={!collapsed}
          inert={!collapsed}
        >
          <MiniIdentity
            agent={agent}
            site={site}
            status={status}
            working={working}
            waiting={waiting}
            modelText={model?.line.text}
          />
          {/* 整塊都是「展開」鈕；放在身分的上面，名字與狀態仍留在文件裡給讀屏軟體 */}
          <button
            type="button"
            onClick={onExpand}
            aria-expanded={!collapsed}
            aria-label={t("chat.hero.expand", { name: agent.name })}
            className="absolute inset-0 rounded-xl focus-visible:outline-2 focus-visible:outline-focus"
          />
        </div>
        {/* 右上的「⋯」：展開、收合都在（換一位秘書、重開對話；DEV-156 U-19）。展開時名字旁邊的 chevron 也能換人。 */}
        <div className="ml-auto flex items-center">
          <ThreadMenu
            agent={agent}
            restart={restart}
            onSwitch={() => setSwitching(true)}
            onMedia={!collapsed}
          />
        </div>
        <AgentSwitcherSheet agentKey={agent.key} open={switching} onOpenChange={setSwitching} />
      </div>
    </header>
  );
}
