// 器材清單（頁內的小抽屜）：新增（用關鍵字推肌群，推不出來請教練補）、改肌群、移除（封存）。
import { Button, Segmented } from "@evergenie/ui";
import { useState } from "react";
import { MUSCLES } from "./coach";
import { addEquipment, archiveRecord, EQUIPMENT, patchEquipment, reasonOf } from "./store";
import { type Equip, newEquipmentFields, parseCues, parseMuscles } from "./workout";

const inputCls = "min-h-11 rounded-lg border border-border bg-bg px-3 text-base";

export function EquipmentPanel({
  equipment,
  onChanged,
}: {
  equipment: Equip[];
  onChanged: () => void;
}) {
  const [name, setName] = useState("");
  const [bodyweight, setBodyweight] = useState(false);
  const [unit, setUnit] = useState<"次" | "秒">("次");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  const [editing, setEditing] = useState("");
  const [removing, setRemoving] = useState("");
  const [primary, setPrimary] = useState("");
  const [secondary, setSecondary] = useState("");
  // 正在改哪一台的座位與注意事項（跟改肌群分開，一次只開一個表單）。
  const [setting, setSetting] = useState("");
  const [seat, setSeat] = useState("");
  const [cues, setCues] = useState("");

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

  const add = () =>
    run(async () => {
      const n = name.trim().replace(/\s+/g, " ");
      if (!n || n.length > 30) throw new Error("器材名稱 1–30 字");
      if (equipment.some((e) => e.name === n)) throw new Error(`已經有「${n}」了`);
      const fields = newEquipmentFields(n);
      if (bodyweight) {
        fields.type = "徒手";
        fields.unit = unit;
      }
      await addEquipment(fields);
      const prim = fields.primary as string[];
      setNote(
        prim.length > 0
          ? `已新增「${n}」，主練肌群：${prim.join("、")}`
          : `已新增「${n}」，推不出練哪些肌群，請教練補`,
      );
      setName("");
      onChanged();
    });

  const saveMuscles = (e: Equip) =>
    run(async () => {
      const p = parseMuscles(primary);
      const s = parseMuscles(secondary);
      const bad = [...p.bad, ...s.bad];
      if (bad.length > 0) throw new Error(`不認得的肌群：${bad.join("、")}`);
      if (p.ok.length === 0) throw new Error("主練至少要一個肌群");
      await patchEquipment(e.id, {
        primary: p.ok,
        secondary: s.ok.filter((m) => !p.ok.includes(m)),
      });
      setEditing("");
      onChanged();
    });

  const saveSetting = (e: Equip) =>
    run(async () => {
      const s = seat.trim();
      if (s.length > 60) throw new Error("座位調整最多 60 字");
      await patchEquipment(e.id, { seat: s, cues: parseCues(cues) });
      setSetting("");
      onChanged();
    });

  return (
    <div data-testid="equipment-panel" className="flex flex-col gap-3">
      <form
        className="flex flex-col gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void add();
        }}
      >
        <label className="flex flex-col gap-1 text-sm">
          <span className="font-medium">新增器材（或徒手動作）</span>
          <input
            data-testid="equipment-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className={inputCls}
            placeholder="例：啞鈴彎舉"
          />
        </label>
        <div className="flex flex-wrap items-center gap-3">
          <label className="flex min-h-11 items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={bodyweight}
              onChange={(e) => setBodyweight(e.target.checked)}
              className="size-5"
            />
            徒手動作（沒有重量）
          </label>
          {bodyweight ? (
            <Segmented
              ariaLabel="單位"
              options={[
                { value: "次", label: "次" },
                { value: "秒", label: "秒" },
              ]}
              value={unit}
              onChange={setUnit}
            />
          ) : null}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" variant="primary" disabled={busy || !name.trim()}>
            新增
          </Button>
          {note ? (
            <span role="status" className="text-sm text-muted">
              {note}
            </span>
          ) : null}
        </div>
      </form>
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
      {equipment.length === 0 ? (
        <p className="text-sm text-muted">還沒有器材，先新增一個。</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {equipment.map((e) => (
            <li
              key={e.id}
              data-testid="equipment-row"
              data-name={e.name}
              className="flex flex-col gap-2 rounded-lg border border-border p-3"
            >
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <span className="font-medium">
                  {e.name}
                  {e.type === "徒手" ? (
                    <span className="text-sm font-normal text-muted">
                      （徒手 · {e.unit || "次"}）
                    </span>
                  ) : null}
                </span>
                <span className="text-sm text-muted">
                  {e.primary.length > 0
                    ? `${e.primary.join("、")}${e.secondary.length > 0 ? `（輔助：${e.secondary.join("、")}）` : ""}`
                    : "肌群待教練補"}
                </span>
              </div>
              {setting !== e.id && (e.seat || e.cues.length > 0) ? (
                <div
                  data-testid="equipment-setting"
                  className="flex flex-col gap-1 text-sm text-muted"
                >
                  {e.seat ? <span>{e.seat}</span> : null}
                  {e.cues.length > 0 ? (
                    <span>
                      注意事項 {e.cues.length} 條：{e.cues[0]}
                      {e.cues.length > 1 ? "…" : ""}
                    </span>
                  ) : null}
                </div>
              ) : null}
              {setting === e.id ? (
                <form
                  className="flex flex-col gap-2"
                  onSubmit={(ev) => {
                    ev.preventDefault();
                    void saveSetting(e);
                  }}
                >
                  <label className="flex flex-col gap-1 text-sm">
                    <span className="font-medium">座位調整（可空）</span>
                    <input
                      data-testid="equipment-seat"
                      value={seat}
                      onChange={(ev) => setSeat(ev.target.value)}
                      className={inputCls}
                      placeholder="例：座位 3"
                    />
                  </label>
                  <label className="flex flex-col gap-1 text-sm">
                    <span className="font-medium">注意事項（一行一條，最重要的放第一行）</span>
                    <textarea
                      data-testid="equipment-cues"
                      value={cues}
                      onChange={(ev) => setCues(ev.target.value)}
                      rows={4}
                      className="rounded-lg border border-border bg-bg px-3 py-2 text-base"
                    />
                  </label>
                  <div className="flex gap-2">
                    <Button type="submit" variant="primary" disabled={busy}>
                      存
                    </Button>
                    <Button onClick={() => setSetting("")}>取消</Button>
                  </div>
                </form>
              ) : editing === e.id ? (
                <form
                  className="flex flex-col gap-2"
                  onSubmit={(ev) => {
                    ev.preventDefault();
                    void saveMuscles(e);
                  }}
                >
                  <label className="flex flex-col gap-1 text-sm">
                    <span className="font-medium">主練肌群</span>
                    <input
                      value={primary}
                      onChange={(ev) => setPrimary(ev.target.value)}
                      className={inputCls}
                      list="fitness-muscles"
                    />
                  </label>
                  <label className="flex flex-col gap-1 text-sm">
                    <span className="font-medium">輔助肌群（可空）</span>
                    <input
                      value={secondary}
                      onChange={(ev) => setSecondary(ev.target.value)}
                      className={inputCls}
                      list="fitness-muscles"
                    />
                  </label>
                  <p className="text-xs text-muted">
                    多個用頓號或逗號分隔，只能用名單裡的肌群名稱。
                  </p>
                  <div className="flex gap-2">
                    <Button type="submit" variant="primary" disabled={busy}>
                      存肌群
                    </Button>
                    <Button onClick={() => setEditing("")}>取消</Button>
                  </div>
                </form>
              ) : removing === e.id ? (
                <div
                  className="flex flex-wrap items-center gap-2 text-sm"
                  data-testid="equipment-confirm"
                >
                  <span>確定移除？（歷史紀錄不動，只是不再出現在清單）</span>
                  <Button
                    variant="danger"
                    disabled={busy}
                    onClick={() =>
                      void run(async () => {
                        await archiveRecord(EQUIPMENT, e.id);
                        setRemoving("");
                        onChanged();
                      })
                    }
                  >
                    移除
                  </Button>
                  <Button onClick={() => setRemoving("")}>取消</Button>
                </div>
              ) : (
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="ghost"
                    aria-label={`改 ${e.name} 的肌群`}
                    onClick={() => {
                      setPrimary(e.primary.join("、"));
                      setSecondary(e.secondary.join("、"));
                      setSetting("");
                      setEditing(e.id);
                    }}
                  >
                    改肌群
                  </Button>
                  <Button
                    variant="ghost"
                    aria-label={`改 ${e.name} 的座位與注意事項`}
                    onClick={() => {
                      setSeat(e.seat);
                      setCues(e.cues.join("\n"));
                      setEditing("");
                      setSetting(e.id);
                    }}
                  >
                    座位與注意事項
                  </Button>
                  <Button
                    variant="ghost"
                    aria-label={`移除 ${e.name}`}
                    onClick={() => setRemoving(e.id)}
                  >
                    移除
                  </Button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      <datalist id="fitness-muscles">
        {MUSCLES.map((m) => (
          <option key={m} value={m} />
        ))}
      </datalist>
    </div>
  );
}
