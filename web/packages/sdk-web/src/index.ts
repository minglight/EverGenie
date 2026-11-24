export { chatDraftKey, seedChatDraft } from "./draft";
export { type Api, api, makeApi } from "./gen/client";
export type * from "./gen/types";
export { ApiError, call, onStreamsChange, stream, streamsDown } from "./transport";
export {
  type HomeBackupUploadResult,
  MAX_HOME_BACKUP_UPLOAD_BYTES,
  MAX_PERSONA_UPLOAD_BYTES,
  MAX_UPLOAD_BYTES,
  type PersonaUploadResult,
  type UploadResult,
  upload,
  uploadHomeBackup,
  uploadPersona,
} from "./upload";
export {
  ALL_KINDS,
  emitVaultChanged,
  onLiveChange,
  onVaultChanged,
  type VaultChange,
} from "./vaultEvents";
