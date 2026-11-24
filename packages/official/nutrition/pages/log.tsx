import { api } from "@evergenie/sdk-web";
import { Button, EmptyState, LoadFailed, MediaViewer, Spinner } from "@evergenie/ui";
import { useEffect, useState } from "react";
import {
  canGoNext,
  dateFromSearch,
  dateStr,
  dayTotals,
  emptyDayHint,
  type Fav,
  favFieldsFromMeal,
  fmt,
  isDateStr,
  type Meal,
  mealFieldsFromFav,
  mealsOfDay,
  noGoalText,
  shiftDate,
} from "./lib/nutrition";
import { GoalBar, useLoad } from "./lib/parts";
import {
  addFav,
  addMeal,
  archiveRecord,
  loadFavs,
  loadGoal,
  loadMeals,
  MEAL,
  reasonOf,
} from "./lib/store";

// 飲食：某一天吃了什麼、對目標的進度、常吃餐點、跟記錄秘書說話（REQ-128）。
export default function Log({ search }: { pkg: string; page: string; search: string }) {
  const today = dateStr(new Date());
  const [date, setDate] = useState(() => dateFromSearch(search, new Date()));
  const meals = useLoad(loadMeals);
  const goal = useLoad(loadGoal);
  const favs = useLoad(loadFavs);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [big, setBig] = useState("");
  const agent = useNutritionAgent();

  const go = (next: string) => {
    setDate(next);
    // 網址帶日期，重新整理與書籤回到同一天（FEN-4）；不新增歷史紀錄。
    history.replaceState(history.state, "", next === today ? location.pathname : `?date=${next}`);
  };

  const reloadAll = () => {
    meals.reload();
    favs.reload();
  };

  const logFav = async (fav: Fav) => {
    setBusy(true);
    setError("");
    try {
      await addMeal(mealFieldsFromFav(fav, new Date()));
      if (date !== today) go(today);
      meals.reload();
    } catch (e) {
      setError(reasonOf(e));
    } finally {
      setBusy(false);
    }
  };

  const loading = meals.data === undefined && !meals.error;
  const dayMeals = mealsOfDay(meals.data ?? [], date);
  const totals = dayTotals(meals.data ?? [], date);
  const g = goal.data;

  // 餐點讀不到：整頁只放白話提示＋「再試一次」，不畫「這天還沒有記錄」（會以為資料不見了）。
  if (meals.error && meals.data === undefined) {
    return (
      <section data-testid="nutrition-log" className="flex flex-col gap-4">
        <LoadFailed
          what="飲食記錄"
          onRetry={() => {
            meals.reload();
            goal.reload();
            favs.reload();
          }}
        />
      </section>
    );
  }

  return (
    <section data-testid="nutrition-log" className="flex flex-col gap-4">
      <DateNav date={date} today={today} onChange={go} />

      <div
        data-testid="nutrition-progress"
        className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4"
      >
        <div className="flex items-baseline justify-between gap-2">
          <h2 className="font-semibold">{date === today ? "今天" : date}的進度</h2>
          <span className="text-sm text-muted">{totals.count} 餐</span>
        </div>
        {goal.data === undefined && !goal.error ? (
          <Spinner label="載入中" />
        ) : g && (g.kcal > 0 || g.protein > 0) ? (
          <>
            <GoalBar
              testId="nutrition-kcal"
              label="熱量"
              value={totals.kcal}
              target={g.kcal}
              unit="大卡"
            />
            <GoalBar
              testId="nutrition-protein"
              label="蛋白質"
              value={totals.protein}
              target={g.protein}
              unit="公克"
            />
            {g.carb > 0 || g.fat > 0 ? (
              <p className="text-xs text-muted">
                碳水 {fmt(totals.carb)}
                {g.carb > 0 ? ` / ${fmt(g.carb)}` : ""} 公克 · 脂肪 {fmt(totals.fat)}
                {g.fat > 0 ? ` / ${fmt(g.fat)}` : ""} 公克
              </p>
            ) : null}
          </>
        ) : goal.data === undefined ? null : (
          <div className="flex flex-col gap-2 text-sm">
            <p data-testid="nutrition-no-goal" className="text-muted">
              {noGoalText(date, today, totals)}
            </p>
            <a
              href="/p/nutrition/trend#goal"
              className="inline-flex min-h-11 items-center font-medium text-accent underline"
            >
              設定目標
            </a>
          </div>
        )}
        {goal.error ? <LoadFailed what="飲食目標" onRetry={goal.reload} variant="inline" /> : null}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <a
          href="/p/nutrition/trend"
          className="inline-flex min-h-11 items-center rounded-lg px-3 font-medium text-accent underline"
        >
          看趨勢
        </a>
      </div>

      {favs.data && favs.data.length > 0 ? (
        <div className="flex flex-col gap-2" data-testid="nutrition-favs">
          <h2 className="text-sm font-semibold text-muted">常吃的（點一下記一筆）</h2>
          <div className="flex flex-wrap gap-2">
            {favs.data.map((f) => (
              <button
                key={f.id}
                type="button"
                data-testid="nutrition-fav"
                disabled={busy}
                onClick={() => void logFav(f)}
                className="min-h-11 rounded-full border border-border bg-surface px-4 text-sm hover:bg-hover disabled:opacity-50"
              >
                記一筆「{f.name}」 · {fmt(f.kcal)} 大卡
              </button>
            ))}
          </div>
        </div>
      ) : null}

      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
      {meals.error ? <LoadFailed what="飲食記錄" onRetry={reloadAll} variant="inline" /> : null}

      {loading ? (
        <div className="flex justify-center py-8">
          <Spinner label="載入中" />
        </div>
      ) : dayMeals.length === 0 ? (
        <EmptyState
          title="這天還沒有記錄"
          hint={emptyDayHint(agent?.name ?? "", (favs.data?.length ?? 0) > 0)}
        />
      ) : (
        <ul className="flex flex-col gap-3" data-testid="nutrition-meals">
          {dayMeals.map((m) => (
            <MealCard key={m.id} meal={m} onOpenPhoto={setBig} onChanged={reloadAll} />
          ))}
        </ul>
      )}

      <MediaViewer
        items={big ? [{ src: big, name: "餐點大圖" }] : []}
        index={big ? 0 : null}
        onIndex={() => setBig("")}
      />
    </section>
  );
}

function useNutritionAgent(): { id: string; name: string } | null {
  const [agent, setAgent] = useState<{ id: string; name: string } | null>(null);
  useEffect(() => {
    let live = true;
    api.web
      .getRoster({})
      .then((r) => {
        const a = (r.agents ?? []).find((x) => x.id === "health" && x.enabled);
        if (live && a?.id) setAgent({ id: a.id, name: a.name || a.id });
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, []);
  return agent;
}

function DateNav({
  date,
  today,
  onChange,
}: {
  date: string;
  today: string;
  onChange: (d: string) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-2" data-testid="nutrition-datenav">
      <Button aria-label="前一天" onClick={() => onChange(shiftDate(date, -1))}>
        ‹ 前一天
      </Button>
      {/* 日期只寫一次（DEV-156 U-29）：日期選擇器本身就是那個日期，上面不再重複一行；底下只說「今天」或給「回到今天」。 */}
      <div className="flex flex-col items-center text-center">
        <input
          type="date"
          data-testid="nutrition-datepick"
          aria-label="選日期"
          value={date}
          max={today}
          onChange={(e) => {
            if (isDateStr(e.target.value)) onChange(e.target.value);
          }}
          className="min-h-11 rounded-lg border border-border bg-bg px-2 text-[15px] font-semibold"
        />
        {date === today ? (
          <span className="text-xs text-muted">今天</span>
        ) : (
          <button
            type="button"
            onClick={() => onChange(today)}
            className="min-h-6 text-xs font-medium text-accent underline"
          >
            回到今天
          </button>
        )}
      </div>
      {/* 今天以後還沒發生：跟日期選擇器的 max 一致，不讓人按到明天的空白頁（NUT-8）。 */}
      <Button
        aria-label="後一天"
        disabled={!canGoNext(date, today)}
        onClick={() => onChange(shiftDate(date, 1))}
      >
        後一天 ›
      </Button>
    </div>
  );
}

function timeOf(at: string): string {
  return at.length >= 16 ? at.slice(11, 16) : "";
}

function MealCard({
  meal,
  onOpenPhoto,
  onChanged,
}: {
  meal: Meal;
  onOpenPhoto: (src: string) => void;
  onChanged: () => void;
}) {
  const [mode, setMode] = useState<"" | "delete" | "fav">("");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(reasonOf(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <li
      data-testid="nutrition-meal"
      data-id={meal.id}
      className="flex flex-col gap-2 rounded-[18px] border border-border bg-surface p-4"
    >
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="font-semibold">
          {meal.meal} <span className="text-sm font-normal text-muted">{timeOf(meal.at)}</span>
        </h3>
        <span className="font-semibold">{fmt(meal.kcal)} 大卡</span>
      </div>
      <p className="text-sm text-muted">
        蛋白質 {fmt(meal.protein)} · 碳水 {fmt(meal.carb)} · 脂肪 {fmt(meal.fat)}（公克）
        {meal.conf ? ` · 把握${meal.conf}` : ""}
      </p>
      {meal.note ? <p className="text-sm">{meal.note}</p> : null}
      {meal.items.length > 0 ? (
        <details className="text-sm">
          <summary className="min-h-11 cursor-pointer py-2.5 font-medium">
            品項（{meal.items.length}）
          </summary>
          <ul className="flex flex-col gap-1 pb-1">
            {meal.items.map((it, i) => (
              <li key={i} className="flex justify-between gap-2">
                <span>
                  {it.name}
                  {it.qty ? <span className="text-muted">（{it.qty}）</span> : null}
                </span>
                <span className="text-muted">{fmt(it.kcal)} 大卡</span>
              </li>
            ))}
          </ul>
        </details>
      ) : null}
      {meal.photos.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {meal.photos.map((p) => (
            <button
              key={p}
              type="button"
              data-testid="nutrition-photo"
              aria-label="看大圖"
              onClick={() => onOpenPhoto(p)}
              className="size-20 overflow-hidden rounded-lg border border-border bg-surface-2"
            >
              <img src={p} alt="餐點照片" className="size-full object-cover" />
            </button>
          ))}
        </div>
      ) : null}

      {mode === "fav" ? (
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            const n = name.trim();
            if (!n) return;
            void run(async () => {
              await addFav(favFieldsFromMeal(meal, n));
              setMode("");
              setNote("已存成常吃");
              onChanged();
            });
          }}
        >
          <label className="flex min-w-0 flex-1 flex-col gap-1 text-sm">
            <span className="font-medium">常吃的名稱</span>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="min-h-11 rounded-lg border border-border bg-bg px-3 text-base"
              data-testid="nutrition-fav-name"
            />
          </label>
          <Button type="submit" variant="primary" disabled={busy || !name.trim()}>
            存
          </Button>
          <Button onClick={() => setMode("")}>取消</Button>
        </form>
      ) : mode === "delete" ? (
        <div className="flex flex-wrap items-center gap-2 text-sm" data-testid="nutrition-confirm">
          <span>確定刪除這餐？（會封存，檔案還在）</span>
          <Button
            variant="danger"
            disabled={busy}
            onClick={() =>
              void run(async () => {
                await archiveRecord(MEAL, meal.id);
                onChanged();
              })
            }
          >
            刪除
          </Button>
          <Button onClick={() => setMode("")}>取消</Button>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="ghost"
            onClick={() => {
              setName(meal.items[0]?.name || meal.meal);
              setNote("");
              setMode("fav");
            }}
          >
            存成常吃
          </Button>
          <Button variant="ghost" aria-label={`刪除 ${meal.id}`} onClick={() => setMode("delete")}>
            刪除
          </Button>
          {note ? <span className="text-sm text-muted">{note}</span> : null}
        </div>
      )}
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
    </li>
  );
}
