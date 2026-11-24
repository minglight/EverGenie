import { cn } from "@evergenie/ui";

export interface ToggleProps {
  checked: boolean;
  onChange: (next: boolean) => void;
  /** 螢幕閱讀器與測試用的名稱（例如「啟用 示範套件」）。 */
  label: string;
  disabled?: boolean;
}

// 開關：role="switch"；可點範圍 44×44（FEN-7），看得見的軌道是 46×28。
export function Toggle({ checked, onChange, label, disabled }: ToggleProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className="group flex min-h-11 min-w-12 shrink-0 items-center justify-center focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
    >
      <span
        className={cn(
          "relative h-7 w-[46px] rounded-full transition-colors group-focus-visible:outline-2 group-focus-visible:outline-offset-2 group-focus-visible:outline-focus",
          checked ? "bg-accent" : "bg-border-strong",
        )}
      >
        <span
          className={cn(
            "absolute top-0.5 left-0.5 size-6 rounded-full bg-white shadow-[0_1px_3px_rgb(0_0_0/0.3)] transition-transform",
            checked && "translate-x-[18px]",
          )}
        />
      </span>
    </button>
  );
}
