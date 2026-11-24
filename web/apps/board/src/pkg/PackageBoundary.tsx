import { ErrorBoundary, LoadFailed } from "@evergenie/ui";
import { type ReactNode, useEffect } from "react";
import { BundleLoadError } from "./loadBundle";

// 程式沒抓到時隔多久自己再試一次（之後每次都隔這麼久；連回網路時馬上試）。
const AUTO_RETRY_MS = 8000;

/**
 * 套件畫面出錯時的後備畫面：跟核心頁同一種白話提示「〈標題〉暫時載不進來」＋「再試一次」（UXC-8）。
 * 錯誤原文（多半是英文）只寫 console，不上畫面。程式沒抓到（斷網、服務剛重開）會自己再試；畫面自己壞掉的只給手動。
 */
export function PackageBoundary({
  title,
  children,
  fallback,
  onError,
  variant = "block",
}: {
  title: string;
  children: ReactNode;
  /** 自訂後備畫面（選單元件失敗時換回預設選單）。 */
  fallback?: (error: Error) => ReactNode;
  onError?: (error: Error) => void;
  /** 預設的後備畫面長相：整塊（頁面）或一行（卡片、分頁）。 */
  variant?: "block" | "inline";
}) {
  return (
    <ErrorBoundary
      onError={(error) => {
        console.error(`套件畫面「${title}」載入失敗`, error);
        onError?.(error);
      }}
      fallback={({ error, reset }) =>
        fallback ? (
          fallback(error)
        ) : (
          <Failed title={title} error={error} reset={reset} variant={variant} />
        )
      }
    >
      {children}
    </ErrorBoundary>
  );
}

function Failed({
  title,
  error,
  reset,
  variant,
}: {
  title: string;
  error: Error;
  reset: () => void;
  variant: "block" | "inline";
}) {
  const auto = error instanceof BundleLoadError;
  useEffect(() => {
    if (!auto) return;
    const timer = setTimeout(reset, AUTO_RETRY_MS);
    window.addEventListener("online", reset);
    return () => {
      clearTimeout(timer);
      window.removeEventListener("online", reset);
    };
  }, [auto, reset]);
  return <LoadFailed what={title} onRetry={reset} auto={auto} variant={variant} />;
}
