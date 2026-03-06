type SnapshotBehaviorBar = {
  id: number;
  progressPct: number;
  state: "queued" | "active";
};

type PlayerSnapshotProps = {
  name: string;
  realmId?: number;
  tick?: number;
  day?: number;
  clock?: string;
  dayPart?: string;
  marketState?: string;
  streamStatus: "connecting" | "live" | "fallback" | "offline";
  staminaCurrent: number;
  staminaMax: number;
  coins?: number;
  queuedOrActive?: number;
  behaviorBars?: SnapshotBehaviorBar[];
  realmPausedMessage?: string | null;
};

export function PlayerSnapshot(props: PlayerSnapshotProps) {
  const staminaPercent = props.staminaMax > 0 ? Math.min(100, Math.max(0, (props.staminaCurrent / props.staminaMax) * 100)) : 0;

  return (
    <section className="snapshot-bar panel">
      <div>
        <h3>Player Snapshot</h3>
        <p>{props.name} · realm {props.realmId ?? "-"}</p>
      </div>
      <div className="snapshot-row">
        <span>Tick {props.tick ?? "-"}</span>
        <span>Day {props.day ?? "-"}</span>
        <span>{props.clock ?? "--:--"}{props.dayPart ? ` · ${props.dayPart}` : ""}</span>
        <span>Market {props.marketState ?? "-"}</span>
        <span className={`stream-pill ${props.streamStatus}`}>Stream {props.streamStatus}</span>
      </div>
      <div className="snapshot-row">
        <div className="stamina-inline" title={`${props.staminaCurrent} / ${props.staminaMax}`}>
          <div className="progress-wrap"><div className="progress-bar" style={{ width: `${staminaPercent}%` }} /></div>
          <span>Stamina {props.staminaCurrent}/{props.staminaMax}</span>
        </div>
        <span>Coins {props.coins ?? "-"}</span>
        <span>Queued/Active {props.queuedOrActive ?? "-"}</span>
      </div>
      <div className="snapshot-row">
        <div className="behavior-skyline" title="Queued and active behavior progress">
          {props.behaviorBars && props.behaviorBars.length > 0 ? (
            props.behaviorBars.map((bar) => (
              <div key={bar.id} className={`behavior-bar behavior-bar-${bar.state}`} aria-hidden="true">
                <div className="behavior-bar-fill" style={{ height: `${bar.progressPct}%` }} />
              </div>
            ))
          ) : (
            <span className="behavior-skyline-empty">No active behaviors</span>
          )}
        </div>
      </div>
      {props.realmPausedMessage ? <div className="notice error">{props.realmPausedMessage}</div> : null}
    </section>
  );
}
