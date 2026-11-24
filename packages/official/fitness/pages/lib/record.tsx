// 「記錄」分頁：重訓（挑器材、記這組、即時建議、收操）與有氧（跑步、划船、游泳、騎車，一次記完）。
import { Button, Segmented } from "@evergenie/ui";
import { useRef, useState } from "react";
import {
  CARDIO,
  COMMON_FIELDS,
  checkCardio,
  FIELDS,
  type FieldDef,
  isCardio,
  type NumKey,
  reviewCardio,
  SPORTS,
  type Sport,
  STROKES,
} from "./cardio";
import { review as coachReview, FEELS, feedback, RULES, type SetRec } from "./coach";
import { atStr, dateStr, parseAt } from "./dates";
import { EquipmentPicker, loadRemembered, saveRemembered } from "./picker";
import {
  addEquipment,
  addWorkout,
  archiveRecord,
  loadWorkouts,
  patchWorkout,
  reasonOf,
  WORKOUT,
} from "./store";
import {
  activeWorkout,
  type Equip,
  eqMapOf,
  groupSets,
  initialPick,
  isBodyweight,
  lastSetOf,
  newEquipmentFields,
  parseSetInput,
  planSet,
  sortWorkouts,
  staleCloseFields,
  toWorkout,
  type Workout,
} from "./workout";

const inputCls = "min-h-11 w-full rounded-lg border border-border bg-bg px-3 text-base";
const card = "flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4";
const feelOptions = FEELS.map((f) => ({ value: f, label: f }));

/** 重量、次數旁邊的 −／＋：在健身房單手按比打字快（DEV-146）。 */
function Stepper({
  label,
  value,
  onChange,
  step,
  min,
  decimal,
  testid,
  unitWord,
}: {
  label: string;
  /** −／＋ 按鈕唸的單位（公斤、下、秒）；不能含欄位名稱，不然「重量」這種標籤會對到按鈕。 */
  unitWord: string;
  value: string;
  onChange: (v: string) => void;
  step: number;
  min: number;
  decimal: boolean;
  testid: string;
}) {
  const bump = (d: number) => {
    const n = Number(value);
    // 空的按「−」不做事：空白不能變成 0 kg 記進去（FIT-13）。
    if ((value.trim() === "" || !Number.isFinite(n)) && d < 0) return;
    const base = value.trim() === "" || !Number.isFinite(n) ? 0 : n;
    const next = Math.max(min, Math.round((base + d) * 10) / 10);
    onChange(String(next));
  };
  const btn =
    "min-h-11 min-w-11 shrink-0 rounded-lg border border-border bg-bg text-lg font-semibold hover:bg-hover";
  return (
    <div className="flex flex-col gap-1 text-sm">
      <label htmlFor={testid} className="font-medium">
        {label}
      </label>
      <div className="flex items-center gap-1">
        <button
          type="button"
          className={btn}
          aria-label={`減 ${step} ${unitWord}`}
          onClick={() => bump(-step)}
        >
          −
        </button>
        <input
          id={testid}
          data-testid={testid}
          inputMode={decimal ? "decimal" : "numeric"}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className={`${inputCls} min-w-0 text-center`}
        />
        <button
          type="button"
          className={btn}
          aria-label={`加 ${step} ${unitWord}`}
          onClick={() => bump(step)}
        >
          ＋
        </button>
      </div>
    </div>
  );
}

/**
 * 重訓（DEV-146 重排）：由上往下是「這台記一組」→ 教練說 → 挑器材 → 這次已經做的（進行中＋收操）。
 * 常用的在最上面，已經做完的紀錄放最下面，記越多組也不會把挑器材擠下去（FIT-19）。
 */
export function Strength({
  workouts,
  equipment,
  reload,
  onManageEquipment,
  managing = false,
}: {
  workouts: Workout[];
  equipment: Equip[];
  reload: () => void;
  onManageEquipment: () => void;
  managing?: boolean;
}) {
  const eqm = eqMapOf(equipment);
  const active = activeWorkout(workouts);
  const [sel, setSel] = useState(() =>
    initialPick(loadRemembered(), workouts, equipment, new Date()),
  );
  const [weight, setWeight] = useState(() => {
    const l = sel ? lastSetOf(workouts, sel) : null;
    return l ? String(l.set.weight) : "";
  });
  const [reps, setReps] = useState(() => {
    const l = sel ? lastSetOf(workouts, sel) : null;
    return l ? String(l.set.reps) : "";
  });
  const [feel, setFeel] = useState<string>(() => {
    const l = sel ? lastSetOf(workouts, sel) : null;
    return l?.set.feel || "剛好";
  });
  const [fb, setFb] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // 錯誤是哪一區出的：記一組（表單）或進行中那張卡（收操、刪最後一組）；寫在按的那顆按鈕旁邊（FIT-12）。
  const [errorAt, setErrorAt] = useState<"set" | "active">("set");
  const [finishing, setFinishing] = useState(false);
  const [fFeel, setFFeel] = useState<string>("剛好");
  const [fNote, setFNote] = useState("");
  const [done, setDone] = useState("");
  const formRef = useRef<HTMLFormElement>(null);
  const reviewRef = useRef<HTMLDivElement>(null);

  const selected = equipment.find((e) => e.name === sel);
  const bodyweight = isBodyweight(selected);
  const last = sel ? lastSetOf(workouts, sel) : null;
  const unit = bodyweight ? selected?.unit || "次" : "下";

  const run = async (fn: () => Promise<void>, at: "set" | "active" = "set"): Promise<boolean> => {
    setBusy(true);
    setError("");
    setErrorAt(at);
    try {
      await fn();
      return true;
    } catch (e) {
      setError(reasonOf(e));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const pick = (name: string) => {
    setSel(name);
    saveRemembered(name);
    setFb([]);
    setDone("");
    setError("");
    const l = lastSetOf(workouts, name);
    setWeight(l ? String(l.set.weight) : "");
    setReps(l ? String(l.set.reps) : "");
    if (l?.set.feel) setFeel(l.set.feel);
    // 表單在挑器材上面：從下面的部位清單挑完，捲回表單（手機上不然看不到選了什麼）。
    requestAnimationFrame(() =>
      formRef.current?.scrollIntoView?.({ block: "nearest", behavior: "smooth" }),
    );
  };

  // 搜尋找不到：直接用搜尋的字新增這台器材並選它（肌群照名稱推，推不出來教練之後補；REQ-304）。
  const addFromSearch = (q: string) =>
    run(async () => {
      const n = q.trim().replace(/\s+/g, " ");
      if (!n || n.length > 30) throw new Error("器材名稱 1–30 字");
      if (!equipment.some((e) => e.name === n)) await addEquipment(newEquipmentFields(n));
      reload();
      pick(n);
    });

  const addSet = () =>
    run(async () => {
      if (!sel) throw new Error("先選一個器材");
      const c = parseSetInput(weight, reps, bodyweight, bodyweight ? `${unit}數` : "次數");
      if (!c.ok) throw new Error(c.error);
      const w = c.weight;
      const r = c.reps;
      const now = new Date();
      const today = dateStr(now);
      const rec: SetRec = { equipment: sel, weight: w, reps: r, feel };
      const plan = planSet(workouts, now);
      if (plan.stale) {
        // 忘了收操：舊的那筆標完成（備註加一句、檢討用規則算），這一組開新的一筆。
        await patchWorkout(plan.stale.id, staleCloseFields(plan.stale, workouts, eqm, today));
      }
      let id: string;
      if (plan.active) {
        id = plan.active.id;
        await patchWorkout(id, { sets: [...plan.active.sets, rec] });
      } else {
        id = await addWorkout(() => ({
          type: "重訓",
          date: today,
          started: atStr(now),
          status: "進行中",
          sets: [rec],
        }));
      }
      saveRemembered(sel); // 記一組也算「剛才在用」，重新計時
      const fresh = sortWorkouts(await loadWorkouts());
      setFb(feedback(fresh, id, eqm, today));
      setDone("");
      reload();
    });

  const removeLast = () =>
    run(async () => {
      if (!active) return;
      if (active.sets.length <= 1)
        await archiveRecord(WORKOUT, active.id); // 一組都沒有的訓練不留
      else await patchWorkout(active.id, { sets: active.sets.slice(0, -1) });
      setFb([]);
      reload();
    }, "active");

  const finish = () =>
    run(async () => {
      if (!active) return;
      const now = new Date();
      const note = fNote.trim().slice(0, 120);
      const closed: Workout = {
        ...active,
        status: "完成",
        finished: atStr(now),
        feeling: fFeel,
        note,
      };
      const all = sortWorkouts(workouts.map((x) => (x.id === active.id ? closed : x)));
      const text = coachReview(closed, all, eqm, dateStr(now));
      await patchWorkout(active.id, {
        status: "完成",
        finished: closed.finished,
        feeling: fFeel,
        ...(note ? { note } : {}),
        review: text,
        review_by: "規則",
      });
      setFinishing(false);
      setFNote("");
      setFb([]);
      setDone(text);
      // 收操了：下次來不要再預選這次的器材。
      setSel("");
      saveRemembered("");
      reload();
      // 收操按鈕在最下面，檢討在最上面：捲上去給他看。
      requestAnimationFrame(() =>
        reviewRef.current?.scrollIntoView?.({ block: "start", behavior: "smooth" }),
      );
    }, "active");

  const reps0 = bodyweight ? `${unit}數` : "次數";

  return (
    <div className="flex flex-col gap-4" data-testid="fitness-strength">
      {done ? (
        <div ref={reviewRef} data-testid="workout-review" className={`${card} scroll-mt-4`}>
          <h2 className="font-semibold">這次的檢討</h2>
          <p className="whitespace-pre-line text-sm">{done}</p>
        </div>
      ) : null}

      {sel ? (
        <form
          ref={formRef}
          data-testid="set-form"
          className={`${card} scroll-mt-4`}
          onSubmit={(e) => {
            e.preventDefault();
            void addSet();
          }}
        >
          <div className="flex items-baseline justify-between gap-2">
            <h2 className="min-w-0 break-words font-semibold">{sel}</h2>
            {active && active.sets.length > 0 ? (
              <span className="shrink-0 text-sm text-muted">
                這次第 {active.sets.filter((s) => s.equipment === sel).length + 1} 組
              </span>
            ) : null}
          </div>
          {last ? (
            <p className="text-sm text-muted">
              {last.current ? "這次" : last.date}最近一組：
              {last.set.weight > 0
                ? `${last.set.weight} kg × ${last.set.reps}`
                : `徒手 ${last.set.reps}`}
              （{last.set.feel}）
            </p>
          ) : (
            <p className="text-sm text-muted">這台還沒有記錄，先填第一組。</p>
          )}
          <div className={`grid gap-3 ${bodyweight ? "grid-cols-1" : "grid-cols-2"}`}>
            {bodyweight ? null : (
              <Stepper
                label="重量（kg）"
                testid="set-weight"
                value={weight}
                onChange={setWeight}
                step={RULES.step_kg}
                min={0}
                decimal
                unitWord="公斤"
              />
            )}
            <Stepper
              label={reps0}
              testid="set-reps"
              value={reps}
              onChange={setReps}
              step={1}
              min={1}
              decimal={false}
              unitWord={bodyweight ? unit : "下"}
            />
          </div>
          <span className="text-sm font-medium">這組感覺</span>
          <Segmented ariaLabel="這組感覺" options={feelOptions} value={feel} onChange={setFeel} />
          <Button type="submit" variant="primary" disabled={busy}>
            記這組
          </Button>
          {/* 錯誤放在按鈕正下方：放在整頁最下面會被手機底部列蓋住，按了像沒反應（FIT-12）。 */}
          {error && errorAt === "set" ? (
            <p role="alert" className="text-sm text-danger">
              {error}
            </p>
          ) : null}
        </form>
      ) : null}

      {fb.length > 0 || (selected && (selected.seat || selected.cues.length > 0)) ? (
        <div data-testid="coach-feedback" className={card}>
          <h2 className="font-semibold">教練說</h2>
          {fb.map((line) => (
            <p key={line} className="text-sm">
              {line}
            </p>
          ))}
          {/* 選到的器材的座位調整與注意事項：還沒記第一組就先看到（練前提醒）。 */}
          {selected?.seat ? (
            <p data-testid="coach-seat" className="text-sm font-medium">
              {selected.seat}
            </p>
          ) : null}
          {selected && selected.cues.length > 0 ? (
            <div data-testid="coach-cues" className="flex flex-col gap-1 text-sm">
              <span className="font-medium">注意事項</span>
              <ul className="flex list-disc flex-col gap-1 pl-5">
                {selected.cues.map((c) => (
                  <li key={c} className="break-words">
                    {c}
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </div>
      ) : null}

      <EquipmentPicker
        workouts={workouts}
        equipment={equipment}
        active={active}
        sel={sel}
        busy={busy}
        onPick={pick}
        onAdd={addFromSearch}
        onManage={onManageEquipment}
        managing={managing}
      />

      {error && errorAt === "set" && !sel ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}

      {active ? (
        <div data-testid="active-workout" className={card}>
          <div className="flex items-baseline justify-between gap-2">
            <h2 className="font-semibold">進行中：{active.sets.length} 組</h2>
            <span className="text-sm text-muted">{active.started.slice(11)} 開始</span>
          </div>
          {active.sets.length > 0 ? (
            <ol className="flex flex-col gap-2 text-sm">
              {groupSets(active.sets).map((g, gi, all) => (
                <li key={gi} className="flex flex-col gap-0.5">
                  <span className="min-w-0 break-words font-medium">{g.equipment}</span>
                  {g.sets.map((s, i) => (
                    <span key={i} className="flex justify-between gap-2 pl-3">
                      <span className="text-muted">
                        第{" "}
                        {all
                          .slice(0, gi)
                          .filter((x) => x.equipment === g.equipment)
                          .reduce((n, x) => n + x.sets.length, 0) +
                          i +
                          1}{" "}
                        組
                      </span>
                      {/* 右邊的重量、次數、感覺固定一行，不被切成「很／硬」（FIT-14）。 */}
                      <span
                        data-testid="active-set-detail"
                        className="shrink-0 whitespace-nowrap text-muted"
                      >
                        {s.weight > 0 ? `${s.weight} kg × ${s.reps}` : `徒手 ${s.reps}`} · {s.feel}
                      </span>
                    </span>
                  ))}
                </li>
              ))}
            </ol>
          ) : null}
          {finishing ? (
            <form
              data-testid="finish-form"
              className="flex flex-col gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                void finish();
              }}
            >
              <span className="text-sm font-medium">整體感覺</span>
              <Segmented
                ariaLabel="整體感覺"
                options={feelOptions}
                value={fFeel}
                onChange={setFFeel}
              />
              <label className="flex flex-col gap-1 text-sm">
                <span className="font-medium">備註（可空）</span>
                <input
                  value={fNote}
                  onChange={(e) => setFNote(e.target.value)}
                  maxLength={120}
                  className={inputCls}
                />
              </label>
              <div className="flex gap-2">
                <Button type="submit" variant="primary" disabled={busy}>
                  確定收操
                </Button>
                <Button onClick={() => setFinishing(false)}>取消</Button>
              </div>
            </form>
          ) : (
            <div className="flex flex-wrap gap-2">
              <Button variant="primary" onClick={() => setFinishing(true)}>
                收操
              </Button>
              <Button
                variant="ghost"
                disabled={busy || active.sets.length === 0}
                onClick={() => void removeLast()}
              >
                刪掉最後一組
              </Button>
            </div>
          )}
          {error && errorAt === "active" ? (
            <p role="alert" className="text-sm text-danger">
              {error}
            </p>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function localInput(d: Date): string {
  return atStr(d).replace(" ", "T");
}

const fieldCls = "flex flex-col gap-1 text-sm";

function NumField({
  f,
  value,
  onChange,
}: {
  f: FieldDef;
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <label className={fieldCls}>
      <span className="font-medium">
        {f.label}
        {f.unit ? `（${f.unit}）` : ""}
      </span>
      <input
        inputMode={f.int ? "numeric" : "decimal"}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={inputCls}
      />
    </label>
  );
}

const sportOptions = CARDIO.map((s) => ({ value: s, label: s }));
const poolOptions = [
  { value: "25", label: "25 m 池" },
  { value: "50", label: "50 m 池" },
  { value: "0", label: "開放水域" },
];

/** 有氧一次記完：先挑項目（跑步、划船、游泳、騎車），共同的是時間、距離、心率，其他欄位各項目不同。 */
export function CardioForm({ workouts, reload }: { workouts: Workout[]; reload: () => void }) {
  const [sport, setSport] = useState<Sport>(
    () => (sortWorkouts(workouts).find((w) => isCardio(w.type))?.type as Sport) ?? "跑步",
  );
  const [dist, setDist] = useState("");
  const [min, setMin] = useState("");
  const [vals, setVals] = useState<Partial<Record<NumKey, string>>>({});
  const [pool, setPool] = useState("25");
  const [stroke, setStroke] = useState("");
  const [indoor, setIndoor] = useState(false);
  const [feel, setFeel] = useState<string>("剛好");
  const [note, setNote] = useState("");
  const [when, setWhen] = useState(() => localInput(new Date()));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState("");

  const spec = SPORTS[sport];
  const setVal = (k: NumKey) => (v: string) => setVals((o) => ({ ...o, [k]: v }));
  const pick = (s: Sport) => {
    setSport(s);
    setVals({});
    setDist("");
    setIndoor(false);
    setStroke("");
    setError("");
    setDone("");
  };
  const pickPool = (p: string) => {
    setPool(p);
    // 換成開放水域：趟數沒意義，清掉；換回泳池：自己填的距離不用了
    if (p === "0") setVals((o) => ({ ...o, laps: "" }));
    else setDist("");
  };
  const swimPool = sport === "游泳" && pool !== "0";
  const lapsDist = swimPool && vals.laps ? Number(pool) * Number(vals.laps) : 0;
  const main = [...COMMON_FIELDS, ...FIELDS[sport]].filter((f) => !f.more);
  const more = [...COMMON_FIELDS, ...FIELDS[sport]].filter((f) => f.more);

  const submit = async () => {
    setBusy(true);
    setError("");
    try {
      const values = { ...vals };
      if (sport === "游泳") values.pool_m = pool;
      const c = checkCardio(sport, {
        dist: swimPool ? "" : dist,
        minutes: min,
        values,
        stroke_style: stroke,
        indoor,
      });
      if (!c.ok) throw new Error(c.error);
      const start = parseAt(when);
      if (!start) throw new Error("開始時間要填完整");
      if (start.getTime() > Date.now() + 60_000) throw new Error("開始時間不能在未來");
      const end = new Date(start.getTime() + Math.round(Number(min)) * 60_000);
      let text = "";
      await addWorkout((id) => {
        const fields: Record<string, unknown> = {
          type: sport,
          date: dateStr(start),
          started: atStr(start),
          finished: atStr(end),
          status: "完成",
          feeling: feel,
          ...(note.trim() ? { note: note.trim().slice(0, 120) } : {}),
          ...c.fields,
          source: "web",
        };
        const me = toWorkout({ id, fields });
        text = reviewCardio(me, sortWorkouts([me, ...workouts]));
        return { ...fields, review: text, review_by: "規則" };
      });
      setDone(text);
      setDist("");
      setMin("");
      setVals({});
      setNote("");
      setWhen(localInput(new Date()));
      reload();
    } catch (e) {
      setError(reasonOf(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-4" data-testid="fitness-cardio">
      <Segmented ariaLabel="有氧項目" options={sportOptions} value={sport} onChange={pick} />
      <form
        className={card}
        data-testid="cardio-form"
        data-sport={sport}
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <h2 className="font-semibold">{sport}</h2>
        {sport === "游泳" ? (
          <>
            <Segmented ariaLabel="泳池" options={poolOptions} value={pool} onChange={pickPool} />
            {swimPool ? (
              <NumField
                f={{ key: "laps", label: "趟數", unit: "", min: 1, max: 800, int: true }}
                value={vals.laps ?? ""}
                onChange={setVal("laps")}
              />
            ) : null}
          </>
        ) : null}
        <div className="grid grid-cols-2 gap-3">
          {swimPool ? (
            <p className="flex flex-col justify-end gap-1 text-sm" data-testid="swim-distance">
              <span className="font-medium">距離</span>
              <span className="min-h-11 content-center">
                {lapsDist > 0 ? `${lapsDist} m（池長 × 趟數）` : "填趟數就會算"}
              </span>
            </p>
          ) : (
            <label className={fieldCls}>
              <span className="font-medium">距離（{spec.unit === "m" ? "公尺" : "km"}）</span>
              <input
                inputMode="decimal"
                value={dist}
                onChange={(e) => setDist(e.target.value)}
                className={inputCls}
              />
            </label>
          )}
          <label className={fieldCls}>
            <span className="font-medium">時間（分鐘）</span>
            <input
              inputMode="decimal"
              value={min}
              onChange={(e) => setMin(e.target.value)}
              className={inputCls}
            />
          </label>
          {main.map((f) => (
            <NumField key={f.key} f={f} value={vals[f.key] ?? ""} onChange={setVal(f.key)} />
          ))}
        </div>
        {sport === "游泳" ? (
          <label className={fieldCls}>
            <span className="font-medium">主要泳姿</span>
            <select value={stroke} onChange={(e) => setStroke(e.target.value)} className={inputCls}>
              <option value="">不填</option>
              {STROKES.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
        ) : null}
        {sport === "跑步" || sport === "騎車" ? (
          <label className="flex min-h-11 items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={indoor}
              onChange={(e) => setIndoor(e.target.checked)}
              className="h-5 w-5"
            />
            <span>室內（{sport === "跑步" ? "跑步機" : "飛輪、訓練台"}）</span>
          </label>
        ) : null}
        {more.length > 0 ? (
          <details data-testid="cardio-more" className="rounded-lg border border-border px-3">
            <summary className="min-h-11 cursor-pointer content-center text-sm font-medium">
              更多指數（手錶或機器上有再填）
            </summary>
            <div className="grid grid-cols-2 gap-3 pb-3">
              {more.map((f) => (
                <NumField key={f.key} f={f} value={vals[f.key] ?? ""} onChange={setVal(f.key)} />
              ))}
            </div>
          </details>
        ) : null}
        <span className="text-sm font-medium">感覺</span>
        <Segmented
          ariaLabel={`${sport}感覺`}
          options={feelOptions}
          value={feel}
          onChange={setFeel}
        />
        <label className={fieldCls}>
          <span className="font-medium">開始時間</span>
          <input
            type="datetime-local"
            value={when}
            onChange={(e) => setWhen(e.target.value)}
            className={inputCls}
          />
        </label>
        <label className={fieldCls}>
          <span className="font-medium">備註（可空）</span>
          <input
            value={note}
            onChange={(e) => setNote(e.target.value)}
            maxLength={120}
            className={inputCls}
          />
        </label>
        <Button type="submit" variant="primary" disabled={busy}>
          {busy ? "記錄中…" : `記這次${sport}`}
        </Button>
      </form>
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
      {done ? (
        <div data-testid="cardio-review" className={card}>
          <h2 className="font-semibold">這次的檢討</h2>
          <p className="whitespace-pre-line text-sm">{done}</p>
        </div>
      ) : null}
    </div>
  );
}
