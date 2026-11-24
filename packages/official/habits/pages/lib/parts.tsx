// 習慣頁面共用的小零件：載入用的 hook（可以定時刷新）、本週進度條、規則說明、「約一個新習慣」。
import { onLiveChange, seedChatDraft } from "@evergenie/sdk-web";
import { Button, Sheet } from "@evergenie/ui";
import { useCallback, useEffect, useRef, useState } from "react";
import { type Habit, type RuleSection, soleAgent } from "./habits";
import { reasonOf } from "./store";

export interface Loaded<T> {
  data: T | undefined;
  error: string;
  reload: () => void;
}

/** 載一次、之後可以 reload；pollMs > 0 時定時再載一次（程式算完約 2 秒後才更新 status，所以頁面每 3 秒看一次）。 */
export function useLoad<T>(fn: () => Promise<T>, pollMs = 0): Loaded<T> {
  const [data, setData] = useState<T>();
  const [error, setError] = useState("");
  const [tick, setTick] = useState(0);
  const fnRef = useRef(fn);
  fnRef.current = fn;
  useEffect(() => {
    let live = true;
    const run = () =>
      fnRef
        .current()
        .then((d) => {
          if (!live) return;
          setError("");
          setData(d);
        })
        .catch((e: unknown) => {
          if (!live) return;
          console.warn("讀取失敗", e);
          setError(reasonOf(e));
        });
    void run();
    const timer = pollMs > 0 ? setInterval(run, pollMs) : undefined;
    return () => {
      live = false;
      if (timer) clearInterval(timer);
    };
  }, [tick, pollMs]);
  // 重讀時先清掉錯誤：畫面回到「載入中」，再失敗才又顯示錯誤，不會按了「再試一次」像沒反應（HAB-12）。
  const reload = useCallback(() => {
    setError("");
    setTick((n) => n + 1);
  }, []);
  // 套件的程式算好進度、秘書幫你打了卡：今天頁的卡片（沒有輪詢）也跟著重讀（PKG-49）。
  // 部署前就開著的舊看板外殼沒有 onLiveChange（共用模組是外殼給的）：沒有就不接，頁面照常能用（PKG-49）。
  useEffect(
    () => (typeof onLiveChange === "function" ? onLiveChange("habits.", reload) : undefined),
    [reload],
  );
  return { data, error, reload };
}

/** 本週進度條：done / need。 */
export function WeekBar({ done, need, testId }: { done: number; need: number; testId?: string }) {
  const pct = need > 0 ? Math.min(100, Math.round((done / need) * 100)) : 0;
  return (
    <div
      role="progressbar"
      aria-label="本週進度"
      aria-valuemin={0}
      aria-valuemax={need}
      aria-valuenow={done}
      data-testid={testId}
      className="h-2.5 w-full overflow-hidden rounded-full bg-surface-2"
    >
      <div className="h-full rounded-full bg-accent" style={{ width: `${pct}%` }} />
    </div>
  );
}

/** 規則說明的三段（怎樣算完成／怎樣拿點／現在進度），每一欄標題跟內容分開（DEV-142）。 */
export function RuleList({ sections }: { sections: RuleSection[] }) {
  return (
    <div className="flex flex-col gap-4">
      {sections.map((s) => (
        <section key={s.id} data-testid={`rules-${s.id}`} className="flex flex-col gap-2">
          <h3 className="text-base font-bold">{s.title}</h3>
          {/* 小標題與內容要一眼分得開：小標題主色、粗、帶左邊色條；內容一般字重、行距鬆一點，每項之間細線隔開。 */}
          <dl className="flex flex-col divide-y divide-border rounded-xl border border-border bg-surface px-3">
            {s.rows.map(([label, value]) => (
              <div key={label} className="flex flex-col gap-1 py-2.5">
                <dt className="border-l-2 border-accent pl-2 text-xs font-bold tracking-wide text-accent">
                  {label}
                </dt>
                <dd className="pl-2.5 text-sm leading-relaxed text-fg">{value}</dd>
              </div>
            ))}
          </dl>
        </section>
      ))}
    </div>
  );
}

/** 幫主人在聊天起個頭；舊外殼（部署前就開著的分頁）沒有這個函式就跳過，聊天照樣打得開（PKG-49）。 */
export function seedDraft(session: string, text: string): void {
  if (typeof seedChatDraft === "function") seedChatDraft(session, text);
}

/** 跟約這個習慣的秘書聊怎麼改：打開她的聊天、先起好頭（DEV-142）。 */
export function DiscussLink({ habit }: { habit: Pick<Habit, "id" | "name" | "agent"> }) {
  return (
    <a
      href={`/chat/${habit.agent}`}
      data-testid="habit-discuss"
      onClick={() => seedDraft(habit.agent, `我想調整習慣「${habit.name}」（${habit.id}）：`)}
      className="inline-flex min-h-11 items-center justify-center rounded-full border border-border px-4 text-sm text-fg no-underline outline-none focus-visible:outline-2 focus-visible:outline-focus"
    >
      跟秘書討論修改
    </a>
  );
}

/** 卡片上的「怎麼算」按鈕（DEV-156 U-14，以前叫「規則」）：點了從底部跳出完整規則，最上面可以直接找秘書討論修改。 */
export function RuleButton({
  habit,
  sections,
}: {
  habit: Pick<Habit, "id" | "name" | "agent">;
  sections: RuleSection[];
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        data-testid="habit-rules"
        onClick={() => setOpen(true)}
        className="min-h-11 shrink-0 rounded-full border border-border px-3 text-sm text-fg"
      >
        怎麼算
      </button>
      <Sheet open={open} onOpenChange={setOpen} title={`${habit.name}怎麼算`}>
        <div className="flex max-h-[70vh] flex-col gap-3 overflow-y-auto px-4 pb-2">
          {/* 討論鈕放最上面：抽屜打開時焦點會跳到第一個能按的東西，放下面會整個捲到底。 */}
          <DiscussLink habit={habit} />
          <RuleList sections={sections} />
        </div>
      </Sheet>
    </>
  );
}

export const NEW_HABIT_DRAFT = "我想約一個新習慣：";

/**
 * 約新習慣：先選跟哪位秘書約，幫主人在那串聊天起個頭再打開（DEV-142）；聊出提示、兩分鐘版本後由秘書寫進來。
 * 只有一位秘書就不問，按了直接開她的聊天、起好頭（HAB-22，DEV-157）。
 * 習慣頁與今天頁（還沒有習慣時）共用這一個（HAB-22）。names 是職位 id → 秘書名字（loadAgentNames）。
 */
export function NewHabit({ names }: { names: Record<string, string> | undefined }) {
  const [open, setOpen] = useState(false);
  const sole = soleAgent(names);
  if (sole) {
    return (
      <a
        href={`/chat/${sole}`}
        data-testid="habit-new"
        data-agent={sole}
        onClick={() => seedDraft(sole, NEW_HABIT_DRAFT)}
        className="inline-flex min-h-11 items-center justify-center self-start rounded-full bg-accent px-5 font-semibold text-accent-fg no-underline outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
      >
        約一個新習慣
      </a>
    );
  }
  return (
    <>
      <Button data-testid="habit-new" variant="primary" onClick={() => setOpen(true)}>
        約一個新習慣
      </Button>
      <NewHabitSheet open={open} onOpenChange={setOpen} names={names} />
    </>
  );
}

/** 「跟誰約？」的抽屜：選一位秘書，開那位的聊天並起好頭。按鈕以外的入口（今天頁一行版的「約一個」）直接用這個。 */
export function NewHabitSheet({
  open,
  onOpenChange,
  names,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  names: Record<string, string> | undefined;
}) {
  const agents = Object.entries(names ?? {});
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title="跟誰約？">
      <div className="flex flex-col gap-2 px-4 pb-2">
        <p className="text-sm text-muted">
          選一位秘書，聊天會打開並幫你起好頭，說你想養成什麼就好。
        </p>
        {names === undefined ? (
          <p className="text-sm text-muted">載入中…</p>
        ) : agents.length === 0 ? (
          <p className="text-sm text-muted">
            還沒有秘書。
            <a href="/settings/agents" className="text-accent underline">
              先到設定加一位
            </a>
            ，再回來約。
          </p>
        ) : (
          <ul className="flex flex-col gap-2">
            {agents.map(([id, name]) => (
              <li key={id}>
                <a
                  href={`/chat/${id}`}
                  data-testid="habit-new-agent"
                  data-agent={id}
                  onClick={() => seedDraft(id, NEW_HABIT_DRAFT)}
                  className="flex min-h-11 items-center rounded-xl border border-border px-4 text-fg no-underline"
                >
                  {name}
                </a>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Sheet>
  );
}
