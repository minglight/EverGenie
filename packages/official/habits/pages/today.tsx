import { EmptyLine, LoadFailed, Spinner } from "@evergenie/ui";
import { type ReactNode, useEffect, useReducer, useState } from "react";
import { dateStr, pickOneThing, soleAgent, todayCardState } from "./lib/habits";
import { NEW_HABIT_DRAFT, NewHabit, NewHabitSheet, seedDraft, useLoad } from "./lib/parts";
import {
  checkin,
  loadAgentNames,
  loadHabits,
  loadStatuses,
  loadWallet,
  reasonOf,
} from "./lib/store";

// 「今天不做」記在這台的瀏覽器（值是那天的日期）：只是把卡片收起來到明天，不記任何紀錄；
// 沒做的那天照規則過完自動算跳過或斷了，跟沒按一樣。
const OFF_KEY = "eg.habits.today.off";

function readOff(): string {
  try {
    return window.localStorage.getItem(OFF_KEY) ?? "";
  } catch {
    return "";
  }
}

function saveOff(today: string) {
  try {
    window.localStorage.setItem(OFF_KEY, today);
  } catch {
    // 存不了就只收這一次畫面
  }
}

// 今天頁的卡片（slot core.overview.cards）：標題固定「今天的習慣」；今天只有一件事（HAB-15），一鍵做完；不做也能收起來，其他不催。
// 還沒有習慣時宣告「現在是空的」（EmptyLine）：今天頁的手機版縮成一行「今天的習慣：還沒約　約一個」，其他地方照舊整張卡。
export default function Today() {
  const habits = useLoad(loadHabits);
  const statuses = useLoad(loadStatuses);
  const wallet = useLoad(loadWallet);
  // 錢包讀不到只是不顯示點數；習慣或打卡狀態讀不到就說出來、給「再試一次」，不一直轉圈（HAB-12）。
  const state = todayCardState([habits, statuses]);
  // 按過的「做了」「今天不做」都記是哪一天的：頁面開著過了午夜，昨天按的不會把今天的卡也收掉。
  const [justDone, setJustDone] = useState({ id: "", date: "" });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState("");
  // 手機從背景回來不會重新載入：回到前景重畫一次，過了午夜「今天」才會換日。
  const [, wake] = useReducer((n: number) => n + 1, 0);
  useEffect(() => {
    const onShow = () => {
      if (document.visibilityState === "visible") wake();
    };
    document.addEventListener("visibilitychange", onShow);
    return () => document.removeEventListener("visibilitychange", onShow);
  }, []);
  const now = new Date();
  const today = dateStr(now);
  const yesterday = dateStr(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1));
  const [offDate, setOffDate] = useState(readOff);
  const off = offDate === today;
  const doneNow = justDone.date === today ? justDone.id : "";
  const active = (habits.data ?? []).filter((h) => h.status === "active");
  const one = doneNow || off ? null : pickOneThing(active, statuses.data ?? {}, today, yesterday);
  const done = async (id: string) => {
    setBusy(true);
    setProblem("");
    try {
      await checkin(id, today);
      setJustDone({ id, date: today });
      statuses.reload();
    } catch (e) {
      setProblem(reasonOf(e));
    } finally {
      setBusy(false);
    }
  };
  const cardCls =
    "flex flex-col gap-2 rounded-[18px] border border-border bg-surface p-4 text-fg no-underline";
  const head = (
    <div className="flex items-baseline justify-between gap-2">
      <h2 className="font-semibold">今天的習慣</h2>
      {wallet.data ? (
        <span data-testid="today-points" className="text-sm text-muted">
          {wallet.data.points} 點
        </span>
      ) : null}
    </div>
  );
  if (state === "error") {
    // 讀不到：整張卡換成一行白話提示＋「再試一次」（今天頁會把好幾張合成一條）；按了先回到載入中再重讀（HAB-12）。
    return (
      <LoadFailed
        what="今天的習慣"
        onRetry={() => {
          habits.reload();
          statuses.reload();
          wallet.reload();
        }}
        variant="inline"
        className="rounded-[18px] border border-border bg-surface p-4"
      />
    );
  }
  const all = (
    <a
      href="/p/habits/list"
      data-testid="today-all"
      // 字只有一行高，可點範圍用 ::after 補到 44（FEN-7）。
      className="relative text-sm text-muted underline after:absolute after:inset-x-0 after:top-1/2 after:h-11 after:-translate-y-1/2 after:content-['']"
    >
      全部習慣
    </a>
  );
  const card = (
    <div data-testid="habits-today" className={cardCls}>
      {head}
      {state === "loading" ? (
        <Spinner label="載入中" />
      ) : active.length === 0 ? (
        <NoHabitYet outfits={wallet.data?.outfitsReady === true} />
      ) : one ? (
        <div data-testid="today-one" data-habit={one.id} className="flex flex-col gap-1.5">
          <p className="text-xs text-muted">今天只有一件事</p>
          <p className="text-base font-semibold">{one.name}</p>
          {one.twoMin ? <p className="text-sm">兩分鐘版本：{one.twoMin}</p> : null}
          {one.cue ? <p className="text-xs text-muted">{one.cue}</p> : null}
          <div className="mt-1 flex gap-2">
            <button
              type="button"
              data-testid="today-one-done"
              disabled={busy}
              className="min-h-11 flex-1 rounded-full bg-accent px-4 font-semibold text-accent-fg disabled:opacity-50"
              onClick={() => void done(one.id)}
            >
              做了
            </button>
            <button
              type="button"
              data-testid="today-one-off"
              disabled={busy}
              className="min-h-11 flex-1 rounded-full border border-border px-4 text-fg disabled:opacity-50"
              onClick={() => {
                saveOff(today);
                setOffDate(today);
              }}
            >
              今天不做
            </button>
          </div>
          <p className="text-xs text-muted">不按也沒關係，今天過完會照規則自動算。</p>
          {problem ? (
            <p role="alert" className="text-sm text-danger">
              {problem}
            </p>
          ) : null}
        </div>
      ) : off ? (
        <p data-testid="today-off" role="status" className="text-sm font-medium">
          好，今天先這樣，明天再說。
        </p>
      ) : (
        <p data-testid="today-enough" className="text-sm font-medium">
          今天這樣就夠了。
        </p>
      )}
      {active.length > 0 ? all : null}
    </div>
  );
  if (state !== "loading" && active.length === 0) return <NoHabitLine>{card}</NoHabitLine>;
  return card;
}

// 還沒有習慣的一行版：「約一個」開跟卡片上同一個「跟誰約？」抽屜；只有一位秘書就直接開她的聊天、起好頭（HAB-22）。
// 秘書名單在還沒有習慣時才讀（要知道是不是只有一位）。
function NoHabitLine({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const names = useLoad(loadAgentNames);
  const sole = soleAgent(names.data);
  return (
    <>
      <EmptyLine
        label="今天的習慣"
        text="還沒約"
        action={
          sole
            ? {
                label: "約一個",
                href: `/chat/${sole}`,
                onClick: () => seedDraft(sole, NEW_HABIT_DRAFT),
              }
            : { label: "約一個", onClick: () => setOpen(true) }
        }
        data-testid="habits-today-empty"
      >
        {children}
      </EmptyLine>
      {open ? <NewHabitSheet open onOpenChange={setOpen} names={names.data} /> : null}
    </>
  );
}

// 還沒有習慣：跟習慣頁同一顆「約一個新習慣」（選秘書、起好頭、開聊天，HAB-22）。秘書名單只在這時候才讀。
// 點數還換不到衣服時不承諾「換衣服」（錢包的 outfits_ready，HAB-23；DEV-156 U-09）；錢包讀到之前先用不承諾的那句。
function NoHabitYet({ outfits }: { outfits: boolean }) {
  const names = useLoad(loadAgentNames);
  return (
    <div data-testid="today-empty" className="flex flex-col items-start gap-2">
      <p className="text-sm text-muted">
        {outfits
          ? "還沒有約定習慣。從一件很小的事開始，做到會有點數換衣服。"
          : "還沒有約定習慣。從一件很小的事開始，做到就有點數。"}
      </p>
      <NewHabit names={names.data} />
    </div>
  );
}
