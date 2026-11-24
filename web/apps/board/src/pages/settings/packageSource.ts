import { t } from "../../i18n/zh-TW";

/** 把安裝來源的 git 網址縮短成看得懂的「主機與路徑」（去掉通訊協定、帳號、結尾的 .git）。 */
export function sourceWhere(url: string): string {
  const raw = url.trim();
  if (raw === "") return "";
  if (raw.startsWith("file://")) return "";
  let rest = raw;
  const scp = /^[^@/\s]+@([^:/\s]+):(.+)$/.exec(raw); // git@host:owner/repo.git
  if (scp) {
    rest = `${scp[1]}/${scp[2]}`;
  } else {
    rest = raw.replace(/^[a-z+]+:\/\//i, "").replace(/^[^@/]*@/, "");
  }
  return rest.replace(/\/+$/, "").replace(/\.git$/, "");
}

/** 套件列上「來自 …」那一行；本機資料夾（file://）只說「來自本機資料夾」。 */
export function sourceLabel(url: string | undefined): string {
  if (!url?.trim()) return "";
  const where = sourceWhere(url);
  return where === ""
    ? t("settings.packages.source.folder")
    : t("settings.packages.source", { where });
}
