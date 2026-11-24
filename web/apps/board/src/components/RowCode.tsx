import { cn } from "@evergenie/ui";
import { useShowCodes } from "../data/queries";

// 清單列標題前面的編號（DEV-156 U-22，UXC-8）：簡單模式（預設）不露，完整模式才露；詳情頁、搜尋照舊顯示編號。
// sep 是編號跟標題中間的分隔（預設「 · 」）。
export function RowCode({
  id,
  className,
  sep = " · ",
}: {
  id?: string;
  className?: string;
  sep?: string;
}) {
  const show = useShowCodes();
  if (!show || !id) return null;
  return (
    <>
      <span className={cn("font-mono text-xs font-medium text-muted", className)}>{id}</span>
      {sep}
    </>
  );
}
