import { cn, EmptyState, LoadFailed, MoreMenu, Spinner } from "@evergenie/ui";
import { useState } from "react";
import { canEdit, canEditTab } from "./lib/caps";
import {
  chosenFilter,
  type FilterEntry,
  type FilterOption,
  filterEntries,
  filterHref,
  filterOptions,
  gridOf,
  HABITS_HREF,
  mineHref,
  nextOutfitHint,
  type Person,
  personaFromSearch,
  type ShowFilter,
  showroomPeople,
  T,
  walletHint,
} from "./lib/closet";
import { editHref } from "./lib/edit";
import {
  card,
  EmptyCloset,
  OutfitTile,
  PersonPicker,
  useLoad,
  usePickedPersona,
  VariantSwitches,
} from "./lib/parts";
import {
  loadCapabilities,
  loadHabitsAvailable,
  loadMineAvailable,
  loadPeople,
  loadState,
  loadUnadopted,
} from "./lib/store";

const POLL_MS = 3000;

// 展示間（CLO-15，作者 2026-11-03）：頂端點數，右上「⋯」裡「新增角色」「加一套衣服」（DEV-156 U-15；DEV-143：開到核心的角色頁——角色清單的
// 「新增角色」、那位角色的「衣服」分頁；closet-mine 沒開就不顯示）；上面一排角色頭像，一次只看一位（網址 ?persona=、記得上次看誰）：
// 所有衣服（擁有的真圖、沒擁有的剪影加鎖頭〔開發家沒兌換的也是真圖，CLO-27〕，點進詳情用點數兌換）。
// 設計中的衣服不在這頁（CLO-37）：一般家看不到；開發家（有 edit 能力）在那位角色的一段裡有一行到角色頁「修改」分頁的入口
// （有還沒採用的照片才出現），採用、評論都在那裡（CLO-35）。只有開發家才有的東西（說明、篩選、入口）一律看能力，不問是哪種家。
// 頁面不算點數、不判斷能不能兌換，只讀套件的程式算好的 person 與 state（REQ-125）。
// 隱藏版（CLO-17）開著（時段內或按了開關）時，擁有的衣服顯示隱藏版的圖；開關跟衣櫃同一排，放在點數下面。
export default function Showroom({ search = "" }: { search?: string }) {
  const people = useLoad(loadPeople, POLL_MS);
  const state = useLoad(loadState, POLL_MS);
  const mine = useLoad(loadMineAvailable, POLL_MS * 10);
  const canAdd = mine.data === true;
  // 點數在「習慣」賺：習慣套件能用才連過去（CLO-24）；讀不到當沒開、不連。
  const habits = useLoad(loadHabitsAvailable, POLL_MS * 10);
  // 能不能修改衣服（edit，開發家才有；CLO-37）：沒兌換的也看得到真圖的說明（CLO-27）、篩選（CLO-29）、到「修改」的入口都看它。
  // 能力不會中途變，讀一次就好；還沒讀到、讀不到都當不能。
  // 能力跟著輪詢：程式這一輪才寫好（或開發家剛換）時不用重新整理就跟上（CLO-37）。
  const caps = useLoad(loadCapabilities, POLL_MS);
  const edit = canEdit(caps.data);
  const editTab = canEditTab(caps.data);
  const error = people.error || state.error;
  const list = showroomPeople(people.data ?? [], edit);
  const [current, pick] = usePickedPersona(list, search);
  const person = list.find((p) => p.persona === current);
  const empty = people.data !== undefined && list.length === 0;
  const ready = people.data !== undefined && !empty;
  // 開發家的篩選（CLO-29）：一般家沒有這排（options 是空的）；網址 ?filter=。
  const options = filterOptions(edit, mine.data, people.data ?? []);
  const [wanted, setFilter] = useQueryChoice(search, "filter", "all");
  const filter = chosenFilter(wanted, options);
  // 「再 N 點」：正在看某位角色時照那位的下一套算，篩選這類總覽照全部（DEV-157）。
  const focus = filter === "all" ? person : undefined;
  const nextHint = nextOutfitHint(people.data ?? [], state.data, focus);

  return (
    <section
      data-testid="showroom"
      // 測試用：closet-mine 開沒開讀到了沒（loading｜true｜false），按鈕顯示與否要等它有結果才算數。
      data-mine={mine.data === undefined ? "loading" : String(mine.data)}
      // 測試用：能力讀到了沒（loading｜true｜false）；「沒有開發家的東西」要等它有結果才算數。
      data-edit={caps.data === undefined && !caps.error ? "loading" : String(edit)}
      className="flex flex-col gap-4"
    >
      {/* 「新增角色」「加一套衣服」收在右上的「⋯」（DEV-156 U-15）：展示間是來看衣服、兌換的，做衣服的鈕不搶主位。 */}
      <div className="flex items-start gap-1">
        <div data-testid="closet-points" className={cn(card, "min-w-0 flex-1")}>
          <div className="flex items-baseline justify-between gap-2">
            <h2 className="font-semibold">{T.points}</h2>
            <span className="text-2xl font-bold" data-testid="closet-points-value">
              {state.data ? state.data.points : state.error ? "－" : "…"}
              <span className="ml-1 text-sm font-medium text-muted">{T.pointUnit}</span>
            </span>
          </div>
          {state.data !== undefined && walletHint(state.data) ? (
            <p data-testid="closet-wallet-hint" className="text-xs text-muted">
              {walletHint(state.data)}
            </p>
          ) : null}
          {nextHint ? (
            <p data-testid="closet-next-hint" className="text-sm font-medium">
              {nextHint}
            </p>
          ) : null}
          <p data-testid="showroom-hint" className="text-sm text-muted">
            {edit ? T.showroomHintDev : T.showroomHint}
          </p>
          {habits.data === true ? (
            <a
              href={HABITS_HREF}
              data-testid="closet-to-habits"
              className="inline-flex min-h-11 items-center self-start text-sm font-medium text-accent no-underline"
            >
              {T.toHabits}
            </a>
          ) : null}
        </div>
        {canAdd ? (
          <MoreMenu
            label={T.showroomMenu}
            title={T.showroomMenuTitle}
            testId="showroom-add"
            className="text-muted hover:bg-hover hover:text-fg"
            items={[
              {
                key: "new",
                label: T.newCharacter,
                href: mineHref("new"),
                testId: "showroom-new-character",
              },
              {
                key: "outfit",
                label: T.addOutfit,
                href: mineHref("person", current),
                testId: "showroom-add-outfit",
              },
            ]}
          />
        ) : null}
      </div>
      {/* 開發家的篩選那排緊接在點數卡下面（一打開就看得到，不用捲過開關與按鈕；DEV-156），然後才是隱藏版開關、角色頭像列。
          讀到衣服、而且有衣服才畫。 */}
      {ready ? <FilterRow options={options} value={filter} onChange={setFilter} /> : null}
      <VariantSwitches onChanged={people.reload} />
      {error ? (
        <LoadFailed
          what={T.loadWhat}
          auto
          onRetry={() => {
            people.reload();
            state.reload();
          }}
          variant={people.data === undefined ? "block" : "inline"}
          className={people.data === undefined ? "py-8" : undefined}
        />
      ) : null}
      {people.data === undefined ? (
        // 讀不到時只留上面那條提示，不畫空的角色列（會以為衣服不見了）。
        people.error ? null : (
          <div className="flex justify-center py-8">
            <Spinner label={T.loading} />
          </div>
        )
      ) : empty ? (
        // 一套衣服都沒有：照角色包的狀態說明（CLO-25）；讀不到狀態就用原本的說明。
        <EmptyCloset
          wanted={personaFromSearch(search)}
          fallback={{ title: T.showroomEmptyTitle, hint: T.showroomEmptyHint }}
          onSynced={people.reload}
        />
      ) : (
        <>
          {filter !== "all" ? (
            <Filtered
              filter={filter}
              entries={filterEntries(people.data ?? [], filter, mine.data === true)}
              editTab={editTab}
            />
          ) : null}
          {filter !== "all" ? null : <PersonPicker people={list} current={current} onPick={pick} />}
          {filter === "all" && person ? (
            <PersonSection key={person.persona} person={person} edit={edit} editTab={editTab} />
          ) : null}
        </>
      )}
    </section>
  );
}

/**
 * 記在網址的一個選擇（?<key>=；等於 fallback 時不寫）：replaceState 帶著原本的 history.state（FEN-29），
 * 重新整理、從別頁按上一頁回來都是同一個畫面（UXC-3）。
 */
function useQueryChoice(
  search: string,
  key: string,
  fallback: string,
): [string, (v: string) => void] {
  const [value, setValue] = useState(() => new URLSearchParams(search).get(key) ?? fallback);
  const set = (v: string) => {
    setValue(v);
    try {
      const q = new URLSearchParams(window.location.search);
      if (v === fallback) q.delete(key);
      else q.set(key, v);
      const s = q.toString();
      window.history.replaceState(
        window.history.state,
        "",
        `${window.location.pathname}${s ? `?${s}` : ""}${window.location.hash}`,
      );
    } catch {
      // 換不了網址也不影響畫面。
    }
  };
  return [value, set];
}

/** 開發家的篩選那一排（CLO-29）：單選的小按鈕、後面帶數量；手機一排、自己橫向捲動（頁面不橫向捲動）。 */
function FilterRow({
  options,
  value,
  onChange,
}: {
  options: FilterOption[];
  value: ShowFilter;
  onChange: (v: ShowFilter) => void;
}) {
  if (options.length === 0) return null;
  return (
    <fieldset
      data-testid="showroom-filter"
      aria-label={T.filterGroup}
      className="-mx-1 flex min-w-0 gap-2 overflow-x-auto border-0 px-1 py-1"
    >
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          data-testid={`showroom-filter-${o.value}`}
          aria-pressed={o.value === value}
          onClick={() => onChange(o.value)}
          className={`inline-flex min-h-11 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-3 text-sm font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus ${
            o.value === value
              ? "border-accent bg-accent-soft text-accent"
              : "border-border bg-surface text-fg"
          }`}
        >
          {o.label}
          <span data-testid="showroom-filter-count" className="text-xs text-muted">
            {o.count}
          </span>
        </button>
      ))}
    </fieldset>
  );
}

/** 選了篩選：跨所有角色列出符合的衣服，每一格標角色名。 */
function Filtered({ filter, entries, editTab }: { filter: ShowFilter; entries: FilterEntry[]; editTab: boolean }) {
  if (entries.length === 0) return <EmptyState title={T.filterNone} />;
  const hint =
    filter === "promotable"
      ? editTab
        ? T.filterHintPromotable
        : T.filterHintPromotableNoTab
      : filter === "mine"
        ? T.filterHintMine
        : "";
  return (
    <div data-testid="showroom-filtered" data-filter={filter} className={card}>
      {hint ? <p className="text-sm text-muted">{hint}</p> : null}
      <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        {entries.map((e) => (
          <OutfitTile
            key={e.outfit.record}
            outfit={e.outfit}
            href={filterHref(filter, e, editTab)}
            owner={e.person.name}
            edit
          />
        ))}
      </ul>
    </div>
  );
}

/**
 * 一位角色的衣服格子（不含設計中的，gridOf）。能修改衣服的家（開發家）多一行到那位「修改」分頁的入口（CLO-37），
 * 只有設計中衣服的角色說一句還沒有上架的衣服；一般家只有設計中衣服的角色本來就不列（showroomPeople）。
 */
function PersonSection({ person, edit, editTab }: { person: Person; edit: boolean; editTab: boolean }) {
  const grid = gridOf(person);
  return (
    <div data-testid="person-section" data-persona={person.persona} className={card}>
      <h2 className="min-w-0 truncate text-lg font-semibold">{person.name}</h2>
      {editTab ? <ToEdit persona={person.persona} /> : null}
      {grid.length > 0 ? (
        <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3">
          {grid.map((o) => (
            <OutfitTile key={o.record} outfit={o} edit={edit} />
          ))}
        </ul>
      ) : (
        <p className="text-sm text-muted">{T.noShownOutfits}</p>
      )}
    </div>
  );
}

/**
 * 開發家：到這位角色「修改」分頁的入口（CLO-37），有還沒採用的照片、或照片都採用了等上架的整套（CLO-38）才出現，寫幾張、幾套；
 * 張數跟「修改」分頁打開「開發中」看到的一樣（同一份 closet.photo、同一個算法）。讀不到就不顯示（不擋展示間）。父層用 key 換角色，換人就重讀。
 */
function ToEdit({ persona }: { persona: string }) {
  const u = useLoad(() => loadUnadopted(persona), POLL_MS);
  if (!u.data || (u.data.count === 0 && u.data.ready === 0)) return null;
  return (
    <a
      href={editHref(persona, u.data)}
      data-testid="showroom-to-edit"
      data-count={u.data.count}
      data-ready={u.data.ready}
      className="inline-flex min-h-11 items-center self-start text-sm font-medium text-accent no-underline"
    >
      {T.toEdit(u.data.count, u.data.ready)}
    </a>
  );
}
