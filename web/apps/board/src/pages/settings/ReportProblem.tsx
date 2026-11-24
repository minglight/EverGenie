import { api } from "@evergenie/sdk-web";
import { Segmented, showToast } from "@evergenie/ui";
import { useState } from "react";
import { ActionButton, actionButtonClass } from "../../components/ActionButton";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import { SettingGroup } from "./SettingRow";

type Level = "basic" | "detail";
interface Bundle {
  level: Level;
  title: string;
  body: string;
  link: string;
}

async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

// 設定 → 系統健康最下面的「回報問題」（DEV-153，WEB-191）：按了才產生 → 內容整段先給主人看 → 再由他決定要不要
// 開回報頁（帶好標題與內容）或複製。這裡不會自己送出任何東西；回報頁還沒設定時（issue_link 空）只給「複製內容」。
export function ReportProblem() {
  const [level, setLevel] = useState<Level>("basic");
  const [bundle, setBundle] = useState<Bundle | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const make = async (want: Level) => {
    setBusy(true);
    setError("");
    try {
      const res = await api.system.report({ detail: want === "detail" });
      setBundle({
        level: want,
        title: res.title ?? "",
        body: res.body ?? "",
        link: res.issue_link ?? "",
      });
    } catch (err) {
      // 沒拿到新的那一份：舊的（另一種詳細程度）不留在畫面上，免得選了「一般」卻複製到詳細版。
      setBundle(null);
      setError(reasonOf(err));
    } finally {
      setBusy(false);
    }
  };
  const pick = (next: Level) => {
    setLevel(next);
    if (bundle && bundle.level !== next) void make(next);
  };
  const copy = async () => {
    if (!bundle) return;
    const ok = await copyText(`${bundle.title}\n\n${bundle.body}`);
    showToast(t(ok ? "settings.report.copied" : "settings.report.copyFailed"));
  };

  return (
    <SettingGroup title={t("settings.report.title")}>
      <div data-testid="report-problem" className="flex flex-col items-start gap-3 px-4 py-3.5">
        <p className="text-[13px] leading-snug text-muted">{t("settings.report.intro")}</p>
        <Segmented
          ariaLabel={t("settings.report.level")}
          value={level}
          onChange={pick}
          wrap
          options={[
            { value: "basic", label: t("settings.report.level.basic") },
            { value: "detail", label: t("settings.report.level.detail") },
          ]}
        />
        {level === "detail" ? (
          <p className="text-[13px] leading-snug text-muted">{t("settings.report.detailHint")}</p>
        ) : null}
        {bundle === null ? (
          <ActionButton
            tone="outline"
            busy={busy}
            disabled={busy}
            data-testid="report-make"
            onClick={() => void make(level)}
          >
            {t("settings.report.make")}
          </ActionButton>
        ) : busy ? (
          // 換詳細程度、正在重新整理：舊內容先收起來，不能複製或開回報頁。
          <p data-testid="report-busy" role="status" className="text-[13px] text-muted">
            {t("settings.report.busy")}
          </p>
        ) : (
          <>
            <p className="text-[13px] leading-snug font-medium">{t("settings.report.preview")}</p>
            <pre
              data-testid="report-preview"
              // 內容長：這一塊自己捲（上下與左右），頁面不出現水平捲動（FEN-7）。
              className="max-h-[50vh] w-full overflow-auto rounded-xl border border-border bg-surface-2 p-3 text-[12px] leading-relaxed whitespace-pre-wrap break-words"
            >
              {`${bundle.title}\n\n${bundle.body}`}
            </pre>
            <div className="flex flex-wrap items-center gap-2">
              {bundle.link ? (
                // hg-allow-new-tab: 站外的回報頁（不是圖片或影片），看完回看板繼續用
                <a
                  href={bundle.link}
                  target="_blank"
                  rel="noopener noreferrer"
                  data-testid="report-open"
                  className={actionButtonClass("primary", "page")}
                >
                  {t("settings.report.open")}
                </a>
              ) : null}
              <ActionButton
                tone={bundle.link ? "outline" : "primary"}
                data-testid="report-copy"
                onClick={() => void copy()}
              >
                {t("settings.report.copy")}
              </ActionButton>
            </div>
            <p className="text-[12px] leading-snug text-muted">
              {t(bundle.link ? "settings.report.nextOpen" : "settings.report.nextCopy")}
            </p>
          </>
        )}
        {error ? (
          <p role="alert" className="text-[13px] leading-snug text-danger">
            {error}
          </p>
        ) : null}
      </div>
    </SettingGroup>
  );
}
