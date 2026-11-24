// 挑器材（FIT-17，DEV-146）：剛才在用、常用、先挑部位再挑器材、搜尋（找不到可以直接新增）。
// 選了哪台、看哪個部位存在瀏覽器（FIT-18）：切到有氧、歷史或離開頁面再回來都還在。
import { Button, EmptyState } from "@evergenie/ui";
import { useState } from "react";
import {
  type BodyPart,
  type Equip,
  frequentExcept,
  groupByPart,
  matchEquipment,
  type Remembered,
  recentInWorkout,
  type Workout,
} from "./workout";

const KEY_SEL = "eg.fitness.equipment.v1";
const KEY_PART = "eg.fitness.part.v1";

/** 瀏覽器存不了（私密模式、被擋）就當沒存，頁面照常用。 */
export function loadRemembered(): Remembered | null {
  try {
    const raw = localStorage.getItem(KEY_SEL);
    if (!raw) return null;
    const v = JSON.parse(raw) as Partial<Remembered>;
    return typeof v.name === "string" && typeof v.at === "number"
      ? { name: v.name, at: v.at }
      : null;
  } catch {
    return null;
  }
}

export function saveRemembered(name: string): void {
  try {
    if (name) localStorage.setItem(KEY_SEL, JSON.stringify({ name, at: Date.now() }));
    else localStorage.removeItem(KEY_SEL);
  } catch {
    // 存不了就算了
  }
}

function loadPart(): string {
  try {
    return localStorage.getItem(KEY_PART) ?? "";
  } catch {
    return "";
  }
}

function savePart(p: string): void {
  try {
    if (p) localStorage.setItem(KEY_PART, p);
    else localStorage.removeItem(KEY_PART);
  } catch {
    // 存不了就算了
  }
}

const inputCls = "min-h-11 w-full rounded-lg border border-border bg-bg px-3 text-base";

function Chip({
  name,
  selected,
  onPick,
}: {
  name: string;
  selected: boolean;
  onPick: (n: string) => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={() => onPick(name)}
      className={`min-h-11 rounded-full border px-4 text-sm ${selected ? "border-accent bg-accent text-accent-fg" : "border-border bg-bg hover:bg-hover"}`}
    >
      {name}
    </button>
  );
}

function Row({
  title,
  testid,
  names,
  sel,
  onPick,
}: {
  title: string;
  testid: string;
  names: string[];
  sel: string;
  onPick: (n: string) => void;
}) {
  if (names.length === 0) return null;
  return (
    <div className="flex flex-col gap-2" data-testid={testid}>
      <span id={`${testid}-title`} className="text-xs font-medium text-muted">
        {title}
      </span>
      <fieldset
        className="m-0 min-w-0 border-0 p-0 flex flex-wrap gap-2"
        aria-labelledby={`${testid}-title`}
      >
        {names.map((n) => (
          <Chip key={n} name={n} selected={sel === n} onPick={onPick} />
        ))}
      </fieldset>
    </div>
  );
}

export function EquipmentPicker({
  workouts,
  equipment,
  active,
  sel,
  busy,
  onPick,
  onAdd,
  onManage,
  managing,
}: {
  workouts: Workout[];
  equipment: Equip[];
  active: Workout | null;
  sel: string;
  busy: boolean;
  onPick: (name: string) => void;
  /** 搜尋找不到：用搜尋的字新增並選它（REQ-304）。 */
  onAdd: (name: string) => Promise<boolean>;
  onManage: () => void;
  /** 管理器材的抽屜開著沒（按鈕是開關）。 */
  managing: boolean;
}) {
  const [q, setQ] = useState("");
  const groups = groupByPart(equipment);
  const [part, setPartState] = useState<string>(() => {
    const p = loadPart();
    return groups.some((g) => g.part === p) ? p : "";
  });
  const setPart = (p: string) => {
    const next = p === part ? "" : p;
    setPartState(next);
    savePart(next);
  };

  const recent = recentInWorkout(active, equipment);
  const freq = frequentExcept(workouts, equipment, recent);
  const results = q.trim() ? matchEquipment(equipment, q) : [];
  const inPart = groups.find((g) => g.part === part)?.items ?? [];
  const pick = (n: string) => {
    onPick(n);
    setQ("");
  };

  return (
    <div className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4">
      <div className="flex items-center justify-between gap-2">
        <h2 className="font-semibold">{sel ? "換器材" : "挑器材"}</h2>
        <Button variant="ghost" aria-expanded={managing} onClick={onManage}>
          管理器材
        </Button>
      </div>
      {equipment.length === 0 ? (
        <EmptyState title="還沒有器材" hint="按右上角「管理器材」新增第一個，例如「啞鈴彎舉」" />
      ) : (
        <>
          <Row title="剛才在用" testid="equipment-recent" names={recent} sel={sel} onPick={pick} />
          <Row
            title={recent.length > 0 ? "常用" : "常用器材"}
            testid="equipment-frequent"
            names={freq}
            sel={sel}
            onPick={pick}
          />
          {groups.length > 1 ? (
            <div className="flex flex-col gap-2">
              <span className="text-xs font-medium text-muted">依部位</span>
              <div className="flex flex-wrap gap-2" data-testid="equipment-parts">
                {groups.map((g) => (
                  <button
                    key={g.part}
                    type="button"
                    aria-pressed={part === g.part}
                    aria-label={`部位：${g.part}（${g.items.length} 台）`}
                    onClick={() => setPart(g.part)}
                    className={`min-h-11 rounded-lg border px-3 text-sm ${part === g.part ? "border-accent font-semibold text-accent" : "border-border bg-bg hover:bg-hover"}`}
                  >
                    {g.part}
                    <span className="ml-1 text-xs text-muted">{g.items.length}</span>
                  </button>
                ))}
              </div>
              {part && inPart.length > 0 ? (
                <fieldset
                  aria-label={`${part}的器材`}
                  className="m-0 min-w-0 border-0 flex flex-wrap gap-2 rounded-lg bg-bg p-2"
                  data-testid="equipment-in-part"
                  data-part={part as BodyPart}
                >
                  {inPart.map((e) => (
                    <Chip key={e.id} name={e.name} selected={sel === e.name} onPick={pick} />
                  ))}
                </fieldset>
              ) : null}
            </div>
          ) : null}
          <label className="flex flex-col gap-1 text-sm">
            <span className="font-medium">搜尋器材或肌群</span>
            <input
              data-testid="equipment-search"
              type="search"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              className={inputCls}
              placeholder="例：背、二頭"
            />
          </label>
          {q.trim() ? (
            results.length === 0 ? (
              <div className="flex flex-wrap items-center gap-2">
                <p className="text-sm text-muted">找不到「{q.trim()}」。</p>
                <Button
                  variant="secondary"
                  data-testid="equipment-add-from-search"
                  disabled={busy}
                  onClick={() =>
                    void onAdd(q).then((ok) => {
                      if (ok) setQ("");
                    })
                  }
                >
                  新增「{q.trim()}」
                </Button>
              </div>
            ) : (
              <fieldset
                className="m-0 min-w-0 border-0 p-0 flex flex-wrap gap-2"
                aria-label="搜尋結果"
                data-testid="equipment-results"
              >
                {results.map((e) => (
                  <Chip key={e.id} name={e.name} selected={sel === e.name} onPick={pick} />
                ))}
              </fieldset>
            )
          ) : null}
        </>
      )}
    </div>
  );
}
