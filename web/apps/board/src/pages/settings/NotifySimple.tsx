import { cn } from "@evergenie/ui";
import { Check } from "lucide-react";
import { useState } from "react";
import { type TextKey, t } from "../../i18n/zh-TW";
import {
  type CategoryId,
  overrideCount,
  type PresetId,
  presetIds,
  presetRows,
  withPreset,
} from "./prefs";
import type { AutosavedPrefs } from "./useAutosavedPrefs";

// 簡單模式的通知：三張預設組卡片（安靜／標準／積極）。選了只改 preset，不寫逐類設定；
// 每張卡可以展開「每類會怎麼送」，內容來自核心的預設表（跟實際送法一致）。
export function PresetCards({
  auto,
}: {
  auto: AutosavedPrefs & { prefs: NonNullable<AutosavedPrefs["prefs"]> };
}) {
  const { prefs } = auto;
  const [open, setOpen] = useState<PresetId | null>(null);
  const [pending, setPending] = useState<PresetId | null>(null);
  const current = prefs.preset ?? "standard";
  const overrides = overrideCount(prefs);

  const apply = (id: PresetId) => {
    setPending(null);
    auto.edit("preset", (p) => withPreset(p, id));
  };
  const choose = (id: PresetId) => {
    if (id === current && overrides === 0) return;
    // 有自己調過的設定：選預設組會清掉它們，先確認一次。
    if (overrides > 0) setPending(id);
    else apply(id);
  };

  return (
    <div className="flex flex-col gap-3">
      <fieldset className="m-0 flex flex-col gap-2.5 border-0 p-0">
        <legend className="sr-only">{t("settings.notify.simple.group")}</legend>
        {presetIds.map((id) => {
          const on = id === current;
          const expanded = open === id;
          return (
            <div
              key={id}
              data-testid={`preset-${id}`}
              className={cn(
                "flex flex-col gap-2 rounded-[18px] border px-4 py-3.5 shadow-[var(--shadow-card)]",
                on ? "border-accent bg-accent-soft" : "border-border bg-surface",
              )}
            >
              <button
                type="button"
                aria-pressed={on}
                onClick={() => choose(id)}
                className="flex min-h-11 items-start gap-3 text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
              >
                <span
                  aria-hidden="true"
                  className={cn(
                    "mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full border",
                    on ? "border-accent bg-accent text-accent-fg" : "border-border-strong",
                  )}
                >
                  {on ? <Check className="size-3.5" strokeWidth={3} /> : null}
                </span>
                <span className="min-w-0">
                  <span className="block text-[15px] leading-snug font-semibold">
                    {t(`settings.notify.preset.${id}` as TextKey)}
                  </span>
                  <span className="mt-0.5 block text-[13px] leading-snug text-muted">
                    {t(`settings.notify.preset.${id}.hint` as TextKey)}
                  </span>
                </span>
              </button>
              <button
                type="button"
                aria-expanded={expanded}
                onClick={() => setOpen(expanded ? null : id)}
                className="flex min-h-11 w-fit items-center rounded-lg px-2 text-[13px] font-semibold text-accent focus-visible:outline-2 focus-visible:outline-focus"
              >
                {t(expanded ? "settings.notify.preset.hide" : "settings.notify.preset.show")}
              </button>
              {expanded ? (
                <ul
                  data-testid={`preset-${id}-detail`}
                  className="m-0 flex list-none flex-col gap-1.5 rounded-[12px] bg-surface-2 p-3 text-[13px]"
                >
                  {presetRows(prefs, id).map((row) => (
                    <li key={row.category} className="flex justify-between gap-3">
                      <span>
                        {t(`settings.notify.cat.${row.category as CategoryId}` as TextKey)}
                      </span>
                      <span className="text-muted">
                        {row.mode === "digest"
                          ? t("settings.notify.preset.digestEvery", { n: row.hours })
                          : t(`settings.notify.mode.${row.mode}` as TextKey)}
                      </span>
                    </li>
                  ))}
                </ul>
              ) : null}
            </div>
          );
        })}
      </fieldset>
      {overrides > 0 ? (
        <p
          data-testid="notify-overrides"
          className="rounded-[12px] bg-attention-soft px-3.5 py-2.5 text-[13px] leading-snug text-attention-fg"
        >
          {t("settings.notify.overrides", { n: overrides })}
        </p>
      ) : null}
      {pending ? (
        <div
          role="alertdialog"
          aria-label={t("settings.notify.overrides.title")}
          data-testid="notify-overrides-confirm"
          className="flex flex-col gap-2.5 rounded-[14px] border border-border-strong bg-surface px-4 py-3"
        >
          <p className="text-[14px] leading-snug">
            {t("settings.notify.overrides.confirm", {
              name: t(`settings.notify.preset.${pending}` as TextKey),
              n: overrides,
            })}
          </p>
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              onClick={() => apply(pending)}
              className="min-h-11 rounded-xl bg-accent px-4 text-[15px] font-semibold text-accent-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
            >
              {t("settings.notify.overrides.yes")}
            </button>
            <button
              type="button"
              onClick={() => setPending(null)}
              className="min-h-11 rounded-xl border border-border-strong bg-surface px-4 text-[15px] font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
            >
              {t("settings.notify.overrides.no")}
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}
