import { api, type PackagePreviewResponse } from "@evergenie/sdk-web";
import { showToast } from "@evergenie/ui";
import { useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { invalidatePackageViews } from "../../data/queries";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";

const inputClass =
  "w-full rounded-xl border border-border-strong bg-bg px-3 py-2.5 text-base leading-snug text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus";

/** 預覽卡裡「會帶來什麼」的幾行；沒有內容的那一行不出現。 */
function bringsLines(p: PackagePreviewResponse): string[] {
  const rows: [Parameters<typeof t>[0], string[] | undefined][] = [
    ["settings.packages.url.agents", p.agents],
    ["settings.packages.url.pages", p.pages],
    ["settings.packages.url.jobs", p.jobs],
    ["settings.packages.url.dataKinds", p.data_kinds],
    ["settings.packages.url.reads", p.reads],
    ["settings.packages.url.writes", p.writes],
  ];
  return rows
    .filter(([, list]) => (list?.length ?? 0) > 0)
    .map(([key, list]) => t(key, { list: (list ?? []).join("、") }));
}

function PreviewCard({
  preview,
  busy,
  onInstall,
}: {
  preview: PackagePreviewResponse;
  busy: boolean;
  onInstall: () => void;
}) {
  const problems = preview.problems ?? [];
  // 「已經裝了」的警告由下面專屬的一句取代，不重複。
  const warnings = preview.exists ? [] : (preview.warnings ?? []);
  const lines = bringsLines(preview);
  const blocked = problems.length > 0 || preview.exists;
  return (
    <div
      data-testid="package-url-preview"
      className="flex flex-col gap-2.5 rounded-xl border border-border bg-bg px-3.5 py-3"
    >
      <div>
        <p className="text-[15px] leading-snug font-semibold break-words">
          {preview.name || preview.id}
        </p>
        {preview.version ? (
          <p className="mt-0.5 text-xs text-muted">
            {t("settings.packages.url.version", { v: preview.version })}
          </p>
        ) : null}
        {preview.description ? (
          <p className="mt-1 text-[13px] leading-snug text-muted break-words">
            {preview.description}
          </p>
        ) : null}
      </div>
      {preview.runs_program ? (
        <p
          role="status"
          data-testid="package-url-runs"
          className="rounded-xl bg-attention-soft px-3 py-2 text-[13px] leading-snug font-semibold text-attention-fg"
        >
          {t("settings.packages.url.runs")}
        </p>
      ) : null}
      <div className="flex flex-col gap-1 text-[13px] leading-snug">
        <p className="font-semibold">{t("settings.packages.url.brings")}</p>
        {lines.length === 0 ? (
          <p className="text-muted">{t("settings.packages.url.bringsNothing")}</p>
        ) : (
          lines.map((line) => (
            <p key={line} className="break-words text-muted">
              {line}
            </p>
          ))
        )}
      </div>
      {warnings.map((w) => (
        <p key={w} className="text-[13px] leading-snug break-words text-attention-fg">
          {w}
        </p>
      ))}
      {preview.exists ? (
        <p role="status" className="text-[13px] leading-snug text-attention-fg">
          {t("settings.packages.url.exists")}
        </p>
      ) : null}
      {problems.length > 0 ? (
        <div role="alert" data-testid="package-url-problems" className="flex flex-col gap-1">
          <p className="text-[13px] font-semibold text-danger">
            {t("settings.packages.url.problems")}
          </p>
          <ul className="m-0 flex list-none flex-col gap-1 p-0">
            {problems.map((p) => (
              <li key={p} className="text-[13px] leading-snug break-words text-danger">
                {p}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      <div>
        <ActionButton tone="primary" busy={busy} disabled={blocked} onClick={onInstall}>
          {busy ? t("settings.packages.url.installing") : t("settings.packages.url.install")}
        </ActionButton>
      </div>
    </div>
  );
}

// 「從網址安裝」（REQ-282）：貼 git 網址 → 看看（預覽）→ 確認 → 安裝。預覽有問題或已經裝了就不能裝；
// 網址一定要先看過預覽，所以安裝時送 confirmed: true。
export function PackageUrlInstall() {
  const client = useQueryClient();
  const urlId = useId();
  const refId = useId();
  const refHintId = useId();
  const [open, setOpen] = useState(false);
  const [source, setSource] = useState("");
  const [ref, setRef] = useState("");
  const [preview, setPreview] = useState<PackagePreviewResponse | null>(null);
  const [looking, setLooking] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [error, setError] = useState("");

  const close = () => {
    setOpen(false);
    setSource("");
    setRef("");
    setPreview(null);
    setError("");
  };
  const look = async (e: FormEvent) => {
    e.preventDefault();
    setLooking(true);
    setError("");
    setPreview(null);
    try {
      setPreview(
        await api.package.preview({ source: source.trim(), ref: ref.trim() || undefined }),
      );
    } catch (err) {
      setError(t("settings.packages.failed", { reason: reasonOf(err) }));
    } finally {
      setLooking(false);
    }
  };
  const install = async () => {
    if (!preview) return;
    setInstalling(true);
    setError("");
    try {
      const res = await api.package.install({
        source: source.trim(),
        ref: ref.trim() || undefined,
        confirmed: true,
      });
      showToast(
        t("settings.packages.url.done", {
          name: res.package?.name || res.package?.id || preview.name || "",
        }),
      );
      close();
    } catch (err) {
      setError(t("settings.packages.failed", { reason: reasonOf(err) }));
    } finally {
      setInstalling(false);
      void invalidatePackageViews(client);
    }
  };

  if (!open) {
    return (
      <div>
        <ActionButton tone="outline" onClick={() => setOpen(true)}>
          {t("settings.packages.url")}
        </ActionButton>
      </div>
    );
  }
  const edited = () => setPreview(null);
  return (
    <div className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface px-4 py-3.5 shadow-[var(--shadow-card)]">
      <form onSubmit={(e) => void look(e)} className="flex flex-col gap-3">
        <div className="flex flex-col gap-1.5">
          <label htmlFor={urlId} className="text-[13px] font-semibold">
            {t("settings.packages.url.label")}
          </label>
          <input
            id={urlId}
            value={source}
            onChange={(e) => {
              setSource(e.target.value);
              edited();
            }}
            placeholder="https://github.com/…"
            inputMode="url"
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            className={inputClass}
          />
          <p className="text-xs text-muted">{t("settings.packages.url.hint")}</p>
        </div>
        <div className="flex flex-col gap-1.5">
          <label htmlFor={refId} className="text-[13px] font-semibold">
            {t("settings.packages.url.ref")}
          </label>
          <input
            id={refId}
            aria-describedby={refHintId}
            value={ref}
            onChange={(e) => {
              setRef(e.target.value);
              edited();
            }}
            placeholder="v1.0.0"
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            className={inputClass}
          />
          <p id={refHintId} className="text-xs text-muted">
            {t("settings.packages.url.ref.hint")}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <ActionButton
            type="submit"
            tone={preview ? "outline" : "primary"}
            busy={looking}
            disabled={source.trim() === "" || installing}
          >
            {looking ? t("settings.packages.url.looking") : t("settings.packages.url.look")}
          </ActionButton>
          <ActionButton tone="outline" disabled={looking || installing} onClick={close}>
            {t("settings.packages.url.cancel")}
          </ActionButton>
        </div>
      </form>
      {preview ? (
        <PreviewCard preview={preview} busy={installing} onInstall={() => void install()} />
      ) : null}
      {error ? (
        <p role="alert" className="text-xs leading-snug text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}
