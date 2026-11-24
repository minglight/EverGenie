import { Avatar } from "../components/Avatar";
import { cn } from "../lib/cn";

export interface AgentOption {
  id: string;
  name: string;
  color?: string;
  colorDark?: string;
  avatarUrl?: string;
}

export interface AgentPickerProps {
  agents: AgentOption[];
  value: string;
  onChange: (id: string) => void;
  /** 群組名稱（讀屏軟體用）。 */
  label: string;
  className?: string;
}

// 秘書用頭像選擇器（FEN-17）：一排頭像＋人名，單選。
export function AgentPicker({ agents, value, onChange, label, className }: AgentPickerProps) {
  return (
    <fieldset
      className={cn("m-0 flex min-w-0 flex-wrap gap-1.5 border-0 p-0", className)}
      aria-label={label}
    >
      {agents.map((a) => {
        const on = a.id === value;
        return (
          <button
            key={a.id}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(a.id)}
            className={cn(
              "flex min-h-11 items-center gap-2 rounded-full border py-1 pr-3.5 pl-1.5 text-[14px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
              on
                ? "border-accent bg-accent-soft font-semibold text-accent"
                : "border-border bg-surface text-fg",
            )}
          >
            <Avatar
              name={a.name}
              color={a.color}
              colorDark={a.colorDark}
              src={a.avatarUrl}
              size={28}
            />
            {a.name}
          </button>
        );
      })}
    </fieldset>
  );
}
