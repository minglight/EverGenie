import { EmptyLine, LoadFailed, Spinner } from "@evergenie/ui";
import { latestDateOf, measuredLabel, summarize } from "./lib/body";
import { dateStr } from "./lib/dates";
import { loadBody, useLoad } from "./lib/store";

// 今天頁的卡片（slot core.overview.cards）：最近體重與 7 天平均；點了去「身體」（REQ-114、129）。
// 最近一筆不是今天量的就標日期（BOD-5）。
// 一筆體重都還沒有：宣告「現在是空的」（EmptyLine），今天頁的手機版縮成一行「體重：還沒記　＋記一筆」，其他地方照舊整張卡。
export default function Today() {
  const body = useLoad(loadBody);
  const today = dateStr(new Date());
  const items = body.data ?? [];
  const sum = summarize(items, today);
  const when = measuredLabel(latestDateOf(items, "weight") ?? today, today);
  // 讀不到：整張卡換成一行白話提示＋「再試一次」（今天頁會把好幾張合成一條），不放連結（按鈕不能包在連結裡）。
  if (body.error && body.data === undefined) {
    return (
      <LoadFailed
        what="今天的身體"
        onRetry={body.reload}
        variant="inline"
        className="rounded-[18px] border border-border bg-surface p-4"
      />
    );
  }
  const card = (
    <a
      href="/p/body/log"
      data-testid="body-today"
      className="flex flex-col gap-2 rounded-[18px] border border-border bg-surface p-4 text-fg no-underline"
    >
      <h2 className="font-semibold">今天的身體</h2>
      {body.data === undefined ? (
        <Spinner label="載入中" />
      ) : sum.weight.latest !== null ? (
        <p data-testid="today-weight" className="text-sm">
          體重 <span className="font-semibold">{sum.weight.latest}</span> kg
          {when ? (
            <span data-testid="today-weight-when" className="text-muted">
              {" "}
              · {when}量的
            </span>
          ) : null}
          {sum.weight.avg7 !== null ? (
            <span className="text-muted">（7 天平均 {sum.weight.avg7}）</span>
          ) : null}
        </p>
      ) : (
        <p className="text-sm text-muted">還沒有體重記錄。</p>
      )}
    </a>
  );
  if (body.data !== undefined && sum.weight.latest === null) {
    return (
      <EmptyLine
        label="體重"
        text="還沒記"
        action={{ label: "＋記一筆", href: "/p/body/log" }}
        data-testid="body-today-empty"
      >
        {card}
      </EmptyLine>
    );
  }
  return card;
}
