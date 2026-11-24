import { createContext, type ReactNode, useContext } from "react";
import { cn } from "../lib/cn";

const CompactContext = createContext(false);

/**
 * 宿主用：包在裡面的 `EmptyLine` 一律畫成一行。今天頁在手機把套件卡片的 slot 包起來（DEV-156 U-04），
 * 沒資料的卡片就不佔一整張的位置；沒包（桌機、別的 slot）時 `EmptyLine` 照舊畫整張卡。
 */
export function CompactEmpty({ children }: { children: ReactNode }) {
  return <CompactContext.Provider value={true}>{children}</CompactContext.Provider>;
}

/** 連結也可以帶 onClick：點下去先做（例：在聊天起好頭），再照連結換頁（DEV-157 習慣的「約一個」）。 */
export type EmptyLineAction =
  | { label: string; href: string; onClick?: () => void }
  | { label: string; onClick: () => void; href?: undefined };

export interface EmptyLineProps {
  /** 這張卡是什麼（「體重」「今天的習慣」）。 */
  label: string;
  /** 現在的狀況，一句短話（「還沒記」「還沒約」）。 */
  text: string;
  /** 下一步：一個連結（`href`）或一顆按鈕（`onClick`），例「＋記一筆」。 */
  action?: EmptyLineAction;
  /** 完整的卡片：宿主沒要求縮成一行時畫這個。 */
  children: ReactNode;
  className?: string;
  "data-testid"?: string;
}

const actionClass =
  "inline-flex min-h-11 shrink-0 items-center rounded-lg px-1 text-sm font-medium whitespace-nowrap text-accent no-underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus";

/**
 * 卡片宣告「我現在是空的」（DEV-156 U-04）：套件的卡片沒有資料時，用它把整張卡包起來，並給一行版的字與下一步。
 * 宿主要求精簡時（`CompactEmpty`，今天頁的手機版）只畫一行「〈label〉：〈text〉　〈action〉」；其他地方照舊畫 children。
 * 有資料時不要用它（直接畫卡片）。沒用它的卡片不受影響，永遠畫整張。
 */
export function EmptyLine({
  label,
  text,
  action,
  children,
  className,
  "data-testid": testId,
}: EmptyLineProps) {
  const compact = useContext(CompactContext);
  if (!compact) return <>{children}</>;
  return (
    <div
      data-testid={testId ?? "empty-line"}
      data-empty-line=""
      className={cn(
        "flex min-h-12 items-center gap-2 rounded-[14px] border border-border bg-surface py-0.5 pr-3 pl-4 text-sm text-fg",
        className,
      )}
    >
      <p className="m-0 min-w-0 flex-1">
        <span className="font-medium">{label}</span>
        <span className="text-muted">：{text}</span>
      </p>
      {action === undefined ? null : action.href !== undefined ? (
        <a href={action.href} onClick={action.onClick} className={actionClass}>
          {action.label}
        </a>
      ) : (
        <button type="button" onClick={action.onClick} className={actionClass}>
          {action.label}
        </button>
      )}
    </div>
  );
}
