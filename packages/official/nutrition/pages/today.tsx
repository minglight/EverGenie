import { EmptyLine, LoadFailed, Spinner } from "@evergenie/ui";
import { dateStr, dayTotals } from "./lib/nutrition";
import { GoalBar, useLoad } from "./lib/parts";
import { loadGoal, loadMeals } from "./lib/store";

// 今天頁的卡片（slot core.overview.cards）：今天的熱量、蛋白質對目標，點了去「飲食」（REQ-114、128）。
// 今天一餐都還沒記：宣告「現在是空的」（EmptyLine），今天頁的手機版縮成一行，其他地方照舊整張卡。
export default function Today() {
  const meals = useLoad(loadMeals);
  const goal = useLoad(loadGoal);
  const ready = meals.data !== undefined && (goal.data !== undefined || goal.error !== "");
  const totals = dayTotals(meals.data ?? [], dateStr(new Date()));
  const g = goal.data;
  // 讀不到：整張卡換成一行白話提示＋「再試一次」（今天頁會把好幾張合成一條），不放連結（按鈕不能包在連結裡）。
  if (meals.error && meals.data === undefined) {
    return (
      <LoadFailed
        what="今天吃了什麼"
        onRetry={() => {
          meals.reload();
          goal.reload();
        }}
        variant="inline"
        className="rounded-[18px] border border-border bg-surface p-4"
      />
    );
  }
  const card = (
    <a
      href="/p/nutrition/log"
      data-testid="nutrition-today"
      className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4 text-fg no-underline"
    >
      <div className="flex items-baseline justify-between gap-2">
        <h2 className="font-semibold">今天吃了什麼</h2>
        {ready ? <span className="text-sm text-muted">{totals.count} 餐</span> : null}
      </div>
      {!ready ? (
        <Spinner label="載入中" />
      ) : g && (g.kcal > 0 || g.protein > 0) ? (
        <>
          <GoalBar
            testId="today-kcal"
            label="熱量"
            value={totals.kcal}
            target={g.kcal}
            unit="大卡"
          />
          <GoalBar
            testId="today-protein"
            label="蛋白質"
            value={totals.protein}
            target={g.protein}
            unit="公克"
          />
        </>
      ) : (
        <p className="text-sm text-muted">
          還沒設目標。今天累計 {Math.round(totals.kcal)} 大卡、蛋白質 {Math.round(totals.protein)}{" "}
          公克。
        </p>
      )}
    </a>
  );
  if (ready && totals.count === 0) {
    return (
      <EmptyLine
        label="飲食"
        text="今天還沒記"
        action={{ label: "＋記一餐", href: "/p/nutrition/log" }}
        data-testid="nutrition-today-empty"
      >
        {card}
      </EmptyLine>
    );
  }
  return card;
}
