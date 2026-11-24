import { Button, EmptyState, Field, LoadFailed, Markdown, Segmented, Spinner } from "@evergenie/ui";
import { useEffect, useRef, useState } from "react";
import { dayClass, dayMark, dayWord } from "./lib/dayLook";
import {
  CADENCES,
  cadenceText,
  dateStr,
  fieldsFromInput,
  type Habit,
  type HabitInput,
  type HabitStatus,
  monthCells,
  nextFormValue,
  nextPointText,
  progressText,
  ruleSections,
  STATUSES,
  validateForm,
  weekLabel,
  weekStartLabel,
} from "./lib/habits";
import { DiscussLink, RuleList, useLoad, WeekBar } from "./lib/parts";
import {
  checkin,
  loadAgentNames,
  loadHabit,
  loadStatus,
  patchHabit,
  reasonOf,
  undoCheckin,
} from "./lib/store";

const card = "flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4";
const inputCls = "min-h-11 w-full rounded-lg border border-border bg-bg px-3 text-base";

function idFromSearch(search: string): string {
  return new URLSearchParams(search).get("id") ?? "";
}

// 習慣詳情：近 8 週的週結果、約定時聊出來的內容、編輯（存檔前用跟程式同一套規則檢查）（REQ-124）。
export default function Detail({ search }: { pkg: string; page: string; search: string }) {
  const id = idFromSearch(search);
  const habit = useLoad(() => loadHabit(id));
  const status = useLoad(() => loadStatus(id), 3000);
  const names = useLoad(loadAgentNames);
  const h = habit.data;

  return (
    <section data-testid="habit-detail" className="flex flex-col gap-4">
      {habit.error ? (
        <LoadFailed
          what="這個習慣"
          onRetry={habit.reload}
          variant={habit.data === undefined ? "block" : "inline"}
          className={habit.data === undefined ? "py-8" : undefined}
        />
      ) : null}
      {!id ? (
        <EmptyState title="沒有指定習慣" hint="從習慣清單點一個習慣進來" />
      ) : habit.data === undefined && !habit.error ? (
        <div className="flex justify-center py-8">
          <Spinner label="載入中" />
        </div>
      ) : h === null ? (
        <EmptyState
          tone="missing"
          title={`找不到習慣 ${id}`}
          hint="它可能已經被刪掉；賺到的點數還在錢包裡"
        />
      ) : h ? (
        <>
          <Summary habit={h} agentName={names.data?.[h.agent] ?? h.agent} status={status.data} />
          <Calendar habit={h} status={status.data} onChanged={status.reload} />
          <div data-testid="habit-rules-card" className={card}>
            <h2 className="font-semibold">怎麼算</h2>
            <RuleList sections={ruleSections(h, status.data)} />
            <DiscussLink habit={h} />
          </div>
          <Weeks status={status.data} />
          <Notes habit={h} />
          <Edit key={h.id} habit={h} onSaved={habit.reload} />
        </>
      ) : null}
    </section>
  );
}

function Summary({
  habit,
  agentName,
  status,
}: {
  habit: Habit;
  agentName: string;
  status: HabitStatus | null | undefined;
}) {
  const st = status;
  return (
    <div data-testid="habit-summary" className={card}>
      <div>
        {/* 第一層標題是外殼的頁名「習慣」；這裡是它下面的一段（一頁一個 h1）。 */}
        <h2 className="text-xl font-semibold">{habit.name}</h2>
        <p className="text-xs text-muted">
          跟 {agentName} 約的 · {cadenceText(habit)} · 從 {habit.start} 開始
          {habit.status === "ended" ? ` · 已結束${habit.ended ? `（${habit.ended}）` : ""}` : ""}
        </p>
      </div>
      {st ? (
        <>
          <div className="flex items-baseline justify-between gap-2 text-sm">
            <span data-testid="detail-progress-text" className="font-medium">
              {progressText(habit.cadence, st)}
            </span>
            <span className="text-muted">連續 {st.streakWeeks} 週</span>
          </div>
          <WeekBar done={st.done} need={st.need} />
          <p className="text-xs text-muted">
            累計達標 {st.achievedWeeks} 週 · 這個習慣賺到 {st.earnedPoints} 點
            {st.bonusPoints > 0 ? `（含連續加分 ${st.bonusPoints} 點）` : ""}
            {habit.status === "active" ? ` · ${nextPointText(st)}` : ""}
            {habit.status === "active" && habit.bonusWeeks > 0
              ? ` · 再連續 ${st.nextBonusWeeks} 週有連續加分`
              : ""}
          </p>
        </>
      ) : (
        <p className="text-sm text-muted">還在算（程式算完會出現在這裡）。</p>
      )}
    </div>
  );
}

// 月曆：每天一格，點一下打卡、再點撤銷；顏色是程式算好的（status），頁面只畫（HAB-14）。
function Calendar({
  habit,
  status,
  onChanged,
}: {
  habit: Habit;
  status: HabitStatus | null | undefined;
  onChanged: () => void;
}) {
  const now = new Date();
  const today = dateStr(now);
  const [cursor, setCursor] = useState({ y: now.getFullYear(), m: now.getMonth() + 1 });
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const move = (delta: number) =>
    setCursor(({ y, m }) => {
      const d = new Date(y, m - 1 + delta, 1);
      return { y: d.getFullYear(), m: d.getMonth() + 1 };
    });
  const cells = monthCells(cursor.y, cursor.m, habit, status ?? undefined, today);
  const atNow = cursor.y === now.getFullYear() && cursor.m === now.getMonth() + 1;
  const tap = async (date: string, undo: boolean) => {
    setBusy(date);
    setError("");
    try {
      await (undo ? undoCheckin(habit.id, date) : checkin(habit.id, date));
      onChanged();
    } catch (e) {
      setError(reasonOf(e));
    } finally {
      setBusy("");
    }
  };
  const navBtn = "min-h-11 min-w-11 rounded-lg border border-border px-2 disabled:opacity-30";
  return (
    <div data-testid="habit-calendar" className={card}>
      <div className="flex items-center justify-between gap-2">
        <button type="button" aria-label="上個月" className={navBtn} onClick={() => move(-1)}>
          ‹
        </button>
        <h2 data-testid="calendar-title" className="font-semibold">
          {cursor.y} 年 {cursor.m} 月
        </h2>
        <button
          type="button"
          aria-label="下個月"
          className={navBtn}
          disabled={atNow}
          onClick={() => move(1)}
        >
          ›
        </button>
      </div>
      <div className="grid grid-cols-7 gap-1 text-center text-xs text-muted" aria-hidden="true">
        {["一", "二", "三", "四", "五", "六", "日"].map((w) => (
          <span key={w}>{w}</span>
        ))}
      </div>
      <div className="grid grid-cols-7 gap-1">
        {cells.map((c, i) =>
          c ? (
            <button
              key={c.date}
              type="button"
              data-testid="calendar-day"
              data-date={c.date}
              data-state={c.state}
              disabled={!c.clickable || busy !== ""}
              aria-label={`${c.date} ${dayWord(c)}`}
              onClick={() => void tap(c.date, c.canUndo)}
              className={`flex aspect-square min-h-10 flex-col items-center justify-center rounded-lg text-sm leading-none ${dayClass(c)} ${c.date === today ? "outline-2 outline-focus" : ""}`}
            >
              {c.day}
              <span aria-hidden="true" className="mt-0.5 h-3 text-xs">
                {dayMark(c)}
              </span>
            </button>
          ) : (
            // biome-ignore lint/suspicious/noArrayIndexKey: 月初的空格沒有別的識別
            <span key={i} />
          ),
        )}
      </div>
      <p className="text-xs text-muted">
        點一下打卡，再點一下撤銷。✓ 做了；－
        跳過（還在容許內；淡灰是這週第一次，淡紫虛線是第二次起）；✕
        沒做、斷了。自動來源記的不能在這裡撤銷。
      </p>
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

const CELL: Record<string, string> = {
  hit: "bg-ok text-accent-fg",
  miss: "bg-attention text-on-attention",
  open: "border border-border-strong bg-surface-2 text-fg",
  skip: "bg-surface-2 text-muted",
};

function Weeks({ status }: { status: HabitStatus | null | undefined }) {
  const weeks = status?.weeks ?? [];
  if (weeks.length === 0) return null;
  return (
    <div data-testid="habit-weeks" className={card}>
      <h2 className="font-semibold">近 {weeks.length} 週</h2>
      <ul className="grid grid-cols-4 gap-2">
        {weeks.map((w) => (
          <li
            key={w.week}
            data-testid="week-cell"
            data-week={w.week}
            data-result={w.result}
            title={`${w.week} 起這週：${weekLabel(w.result)}`}
            className={`flex min-h-11 flex-col items-center justify-center rounded-lg px-1 py-1.5 text-center text-xs ${CELL[w.result] ?? CELL.open}`}
          >
            <span className="font-semibold">{weekStartLabel(w.week)}</span>
            <span>{weekLabel(w.result)}</span>
          </li>
        ))}
      </ul>
      <p className="text-xs text-muted">
        格子上的日期是那一週的週一；「不計」是週中才約定的第一週。
      </p>
    </div>
  );
}

function Notes({ habit }: { habit: Habit }) {
  const rows: [string, string][] = [
    ["我想成為", habit.identity],
    ["提示（做完什麼之後做）", habit.cue],
    ["兩分鐘版本", habit.twoMin],
  ];
  const shown = rows.filter(([, v]) => v);
  if (shown.length === 0 && !habit.body.trim()) return null;
  return (
    <div data-testid="habit-notes" className={card}>
      {shown.map(([label, value]) => (
        <div key={label} className="flex flex-col gap-0.5">
          <span className="text-xs font-semibold text-muted">{label}</span>
          <span className="text-sm">{value}</span>
        </div>
      ))}
      {habit.body.trim() ? <Markdown>{habit.body}</Markdown> : null}
    </div>
  );
}

function inputOf(h: Habit): HabitInput {
  return {
    name: h.name,
    cadence: h.cadence,
    target: h.target,
    missDays: h.missDays,
    weekSkips: h.weekSkips,
    mode: h.mode,
    grace: h.grace,
    status: h.status,
    sources: h.sources,
    subtasks: h.subtasks,
    allowMiss: h.allowMiss,
    identity: h.identity,
    cue: h.cue,
    twoMin: h.twoMin,
    bonusWeeks: h.bonusWeeks,
  };
}

function NumberField({
  label,
  hint,
  value,
  onChange,
  testId,
}: {
  label: string;
  hint?: string;
  value: number;
  onChange: (n: number) => void;
  testId: string;
}) {
  return (
    <Field label={label} hint={hint}>
      {(c) => (
        <input
          {...c}
          data-testid={testId}
          type="number"
          inputMode="numeric"
          value={Number.isNaN(value) ? "" : value}
          onChange={(e) => onChange(e.target.value === "" ? Number.NaN : Number(e.target.value))}
          className={inputCls}
        />
      )}
    </Field>
  );
}

function Edit({ habit, onSaved }: { habit: Habit; onSaved: () => void }) {
  const [v, setV] = useState<HabitInput>(() => inputOf(habit));
  const [error, setError] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  // 即時更新會一直換 habit 物件：內容沒變就不動；主人正在改、還沒存的不洗掉（PKG-49 補）。
  const base = useRef<HabitInput>(inputOf(habit));
  useEffect(() => {
    const next = inputOf(habit);
    if (JSON.stringify(next) === JSON.stringify(base.current)) return;
    const prev = base.current;
    base.current = next;
    setV((cur) => nextFormValue(cur, prev, next));
  }, [habit]);
  const set = (p: Partial<HabitInput>) => setV((cur) => ({ ...cur, ...p }));

  const save = async () => {
    setMsg("");
    const problem = validateForm(v);
    if (problem) return setError(problem);
    setError("");
    setBusy(true);
    try {
      const fields = fieldsFromInput(v);
      const unset: string[] = [];
      if (v.cadence !== "weekly") unset.push("target");
      if (v.status === "ended" && habit.status !== "ended") fields.ended = dateStr(new Date());
      if (v.status === "active") unset.push("ended");
      await patchHabit(habit.id, fields, unset);
      setMsg("已儲存（點數會在幾秒內重新計算）");
      onSaved();
    } catch (e) {
      setError(reasonOf(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form
      data-testid="habit-edit"
      className={card}
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <h2 className="font-semibold">修改約定</h2>
      <p className="text-xs text-muted">
        這裡只改常用的幾項；連漏幾天、自動來源、子項目、連續幾週加分這些規則，按上面的「跟秘書討論修改」跟秘書聊。改了立刻重算，已經賺到的點數不會少。
      </p>
      <Field label="名稱">
        {(c) => (
          <input
            {...c}
            data-testid="edit-name"
            value={v.name}
            onChange={(e) => set({ name: e.target.value })}
            className={inputCls}
          />
        )}
      </Field>
      <div className="flex flex-col gap-1.5">
        <span className="text-[13px] font-semibold">頻率</span>
        <Segmented
          ariaLabel="頻率"
          options={CADENCES.map((o) => ({ ...o }))}
          value={v.cadence as "daily" | "weekly"}
          onChange={(cadence) => set({ cadence })}
        />
      </div>
      {v.cadence === "weekly" ? (
        <NumberField
          label="每週幾次"
          hint="1 到 7"
          value={v.target}
          onChange={(target) => set({ target })}
          testId="edit-target"
        />
      ) : null}
      <Field label="提示（做完什麼之後做）" hint="選填，例：刷完牙之後">
        {(c) => (
          <input
            {...c}
            data-testid="edit-cue"
            value={v.cue}
            onChange={(e) => set({ cue: e.target.value })}
            className={inputCls}
          />
        )}
      </Field>
      <Field label="兩分鐘版本" hint="選填，例：換上運動鞋就算">
        {(c) => (
          <input
            {...c}
            data-testid="edit-two-min"
            value={v.twoMin}
            onChange={(e) => set({ twoMin: e.target.value })}
            className={inputCls}
          />
        )}
      </Field>
      <div className="flex flex-col gap-1.5">
        <span className="text-[13px] font-semibold">狀態</span>
        <Segmented
          ariaLabel="狀態"
          options={STATUSES.map((o) => ({ ...o }))}
          value={v.status as "active" | "ended"}
          onChange={(status) => set({ status })}
        />
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" variant="primary" disabled={busy} data-testid="edit-save">
          存檔
        </Button>
        {msg ? (
          <span role="status" data-testid="edit-saved" className="text-sm text-muted">
            {msg}
          </span>
        ) : null}
      </div>
      {error ? (
        <p role="alert" data-testid="edit-error" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
    </form>
  );
}
