import { Button } from "@evergenie/ui";
import { useState } from "react";
import { TASK_POINTS, taskLeft, todoHowLines, todoNextLine, todoProgress } from "./lib/habits";
import { useLoad } from "./lib/parts";
import { exchangeTaskPoints, loadWallet, reasonOf } from "./lib/store";

// 待辦頁最上面（slot core.reminders.top）：做完會累積的待辦攢到 30（TASK_POINTS）換 1 點（HAB-18、DEV-149）。
// 畫面上只有一種「點數」（DEV-156 U-14）：這裡是一條進度條加一句下一步，完整規則收在「怎麼算」。
export default function TodoPoints() {
  const wallet = useLoad(loadWallet, 3000);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  const w = wallet.data;
  if (!w) return null; // 讀不到只是不顯示，不擋待辦頁
  const { canExchange } = taskLeft(w);
  const progress = todoProgress(w);
  const exchange = async () => {
    setBusy(true);
    setError("");
    try {
      await exchangeTaskPoints();
      setNote("換好了：多 1 點。");
      for (const ms of [2500, 5000]) setTimeout(wallet.reload, ms);
      setTimeout(() => setNote(""), 6000);
    } catch (e) {
      setError(reasonOf(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div
      data-testid="todo-points"
      className="flex flex-col gap-2 rounded-[18px] border border-border bg-surface px-4 py-3"
    >
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="text-sm font-semibold">做待辦也能拿點數</span>
          <span className="text-sm text-muted" data-testid="todo-points-next">
            {note || error || todoNextLine(w)}
          </span>
        </div>
        {canExchange ? (
          <Button
            data-testid="todo-points-exchange"
            disabled={busy}
            onClick={() => void exchange()}
          >
            換成 1 點
          </Button>
        ) : null}
      </div>
      <div
        role="progressbar"
        aria-label="累積到下一點"
        aria-valuemin={0}
        aria-valuemax={TASK_POINTS}
        aria-valuenow={progress}
        aria-valuetext={`${progress} / ${TASK_POINTS}`}
        data-testid="todo-points-progress"
        className="h-2.5 w-full overflow-hidden rounded-full bg-surface-2"
      >
        <div
          className="h-full rounded-full bg-accent"
          style={{ width: `${Math.round((progress / TASK_POINTS) * 100)}%` }}
        />
      </div>
      <details className="text-sm" data-testid="todo-points-how">
        <summary className="flex min-h-11 cursor-pointer items-center text-muted">怎麼算</summary>
        <ul className="flex list-disc flex-col gap-1 pb-1 pl-5 text-muted">
          {todoHowLines(w.outfitsReady).map((line) => (
            <li key={line}>{line}</li>
          ))}
        </ul>
      </details>
    </div>
  );
}
