import { api, type ExtensionPoint } from "@evergenie/sdk-web";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { invalidatePackageViews } from "../../data/queries";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import { packageExtensionsQuery } from "./queries";
import { SettingGroup } from "./SettingRow";

const inputClass =
  "w-full rounded-xl border border-border-strong bg-bg px-3 py-2.5 text-base leading-snug text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus";

// 一個可以調的擴充點：setting 單行、prompt_append 多行；離開欄位才送，清空＝拿掉設定（value:{}）。
function Field({ ext, saved, label }: { ext: ExtensionPoint; saved: string; label: string }) {
  const client = useQueryClient();
  const [text, setText] = useState(saved);
  const [shownSaved, setShownSaved] = useState(saved);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  // 伺服器那邊變了（別處改的、重抓到新值），欄位跟著換。
  if (saved !== shownSaved) {
    setShownSaved(saved);
    setText(saved);
  }
  const multi = ext.kind === "prompt_append";

  const commit = async () => {
    if (text === saved) return;
    setError("");
    setDone(false);
    try {
      await api.package.setExtension({
        package: ext.package,
        extension: ext.id,
        value: text === "" ? {} : { value: text },
      });
      setDone(true);
    } catch (err) {
      setError(t("settings.packages.failed", { reason: reasonOf(err) }));
    } finally {
      await invalidatePackageViews(client);
    }
  };

  const props = {
    "aria-label": label,
    value: text,
    placeholder: ext.default_value
      ? t("settings.packages.extensions.default", { value: ext.default_value })
      : undefined,
    onChange: (e: { target: { value: string } }) => {
      setText(e.target.value);
      setDone(false);
    },
    onBlur: () => void commit(),
    className: inputClass,
  };
  return (
    <div className="flex flex-col gap-1.5">
      {multi ? <textarea rows={4} {...props} /> : <input type="text" {...props} />}
      {error ? (
        <p role="alert" className="text-xs leading-snug text-danger">
          {error}
        </p>
      ) : done ? (
        <p role="status" className="text-xs text-ok">
          {t("settings.packages.extensions.saved")}
        </p>
      ) : null}
    </div>
  );
}

// 擴充點：依套件分段。page_slot 只顯示（頁面位置由套件自己決定，不能改）。
export function PackageExtensions({ names }: { names: Record<string, string> }) {
  const q = useQuery(packageExtensionsQuery);
  if (q.data === undefined) {
    return q.isError ? (
      <LoadFailed what={t("settings.loadFailed")} onRetry={() => void q.refetch()} />
    ) : (
      <LoadingBlock />
    );
  }
  const { extensions = [], values = {}, warnings = [] } = q.data;
  const byPackage = new Map<string, ExtensionPoint[]>();
  for (const e of extensions) {
    const key = e.package ?? "";
    byPackage.set(key, [...(byPackage.get(key) ?? []), e]);
  }
  if (byPackage.size === 0) {
    return (
      <SettingGroup title={t("settings.packages.extensions")}>
        <p className="px-4 py-3.5 text-[13px] text-muted">
          {t("settings.packages.extensions.empty")}
        </p>
      </SettingGroup>
    );
  }
  return (
    <div className="flex flex-col gap-5">
      {[...byPackage.entries()].map(([pkg, list]) => (
        <SettingGroup
          key={pkg}
          title={`${t("settings.packages.extensions")} · ${names[pkg] || pkg}`}
        >
          {warnings
            .filter((w) => w.includes(pkg))
            .map((w) => (
              <p
                key={w}
                className="bg-attention-soft px-4 py-2.5 text-[13px] leading-snug text-attention-fg"
              >
                {w}
              </p>
            ))}
          {list.map((ext) => {
            const label = ext.description || ext.id || "";
            return (
              <div
                key={ext.id}
                data-testid={`extension-${pkg}-${ext.id}`}
                className="flex flex-col gap-2 px-4 py-3.5"
              >
                <p className="text-[15px] leading-snug font-semibold">{label}</p>
                {ext.kind === "page_slot" ? (
                  <p className="text-[13px] leading-snug text-muted">
                    {t("settings.packages.extensions.slot")}
                  </p>
                ) : (
                  <Field ext={ext} saved={values[`${pkg}.${ext.id}`] ?? ""} label={label} />
                )}
              </div>
            );
          })}
        </SettingGroup>
      ))}
    </div>
  );
}
