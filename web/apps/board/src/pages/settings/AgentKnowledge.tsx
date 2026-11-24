import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check } from "lucide-react";
import { useEffect, useState } from "react";
import { KnowledgePicker } from "../../components/KnowledgePicker";
import { enabledIds, knowledgeKey, knowledgeQuery, setKnowledge } from "../../data/knowledge";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";

// 「職位與秘書」一張卡裡的「她帶的知識」（DEV-161）：列這個職位的可選知識，勾了就存（set_knowledge）。
// 沒有可選知識、或讀不到，這塊就不出現（不影響卡片上其他設定）。
export function AgentKnowledge({ agent }: { agent: string }) {
  const client = useQueryClient();
  const query = useQuery(knowledgeQuery(agent));
  const [error, setError] = useState("");
  // 剛勾的結果先放在這裡：勾選框是受控的，要在同一個事件裡就換新的值，不然會先彈回去再變。存好、重讀完就拿掉。
  const [pending, setPending] = useState<string[] | null>(null);
  // 「已存」放在清單底下、勾選的那個地方，不用底部的提示：手機上提示會蓋住頁面底下的按鈕（DEV-161）。2.5 秒後淡出。
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    if (!saved) return;
    const timer = setTimeout(() => setSaved(false), 2500);
    return () => clearTimeout(timer);
  }, [saved]);
  const key = knowledgeKey(agent);

  const save = useMutation({
    mutationKey: key,
    mutationFn: (enabled: string[]) => setKnowledge(agent, enabled),
    onMutate: () => {
      setError("");
      setSaved(false);
    },
    onSuccess: () => setSaved(true),
    onError: (err) => setError(t("knowledge.failed", { reason: reasonOf(err) })),
    // 連續快速勾的時候，只在最後一次存完才重讀，前一次的回應不會把後一次剛勾的蓋回去；
    // 失敗也重讀，畫面回到伺服器真正的樣子。
    onSettled: async () => {
      if (client.isMutating({ mutationKey: key }) > 1) return;
      await client.invalidateQueries({ queryKey: key });
      setPending(null);
    },
  });

  const blocks = query.data ?? [];
  if (blocks.length === 0) return null;
  const checked = new Set(pending ?? enabledIds(blocks));
  return (
    <div className="flex flex-col gap-1.5">
      <KnowledgePicker
        agent={agent}
        blocks={blocks}
        checked={checked}
        onToggle={(id, next) => {
          const now = new Set(checked);
          if (next) now.add(id);
          else now.delete(id);
          const ids = blocks.filter((b) => now.has(b.id)).map((b) => b.id);
          setPending(ids);
          save.mutate(ids);
        }}
      />
      {error ? (
        <p role="alert" className="text-xs leading-snug text-danger">
          {error}
        </p>
      ) : null}
      <p
        role="status"
        aria-live="polite"
        data-testid="knowledge-saved"
        className="m-0 flex min-h-5 items-center gap-1.5 text-[13px] font-medium text-ok transition-opacity duration-300"
        style={{ opacity: saved ? 1 : 0 }}
      >
        {saved ? (
          <>
            <Check className="size-4" strokeWidth={2.2} aria-hidden="true" />
            {t("knowledge.saved")}
          </>
        ) : null}
      </p>
    </div>
  );
}
