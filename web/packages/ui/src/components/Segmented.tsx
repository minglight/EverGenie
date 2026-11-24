import { useEffect, useRef, useState } from "react";
import { cn } from "../lib/cn";

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
  /** 選填：這一格有幾筆，接在名字後面；0 的數字用灰字、不加粗（還是點得進去，點了顯示空狀態）。 */
  count?: number;
}

export interface SegmentedProps<T extends string> {
  options: SegmentedOption<T>[];
  value: T;
  onChange: (value: T) => void;
  ariaLabel: string;
  /** 桌機的緊湊版（按鈕高 30）；不給就是高 44（觸控目標，UXC-5，DEV-156 U-11）。 */
  compact?: boolean;
  /** 選項字比較長：放不下就換行（手機上的跟進方式）。 */
  wrap?: boolean;
  /** 選項超過 5 個：橫向捲動，每個選項不縮小（例如規則頁的文件清單）。 */
  scroll?: boolean;
  className?: string;
}

// 少量選項（≤ 5）用分段按鈕，不用下拉（FEN-17）。
export function Segmented<T extends string>({
  options,
  value,
  onChange,
  ariaLabel,
  compact,
  wrap,
  scroll,
  className,
}: SegmentedProps<T>) {
  const ref = useRef<HTMLFieldSetElement>(null);
  // 橫向捲動時（例如手機上規則頁的文件清單）：選到的那格一定捲進畫面，右邊還有就淡出提示（WEB-113）。
  const [more, setMore] = useState(false);
  // 數字（count）通常晚一點才讀到、按鈕會變寬：選項的字變了也要再確認選到的那格看得到（DEV-107）。
  // 使用者自己滑過之後就不再拉回（有單異動、數字變了，不能把他正在看的分頁列跳走）；換選到的那格才重新算。
  const sig = options.map((o) => `${o.label}${o.count ?? ""}`).join("|");
  const touched = useRef(false);
  // biome-ignore lint/correctness/useExhaustiveDependencies: value 變了要重設「自己滑過」
  useEffect(() => {
    touched.current = false;
  }, [value]);
  // biome-ignore lint/correctness/useExhaustiveDependencies: 換了選項（value、sig）要重新捲到選到的那格
  useEffect(() => {
    const el = ref.current;
    if (!scroll || !el) return;
    const on = el.querySelector<HTMLElement>('[aria-pressed="true"]');
    if (on && !touched.current) {
      const start = on.offsetLeft;
      const end = start + on.offsetWidth;
      if (start < el.scrollLeft) el.scrollLeft = Math.max(0, start - 8);
      else if (end > el.scrollLeft + el.clientWidth) el.scrollLeft = end - el.clientWidth + 8;
    }
    const update = () => setMore(el.scrollLeft + el.clientWidth < el.scrollWidth - 2);
    update();
    const mine = () => {
      touched.current = true;
    };
    el.addEventListener("scroll", update, { passive: true });
    el.addEventListener("touchstart", mine, { passive: true });
    el.addEventListener("wheel", mine, { passive: true });
    window.addEventListener("resize", update);
    return () => {
      el.removeEventListener("touchstart", mine);
      el.removeEventListener("wheel", mine);
      el.removeEventListener("scroll", update);
      window.removeEventListener("resize", update);
    };
  }, [scroll, value, sig]);
  return (
    <fieldset
      ref={ref}
      data-more={scroll && more ? "" : undefined}
      className={cn(
        "m-0 flex min-w-0 gap-0.5 rounded-[10px] border-0 bg-surface-2 p-0.5",
        wrap && "flex-wrap",
        scroll && "hg-scroll relative overflow-x-auto",
        scroll && more && "[mask-image:linear-gradient(to_right,#000_calc(100%-40px),transparent)]",
        className,
      )}
    >
      <legend className="sr-only">{ariaLabel}</legend>
      {options.map((o) => {
        const on = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(o.value)}
            className={cn(
              "flex-1 rounded-lg px-2 text-[13px] font-medium whitespace-nowrap transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
              compact ? "h-[30px]" : "h-11",
              scroll && "flex-none px-3.5",
              on ? "bg-segment-on text-fg shadow-[0_1px_3px_rgb(0_0_0/0.12)]" : "text-muted",
            )}
          >
            {o.label}
            {o.count === undefined ? null : (
              <span
                className={cn(
                  "ml-1.5 text-[12px] tabular-nums",
                  // 0 用正常的灰字，不用半透明（半透明在淺色只有 2.06:1，DEV-156 U-26）。
                  o.count === 0 ? "font-normal text-muted" : "font-semibold",
                )}
              >
                {o.count}
              </span>
            )}
          </button>
        );
      })}
    </fieldset>
  );
}
