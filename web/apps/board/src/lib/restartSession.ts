import { type Api, ApiError, type Session } from "@evergenie/sdk-web";

// 重開一串對話（DEV-106）：跟 `evergenie sessions restart` 一樣，先關再開，對話內容留著（不是 rotate，rotate 會清掉）。
// 聊天頁的「重開對話」列與設定頁換帳號之後的「重開她的對話」共用。
export async function restartSession(api: Api, session: Session): Promise<Session | undefined> {
  const name = session.name ?? "";
  try {
    await api.session.close({ name });
  } catch (err) {
    // 本來就沒在跑：直接開（跟 CLI 一樣）。其他錯誤照實回報，不往下開。
    if (!(err instanceof ApiError && err.code === "not_found")) throw err;
  }
  const res = await api.session.open({ name, agent: session.agent, kind: session.kind });
  return res.session;
}
