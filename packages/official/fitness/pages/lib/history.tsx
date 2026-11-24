// 「歷史」分頁：趨勢圖（重訓與有氧，時間軸）與訓練清單（新到舊，檢討與指數可展開）。
import { EmptyState, Segmented } from "@evergenie/ui";
import { useState } from "react";
import { CARDIO, cardioDetails, cardioSeries, isCardio, metricsFor, type Sport } from "./cardio";
import { TimeChart, WeekBars } from "./charts";
import { dateStr, dayLabel } from "./dates";
import {
  isBodyweightHistory,
  type StrengthMetric,
  sortWorkouts,
  strengthSeries,
  summaryOf,
  type Workout,
  weeklyTotals,
} from "./workout";

const card = "flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4";

export function History({ workouts }: { workouts: Workout[] }) {
  const sorted = sortWorkouts(workouts);
  const [day, setDay] = useState("");
  const shown = day ? sorted.filter((w) => w.date === day) : sorted;

  if (sorted.length === 0) {
    return <EmptyState title="還沒有訓練記錄" hint="到「記錄」記下第一組，或請教練補記" />;
  }
  return (
    <div className="flex flex-col gap-4" data-testid="fitness-history">
      <Trends workouts={sorted} />
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <label className="flex items-center gap-2">
          <span className="font-medium">選日期</span>
          <input
            type="date"
            data-testid="history-date"
            value={day}
            onChange={(e) => setDay(e.target.value)}
            className="min-h-11 rounded-lg border border-border bg-bg px-3 text-base"
          />
        </label>
        {day ? (
          <button
            type="button"
            onClick={() => setDay("")}
            className="min-h-11 px-2 font-medium text-accent underline"
          >
            看全部
          </button>
        ) : null}
      </div>
      {day && shown.length === 0 ? (
        <p data-testid="history-day-empty" className="text-sm text-muted">
          {day} 沒有訓練記錄。
        </p>
      ) : null}
      <ul className="flex flex-col gap-3" data-testid="workout-list">
        {shown.map((w) => (
          <li
            key={w.id}
            data-testid="workout-item"
            data-id={w.id}
            className="rounded-[18px] border border-border bg-surface p-4"
          >
            <details>
              <summary className="flex min-h-11 cursor-pointer flex-wrap items-baseline justify-between gap-2">
                <span className="font-semibold">
                  {dayLabel(w.date)} · {w.type}
                  {w.status === "進行中" ? "（進行中）" : ""}
                </span>
                <span className="text-sm text-muted">
                  {summaryOf(w)}
                  {w.feeling ? ` · ${w.feeling}` : ""}
                </span>
              </summary>
              <div className="flex flex-col gap-2 pt-2 text-sm">
                {w.sets.length > 0 ? (
                  <ol className="flex flex-col gap-1">
                    {w.sets.map((s, i) => (
                      <li key={`${s.equipment}-${i}`} className="flex justify-between gap-2">
                        <span>{s.equipment}</span>
                        <span className="text-muted">
                          {s.weight > 0 ? `${s.weight} kg × ${s.reps}` : `徒手 ${s.reps}`} ·{" "}
                          {s.feel}
                        </span>
                      </li>
                    ))}
                  </ol>
                ) : null}
                {isCardio(w.type) && cardioDetails(w).length > 0 ? (
                  <p data-testid="workout-cardio-details" className="text-muted">
                    {cardioDetails(w).join(" · ")}
                  </p>
                ) : null}
                {w.note ? <p>備註：{w.note}</p> : null}
                {w.review ? (
                  <div>
                    <p className="whitespace-pre-line" data-testid="workout-review-text">
                      {w.review}
                    </p>
                    <p className="text-xs text-muted">檢討：{w.review_by || "規則"}</p>
                  </div>
                ) : (
                  <p className="text-muted">還沒有檢討。</p>
                )}
              </div>
            </details>
          </li>
        ))}
      </ul>
    </div>
  );
}

const selectCls = "min-h-11 rounded-lg border border-border bg-bg px-3 text-base";
const kg1 = (v: number) => String(Math.round(v * 10) / 10);

/** 趨勢：重訓（每週總量，或某器材每次的最大重量／估算 1RM／總量）與有氧（每週分鐘，或某項目的距離、配速、心率…）都畫在時間軸上。 */
function Trends({ workouts }: { workouts: Workout[] }) {
  const today = dateStr(new Date());
  const names = [
    ...new Set(
      workouts.filter((w) => w.type === "重訓").flatMap((w) => w.sets.map((s) => s.equipment)),
    ),
  ];
  const sports = CARDIO.filter((s) => workouts.some((w) => w.type === s));
  const kinds = [
    ...(names.length > 0 ? [{ value: "重訓", label: "重訓" }] : []),
    ...(sports.length > 0 ? [{ value: "有氧", label: "有氧" }] : []),
  ];
  const [kind, setKind] = useState("");
  const k = kinds.some((x) => x.value === kind) ? kind : (kinds[0]?.value ?? "");
  const [eq, setEq] = useState("");
  const chosen = names.includes(eq) ? eq : "";
  const [sm, setSm] = useState<StrengthMetric>("max");
  const [sport, setSport] = useState("");
  const sp = (sports as readonly string[]).includes(sport) ? (sport as Sport) : null;
  const [mk, setMk] = useState("");
  if (kinds.length === 0) return null;

  const bw = chosen ? isBodyweightHistory(workouts, chosen) : false;
  const sOptions = bw
    ? [
        { value: "max", label: "最多次數" },
        { value: "volume", label: "總次數" },
      ]
    : [
        { value: "max", label: "最大重量" },
        { value: "e1rm", label: "估算 1RM" },
        { value: "volume", label: "總量" },
      ];
  const sMetric: StrengthMetric = bw && sm === "e1rm" ? "max" : sm;
  const sLabel = sOptions.find((o) => o.value === sMetric)?.label ?? "";
  const sFmt = (v: number) => (bw ? `${kg1(v)} 下` : `${kg1(v)} kg`);

  const metrics = sp ? metricsFor(sp) : [];
  const metric = metrics.find((m) => m.key === mk) ?? metrics[0];

  return (
    <div className={card} data-testid="fitness-trends">
      <h2 className="font-semibold">趨勢</h2>
      {kinds.length > 1 ? (
        <Segmented ariaLabel="趨勢種類" options={kinds} value={k} onChange={setKind} />
      ) : null}
      {k === "重訓" ? (
        <>
          <label className="flex flex-col gap-1 text-sm">
            <span className="font-medium">看哪個</span>
            <select
              data-testid="history-equipment"
              value={chosen}
              onChange={(e) => setEq(e.target.value)}
              className={selectCls}
            >
              <option value="">全部器材：每週總量</option>
              {names.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </label>
          {chosen ? (
            <>
              <Segmented
                ariaLabel="重訓指標"
                options={sOptions}
                value={sMetric}
                onChange={(v) => setSm(v as StrengthMetric)}
              />
              <TimeChart
                key={`${chosen}-${sMetric}`}
                testId="max-chart"
                label={`${chosen} ${sLabel}`}
                points={strengthSeries(workouts, chosen, sMetric)}
                fmt={sFmt}
              />
              {sMetric === "e1rm" ? (
                <p className="text-xs text-muted">
                  估算 1RM＝用每組的重量和次數推「最多能舉一下的重量」（Epley
                  公式），只是參考，不用真的去試。
                </p>
              ) : null}
            </>
          ) : (
            <WeekBars
              testId="week-volume"
              label="重訓總量（重量×次數）"
              bars={weeklyTotals(workouts, today, "重訓")}
              fmt={(v) => `${Math.round(v)} kg`}
            />
          )}
        </>
      ) : (
        <>
          <label className="flex flex-col gap-1 text-sm">
            <span className="font-medium">看哪個</span>
            <select
              data-testid="history-sport"
              value={sp ?? ""}
              onChange={(e) => setSport(e.target.value)}
              className={selectCls}
            >
              <option value="">全部有氧：每週分鐘</option>
              {sports.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
          {sp && metric ? (
            <>
              <label className="flex flex-col gap-1 text-sm">
                <span className="font-medium">指標</span>
                <select
                  data-testid="history-metric"
                  value={metric.key}
                  onChange={(e) => setMk(e.target.value)}
                  className={selectCls}
                >
                  {metrics.map((m) => (
                    <option key={m.key} value={m.key}>
                      {m.label}
                    </option>
                  ))}
                </select>
              </label>
              <TimeChart
                key={`${sp}-${metric.key}`}
                testId="cardio-chart"
                label={`${sp} ${metric.label}`}
                points={cardioSeries(workouts, sp, metric)}
                fmt={metric.fmt}
                lower={metric.lower}
              />
            </>
          ) : (
            <WeekBars
              testId="week-cardio"
              label="有氧時間"
              bars={weeklyTotals(workouts, today, "有氧")}
              fmt={(v) => `${Math.round(v)} 分`}
            />
          )}
        </>
      )}
    </div>
  );
}
