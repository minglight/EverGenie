import type { KnowledgeBlock } from "../data/knowledge";
import { t } from "../i18n/zh-TW";

// 「她帶的知識」：可選知識的勾選清單（設定 → 職位與秘書、新家歡迎頁共用，DEV-161）。
// 每一列整列可點（至少 44px，UXC-5）：勾選框＋標題＋一句說明。基本知識一定帶，不列。
// 沒有可選知識（blocks 是空的）就整塊不畫。
export function KnowledgePicker({
  agent,
  blocks,
  checked,
  onToggle,
  disabled,
  hint = true,
}: {
  /** 職位 id（只用來分辨測試用的 data-agent）。 */
  agent: string;
  blocks: readonly KnowledgeBlock[];
  checked: ReadonlySet<string>;
  onToggle: (id: string, next: boolean) => void;
  disabled?: boolean;
  /** 底下那句說明；歡迎頁空間小，同樣要有，所以預設都畫。 */
  hint?: boolean;
}) {
  if (blocks.length === 0) return null;
  return (
    <fieldset
      data-testid="knowledge"
      data-agent={agent}
      className="m-0 flex min-w-0 flex-col gap-1 border-0 p-0"
    >
      <legend className="mb-1 p-0 text-xs font-semibold text-muted">{t("knowledge.title")}</legend>
      <ul className="m-0 flex list-none flex-col gap-1 p-0">
        {blocks.map((b) => (
          <li key={b.id}>
            <label className="flex min-h-11 cursor-pointer items-start gap-3 rounded-xl border border-border bg-surface px-3 py-2.5 has-[:disabled]:cursor-not-allowed has-[:disabled]:opacity-60 has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-focus">
              <input
                type="checkbox"
                data-testid="knowledge-block"
                data-block={b.id}
                className="mt-0.5 size-5 shrink-0 accent-[var(--accent)]"
                checked={checked.has(b.id)}
                disabled={disabled}
                onChange={(e) => onToggle(b.id, e.target.checked)}
              />
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="text-[15px] leading-snug font-medium break-words">{b.title}</span>
                {b.summary ? (
                  <span className="text-[13px] leading-snug break-words text-muted">
                    {b.summary}
                  </span>
                ) : null}
              </span>
            </label>
          </li>
        ))}
      </ul>
      {hint ? <p className="m-0 text-xs leading-snug text-muted">{t("knowledge.hint")}</p> : null}
    </fieldset>
  );
}
