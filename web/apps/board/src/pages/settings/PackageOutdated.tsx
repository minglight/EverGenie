import { api, type PackageOutdated as Outdated } from "@evergenie/sdk-web";
import { showToast } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { invalidatePackageViews } from "../../data/queries";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import { packageOutdatedQuery } from "./queries";

// 從網址裝的套件有新版（REQ-282）：進頁時問一次；〔更新到 x.y.z〕先確認（資料會保留）才送 package.upgrade。
// 查不到新版（沒網路）只在最後一行輕輕提一句，不當成錯誤。
export function PackageOutdated() {
  const client = useQueryClient();
  const query = useQuery(packageOutdatedQuery);
  const [pending, setPending] = useState<Outdated | null>(null);
  const [busyId, setBusyId] = useState("");
  const [error, setError] = useState("");

  const list = query.data?.packages ?? [];
  const failed = query.data?.failed ?? [];
  if (list.length === 0 && failed.length === 0) return null;

  const upgrade = async (pkg: Outdated) => {
    setPending(null);
    setBusyId(pkg.id ?? "");
    setError("");
    try {
      const res = await api.package.upgrade({ id: pkg.id, ref: pkg.latest });
      showToast(
        t("settings.packages.outdated.done", {
          name: pkg.name || pkg.id || "",
          latest: res.to || pkg.latest || "",
        }),
      );
    } catch (err) {
      setError(t("settings.packages.failed", { reason: reasonOf(err) }));
    } finally {
      setBusyId("");
      void invalidatePackageViews(client);
    }
  };

  return (
    <section data-testid="packages-outdated" className="flex flex-col gap-2">
      {list.length > 0 ? (
        <>
          <h2 className="px-1 text-[13px] font-semibold text-muted">
            {t("settings.packages.outdated")}
          </h2>
          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {list.map((p) => (
              <li
                key={p.id}
                className="flex flex-col gap-2 rounded-[18px] border border-border bg-surface px-4 py-3.5 shadow-[var(--shadow-card)] sm:flex-row sm:items-center sm:justify-between"
              >
                <p className="text-[14px] leading-snug font-semibold break-words">
                  {t("settings.packages.outdated.row", {
                    name: p.name || p.id || "",
                    current: p.current || "—",
                    latest: p.latest || "—",
                  })}
                </p>
                <ActionButton
                  tone="primary"
                  busy={busyId === p.id}
                  disabled={busyId !== ""}
                  onClick={() => setPending(p)}
                >
                  {t("settings.packages.outdated.go", { latest: p.latest || "" })}
                </ActionButton>
              </li>
            ))}
          </ul>
        </>
      ) : null}
      {failed.length > 0 ? (
        <p className="px-1 text-xs leading-snug text-muted">
          {t("settings.packages.outdated.failed", { n: failed.length })}
        </p>
      ) : null}
      {error ? (
        <p role="alert" className="px-1 text-xs leading-snug text-danger">
          {error}
        </p>
      ) : null}
      <ConfirmDialog
        open={pending !== null}
        tone="primary"
        title={t("settings.packages.outdated.title", {
          name: pending?.name || pending?.id || "",
          latest: pending?.latest || "",
        })}
        body={t("settings.packages.outdated.body")}
        confirmLabel={t("settings.packages.outdated.yes")}
        cancelLabel={t("settings.packages.outdated.no")}
        onCancel={() => setPending(null)}
        onConfirm={() => {
          if (pending) void upgrade(pending);
        }}
      />
    </section>
  );
}
