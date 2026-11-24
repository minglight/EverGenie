interface Item {
  id?: string;
  title?: string;
  route?: string;
}

// 好的自訂選單元件：取代整個導覽區，桌機是左欄、手機是底部列。
export default function Menu({
  pool,
  navigate,
  current,
  layout,
}: {
  pool: Item[];
  navigate: (route: string) => void;
  current: string;
  layout: "desktop" | "mobile";
}) {
  const desktop = layout === "desktop";
  return (
    <nav
      aria-label="自訂選單"
      data-testid="custom-menu"
      data-layout={layout}
      className={
        desktop
          ? "flex w-56 shrink-0 flex-col gap-1 border-r border-border bg-side p-3"
          : "flex shrink-0 gap-1 overflow-x-auto border-t border-border bg-surface p-1.5"
      }
    >
      {pool.map((it) => (
        <button
          key={it.id}
          type="button"
          onClick={() => navigate(it.route ?? "/")}
          aria-current={current === it.route ? "page" : undefined}
          className="min-h-11 shrink-0 rounded-lg px-3 text-left text-sm font-medium text-fg hover:bg-hover"
        >
          {it.title}
        </button>
      ))}
    </nav>
  );
}
