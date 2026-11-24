import { EmptyLine, LoadFailed, Spinner } from "@evergenie/ui";
import { dateStr, mondayOf } from "./lib/dates";
import { useLoad } from "./lib/parts";
import { loadWorkouts } from "./lib/store";
import { activeWorkout, isStale, weekCardioCount, weekStrengthCount } from "./lib/workout";

// 今天頁的卡片（slot core.overview.cards）：本週完成的重訓與有氧次數、進行中的訓練（忘了收操的寫明白）；點了去「運動」（REQ-114、127）。體重在身體套件自己的卡片。
// 這週還沒練、也沒有進行中的訓練：宣告「現在是空的」（EmptyLine），今天頁的手機版縮成一行，其他地方照舊整張卡。
export default function Today() {
  const workouts = useLoad(loadWorkouts);
  const today = dateStr(new Date());
  const ready = workouts.data !== undefined;
  const week = weekStrengthCount(workouts.data ?? [], mondayOf(today));
  const cardio = weekCardioCount(workouts.data ?? [], mondayOf(today));
  const active = activeWorkout(workouts.data ?? []);
  // 讀不到：整張卡換成一行白話提示＋「再試一次」（今天頁會把好幾張合成一條），不放連結（按鈕不能包在連結裡）。
  if (workouts.error && !ready) {
    return (
      <LoadFailed
        what="今天的運動"
        onRetry={workouts.reload}
        variant="inline"
        className="rounded-[18px] border border-border bg-surface p-4"
      />
    );
  }
  const card = (
    <a
      href="/p/fitness/log"
      data-testid="fitness-today"
      className="flex flex-col gap-2 rounded-[18px] border border-border bg-surface p-4 text-fg no-underline"
    >
      <div className="flex items-baseline justify-between gap-2">
        <h2 className="font-semibold">今天的運動</h2>
        {ready ? (
          <span data-testid="today-week" className="text-sm text-muted">
            本週完成重訓 {week} 次 · 有氧 {cardio} 次
          </span>
        ) : null}
      </div>
      {!ready ? (
        <Spinner label="載入中" />
      ) : active && isStale(active, new Date()) ? (
        // 忘了收操：不再一直寫「進行中」，說清楚是上次那筆、點進去就能收操（FIT-15）。
        <p data-testid="today-stale" className="text-sm font-medium">
          {active.sets.length > 0
            ? `上次的訓練（${active.sets.length} 組）還沒收操，點這裡收操`
            : "上次開了訓練還沒收操，點這裡收操"}
        </p>
      ) : active ? (
        <p data-testid="today-active" className="text-sm font-medium">
          進行中：{active.sets.length} 組
        </p>
      ) : (
        <p className="text-sm text-muted">現在沒有進行中的訓練。</p>
      )}
    </a>
  );
  if (ready && week === 0 && cardio === 0 && !active) {
    return (
      <EmptyLine
        label="運動"
        text="這週還沒練"
        action={{ label: "＋記一筆", href: "/p/fitness/log" }}
        data-testid="fitness-today-empty"
      >
        {card}
      </EmptyLine>
    );
  }
  return card;
}
