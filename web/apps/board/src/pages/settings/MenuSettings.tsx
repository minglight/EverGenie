import type { MenuItem } from "@evergenie/sdk-web";
import { api } from "@evergenie/sdk-web";
import { cn, showToast } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronUp, Trash2 } from "lucide-react";
import { type DragEvent, useMemo, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { withSectionTitles } from "../../components/SectionTabs";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { menuQuery } from "../../data/queries";
import { type TextKey, t } from "../../i18n/zh-TW";
import { iconFor, iconNames } from "../../lib/icons";
import { useIsDesktop } from "../../lib/media";
import { reasonOf } from "../../lib/reason";
import { TierBadge } from "../../pkg/TierBadge";
import {
  addGroup,
  deleteGroup,
  type EditResult,
  type Entry,
  fromPrefs,
  gatherSections,
  isGroup,
  itemOrder,
  moveBefore,
  moveBy,
  moveInto,
  moveOut,
  moveToTop,
  OVERVIEW_ID,
  renameGroup,
  SETTINGS_ID,
  sameHidden,
  sameTree,
  sectionFollowers,
  sectionParts,
  setGroupIcon,
  toPrefs,
  tuckItem,
  unplaced,
  untuckItem,
} from "./menuEditor";
import { menuPrefsQuery } from "./queries";
import { SettingGroup } from "./SettingRow";

const iconButton =
  "flex size-11 shrink-0 items-center justify-center rounded-lg text-muted hover:bg-hover hover:text-fg focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus disabled:cursor-not-allowed disabled:opacity-30";
const selectClass =
  "min-h-11 min-w-0 max-w-[10.5rem] rounded-lg border border-border-strong bg-bg px-2 text-[13px] text-fg outline-none focus-visible:outline-2 focus-visible:outline-focus";

interface Handlers {
  up: (id: string) => void;
  down: (id: string) => void;
  into: (id: string, group: string) => void;
  out: (id: string) => void;
  toTop: (id: string) => void;
  tuck: (id: string) => void;
  dragStart: (id: string) => void;
  dropOn: (target: string, kind: "item" | "group") => void;
  dragging: string | null;
  setDragOver: (id: string | null) => void;
  dragOver: string | null;
  draggable: boolean;
}

/** 一列的名稱：同一頁的項目（section）合成一列，用 section 的名稱（REQ-237，跟側欄一樣）。 */
function rowName(item: MenuItem): string {
  return (item.section && item.section_title) || item.title || item.id || "";
}

/** 合成一列時，下面一行小字列出它代表的分頁。 */
function PartsLine({ parts }: { parts: string[] }) {
  if (parts.length === 0) return null;
  return (
    <span className="w-full basis-full pl-[26px] text-xs text-muted">
      {t("settings.menu.sectionParts", { parts: parts.join(t("settings.menu.sectionParts.sep")) })}
    </span>
  );
}

interface GroupChoice {
  id: string;
  title: string;
}

// 「放進群組…」下拉：選了就放，下拉自己回到提示字。
function IntoGroup({
  name,
  groups,
  onPick,
}: {
  name: string;
  groups: GroupChoice[];
  onPick: (group: string) => void;
}) {
  if (groups.length === 0) return null;
  return (
    <select
      aria-label={t("settings.menu.moveInto", { name })}
      className={selectClass}
      value=""
      onChange={(e) => {
        if (e.target.value) onPick(e.target.value);
      }}
    >
      <option value="">{t("settings.menu.intoPlaceholder")}</option>
      {groups.map((g) => (
        <option key={g.id} value={g.id}>
          {g.title}
        </option>
      ))}
    </select>
  );
}

// 拖拉需要的屬性：整列可拖、可放；放在列上的規則在 dropOn。
function dragProps(id: string, kind: "item" | "group", h: Handlers) {
  if (!h.draggable) return {};
  return {
    draggable: true,
    onDragStart: (e: DragEvent) => {
      e.dataTransfer.effectAllowed = "move";
      e.dataTransfer.setData("text/plain", id);
      h.dragStart(id);
    },
    onDragEnd: () => h.setDragOver(null),
    onDragOver: (e: DragEvent) => {
      if (!h.dragging) return;
      e.preventDefault();
      h.setDragOver(id);
    },
    onDragLeave: () => h.setDragOver(null),
    onDrop: (e: DragEvent) => {
      e.preventDefault();
      h.setDragOver(null);
      h.dropOn(id, kind);
    },
  };
}

function ItemRow({
  id,
  item,
  nested,
  groups,
  parts,
  h,
}: {
  id: string;
  item: MenuItem;
  nested: boolean;
  groups: GroupChoice[];
  parts: string[];
  h: Handlers;
}) {
  const Icon = iconFor(item.icon);
  const fixed = id === SETTINGS_ID;
  const canTuck = !fixed && id !== OVERVIEW_ID;
  const name = rowName(item) || id;
  return (
    <div
      data-testid="menu-item-row"
      data-item={id}
      {...dragProps(id, "item", h)}
      className={cn(
        "flex flex-wrap items-center gap-x-2 gap-y-1 py-1 pr-2 pl-3",
        nested && "pl-9",
        h.dragOver === id && "bg-accent-soft",
        h.draggable && !fixed && "cursor-grab",
      )}
    >
      <Icon className="size-[18px] shrink-0 text-muted" aria-hidden="true" />
      <span className="min-w-0 flex-1 basis-24 truncate text-[15px] font-medium">{name}</span>
      {/* 哪一層只是標籤（官方不標），放哪裡照使用者的樹（REQ-350）。 */}
      <TierBadge tier={item.tier} />
      {fixed ? <span className="text-xs text-muted">{t("settings.menu.fixed")}</span> : null}
      <div className="ml-auto flex flex-wrap items-center justify-end gap-x-0.5 max-sm:ml-0 max-sm:w-full max-sm:justify-start max-sm:pl-7">
        <button
          type="button"
          className={iconButton}
          aria-label={t("settings.menu.up", { name })}
          onClick={() => h.up(id)}
        >
          <ChevronUp className="size-5" aria-hidden="true" />
        </button>
        <button
          type="button"
          className={iconButton}
          aria-label={t("settings.menu.down", { name })}
          onClick={() => h.down(id)}
        >
          <ChevronDown className="size-5" aria-hidden="true" />
        </button>
        {fixed ? null : <IntoGroup name={name} groups={groups} onPick={(g) => h.into(id, g)} />}
        {nested && !fixed ? (
          <button
            type="button"
            className="min-h-11 shrink-0 rounded-lg px-2.5 text-[13px] font-medium text-muted hover:bg-hover hover:text-fg focus-visible:outline-2 focus-visible:outline-focus"
            aria-label={t("settings.menu.moveOutOf", { name })}
            onClick={() => h.out(id)}
          >
            {t("settings.menu.moveOut")}
          </button>
        ) : null}
        {canTuck ? (
          <button
            type="button"
            className="min-h-11 shrink-0 rounded-lg px-2.5 text-[13px] font-medium text-muted hover:bg-hover hover:text-fg focus-visible:outline-2 focus-visible:outline-focus"
            aria-label={t("settings.menu.tuckOf", { name })}
            onClick={() => h.tuck(id)}
          >
            {t("settings.menu.tuck")}
          </button>
        ) : null}
      </div>
      <PartsLine parts={parts} />
    </div>
  );
}

function GroupBlock({
  entry,
  pool,
  groups,
  h,
  onTree,
  tree,
  skip,
  partsOf,
}: {
  entry: Entry;
  pool: Map<string, MenuItem>;
  groups: GroupChoice[];
  h: Handlers;
  onTree: (tree: Entry[]) => void;
  tree: Entry[];
  /** 合成一列時不另外畫的項目。 */
  skip: Set<string>;
  partsOf: (id: string) => string[];
}) {
  const id = entry.group ?? "";
  const [pickIcon, setPickIcon] = useState(false);
  const Icon = iconFor(entry.icon);
  const others = groups.filter((g) => g.id !== id);
  const title = entry.title ?? id;
  const kids = (entry.children ?? []).filter((c) => pool.has(c) && !skip.has(c));
  return (
    <div data-testid="menu-group" data-group={id} className="flex flex-col">
      <div
        {...dragProps(id, "group", h)}
        className={cn(
          "flex flex-wrap items-center gap-x-2 gap-y-1 bg-surface-2 py-1 pr-2 pl-2",
          h.dragOver === id && "bg-accent-soft",
          h.draggable && "cursor-grab",
        )}
      >
        <button
          type="button"
          className={iconButton}
          aria-label={t("settings.menu.groupIcon", { name: title })}
          aria-expanded={pickIcon}
          onClick={() => setPickIcon((v) => !v)}
        >
          <Icon className="size-5 text-fg" aria-hidden="true" />
        </button>
        <input
          key={title}
          aria-label={t("settings.menu.groupName")}
          defaultValue={title}
          onBlur={(e) => {
            const next = e.target.value;
            if (next.trim() === title) return;
            const res = renameGroup(tree, id, next);
            if (res.ok) onTree(res.tree);
            else e.target.value = title;
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") e.currentTarget.blur();
          }}
          className="min-h-11 min-w-0 flex-1 basis-28 rounded-lg border border-transparent bg-transparent px-2 text-[15px] font-semibold outline-none hover:border-border focus:border-border-strong focus:bg-bg focus-visible:outline-2 focus-visible:outline-focus"
        />
        <div className="ml-auto flex flex-wrap items-center justify-end gap-x-0.5 max-sm:ml-0 max-sm:w-full max-sm:justify-start max-sm:pl-7">
          <button
            type="button"
            className={iconButton}
            aria-label={t("settings.menu.up", { name: title })}
            onClick={() => h.up(id)}
          >
            <ChevronUp className="size-5" aria-hidden="true" />
          </button>
          <button
            type="button"
            className={iconButton}
            aria-label={t("settings.menu.down", { name: title })}
            onClick={() => h.down(id)}
          >
            <ChevronDown className="size-5" aria-hidden="true" />
          </button>
          {/* 群組那一列沒有「放進群組…」：選單最多兩層，群組放進群組一定被擋，給了只會失敗（WEB-105）。 */}
          <button
            type="button"
            className={iconButton}
            aria-label={`${t("settings.menu.deleteGroup")}：${title}`}
            title={t("settings.menu.deleteGroup.hint")}
            onClick={() => onTree(deleteGroup(tree, id))}
          >
            <Trash2 className="size-[18px]" aria-hidden="true" />
          </button>
        </div>
      </div>
      {pickIcon ? (
        <fieldset className="m-0 flex min-w-0 flex-wrap gap-1 border-0 px-3 py-2">
          <legend className="sr-only">{t("settings.menu.pickIcon")}</legend>
          {iconNames.map((name) => {
            const I = iconFor(name);
            return (
              <button
                key={name}
                type="button"
                aria-pressed={entry.icon === name}
                aria-label={t(`menu.icon.${name}` as TextKey)}
                onClick={() => {
                  onTree(setGroupIcon(tree, id, name));
                  setPickIcon(false);
                }}
                className={cn(
                  "flex size-11 items-center justify-center rounded-lg border focus-visible:outline-2 focus-visible:outline-focus",
                  entry.icon === name
                    ? "border-accent bg-accent-soft text-accent"
                    : "border-border text-muted hover:bg-hover",
                )}
              >
                <I className="size-5" aria-hidden="true" />
              </button>
            );
          })}
        </fieldset>
      ) : null}
      {kids.length === 0 ? (
        <p className="py-2 pr-3 pl-9 text-[13px] text-muted">{t("settings.menu.emptyGroup")}</p>
      ) : (
        kids.map((c) => {
          const item = pool.get(c);
          return item ? (
            <ItemRow key={c} id={c} item={item} nested groups={others} parts={partsOf(c)} h={h} />
          ) : null;
        })
      )}
    </div>
  );
}

// 設定 → 選單（REQ-141）：用上移／下移與下拉組選單（桌機也能拖拉），最多兩層；存了選單立刻變。
// 同一頁的項目（例 健康＝飲食、運動、身體）跟側欄一樣是一列，搬它就是整組一起搬（gatherSections，REQ-237）。
export function MenuSettings() {
  const prefsQ = useQuery(menuPrefsQuery);
  const menuQ = useQuery(menuQuery());
  const client = useQueryClient();
  const desktop = useIsDesktop();
  const [draft, setDraft] = useState<Entry[] | null>(null);
  const [hiddenDraft, setHiddenDraft] = useState<string[] | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [dragging, setDragging] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState<string | null>(null);

  // 同 section 的項目都帶上 section 的名稱：哪一項當那一列的代表都叫同一個名字（WEB-136）。
  const allItems = useMemo(() => withSectionTitles(menuQ.data?.pool ?? []), [menuQ.data]);
  const initialHidden = useMemo(() => prefsQ.data?.hidden ?? [], [prefsQ.data]);
  const hidden = hiddenDraft ?? initialHidden;
  // 收到設定的項目不在編輯器的樹上（位置留著，放回來就回原位，MNU-10）。
  const poolList = useMemo(
    () => allItems.filter((it) => !hidden.includes(it.id ?? "")),
    [allItems, hidden],
  );
  const initial = useMemo(
    () => gatherSections(fromPrefs(prefsQ.data?.tree), poolList),
    [prefsQ.data, poolList],
  );
  const tree = draft ?? initial;
  const dirty =
    (draft !== null && !sameTree(draft, initial)) ||
    (hiddenDraft !== null && !sameHidden(hiddenDraft, initialHidden));
  const pool = useMemo(() => new Map(poolList.map((it) => [it.id ?? "", it] as const)), [poolList]);

  if (prefsQ.data === undefined || menuQ.data === undefined) {
    return prefsQ.isError || menuQ.isError ? (
      <LoadFailed
        what={t("settings.loadFailed")}
        onRetry={() => {
          void prefsQ.refetch();
          void menuQ.refetch();
        }}
      />
    ) : (
      <LoadingBlock />
    );
  }

  const renderer = prefsQ.data.renderer ?? "";
  const groups: GroupChoice[] = tree
    .filter(isGroup)
    .map((g) => ({ id: g.group ?? "", title: g.title || g.group || "" }));
  const followers = sectionFollowers(itemOrder(tree), poolList);
  const partsOf = (id: string) => sectionParts(id, poolList);
  const visible = (id: string) =>
    (pool.has(id) && !followers.has(id)) || tree.some((e) => e.group === id);
  // 每次搬完都把同一頁的同伴接回剛搬的那一項後面。
  const edit = (next: Entry[], anchor?: string) => setDraft(gatherSections(next, poolList, anchor));
  const apply = (res: EditResult, anchor: string) => {
    if (res.ok) edit(res.tree, anchor);
    else if (res.reason === "third_level") showToast(t("settings.menu.maxTwoLevels"));
  };

  const persist = async (nextTree: Entry[], nextRenderer: string, nextHidden?: string[]) => {
    setSaving(true);
    setError("");
    try {
      await api.web.setMenuPrefs({
        prefs: { tree: nextTree, renderer: nextRenderer, hidden: nextHidden },
      });
      await Promise.all([
        client.invalidateQueries({ queryKey: menuPrefsQuery.queryKey }),
        client.invalidateQueries({ queryKey: ["menu"] }),
      ]);
      setDraft(null);
      setHiddenDraft(null);
      showToast(t("settings.menu.saved"));
    } catch (err) {
      setError(t("settings.menu.failed", { reason: reasonOf(err) }));
    } finally {
      setSaving(false);
    }
  };

  const h: Handlers = {
    up: (id) => edit(moveBy(tree, id, -1, visible), id),
    down: (id) => edit(moveBy(tree, id, 1, visible), id),
    into: (id, group) => apply(moveInto(tree, id, group), id),
    out: (id) => apply(moveOut(tree, id), id),
    toTop: (id) => apply(moveToTop(tree, id), id),
    tuck: (id) => setHiddenDraft(tuckItem(hidden, id, allItems)),
    dragStart: setDragging,
    dragging,
    dragOver,
    setDragOver,
    draggable: desktop,
    dropOn: (target, kind) => {
      const id = dragging;
      setDragging(null);
      if (!id || id === target) return;
      // 放在群組標題上＝放進那個群組（群組放進群組會被擋下）；放在項目上＝排在它前面。
      apply(kind === "group" ? moveInto(tree, id, target) : moveBefore(tree, id, target), id);
    },
  };

  const unplacedAll = unplaced(poolList, tree);
  const unplacedSkip = sectionFollowers(
    unplacedAll.map((it) => it.id ?? ""),
    poolList,
  );
  const missing = unplacedAll.filter((it) => !unplacedSkip.has(it.id ?? ""));
  const tuckedAll = allItems.filter((it) => hidden.includes(it.id ?? ""));
  const tuckedSkip = sectionFollowers(
    tuckedAll.map((it) => it.id ?? ""),
    allItems,
  );
  const tuckedRows = tuckedAll.filter((it) => !tuckedSkip.has(it.id ?? ""));
  return (
    <div className="flex flex-col gap-5">
      {/* 手機：同一列動作貼在畫面底部（拇指碰得到、改到清單下面也不用捲回頂端）；桌機照舊在最上面（UXC-5，P6 收尾）。 */}
      <div className="flex flex-wrap items-center gap-2 max-md:sticky max-md:bottom-0 max-md:z-10 max-md:order-last max-md:-mx-4 max-md:border-t max-md:border-border max-md:bg-bg max-md:px-4 max-md:py-3">
        <ActionButton
          tone="primary"
          busy={saving}
          disabled={!dirty}
          onClick={() => void persist(toPrefs(tree), renderer, hidden)}
        >
          {t(saving ? "settings.menu.saving" : "settings.menu.save")}
        </ActionButton>
        <ActionButton tone="outline" disabled={saving} onClick={() => void persist([], renderer)}>
          {t("settings.menu.reset")}
        </ActionButton>
        <ActionButton
          tone="outline"
          disabled={saving}
          onClick={() => setDraft(addGroup(tree, t("settings.menu.newGroup")))}
        >
          {t("settings.menu.addGroup")}
        </ActionButton>
        {dirty ? (
          <span role="status" className="text-[13px] font-medium text-attention-fg">
            {t("settings.menu.dirty")}
          </span>
        ) : null}
      </div>
      {error ? (
        <p role="alert" className="text-[13px] leading-snug text-danger">
          {error}
        </p>
      ) : null}

      <SettingGroup title={t("settings.menu.tree")}>
        <div data-testid="menu-tree" className="flex flex-col divide-y divide-border">
          {tree.map((e) => {
            if (isGroup(e)) {
              return (
                <GroupBlock
                  key={e.group}
                  entry={e}
                  pool={pool}
                  groups={groups}
                  h={h}
                  tree={tree}
                  onTree={setDraft}
                  skip={followers}
                  partsOf={partsOf}
                />
              );
            }
            const item = followers.has(e.item ?? "") ? undefined : pool.get(e.item ?? "");
            return item ? (
              <ItemRow
                key={e.item}
                id={e.item ?? ""}
                item={item}
                nested={false}
                groups={groups}
                parts={partsOf(e.item ?? "")}
                h={h}
              />
            ) : null;
          })}
        </div>
        {desktop ? (
          <p className="px-4 py-2.5 text-xs text-muted">{t("settings.menu.dragHint")}</p>
        ) : null}
      </SettingGroup>

      <SettingGroup title={t("settings.menu.tucked")}>
        <p className="px-4 pt-3 text-[13px] text-muted">{t("settings.menu.tucked.hint")}</p>
        {tuckedRows.length === 0 ? (
          <p className="px-4 pb-3.5 text-[13px] text-muted">{t("settings.menu.tucked.empty")}</p>
        ) : (
          tuckedRows.map((it) => {
            const Icon = iconFor(it.icon);
            const name = rowName(it);
            return (
              <div
                key={it.id}
                data-testid="menu-tucked-row"
                data-item={it.id}
                className="flex items-center gap-x-2 py-1 pr-2 pl-3"
              >
                <Icon className="size-[18px] shrink-0 text-muted" aria-hidden="true" />
                <span className="min-w-0 flex-1 truncate text-[15px] font-medium">{name}</span>
                <button
                  type="button"
                  className="min-h-11 shrink-0 rounded-lg px-2.5 text-[13px] font-medium text-muted hover:bg-hover hover:text-fg focus-visible:outline-2 focus-visible:outline-focus"
                  aria-label={t("settings.menu.untuckOf", { name })}
                  onClick={() => setHiddenDraft(untuckItem(hidden, it.id ?? "", allItems))}
                >
                  {t("settings.menu.untuck")}
                </button>
              </div>
            );
          })
        )}
      </SettingGroup>

      <SettingGroup title={t("settings.menu.unplaced")}>
        <p className="px-4 pt-3 text-[13px] text-muted">{t("settings.menu.unplaced.hint")}</p>
        {missing.length === 0 ? (
          <p className="px-4 pb-3.5 text-[13px] text-muted">{t("settings.menu.unplaced.empty")}</p>
        ) : (
          missing.map((it) => {
            const Icon = iconFor(it.icon);
            const name = rowName(it);
            return (
              <div
                key={it.id}
                data-testid="menu-unplaced-row"
                data-item={it.id}
                className="flex flex-wrap items-center gap-x-2 gap-y-1 py-1 pr-2 pl-3"
              >
                <Icon className="size-[18px] shrink-0 text-muted" aria-hidden="true" />
                <span className="min-w-0 flex-1 basis-24 truncate text-[15px] font-medium">
                  {name}
                </span>
                <div className="ml-auto flex flex-wrap items-center justify-end gap-x-1">
                  <IntoGroup name={name} groups={groups} onPick={(g) => h.into(it.id ?? "", g)} />
                  <button
                    type="button"
                    className="min-h-11 shrink-0 rounded-lg px-2.5 text-[13px] font-medium text-muted hover:bg-hover hover:text-fg focus-visible:outline-2 focus-visible:outline-focus"
                    aria-label={`${t("settings.menu.toTop")}：${name}`}
                    onClick={() => h.toTop(it.id ?? "")}
                  >
                    {t("settings.menu.toTop")}
                  </button>
                </div>
                <PartsLine parts={partsOf(it.id ?? "")} />
              </div>
            );
          })
        )}
      </SettingGroup>

      <div className="flex flex-col gap-1.5 px-1">
        <p data-testid="menu-renderer" className="text-[13px] text-muted">
          {t("settings.menu.renderer", { name: renderer || t("settings.menu.renderer.default") })}
        </p>
        {renderer ? (
          <div>
            <ActionButton
              tone="outline"
              disabled={saving}
              onClick={() => void persist(toPrefs(initial), "", hidden)}
            >
              {t("settings.menu.renderer.reset")}
            </ActionButton>
          </div>
        ) : null}
        <p className="text-xs text-muted">{t("settings.menu.renderer.hint")}</p>
      </div>
    </div>
  );
}
