import { ApiError } from "./transport";

/** 伺服器的上限（internal/base/app/board/upload.go）；先在本機擋掉，免得白傳一次。 */
export const MAX_UPLOAD_BYTES = 20 * 1024 * 1024;

export interface UploadResult {
  /** 存的檔名（`runtime/share/` 底下）。 */
  name: string;
  /** `/share/<name>`。 */
  url: string;
  size: number;
}

/**
 * 上傳檔案到 runtime/share/（聊天附件 REQ-82、套件頁面也用同一個，例如 closet-mine）：POST /api/web/upload，body 是檔案原始位元組。
 * 檔名放 X-Filename（encodeURIComponent）；X-Requested-With 讓跨站表單送不出這個請求。
 * `base` 是要傳到哪一台（看板的多台，S3）：預設 ""＝同源；別台給它整理過的 origin（不帶結尾斜線）。
 * 回傳的 `url` 照那台伺服器給的相對網址（`/share/<name>`），要在別台的看板上顯示時由呼叫端補成那台的網址。
 */
export async function upload(file: File, base = ""): Promise<UploadResult> {
  if (file.size > MAX_UPLOAD_BYTES) {
    throw new ApiError("invalid", "檔案太大（上限 20 MB）");
  }
  const res = await post(`${base}/api/web/upload`, file, {
    "Content-Type": file.type || "application/octet-stream",
    "X-Filename": encodeURIComponent(file.name),
  });
  return (await res.json()) as UploadResult;
}

/**
 * 角色匯出檔的上限，跟 Go 的 sdk.PersonaImportMaxBytes 同一個數字（看板上傳與核心匯入都用它）；
 * 寫成數字字面值，sdk 的 TestPersonaUploadLimitMatchesWeb 比對兩邊。
 */
export const MAX_PERSONA_UPLOAD_BYTES = 2147483648;

export interface PersonaUploadResult {
  /** 存的檔名（`runtime/persona-import/uploads/` 底下），交給 `personas.import` 的 `upload`。 */
  name: string;
  size: number;
}

/**
 * 上傳角色匯出檔（`.hgpersona`，DEV-116 S3）：POST /api/web/persona-upload，body 是檔案原始位元組。
 * 存在家的 runtime/（不公開伺服），匯入完由核心刪掉；`base` 同 `upload`。
 */
export async function uploadPersona(file: File, base = ""): Promise<PersonaUploadResult> {
  if (file.size > MAX_PERSONA_UPLOAD_BYTES) {
    throw new ApiError("invalid", `檔案太大（上限 ${MAX_PERSONA_UPLOAD_BYTES / 2 ** 30} GB）`);
  }
  const res = await post(`${base}/api/web/persona-upload`, file, {
    "Content-Type": "application/octet-stream",
  });
  return (await res.json()) as PersonaUploadResult;
}

/**
 * 整個家的備份檔（`.egbackup`）從看板上傳的上限，跟 Go 的 sdk.HomeBackupUploadMaxBytes 同一個數字；
 * 寫成數字字面值，sdk 的 TestHomeBackupUploadLimitMatchesWeb 比對兩邊。
 */
export const MAX_HOME_BACKUP_UPLOAD_BYTES = 2147483648;

export interface HomeBackupUploadResult {
  /** 存的檔名（家的暫存上傳資料夾底下），交給 `backup.preview`、`backup.restore` 的 `upload`。 */
  name: string;
  size: number;
}

/**
 * 上傳備份檔（DEV-153）：POST /api/web/home-backup-upload，body 是檔案原始位元組。存在家的暫存（不公開伺服），一天後清。
 */
export async function uploadHomeBackup(file: File, base = ""): Promise<HomeBackupUploadResult> {
  if (file.size > MAX_HOME_BACKUP_UPLOAD_BYTES) {
    throw new ApiError(
      "invalid",
      `備份檔太大（上限 ${MAX_HOME_BACKUP_UPLOAD_BYTES / 2 ** 30} GB），沒辦法從看板上傳；跟任何一位秘書說「用這台電腦上的備份檔還原」，告訴她檔案放在哪裡`,
    );
  }
  const res = await post(`${base}/api/web/home-backup-upload`, file, {
    "Content-Type": "application/octet-stream",
  });
  return (await res.json()) as HomeBackupUploadResult;
}

async function post(url: string, file: File, headers: Record<string, string>): Promise<Response> {
  let res: Response;
  try {
    res = await fetch(url, {
      method: "POST",
      headers: { ...headers, "X-Requested-With": "evergenie" },
      body: file,
    });
  } catch {
    throw new ApiError("unavailable", "連不到看板服務，請再試一次");
  }
  if (!res.ok) {
    let message = `上傳失敗（${res.status}）`;
    try {
      const body = (await res.json()) as { message?: string };
      if (body.message) message = body.message;
    } catch {
      // 不是 JSON：用狀態碼
    }
    throw new ApiError(res.status === 413 ? "invalid" : "unavailable", message);
  }
  return res;
}
