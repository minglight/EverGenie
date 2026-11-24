import { api } from "@evergenie/sdk-web";
import { queryOptions } from "@tanstack/react-query";

// 整個家的備份現況（DEV-153）：上次備份、這台現有的備份、上一次還原的結果。
export const backupStatusQuery = queryOptions({
  queryKey: ["backup.status"],
  queryFn: () => api.backup.getStatus({}),
  staleTime: 15_000,
});

// 每週的備份掃描（DEV-163）：上一次的報告（還沒掃過是 undefined）與「另一次還在跑」。
export const backupSweepQuery = queryOptions({
  queryKey: ["system.backup_sweep"],
  queryFn: () => api.system.backupSweep({}),
  staleTime: 60_000,
});
