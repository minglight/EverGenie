// 趨勢圖：時間軸折線（點的橫向位置照真正的日期）與每週長條。只有一條資料，所以不放圖例，標題說明是什麼。
// 顏色只用 token（var(--accent) 等，亮暗自動換）；點一下（或滑過）某個點，下面那行顯示那次的日期與數值，預設是最近一次。
import { useState } from "react";
import { dayLabel } from "./dates";

export interface ChartPoint {
  id: string;
  date: string;
  value: number;
}

const W = 320;
const H = 150;
const L = 44; // 左邊放 y 軸數字
const R = 10;
const T = 12;
const B = 24; // 下面放日期

function dayNum(date: string): number {
  const [y, m, d] = date.split("-").map(Number) as [number, number, number];
  return Math.round(new Date(y, m - 1, d).getTime() / 86_400_000);
}

const md = (date: string) => {
  const [, m, d] = date.split("-").map(Number);
  return `${m}/${d}`;
};

/** 時間軸折線。lower＝越低越好（配速、SWOLF）：y 軸反過來，往上＝變好。 */
export function TimeChart({
  points,
  label,
  fmt,
  lower = false,
  testId,
}: {
  points: readonly ChartPoint[];
  label: string;
  fmt: (v: number) => string;
  lower?: boolean;
  testId: string;
}) {
  const [sel, setSel] = useState<number | null>(null);
  if (points.length === 0) {
    return (
      <p data-testid={`${testId}-empty`} className="text-sm text-muted">
        還沒有這項的數字，記了之後這裡會畫出來。
      </p>
    );
  }
  const idx = sel !== null && sel < points.length ? sel : points.length - 1;
  const cur = points[idx] ?? points[points.length - 1];
  // 同一天有好幾次：往右錯開一點（一天內平均排開），點才不會疊在一起。
  const days = points.map((p, i) => {
    const same = points.filter((q) => q.date === p.date);
    const k = points.slice(0, i).filter((q) => q.date === p.date).length;
    return dayNum(p.date) + (same.length > 1 ? (k / same.length) * 0.8 : 0);
  });
  const d0 = Math.min(...days);
  const d1 = Math.max(...days);
  const vals = points.map((p) => p.value);
  let lo = Math.min(...vals);
  let hi = Math.max(...vals);
  if (lo === hi) {
    lo -= Math.abs(lo) * 0.1 || 1;
    hi += Math.abs(hi) * 0.1 || 1;
  }
  // 照日期排在時間軸上；全部同一天時改成平均排開（不然點會疊在一起點不到）。
  const x = (i: number) => {
    if (points.length === 1) return (L + W - R) / 2;
    if (d1 === d0) return L + (i * (W - L - R)) / (points.length - 1);
    return L + (((days[i] ?? d0) - d0) * (W - L - R)) / (d1 - d0);
  };
  const y = (v: number) => {
    const t = (v - lo) / (hi - lo);
    return T + (lower ? t : 1 - t) * (H - T - B);
  };
  const ticks = [hi, (hi + lo) / 2, lo];
  const nearest = (e: { clientX: number; currentTarget: SVGRectElement }) => {
    const box = e.currentTarget.ownerSVGElement?.getBoundingClientRect();
    if (!box || box.width === 0) return idx;
    const px = ((e.clientX - box.left) / box.width) * W;
    let best = 0;
    for (let i = 1; i < points.length; i++) {
      if (Math.abs(x(i) - px) < Math.abs(x(best) - px)) best = i;
    }
    return best;
  };
  const line = points.map((p, i) => `${x(i)},${y(p.value)}`).join(" ");
  const first = points[0];
  const last = points[points.length - 1];
  return (
    <div data-testid={testId} className="flex flex-col gap-1">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={`${label}：${points.length} 次，最近 ${fmt(last?.value ?? 0)}`}
        className="h-40 w-full"
      >
        {ticks.map((v) => (
          <g key={v}>
            <line x1={L} x2={W - R} y1={y(v)} y2={y(v)} stroke="var(--border)" strokeWidth="1" />
            <text x={L - 6} y={y(v) + 4} textAnchor="end" fontSize="10" fill="var(--muted)">
              {fmt(v).replace(/[\s/].*$/, "")}
            </text>
          </g>
        ))}
        {first ? (
          <text x={L} y={H - 6} fontSize="10" fill="var(--muted)">
            {md(first.date)}
          </text>
        ) : null}
        {last && points.length > 1 ? (
          <text x={W - R} y={H - 6} textAnchor="end" fontSize="10" fill="var(--muted)">
            {md(last.date)}
          </text>
        ) : null}
        <line
          x1={x(idx)}
          x2={x(idx)}
          y1={T}
          y2={H - B}
          stroke="var(--border-strong)"
          strokeWidth="1"
          strokeDasharray="3 3"
        />
        {points.length > 1 ? (
          <polyline
            points={line}
            fill="none"
            stroke="var(--accent)"
            strokeWidth="2"
            strokeLinejoin="round"
            strokeLinecap="round"
          />
        ) : null}
        {points.map((p, i) => (
          <circle
            key={p.id}
            data-testid={`${testId}-point`}
            cx={x(i)}
            cy={y(p.value)}
            r={i === idx ? 5.5 : 4}
            fill="var(--accent)"
            stroke="var(--surface)"
            strokeWidth="2"
          />
        ))}
        {/* 整個圖都是點擊範圍：點哪裡就挑橫向最近的那個點（手機手指也點得到，不用剛好點中小圓點）。 */}
        {/* biome-ignore lint/a11y/noStaticElementInteractions: 數值另外在下面那行與「看數字」表格提供，這裡只是加速挑點 */}
        <rect
          data-testid={`${testId}-hit`}
          x={0}
          y={0}
          width={W}
          height={H}
          fill="transparent"
          onClick={(e) => setSel(nearest(e))}
          onMouseMove={(e) => setSel(nearest(e))}
        />
      </svg>
      <p data-testid={`${testId}-readout`} className="text-sm">
        <span className="font-medium">{cur ? dayLabel(cur.date) : ""}</span>
        <span className="text-muted"> · {label} </span>
        <span className="font-semibold">{cur ? fmt(cur.value) : ""}</span>
      </p>
      <p className="text-xs text-muted">
        {points.length} 次
        {points.length > 1 && first && last
          ? ` · 從 ${fmt(first.value)} 到 ${fmt(last.value)}${lower ? "（越低越好）" : ""}`
          : ""}
      </p>
      <DataTable
        rows={points.map((p) => ({ k: p.id, a: dayLabel(p.date), b: fmt(p.value) }))}
        head={["日期", label]}
      />
    </div>
  );
}

export interface Bar {
  week: string;
  value: number;
}

/** 每週長條（週一的日期當標籤）；點一下看那週的數字，預設本週。 */
export function WeekBars({
  bars,
  label,
  fmt,
  testId,
}: {
  bars: readonly Bar[];
  label: string;
  fmt: (v: number) => string;
  testId: string;
}) {
  const [sel, setSel] = useState<number | null>(null);
  const idx = sel !== null && sel < bars.length ? sel : bars.length - 1;
  const cur = bars[idx];
  const hi = Math.max(...bars.map((b) => b.value), 0) || 1;
  const n = bars.length || 1;
  const slot = (W - L - R) / n;
  const bw = Math.max(slot - 2, 2); // 長條之間留 2px
  const base = H - B;
  const yv = (v: number) => base - (v / hi) * (H - T - B);
  return (
    <div data-testid={testId} className="flex flex-col gap-1">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={`${label}：最近 ${n} 週`}
        className="h-40 w-full"
      >
        {[hi, hi / 2].map((v) => (
          <g key={v}>
            <line x1={L} x2={W - R} y1={yv(v)} y2={yv(v)} stroke="var(--border)" strokeWidth="1" />
            <text x={L - 6} y={yv(v) + 4} textAnchor="end" fontSize="10" fill="var(--muted)">
              {fmt(v).replace(/[\s/].*$/, "")}
            </text>
          </g>
        ))}
        <line x1={L} x2={W - R} y1={base} y2={base} stroke="var(--border-strong)" strokeWidth="1" />
        {bars.map((b, i) => {
          const h = base - yv(b.value);
          const x0 = L + i * slot + 1;
          const r = Math.min(4, bw / 2, h);
          // 上面兩角圓 4px、貼著底線
          const d =
            h <= 0
              ? ""
              : `M${x0},${base} V${base - h + r} Q${x0},${base - h} ${x0 + r},${base - h} H${x0 + bw - r} Q${x0 + bw},${base - h} ${x0 + bw},${base - h + r} V${base} Z`;
          return (
            <g key={b.week}>
              {d ? <path d={d} fill="var(--accent)" opacity={i === idx ? 1 : 0.55} /> : null}
              {/* 一週一格，手機上一格約 20px（比 44px 小）：下面那行與「看數字」表格是正式的讀法，點長條只是捷徑（FIT-11）。 */}
              {/* biome-ignore lint/a11y/noStaticElementInteractions: 數值另外在下面那行與「看數字」表格提供 */}
              <rect
                data-testid={`${testId}-bar`}
                x={L + i * slot}
                y={T}
                width={slot}
                height={H - T - B}
                fill="transparent"
                onClick={() => setSel(i)}
                onMouseEnter={() => setSel(i)}
              />
            </g>
          );
        })}
        {bars[0] ? (
          <text x={L} y={H - 6} fontSize="10" fill="var(--muted)">
            {md(bars[0].week)}
          </text>
        ) : null}
        <text x={W - R} y={H - 6} textAnchor="end" fontSize="10" fill="var(--muted)">
          本週
        </text>
      </svg>
      <p data-testid={`${testId}-readout`} className="text-sm">
        <span className="font-medium">
          {cur ? (idx === bars.length - 1 ? "本週" : `${md(cur.week)} 那週`) : ""}
        </span>
        <span className="text-muted"> · {label} </span>
        <span className="font-semibold">{cur ? fmt(cur.value) : ""}</span>
      </p>
      <DataTable
        rows={bars.map((b) => ({ k: b.week, a: `${md(b.week)} 那週`, b: fmt(b.value) }))}
        head={["週", label]}
      />
    </div>
  );
}

function DataTable({
  head,
  rows,
}: {
  head: [string, string];
  rows: { k: string; a: string; b: string }[];
}) {
  return (
    <details className="text-sm">
      <summary className="min-h-11 cursor-pointer py-2 font-medium text-accent">看數字</summary>
      <table className="w-full text-left">
        <thead>
          <tr className="text-muted">
            <th className="py-1 font-medium">{head[0]}</th>
            <th className="py-1 text-right font-medium">{head[1]}</th>
          </tr>
        </thead>
        <tbody>
          {[...rows].reverse().map((r) => (
            <tr key={r.k} className="border-t border-border">
              <td className="py-1">{r.a}</td>
              <td className="py-1 text-right">{r.b}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </details>
  );
}
