import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { uiConfig } from "./uiConfig";
import webPackage from "../package.json";
import {
  type RecentEvent,
  type WorldStreamEvent,
  ascend,
  getSystemVersion,
  getBehaviorCatalog,
  getMarketStatus,
  getPlayerBehaviors,
  getPlayerInventory,
  getPlayerStatus,
  getWorldStatus,
  newGame,
  startBehavior
} from "./api";

export function App() {
  const queryClient = useQueryClient();
  const [actionNameInput, setActionNameInput] = useState("Wanderer");
  const [actionMenuOpen, setActionMenuOpen] = useState(false);
  const [actionModal, setActionModal] = useState<null | "new-game" | "ascend">(null);
  const [hasPromptedInitialNewGame, setHasPromptedInitialNewGame] = useState(false);
  const [selectedBehavior, setSelectedBehavior] = useState("player_scavenge_scrap");
  const [viewsTab, setViewsTab] = useState<"market" | "progression" | "influence">("market");
  const [marketWait, setMarketWait] = useState("12h");
  const [selectedEvent, setSelectedEvent] = useState<RecentEvent | null>(null);
  const [streamEvent, setStreamEvent] = useState<WorldStreamEvent | null>(null);
  const [streamConnected, setStreamConnected] = useState(false);
  const lastStreamTickRef = useRef<number | null>(null);
  const lastStreamMarketStateRef = useRef<string | null>(null);
  const lastStreamCoinsRef = useRef<number | null>(null);
  const lastStreamQueuedRef = useRef<number | null>(null);
  const invalidateTimersRef = useRef<Record<string, number | null>>({
    "world-status": null,
    "market-status": null,
    "player-status": null,
    "player-inventory": null,
    "player-behaviors": null,
    "behavior-catalog": null
  });

  const scheduleInvalidate = (key: string, delayMs: number = uiConfig.invalidation.defaultDebounceMs) => {
    const timers = invalidateTimersRef.current;
    const existingTimer = timers[key];
    if (existingTimer !== null && existingTimer !== undefined) {
      return;
    }

    timers[key] = window.setTimeout(() => {
      void queryClient.invalidateQueries({ queryKey: [key] });
      timers[key] = null;
    }, delayMs);
  };

  const playerStatus = useQuery({ queryKey: ["player-status"], queryFn: getPlayerStatus });
  const playerInventory = useQuery({ queryKey: ["player-inventory"], queryFn: getPlayerInventory });
  const playerBehaviors = useQuery({ queryKey: ["player-behaviors"], queryFn: getPlayerBehaviors });
  const marketStatus = useQuery({ queryKey: ["market-status"], queryFn: getMarketStatus });
  const worldStatus = useQuery({ queryKey: ["world-status"], queryFn: getWorldStatus });
  const behaviorCatalog = useQuery({ queryKey: ["behavior-catalog"], queryFn: getBehaviorCatalog });
  const systemVersion = useQuery({ queryKey: ["system-version"], queryFn: getSystemVersion, staleTime: Infinity, refetchOnWindowFocus: false });

  const invalidateCore = async () => {
    await queryClient.invalidateQueries({ queryKey: ["player-status"] });
    await queryClient.invalidateQueries({ queryKey: ["player-inventory"] });
    await queryClient.invalidateQueries({ queryKey: ["player-behaviors"] });
    await queryClient.invalidateQueries({ queryKey: ["behavior-catalog"] });
    await queryClient.invalidateQueries({ queryKey: ["market-status"] });
    await queryClient.invalidateQueries({ queryKey: ["world-status"] });
  };

  const newGameMutation = useMutation({
    mutationFn: newGame,
    onSuccess: invalidateCore
  });

  const startBehaviorMutation = useMutation({
    mutationFn: ({ behaviorKey, marketWait }: { behaviorKey: string; marketWait?: string }) =>
      startBehavior(behaviorKey, marketWait),
    onSuccess: invalidateCore
  });

  const ascendMutation = useMutation({
    mutationFn: ascend,
    onSuccess: invalidateCore
  });

  const player = playerStatus.data;
  const inventory = playerInventory.data?.inventory ?? {};
  const behaviors = playerBehaviors.data?.behaviors ?? [];
  const stats = player?.stats ?? {};
  const marketTickers = marketStatus.data?.tickers ?? [];
  const events = worldStatus.data?.recentEvents ?? [];

  const inventoryRows = useMemo(() => {
    return Object.entries(inventory).sort((a, b) => b[1] - a[1]);
  }, [inventory]);

  const ascension = player?.ascension;
  const canAscend = !!ascension?.available;
  const catalog = behaviorCatalog.data ?? [];
  const queueBehaviors = useMemo(() => {
    return catalog
      .filter((behavior) => behavior.queueVisible ?? behavior.available)
      .sort((left, right) => {
        if (left.available !== right.available) {
          return left.available ? -1 : 1;
        }
        return left.key.localeCompare(right.key);
      });
  }, [catalog]);
  const selectedBehaviorMeta = catalog.find((behavior) => behavior.key === selectedBehavior);
  const selectedRequiresMarketOpen = !!selectedBehaviorMeta?.requiresMarketOpen;

  useEffect(() => {
    if (queueBehaviors.length === 0) {
      if (selectedBehavior !== "") {
        setSelectedBehavior("");
      }
      return;
    }

    const selectedStillAvailable = queueBehaviors.some((behavior) => behavior.key === selectedBehavior);
    if (!selectedStillAvailable) {
      setSelectedBehavior(queueBehaviors[0].key);
    }
  }, [queueBehaviors, selectedBehavior]);

  const activeError =
    playerStatus.error ??
    playerInventory.error ??
    playerBehaviors.error ??
    marketStatus.error ??
    worldStatus.error ??
    behaviorCatalog.error ??
    newGameMutation.error ??
    startBehaviorMutation.error ??
    ascendMutation.error;

  useEffect(() => {
    let socket: WebSocket | null = null;
    let reconnectHandle: number | null = null;
    let closedByEffect = false;

    const connect = () => {
      const protocol = window.location.protocol === "https:" ? "wss" : "ws";
      socket = new WebSocket(`${protocol}://${window.location.host}/v1/stream/world`);

      socket.onopen = () => {
        setStreamConnected(true);
      };

      socket.onmessage = (event) => {
        try {
          const parsed = JSON.parse(event.data) as WorldStreamEvent;
          if (parsed.type === "world_snapshot") {
            setStreamEvent(parsed);
          }
        } catch {
          // ignore malformed events and keep stream alive
        }
      };

      socket.onclose = () => {
        setStreamConnected(false);
        if (!closedByEffect) {
          reconnectHandle = window.setTimeout(connect, uiConfig.stream.reconnectDelayMs);
        }
      };

      socket.onerror = () => {
        socket?.close();
      };
    };

    connect();

    return () => {
      closedByEffect = true;
      setStreamConnected(false);
      if (reconnectHandle !== null) {
        window.clearTimeout(reconnectHandle);
      }
      socket?.close();
    };
  }, []);

  useEffect(() => {
    return () => {
      const timers = invalidateTimersRef.current;
      for (const key of Object.keys(timers)) {
        const timer = timers[key];
        if (timer !== null && timer !== undefined) {
          window.clearTimeout(timer);
          timers[key] = null;
        }
      }
    };
  }, []);

  useEffect(() => {
    if (!streamConnected || !streamEvent) {
      return;
    }

    const currentTick = streamEvent.tick;
    const prevTick = lastStreamTickRef.current;
    const currentMarketState = streamEvent.marketState;
    const prevMarketState = lastStreamMarketStateRef.current;
    const currentCoins = streamEvent.player?.coins ?? null;
    const prevCoins = lastStreamCoinsRef.current;
    const currentQueued = streamEvent.player?.queuedOrActiveBehaviors ?? null;
    const prevQueued = lastStreamQueuedRef.current;

    if (prevTick === null || currentTick >= prevTick + uiConfig.invalidation.worldTickRefreshEveryTicks) {
      scheduleInvalidate("world-status");
      scheduleInvalidate("market-status");
      lastStreamTickRef.current = currentTick;
    }

    if (prevMarketState !== null && prevMarketState !== currentMarketState) {
      scheduleInvalidate("market-status", uiConfig.invalidation.marketStateDebounceMs);
    }

    if (prevCoins !== null && currentCoins !== null && prevCoins !== currentCoins) {
      scheduleInvalidate("player-status");
      scheduleInvalidate("player-inventory");
      scheduleInvalidate("behavior-catalog");
    }

    if (prevQueued !== null && currentQueued !== null && prevQueued !== currentQueued) {
      scheduleInvalidate("player-behaviors");
      scheduleInvalidate("behavior-catalog");
    }

    lastStreamTickRef.current = currentTick;
    lastStreamMarketStateRef.current = currentMarketState;
    lastStreamCoinsRef.current = currentCoins;
    lastStreamQueuedRef.current = currentQueued;
  }, [streamConnected, streamEvent]);

  const minuteOfDay = streamEvent?.minuteOfDay ?? marketStatus.data?.minuteOfDay ?? ((player?.simulationTick ?? 0) % (24 * 60));
  const clockHours = Math.floor(minuteOfDay / 60);
  const clockMinutes = minuteOfDay % 60;
  const clockLabel = streamEvent?.clock ?? `${String(clockHours).padStart(2, "0")}:${String(clockMinutes).padStart(2, "0")}`;
  const dayPart = streamEvent?.dayPart ?? getDayPart(minuteOfDay);
  const tickLabel = streamEvent?.tick ?? player?.simulationTick ?? 0;
  const currentTick = streamEvent?.tick ?? player?.simulationTick ?? 0;
  const dayLabel = (streamEvent?.day ?? Math.floor((player?.worldAgeDays ?? 0))) + 1;
  const marketOpen = streamEvent?.marketOpen ?? marketStatus.data?.isOpen;
  const marketOpensLabel = formatOpenCountdown(marketStatus.data?.minutesToOpen ?? 0);
  const marketClosesLabel = formatDurationMinutesCompact(marketStatus.data?.minutesToClose ?? 0);
  const progressionPaths = useMemo(() => buildProgressionPaths(catalog), [catalog]);
  const pathInfluence = useMemo(() => computePathInfluence(behaviors, stats), [behaviors, stats]);
  const showDevDiagnostics = Boolean((import.meta as ImportMeta & { env?: { DEV?: boolean } }).env?.DEV);

  const openActionModal = (mode: "new-game" | "ascend") => {
    setActionMenuOpen(false);
    setActionModal(mode);
    if (mode === "ascend") {
      setActionNameInput(player?.playerName ?? "Wanderer");
      return;
    }
    setActionNameInput("Wanderer");
  };

  useEffect(() => {
    if (hasPromptedInitialNewGame) {
      return;
    }
    if (playerStatus.isLoading) {
      return;
    }
    if (!player?.hasPrimaryPlayer) {
      setActionMenuOpen(false);
      setActionModal("new-game");
      setActionNameInput("Wanderer");
      setHasPromptedInitialNewGame(true);
    }
  }, [hasPromptedInitialNewGame, playerStatus.isLoading, player?.hasPrimaryPlayer]);

  const submitActionModal = () => {
    const name = actionNameInput.trim() || "Wanderer";
    if (actionModal === "new-game") {
      newGameMutation.mutate(name);
    }
    if (actionModal === "ascend") {
      ascendMutation.mutate(name);
    }
    setActionModal(null);
  };

  return (
    <div className="app-shell">
      <div className="aurora" />
      <header className="topbar glass">
        <div>
          <h1>Lived</h1>
          <p>Server-authoritative idle simulation command deck</p>
        </div>
        <div className="topbar-right">
          <div className="statline">
            <span>Tick {tickLabel}</span>
            <span>Day {dayLabel}</span>
            <span>{clockLabel} · {dayPart}</span>
            <span>{marketOpen ? "Market Open" : "Market Closed"}</span>
            <span className={streamConnected ? "stream live" : "stream fallback"}>{streamConnected ? "Live Stream" : "REST Polling"}</span>
          </div>
          <div className="actions-menu">
            <button type="button" onClick={() => setActionMenuOpen((isOpen) => !isOpen)}>Actions ▾</button>
            {actionMenuOpen ? (
              <div className="actions-popover glass">
                <button type="button" onClick={() => openActionModal("new-game")}>Start New Game</button>
                <button type="button" className="danger" onClick={() => openActionModal("ascend")} disabled={!canAscend}>Ascend</button>
                {!canAscend ? <p className="subtle">{ascension?.reason ?? "Ascension is currently locked."}</p> : null}
              </div>
            ) : null}
          </div>
        </div>
      </header>

      {activeError ? <div className="banner error">{(activeError as Error).message}</div> : null}

      {showDevDiagnostics ? (
        <section className="dev-diagnostics glass">
          <strong>Stream Diagnostics</strong>
          <span>mode: {streamConnected ? "live websocket" : "rest fallback"}</span>
          <span>last event: {streamEvent?.at ?? "none"}</span>
          <span>tick: {streamEvent?.tick ?? "-"}</span>
          <span>reconnect: {uiConfig.stream.reconnectDelayMs}ms</span>
          <span>default debounce: {uiConfig.invalidation.defaultDebounceMs}ms</span>
          <span>market debounce: {uiConfig.invalidation.marketStateDebounceMs}ms</span>
          <span>world refresh ticks: {uiConfig.invalidation.worldTickRefreshEveryTicks}</span>
        </section>
      ) : null}

      <main className="grid">
        <section className="card glass control">
          <h2>Run Control</h2>
          <div className="stack">
            <label>
              Queue Behavior
              <select value={selectedBehavior} onChange={(e) => setSelectedBehavior(e.target.value)}>
                {queueBehaviors.length === 0 ? (
                  <option value="">No behaviors currently discovered</option>
                ) : (
                  queueBehaviors.map((behavior) => (
                    <option key={behavior.key} value={behavior.key}>
					  {behavior.key} ({behavior.durationMinutes}m{behavior.staminaCost ? ` · -${behavior.staminaCost} stam` : ""}){behavior.available ? "" : " · prep needed"}
                    </option>
                  ))
                )}
              </select>
            </label>
            <button
              onClick={() => startBehaviorMutation.mutate({
                behaviorKey: selectedBehavior,
                marketWait: selectedRequiresMarketOpen ? marketWait : undefined
              })}
              disabled={startBehaviorMutation.isPending || !selectedBehavior}
            >
              {startBehaviorMutation.isPending ? "Queuing..." : "Queue Behavior"}
            </button>

            {!selectedBehaviorMeta?.available && selectedBehaviorMeta?.unavailableReason ? (
              <p className="subtle">Current prep needed: {selectedBehaviorMeta.unavailableReason}</p>
            ) : null}

            {selectedRequiresMarketOpen ? (
              <label>
                Market Wait Timeout
                <select value={marketWait} onChange={(e) => setMarketWait(e.target.value)}>
                  <option value="6h">6h</option>
                  <option value="12h">12h</option>
                  <option value="1d">1d</option>
                  <option value="2d">2d</option>
                </select>
              </label>
            ) : null}

            <p className="subtle">Run actions (new game/ascend) are in the top-right Actions menu.</p>
          </div>
        </section>

        <section className="card glass player">
          <h2>Player State</h2>
          <div className="kpis">
            <div>
              <span>Name</span>
              <strong>{player?.playerName ?? "No player"}</strong>
            </div>
            <div>
              <span>Ascensions</span>
              <strong>{player?.ascensionCount ?? 0}</strong>
            </div>
            <div>
              <span>Wealth Bonus</span>
              <strong>{player?.wealthBonusPct ?? 0}%</strong>
            </div>
            <div>
              <span>Strength</span>
              <strong>{stats.strength ?? 0}</strong>
            </div>
            <div>
              <span>Social</span>
              <strong>{stats.social ?? 0}</strong>
            </div>
            <div>
              <span>Stamina</span>
              <strong>{stats.stamina ?? 0} / {stats.max_stamina ?? 0}</strong>
            </div>
            <div>
              <span>Recovery/h</span>
              <strong>{stats.stamina_recovery_rate ?? 0}</strong>
            </div>
          </div>
          <h3>Inventory</h3>
          <div className="table-wrap compact">
            <table>
              <thead>
                <tr>
                  <th>Item</th>
                  <th>Qty</th>
                </tr>
              </thead>
              <tbody>
                {inventoryRows.length === 0 ? (
                  <tr>
                    <td colSpan={2}>No items yet.</td>
                  </tr>
                ) : (
                  inventoryRows.map(([item, qty]) => (
                    <tr key={item}>
                      <td>{item}</td>
                      <td>{qty}</td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </section>

        <section className="card glass queue">
          <h2>Behavior Queue & History</h2>
          <div className="table-wrap tall">
            <table>
              <thead>
                <tr>
                  <th>Behavior</th>
                  <th>State</th>
                  <th>Start</th>
                  <th>Target</th>
                  <th>Timeout</th>
                </tr>
              </thead>
              <tbody>
                {behaviors.length === 0 ? (
                  <tr>
                    <td colSpan={5}>No behaviors yet.</td>
                  </tr>
                ) : (
                  behaviors.map((behavior) => (
                    <tr key={behavior.id}>
                      <td>{behavior.key}</td>
                      <td>
                        <div>{behavior.state}</div>
                        {behavior.failureReason ? <div className="subtle">{behavior.failureReason}</div> : null}
                      </td>
                      <td>{formatTickPointLabel(behavior.startedAtTick || behavior.scheduledAtTick, currentTick)}</td>
                      <td>{formatTickPointLabel(behavior.completesAtTick || behavior.scheduledAtTick, currentTick)}</td>
                      <td>
                        {behavior.marketWaitUntilTick
                          ? formatTickPointLabel(behavior.marketWaitUntilTick, currentTick)
                          : "-"}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </section>

        <section className="card glass views">
          <h2>Views</h2>
          <div className="control-tabs">
            <button type="button" className={viewsTab === "progression" ? "active" : ""} onClick={() => setViewsTab("progression")}>
              Progression Tree
            </button>
            <button type="button" className={viewsTab === "market" ? "active" : ""} onClick={() => setViewsTab("market")}>
              Market Ticker
            </button>
            <button type="button" className={viewsTab === "influence" ? "active" : ""} onClick={() => setViewsTab("influence")}>
              Path Influence
            </button>
          </div>

          <div className="views-body">
          {viewsTab === "market" ? (
            <div className="views-tab-panel">
              <p className="subtle">
                Session: <strong>{marketStatus.data?.sessionState ?? "unknown"}</strong>
                {" · "}
                Opens in {marketOpensLabel} · Closes in {marketClosesLabel}
              </p>
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Symbol</th>
                      <th>Price</th>
                      <th>Δ</th>
                      <th>Source</th>
                    </tr>
                  </thead>
                  <tbody>
                    {marketTickers.length === 0 ? (
                      <tr>
                        <td colSpan={4}>No market symbols yet.</td>
                      </tr>
                    ) : (
                      marketTickers.map((ticker) => (
                        <tr key={ticker.symbol}>
                          <td>{ticker.symbol}</td>
                          <td>{ticker.price}</td>
                          <td className={ticker.delta >= 0 ? "up" : "down"}>
                            {ticker.delta >= 0 ? `+${ticker.delta}` : ticker.delta}
                          </td>
                          <td>{ticker.lastSource || "-"}</td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          ) : viewsTab === "progression" ? (
            <div className="views-tab-panel">
              <p className="subtle">Path-focused tree view for planning different prestige lives.</p>
              <div className="progression-paths">
                {progressionPaths.map((path) => (
                  <section key={path.id} className="path-card">
                    <h3>{path.title}</h3>
                    <p className="subtle">{path.description}</p>
                    <div className="table-wrap progression-wrap">
                      <ul className="tree-list">
                        {path.nodes.map((node) => {
                          const unlocks = node.requirements?.unlocks ?? [];
                          const requiredItems = Object.entries(node.requirements?.items ?? {})
                            .map(([itemKey, quantity]) => `${quantity} ${itemKey}`)
                            .join(", ");
                          const grantsUnlocks = (node.grantsUnlocks ?? []).join(", ");
                          return (
                            <li key={node.key} className={`tree-node depth-${Math.min(node.depth, 3)}`}>
                              <div className="tree-title">
                                <strong>{node.key}</strong>
                                <span className={node.available ? "up" : "down"}>{node.available ? "Ready" : node.unavailableReason || "Locked"}</span>
                              </div>
                              <div className="tree-meta">
                                <span>{node.durationMinutes}m</span>
                                {node.staminaCost > 0 ? <span>Stamina: -{node.staminaCost}</span> : null}
                                {unlocks.length > 0 ? <span>Needs unlocks: {unlocks.join(", ")}</span> : null}
                                {requiredItems ? <span>Needs items: {requiredItems}</span> : null}
                                {grantsUnlocks ? <span>Unlocks next: {grantsUnlocks}</span> : null}
                              </div>
                            </li>
                          );
                        })}
                      </ul>
                    </div>
                  </section>
                ))}
              </div>
            </div>
          ) : (
            <div className="views-tab-panel">
              <p className="subtle">Identity trends from what you repeatedly do, not a fixed class selection.</p>
              <div className="influence-list">
                {pathInfluence.map((entry) => (
                  <div key={entry.id} className="influence-row">
                    <span>{entry.label}</span>
                    <div className="influence-meter">
                      <div className="influence-fill" style={{ width: `${entry.percent}%` }} />
                    </div>
                    <strong>{entry.percent}%</strong>
                  </div>
                ))}
              </div>
            </div>
          )}
          </div>
        </section>

        <section className="card glass events">
          <h2>World Feed</h2>
          <ul>
            {events.length === 0 ? <li>Nothing notable yet.</li> : events.map((event) => (
              <li key={event.id}>
                <button type="button" className="event-btn" onClick={() => setSelectedEvent(event)}>
                  [{`Day ${event.day + 1} ${event.clock}`}] {event.message}
                </button>
              </li>
            ))}
          </ul>
        </section>
      </main>

      {selectedEvent ? (
        <div className="event-modal-backdrop" onClick={() => setSelectedEvent(null)}>
          <section className="event-modal glass" onClick={(event) => event.stopPropagation()}>
            <h3>World Event Details</h3>
            <p><strong>When:</strong> Day {selectedEvent.day + 1} at {selectedEvent.clock} (tick {selectedEvent.tick})</p>
            <p><strong>Type:</strong> {selectedEvent.eventType}</p>
            <p><strong>What happened:</strong> {selectedEvent.message}</p>
            <p><strong>Meaning:</strong> {describeEventMeaning(selectedEvent.eventType, selectedEvent.message)}</p>
            <button type="button" onClick={() => setSelectedEvent(null)}>Close</button>
          </section>
        </div>
      ) : null}

      {actionModal ? (
        <div className="event-modal-backdrop" onClick={() => setActionModal(null)}>
          <section className="event-modal glass action-modal" onClick={(event) => event.stopPropagation()}>
            <h3>{actionModal === "new-game" ? "Start New Game" : "Ascend"}</h3>
            <label>
              Name
              <input value={actionNameInput} onChange={(event) => setActionNameInput(event.target.value)} />
            </label>
            {actionModal === "ascend" && !canAscend ? (
              <p className="subtle ascension-locked">{ascension?.reason ?? "Ascension is currently locked."}</p>
            ) : null}
            <div className="modal-actions">
              {player?.hasPrimaryPlayer ? <button type="button" onClick={() => setActionModal(null)}>Cancel</button> : null}
              <button
                type="button"
                className={actionModal === "ascend" ? "danger" : ""}
                disabled={actionModal === "ascend" ? (!canAscend || ascendMutation.isPending) : newGameMutation.isPending}
                onClick={submitActionModal}
              >
                {actionModal === "new-game"
                  ? (newGameMutation.isPending ? "Starting..." : "Start")
                  : (ascendMutation.isPending ? "Ascending..." : "Ascend")}
              </button>
            </div>
          </section>
        </div>
      ) : null}

      <footer className="version-bar">
        <span>API {systemVersion.data?.api ?? "-"}</span>
        <span>Backend {systemVersion.data?.backend ?? "-"}</span>
        <span>Frontend {webPackage.version}</span>
      </footer>
    </div>
  );
}

function getDayPart(minuteOfDay: number) {
  if (minuteOfDay < 300) {
    return "Night";
  }
  if (minuteOfDay < 480) {
    return "Dawn";
  }
  if (minuteOfDay < 720) {
    return "Morning";
  }
  if (minuteOfDay < 1020) {
    return "Afternoon";
  }
  if (minuteOfDay < 1200) {
    return "Evening";
  }
  return "Late Night";
}

function formatDurationMinutesCompact(minutes: number) {
  if (minutes <= 0) {
    return "0m";
  }

  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  if (hours <= 0) {
    return `${remainingMinutes}m`;
  }
  if (remainingMinutes === 0) {
    return `${hours}h`;
  }

  return `${hours}h ${remainingMinutes}m`;
}

function formatOpenCountdown(minutes: number) {
  if (minutes < 60) {
    return `${Math.max(0, minutes)}m`;
  }
  return `${Math.ceil(minutes / 60)}h`;
}

function formatTickPointLabel(tick: number, currentTick: number) {
  if (!tick || tick < 0) {
    return "-";
  }

  const minuteOfDay = ((tick % (24 * 60)) + (24 * 60)) % (24 * 60);
  const day = Math.floor(tick / (24 * 60)) + 1;
  const hours = Math.floor(minuteOfDay / 60);
  const minutes = minuteOfDay % 60;
  const delta = tick - currentTick;
  const relative = delta > 0 ? `in ${formatDurationMinutesCompact(delta)}` : delta < 0 ? `${formatDurationMinutesCompact(Math.abs(delta))} ago` : "now";
  return `Day ${day} ${String(hours).padStart(2, "0")}:${String(minutes).padStart(2, "0")} (${relative})`;
}

type ProgressionPath = {
  id: string;
  title: string;
  description: string;
  nodes: Array<{
    key: string;
    durationMinutes: number;
    staminaCost: number;
    available: boolean;
    unavailableReason?: string;
    requirements?: {
      unlocks?: string[];
      items?: Record<string, number>;
    };
    grantsUnlocks?: string[];
    depth: number;
  }>;
};

function buildProgressionPaths(catalog: Array<{
  key: string;
  durationMinutes: number;
  staminaCost?: number;
  available: boolean;
  unavailableReason?: string;
  requirements?: {
    unlocks?: string[];
    items?: Record<string, number>;
  };
  grantsUnlocks?: string[];
  statDeltas?: Record<string, number>;
}>) {
  const grouped: Record<string, ProgressionPath> = {
    economy: {
      id: "economy",
      title: "Economy Path",
      description: "Trade-focused progression and market-driven growth.",
      nodes: []
    },
    strength: {
      id: "strength",
      title: "Strength Path",
      description: "Physical training path for faster labor outcomes.",
      nodes: []
    },
    social: {
      id: "social",
      title: "Social Path",
      description: "Relationship and negotiation path for market advantages.",
      nodes: []
    }
  };

  for (const behavior of catalog) {
    const pathId = pickPathId(behavior.key, behavior.statDeltas);
    const depth = (behavior.requirements?.unlocks ?? []).length;
    grouped[pathId].nodes.push({
      key: behavior.key,
      durationMinutes: behavior.durationMinutes,
      staminaCost: behavior.staminaCost ?? 0,
      available: behavior.available,
      unavailableReason: behavior.unavailableReason,
      requirements: behavior.requirements,
      grantsUnlocks: behavior.grantsUnlocks,
      depth
    });
  }

  for (const path of Object.values(grouped)) {
    path.nodes.sort((left, right) => {
      if (left.depth !== right.depth) {
        return left.depth - right.depth;
      }
      return left.key.localeCompare(right.key);
    });
  }

  return Object.values(grouped).filter((path) => path.nodes.length > 0);
}

function pickPathId(key: string, statDeltas?: Record<string, number>) {
  if ((statDeltas?.social ?? 0) > 0 || key.includes("social")) {
    return "social";
  }
  if ((statDeltas?.strength ?? 0) > 0 || key.includes("weight") || key.includes("pushup") || key.includes("chop") || key.includes("run")) {
    return "strength";
  }
  return "economy";
}

function describeEventMeaning(eventType: string, message: string) {
  if (eventType === "behavior_started") {
    return "A queued activity is now actively consuming world time."
  }
  if (eventType === "behavior_completed") {
    return "An activity resolved and applied its outputs, unlocks, or stat progress."
  }
  if (eventType === "behavior_failed") {
    return "An activity could not complete, often due to missing requirements or timeout constraints."
  }
  if (eventType.includes("market") || message.toLowerCase().includes("market")) {
    return "A market-related system event shifted prices or session state."
  }
  return "A world simulation update was recorded in the event timeline."
}

function computePathInfluence(
  behaviors: Array<{ key: string; state: string }>,
  stats: Record<string, number>
) {
  const score = {
    economy: 1,
    strength: 1,
    social: 1
  };

  for (const behavior of behaviors) {
    if (behavior.state !== "completed") {
      continue;
    }

    const path = pickPathId(behavior.key);
    score[path] += 4;
  }

  score.strength += Math.max(0, Math.floor((stats.strength ?? 0) / 2));
  score.social += Math.max(0, Math.floor((stats.social ?? 0) / 2));

  const total = score.economy + score.strength + score.social;
  const toPercent = (value: number) => Math.round((value / total) * 100);

  return [
    { id: "economy", label: "Economy", percent: toPercent(score.economy) },
    { id: "strength", label: "Strength", percent: toPercent(score.strength) },
    { id: "social", label: "Social", percent: toPercent(score.social) }
  ];
}
