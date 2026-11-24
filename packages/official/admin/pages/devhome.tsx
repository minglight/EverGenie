import { Spinner } from "@evergenie/ui";
import { Card, LoadError, RefreshButton, Row, useLoad } from "./lib/parts";
import { type DevHome, fill, type HomeState, homeDetail, T, whenText } from "./lib/release";
import { loadDevHome } from "./lib/store";

const POLL_MS = 15_000;

const STATE_LABEL: Record<HomeState, string> = {
  running: T.stateRunning,
  stopped: T.stateStopped,
  unset: T.stateUnset,
};

// 開發家狀態（同一台的開發家）：開了沒、版本、看板 port；沒設定就說要填哪個欄位。
// 只讀 admin 程式寫的 admin.devhome；開發用帳號就是 admin 秘書的帳號，在一般的秘書設定改（不另做 API）。
export default function DevHomePage() {
  const home = useLoad(loadDevHome, POLL_MS);
  const h = home.data;
  return (
    <div data-testid="admin-devhome" className="flex flex-col gap-4">
      {/* 頁名由外殼的頁首寫（套件選單的 title），這裡不再放一個同名的 h1。 */}
      <header className="flex flex-col gap-1">
        <p className="text-sm text-muted">{T.homeSub}</p>
      </header>
      <LoadError error={home.error} />
      {h === undefined && !home.error ? (
        <div className="flex justify-center py-8">
          <Spinner label={T.loading} />
        </div>
      ) : h === null ? (
        <p className="text-sm text-muted">{T.notYet}</p>
      ) : h ? (
        <HomeCard h={h} />
      ) : null}
      <p className="text-[13px] text-muted">{T.homeAccount}</p>
      <RefreshButton target="devhome" onDone={home.reload} />
    </div>
  );
}

function HomeCard({ h }: { h: DevHome }) {
  if (h.state === "unset") {
    return (
      <Card title={T.homeCard} testId="admin-devhome-unset">
        <p className="text-sm">{T.stateUnset}</p>
        <p className="text-[13px] text-muted">{T.homeUnsetHint}</p>
        <pre className="overflow-x-auto rounded-lg bg-bg p-2 text-[13px]">{T.homeUnsetCode}</pre>
      </Card>
    );
  }
  return (
    <Card title={T.homeCard} testId="admin-devhome-card">
      <Row label={T.homeState}>
        <span data-testid="admin-devhome-state">{STATE_LABEL[h.state]}</span>
      </Row>
      {h.path ? <Row label={T.homePath}>{h.path}</Row> : null}
      {h.state === "running" ? (
        <>
          <Row label={T.homeVersion}>{h.version || T.homeVersionUnknown}</Row>
          <Row label={T.homePort}>{h.boardPort > 0 ? String(h.boardPort) : T.homePortUnknown}</Row>
          {h.url ? (
            <Row label={T.homeUrl}>
              {/* hg-allow-new-tab: 另一個家的看板（別的網站） */}
              <a href={h.url} target="_blank" rel="noreferrer" className="text-accent underline">
                {h.url}
              </a>
            </Row>
          ) : null}
        </>
      ) : null}
      {homeDetail(h) ? <Row label={T.homeDetail}>{homeDetail(h)}</Row> : null}
      {h.state === "stopped" ? <p className="text-[13px] text-muted">{T.homeStoppedHint}</p> : null}
      {h.computedAt ? (
        <p className="text-xs text-muted">{fill(T.computedAt, { at: whenText(h.computedAt) })}</p>
      ) : null}
    </Card>
  );
}
