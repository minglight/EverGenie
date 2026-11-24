import { Button, EmptyState, LoadFailed, Spinner } from "@evergenie/ui";
import { useRef, useState } from "react";
import { dayClass, dayMark, dayWord } from "./lib/dayLook";
import {
  dateStr,
  doneLine,
  doneToday,
  type Habit,
  type HabitStatus,
  howDone,
  type PendingMark,
  pointLine,
  progressText,
  ruleSections,
  statusId,
  subLabel,
  type Wallet,
  weekCells,
  withPendingMarks,
} from "./lib/habits";
import { NewHabit, RuleButton, useLoad } from "./lib/parts";
import {
  checkin,
  loadAgentNames,
  loadClosetAvailable,
  loadHabits,
  loadStatuses,
  loadWallet,
  reasonOf,
  undoCheckin,
} from "./lib/store";

// 打卡鈕：還沒打是主色實心（跟白色格子分得開）；打過變淡綠底綠字，一眼看出做完了。
const doneBtn = "border border-ok bg-ok-soft text-ok hover:bg-ok-soft hover:opacity-100";
// 主打卡鈕打過就停用，但要看得清楚是「做完了」，不是灰掉。
const doneMainBtn = `${doneBtn} disabled:opacity-100`;
// 子項目一列一顆，不能都是實心主色（一個畫面一個主要動作）：還沒打用淡紫底紫字，一樣不是白的。
const todoBtn = "border border-accent bg-accent-soft text-accent hover:bg-accent-soft";
const card = "flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4";
const POLL_MS = 3000;
// 打卡後的小字「記下了」留 2 秒就收（DEV-156 U-20：不留一句「正在更新進度…」讓人不知道好了沒）。
const NOTE_MS = 2000;
// 先改掉的畫面最多留這麼久（程式約 2 秒重算、頁面每 3 秒讀一次）。
const PENDING_MS = 10_000;
// 展示間（衣櫃套件的頁面）：點數在那裡換衣服。
const SHOWROOM_HREF = "/p/closet/showroom";

// 習慣：錢包點數、「約一個新習慣」、每個進行中的習慣一張卡、已結束的收起來（REQ-124）。
// 卡片只放三層（DEV-142）：名稱＋怎樣算完成／點數的下一步各一行（DEV-156 U-14）、本週七格、打卡（或自動標記）；完整規則按「怎麼算」跳出來看。
// 頁面不算點數：status、wallet 由套件的程式算好（打卡後約 2 秒更新），頁面每 3 秒重新讀一次。
export default function List() {
  const habits = useLoad(loadHabits, POLL_MS);
  const statuses = useLoad(loadStatuses, POLL_MS);
  const wallet = useLoad(loadWallet, POLL_MS);
  const names = useLoad(loadAgentNames);
  const closet = useLoad(loadClosetAvailable);
  const today = dateStr(new Date());
  const all = habits.data ?? [];
  const active = all.filter((h) => h.status === "active");
  const ended = all.filter((h) => h.status === "ended");
  const error = habits.error || statuses.error || wallet.error;

  return (
    <section data-testid="habits-list" className="flex flex-col gap-4">
      <WalletCard
        wallet={wallet.data}
        failed={wallet.error !== ""}
        showroom={closet.data === true}
        outfits={wallet.data?.outfitsReady === true}
      />
      <NewHabit names={names.data} />
      {/* 讀不到：白話提示＋「再試一次」；習慣本身讀不到時不畫下面的空狀態（會以為資料不見了）。這頁每 3 秒自己再讀。 */}
      {error ? (
        <LoadFailed
          what="習慣"
          auto
          onRetry={() => {
            habits.reload();
            statuses.reload();
            wallet.reload();
          }}
          variant={habits.data === undefined ? "block" : "inline"}
          className={habits.data === undefined ? "py-8" : undefined}
        />
      ) : null}
      {habits.data === undefined ? (
        habits.error ? null : (
          <div className="flex justify-center py-8">
            <Spinner label="載入中" />
          </div>
        )
      ) : all.length === 0 ? (
        <EmptyState
          title="還沒有約定習慣"
          hint="按上面的「約一個新習慣」，跟秘書聊聊你想養成什麼；約好會出現在這裡"
        />
      ) : (
        <>
          {active.length === 0 ? (
            <p className="text-sm text-muted">現在沒有進行中的習慣。</p>
          ) : (
            <ul className="flex flex-col gap-3">
              {active.map((h) => (
                <li key={h.id}>
                  <HabitCard
                    habit={h}
                    status={statuses.data?.[statusId(h.id)]}
                    agentName={names.data?.[h.agent] ?? h.agent}
                    today={today}
                    onChanged={() => {
                      statuses.reload();
                      for (const ms of [2500, 5000]) setTimeout(statuses.reload, ms);
                    }}
                  />
                </li>
              ))}
            </ul>
          )}
          {ended.length > 0 ? (
            <details data-testid="habits-ended" className={card}>
              <summary className="min-h-11 cursor-pointer py-2 font-semibold">
                已結束（{ended.length}）
              </summary>
              <ul className="flex flex-col gap-2 pt-1">
                {ended.map((h) => (
                  <li key={h.id} data-testid="habit-ended" data-habit={h.id}>
                    <a
                      href={`/p/habits/detail?id=${h.id}`}
                      className="flex min-h-11 flex-col justify-center text-fg no-underline"
                    >
                      <span className="font-medium">{h.name}</span>
                      <span className="text-xs text-muted">
                        累計達標 {statuses.data?.[statusId(h.id)]?.achievedWeeks ?? 0} 週 · 賺到{" "}
                        {statuses.data?.[statusId(h.id)]?.earnedPoints ?? 0} 點
                      </span>
                    </a>
                  </li>
                ))}
              </ul>
            </details>
          ) : null}
        </>
      )}
    </section>
  );
}

// 錢包卡：點數拿去展示間換衣服（HAB-22）；衣櫃套件沒開就不放連結（點了會是空頁）。
// 點數還換不到衣服（錢包的 outfits_ready，HAB-23；DEV-156 U-09）：連結照樣在（展示間會說衣服包怎麼裝），但不寫「換衣服」。
function WalletCard({
  wallet,
  failed,
  showroom,
  outfits,
}: {
  wallet: Wallet | undefined;
  /** 點數讀不到：數字畫「－」、不一直寫載入中（上面已經有一條提示）。 */
  failed: boolean;
  showroom: boolean;
  /** 點數真的換得到衣服（錢包的 outfits_ready）。 */
  outfits: boolean;
}) {
  return (
    <div data-testid="habits-wallet" className={card}>
      <div className="flex items-baseline justify-between gap-2">
        <h2 className="font-semibold">我的點數</h2>
        <span className="text-2xl font-bold" data-testid="wallet-points">
          {wallet ? wallet.points : failed ? "－" : "…"}
          <span className="ml-1 text-sm font-medium text-muted">點</span>
        </span>
      </div>
      <p className="text-xs text-muted">
        {wallet
          ? `累計賺到 ${wallet.earned} 點，已經花掉 ${wallet.spent} 點；點數只增不減。`
          : failed
            ? "現在讀不到點數。"
            : "載入中"}
      </p>
      {showroom ? (
        <a
          href={SHOWROOM_HREF}
          data-testid="wallet-showroom"
          className="inline-flex min-h-11 items-center self-start text-sm font-medium text-accent underline"
        >
          {outfits ? "到展示間換衣服" : "到展示間看看"}
        </a>
      ) : null}
    </div>
  );
}

function HabitCard({
  habit,
  status: real,
  agentName,
  today,
  onChanged,
}: {
  habit: Habit;
  status: HabitStatus | undefined;
  agentName: string;
  today: string;
  onChanged: () => void;
}) {
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  // 剛送出、程式還沒重算的那一下：先把畫面改掉（DEV-156 U-20）。base 是送出當下那份進度的時間，
  // 程式重算好（computedAt 變了）就自動換回真的；最多留 PENDING_MS，免得程式沒重算時一直顯示猜的。
  // 同一份進度底下連續按好幾下（勾了甲再勾乙）時，動作照順序都套上去，前一個不會彈回去。
  const [pending, setPending] = useState<{ marks: PendingMark[]; base: string } | null>(null);
  const pendingStamp = useRef(0);
  const subs = habit.subtasks;
  const status =
    real && pending && real.computedAt === pending.base
      ? withPendingMarks(habit, real, today, pending.marks)
      : real;
  const done = doneToday(status, today);
  const auto = howDone(habit) === "auto";

  const run = async (key: string, job: () => Promise<void>, text: string, mark: PendingMark) => {
    setBusy(key);
    setError("");
    try {
      await job();
      if (real) {
        const base = real.computedAt;
        setPending((p) => ({ marks: p && p.base === base ? [...p.marks, mark] : [mark], base }));
        // 最後一次動作之後 PENDING_MS 還沒重算好才收（中間又按了就重新算時間）。
        const stamp = ++pendingStamp.current;
        setTimeout(() => {
          if (pendingStamp.current === stamp) setPending(null);
        }, PENDING_MS);
      }
      setNote(text);
      onChanged();
      setTimeout(() => setNote((n) => (n === text ? "" : n)), NOTE_MS);
    } catch (e) {
      setError(reasonOf(e));
    } finally {
      setBusy("");
    }
  };

  return (
    <div data-testid="habit-card" data-habit={habit.id} className={card}>
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 flex-col gap-0.5">
          <a
            href={`/p/habits/detail?id=${habit.id}`}
            data-testid="habit-name"
            className="inline-flex min-h-11 items-center text-lg font-semibold text-fg no-underline"
          >
            {habit.name}
            <span aria-hidden="true" className="ml-1 text-muted">
              ›
            </span>
          </a>
          <p className="text-xs text-muted">跟 {agentName} 約的</p>
        </div>
        <RuleButton habit={habit} sections={ruleSections(habit, status)} />
      </div>
      <dl className="flex flex-col gap-1 text-sm">
        <div className="flex gap-2">
          <dt className="shrink-0 font-semibold">完成</dt>
          <dd data-testid="habit-done-rule">{doneLine(habit)}</dd>
        </div>
        <div className="flex gap-2">
          <dt className="shrink-0 font-semibold">點數</dt>
          <dd data-testid="habit-point-rule">{pointLine(status)}</dd>
        </div>
      </dl>
      <div className="flex flex-col gap-1.5">
        <span data-testid="habit-progress-text" className="text-sm font-medium">
          {progressText(habit.cadence, status)}
        </span>
        <WeekDays habit={habit} status={status} today={today} />
      </div>

      {subs.length > 0 ? (
        <ul data-testid="habit-subs" className="flex flex-col gap-2">
          {subs.map((raw) => {
            const name = raw.split("=")[0]?.trim() ?? raw;
            const t = status?.todaySubs.find((s) => s.name === name);
            const auto = t?.done && t.how === "auto";
            return (
              <li key={raw} className="flex items-center justify-between gap-2">
                <span className="text-sm">{subLabel(raw)}</span>
                {auto ? (
                  <span data-testid="sub-auto" className="text-sm text-muted">
                    已自動算到
                  </span>
                ) : (
                  <Button
                    data-testid="sub-toggle"
                    aria-pressed={!!t?.done}
                    variant="secondary"
                    className={t?.done ? doneBtn : todoBtn}
                    disabled={busy !== ""}
                    aria-label={t?.done ? `已做 ${name}（再按一下撤銷）` : `打卡 ${name}`}
                    onClick={() =>
                      run(
                        `sub:${name}`,
                        () =>
                          t?.done
                            ? undoCheckin(habit.id, today, name)
                            : checkin(habit.id, today, name),
                        t?.done ? `撤銷了「${name}」` : `記下了「${name}」`,
                        { kind: t?.done ? "undo" : "checkin", sub: name },
                      )
                    }
                  >
                    {t?.done ? "已做 ✓" : "打卡"}
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          {auto ? (
            <span data-testid="habit-auto" className="text-sm font-medium">
              {done ? "今天已自動算到 ✓" : "自動算，不用按"}
            </span>
          ) : (
            <Button
              data-testid="habit-checkin"
              variant="primary"
              className={done ? doneMainBtn : undefined}
              disabled={busy !== "" || done}
              onClick={() =>
                run("checkin", () => checkin(habit.id, today), "記下了", { kind: "checkin" })
              }
            >
              {done ? "今天已打卡 ✓" : "今天打卡"}
            </Button>
          )}
          {auto && !done ? (
            <button
              type="button"
              data-testid="habit-checkin"
              disabled={busy !== ""}
              onClick={() =>
                run("checkin", () => checkin(habit.id, today), "補上了", { kind: "checkin" })
              }
              className="min-h-11 px-2 text-sm text-muted underline disabled:opacity-50"
            >
              沒記到？手動補今天
            </button>
          ) : null}
          {(status?.todayManual ?? 0) > 0 ? (
            <Button
              data-testid="habit-undo"
              variant="ghost"
              disabled={busy !== ""}
              onClick={() =>
                run("undo", () => undoCheckin(habit.id, today), "撤銷了", { kind: "undo" })
              }
            >
              撤銷今天
            </Button>
          ) : null}
        </div>
      )}
      {note ? (
        <p role="status" className="text-xs text-muted">
          {note}
        </p>
      ) : null}
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

const WEEK_NAMES = ["一", "二", "三", "四", "五", "六", "日"];

// 本週七格：長相照 dayLook（做了 ✓、斷了 ✕、跳過 －），今天加框；只看不點（要補打或撤銷點名字進月曆，HAB-17）。
function WeekDays({
  habit,
  status,
  today,
}: {
  habit: Habit;
  status: HabitStatus | undefined;
  today: string;
}) {
  const cells = weekCells(habit, status, today);
  return (
    <ol data-testid="habit-week" aria-label="本週" className="grid grid-cols-7 gap-1">
      {cells.map((c, i) => (
        <li
          key={c.date}
          data-testid="habit-week-day"
          data-date={c.date}
          data-state={c.state}
          className={`flex h-9 flex-col items-center justify-center rounded-lg text-xs leading-none ${dayClass(c)} ${c.date === today ? "outline-2 outline-focus" : ""}`}
        >
          <span aria-hidden="true">{WEEK_NAMES[i]}</span>
          <span className="sr-only">{`週${WEEK_NAMES[i]} ${dayWord(c)}`}</span>
          <span aria-hidden="true" className="mt-0.5 h-3">
            {dayMark(c)}
          </span>
        </li>
      ))}
    </ol>
  );
}
