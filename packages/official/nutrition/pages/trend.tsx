import { Button, LoadFailed, Segmented, Spinner } from "@evergenie/ui";
import { useEffect, useState } from "react";
import {
  dateStr,
  fmt,
  GOAL_MODES,
  type Goal,
  type GoalMode,
  recentAverage,
  trendDays,
} from "./lib/nutrition";
import { useLoad } from "./lib/parts";
import { loadGoal, loadMeals, reasonOf, saveGoal } from "./lib/store";

const DAYS = 14;

// 飲食趨勢：近 14 天熱量長條（沒記錄的日子留空，不畫 0）、近 7 天平均、目標編輯（REQ-128）。
export default function Trend() {
  const meals = useLoad(loadMeals);
  const goal = useLoad(loadGoal);
  const days = trendDays(meals.data ?? [], dateStr(new Date()), DAYS);
  const avg = recentAverage(days, 7);
  const target = goal.data && goal.data.kcal > 0 ? goal.data.kcal : 0;
  const top = Math.max(target * 1.1, ...days.map((d) => d.kcal ?? 0), 1);

  // 餐點讀不到：整頁只放白話提示＋「再試一次」，不畫空的長條圖與「還沒有記錄」（會以為資料不見了）。
  if (meals.error && meals.data === undefined) {
    return (
      <section data-testid="nutrition-trend" className="flex flex-col gap-4">
        <LoadFailed
          what="飲食趨勢"
          onRetry={() => {
            meals.reload();
            goal.reload();
          }}
        />
      </section>
    );
  }

  return (
    <section data-testid="nutrition-trend" className="flex flex-col gap-4">
      <div className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4">
        <h2 className="font-semibold">近 {DAYS} 天熱量</h2>
        {meals.data === undefined && !meals.error ? (
          <Spinner label="載入中" />
        ) : (
          <>
            <div
              className="relative flex h-40 items-end gap-1"
              role="img"
              aria-label={`近 ${DAYS} 天每天的熱量`}
            >
              {target > 0 ? (
                <div
                  data-testid="trend-goal-line"
                  className="pointer-events-none absolute inset-x-0 border-t-2 border-dashed border-muted"
                  style={{ bottom: `${(target / top) * 100}%` }}
                />
              ) : null}
              {days.map((d) => (
                <div
                  key={d.date}
                  data-testid="trend-day"
                  data-date={d.date}
                  data-empty={d.kcal === null ? "true" : "false"}
                  title={d.kcal === null ? `${d.date} 沒有記錄` : `${d.date} ${fmt(d.kcal)} 大卡`}
                  className="flex h-full min-w-0 flex-1 flex-col justify-end"
                >
                  {d.kcal === null ? (
                    <div className="h-px w-full bg-border" />
                  ) : (
                    <div
                      className={`w-full rounded-t ${target > 0 && d.kcal > target ? "bg-attention" : "bg-accent"}`}
                      style={{ height: `${Math.max(2, (d.kcal / top) * 100)}%` }}
                    />
                  )}
                </div>
              ))}
            </div>
            <div className="flex gap-1 text-[10px] text-muted">
              {days.map((d) => (
                <span key={d.date} className="min-w-0 flex-1 text-center">
                  {Number(d.date.slice(8))}
                </span>
              ))}
            </div>
            <p className="text-xs text-muted">
              {target > 0
                ? `虛線是每天 ${fmt(target)} 大卡的目標，橘色是超過目標的日子。`
                : "還沒設目標。"}
              沒有記錄的日子留空，不算 0。
            </p>
          </>
        )}
        {meals.error ? (
          <LoadFailed what="飲食趨勢" onRetry={meals.reload} variant="inline" />
        ) : null}
      </div>

      <div
        data-testid="trend-average"
        className="flex flex-col gap-1 rounded-[18px] border border-border bg-surface p-4"
      >
        <h2 className="font-semibold">近 7 天平均</h2>
        {avg ? (
          <>
            <p>
              熱量 <span className="font-semibold">{fmt(Math.round(avg.kcal))}</span> 大卡 · 蛋白質{" "}
              <span className="font-semibold">{fmt(Math.round(avg.protein))}</span> 公克
            </p>
            <p className="text-xs text-muted">只算有記錄的 {avg.days} 天。</p>
          </>
        ) : (
          <p className="text-sm text-muted">近 7 天還沒有記錄。</p>
        )}
      </div>

      <GoalEditor
        goal={goal.data}
        loading={goal.data === undefined && !goal.error}
        loadError={goal.error}
        onSaved={goal.reload}
      />
    </section>
  );
}

const EMPTY: Goal = { mode: "維持", kcal: 2000, protein: 100, carb: 0, fat: 0 };

function GoalEditor({
  goal,
  loading,
  loadError,
  onSaved,
}: {
  goal: Goal | null | undefined;
  loading: boolean;
  loadError: string;
  onSaved: () => void;
}) {
  const [mode, setMode] = useState<GoalMode>("維持");
  const [vals, setVals] = useState({ kcal: "", protein: "", carb: "", fat: "" });
  const [state, setState] = useState<"" | "saving" | "saved">("");
  const [error, setError] = useState("");
  const [field, setField] = useState("");

  // 讀到目標（或確定還沒有）就填進表單；之後使用者改的不會被覆蓋。
  const [filled, setFilled] = useState(false);
  useEffect(() => {
    if (filled || goal === undefined) return;
    const g = goal ?? EMPTY;
    setMode((GOAL_MODES as readonly string[]).includes(g.mode) ? (g.mode as GoalMode) : "維持");
    setVals({
      kcal: String(g.kcal),
      protein: String(g.protein),
      carb: String(g.carb),
      fat: String(g.fat),
    });
    setFilled(true);
  }, [goal, filled]);

  useEffect(() => {
    if (filled && location.hash === "#goal") {
      document.getElementById("goal")?.scrollIntoView();
    }
  }, [filled]);

  const submit = async () => {
    const out: Record<string, number> = {};
    for (const [k, label] of [
      ["kcal", "熱量"],
      ["protein", "蛋白質"],
      ["carb", "碳水"],
      ["fat", "脂肪"],
    ] as const) {
      const raw = vals[k].trim();
      const n = Number(raw);
      if (raw === "" || !Number.isFinite(n) || n < 0) {
        setField(k);
        setError(`${label}要填 0 以上的數字`);
        return;
      }
      out[k] = n;
    }
    setField("");
    setError("");
    setState("saving");
    try {
      await saveGoal(
        {
          mode,
          kcal: out.kcal ?? 0,
          protein: out.protein ?? 0,
          carb: out.carb ?? 0,
          fat: out.fat ?? 0,
        },
        goal != null,
      );
      setState("saved");
      onSaved();
    } catch (e) {
      setState("");
      setError(reasonOf(e));
    }
  };

  const rows: { key: keyof typeof vals; label: string; hint: string }[] = [
    { key: "kcal", label: "熱量（大卡）", hint: "每天" },
    { key: "protein", label: "蛋白質（公克）", hint: "每天" },
    { key: "carb", label: "碳水（公克）", hint: "0 代表不設目標" },
    { key: "fat", label: "脂肪（公克）", hint: "0 代表不設目標" },
  ];

  return (
    <form
      id="goal"
      data-testid="goal-editor"
      className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <h2 className="font-semibold">目標</h2>
      {loading ? (
        <Spinner label="載入中" />
      ) : (
        <>
          {goal === null ? (
            <p className="text-sm text-muted">還沒設目標，填好按「存目標」。</p>
          ) : null}
          <Segmented
            ariaLabel="目標類型"
            options={GOAL_MODES.map((m) => ({ value: m, label: m }))}
            value={mode}
            onChange={(m) => {
              setMode(m);
              setState("");
            }}
          />
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {rows.map((r) => (
              <label key={r.key} className="flex flex-col gap-1 text-sm">
                <span className="font-medium">{r.label}</span>
                <input
                  inputMode="decimal"
                  enterKeyHint="done"
                  value={vals[r.key]}
                  aria-invalid={field === r.key}
                  onChange={(e) => {
                    setVals({ ...vals, [r.key]: e.target.value });
                    setState("");
                  }}
                  className="min-h-11 rounded-lg border border-border bg-bg px-3 text-base"
                />
                <span className="text-xs text-muted">{r.hint}</span>
              </label>
            ))}
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" variant="primary" disabled={state === "saving"}>
              {state === "saving" ? "儲存中…" : "存目標"}
            </Button>
            {state === "saved" ? (
              <span role="status" className="text-sm text-muted">
                已儲存
              </span>
            ) : null}
          </div>
        </>
      )}
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : loadError ? (
        <LoadFailed what="飲食目標" onRetry={onSaved} variant="inline" />
      ) : null}
    </form>
  );
}
