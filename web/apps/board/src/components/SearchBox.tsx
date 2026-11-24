import { Search, X } from "lucide-react";
import { t } from "../i18n/zh-TW";

// 清單上方的搜尋框（DEV-107）：輸入就篩，叉叉清掉；高 44 好點。
export function SearchBox({
  value,
  onChange,
  placeholder,
  label,
}: {
  value: string;
  onChange: (next: string) => void;
  placeholder: string;
  label: string;
}) {
  return (
    <div className="relative">
      <Search
        className="pointer-events-none absolute top-1/2 left-3.5 size-[18px] -translate-y-1/2 text-muted"
        aria-hidden="true"
      />
      <input
        type="search"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        aria-label={label}
        enterKeyHint="search"
        className="min-h-11 w-full rounded-xl border border-border-strong bg-surface pr-11 pl-10 text-base text-fg placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus [&::-webkit-search-cancel-button]:appearance-none"
      />
      {value ? (
        <button
          type="button"
          aria-label={t("search.clear")}
          onClick={() => onChange("")}
          className="absolute top-1/2 right-0 flex size-11 -translate-y-1/2 items-center justify-center text-muted focus-visible:outline-2 focus-visible:outline-focus"
        >
          <X className="size-4" aria-hidden="true" />
        </button>
      ) : null}
    </div>
  );
}
