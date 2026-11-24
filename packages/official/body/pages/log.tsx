import { Button, EmptyState, LoadFailed, Spinner } from "@evergenie/ui";
import { useState } from "react";
import {
  type BodyEntry,
  type BodyKey,
  checkMetric,
  describeBody,
  EXTRA_KEYS,
  type MainSummary,
  METRICS,
  summarize,
  type TrendPoint,
  trendLine,
} from "./lib/body";
import { dateStr, dayLabel, isDateStr } from "./lib/dates";
import { archiveBody, loadBody, reasonOf, saveBody, useLoad } from "./lib/store";

const inputCls = "min-h-11 w-full rounded-lg border border-border bg-bg px-3 text-base";
const card = "flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4";
const KEYS: BodyKey[] = ["weight", "fat", ...EXTRA_KEYS];

const blank = (): Record<BodyKey, string> => ({
  weight: "",
  fat: "",
  bmi: "",
  bmr: "",
  visceral: "",
  score: "",
});

// 身體：記體重與體脂（一天一筆）、近 7 天平均、趨勢圖、清單（REQ-129）。
// 任何秘書聽到體重都可以用 `evergenie data` 寫進同一份（body.entry）。
export default function Body() {
  const body = useLoad(loadBody);
  const today = dateStr(new Date());
  const items = body.data ?? [];
  const sum = summarize(items, today);

  return (
    <section data-testid="body-page" className="flex flex-col gap-4">
      <Form items={items} today={today} onSaved={body.reload} />
      {body.error ? (
        <LoadFailed
          what="身體數據"
          onRetry={body.reload}
          variant={body.data === undefined ? "block" : "inline"}
          className={body.data === undefined ? "py-8" : undefined}
        />
      ) : null}
      {body.data === undefined && !body.error ? (
        <div className="flex justify-center py-8">
          <Spinner label="載入中" />
        </div>
      ) : body.data ? (
        <>
          <div data-testid="body-averages" className={card}>
            <h2 className="font-semibold">近 7 天平均</h2>
            <Average label="體重" unit="kg" s={sum.weight} testId="body-avg-weight" />
            <Average label="體脂" unit="%" s={sum.fat} testId="body-avg-fat" />
            <p className="text-xs text-muted">
              跟前 7 天的平均比；只算有記錄的日子。單日會因為水分上下跳，看平均比較準。
            </p>
          </div>
          <Charts items={items} />
          <List items={items} onChanged={body.reload} />
        </>
      ) : null}
    </section>
  );
}

function Average({
  label,
  unit,
  s,
  testId,
}: {
  label: string;
  unit: string;
  s: MainSummary;
  testId: string;
}) {
  if (s.avg7 === null && s.latest === null) return null;
  return (
    <p data-testid={testId}>
      {label}{" "}
      {s.avg7 !== null ? (
        <>
          <span className="font-semibold">{s.avg7}</span> {unit}
          {s.delta !== null ? (
            <span className="text-muted">
              （比前 7 天{s.delta > 0 ? "多" : s.delta < 0 ? "少" : "沒變"}
              {s.delta !== 0 ? ` ${Math.abs(s.delta)}` : ""}）
            </span>
          ) : (
            <span className="text-muted">（前 7 天沒有記錄）</span>
          )}
        </>
      ) : (
        <span className="text-muted">
          最近 7 天沒有記錄，最新 {s.latest} {unit}
        </span>
      )}
    </p>
  );
}

function Form({
  items,
  today,
  onSaved,
}: {
  items: BodyEntry[];
  today: string;
  onSaved: () => void;
}) {
  const [date, setDate] = useState(today);
  const [vals, setVals] = useState(blank);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [msg, setMsg] = useState("");
  const exists = items.some((x) => x.date === date);

  const submit = async () => {
    setError("");
    setMsg("");
    if (!isDateStr(date)) return setError("日期要填完整");
    if (date > today) return setError("日期不能在未來");
    const fields: Record<string, number | string> = {};
    for (const k of KEYS) {
      const c = checkMetric(k, vals[k]);
      if (c.error) return setError(c.error);
      if (c.value !== null) fields[k] = c.value;
    }
    if (fields.weight === undefined) return setError("體重要填");
    if (note.trim()) fields.note = note.trim().slice(0, 80);
    setBusy(true);
    try {
      await saveBody(date, fields, exists);
      setMsg(exists ? `已更新 ${date}` : `已記 ${date}`);
      setVals(blank());
      setNote("");
      onSaved();
    } catch (e) {
      setError(reasonOf(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form
      data-testid="body-form"
      className={card}
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <h2 className="font-semibold">記體重</h2>
      <label className="flex flex-col gap-1 text-sm">
        <span className="font-medium">日期</span>
        <input
          type="date"
          value={date}
          max={today}
          onChange={(e) => setDate(e.target.value)}
          className={inputCls}
        />
      </label>
      <div className="grid grid-cols-2 gap-3">
        {(["weight", "fat"] as const).map((k) => (
          <label key={k} className="flex flex-col gap-1 text-sm">
            <span className="font-medium">
              {METRICS[k].label}（{METRICS[k].unit}）{k === "fat" ? "（可空）" : ""}
            </span>
            <input
              inputMode="decimal"
              value={vals[k]}
              onChange={(e) => setVals({ ...vals, [k]: e.target.value })}
              className={inputCls}
            />
          </label>
        ))}
      </div>
      <details>
        <summary className="min-h-11 cursor-pointer py-2 text-sm font-medium">
          更多數據（體脂秤給的，可空）
        </summary>
        <div className="grid grid-cols-2 gap-3 pt-1">
          {EXTRA_KEYS.map((k) => (
            <label key={k} className="flex flex-col gap-1 text-sm">
              <span className="font-medium">
                {METRICS[k].label}
                {METRICS[k].unit ? `（${METRICS[k].unit}）` : ""}
              </span>
              <input
                inputMode="decimal"
                value={vals[k]}
                onChange={(e) => setVals({ ...vals, [k]: e.target.value })}
                className={inputCls}
              />
            </label>
          ))}
        </div>
      </details>
      <label className="flex flex-col gap-1 text-sm">
        <span className="font-medium">備註（可空）</span>
        <input
          value={note}
          onChange={(e) => setNote(e.target.value)}
          maxLength={80}
          className={inputCls}
        />
      </label>
      {exists ? (
        <p className="text-xs text-muted">{date} 已經有記錄，填的欄位會覆蓋原本的數字。</p>
      ) : null}
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" variant="primary" disabled={busy}>
          {exists ? "更新這天" : "記下來"}
        </Button>
        {msg ? (
          <span role="status" className="text-sm text-muted">
            {msg}
          </span>
        ) : null}
      </div>
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
    </form>
  );
}

function dayNo(date: string): number {
  const [y, m, d] = date.split("-").map(Number) as [number, number, number];
  return Date.UTC(y, m - 1, d) / 86_400_000;
}

function Charts({ items }: { items: BodyEntry[] }) {
  const w = trendLine(items, "weight");
  const f = trendLine(items, "fat");
  if (w.length === 0 && f.length === 0) return null;
  return (
    <div data-testid="body-charts" className={card}>
      <h2 className="font-semibold">趨勢</h2>
      {w.length > 0 ? <Chart label="體重（kg）" pts={w} testId="body-chart-weight" /> : null}
      {f.length > 0 ? <Chart label="體脂（%）" pts={f} testId="body-chart-fat" /> : null}
      <p className="text-xs text-muted">點是當天，線是 7 天平均；沒有記錄的日子不畫。</p>
    </div>
  );
}

function Chart({ label, pts, testId }: { label: string; pts: TrendPoint[]; testId: string }) {
  const W = 300;
  const H = 130;
  const pad = 16;
  const vals = pts.flatMap((p) => [p.v, p.avg]);
  const lo = Math.min(...vals);
  const hi = Math.max(...vals);
  const span = hi - lo || 1;
  const d0 = dayNo(pts[0]?.date ?? "1970-01-01");
  const d1 = dayNo(pts[pts.length - 1]?.date ?? "1970-01-01");
  const x = (date: string) =>
    d1 === d0 ? W / 2 : pad + ((dayNo(date) - d0) / (d1 - d0)) * (W - 2 * pad);
  const y = (v: number) => H - pad - ((v - lo) / span) * (H - 2 * pad);
  const avgLine = pts.map((p) => `${x(p.date)},${y(p.avg)}`).join(" ");
  return (
    <div data-testid={testId} data-points={pts.length} className="flex flex-col gap-1">
      <span className="text-sm font-medium">{label}</span>
      <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label={`${label}趨勢`} className="h-36 w-full">
        {pts.length > 1 ? (
          <polyline points={avgLine} fill="none" stroke="var(--color-accent)" strokeWidth="2" />
        ) : null}
        {pts.map((p) => (
          <circle
            key={p.date}
            data-testid="body-point"
            data-date={p.date}
            cx={x(p.date)}
            cy={y(p.v)}
            r="3.5"
            fill="var(--color-muted)"
          />
        ))}
      </svg>
      <p className="text-xs text-muted">
        最低 {lo} · 最高 {hi}
      </p>
    </div>
  );
}

function List({ items, onChanged }: { items: BodyEntry[]; onChanged: () => void }) {
  const [confirm, setConfirm] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const [day, setDay] = useState("");
  if (items.length === 0) {
    return <EmptyState title="還沒有身體數據" hint="在上面記下今天的體重，或請秘書幫你記" />;
  }
  const shown = day ? items.filter((x) => x.date === day) : items;
  return (
    <div className="flex flex-col gap-2">
      <h2 className="font-semibold">記錄</h2>
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <label className="flex items-center gap-2">
          <span className="font-medium">選日期</span>
          <input
            type="date"
            data-testid="body-datepick"
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
        <p data-testid="body-day-empty" className="text-sm text-muted">
          {day} 沒有記錄。
        </p>
      ) : null}
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
      <ul data-testid="body-list" className="flex flex-col gap-2">
        {shown.map((x) => (
          <li
            key={x.date}
            data-testid="body-entry"
            data-date={x.date}
            className="flex flex-col gap-2 rounded-[18px] border border-border bg-surface p-4"
          >
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <span className="font-medium">{dayLabel(x.date)}</span>
              <span className="text-sm">{describeBody(x)}</span>
            </div>
            {x.note ? <p className="text-sm text-muted">{x.note}</p> : null}
            {confirm === x.date ? (
              <div data-testid="body-confirm" className="flex flex-wrap items-center gap-2 text-sm">
                <span>確定刪除這天？（會封存，檔案還在）</span>
                <Button
                  variant="danger"
                  disabled={busy}
                  onClick={async () => {
                    setBusy(true);
                    setError("");
                    try {
                      await archiveBody(x.date);
                      setConfirm("");
                      onChanged();
                    } catch (e) {
                      setError(reasonOf(e));
                    } finally {
                      setBusy(false);
                    }
                  }}
                >
                  刪除
                </Button>
                <Button onClick={() => setConfirm("")}>取消</Button>
              </div>
            ) : (
              <Button
                variant="ghost"
                className="self-start"
                aria-label={`刪除 ${x.date}`}
                onClick={() => setConfirm(x.date)}
              >
                刪除
              </Button>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
