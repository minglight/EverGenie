// 有氧（跑步、划船、游泳、騎車）：各項目的欄位定義、輸入檢查、配速、規則檢討、趨勢資料。
// 四種共同的是心率（平均、最大）與熱量；其他欄位各自不同（見 SPORTS 與 FIELDS）。
// 沒有 I/O，方便單元測試；不 import workout.ts（workout.ts 會 import 這支）。
import { kg, paceSec, type WorkoutLike } from "./coach";

// 同一份清單另外寫在 schemas/workout.toml 的 type 列舉與 habits 的 engine.go（cardioTypes）；改這裡三處一起改。
export const CARDIO = ["跑步", "划船", "游泳", "騎車"] as const;
export type Sport = (typeof CARDIO)[number];
export const isCardio = (t: string): t is Sport => (CARDIO as readonly string[]).includes(t);
export const STROKES = ["自由式", "蛙式", "仰式", "蝶式", "混合"] as const;

/** 有氧的數字指數；0＝沒填。 */
export const NUM_KEYS = [
  "avg_hr",
  "max_hr",
  "calories",
  "cadence_spm",
  "stride_m",
  "gct_ms",
  "vertical_osc_cm",
  "training_effect",
  "elevation_m",
  "avg_power_w",
  "stroke_rate_spm",
  "pool_m",
  "laps",
  "swolf",
  "avg_speed_kmh",
  "cadence_rpm",
] as const;
export type NumKey = (typeof NUM_KEYS)[number];

export type CardioMetrics = Record<NumKey, number> & { stroke_style: string; indoor: boolean };

function num(v: unknown): number {
  if (typeof v === "number" && Number.isFinite(v)) return v;
  if (typeof v === "string" && v.trim() !== "") {
    const n = Number(v);
    if (Number.isFinite(n)) return n;
  }
  return 0;
}

export function emptyMetrics(): CardioMetrics {
  const m = { stroke_style: "", indoor: false } as CardioMetrics;
  for (const k of NUM_KEYS) m[k] = 0;
  return m;
}

/** vault 記錄的欄位換成有氧指數（沒填的是 0）。 */
export function toMetrics(f: Record<string, unknown>): CardioMetrics {
  const m = emptyMetrics();
  for (const k of NUM_KEYS) m[k] = num(f[k]);
  m.stroke_style = typeof f.stroke_style === "string" ? f.stroke_style : "";
  m.indoor = f.indoor === true;
  return m;
}

export interface SportSpec {
  /** 距離在畫面上用公里還是公尺（存的一律是 distance_km）。 */
  unit: "km" | "m";
  min: number;
  max: number;
  /** 配速以多少公尺為單位；null＝騎車，看均速。 */
  paceM: 1000 | 500 | 100 | null;
  /** 動詞：跑、划、游、騎（檢討用）。 */
  verb: string;
  /** 「輕鬆」時建議多加的量。 */
  step: string;
  /** 跟上次比，距離差多少才算有差（公里）。 */
  distEps: number;
}

export const SPORTS: Record<Sport, SportSpec> = {
  跑步: { unit: "km", min: 0.1, max: 100, paceM: 1000, verb: "跑", step: "0.5 km", distEps: 0.1 },
  划船: { unit: "m", min: 100, max: 50000, paceM: 500, verb: "划", step: "500 m", distEps: 0.1 },
  游泳: { unit: "m", min: 25, max: 20000, paceM: 100, verb: "游", step: "100 m", distEps: 0.025 },
  騎車: { unit: "km", min: 0.5, max: 400, paceM: null, verb: "騎", step: "5 km", distEps: 0.5 },
};

export interface FieldDef {
  key: NumKey;
  label: string;
  unit: string;
  min: number;
  max: number;
  int?: boolean;
  /** 收在「更多指數」裡（手錶才有的）。 */
  more?: boolean;
}

/** 四種共同的欄位。 */
export const COMMON_FIELDS: readonly FieldDef[] = [
  { key: "avg_hr", label: "平均心率", unit: "bpm", min: 30, max: 230, int: true },
  { key: "max_hr", label: "最大心率", unit: "bpm", min: 30, max: 230, int: true },
  { key: "calories", label: "熱量", unit: "大卡", min: 1, max: 5000, int: true, more: true },
];

/** 各項目自己的欄位（游泳的池長、趟數、泳姿與騎車的室內／戶外在表單另外處理）。 */
export const FIELDS: Record<Sport, readonly FieldDef[]> = {
  跑步: [
    { key: "cadence_spm", label: "步頻", unit: "步/分", min: 100, max: 250, int: true },
    { key: "stride_m", label: "步幅", unit: "m", min: 0.3, max: 3, more: true },
    { key: "elevation_m", label: "爬升", unit: "m", min: 0, max: 10000, more: true },
    { key: "gct_ms", label: "觸地時間", unit: "ms", min: 100, max: 400, int: true, more: true },
    { key: "vertical_osc_cm", label: "垂直振幅", unit: "cm", min: 2, max: 20, more: true },
    { key: "training_effect", label: "有氧訓練效果", unit: "", min: 0, max: 5, more: true },
  ],
  划船: [
    { key: "stroke_rate_spm", label: "槳頻", unit: "下/分", min: 10, max: 60, int: true },
    { key: "avg_power_w", label: "平均功率", unit: "W", min: 10, max: 1500, int: true },
  ],
  游泳: [{ key: "swolf", label: "SWOLF", unit: "", min: 10, max: 200, int: true }],
  騎車: [
    { key: "cadence_rpm", label: "踏頻", unit: "轉/分", min: 20, max: 200, int: true },
    { key: "avg_power_w", label: "平均功率", unit: "W", min: 10, max: 2000, int: true },
    { key: "elevation_m", label: "爬升", unit: "m", min: 0, max: 10000, more: true },
  ],
};

export const MAX_MIN = 720;

/** 有氧檢討的門檻（跟 coach.ts 的 RULES 一樣集中在這裡調）。 */
export const CARDIO_RULES = {
  /** 配速差幾秒才算有差：跑步（每公里）、划船與游泳。 */
  pace_eps_run: 3,
  pace_eps_other: 2,
  /** 騎車均速差多少 km/h 才算有差。 */
  speed_eps: 0.5,
  /** 平均心率差多少才提。 */
  hr_eps: 3,
  /** 跑步步頻低於這個數提醒。 */
  low_cadence: 160,
} as const;

function mss(sec: number): string {
  const s = Math.round(sec);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/** 這個項目的配速（m:ss／單位）；騎車或數字不合理回 ""。 */
export function paceFor(sport: Sport, km: number, minutes: number): string {
  const per = SPORTS[sport].paceM;
  if (per === null || !(km > 0) || !(minutes > 0)) return "";
  return mss((minutes * 60) / ((km * 1000) / per));
}

/** 均速 km/h（一位小數）。 */
export function speedOf(km: number, minutes: number): number {
  if (!(km > 0) || !(minutes > 0)) return 0;
  return Math.round((km / (minutes / 60)) * 10) / 10;
}

export function paceUnit(sport: Sport): string {
  const per = SPORTS[sport].paceM;
  return per === 1000 ? "/km" : per === null ? "" : `/${per}m`;
}

/** 距離的寫法：跑步、騎車用公里，划船、游泳用公尺。 */
export function distText(sport: Sport, km: number): string {
  return SPORTS[sport].unit === "m" ? `${Math.round(km * 1000)} m` : `${kg(km)} km`;
}

/** 表單輸入（都是字串，空字串＝沒填）。 */
export interface CardioInput {
  /** 距離，單位照 SPORTS[sport].unit；游泳用池長×趟數時可空。 */
  dist: string;
  minutes: string;
  /** 數字指數（key 照 NumKey）。 */
  values: Partial<Record<NumKey, string>>;
  stroke_style?: string;
  indoor?: boolean;
}

export interface CardioCheck {
  ok: boolean;
  error: string;
  /** 要寫進 vault 的欄位（距離、時間、配速／均速、有填的指數）。 */
  fields: Record<string, unknown>;
}

const bad = (error: string): CardioCheck => ({ ok: false, error, fields: {} });

function rangeText(f: { label: string; unit: string; min: number; max: number }): string {
  return `${f.label} ${f.min}–${f.max}${f.unit ? ` ${f.unit}` : ""}`;
}

/** 檢查一次有氧的輸入並算出要存的欄位。 */
export function checkCardio(sport: Sport, input: CardioInput): CardioCheck {
  const spec = SPORTS[sport];
  const minutes = Number(input.minutes);
  const vals: Partial<Record<NumKey, number>> = {};
  const defs = [...COMMON_FIELDS, ...FIELDS[sport]];
  if (sport === "游泳") {
    defs.push(
      { key: "pool_m", label: "池長", unit: "m", min: 0, max: 100, int: true },
      { key: "laps", label: "趟數", unit: "", min: 1, max: 800, int: true },
    );
  }
  for (const f of defs) {
    const raw = (input.values[f.key] ?? "").trim();
    if (raw === "") continue;
    const v = Number(raw);
    if (!Number.isFinite(v) || v < f.min || v > f.max || (f.int && !Number.isInteger(v))) {
      return bad(`${rangeText(f)}${f.int ? "（整數）" : ""}`);
    }
    vals[f.key] = v;
  }
  let dist = input.dist.trim() === "" ? Number.NaN : Number(input.dist);
  if (sport === "游泳" && Number.isNaN(dist) && (vals.pool_m ?? 0) > 0 && vals.laps) {
    dist = (vals.pool_m ?? 0) * vals.laps;
  }
  const distLabel = `距離 ${spec.min}–${spec.max} ${spec.unit}`;
  if (!Number.isFinite(dist) || !Number.isFinite(minutes)) {
    return bad(sport === "游泳" ? "距離（或池長＋趟數）、時間要是數字" : "距離、時間要是數字");
  }
  if (dist < spec.min || dist > spec.max || minutes < 1 || minutes > MAX_MIN) {
    return bad(`${distLabel}、時間 1–${MAX_MIN} 分`);
  }
  if (vals.avg_hr && vals.max_hr && vals.max_hr < vals.avg_hr) {
    return bad("最大心率不能比平均心率低");
  }
  const km = Math.round((spec.unit === "m" ? dist / 1000 : dist) * 1000) / 1000;
  const mins = Math.round(minutes * 10) / 10;
  const fields: Record<string, unknown> = {
    distance_km: spec.unit === "m" ? km : Math.round(km * 100) / 100,
    duration_min: mins,
  };
  if (spec.paceM !== null) fields.pace = paceFor(sport, km, minutes);
  else fields.avg_speed_kmh = speedOf(km, minutes);
  if (sport === "跑步" && vals.cadence_spm && !vals.stride_m) {
    // 沒填步幅但有步頻：用距離 ÷ 總步數推。
    const stride = Math.round(((km * 1000) / (vals.cadence_spm * minutes)) * 100) / 100;
    if (stride >= 0.3 && stride <= 3) vals.stride_m = stride; // 推出來不合理（距離或步頻填錯）就不存
  }
  for (const [k, v] of Object.entries(vals)) fields[k] = v;
  if (sport === "游泳" && input.stroke_style) fields.stroke_style = input.stroke_style;
  if ((sport === "騎車" || sport === "跑步") && input.indoor) fields.indoor = true;
  return { ok: true, error: "", fields };
}

function numOf(id: string): number {
  return Number.parseInt(id.split("-")[1] ?? "0", 10) || 0;
}

function m(w: WorkoutLike): CardioMetrics {
  return w.cardio ?? emptyMetrics();
}

/** 有氧記完的檢討：跟同一項目上一次比距離、配速（騎車比均速）、心率，再依感覺給下次建議。w 是剛記的那次，workouts 含它（新的在前）。 */
export function reviewCardio(w: WorkoutLike, workouts: readonly WorkoutLike[]): string {
  const sport: Sport = isCardio(w.type) ? w.type : "跑步";
  const spec = SPORTS[sport];
  const km = w.distance_km;
  const me = m(w);
  const speed = me.avg_speed_kmh || speedOf(km, w.duration_min);
  const how = spec.paceM === null ? `均速 ${kg(speed)} km/h` : `配速 ${w.pace}${paceUnit(sport)}`;
  const lines = [`這次：${distText(sport, km)}、${kg(w.duration_min)} 分，${how}。`];
  if (me.avg_hr > 0) {
    lines.push(`心率：平均 ${me.avg_hr}${me.max_hr > 0 ? `、最大 ${me.max_hr}` : ""} bpm。`);
  }
  // 同一分鐘記兩筆時 started 一樣，用單號判先後
  const prev = workouts.find((x) => x.type === sport && numOf(x.id) < numOf(w.id));
  if (prev && prev.distance_km > 0) {
    const pm = m(prev);
    const dk = km - prev.distance_km;
    const part: string[] = [];
    if (Math.abs(dk) >= spec.distEps - 1e-9) {
      part.push(`距離${dk > 0 ? "多" : "少"} ${distText(sport, Math.abs(dk))}`);
    }
    // faster：> 0＝比上次快
    let faster = 0;
    if (spec.paceM === null) {
      const ps = pm.avg_speed_kmh || speedOf(prev.distance_km, prev.duration_min);
      const ds = Math.round((speed - ps) * 10) / 10;
      if (Math.abs(ds) >= CARDIO_RULES.speed_eps)
        part.push(`均速${ds > 0 ? "快" : "慢"} ${kg(Math.abs(ds))} km/h`);
      faster = ds;
    } else if (prev.pace) {
      const dp = paceSec(w.pace) - paceSec(prev.pace);
      const eps = sport === "跑步" ? CARDIO_RULES.pace_eps_run : CARDIO_RULES.pace_eps_other;
      if (Math.abs(dp) >= eps) {
        part.push(`配速${dp < 0 ? "快" : "慢"} ${Math.abs(dp)} 秒${paceUnit(sport)}`);
      }
      faster = -dp;
    }
    const dh = me.avg_hr > 0 && pm.avg_hr > 0 ? me.avg_hr - pm.avg_hr : 0;
    if (Math.abs(dh) >= CARDIO_RULES.hr_eps)
      part.push(`平均心率${dh < 0 ? "低" : "高"} ${Math.abs(dh)}`);
    lines.push(`跟上次比：${part.length > 0 ? part.join("、") : "差不多"}。`);
    if (dh <= -CARDIO_RULES.hr_eps && faster >= 0)
      lines.push("一樣快（或更快）心率卻更低，心肺有進步。");
  } else {
    lines.push(`這是第一筆${sport}，之後拿它當比較基準。`);
  }
  if (sport === "跑步" && me.cadence_spm > 0 && me.cadence_spm < CARDIO_RULES.low_cadence) {
    lines.push(`步頻 ${me.cadence_spm} 偏低：試著步子小一點、腳步快一點（170 上下比較省力）。`);
  }
  const speedWord = spec.paceM === null ? "速度" : "配速";
  if (w.feeling === "吃力") {
    lines.push(`下次：${speedWord}或距離先不要加，睡好再${spec.verb}。`);
  } else if (w.feeling === "輕鬆") {
    lines.push(
      `下次：可以多${spec.verb} ${spec.step}，或把${speedWord}加快一點點（二選一，不要同時）。`,
    );
  } else lines.push("下次：照這次的強度，穩定了再加距離。");
  return lines.join("\n");
}

/** 趨勢圖上的一個點。 */
export interface Point {
  id: string;
  date: string;
  value: number;
}

export interface MetricDef {
  key: string;
  label: string;
  /** 越低越好（配速、SWOLF、同強度的心率）：圖上往上＝變好。 */
  lower?: boolean;
  get: (w: WorkoutLike) => number;
  fmt: (v: number) => string;
}

const unitFmt = (u: string) => (v: number) => `${kg(Math.round(v * 100) / 100)}${u ? ` ${u}` : ""}`;

/** 這個項目可以畫的指標（順序＝選單順序）。 */
export function metricsFor(sport: Sport): MetricDef[] {
  const spec = SPORTS[sport];
  const list: MetricDef[] = [
    {
      key: "distance",
      label: "距離",
      get: (w) => (spec.unit === "m" ? Math.round(w.distance_km * 1000) : w.distance_km),
      fmt: unitFmt(spec.unit),
    },
    { key: "duration", label: "時間", get: (w) => w.duration_min, fmt: unitFmt("分") },
  ];
  if (spec.paceM === null) {
    list.push({
      key: "speed",
      label: "均速",
      get: (w) => m(w).avg_speed_kmh || speedOf(w.distance_km, w.duration_min),
      fmt: unitFmt("km/h"),
    });
  } else {
    list.push({
      key: "pace",
      label: "配速",
      lower: true,
      get: (w) => (w.pace ? paceSec(w.pace) : 0),
      fmt: (v) => `${mss(v)}${paceUnit(sport)}`,
    });
  }
  list.push({ key: "avg_hr", label: "平均心率", get: (w) => m(w).avg_hr, fmt: unitFmt("bpm") });
  for (const f of FIELDS[sport]) {
    list.push({
      key: f.key,
      label: f.label,
      lower: f.key === "swolf" || f.key === "gct_ms" || f.key === "vertical_osc_cm",
      get: (w) => m(w)[f.key],
      fmt: unitFmt(f.unit),
    });
  }
  return list;
}

/** 某項目某指標的趨勢（舊→新）；沒填的那次跳過。workouts 是新的在前。 */
export function cardioSeries(
  workouts: readonly WorkoutLike[],
  sport: Sport,
  metric: MetricDef,
): Point[] {
  return [...workouts]
    .filter((w) => w.type === sport)
    .reverse()
    .map((w) => ({ id: w.id, date: w.date, value: metric.get(w) }))
    .filter((p) => p.value > 0)
    .sort((a, b) => (a.date < b.date ? -1 : a.date > b.date ? 1 : 0));
}

/** 歷史清單的摘要：距離、時間、配速或均速、平均心率。 */
export function cardioSummary(w: WorkoutLike): string {
  const sport: Sport = isCardio(w.type) ? w.type : "跑步";
  const me = m(w);
  const parts = [distText(sport, w.distance_km), `${kg(w.duration_min)} 分`];
  if (SPORTS[sport].paceM === null) {
    const s = me.avg_speed_kmh || speedOf(w.distance_km, w.duration_min);
    if (s > 0) parts.push(`${kg(s)} km/h`);
  } else if (w.pace) parts.push(`${w.pace}${paceUnit(sport)}`);
  if (me.avg_hr > 0) parts.push(`♥ ${me.avg_hr}`);
  return parts.join(" · ");
}

/** 歷史展開後的指數清單（只列有填的），例：「步頻 172 步/分」。 */
export function cardioDetails(w: WorkoutLike): string[] {
  if (!isCardio(w.type)) return [];
  const me = m(w);
  const out: string[] = [];
  for (const f of [...COMMON_FIELDS, ...FIELDS[w.type]]) {
    if (me[f.key] > 0) out.push(`${f.label} ${kg(me[f.key])}${f.unit ? ` ${f.unit}` : ""}`);
  }
  if (w.type === "游泳") {
    if (me.pool_m > 0) out.push(`池長 ${me.pool_m} m${me.laps > 0 ? ` × ${me.laps} 趟` : ""}`);
    if (me.stroke_style) out.push(me.stroke_style);
  }
  if (me.indoor) out.push("室內");
  return out;
}
