export type ApiResponse<T> = {
  status: "success" | "error";
  message: string;
  requestId?: string;
  data: T;
};

export type SessionTokens = {
  accessToken: string;
  refreshToken: string;
};

export type AccountRole = "player" | "moderator" | "admin";

export type AccountData = {
  id: number;
  username: string;
  status: string;
  roles: AccountRole[];
};

export type CharacterBrief = {
  id: number;
  playerId: number;
  realmId: number;
  name: string;
  isPrimary: boolean;
  status: string;
};

export type AuthResponseData = SessionTokens & {
  account: AccountData;
};

export type MeData = {
  account: AccountData;
  characters: CharacterBrief[];
};

export type OnboardingStatusData = {
  onboarded: boolean;
  characters: CharacterBrief[];
  defaultRealm: number;
};

export type OnboardingStartData = {
  character: CharacterBrief;
  created: boolean;
};

export type VersionData = {
  api: string;
  backend: string;
  frontend: string;
};

export type BehaviorView = {
  id: number;
  key: string;
  actorType: string;
  actorId: number;
  state: string;
  mode?: "once" | "repeat" | "repeat-until";
  repeatIntervalMinutes?: number;
  repeatUntilTick?: number;
  scheduledAtTick: number;
  startedAtTick: number;
  completesAtTick: number;
  durationMinutes: number;
  marketWaitDurationMinutes?: number;
  marketWaitUntilTick?: number;
  resultMessage: string;
  failureReason: string;
};

export type AscensionEligibility = {
  available: boolean;
  requirementCoins: number;
  currentCoins: number;
  reason: string;
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
  coreStats: Record<string, number>;
  derivedStats: Record<string, number>;
  stats: Record<string, number>;
  behaviors: BehaviorView[];
  ascensionCount: number;
  wealthBonusPct: number;
  ascension: AscensionEligibility;
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

export type BehaviorCatalogEntry = {
  key: string;
  name?: string;
  label?: string;
  summary?: string;
  exclusiveGroup?: string;
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

export type FeedEvent = {
  id: number;
  tick: number;
  day: number;
  minuteOfDay: number;
  clock: string;
  message: string;
  eventType: string;
};

export type ChatChannel = {
  key: string;
  name: string;
  subject?: string;
  description: string;
  scope?: string;
  scopeKey?: string;
};

export type ChatMessage = {
  id: number;
  realmId: number;
  channel: string;
  messageClass: "player" | "moderator" | "admin" | "system";
  authorRole: string;
  authorBadges: string[];
  censored: boolean;
  censorHits: number;
  tick: number;
  day: number;
  minuteOfDay: number;
  clock: string;
  author: string;
  message: string;
};

export type ChatMessagesData = {
  realmId: number;
  channel: string;
  scope?: string;
  scopeKey?: string;
  messages: ChatMessage[];
};

export type AdminRealm = {
  realmId: number;
  activeCharacters: number;
};

export type AdminWordRule = {
  id: number;
  term: string;
  matchMode: string;
  reasonCode: string;
  updatedAt: string;
};

export type AdminChatChannelBinding = {
  scope: "realm";
  scopeKey: string;
  realmId: number;
  key: string;
};

export type AdminChatPolicyBinding = {
  policyScope: "global";
  policyScopeKey: string;
  scope: string;
};

export type AdminAuditEntry = {
  id: number;
  realmId: number;
  actorAccountId: number;
  actorUsername: string;
  actionKey: string;
  reasonCode: string;
  note: string;
  occurredTick: number;
  createdAt: string;
  updatedAt: string;
  before?: unknown;
  after?: unknown;
};

export type AdminCharacterEntry = {
  id: number;
  accountId: number;
  accountUsername: string;
  playerId: number;
  realmId: number;
  name: string;
  isPrimary: boolean;
  status: string;
  updatedAt: string;
};

type StoredSession = SessionTokens & {
  account: AccountData;
};

const storageKey = "lived.session";
const sessionChangedEventName = "lived:session-changed";
let inMemorySession: StoredSession | null = null;
let refreshInFlight: Promise<boolean> | null = null;

function notifySessionChanged(): void {
  if (typeof window !== "undefined") {
    window.dispatchEvent(new Event(sessionChangedEventName));
  }
}

export function subscribeSessionChanges(listener: () => void): () => void {
  if (typeof window === "undefined") {
    return () => {
      // no-op for non-browser contexts
    };
  }

  window.addEventListener(sessionChangedEventName, listener);
  return () => {
    window.removeEventListener(sessionChangedEventName, listener);
  };
}

function loadSessionFromStorage(): StoredSession | null {
  if (typeof window === "undefined") {
    return null;
  }

  const raw = window.localStorage.getItem(storageKey);
  if (!raw) {
    return null;
  }

  try {
    const parsed = JSON.parse(raw) as StoredSession;
    if (!parsed.accessToken || !parsed.refreshToken || !parsed.account) {
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

function saveSessionToStorage(session: StoredSession | null): void {
  if (typeof window === "undefined") {
    return;
  }

  if (!session) {
    window.localStorage.removeItem(storageKey);
    return;
  }

  window.localStorage.setItem(storageKey, JSON.stringify(session));
}

export function getSession(): StoredSession | null {
  if (inMemorySession) {
    return inMemorySession;
  }
  inMemorySession = loadSessionFromStorage();
  return inMemorySession;
}

export function setSession(tokens: SessionTokens, account: AccountData): void {
  inMemorySession = {
    accessToken: tokens.accessToken,
    refreshToken: tokens.refreshToken,
    account
  };
  saveSessionToStorage(inMemorySession);
  notifySessionChanged();
}

export function clearSession(): void {
  inMemorySession = null;
  saveSessionToStorage(null);
  notifySessionChanged();
}

function withQuery(path: string, query?: Record<string, string | number | undefined>): string {
  if (!query) {
    return path;
  }
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined) {
      continue;
    }
    params.set(key, String(value));
  }
  const queryString = params.toString();
  return queryString ? `${path}?${queryString}` : path;
}

async function request<T>(
  path: string,
  init?: RequestInit,
  options?: { auth?: boolean; retryOnAuth?: boolean }
): Promise<T> {
  const requireAuth = options?.auth ?? false;
  const retryOnAuth = options?.retryOnAuth ?? true;
  const session = getSession();

  if (requireAuth && !session?.accessToken) {
    clearSession();
    throw new Error("Not authenticated. Please login again.");
  }

  const headers = new Headers(init?.headers ?? {});
  if (!headers.has("Content-Type") && init?.body) {
    headers.set("Content-Type", "application/json");
  }
  if (requireAuth && session?.accessToken) {
    headers.set("Authorization", `Bearer ${session.accessToken}`);
  }

  const response = await fetch(path, { ...init, headers });

  if (response.status === 401 && requireAuth && retryOnAuth) {
    const refreshToken = getSession()?.refreshToken ?? session?.refreshToken;
    if (refreshToken) {
      const refreshed = await refreshSessionInternal(refreshToken);
      if (refreshed) {
        return request<T>(path, init, { auth: requireAuth, retryOnAuth: false });
      }
    }
    clearSession();
  }

  let body: ApiResponse<T> | null = null;
  const contentType = response.headers.get("content-type") ?? "";
  if (contentType.includes("application/json")) {
    body = (await response.json()) as ApiResponse<T>;
  }

  if (!response.ok) {
    throw new Error(body?.message ?? `Request failed with ${response.status}`);
  }

  if (!body) {
    throw new Error("Unexpected non-JSON response");
  }

  if (body.status === "error") {
    throw new Error(body.message || "Request returned error status");
  }

  return body.data;
}

async function refreshSessionInternal(refreshToken: string): Promise<boolean> {
  if (refreshInFlight) {
    return refreshInFlight;
  }

  refreshInFlight = (async () => {
    try {
      const refreshed = await request<AuthResponseData>(
        "/v1/auth/refresh",
        {
          method: "POST",
          body: JSON.stringify({ refreshToken })
        },
        { auth: false, retryOnAuth: false }
      );
      setSession(
        { accessToken: refreshed.accessToken, refreshToken: refreshed.refreshToken },
        refreshed.account
      );
      return true;
    } catch {
      return false;
    }
  })();

  try {
    return await refreshInFlight;
  } finally {
    refreshInFlight = null;
  }
}

export async function register(username: string, password: string): Promise<AuthResponseData> {
  const data = await request<AuthResponseData>("/v1/auth/register", {
    method: "POST",
    body: JSON.stringify({ username, password })
  });
  setSession({ accessToken: data.accessToken, refreshToken: data.refreshToken }, data.account);
  return data;
}

export async function login(username: string, password: string): Promise<AuthResponseData> {
  const data = await request<AuthResponseData>("/v1/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password })
  });
  setSession({ accessToken: data.accessToken, refreshToken: data.refreshToken }, data.account);
  return data;
}

export async function refreshSession(): Promise<boolean> {
  const session = getSession();
  if (!session?.refreshToken) {
    return false;
  }
  return refreshSessionInternal(session.refreshToken);
}

export async function logout(): Promise<void> {
  try {
    await request<unknown>("/v1/auth/logout", { method: "POST" }, { auth: true });
  } finally {
    clearSession();
  }
}

export async function getMe(): Promise<MeData> {
  const data = await request<MeData>("/v1/auth/me", { method: "GET" }, { auth: true });
  const session = getSession();
  if (session) {
    setSession({ accessToken: session.accessToken, refreshToken: session.refreshToken }, data.account);
  }
  return data;
}

export async function getOnboardingStatus(): Promise<OnboardingStatusData> {
  return request<OnboardingStatusData>("/v1/onboarding/status", { method: "GET" }, { auth: true });
}

export async function startOnboarding(name: string, realmId: number): Promise<OnboardingStartData> {
  return request<OnboardingStartData>(
    "/v1/onboarding/start",
    {
      method: "POST",
      body: JSON.stringify({ name, realmId })
    },
    { auth: true }
  );
}

export async function getPlayerStatus(characterId?: number): Promise<PlayerStatusData> {
  return request<PlayerStatusData>(withQuery("/v1/player/status", { characterId }), { method: "GET" }, { auth: true });
}

export async function getPlayerInventory(characterId?: number): Promise<PlayerInventoryData> {
  return request<PlayerInventoryData>(withQuery("/v1/player/inventory", { characterId }), { method: "GET" }, { auth: true });
}

export async function getPlayerBehaviors(characterId?: number): Promise<PlayerBehaviorsData> {
  return request<PlayerBehaviorsData>(withQuery("/v1/player/behaviors", { characterId }), { method: "GET" }, { auth: true });
}

export async function getBehaviorCatalog(characterId?: number): Promise<BehaviorCatalogEntry[]> {
  const data = await request<{ behaviors: BehaviorCatalogEntry[] }>(
    withQuery("/v1/system/behaviors/catalog", { characterId }),
    { method: "GET" },
    { auth: true }
  );
  return data.behaviors;
}

export async function startBehavior(
  behaviorKey: string,
  characterId?: number,
  marketWait?: string,
  mode?: "once" | "repeat" | "repeat-until",
  repeatUntil?: string
): Promise<unknown> {
  return request<unknown>(
    withQuery("/v1/system/behaviors/start", { characterId }),
    {
      method: "POST",
      body: JSON.stringify({ behaviorKey, marketWait, mode, repeatUntil })
    },
    { auth: true }
  );
}

export async function getMarketStatus(realmId?: number, characterId?: number): Promise<MarketStatus> {
  return request<MarketStatus>(withQuery("/v1/system/market/status", { realmId, characterId }), { method: "GET" }, { auth: true });
}

export async function ascend(name?: string, characterId?: number): Promise<{ ascensionCount: number; wealthBonusPct: number }> {
  return request<{ ascensionCount: number; wealthBonusPct: number }>(
    withQuery("/v1/system/ascend", { characterId }),
    {
      method: "POST",
      body: JSON.stringify({ name })
    },
    { auth: true }
  );
}

export async function getSystemVersion(): Promise<VersionData> {
  return request<VersionData>("/v1/system/version", { method: "GET" });
}

export async function getFeedPublic(characterId?: number, limit: number = 50): Promise<{ events: FeedEvent[] }> {
  return request<{ events: FeedEvent[] }>(
    withQuery("/v1/feed/public", { characterId, limit }),
    { method: "GET" },
    { auth: true }
  );
}

export async function getChatChannels(characterId?: number): Promise<{ realmId: number; channels: ChatChannel[] }> {
  return request<{ realmId: number; channels: ChatChannel[] }>(withQuery("/v1/chat/channels", { characterId }), { method: "GET" }, { auth: true });
}

export async function getChatMessages(channel: string, characterId?: number, limit: number = 100): Promise<ChatMessagesData> {
  return request<ChatMessagesData>(
    withQuery("/v1/chat/messages", { channel, characterId, limit }),
    { method: "GET" },
    { auth: true }
  );
}

export async function postChatMessage(message: string, channel: string, characterId?: number): Promise<ChatMessage> {
  return request<ChatMessage>(
    withQuery("/v1/chat/messages", { characterId }),
    {
      method: "POST",
      headers: { "Idempotency-Key": `${Date.now()}-${Math.random()}` },
      body: JSON.stringify({ message, channel })
    },
    { auth: true }
  );
}

export async function adminGetRealms(): Promise<{ realms: AdminRealm[] }> {
  return request<{ realms: AdminRealm[] }>("/v1/admin/realms", { method: "GET" }, { auth: true });
}

export async function adminGetStats(windowTicks?: number): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>(withQuery("/v1/admin/stats", { windowTicks }), { method: "GET" }, { auth: true });
}

export async function adminApplyRealmAction(
  realmId: number,
  payload: { action: string; reasonCode: string; note?: string; itemKey?: string; price?: number }
): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>(
    `/v1/admin/realms/${realmId}/actions`,
    {
      method: "POST",
      body: JSON.stringify(payload)
    },
    { auth: true }
  );
}

export async function adminModerateAccount(accountId: number, route: "lock" | "unlock", reasonCode: string, note?: string): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>(
    `/v1/admin/moderation/accounts/${accountId}/${route}`,
    {
      method: "POST",
      body: JSON.stringify({ reasonCode, note })
    },
    { auth: true }
  );
}

export async function adminSetAccountStatus(
  accountId: number,
  payload: { status: "active" | "locked"; reasonCode: string; note?: string; revokeSessions?: boolean }
): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>(
    `/v1/admin/moderation/accounts/${accountId}/status`,
    {
      method: "POST",
      body: JSON.stringify(payload)
    },
    { auth: true }
  );
}

export async function adminSetAccountRole(
  accountId: number,
  payload: { roleKey: string; action: "grant" | "revoke"; reasonCode: string; note?: string }
): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>(
    `/v1/admin/moderation/accounts/${accountId}/roles`,
    {
      method: "POST",
      body: JSON.stringify(payload)
    },
    { auth: true }
  );
}

export async function adminListCharacters(filters: {
  accountId?: number;
  accountUsername?: string;
  realmId?: number;
  status?: "active" | "locked";
  nameLike?: string;
  beforeId?: number;
  limit?: number;
}): Promise<{ entries: AdminCharacterEntry[]; pagination: { hasMore: boolean; nextBeforeId: number } }> {
  return request<{ entries: AdminCharacterEntry[]; pagination: { hasMore: boolean; nextBeforeId: number } }>(
    withQuery("/v1/admin/moderation/characters", {
      accountId: filters.accountId,
      accountUsername: filters.accountUsername,
      realmId: filters.realmId,
      status: filters.status,
      nameLike: filters.nameLike,
      beforeId: filters.beforeId,
      limit: filters.limit
    }),
    { method: "GET" },
    { auth: true }
  );
}

export async function adminModerateCharacter(
  characterId: number,
  payload: { name?: string; status?: "active" | "locked"; isPrimary?: boolean; reasonCode: string; note?: string }
): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>(
    `/v1/admin/moderation/characters/${characterId}`,
    {
      method: "POST",
      body: JSON.stringify(payload)
    },
    { auth: true }
  );
}

export async function adminChatUpsertChannel(payload: { scope?: "realm"; realmId?: number; key: string; name: string; subject?: string; description?: string }): Promise<AdminChatChannelBinding & { name: string; subject?: string; created: boolean }> {
  return request<AdminChatChannelBinding & { name: string; subject?: string; created: boolean }>(
    "/v1/admin/chat/channels",
    {
      method: "POST",
      body: JSON.stringify(payload)
    },
    { auth: true }
  );
}

export async function adminChatDisableChannel(key: string, options?: { scope?: "realm"; realmId?: number }): Promise<AdminChatChannelBinding> {
  return request<AdminChatChannelBinding>(
    withQuery(`/v1/admin/chat/channels/${encodeURIComponent(key)}`, {
      scope: options?.scope,
      realmId: options?.realmId
    }),
    { method: "DELETE" },
    { auth: true }
  );
}

export async function adminChatFlushChannel(key: string, payload: { scope?: "realm"; realmId?: number; reasonCode: string; note?: string }): Promise<AdminChatChannelBinding & { deleted: number }> {
  return request<AdminChatChannelBinding & { deleted: number }>(
    `/v1/admin/chat/channels/${encodeURIComponent(key)}/flush`,
    {
      method: "POST",
      body: JSON.stringify(payload)
    },
    { auth: true }
  );
}

export async function adminChatModerateChannel(
  key: string,
  payload: { scope?: "realm"; realmId?: number; accountId: number; action: "ban" | "unban" | "kick"; durationMinutes?: number; reasonCode: string; note?: string }
): Promise<AdminChatChannelBinding & { accountId: number; action: "ban" | "unban" | "kick"; expiresAt?: string }> {
  return request<AdminChatChannelBinding & { accountId: number; action: "ban" | "unban" | "kick"; expiresAt?: string }>(
    `/v1/admin/chat/channels/${encodeURIComponent(key)}/moderation`,
    {
      method: "POST",
      body: JSON.stringify(payload)
    },
    { auth: true }
  );
}

export async function adminChatSystemMessage(
  key: string,
  payload: { scope?: "realm"; realmId?: number; message: string; reasonCode: string; note?: string }
): Promise<AdminChatChannelBinding & { channel: string; tick: number; id: number }> {
  return request<AdminChatChannelBinding & { channel: string; tick: number; id: number }>(
    `/v1/admin/chat/channels/${encodeURIComponent(key)}/system-message`,
    {
      method: "POST",
      body: JSON.stringify(payload)
    },
    { auth: true }
  );
}

export async function adminChatWordlistList(): Promise<AdminChatPolicyBinding & { rules: AdminWordRule[] }> {
  return request<AdminChatPolicyBinding & { rules: AdminWordRule[] }>("/v1/admin/chat/wordlist", { method: "GET" }, { auth: true });
}

export async function adminChatWordlistAdd(payload: { scope?: "global"; term: string; matchMode?: "contains"; reasonCode: string; note?: string }): Promise<AdminChatPolicyBinding & { id: number; term: string; matchMode: "contains"; created: boolean }> {
  return request<AdminChatPolicyBinding & { id: number; term: string; matchMode: "contains"; created: boolean }>(
    "/v1/admin/chat/wordlist",
    {
      method: "POST",
      body: JSON.stringify(payload)
    },
    { auth: true }
  );
}

export async function adminChatWordlistRemove(ruleId: number): Promise<AdminChatPolicyBinding & { ruleId: number }> {
  return request<AdminChatPolicyBinding & { ruleId: number }>(`/v1/admin/chat/wordlist/${ruleId}`, { method: "DELETE" }, { auth: true });
}

export async function adminAuditList(filters: {
  realmId?: number;
  actorAccountId?: number;
  actorUsername?: string;
  actionKey?: string;
  beforeId?: number;
  includeRawJson?: boolean;
  limit?: number;
}): Promise<{ entries: AdminAuditEntry[]; pagination: { hasMore: boolean; nextBeforeId: number } }> {
  return request<{ entries: AdminAuditEntry[]; pagination: { hasMore: boolean; nextBeforeId: number } }>(
    withQuery("/v1/admin/audit", {
      realmId: filters.realmId,
      actorAccountId: filters.actorAccountId,
      actorUsername: filters.actorUsername,
      actionKey: filters.actionKey,
      beforeId: filters.beforeId,
      includeRawJson: filters.includeRawJson ? "true" : undefined,
      limit: filters.limit
    }),
    { method: "GET" },
    { auth: true }
  );
}
