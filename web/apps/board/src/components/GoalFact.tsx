import { ApiError } from "@evergenie/sdk-web";
import { cn, Spinner } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { activeGoalsQuery, goalQuery } from "../data/queries";
import { homeSearch, PRIMARY, type SiteRef } from "../data/sites";
import { t } from "../i18n/zh-TW";
import { HIT_44 } from "../lib/hit";

// 任務單與待辦詳情的「Goal」欄。2026-10-28 起不看簡單／完整模式，一律顯示（作者：Goal 直接放在任務單，REQ-237）。
// 給了 onChange 就能就地改：選一個進行中的 Goal，或「不掛 Goal」（REQ-232）；沒掛的也顯示，才有地方掛上。
export function GoalFact({
  id,
  onChange,
  site = PRIMARY,
}: {
  id: string | undefined;
  onChange?: (goal: string) => Promise<void>;
  /** 任務單或待辦在哪台（預設主伺服器）：Goal 跟著在同一台。 */
  site?: SiteRef;
}) {
  const goal = useQuery({ ...goalQuery(id ?? "", site), enabled: Boolean(id) });
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  if (!id && !onChange) return null;

  const pick = async (next: string) => {
    if (next === (id ?? "")) {
      setEditing(false);
      return;
    }
    setBusy(true);
    setError("");
    try {
      await onChange?.(next);
      setEditing(false);
    } catch (err) {
      setError(
        t("goal.pick.failed", {
          reason: err instanceof ApiError ? err.message : t("settings.error.generic"),
        }),
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <div data-testid="goal-fact" className="col-span-2 flex min-w-0 flex-col gap-0.5 sm:col-span-1">
      <span className="text-xs text-muted">{t("goal.field")}</span>
      {editing ? (
        <span className="flex items-center gap-2">
          <GoalSelect value={id ?? ""} disabled={busy} site={site} onPick={(v) => void pick(v)} />
          {busy ? <Spinner /> : null}
        </span>
      ) : (
        <span className="flex min-w-0 flex-wrap items-baseline gap-x-1.5 text-sm font-medium break-words">
          {id ? (
            <>
              <Link
                to="/goals/$id"
                params={{ id }}
                search={homeSearch(site)}
                className="font-mono text-accent focus-visible:outline-2 focus-visible:outline-focus"
              >
                {id}
              </Link>
              {goal.data?.title ? <span>{goal.data.title}</span> : null}
            </>
          ) : (
            <span className="text-muted">{t("goal.pick.none")}</span>
          )}
          {onChange ? (
            <button
              type="button"
              onClick={() => setEditing(true)}
              aria-label={t(id ? "goal.pick.change" : "goal.pick.attach")}
              className={cn(
                HIT_44,
                "min-h-8 rounded-md px-1.5 text-[13px] font-semibold text-accent hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus",
              )}
            >
              {t(id ? "goal.pick.change.short" : "goal.pick.attach")}
            </button>
          ) : null}
        </span>
      )}
      {error ? (
        <span role="alert" className="text-xs text-danger">
          {error}
        </span>
      ) : null}
    </div>
  );
}

// 選 Goal：進行中的都列；目前掛的那個就算已經結束也留在選項裡，不會因為打開選單就被換掉。
// 選了就存；按 Esc 或離開不選就收起來。
function GoalSelect({
  value,
  disabled,
  site,
  onPick,
}: {
  value: string;
  disabled: boolean;
  site: SiteRef;
  onPick: (goal: string) => void;
}) {
  const active = useQuery(activeGoalsQuery(site));
  const current = useQuery({ ...goalQuery(value, site), enabled: Boolean(value) });
  const options = active.data ?? [];
  const missing = value && !options.some((g) => g.id === value);
  return (
    <select
      // biome-ignore lint/a11y/noAutofocus: 按「改」之後直接到選單上，不用再點一次
      autoFocus
      aria-label={t("goal.pick.label")}
      value={value}
      disabled={disabled}
      onChange={(e) => onPick(e.target.value)}
      onBlur={() => {
        // 存的時候選單變 disabled 也會失焦，那不是「離開不選」。
        if (!disabled) onPick(value);
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") onPick(value);
      }}
      className="min-h-10 w-full min-w-0 rounded-xl border border-border-strong bg-bg px-2.5 text-sm text-fg focus-visible:outline-2 focus-visible:outline-focus"
    >
      <option value="">{t("goal.pick.none")}</option>
      {missing ? (
        <option value={value}>
          {value} {current.data?.title ?? ""}
        </option>
      ) : null}
      {options.map((g) => (
        <option key={g.id} value={g.id}>
          {g.id} {g.title}
        </option>
      ))}
    </select>
  );
}
