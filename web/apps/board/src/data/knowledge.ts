import { type AgentsKnowledgeResponse, api } from "@evergenie/sdk-web";
import { queryOptions } from "@tanstack/react-query";

// 職位帶的可選知識（DEV-161）：官方職責檔列出可選的知識區塊，主人勾了哪幾個記在家裡。
// 基本知識一定帶、不在這裡出現；這裡只有「可選的」。
// 還沒有秘書紀錄（新家歡迎頁）時，核心也回職責樣板的可選知識，enabled 是預設勾法。

export interface KnowledgeBlock {
  id: string;
  title: string;
  /** 一句說明。 */
  summary: string;
  /** 官方預設勾不勾（新家歡迎頁沒有秘書紀錄時，用它當預設）。 */
  default: boolean;
  /** 這個家現在勾不勾。 */
  enabled: boolean;
}

/** 核心回來的區塊（欄位在型別上都是選用的）整理成畫面用的樣子；沒有代號的丟掉。 */
export const toBlocks = (r: AgentsKnowledgeResponse): KnowledgeBlock[] =>
  (r.blocks ?? []).flatMap((b) =>
    b.id
      ? [
          {
            id: b.id,
            title: b.title || b.id,
            summary: b.summary ?? "",
            default: b.default,
            enabled: b.enabled,
          },
        ]
      : [],
  );

export const getKnowledge = async (agent: string) =>
  toBlocks(await api.agents.knowledge({ agent }));

export const setKnowledge = async (agent: string, enabled: string[]) =>
  toBlocks(await api.agents.setKnowledge({ agent, enabled }));

export const knowledgeKey = (agent: string) => ["agents.knowledge", agent] as const;

export const knowledgeQuery = (agent: string) =>
  queryOptions({
    queryKey: knowledgeKey(agent),
    queryFn: () => getKnowledge(agent),
    staleTime: 10_000,
    retry: false,
  });

/** 勾了的區塊代號（照區塊原本的順序，存的時候順序固定）。 */
export const enabledIds = (blocks: readonly KnowledgeBlock[]) =>
  blocks.filter((b) => b.enabled).map((b) => b.id);
