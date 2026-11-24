import { api, type Package } from "@evergenie/sdk-web";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Toggle } from "../../components/Toggle";
import { invalidatePackageViews } from "../../data/queries";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";

/** 選用套件（manifest optional = true）：預設不啟用，設定 → 套件最上面「進階套件」列出來讓使用者決定（REQ-176）。 */
export function optionalPackages(list: Package[]): Package[] {
  return list.filter((p) => p.optional && p.id);
}

/**
 * 只給開發用的家的套件（manifest dev_only，例：開發工坊）在正式家的設定頁不列——一般人用不到（作者 2026-11-04）。
 * 正式家已經開著的照列，讓人能關掉；家的身分還沒讀到（dev 是 false）時當成正式家。
 */
export function hiddenDevOnly(p: Package, dev: boolean): boolean {
  return Boolean(p.dev_only) && !dev && !p.enabled;
}

// 一個選用套件一列：名稱、說明、開關。開關送 package.set_enabled；被拒（例如依賴的套件沒開）時顯示原因。
function OptionalRow({ pkg }: { pkg: Package }) {
  const client = useQueryClient();
  const id = pkg.id ?? "";
  const name = pkg.name || id;
  const [note, setNote] = useState("");
  const toggle = useMutation({
    mutationFn: (enabled: boolean) => api.package.setEnabled({ id, enabled }),
    onMutate: () => setNote(""),
    onSuccess: (res) => {
      if (res.package?.state === "rejected" && res.package.error) setNote(res.package.error);
    },
    onError: (err) => setNote(t("settings.packages.failed", { reason: reasonOf(err) })),
    onSettled: () => invalidatePackageViews(client),
  });
  // 點了先顯示剛選的（UXC-9），伺服器回來再以伺服器的為準。
  const checked = toggle.isPending ? Boolean(toggle.variables) : pkg.enabled;
  const reason = note || (pkg.enabled ? pkg.error : "");
  return (
    <li
      data-testid={`optional-${id}`}
      className="flex items-start gap-3 border-t border-border pt-3 first:border-t-0 first:pt-0"
    >
      <div className="min-w-0 flex-1">
        <p className="text-[15px] leading-snug font-semibold break-words">{name}</p>
        {pkg.description ? (
          <p className="mt-0.5 text-[13px] leading-snug text-muted break-words">
            {pkg.description}
          </p>
        ) : null}
        {reason ? (
          <p role="status" className="mt-1 text-[13px] leading-snug text-danger break-words">
            {reason}
          </p>
        ) : null}
      </div>
      <Toggle
        checked={checked}
        label={t("settings.packages.enabled", { name })}
        disabled={toggle.isPending}
        onChange={(next) => toggle.mutate(next)}
      />
    </li>
  );
}

export function OptionalPackages({ packages }: { packages: Package[] }) {
  return (
    <ul className="m-0 flex list-none flex-col gap-3 p-0">
      {packages.map((p) => (
        <OptionalRow key={p.id} pkg={p} />
      ))}
    </ul>
  );
}
