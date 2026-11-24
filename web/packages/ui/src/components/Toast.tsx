import { Toaster, toast } from "sonner";

export interface ToastHostProps {
  /** 桌機（≥ 768px）：左下、離側欄 20px；手機：底部列上方 12px。 */
  desktop: boolean;
  /** 手機底部列的高度（含安全區）；toast 在它上方 12px。 */
  barHeight?: number;
  /** 桌機側欄寬度。 */
  sidebarWidth?: number;
}

// sonner 包一層：顏色、圓角、高度照 token；右邊的「復原」用 --toast-action。
export function ToastHost({ desktop, barHeight = 82, sidebarWidth = 240 }: ToastHostProps) {
  return (
    <Toaster
      position={desktop ? "bottom-left" : "bottom-center"}
      offset={desktop ? { left: sidebarWidth + 20, bottom: 20 } : { bottom: barHeight + 12 }}
      mobileOffset={{ bottom: barHeight + 12, left: 12, right: 12, top: 12 }}
      toastOptions={{
        unstyled: true,
        classNames: {
          // 收成一疊時後面那幾則只是透明、還點得到：不在最前面的一律不接點擊（WEB-114），免得點穿最前面那則按到藏著的「復原」。
          toast:
            "[&[data-front=false][data-expanded=false]]:pointer-events-none flex w-full items-center gap-3 bg-toast-bg text-toast-fg text-sm pl-4 pr-1.5 min-h-[52px] rounded-2xl shadow-[0_8px_24px_rgb(0_0_0/0.18)] md:min-h-[46px] md:w-auto md:rounded-xl",
          title: "flex-1 min-w-0",
          actionButton:
            "h-11 px-3 bg-transparent text-toast-action font-semibold text-sm md:h-[34px] md:rounded-lg",
        },
      }}
    />
  );
}

export interface ShowToastOptions {
  /** 帶「復原」按鈕（FEN-21，5 秒內有效）。 */
  undo?: { label: string; onClick: () => void };
  durationMs?: number;
  /** 放在上面（手機的聊天頁：底部是輸入框與「/」選單，放下面會蓋住它們，WEB-114）。 */
  top?: boolean;
}

export function dismissToast(id: string | number) {
  toast.dismiss(id);
}

export function showToast(message: string, options: ShowToastOptions = {}) {
  return toast(message, {
    duration: options.durationMs ?? 5000,
    action: options.undo ? { label: options.undo.label, onClick: options.undo.onClick } : undefined,
    position: options.top ? "top-center" : undefined,
    // 沒有按鈕的 toast 只是告訴你一聲：不接滑鼠與觸控（WEB-114）。滑鼠停在上面不會讓它一直不消失（sonner 停在上面就暫停計時），
    // 手機上點得到被它蓋住的東西。有「復原」的照常可以按。
    className: options.undo ? undefined : "pointer-events-none",
  });
}
