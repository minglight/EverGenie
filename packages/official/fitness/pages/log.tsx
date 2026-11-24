import { Button, LoadFailed, Segmented, Spinner } from "@evergenie/ui";
import { useRef, useState } from "react";
import { EquipmentPanel } from "./lib/equipment";
import { History } from "./lib/history";
import { useLoad } from "./lib/parts";
import { CardioForm, Strength } from "./lib/record";
import { loadEquipment, loadWorkouts } from "./lib/store";

type View = "record" | "history";
type Kind = "重訓" | "有氧";

// 運動：記重訓與有氧（跑步、划船、游泳、騎車）、教練的即時建議與收操檢討、歷史、器材（REQ-127）。
export default function Log({ search }: { pkg: string; page: string; search: string }) {
  const [view, setView] = useState<View>(() =>
    new URLSearchParams(search).get("view") === "history" ? "history" : "record",
  );
  const [kind, setKind] = useState<Kind>("重訓");
  const [drawer, setDrawer] = useState(false);
  const drawerRef = useRef<HTMLDivElement>(null);
  const toggleDrawer = () => {
    setDrawer((d) => !d);
    requestAnimationFrame(() =>
      drawerRef.current?.scrollIntoView?.({ block: "start", behavior: "smooth" }),
    );
  };
  const workouts = useLoad(loadWorkouts);
  const equipment = useLoad(loadEquipment);

  const go = (v: View) => {
    setView(v);
    // 網址帶分頁，重新整理回到同一頁（FEN-4）；不新增歷史紀錄。
    history.replaceState(history.state, "", v === "history" ? "?view=history" : location.pathname);
  };
  const reloadAll = () => {
    workouts.reload();
    equipment.reload();
  };

  const loadError = workouts.error || equipment.error;

  return (
    <section data-testid="fitness-log" className="flex flex-col gap-4">
      <Segmented
        ariaLabel="運動分頁"
        options={[
          { value: "record", label: "記錄" },
          { value: "history", label: "歷史" },
        ]}
        value={view}
        onChange={go}
      />
      <div className="flex flex-wrap items-center gap-2">
        <a
          href="/p/fitness/plan"
          className="inline-flex min-h-11 items-center rounded-lg px-3 font-medium text-accent underline"
        >
          訓練計畫
        </a>
      </div>
      {loadError ? (
        <LoadFailed
          what="運動記錄"
          onRetry={reloadAll}
          variant={workouts.data && equipment.data ? "inline" : "block"}
          className={workouts.data && equipment.data ? undefined : "py-8"}
        />
      ) : null}
      {workouts.data && equipment.data ? (
        view === "history" ? (
          <History workouts={workouts.data} />
        ) : (
          <>
            <Segmented
              ariaLabel="運動種類"
              options={[
                { value: "重訓", label: "重訓" },
                { value: "有氧", label: "有氧" },
              ]}
              value={kind}
              onChange={setKind}
            />
            {/* 管理器材的抽屜放在挑器材上面：以前放整頁最下面，按了像沒反應（DEV-146）。 */}
            {drawer && kind === "重訓" ? (
              <div
                ref={drawerRef}
                data-testid="equipment-drawer"
                className="flex flex-col gap-3 scroll-mt-4 rounded-[18px] border border-border bg-surface p-4"
              >
                <div className="flex items-center justify-between gap-2">
                  <h2 className="font-semibold">器材</h2>
                  <Button onClick={() => setDrawer(false)}>關閉</Button>
                </div>
                <EquipmentPanel equipment={equipment.data} onChanged={reloadAll} />
              </div>
            ) : null}
            {kind === "重訓" ? (
              <Strength
                workouts={workouts.data}
                equipment={equipment.data}
                reload={reloadAll}
                onManageEquipment={toggleDrawer}
                managing={drawer}
              />
            ) : (
              <CardioForm workouts={workouts.data} reload={reloadAll} />
            )}
          </>
        )
      ) : !loadError ? (
        <div className="flex justify-center py-8">
          <Spinner label="載入中" />
        </div>
      ) : null}
    </section>
  );
}
