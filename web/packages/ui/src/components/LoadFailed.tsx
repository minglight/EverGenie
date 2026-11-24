import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import { cn } from "../lib/cn";
import { Button } from "./Button";

export interface LoadFailedProps {
  /** 什麼東西載不進來（「待辦」「今天的習慣」），接在「暫時載不進來」前面。 */
  what: string;
  onRetry: () => void;
  /** 會不會自己再試（頁面有輪詢、外殼會重連）：說明跟著換，不寫做不到的事。 */
  auto?: boolean;
  /** block：整塊置中（頁面主體，預設）；inline：一行字加按鈕（卡片裡、舊資料上方）。 */
  variant?: "block" | "inline";
  className?: string;
}

interface Member {
  what: string;
  retry: () => void;
}

interface Group {
  set: (id: string, m: Member | null) => void;
}

const GroupContext = createContext<Group | null>(null);

const TITLE = "暫時載不進來";
const HINT_AUTO = "稍後會自動重試，也可以手動再試一次。";
const HINT_MANUAL = "按「再試一次」重新讀；還是不行，等一下再回來看。";
const RETRY = "再試一次";

/**
 * 資料或畫面載不進來時的白話提示（UXC-8）：「〈什麼〉暫時載不進來」＋「再試一次」。看板核心頁與套件頁面共用這一個，
 * 不在畫面上放錯誤原文（原文寫 console）。放在 `LoadFailedGroup` 裡面時自己不畫，交給那一組合成一條。
 */
export function LoadFailed({
  what,
  onRetry,
  auto = false,
  variant = "block",
  className,
}: LoadFailedProps) {
  const group = useContext(GroupContext);
  const id = useId();
  const retry = useRef(onRetry);
  retry.current = onRetry;
  useEffect(() => {
    if (!group) return;
    group.set(id, { what, retry: () => retry.current() });
    return () => group.set(id, null);
  }, [group, id, what]);
  if (group) return null;
  if (variant === "inline") {
    return (
      <div
        role="alert"
        data-testid="load-failed"
        className={cn("flex flex-wrap items-center gap-x-3 gap-y-1 text-sm", className)}
      >
        <span className="min-w-0">
          {what}
          {TITLE}
        </span>
        <Button className="px-3 text-sm" onClick={onRetry}>
          {RETRY}
        </Button>
      </div>
    );
  }
  return (
    <div
      role="alert"
      data-testid="load-failed"
      className={cn("flex flex-col items-center gap-2 px-6 py-16 text-center", className)}
    >
      <p className="text-[15px] font-semibold">
        {what}
        {TITLE}
      </p>
      <p className="text-[13px] text-muted">{auto ? HINT_AUTO : HINT_MANUAL}</p>
      <Button variant="primary" className="mt-2" onClick={onRetry}>
        {RETRY}
      </Button>
    </div>
  );
}

/**
 * 一組裡面好幾個 `LoadFailed` 同時出現時合成一條（今天頁的套件卡片：四張都載不進來不要佔滿第一屏）：
 * 「〈甲〉、〈乙〉暫時載不進來　再試一次」，按了每一個都重試。沒有失敗的就什麼都不多畫。
 */
export function LoadFailedGroup({
  children,
  className,
}: {
  children: ReactNode;
  /** 合成的那一條的 class（例：格狀版面裡佔滿一列）。 */
  className?: string;
}) {
  const [members, setMembers] = useState<ReadonlyMap<string, Member>>(new Map());
  const set = useCallback((id: string, m: Member | null) => {
    setMembers((old) => {
      const next = new Map(old);
      if (m) next.set(id, m);
      else next.delete(id);
      return next;
    });
  }, []);
  const group = useMemo(() => ({ set }), [set]);
  const list = [...members.values()];
  return (
    <GroupContext.Provider value={group}>
      {list.length > 0 ? (
        <div
          role="alert"
          data-testid="load-failed-group"
          className={cn(
            "flex flex-wrap items-center gap-x-3 gap-y-1 rounded-[18px] border border-border bg-surface px-4 py-2 text-sm",
            className,
          )}
        >
          <span className="min-w-0">
            {list.map((m) => m.what).join("、")}
            {TITLE}
          </span>
          <Button
            className="px-3 text-sm"
            onClick={() => {
              for (const m of list) m.retry();
            }}
          >
            {RETRY}
          </Button>
        </div>
      ) : null}
      {children}
    </GroupContext.Provider>
  );
}
