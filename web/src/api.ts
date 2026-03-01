export type ApiResponse<T> = {
  status: "success" | "error";
  message: string;
  requestId?: string;
  data: T;
};

export type BehaviorView = {
  id: number;
  key: string;
  actorType: string;
  actorId: number;
  state: string;
  scheduledAtTick: number;
  startedAtTick: number;
  completesAtTick: number;
  durationMinutes: number;
  marketWaitDurationMinutes?: number;
  marketWaitUntilTick?: number;
  resultMessage: string;
  failureReason: string;
};

export type RecentEvent = {
  id: number;
  tick: number;
  day: number;
  minuteOfDay: number;
  clock: string;
  message: string;
  eventType: string;
};

export type PlayerStatusData = {
  version: VersionData;
  save: string;
  players: string[];
  simulationTick: number;
  worldAgeMinutes: number;
  worldAgeHours: number;
  worldAgeDays: number;
  hasPrimaryPlayer: boolean;
  playerName?: string;
  inventory: Record<string, number>;
  stats: Record<string, number>;
  behaviors: BehaviorView[];
  ascensionCount: number;
  wealthBonusPct: number;
  ascension: AscensionEligibility;
};

export type VersionData = {
  api: string;
  backend: string;
  frontend: string;
};

export type AscensionEligibility = {
  available: boolean;
  requirementCoins: number;
  currentCoins: number;
  reason: string;
};

export type PlayerInventoryData = {
  hasPrimaryPlayer: boolean;
  playerName?: string;
  simulationTick: number;
  inventory: Record<string, number>;
};

export type PlayerBehaviorsData = {
  hasPrimaryPlayer: boolean;
  playerName?: string;
  simulationTick: number;
  behaviors: BehaviorView[];
};

export type MarketStatus = {
  tick: number;
  day: number;
  minuteOfDay: number;
  isOpen: boolean;
  sessionState: string;
  minutesToOpen: number;
  minutesToClose: number;
  tickers: Array<{
    symbol: string;
    price: number;
    delta: number;
    lastSource: string;
    updatedTick: number;
    sessionState: string;
  }>;
};

export type WorldStatus = {
  version?: VersionData;
  recentEvents: RecentEvent[];
};

export type BehaviorCatalogEntry = {
  key: string;
  durationMinutes: number;
  staminaCost?: number;
  available: boolean;
  queueVisible?: boolean;
  unavailableReason?: string;
  requirements?: {
    unlocks?: string[];
    items?: Record<string, number>;
  };
  costs?: Record<string, number>;
  statDeltas?: Record<string, number>;
  grantsUnlocks?: string[];
  requiresMarketOpen?: boolean;
  marketWaitDefaultMinutes?: number;
  marketWaitMaxMinutes?: number;
};

export type SystemVersion = VersionData;

export type WorldStreamPlayer = {
  name: string;
  coins: number;
  ascensionCount: number;
  wealthBonusPct: number;
  queuedOrActiveBehaviors: number;
  ascensionReady: boolean;
  ascensionReason: string;
};

export type WorldStreamEvent = {
  type: string;
  at: string;
  tick: number;
  day: number;
  minuteOfDay: number;
  clock: string;
  dayPart: string;
  marketOpen: boolean;
  marketState: string;
  player?: WorldStreamPlayer;
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: {
      "Content-Type": "application/json"
    },
    ...init
  });

  let body: ApiResponse<T>;
  try {
    body = (await response.json()) as ApiResponse<T>;
  } catch {
    throw new Error(`Unexpected response from ${path}`);
  }

  if (!response.ok || body.status === "error") {
    throw new Error(body.message || `Request failed: ${path}`);
  }

  return body.data;
}

export async function getPlayerStatus() {
  return request<PlayerStatusData>("/v1/player/status");
}

export async function getPlayerInventory() {
  return request<PlayerInventoryData>("/v1/player/inventory");
}

export async function getPlayerBehaviors() {
  return request<PlayerBehaviorsData>("/v1/player/behaviors");
}

export async function getMarketStatus() {
  return request<MarketStatus>("/v1/system/market/status");
}

export async function getWorldStatus() {
  return request<WorldStatus>("/v1/system/status");
}

export async function getBehaviorCatalog() {
  const data = await request<{ behaviors: BehaviorCatalogEntry[] }>("/v1/system/behaviors/catalog");
  return data.behaviors;
}

export async function startBehavior(behaviorKey: string, marketWait?: string) {
  return request<{ behaviorKey: string; player: string }>("/v1/system/behaviors/start", {
    method: "POST",
    body: JSON.stringify({ behaviorKey, marketWait })
  });
}

export async function getSystemVersion() {
  return request<SystemVersion>("/v1/system/version");
}

export async function newGame(name: string) {
  return request<unknown>("/v1/system/new", {
    method: "POST",
    body: JSON.stringify({ name })
  });
}

export async function ascend(name?: string) {
  return request<{ ascensionCount: number; wealthBonusPct: number }>("/v1/system/ascend", {
    method: "POST",
    body: JSON.stringify({ name })
  });
}
