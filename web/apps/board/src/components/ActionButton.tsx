import { cn, Kbd, Spinner } from "@evergenie/ui";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { HIT_44 } from "../lib/hit";

export interface ActionButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "type"> {
  /** primary：實心（一個畫面一個主要動作）；secondary：灰底；outline：框線；quiet：沒底；danger：紅底。 */
  tone?: "primary" | "secondary" | "outline" | "quiet" | "danger";
  /** card：今天頁卡片（高 46）；page：詳細頁（高 44）；pane：桌機右欄動作列（高 38）。 */
  size?: "card" | "page" | "pane";
  /** 桌機鍵盤快捷鍵的鍵帽（A、R）。 */
  keycap?: string;
  busy?: boolean;
  children: ReactNode;
  type?: "button" | "submit";
}

const tones = {
  primary: "bg-accent text-accent-fg font-semibold",
  secondary: "bg-surface-2 text-fg font-medium",
  outline: "border border-border-strong bg-surface text-fg font-medium",
  quiet: "bg-transparent text-muted font-medium",
  danger: "bg-danger text-accent-fg font-semibold",
} as const;

const sizes = {
  card: "h-[46px] rounded-xl px-5 text-[15px]",
  page: "h-11 rounded-xl px-5 text-[15px]",
  // 視覺高 38，可點範圍用 ::after 補到 44（手機的角色頁也用這個尺寸，DEV-156 U-11）。
  pane: `${HIT_44} h-[38px] rounded-[10px] px-4 text-sm`,
} as const;

export function actionButtonClass(
  tone: NonNullable<ActionButtonProps["tone"]>,
  size: NonNullable<ActionButtonProps["size"]>,
  className?: string,
): string {
  return cn(
    "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap transition-opacity focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:cursor-not-allowed disabled:opacity-50",
    tones[tone],
    sizes[size],
    className,
  );
}

// 詳細頁、卡片、右欄共用的動作按鈕：可點範圍至少 44px（右欄的桌機版 38px 是滑鼠操作）。
export function ActionButton({
  tone = "secondary",
  size = "page",
  keycap,
  busy,
  className,
  disabled,
  children,
  type = "button",
  ...props
}: ActionButtonProps) {
  return (
    <button
      type={type}
      disabled={disabled || busy}
      className={actionButtonClass(tone, size, className)}
      {...props}
    >
      {busy ? <Spinner className="size-4 text-current" /> : null}
      {children}
      {keycap ? (
        <Kbd className="border-current bg-transparent text-current opacity-70">{keycap}</Kbd>
      ) : null}
    </button>
  );
}
