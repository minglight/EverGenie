// 看板的標誌：深紫底上四顆彩色圓點（沿用舊看板的 favicon）。顏色是 token 的品牌色（--brand-*），亮暗同值，跟 favicon 同一張。
export function GenieMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={className} aria-hidden="true">
      <rect width="32" height="32" rx="8" fill="var(--brand-ink)" />
      <circle cx="11" cy="11" r="5" fill="var(--brand-1)" />
      <circle cx="21" cy="11" r="5" fill="var(--brand-2)" />
      <circle cx="11" cy="21" r="5" fill="var(--brand-3)" />
      <circle cx="21" cy="21" r="5" fill="var(--brand-4)" />
    </svg>
  );
}
