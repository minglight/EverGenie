import { EmptyState, LoadFailed, Markdown, Spinner } from "@evergenie/ui";
import { useLoad } from "./lib/parts";
import { loadNotes } from "./lib/store";

// 訓練計畫：被釘選的那篇筆記在最上面，其他筆記依日期新到舊；唯讀，筆記由教練寫（REQ-127）。
export default function Plan() {
  const notes = useLoad(loadNotes);
  const all = notes.data ?? [];
  const pinned = all.filter((n) => n.pinned).sort((a, b) => (a.date < b.date ? 1 : -1));
  const others = all
    .filter((n) => !n.pinned)
    .sort((a, b) => (a.date < b.date ? 1 : a.date > b.date ? -1 : b.id.localeCompare(a.id)));

  return (
    <section data-testid="fitness-plan" className="flex flex-col gap-4">
      {notes.error ? (
        <LoadFailed
          what="訓練計畫"
          onRetry={notes.reload}
          variant={notes.data === undefined ? "block" : "inline"}
          className={notes.data === undefined ? "py-8" : undefined}
        />
      ) : null}
      {notes.data === undefined && !notes.error ? (
        <div className="flex justify-center py-8">
          <Spinner label="載入中" />
        </div>
      ) : notes.data && all.length === 0 ? (
        <EmptyState title="還沒有訓練計畫" hint="請教練寫一份，寫好會釘在這裡" />
      ) : (
        <>
          {pinned.map((n) => (
            <article
              key={n.id}
              data-testid="plan-pinned"
              className="flex flex-col gap-2 rounded-[18px] border border-border bg-surface p-4"
            >
              <h2 className="text-lg font-semibold">{n.title}</h2>
              {n.date ? <p className="text-xs text-muted">{n.date}</p> : null}
              <Markdown>{n.body}</Markdown>
            </article>
          ))}
          {others.length > 0 ? (
            <div className="flex flex-col gap-2">
              <h2 className="font-semibold">其他筆記</h2>
              <ul data-testid="plan-notes" className="flex flex-col gap-2">
                {others.map((n) => (
                  <li
                    key={n.id}
                    data-testid="plan-note"
                    className="rounded-[18px] border border-border bg-surface p-4"
                  >
                    <details>
                      <summary className="flex min-h-11 cursor-pointer flex-wrap items-baseline justify-between gap-2">
                        <span className="font-medium">{n.title}</span>
                        <span className="text-xs text-muted">{n.date}</span>
                      </summary>
                      <div className="pt-2">
                        <Markdown>{n.body}</Markdown>
                      </div>
                    </details>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </>
      )}
    </section>
  );
}
