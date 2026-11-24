import { api, type Tier } from "@evergenie/sdk-web";
import { Segmented, showToast } from "@evergenie/ui";
import { useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { invalidatePackageViews } from "../../data/queries";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";

const inputClass =
  "w-full rounded-xl border border-border-strong bg-bg px-3 py-2.5 text-base leading-snug text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus";

// 「從資料夾安裝」：按鈕展開表單（絕對路徑＋層級二選一）；錯誤顯示在表單下面。
export function FolderInstall() {
  const client = useQueryClient();
  const pathId = useId();
  const hintId = useId();
  const [open, setOpen] = useState(false);
  const [source, setSource] = useState("");
  const [tier, setTier] = useState<Extract<Tier, "installed" | "mine">>("installed");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const close = () => {
    setOpen(false);
    setSource("");
    setError("");
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await api.package.install({ source: source.trim(), tier });
      showToast(
        t("settings.packages.folder.done", { name: res.package?.name || res.package?.id || "" }),
      );
      close();
    } catch (err) {
      setError(t("settings.packages.failed", { reason: reasonOf(err) }));
    } finally {
      setBusy(false);
      void invalidatePackageViews(client);
    }
  };

  if (!open) {
    return (
      <div>
        <ActionButton tone="outline" onClick={() => setOpen(true)}>
          {t("settings.packages.folder")}
        </ActionButton>
      </div>
    );
  }
  return (
    <form
      onSubmit={(e) => void submit(e)}
      className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface px-4 py-3.5 shadow-[var(--shadow-card)]"
    >
      <div className="flex flex-col gap-1.5">
        <label htmlFor={pathId} className="text-[13px] font-semibold">
          {t("settings.packages.folder.path")}
        </label>
        <input
          id={pathId}
          aria-describedby={hintId}
          value={source}
          onChange={(e) => setSource(e.target.value)}
          placeholder="/Users/…"
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
          className={inputClass}
        />
        <p id={hintId} className="text-xs text-muted">
          {t("settings.packages.folder.path.hint")}
        </p>
      </div>
      <Segmented
        options={[
          { value: "installed", label: t("settings.packages.tier.installed") },
          { value: "mine", label: t("settings.packages.tier.mine") },
        ]}
        value={tier}
        onChange={setTier}
        ariaLabel={t("settings.packages.folder.tier")}
      />
      <div className="flex flex-wrap gap-2">
        <ActionButton type="submit" tone="primary" busy={busy} disabled={source.trim() === ""}>
          {busy ? t("settings.packages.folder.busy") : t("settings.packages.folder.submit")}
        </ActionButton>
        <ActionButton tone="outline" disabled={busy} onClick={close}>
          {t("settings.packages.folder.cancel")}
        </ActionButton>
      </div>
      {error ? (
        <p role="alert" className="text-xs leading-snug text-danger">
          {error}
        </p>
      ) : null}
    </form>
  );
}
