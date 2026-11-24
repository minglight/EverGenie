import { Dialog } from "@base-ui/react/dialog";
import { cn, Kbd, showToast } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Plus, Search, Send, X } from "lucide-react";
import {
  type CSSProperties,
  type KeyboardEvent,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import { useAgents } from "../data/agents";
import { overviewQuery, tasksQuery } from "../data/queries";
import { createReminder, sayToAgent } from "../data/quick";
import { type WaitingItem, waitingItems } from "../data/waiting";
import { type TextKey, t } from "../i18n/zh-TW";
import { fullTime } from "../lib/format";
import { iconFor } from "../lib/icons";
import { parseIntent } from "../lib/intent";
import { useIsDesktop, useVisibleHeight, useVisibleTop } from "../lib/media";
import { settingsCategories, tabLabel } from "../pages/settings/SettingsLayout";
import type { FlatItem } from "./flatten";
import { paletteListMaxHeight, paletteTop } from "./paletteLayout";

type Group = "action" | "waiting" | "tasks" | "goto";

interface Entry {
  key: string;
  group: Group;
  label: string;
  hint: string;
  glyph: "agent" | "page" | "todo" | "say";
  /** 提示用的一列（找不到秘書、還沒打內容）：不能執行。 */
  inert?: boolean;
  agentName?: string;
  agentColor?: { color?: string; dark?: string };
  icon?: string;
  run: () => void;
}

export interface CommandPaletteProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  menuItems: FlatItem[];
}

function matches(haystack: string, q: string): boolean {
  return q === "" || haystack.toLowerCase().includes(q);
}

/** 任務單最多列幾張（再多請到任務單頁找）。 */
const TASK_RESULTS = 6;

const kindKey = {
  approve: "kind.approve",
  review: "kind.review",
  ask: "kind.ask",
} as const;

// 指令面板（S3 先做導覽）：⌘K／Ctrl+K 或搜尋鈕打開；結果分「動作」「等你處理」「任務單」「前往」（秘書的聊天與頁面）。
export function CommandPalette({ open, onOpenChange, menuItems }: CommandPaletteProps) {
  const navigate = useNavigate();
  const client = useQueryClient();
  const { list, view } = useAgents();
  const { data: overview } = useQuery(overviewQuery());
  // 打字找任務單（編號或標題）：只在面板開著時抓還沒結束的單（2026-11-02 稽核，舊看板的搜尋找得到內容）。
  const { data: openTasks } = useQuery({ ...tasksQuery("all"), enabled: open });
  const [query, setQuery] = useState("");
  // 選取記「哪一項」（key）不記位置：清單晚一步多了等你處理、任務單時，選取跟著那一項走（DEV-128）。
  const [activeKey, setActiveKey] = useState<string | null>(null);
  const listId = useId();
  const listRef = useRef<HTMLDivElement>(null);
  const desktop = useIsDesktop();
  // 手機鍵盤打開時清單只用鍵盤上面那塊（WEB-124）；桌機照 CSS。
  const visible = useVisibleHeight();
  const listMax = desktop ? undefined : paletteListMaxHeight(visible);
  // iOS 打開鍵盤把畫面往上推時，面板頂端跟著看得到的那塊往下，不跑出畫面（WEB-128）。
  const visibleTop = useVisibleTop();
  const top = desktop ? undefined : paletteTop(visibleTop);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      setQuery("");
      setActiveKey(null);
    }
  }, [open]);

  const entries = useMemo(() => {
    const q = query.trim().toLowerCase();
    const close = (go: () => void) => () => {
      onOpenChange(false);
      go();
    };
    const waiting: Entry[] = waitingItems(overview)
      .map((item: WaitingItem) => ({ item, agent: view(item.agent) }))
      .filter(({ item, agent }) => matches(`${item.id} ${item.title} ${agent.name}`, q))
      .map(({ item, agent }) => ({
        key: `w:${item.id}`,
        group: "waiting" as const,
        label: `${item.id} · ${item.title}`,
        hint: t(kindKey[item.kind]),
        glyph: "agent" as const,
        agentName: agent.name,
        agentColor: { color: agent.color, dark: agent.colorDark },
        run: close(() => void navigate({ to: item.target.to, params: item.target.params })),
      }));
    const agents = list
      .filter((a) => a.enabled)
      .map((a) => ({ id: a.id ?? "", name: a.name ?? "" }));
    const intent = parseIntent(query, agents);
    const failed = (err: unknown) =>
      showToast(t("quick.failed", { reason: err instanceof Error ? err.message : String(err) }));
    const actions: Entry[] = [];
    if (intent.kind === "say") {
      if (intent.agent && intent.body) {
        const { agent, body } = intent;
        actions.push({
          key: "a:say",
          group: "action",
          label: t("palette.say", { name: agent.name, body }),
          hint: t("palette.hint.say"),
          glyph: "say",
          run: close(() => {
            sayToAgent(
              client,
              agent,
              body,
              (id) => void navigate({ to: "/chat/$session", params: { session: id } }),
            ).catch(failed);
          }),
        });
      } else if (intent.agent) {
        actions.push({
          key: "a:say",
          group: "action",
          label: t("palette.say.pick", { name: intent.agent.name }),
          hint: t("palette.hint.say"),
          glyph: "say",
          inert: true,
          run: () => {},
        });
      } else if (intent.named) {
        actions.push({
          key: "a:say",
          group: "action",
          label: t("palette.say.notFound", { name: intent.query }),
          hint: t("palette.hint.say"),
          glyph: "say",
          inert: true,
          run: () => {},
        });
      }
    } else if (intent.kind === "todo" && !intent.task && intent.content !== "") {
      // 「交代 …」是交代任務（RMD-22），不在這裡新增成待辦。
      const { content, at } = intent;
      const what = [at ? fullTime(at.toISOString()) : "", content].filter(Boolean).join("　");
      actions.push({
        key: "a:todo",
        group: "action",
        label: t("palette.todo", { what }),
        hint: t("palette.hint.todo"),
        glyph: "todo",
        // 只有抓到時間才排第一個；沒抓到時間的話只是備用（排在最後、沒有別的結果才會用到）。
        run: close(() => {
          createReminder(client, { content, owner: "life", at }).catch(failed);
        }),
      });
    }
    const todoWithTime =
      actions[0]?.key === "a:todo" && intent.kind === "todo" && intent.at !== null;
    const page = (item: FlatItem, label: string, route: string, key: string): Entry => ({
      key,
      group: "goto" as const,
      label,
      hint: t("palette.page"),
      glyph: "page" as const,
      icon: item.icon,
      run: close(() => void navigate({ to: route as "/" })),
    });
    // 合成一個入口的頁（「健康」裡的飲食、運動、身體）：打分頁名也找得到，按了到那個分頁（WEB-101）。
    const goto: Entry[] = menuItems.flatMap((item) =>
      matches(item.title, q)
        ? [page(item, item.title, item.route, `g:${item.id}`)]
        : q === ""
          ? []
          : (item.parts ?? [])
              .filter((p) => matches(p.title, q))
              .map((p) =>
                page(
                  item,
                  t("palette.part", { section: item.title, name: p.title }),
                  p.route,
                  `g:${item.id}:${p.route}`,
                ),
              ),
    );
    // 打了字才列設定的分頁（WEB-101）：打「通知」「權限」「套件」直接到「設定 · 那一頁」，不用先進設定再找。
    const settingsTabs: Entry[] =
      q === "" || intent.kind === "say"
        ? []
        : settingsCategories
            .flatMap((c) => c.tabs)
            .map((tab) => ({ tab, name: t(tabLabel[tab]) }))
            .filter(({ name }) => matches(name.toLowerCase(), q))
            .map(({ tab, name }) => ({
              key: `s:${tab}`,
              group: "goto" as const,
              label: t("palette.settingsTab", { name }),
              hint: t("palette.page"),
              glyph: "page" as const,
              icon: "settings",
              run: close(() => void navigate({ to: `/settings/${tab}` as "/settings/notify" })),
            }));
    // 打了字才找內容：還沒結束的任務單（等你處理已經列過的不重複）、秘書（帶去跟她聊天）。
    const listed = new Set(waitingItems(overview).map((w) => w.id));
    const tasks: Entry[] =
      q === ""
        ? []
        : (openTasks ?? [])
            .map((row) => row.task)
            .filter((task) => !listed.has(task.id ?? ""))
            .map((task) => ({ task, agent: view(task.agent) }))
            .filter(({ task, agent }) => matches(`${task.id} ${task.title} ${agent.name}`, q))
            .slice(0, TASK_RESULTS)
            .map(({ task, agent }) => ({
              key: `t:${task.id}`,
              group: "tasks" as const,
              label: `${task.id} · ${task.title}`,
              hint: t(`task.status.${task.status}` as TextKey),
              glyph: "agent" as const,
              agentName: agent.name,
              agentColor: { color: agent.color, dark: agent.colorDark },
              run: close(() => void navigate({ to: "/tasks/$id", params: { id: task.id ?? "" } })),
            }));
    const chats: Entry[] =
      q === "" || intent.kind === "say"
        ? []
        : list
            .filter((a) => a.enabled && matches(`${a.name ?? ""} ${a.id ?? ""}`, q))
            .map((a) => {
              const agent = view(a.id);
              return {
                key: `c:${a.id}`,
                group: "goto" as const,
                label: t("palette.chat", { name: agent.name }),
                hint: t("palette.hint.chat"),
                glyph: "agent" as const,
                agentName: agent.name,
                agentColor: { color: agent.color, dark: agent.colorDark },
                run: close(
                  () => void navigate({ to: "/chat/$session", params: { session: a.id ?? "" } }),
                ),
              };
            });
    const rest = [...waiting, ...tasks, ...chats, ...goto, ...settingsTabs];
    // 說出 @ 的傳話與抓到時間的新增待辦排第一個；沒抓到時間的待辦只在沒有別的結果時才出現。
    if (actions[0]?.key === "a:todo" && !todoWithTime) return rest.length === 0 ? actions : rest;
    return [...actions, ...rest];
  }, [query, overview, openTasks, view, menuItems, navigate, onOpenChange, client, list]);

  const found = activeKey === null ? -1 : entries.findIndex((e) => e.key === activeKey);
  const current = found < 0 ? 0 : found;

  useEffect(() => {
    listRef.current
      ?.querySelector(`[data-index="${current}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [current]);

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    // 輸入法選字中的 Enter／方向鍵不算指令。
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    if (e.key === "ArrowDown" && entries.length > 0) {
      e.preventDefault();
      setActiveKey(entries[(current + 1) % entries.length]?.key ?? null);
    } else if (e.key === "ArrowUp" && entries.length > 0) {
      e.preventDefault();
      setActiveKey(entries[(current - 1 + entries.length) % entries.length]?.key ?? null);
    } else if (e.key === "Enter") {
      e.preventDefault();
      entries[current]?.run();
    }
  };

  let lastGroup: Group | null = null;

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-scrim" />
        <Dialog.Popup
          aria-modal="true"
          aria-label={t("palette.title")}
          initialFocus={inputRef}
          style={top !== undefined ? { top } : undefined}
          className="hg-pop fixed inset-x-3 top-3 z-50 overflow-hidden rounded-2xl border border-border bg-surface shadow-[var(--shadow-pop)] outline-none md:inset-x-auto md:top-[110px] md:left-1/2 md:w-[620px] md:-translate-x-1/2"
        >
          <Dialog.Title className="sr-only">{t("palette.title")}</Dialog.Title>
          <div className="flex items-center gap-2.5 border-b border-border px-4">
            <Search
              className="size-[18px] shrink-0 text-muted"
              strokeWidth={1.8}
              aria-hidden="true"
            />
            <input
              ref={inputRef}
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setActiveKey(null);
              }}
              onKeyDown={onKeyDown}
              role="combobox"
              aria-expanded="true"
              aria-controls={listId}
              aria-activedescendant={entries[current] ? `${listId}-${current}` : undefined}
              aria-label={t("palette.title")}
              placeholder={t("palette.placeholder")}
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              enterKeyHint="go"
              className="h-[50px] min-w-0 flex-1 border-none bg-transparent text-base text-fg outline-none placeholder:text-muted md:h-[54px]"
            />
            <Kbd className="hidden md:inline">Esc</Kbd>
            {/* 手機沒有 Esc：右上角給一顆關閉鈕，不必知道要點外面（DEV-156 U-27）。 */}
            <Dialog.Close
              aria-label={t("palette.close")}
              data-testid="palette-close"
              className="-mr-2.5 flex size-11 shrink-0 items-center justify-center rounded-full text-muted hover:bg-hover hover:text-fg focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus md:hidden"
            >
              <X className="size-5" aria-hidden="true" />
            </Dialog.Close>
          </div>
          <div
            ref={listRef}
            id={listId}
            role="listbox"
            aria-label={t("palette.title")}
            style={listMax ? { maxHeight: listMax } : undefined}
            className="max-h-[calc(var(--hg-visible-h)*0.6)] overflow-y-auto p-2 md:max-h-[380px]"
          >
            {entries.length === 0 ? (
              <div role="status" className="px-2.5 py-4 text-sm text-muted">
                {t("palette.empty", { q: query.trim() })}
              </div>
            ) : null}
            {entries.map((entry, i) => {
              const heading = entry.group !== lastGroup;
              lastGroup = entry.group;
              const Icon = iconFor(entry.icon);
              const on = i === current;
              return (
                <div key={entry.key}>
                  {heading ? (
                    <div
                      className="px-2.5 pt-2 pb-1 text-xs font-semibold text-muted"
                      role="presentation"
                    >
                      {t(
                        entry.group === "action"
                          ? "palette.group.actions"
                          : entry.group === "waiting"
                            ? "palette.group.waiting"
                            : entry.group === "tasks"
                              ? "palette.group.tasks"
                              : "palette.group.goto",
                      )}
                    </div>
                  ) : null}
                  {/* biome-ignore lint/a11y/useKeyWithClickEvents: 鍵盤操作由輸入框的 aria-activedescendant 處理（↑↓ Enter） */}
                  <div
                    id={`${listId}-${i}`}
                    role="option"
                    aria-selected={on}
                    data-index={i}
                    tabIndex={-1}
                    onClick={entry.run}
                    onMouseMove={() => setActiveKey(entry.key)}
                    className={cn(
                      "flex min-h-11 cursor-pointer items-center gap-2.5 rounded-[10px] px-2.5 text-sm md:h-[42px] md:min-h-0",
                      on ? "bg-selected" : "bg-transparent",
                    )}
                  >
                    {entry.glyph === "todo" || entry.glyph === "say" ? (
                      <span
                        aria-hidden="true"
                        className={cn(
                          "flex size-[22px] shrink-0 items-center justify-center rounded-md",
                          entry.inert ? "bg-surface-2 text-muted" : "bg-accent text-accent-fg",
                        )}
                      >
                        {entry.glyph === "todo" ? (
                          <Plus className="size-3.5" strokeWidth={2.2} />
                        ) : (
                          <Send className="size-3.5" strokeWidth={2} />
                        )}
                      </span>
                    ) : entry.glyph === "agent" ? (
                      <span
                        aria-hidden="true"
                        className="hg-agent flex size-[22px] shrink-0 items-center justify-center rounded-md text-xs font-bold text-white"
                        style={
                          {
                            "--agent-color": entry.agentColor?.color || undefined,
                            "--agent-color-dark": entry.agentColor?.dark || undefined,
                          } as CSSProperties
                        }
                      >
                        {Array.from(entry.agentName ?? "?")[0]}
                      </span>
                    ) : (
                      <span
                        aria-hidden="true"
                        className="flex size-[22px] shrink-0 items-center justify-center rounded-md bg-surface-2 text-muted"
                      >
                        <Icon className="size-3.5" strokeWidth={1.8} />
                      </span>
                    )}
                    <span className={cn("min-w-0 flex-1 truncate", entry.inert && "text-muted")}>
                      {entry.label}
                    </span>
                    <span className="shrink-0 text-xs text-muted">{entry.hint}</span>
                  </div>
                </div>
              );
            })}
          </div>
          <div className="hidden gap-3.5 border-t border-border px-4 py-2.5 text-xs text-muted md:flex">
            <span>
              <Kbd>↑</Kbd> <Kbd>↓</Kbd> {t("palette.hint.choose")}
            </span>
            <span>
              <Kbd>Enter</Kbd> {t("palette.hint.run")}
            </span>
            <span>{t("palette.hint.at")}</span>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
