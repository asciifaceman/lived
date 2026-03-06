import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AccountData,
  BehaviorCatalogEntry,
  BehaviorView,
  ChatChannel,
  ChatMessage,
  FeedEvent,
  MarketCandleEntry,
  MarketOverviewSymbol,
  MarketOrderBook,
  MarketOrderView,
  MarketStatus,
  MarketTradeView,
  MeData,
  OnboardingStatusData,
  PlayerInventoryData,
  PlayerStatusData,
  UpgradeCatalogEntry,
  ascend,
  cancelBehavior,
  cancelMarketOrder,
  clearSession,
  getBehaviorCatalog,
  getChatChannels,
  getChatMessages,
  getFeedPublic,
  getMarketCandles,
  getMarketOverview,
  getMarketOrderBook,
  getMarketStatus,
  getMyMarketOrders,
  getRecentMarketTrades,
  getMe,
  getOnboardingStatus,
  getUpgradeCatalog,
  getPlayerBehaviors,
  getPlayerInventory,
  getPlayerStatus,
  getSession,
  getSystemVersion,
  login,
  logout,
  purchaseUpgrade,
  postChatMessage,
  placeMarketOrder,
  refreshSession,
  register,
  setSession,
  subscribeSessionChanges,
  startBehavior,
  startOnboarding,
  switchOnboardingCharacter
} from "./api";
import { AdminModal } from "./components/AdminModal";
import { ChatView } from "./components/ChatView";
import { PlayerSnapshot } from "./components/PlayerSnapshot";
import { useWorldStream } from "./hooks/useWorldStream";
import webPackage from "../package.json";

type AppView = "profile" | "gameplay" | "chat";
type GameplayTab = "overview" | "queue" | "progression" | "market" | "market-overview" | "inventory" | "feed";
type AuthMode = "login" | "register";

type Tokens = {
  accessToken: string;
  refreshToken: string;
};

type ToastType = "info" | "success" | "error";

type ToastItem = {
  id: number;
  type: ToastType;
  message: string;
};

const gameplayRefreshMs = 4500;
const chatRefreshActiveMs = 2500;
// Keep lightweight background polling so future mention notifications can surface even off-chat view.
const chatRefreshBackgroundMs = 30000;
const preferredCharacterStorageKey = "lived.preferred-character-id";

function parseNumber(value: string): number | undefined {
  const trimmed = value.trim();
  if (!trimmed) {
    return undefined;
  }
  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed)) {
    return undefined;
  }
  return parsed;
}

function loadPreferredCharacterID(): number | undefined {
  if (typeof window === "undefined") {
    return undefined;
  }

  const raw = window.localStorage.getItem(preferredCharacterStorageKey);
  if (!raw) {
    return undefined;
  }

  const parsed = Number(raw);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    return undefined;
  }

  return parsed;
}

function savePreferredCharacterID(characterID: number | undefined): void {
  if (typeof window === "undefined") {
    return;
  }

  if (!characterID || characterID <= 0) {
    window.localStorage.removeItem(preferredCharacterStorageKey);
    return;
  }

  window.localStorage.setItem(preferredCharacterStorageKey, String(characterID));
}

function statValue(stats: Record<string, number>, keys: string[], fallback: number = 0): number {
  for (const key of keys) {
    if (key in stats && Number.isFinite(stats[key])) {
      return stats[key];
    }
  }
  return fallback;
}

type StatDisplayRow = {
  id: string;
  label: string;
  value?: number;
  max?: number;
};

type StatCategory = "core" | "derived";

function normalizeStatKey(value: string): string {
  return value.replace(/[_\s-]/g, "").toLowerCase();
}

function titleCaseStatLabel(value: string): string {
  const withSpaces = value
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim();
  if (!withSpaces) {
    return "Stat";
  }
  return withSpaces
    .split(/\s+/)
    .map((token) => token.charAt(0).toUpperCase() + token.slice(1))
    .join(" ");
}

function splitStatMaxKey(key: string): { isMax: boolean; baseKey: string } {
  if (key.startsWith("max") && key.length > 3) {
    const candidate = key.slice(3);
    const normalizedCandidate = candidate.charAt(0).toLowerCase() + candidate.slice(1);
    return { isMax: true, baseKey: normalizedCandidate };
  }
  if (key.endsWith("Max") && key.length > 3) {
    return { isMax: true, baseKey: key.slice(0, -3) };
  }
  if (key.startsWith("max_") && key.length > 4) {
    return { isMax: true, baseKey: key.slice(4) };
  }
  return { isMax: false, baseKey: key };
}

function buildStatDisplayRows(stats: Record<string, number> | undefined): StatDisplayRow[] {
  const rowsByKey = new Map<string, StatDisplayRow>();
  const baseKeyByNormalized = new Map<string, string>();

  for (const key of Object.keys(stats ?? {})) {
    const split = splitStatMaxKey(key);
    if (split.isMax) {
      continue;
    }
    baseKeyByNormalized.set(normalizeStatKey(split.baseKey), split.baseKey);
  }

  for (const [key, value] of Object.entries(stats ?? {})) {
    const split = splitStatMaxKey(key);
    const normalizedBase = normalizeStatKey(split.baseKey);
    const canonicalBaseKey = baseKeyByNormalized.get(normalizedBase) ?? split.baseKey;
    const id = normalizeStatKey(canonicalBaseKey || split.baseKey || key);
    const label = titleCaseStatLabel(canonicalBaseKey || split.baseKey || key);
    const row = rowsByKey.get(id) ?? { id, label };
    if (split.isMax) {
      row.max = value;
    } else {
      row.value = value;
    }
    rowsByKey.set(id, row);
  }

  return Array.from(rowsByKey.values()).sort((left, right) => left.label.localeCompare(right.label));
}

function statContextHint(id: string, category: StatCategory): string | null {
  if (category === "core") {
    if (id === "strength") {
      return "Trainable. Improves physical output-focused actions.";
    }
    if (id === "social") {
      return "Trainable. Improves market/social interaction outcomes.";
    }
    if (id === "financial") {
      return "Trainable. Contributes directly to trading aptitude and market decision quality.";
    }
    if (id === "endurance") {
      return "Trainable. Feeds stamina cap and stamina recovery formulas.";
    }
    return "Trainable attribute.";
  }

  if (id === "stamina") {
    return "Current resource spent by actions; replenishes over time.";
  }
  if (id === "maxstamina") {
    return "Derived: 100 + endurance × 3.";
  }
  if (id === "staminarecoveryrate") {
    return "Derived: 8 + floor(endurance / 4) per hour.";
  }
  if (id === "tradingaptitude") {
    return "Derived: social + financial.";
  }
  return "Derived from attributes and runtime state.";
}

function formatWorldTime(tick: number | undefined): string {
  if (tick === undefined || tick <= 0) {
    return "-";
  }
  const day = Math.floor(tick / (24 * 60));
  const minuteOfDay = ((tick % (24 * 60)) + (24 * 60)) % (24 * 60);
  const hour = Math.floor(minuteOfDay / 60);
  const minute = minuteOfDay % 60;
  return `Day ${day} ${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`;
}

function formatRemainingMinutes(currentTick: number | undefined, targetTick: number | undefined): string {
  if (currentTick === undefined || targetTick === undefined || targetTick <= 0) {
    return "-";
  }

  const remaining = Math.max(0, targetTick - currentTick);
  if (remaining < 60) {
    return `${remaining}m`;
  }

  const hours = Math.floor(remaining / 60);
  const minutes = remaining % 60;
  if (minutes === 0) {
    return `${hours}h`;
  }
  return `${hours}h ${minutes}m`;
}

function formatMarketCountdown(minutes: number | undefined): string {
  if (minutes === undefined || !Number.isFinite(minutes)) {
    return "-";
  }

  const safe = Math.max(0, Math.floor(minutes));
  if (safe <= 60) {
    return `${safe}m`;
  }

  const hours = safe / 60;
  const roundedHours = Math.round(hours * 10) / 10;
  return `${roundedHours}h`;
}

function formatSignedNumber(value: number | undefined): string {
  if (value === undefined || !Number.isFinite(value)) {
    return "-";
  }
  if (value > 0) {
    return `+${value}`;
  }
  return String(value);
}

function formatPercent(value: number | undefined): string {
  if (value === undefined || !Number.isFinite(value)) {
    return "-";
  }
  return `${value.toFixed(1)}%`;
}

function marketRegimeLabel(windowChange: number | undefined, utilizationPct: number | undefined): string {
  const change = windowChange ?? 0;
  const utilization = utilizationPct ?? 50;

  if (change >= 6 && utilization <= 55) {
    return "Bull Expansion";
  }
  if (change <= -6 && utilization >= 45) {
    return "Bear Drawdown";
  }
  if (Math.abs(change) <= 2 && utilization >= 40 && utilization <= 60) {
    return "Range / Balanced";
  }
  if (change > 0) {
    return "Uptrend";
  }
  if (change < 0) {
    return "Downtrend";
  }
  return "Neutral";
}

function candleWindowPoints(rangeKey: string, bucketTicks: number): number {
  const safeBucket = Math.max(1, bucketTicks || 30);
  let targetTicks = 24 * 60;
  switch (rangeKey) {
    case "12h":
      targetTicks = 12 * 60;
      break;
    case "3d":
      targetTicks = 3 * 24 * 60;
      break;
    case "7d":
      targetTicks = 7 * 24 * 60;
      break;
    default:
      targetTicks = 24 * 60;
      break;
  }
  return Math.min(500, Math.max(24, Math.ceil(targetTicks / safeBucket)));
}

function pricePathFromSeries(prices: number[]): string {
  if (prices.length <= 1) {
    return "";
  }

  const width = 100;
  const height = 36;
  const minPrice = Math.min(...prices);
  const maxPrice = Math.max(...prices);
  const priceSpan = Math.max(1, maxPrice - minPrice);

  const points = prices.map((price, index) => {
    const x = (index / (prices.length - 1)) * width;
    const normalized = (price - minPrice) / priceSpan;
    const y = height - normalized * height;
    return `${x.toFixed(2)},${y.toFixed(2)}`;
  });

  return points.join(" ");
}

function candleCloseSeriesPath(candles: MarketCandleEntry[]): string {
  if (candles.length <= 1) {
    return "";
  }
  return pricePathFromSeries(candles.map((entry) => entry.close));
}

type LinePoint = {
  x: number;
  y: number;
  price: number;
  tick: number;
};

function linePointsFromCandles(candles: MarketCandleEntry[], minPrice: number, maxPrice: number): LinePoint[] {
  if (candles.length <= 0) {
    return [];
  }

  const width = 100;
  const height = 44;
  const span = Math.max(1, maxPrice - minPrice);
  const denominator = Math.max(1, candles.length - 1);

  return candles.map((entry, index) => {
    const x = (index / denominator) * width;
    const normalized = (entry.close - minPrice) / span;
    const y = height - normalized * height;
    return { x, y, price: entry.close, tick: entry.bucketStartTick };
  });
}

function linePathFromPoints(points: LinePoint[]): string {
  if (points.length <= 1) {
    return "";
  }
  return points.map((point) => `${point.x.toFixed(2)},${point.y.toFixed(2)}`).join(" ");
}

function behaviorProgressPct(currentTick: number | undefined, behavior: BehaviorView): number {
  if (behavior.state !== "active") {
    return 0;
  }

  const fallbackStart = behavior.completesAtTick - Math.max(1, behavior.durationMinutes);
  const startedAtTick = behavior.startedAtTick > 0 ? behavior.startedAtTick : fallbackStart;
  const totalDuration = Math.max(1, behavior.completesAtTick - startedAtTick);
  const elapsed = currentTick !== undefined ? currentTick - startedAtTick : 0;
  return Math.round(clampPercent((elapsed / totalDuration) * 100));
}

function queueSortOrder(state: string): number {
  if (state === "active") {
    return 0;
  }
  if (state === "queued") {
    return 1;
  }
  if (state === "failed") {
    return 2;
  }
  if (state === "cancelled") {
    return 3;
  }
  if (state === "completed") {
    return 4;
  }
  return 9;
}

function compactResultMessage(behavior: BehaviorView): string {
  if (behavior.state === "cancelled") {
    return behavior.resultMessage || "Cancelled by player.";
  }
  if (behavior.state === "failed") {
    return behavior.failureReason || "Behavior failed.";
  }
  return behavior.resultMessage || "Behavior completed.";
}

function formatBehaviorMode(mode: BehaviorView["mode"] | undefined): string {
  if (!mode || mode === "once") {
    return "Once";
  }
  if (mode === "repeat") {
    return "Repeat";
  }
  return "Repeat Until";
}

function formatBehaviorSchedule(behavior: BehaviorView): string {
  const mode = behavior.mode ?? "once";
  if (mode === "repeat-until" && behavior.repeatUntilTick && behavior.repeatUntilTick > 0) {
    return `${formatBehaviorMode(mode)} (${formatWorldTime(behavior.repeatUntilTick)})`;
  }
  return formatBehaviorMode(mode);
}

function behaviorDisplayLabel(entry: BehaviorCatalogEntry | undefined, key: string): string {
  const directLabel = entry?.label?.trim();
  if (directLabel) {
    return directLabel;
  }
  const directName = entry?.name?.trim();
  if (directName) {
    return directName;
  }
  return titleCaseStatLabel(key);
}

function behaviorCategory(entry: BehaviorCatalogEntry | undefined, key: string): string {
  const explicit = entry?.category?.trim();
  if (explicit) {
    return explicit;
  }

  if (entry?.requiresMarketOpen || key.startsWith("player_sell_")) {
    return "Market";
  }
  if ((entry?.grantsUnlocks?.length ?? 0) > 0) {
    return "Unlocks";
  }
  if (key.includes("rest")) {
    return "Recovery";
  }
  if ((entry?.exclusiveGroup?.trim() ?? "") !== "" || (entry?.statDeltas && Object.keys(entry.statDeltas).length > 0)) {
    return "Training";
  }
  return "General";
}

function formatExclusiveGroupLabel(value: string | undefined): string {
  const trimmed = value?.trim();
  if (!trimmed) {
    return "";
  }
  return titleCaseStatLabel(trimmed);
}

function formatBehaviorRequirements(entry: BehaviorCatalogEntry | undefined): string {
  const requirements = entry?.requirements;
  if (!requirements) {
    return "";
  }

  const parts: string[] = [];
  for (const [resourceKey, amount] of sortedNumericEntries(requirements.items)) {
    if (amount > 0) {
      parts.push(`${amount} ${titleCaseStatLabel(resourceKey)}`);
    }
  }

  const unlocks = (requirements.unlocks ?? [])
    .map((unlock) => unlock.trim())
    .filter((unlock) => unlock.length > 0)
    .map((unlock) => titleCaseStatLabel(unlock));
  if (unlocks.length > 0) {
    parts.push(`Unlocks: ${unlocks.join(", ")}`);
  }

  return parts.join(", ");
}

function queuedStateTitle(behavior: BehaviorView): string {
  if (behavior.state !== "queued") {
    return behavior.state;
  }
  return behavior.waitReason?.trim() || "Queued and waiting to start.";
}

function sortedNumericEntries(values: Record<string, number> | undefined): Array<[string, number]> {
  return Object.entries(values ?? {})
    .filter(([, value]) => Number.isFinite(value) && value !== 0)
    .sort((left, right) => left[0].localeCompare(right[0]));
}

function appendNumericMap(target: Map<string, number>, source: Record<string, number> | undefined): void {
  for (const [key, amount] of Object.entries(source ?? {})) {
    if (!Number.isFinite(amount) || amount === 0) {
      continue;
    }
    target.set(key, (target.get(key) ?? 0) + amount);
  }
}

function formatAggregateMap(values: Map<string, number>): string {
  const parts = Array.from(values.entries())
    .filter(([, amount]) => Number.isFinite(amount) && amount !== 0)
    .sort((left, right) => left[0].localeCompare(right[0]))
    .map(([key, amount]) => `${amount} ${titleCaseStatLabel(key)}`);
  return parts.length > 0 ? parts.join(", ") : "none";
}

function describeBehaviorSpent(entry: BehaviorCatalogEntry | undefined): string {
  const parts: string[] = [];
  const staminaCost = entry?.staminaCost ?? 0;
  if (staminaCost > 0) {
    parts.push(`${staminaCost} stamina`);
  }

  for (const [resourceKey, amount] of sortedNumericEntries(entry?.costs)) {
    if (amount > 0) {
      parts.push(`${amount} ${titleCaseStatLabel(resourceKey)}`);
    }
  }

  return parts.length > 0 ? parts.join(", ") : "none";
}

function describeBehaviorGains(entry: BehaviorCatalogEntry | undefined, behavior: BehaviorView): string {
  if (behavior.gained && Object.keys(behavior.gained).length > 0) {
    const realized = sortedNumericEntries(behavior.gained)
      .map(([key, amount]) => `${amount} ${titleCaseStatLabel(key)}`)
      .join(", ");
    if (realized) {
      return realized;
    }
  }

  const parts: string[] = [];

  for (const [resourceKey, amount] of sortedNumericEntries(entry?.statDeltas)) {
    if (amount > 0) {
      parts.push(`${titleCaseStatLabel(resourceKey)} +${amount}`);
    }
  }

  for (const [resourceKey, amount] of sortedNumericEntries(entry?.costs)) {
    if (amount < 0) {
      parts.push(`${titleCaseStatLabel(resourceKey)} +${Math.abs(amount)}`);
    }
  }

  const resultMessage = behavior.resultMessage?.trim();
  if (resultMessage) {
    parts.push(resultMessage);
  }

  if (parts.length > 0) {
    return parts.join("; ");
  }

  return "see queue results";
}

function formatUpgradeProjection(entry: UpgradeCatalogEntry): string {
  const pieces: string[] = [];
  const nextCosts = Object.entries(entry.nextCosts ?? {}).filter(([, value]) => value > 0);
  if (nextCosts.length > 0) {
    pieces.push(`Cost: ${nextCosts.map(([key, value]) => `${value} ${titleCaseStatLabel(key)}`).join(", ")}`);
  }

  const nextQueueSlots = entry.nextOutputs?.queueSlotsDelta ?? 0;
  if (nextQueueSlots > 0) {
    pieces.push(`Output: +${nextQueueSlots} queue slot${nextQueueSlots === 1 ? "" : "s"}`);
  }

  const nextStats = Object.entries(entry.nextOutputs?.statDeltas ?? {}).filter(([, value]) => value > 0);
  if (nextStats.length > 0) {
    pieces.push(`Output: ${nextStats.map(([key, value]) => `+${value} ${titleCaseStatLabel(key)}`).join(", ")}`);
  }

  if (pieces.length === 0) {
    return "No scaled outputs configured.";
  }

  return pieces.join(" | ");
}

function streamStatusSummary(status: "connecting" | "live" | "fallback" | "offline", attempts: number): string {
  if (status === "live") {
    return "Live";
  }
  if (status === "connecting") {
    return "Connecting";
  }
  if (status === "fallback") {
    if (attempts > 0) {
      return `Reconnecting (${attempts} attempt${attempts === 1 ? "" : "s"})`;
    }
    return "Reconnecting";
  }
  return "Offline";
}

function clampPercent(value: number): number {
  if (!Number.isFinite(value)) {
    return 0;
  }
  if (value < 0) {
    return 0;
  }
  if (value > 100) {
    return 100;
  }
  return value;
}

function formatRealmLabel(realmId: number | undefined): string {
  if (!realmId || realmId <= 0) {
    return "Realm -";
  }
  return `Realm ${realmId}`;
}

type RealmMeta = {
  realmId: number;
  name: string;
  whitelistOnly: boolean;
  canCreateCharacter: boolean;
  decommissioned: boolean;
};

export function App() {
  const lastStreamTickRef = useRef<number | null>(null);
  const selectedCharacterIDRef = useRef<number | undefined>(undefined);
  const behaviorStatesByIDRef = useRef<Map<number, string>>(new Map());
  const behaviorToastPrimedRef = useRef(false);
  const [authMode, setAuthMode] = useState<AuthMode>("login");
  const [authUsername, setAuthUsername] = useState("");
  const [authPassword, setAuthPassword] = useState("");

  const [account, setAccount] = useState<AccountData | null>(getSession()?.account ?? null);
  const [meData, setMeData] = useState<MeData | null>(null);
  const [onboarding, setOnboarding] = useState<OnboardingStatusData | null>(null);

  const [view, setView] = useState<AppView>("profile");
  const [gameplayTab, setGameplayTab] = useState<GameplayTab>("overview");
  const [selectedCharacterId, setSelectedCharacterId] = useState<number | undefined>(undefined);
  const [selectedRealmId, setSelectedRealmId] = useState<number>(1);
  const [newCharacterName, setNewCharacterName] = useState("Wanderer");

  const [playerStatus, setPlayerStatus] = useState<PlayerStatusData | null>(null);
  const [playerInventory, setPlayerInventory] = useState<PlayerInventoryData | null>(null);
  const [playerBehaviors, setPlayerBehaviors] = useState<BehaviorView[]>([]);
  const [behaviorCatalog, setBehaviorCatalog] = useState<BehaviorCatalogEntry[]>([]);
  const [upgradeCatalog, setUpgradeCatalog] = useState<UpgradeCatalogEntry[]>([]);
  const [feedEvents, setFeedEvents] = useState<FeedEvent[]>([]);
  const [marketStatus, setMarketStatus] = useState<MarketStatus | null>(null);
  const [marketSymbol, setMarketSymbol] = useState("scrap");
  const [marketSide, setMarketSide] = useState<"buy" | "sell">("buy");
  const [marketQuantityLots, setMarketQuantityLots] = useState("1");
  const [marketLimitPrice, setMarketLimitPrice] = useState("8");
  const [marketCancelAfter, setMarketCancelAfter] = useState("24h");
  const [myMarketOrders, setMyMarketOrders] = useState<MarketOrderView[]>([]);
  const [marketOrderBook, setMarketOrderBook] = useState<MarketOrderBook | null>(null);
  const [marketTrades, setMarketTrades] = useState<MarketTradeView[]>([]);
  const [marketCandles, setMarketCandles] = useState<MarketCandleEntry[]>([]);
  const [marketAppliedBucketTicks, setMarketAppliedBucketTicks] = useState(30);
  const [marketOverviewSymbols, setMarketOverviewSymbols] = useState<MarketOverviewSymbol[]>([]);
  const [candleBucketTicks, setCandleBucketTicks] = useState(30);
  const [candleWindowRange, setCandleWindowRange] = useState("24h");
  const [marketOverviewBucketTicks, setMarketOverviewBucketTicks] = useState(60);
  const [marketOverviewWindowRange, setMarketOverviewWindowRange] = useState("3d");
  const [marketOverviewSelectedSymbols, setMarketOverviewSelectedSymbols] = useState<string[]>([]);

  const [queueModalOpen, setQueueModalOpen] = useState(false);
  const [selectedBehaviorKey, setSelectedBehaviorKey] = useState("");
  const [queueCategoryFilter, setQueueCategoryFilter] = useState("All");
  const [queueSearch, setQueueSearch] = useState("");
  const [queueMode, setQueueMode] = useState<"once" | "repeat" | "repeat-until">("once");
  const [queueRepeatUntil, setQueueRepeatUntil] = useState("12h");
  const [marketWait, setMarketWait] = useState("12h");

  const [channels, setChannels] = useState<ChatChannel[]>([]);
  const [chatChannel, setChatChannel] = useState("global");
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [chatDraft, setChatDraft] = useState("");

  const [adminOpen, setAdminOpen] = useState(false);

  const [loading, setLoading] = useState(false);
  const [bootstrapping, setBootstrapping] = useState<boolean>(() => !!getSession());
  const [actionBusy, setActionBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [realmPausedMessage, setRealmPausedMessage] = useState<string | null>(null);
  const [toasts, setToasts] = useState<ToastItem[]>([]);

  const isLoggedIn = !!account;
  const hasActiveSession = isLoggedIn && !bootstrapping;
  const isAdmin = (account?.roles ?? []).includes("admin");
  const characters = meData?.characters ?? onboarding?.characters ?? [];
  const onboardingRealms = (onboarding?.realms ?? []) as RealmMeta[];

  const resetClientState = useCallback((options?: { clearAccountContext?: boolean }) => {
    const clearAccountContext = options?.clearAccountContext ?? false;

    if (clearAccountContext) {
      setAccount(null);
      setMeData(null);
      setOnboarding(null);
      setSelectedCharacterId(undefined);
      savePreferredCharacterID(undefined);
    }

    setPlayerStatus(null);
    setPlayerInventory(null);
    setPlayerBehaviors([]);
    setBehaviorCatalog([]);
    setUpgradeCatalog([]);
    setFeedEvents([]);
    setMarketStatus(null);
    setMyMarketOrders([]);
    setMarketOrderBook(null);
    setMarketTrades([]);
    setMarketCandles([]);
    setMarketAppliedBucketTicks(30);
    setMarketOverviewSymbols([]);
    setMessages([]);
    setChannels([]);
    setChatDraft("");
    setQueueModalOpen(false);
    setSelectedBehaviorKey("");
    setRealmPausedMessage(null);
    setAdminOpen(false);
  }, []);

  const selectedCharacter = useMemo(() => {
    if (!selectedCharacterId) {
      return characters.find((entry) => entry.isPrimary) ?? characters[0];
    }
    return characters.find((entry) => entry.id === selectedCharacterId);
  }, [characters, selectedCharacterId]);
  const hasCharacterContext = !!selectedCharacter?.id;
  const realmByID = useMemo(() => {
    const map = new Map<number, RealmMeta>();
    for (const realm of onboardingRealms) {
      map.set(realm.realmId, realm);
    }
    return map;
  }, [onboardingRealms]);
  const formatRealmName = useCallback((realmId: number | undefined) => {
    if (!realmId || realmId <= 0) {
      return "Realm -";
    }
    return realmByID.get(realmId)?.name || formatRealmLabel(realmId);
  }, [realmByID]);

  const onboardingRealmOptions = useMemo(() => {
    if (onboardingRealms.length > 0) {
      const active = onboardingRealms
        .filter((realm) => !realm.decommissioned)
        .map((realm) => realm.realmId)
        .filter((realmId) => Number.isInteger(realmId) && realmId > 0);
      if (active.length > 0) {
        return Array.from(new Set(active)).sort((left, right) => left - right);
      }
    }

    const realmIds = new Set<number>();
    for (const character of characters) {
      if (character.realmId > 0) {
        realmIds.add(character.realmId);
      }
    }
    if (onboarding?.defaultRealm && onboarding.defaultRealm > 0) {
      realmIds.add(onboarding.defaultRealm);
    }
    if (selectedRealmId > 0) {
      realmIds.add(selectedRealmId);
    }
    if (realmIds.size === 0) {
      realmIds.add(1);
    }
    return Array.from(realmIds.values()).sort((left, right) => left - right);
  }, [characters, onboarding?.defaultRealm, onboardingRealms, selectedRealmId]);
  const selectedOnboardingRealm = useMemo(() => {
    return onboardingRealms.find((realm) => realm.realmId === selectedRealmId);
  }, [onboardingRealms, selectedRealmId]);
  const onboardingBlockedByWhitelist = !!selectedOnboardingRealm?.whitelistOnly && !selectedOnboardingRealm?.canCreateCharacter;

  useEffect(() => {
	selectedCharacterIDRef.current = selectedCharacterId;
  }, [selectedCharacterId]);

  const coreStats = playerStatus?.coreStats ?? playerStatus?.stats ?? {};
  const derivedStats = playerStatus?.derivedStats ?? playerStatus?.stats ?? {};
  const enduranceValue = statValue(coreStats, ["endurance"], 0);
  const staminaCurrent = statValue(derivedStats, ["stamina"], 0);
  const staminaMax = statValue(derivedStats, ["maxStamina", "staminaMax", "max_stamina"], Math.max(staminaCurrent, 100));
  const staminaPercent = staminaMax > 0 ? Math.min(100, Math.max(0, (staminaCurrent / staminaMax) * 100)) : 0;
  const queuedOrActiveCount = playerBehaviors.filter((entry) => entry.state === "queued" || entry.state === "active").length;
  const queueCurrentTick = playerStatus?.simulationTick;
  const queueActiveRows = useMemo(() => {
    return playerBehaviors
      .filter((entry) => entry.state === "queued" || entry.state === "active")
      .sort((left, right) => {
        const stateDelta = queueSortOrder(left.state) - queueSortOrder(right.state);
        if (stateDelta !== 0) {
          return stateDelta;
        }
        if (left.completesAtTick !== right.completesAtTick) {
          return left.completesAtTick - right.completesAtTick;
        }
        return left.id - right.id;
      });
  }, [playerBehaviors]);
  const queueHistoryRows = useMemo(() => {
    return playerBehaviors
      .filter((entry) => entry.state === "completed" || entry.state === "failed" || entry.state === "cancelled")
      .sort((left, right) => {
        if (left.completesAtTick !== right.completesAtTick) {
          return right.completesAtTick - left.completesAtTick;
        }
        return right.id - left.id;
      })
      .slice(0, 12);
  }, [playerBehaviors]);
  const marketTicker = useMemo(() => {
    return (marketStatus?.tickers ?? []).find((entry) => entry.symbol === marketSymbol);
  }, [marketStatus?.tickers, marketSymbol]);
  const marketTopBid = useMemo(() => {
    const bids = marketOrderBook?.buys ?? [];
    if (bids.length <= 0) {
      return undefined;
    }
    return bids[0]?.limitPrice;
  }, [marketOrderBook?.buys]);
  const marketTopAsk = useMemo(() => {
    const asks = marketOrderBook?.sells ?? [];
    if (asks.length <= 0) {
      return undefined;
    }
    return asks[0]?.limitPrice;
  }, [marketOrderBook?.sells]);
  const marketMidPrice = useMemo(() => {
    if (!marketTopBid || !marketTopAsk) {
      return undefined;
    }
    return Math.round((marketTopBid + marketTopAsk) / 2);
  }, [marketTopAsk, marketTopBid]);
  const marketSpread = useMemo(() => {
    if (!marketTopBid || !marketTopAsk) {
      return undefined;
    }
    return marketTopAsk - marketTopBid;
  }, [marketTopAsk, marketTopBid]);
  const marketRecentSeries = useMemo(() => {
    return [...marketTrades]
      .filter((entry) => entry.itemKey === marketSymbol)
      .sort((left, right) => {
        if (left.tick !== right.tick) {
          return left.tick - right.tick;
        }
        return left.id - right.id;
      })
      .slice(-40);
  }, [marketSymbol, marketTrades]);
  const marketFlowSummary = useMemo(() => {
    if (marketRecentSeries.length <= 0) {
      return {
        trades: 0,
        volume: 0,
        npcInvolved: 0,
        npcSharePct: undefined as number | undefined,
        vwap: undefined as number | undefined,
        lastMove: undefined as number | undefined
      };
    }

    let volume = 0;
    let notional = 0;
    let npcInvolved = 0;
    for (const trade of marketRecentSeries) {
      volume += trade.quantity;
      notional += trade.price * trade.quantity;
      if (trade.buyerType === "npc" || trade.sellerType === "npc") {
        npcInvolved += 1;
      }
    }

    const firstPrice = marketRecentSeries[0]?.price;
    const lastPrice = marketRecentSeries[marketRecentSeries.length - 1]?.price;
    return {
      trades: marketRecentSeries.length,
      volume,
      npcInvolved,
      npcSharePct: marketRecentSeries.length > 0 ? (npcInvolved / marketRecentSeries.length) * 100 : undefined,
      vwap: volume > 0 ? notional / volume : undefined,
      lastMove: firstPrice !== undefined && lastPrice !== undefined ? lastPrice - firstPrice : undefined
    };
  }, [marketRecentSeries]);
  const marketLiquidityView = useMemo(() => {
    return marketTicker?.liquidity;
  }, [marketTicker?.liquidity]);
  const marketMovementView = useMemo(() => {
    return marketTicker?.movement;
  }, [marketTicker?.movement]);
  const marketRegime = useMemo(() => {
    return marketRegimeLabel(marketMovementView?.windowChange, marketLiquidityView?.utilizationPct);
  }, [marketLiquidityView?.utilizationPct, marketMovementView?.windowChange]);
  const marketDepthView = useMemo(() => {
    const buys = (marketOrderBook?.buys ?? []).slice(0, 6);
    const sells = (marketOrderBook?.sells ?? []).slice(0, 6);
    const maxQty = Math.max(
      1,
      ...buys.map((entry) => entry.quantityOpen),
      ...sells.map((entry) => entry.quantityOpen)
    );

    return {
      buys: buys.map((entry) => ({
        price: entry.limitPrice,
        quantity: entry.quantityOpen,
        widthPct: Math.max(4, Math.round((entry.quantityOpen / maxQty) * 100))
      })),
      sells: sells.map((entry) => ({
        price: entry.limitPrice,
        quantity: entry.quantityOpen,
        widthPct: Math.max(4, Math.round((entry.quantityOpen / maxQty) * 100))
      }))
    };
  }, [marketOrderBook?.buys, marketOrderBook?.sells]);
  const candlePointLimit = useMemo(() => {
    return candleWindowPoints(candleWindowRange, candleBucketTicks);
  }, [candleBucketTicks, candleWindowRange]);
  const marketOverviewPointLimit = useMemo(() => {
    return candleWindowPoints(marketOverviewWindowRange, marketOverviewBucketTicks);
  }, [marketOverviewBucketTicks, marketOverviewWindowRange]);
  const visibleMarketCandles = useMemo(() => {
    return [...marketCandles].slice(-candlePointLimit);
  }, [candlePointLimit, marketCandles]);
  const marketCandleRange = useMemo(() => {
    if (visibleMarketCandles.length === 0) {
      return { min: 0, max: 0, span: 1 };
    }
    const min = Math.min(...visibleMarketCandles.map((entry) => entry.low));
    const max = Math.max(...visibleMarketCandles.map((entry) => entry.high));
    const span = Math.max(1, max - min);
    return { min, max, span };
  }, [visibleMarketCandles]);
  const marketCandleTrendPath = useMemo(() => {
    return candleCloseSeriesPath(visibleMarketCandles);
  }, [visibleMarketCandles]);
  const marketOverviewRows = useMemo(() => {
    return marketOverviewSymbols
      .map((entry) => {
        const sparkline = candleCloseSeriesPath(entry.candles ?? []);
        const first = entry.candles?.[0]?.close;
        const last = entry.candles?.[entry.candles.length - 1]?.close;
        const change = first !== undefined && last !== undefined ? last - first : undefined;
        return {
          symbol: entry.symbol,
          currentPrice: entry.currentPrice,
          delta: entry.delta,
          liquidity: entry.liquidity,
          movement: entry.movement,
          candles: entry.candles,
          sparkline,
          historyChange: change
        };
      })
      .sort((left, right) => left.symbol.localeCompare(right.symbol));
  }, [marketOverviewSymbols]);
  const marketOverviewVisibleRows = useMemo(() => {
    const selected = new Set(marketOverviewSelectedSymbols);
    return marketOverviewRows.filter((row) => selected.has(row.symbol));
  }, [marketOverviewRows, marketOverviewSelectedSymbols]);
  const marketOverviewChartBounds = useMemo(() => {
    const closes = marketOverviewVisibleRows.flatMap((row) => (row.candles ?? []).map((entry) => entry.close));
    if (closes.length <= 0) {
      return { min: 0, max: 0, span: 1 };
    }
    const min = Math.min(...closes);
    const max = Math.max(...closes);
    return { min, max, span: Math.max(1, max - min) };
  }, [marketOverviewVisibleRows]);
  const marketOverviewSeries = useMemo(() => {
    return marketOverviewVisibleRows.map((row) => {
      const points = linePointsFromCandles(row.candles ?? [], marketOverviewChartBounds.min, marketOverviewChartBounds.max);
      return {
        symbol: row.symbol,
        points,
        path: linePathFromPoints(points)
      };
    });
  }, [marketOverviewChartBounds.max, marketOverviewChartBounds.min, marketOverviewVisibleRows]);
  const playerMarketSymbolInventory = playerInventory?.inventory?.[marketSymbol] ?? 0;
  const playerCoins = playerInventory?.inventory?.coins ?? 0;
  const selectedLots = useMemo(() => {
    const parsed = parseNumber(marketQuantityLots);
    if (!parsed || parsed <= 0) {
      return 1;
    }
    return Math.max(1, Math.min(10, Math.floor(parsed)));
  }, [marketQuantityLots]);
  const selectedQuantityUnits = selectedLots * 100;
  const parsedLimitPrice = useMemo(() => {
    const parsed = parseNumber(marketLimitPrice);
    if (!parsed || parsed <= 0) {
      return undefined;
    }
    return Math.floor(parsed);
  }, [marketLimitPrice]);
  const maxSellLots = Math.max(0, Math.floor(playerMarketSymbolInventory / 100));
  const maxBuyLots = parsedLimitPrice && parsedLimitPrice > 0
    ? Math.max(0, Math.floor(playerCoins / (parsedLimitPrice * 100)))
    : 0;
  const coreStatRows = useMemo(() => buildStatDisplayRows(coreStats), [coreStats]);
  const derivedStatRows = useMemo(() => buildStatDisplayRows(derivedStats), [derivedStats]);
  const stream = useWorldStream(selectedCharacter?.id, hasActiveSession && !!selectedCharacter?.id);
  const streamSummary = streamStatusSummary(stream.status, stream.attempts);

  const liveStreamEvent = stream.status === "live" ? stream.event : null;
  const snapshotTick = liveStreamEvent?.tick ?? playerStatus?.simulationTick;
  const snapshotDay = liveStreamEvent?.day ?? (snapshotTick !== undefined ? Math.floor(snapshotTick / (24 * 60)) : undefined);
  const snapshotClock = liveStreamEvent?.clock;
  const snapshotDayPart = liveStreamEvent?.dayPart;
  const snapshotMarketState = liveStreamEvent?.marketState ?? marketStatus?.sessionState;
  const snapshotCoins = liveStreamEvent?.player?.coins ?? playerInventory?.inventory?.coins;
  const snapshotBehaviorBars = useMemo<Array<{ id: number; progressPct: number; state: "queued" | "active" }>>(() => {
    const currentTick = snapshotTick ?? playerStatus?.simulationTick;
    return playerBehaviors
      .filter((entry) => entry.state === "queued" || entry.state === "active")
      .sort((left, right) => {
        if (left.completesAtTick !== right.completesAtTick) {
          return left.completesAtTick - right.completesAtTick;
        }
        return left.id - right.id;
      })
      .slice(0, 14)
      .map((entry) => {
        let progressPct = 0;

        if (entry.state === "active") {
          const fallbackStart = entry.completesAtTick - Math.max(1, entry.durationMinutes);
          const startedAtTick = entry.startedAtTick > 0 ? entry.startedAtTick : fallbackStart;
          const totalDuration = Math.max(1, entry.completesAtTick - startedAtTick);
          const elapsed = currentTick !== undefined ? currentTick - startedAtTick : 0;
          progressPct = clampPercent((elapsed / totalDuration) * 100);
        }

        return {
          id: entry.id,
          progressPct: Math.round(progressPct),
          state: entry.state === "active" ? "active" : "queued"
        };
      });
  }, [playerBehaviors, playerStatus?.simulationTick, snapshotTick]);

  const pushToast = useCallback((type: ToastType, message: string) => {
    const id = Date.now() + Math.floor(Math.random() * 10_000);
    setToasts((current) => {
      const next = [...current, { id, type, message }];
      return next.slice(-5);
    });
    window.setTimeout(() => {
      setToasts((current) => current.filter((entry) => entry.id !== id));
    }, 5000);
  }, []);

  const dismissToast = useCallback((id: number) => {
    setToasts((current) => current.filter((entry) => entry.id !== id));
  }, []);

  const handleError = useCallback((err: unknown) => {
    const message = (err as Error).message;
    const isBootAuthBoundary = bootstrapping && message.toLowerCase().includes("not authenticated");
    if (isBootAuthBoundary) {
      return;
    }
    pushToast("error", message);
    if (message.toLowerCase().includes("maintenance") || message.toLowerCase().includes("realm is under maintenance")) {
      setRealmPausedMessage(message);
    }
  }, [bootstrapping, pushToast]);

  useEffect(() => {
    if (!notice) {
      return;
    }
    pushToast("success", notice);
  }, [notice, pushToast]);

  const queueCatalog = useMemo(() => {
    return behaviorCatalog.filter((entry) => entry.queueVisible ?? entry.available);
  }, [behaviorCatalog]);

  const queueAvailableBehaviors = useMemo(() => {
    return queueCatalog
      .filter((entry) => entry.available)
      .sort((left, right) => {
        const categoryDelta = behaviorCategory(left, left.key).localeCompare(behaviorCategory(right, right.key));
        if (categoryDelta !== 0) {
          return categoryDelta;
        }
        const leftLabel = behaviorDisplayLabel(left, left.key);
        const rightLabel = behaviorDisplayLabel(right, right.key);
        return leftLabel.localeCompare(rightLabel);
      });
  }, [queueCatalog]);

  const inaccessibleQueueCount = useMemo(() => {
    return queueCatalog.filter((entry) => !entry.available).length;
  }, [queueCatalog]);

  const queueCategories = useMemo(() => {
    const values = new Set<string>();
    for (const entry of queueAvailableBehaviors) {
      values.add(behaviorCategory(entry, entry.key));
    }
    return ["All", ...Array.from(values).sort((left, right) => left.localeCompare(right))];
  }, [queueAvailableBehaviors]);

  const filteredQueueBehaviors = useMemo(() => {
    const search = queueSearch.trim().toLowerCase();
    return queueAvailableBehaviors.filter((entry) => {
      const category = behaviorCategory(entry, entry.key);
      if (queueCategoryFilter !== "All" && category !== queueCategoryFilter) {
        return false;
      }
      if (!search) {
        return true;
      }

      const label = behaviorDisplayLabel(entry, entry.key).toLowerCase();
      const summary = (entry.summary ?? "").toLowerCase();
      const group = (entry.exclusiveGroup ?? "").toLowerCase();
      return label.includes(search) || summary.includes(search) || group.includes(search) || entry.key.toLowerCase().includes(search);
    });
  }, [queueAvailableBehaviors, queueCategoryFilter, queueSearch]);

  const activeQueueCount = useMemo(() => {
    return playerBehaviors.filter((entry) => entry.state === "active").length;
  }, [playerBehaviors]);

  const queuedOnlyCount = useMemo(() => {
    return playerBehaviors.filter((entry) => entry.state === "queued").length;
  }, [playerBehaviors]);

  const queueSlotsTotal = playerStatus?.queueSlotsTotal;
  const queueSlotsUsed = playerStatus?.queueSlotsUsed ?? queuedOrActiveCount;
  const queueSlotsAvailable = playerStatus?.queueSlotsAvailable ?? (
    queueSlotsTotal !== undefined ? Math.max(0, queueSlotsTotal - queueSlotsUsed) : undefined
  );
  const queueSlotsExhausted = queueSlotsAvailable !== undefined && queueSlotsAvailable <= 0;

  const behaviorCatalogByKey = useMemo(() => {
    return new Map(behaviorCatalog.map((entry) => [entry.key, entry]));
  }, [behaviorCatalog]);

  const hasSelectedChatChannel = useMemo(() => {
    return channels.some((entry) => entry.key === chatChannel);
  }, [channels, chatChannel]);

  const selectedQueueBehavior = useMemo(() => {
    if (!selectedBehaviorKey) {
      return undefined;
    }
    return behaviorCatalogByKey.get(selectedBehaviorKey);
  }, [behaviorCatalogByKey, selectedBehaviorKey]);

  const selectedQueueModes = useMemo<Array<"once" | "repeat" | "repeat-until">>(() => {
    const configured = selectedQueueBehavior?.scheduleModes;
    if (!configured || configured.length === 0) {
      return ["once", "repeat", "repeat-until"];
    }
    return configured;
  }, [selectedQueueBehavior?.scheduleModes]);

  const progressionLockedBehaviors = useMemo(() => {
    return behaviorCatalog
      .filter((entry) => !entry.available && !entry.consumedThisAscension)
      .sort((left, right) => behaviorDisplayLabel(left, left.key).localeCompare(behaviorDisplayLabel(right, right.key)));
  }, [behaviorCatalog]);

  const progressionConsumedBehaviors = useMemo(() => {
    return behaviorCatalog
      .filter((entry) => entry.consumedThisAscension)
      .sort((left, right) => behaviorDisplayLabel(left, left.key).localeCompare(behaviorDisplayLabel(right, right.key)));
  }, [behaviorCatalog]);

  const sortedUpgradeCatalog = useMemo(() => {
    return [...upgradeCatalog].sort((left, right) => {
      const leftName = (left.name ?? left.key).trim();
      const rightName = (right.name ?? right.key).trim();
      return leftName.localeCompare(rightName);
    });
  }, [upgradeCatalog]);

  const activeExclusiveByGroup = useMemo(() => {
    const byGroup = new Map<string, BehaviorView>();
    for (const behavior of playerBehaviors) {
      if (behavior.state !== "active") {
        continue;
      }
      const activeCatalog = behaviorCatalogByKey.get(behavior.key);
      const activeGroup = (activeCatalog?.exclusiveGroup ?? "").trim().toLowerCase();
      if (!activeGroup || byGroup.has(activeGroup)) {
        continue;
      }
      byGroup.set(activeGroup, behavior);
    }
    return byGroup;
  }, [behaviorCatalogByKey, playerBehaviors]);

  const queueConflictReasonForBehavior = useCallback((entry: BehaviorCatalogEntry | undefined): string => {
    if (!entry?.exclusiveGroup) {
      return "";
    }

    const selectedGroup = entry.exclusiveGroup.trim().toLowerCase();
    if (!selectedGroup) {
      return "";
    }

    const conflicting = activeExclusiveByGroup.get(selectedGroup);
    if (!conflicting) {
      return "";
    }

    const conflictingCatalog = behaviorCatalogByKey.get(conflicting.key);
    return `${behaviorDisplayLabel(conflictingCatalog, conflicting.key)} is active in ${formatExclusiveGroupLabel(selectedGroup)} and blocks this behavior.`;
  }, [activeExclusiveByGroup, behaviorCatalogByKey]);

  const selectedQueueConflictReason = useMemo(() => {
    return queueConflictReasonForBehavior(selectedQueueBehavior);
  }, [queueConflictReasonForBehavior, selectedQueueBehavior]);

  const displayBehaviorLabel = useCallback((key: string) => {
    return behaviorDisplayLabel(behaviorCatalogByKey.get(key), key);
  }, [behaviorCatalogByKey]);

  useEffect(() => {
    behaviorStatesByIDRef.current = new Map();
    behaviorToastPrimedRef.current = false;
  }, [selectedCharacter?.id]);

  useEffect(() => {
    if (!hasActiveSession || !selectedCharacter?.id) {
      behaviorStatesByIDRef.current = new Map();
      behaviorToastPrimedRef.current = false;
      return;
    }

    const previousStates = behaviorStatesByIDRef.current;
    const nextStates = new Map<number, string>();
    for (const behavior of playerBehaviors) {
      nextStates.set(behavior.id, behavior.state);
    }

    if (!behaviorToastPrimedRef.current) {
      behaviorToastPrimedRef.current = true;
      behaviorStatesByIDRef.current = nextStates;
      return;
    }

    for (const behavior of playerBehaviors) {
      const previousState = previousStates.get(behavior.id);
      if (!previousState || previousState === behavior.state) {
        continue;
      }

      const catalogEntry = behaviorCatalogByKey.get(behavior.key);
      const label = behaviorDisplayLabel(catalogEntry, behavior.key);

      if (behavior.state === "active" && previousState === "queued") {
        const spent = describeBehaviorSpent(catalogEntry);
        pushToast("info", `${label} started. Spent: ${spent}.`);
        continue;
      }

      if (behavior.state === "completed" && (previousState === "queued" || previousState === "active")) {
        continue;
      }

      if (behavior.state === "cancelled" && (previousState === "queued" || previousState === "active")) {
        pushToast("info", `${label} cancelled.`);
        continue;
      }

      if (behavior.state === "failed" && (previousState === "queued" || previousState === "active")) {
        const reason = behavior.failureReason?.trim() || "Behavior failed.";
        pushToast("error", `${label} failed. ${reason}`);
      }
    }

    const completedTransitions = playerBehaviors.filter((behavior) => {
      const previousState = previousStates.get(behavior.id);
      return behavior.state === "completed" && (previousState === "queued" || previousState === "active");
    });
    if (completedTransitions.length > 1) {
      const totalSpent = new Map<string, number>();
      const totalGained = new Map<string, number>();
      for (const behavior of completedTransitions) {
        if (behavior.spent && Object.keys(behavior.spent).length > 0) {
          appendNumericMap(totalSpent, behavior.spent);
        } else {
          const fallbackEntry = behaviorCatalogByKey.get(behavior.key);
          if (fallbackEntry) {
            if ((fallbackEntry.staminaCost ?? 0) > 0) {
              totalSpent.set("stamina", (totalSpent.get("stamina") ?? 0) + (fallbackEntry.staminaCost ?? 0));
            }
            appendNumericMap(totalSpent, fallbackEntry.costs);
          }
        }
        if (behavior.gained && Object.keys(behavior.gained).length > 0) {
          appendNumericMap(totalGained, behavior.gained);
        } else {
          const fallbackEntry = behaviorCatalogByKey.get(behavior.key);
          appendNumericMap(totalGained, fallbackEntry?.outputs);
        }
      }
      pushToast(
        "success",
        `${completedTransitions.length} queued runs finished. Spent: ${formatAggregateMap(totalSpent)}. Gained: ${formatAggregateMap(totalGained)}.`
      );
    } else if (completedTransitions.length === 1) {
      const behavior = completedTransitions[0];
      const catalogEntry = behaviorCatalogByKey.get(behavior.key);
      const label = behaviorDisplayLabel(catalogEntry, behavior.key);
      const gains = describeBehaviorGains(catalogEntry, behavior);
      pushToast("success", `${label} finished. Gained: ${gains}.`);
    }

    behaviorStatesByIDRef.current = nextStates;
  }, [behaviorCatalogByKey, hasActiveSession, playerBehaviors, pushToast, selectedCharacter?.id]);

  useEffect(() => {
    if (!hasSelectedChatChannel && channels.length > 0) {
      setChatChannel(channels[0].key);
    }
  }, [channels, hasSelectedChatChannel]);

  useEffect(() => {
    if (queueAvailableBehaviors.length === 0) {
      if (selectedBehaviorKey) {
        setSelectedBehaviorKey("");
      }
      return;
    }

    const selectedStillAvailable = queueAvailableBehaviors.some((entry) => entry.key === selectedBehaviorKey);
    if (!selectedStillAvailable) {
      setSelectedBehaviorKey(queueAvailableBehaviors[0].key);
    }
  }, [queueAvailableBehaviors, selectedBehaviorKey]);

  useEffect(() => {
    if (!queueCategories.includes(queueCategoryFilter)) {
      setQueueCategoryFilter("All");
    }
  }, [queueCategories, queueCategoryFilter]);

  useEffect(() => {
    if (selectedQueueModes.length === 0) {
      setQueueMode("once");
      return;
    }
    if (!selectedQueueModes.includes(queueMode)) {
      setQueueMode(selectedQueueModes[0]);
    }
  }, [queueMode, selectedQueueModes]);

  useEffect(() => {
    const available = marketOverviewRows.map((row) => row.symbol);
    if (available.length <= 0) {
      setMarketOverviewSelectedSymbols([]);
      return;
    }

    setMarketOverviewSelectedSymbols((current) => {
      const filtered = current.filter((symbol) => available.includes(symbol));
      if (filtered.length > 0) {
        return filtered;
      }
      return available.slice(0, Math.min(3, available.length));
    });
  }, [marketOverviewRows]);

  const loadAccountContext = useCallback(async () => {
    if (!getSession()) {
      resetClientState({ clearAccountContext: true });
      return;
    }

    setLoading(true);
    try {
      const me = await getMe();
      setMeData(me);
      setAccount(me.account);

      const onboardingData = await getOnboardingStatus();
      setOnboarding(onboardingData);
      setSelectedRealmId(onboardingData.defaultRealm || 1);

	  const preferredCharacterID = selectedCharacterIDRef.current ?? loadPreferredCharacterID();
	  const preferred = preferredCharacterID ? me.characters.find((entry) => entry.id === preferredCharacterID) : undefined;
	  const nextCharacter = preferred ?? me.characters.find((entry) => entry.isPrimary) ?? me.characters[0];
	  if (nextCharacter) {
		setSelectedCharacterId(nextCharacter.id);
		savePreferredCharacterID(nextCharacter.id);
	  } else {
		setSelectedCharacterId(undefined);
		savePreferredCharacterID(undefined);
	  }
      setRealmPausedMessage(null);
    } catch (err) {
      handleError(err);
      clearSession();
      resetClientState({ clearAccountContext: true });
    } finally {
      setLoading(false);
    }
  }, [handleError, resetClientState]);

  const onCharacterSwitch = useCallback(async (nextCharacterID: number | undefined) => {
    if (!nextCharacterID || nextCharacterID <= 0) {
      return;
    }

    if (nextCharacterID === selectedCharacter?.id) {
      return;
    }

    setActionBusy(true);
    try {
      await switchOnboardingCharacter(nextCharacterID);
      savePreferredCharacterID(nextCharacterID);
      setSelectedCharacterId(nextCharacterID);
      setNotice("Switching character context...");
      if (typeof window !== "undefined") {
        window.location.reload();
      }
    } catch (err) {
      handleError(err);
    } finally {
      setActionBusy(false);
    }
  }, [handleError, selectedCharacter?.id]);

  const loadGameplay = useCallback(async () => {
    if (!hasActiveSession || !selectedCharacter?.id) {
      return;
    }

    try {
      const [status, inventory, behaviors, catalog, upgrades, feed, market, orders, book, trades, selectedCandles, overview] = await Promise.all([
        getPlayerStatus(selectedCharacter.id),
        getPlayerInventory(selectedCharacter.id),
        getPlayerBehaviors(selectedCharacter.id),
        getBehaviorCatalog(selectedCharacter.id),
        getUpgradeCatalog(selectedCharacter.id),
        getFeedPublic(selectedCharacter.id, 30),
        getMarketStatus(selectedCharacter.realmId, selectedCharacter.id),
        getMyMarketOrders("open", 100, selectedCharacter.id),
        getMarketOrderBook(marketSymbol, 20, selectedCharacter.id),
        getRecentMarketTrades(marketSymbol, 100, selectedCharacter.id),
        getMarketCandles(marketSymbol, candleBucketTicks, candlePointLimit, selectedCharacter.realmId, selectedCharacter.id),
        getMarketOverview(marketOverviewBucketTicks, marketOverviewPointLimit, selectedCharacter.realmId, selectedCharacter.id)
      ]);
      setPlayerStatus(status);
      setPlayerInventory(inventory);
      setPlayerBehaviors(behaviors.behaviors);
      setBehaviorCatalog(catalog);
      setUpgradeCatalog(upgrades);
      setFeedEvents(feed.events ?? []);
      setMarketStatus(market);
      setMyMarketOrders(orders.orders ?? []);
      setMarketOrderBook(book);
      setMarketTrades(trades.trades ?? []);
      setMarketCandles(selectedCandles.candles ?? []);
      setMarketAppliedBucketTicks(selectedCandles.bucketTicks ?? candleBucketTicks);
      setMarketOverviewSymbols(overview.symbols ?? []);
      setRealmPausedMessage(null);
    } catch (err) {
      handleError(err);
    }
  }, [
    candleBucketTicks,
    candlePointLimit,
    handleError,
    hasActiveSession,
    marketOverviewBucketTicks,
    marketOverviewPointLimit,
    marketSymbol,
    selectedCharacter
  ]);

  const loadChat = useCallback(async () => {
    if (!hasActiveSession || !selectedCharacter?.id) {
      return;
    }

    try {
      const channelResult = await getChatChannels(selectedCharacter.id);
      const availableChannels = channelResult.channels ?? [];
      setChannels(availableChannels);

      const resolvedChannel = availableChannels.some((entry) => entry.key === chatChannel)
        ? chatChannel
        : availableChannels[0]?.key;

      if (!resolvedChannel) {
        setMessages([]);
        setRealmPausedMessage(null);
        return;
      }

      if (resolvedChannel !== chatChannel) {
        setChatChannel(resolvedChannel);
      }

      const messageResult = await getChatMessages(resolvedChannel, selectedCharacter.id, 100);
      setMessages((current) => {
        const previousMaxID = current.reduce((max, entry) => Math.max(max, entry.id), 0);
        const next = messageResult.messages ?? [];
        if (previousMaxID > 0) {
          const newSystemMessages = next.filter((entry) => entry.id > previousMaxID && entry.messageClass === "system");
          for (const message of newSystemMessages) {
            pushToast("info", `[${message.channel}] ${message.message}`);
          }
        }
        return next;
      });
      setRealmPausedMessage(null);
    } catch (err) {
      handleError(err);
    }
  }, [chatChannel, handleError, hasActiveSession, pushToast, selectedCharacter]);

  useEffect(() => {
    void getSystemVersion();
    const session = getSession();
    if (!session) {
      setBootstrapping(false);
      return;
    }

    void (async () => {
      try {
        const refreshed = await refreshSession();
        if (!refreshed) {
          clearSession();
        }
        await loadAccountContext();
      } finally {
        setBootstrapping(false);
      }
    })();
  }, [loadAccountContext]);

  useEffect(() => {
    return subscribeSessionChanges(() => {
      const session = getSession();
      if (!session) {
        resetClientState({ clearAccountContext: true });
        setView("profile");
        setBootstrapping(false);
      }
    });
  }, [resetClientState]);

  useEffect(() => {
    if (view !== "profile" && !hasCharacterContext) {
      setView("profile");
    }
  }, [hasCharacterContext, view]);

  useEffect(() => {
    if (!hasActiveSession || !selectedCharacter?.id) {
      return;
    }

    void loadGameplay();
    const timer = window.setInterval(() => {
      void loadGameplay();
    }, gameplayRefreshMs);

    return () => window.clearInterval(timer);
  }, [hasActiveSession, loadGameplay, selectedCharacter]);

  useEffect(() => {
    if (!hasActiveSession || !selectedCharacter?.id) {
      return;
    }
    void loadGameplay();
  }, [
    candleBucketTicks,
    candleWindowRange,
    hasActiveSession,
    loadGameplay,
    marketOverviewBucketTicks,
    marketOverviewWindowRange,
    marketSymbol,
    selectedCharacter?.id
  ]);

  useEffect(() => {
    if (!hasActiveSession || !selectedCharacter?.id) {
      return;
    }

    if (view === "chat" || channels.length === 0) {
      void loadChat();
    }

    const intervalMs = view === "chat" ? chatRefreshActiveMs : chatRefreshBackgroundMs;
    const timer = window.setInterval(() => {
      void loadChat();
    }, intervalMs);

    return () => window.clearInterval(timer);
  }, [channels.length, hasActiveSession, loadChat, selectedCharacter, view]);

  useEffect(() => {
    const tick = stream.event?.tick;
    if (!tick || !hasActiveSession || !selectedCharacter?.id) {
      return;
    }

    if (lastStreamTickRef.current === tick) {
      return;
    }

    lastStreamTickRef.current = tick;
    void loadGameplay();
    if (view === "chat") {
      void loadChat();
    }
  }, [hasActiveSession, loadChat, loadGameplay, selectedCharacter, stream.event?.tick, view]);

  const onAuthSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setActionBusy(true);
    setNotice(null);

    try {
      const data = authMode === "login" ? await login(authUsername, authPassword) : await register(authUsername, authPassword);
      setSession({ accessToken: data.accessToken, refreshToken: data.refreshToken } as Tokens, data.account);
      setAuthPassword("");
      resetClientState();
      setNotice(authMode === "login" ? "Logged in." : "Registered and logged in.");
      await loadAccountContext();
      setView("profile");
    } catch (err) {
      handleError(err);
    } finally {
      setActionBusy(false);
    }
  };

  const onStartOnboarding = async () => {
    const parsedRealm = parseNumber(String(selectedRealmId)) ?? 1;
    setActionBusy(true);
    try {
      await startOnboarding(newCharacterName, parsedRealm);
      setNotice("Character created.");
      await loadAccountContext();
    } catch (err) {
      handleError(err);
    } finally {
      setActionBusy(false);
    }
  };

  const onQueueBehavior = async () => {
    if (!selectedBehaviorKey || !selectedCharacter?.id) {
      return;
    }
    setActionBusy(true);
    try {
      const selected = queueAvailableBehaviors.find((entry) => entry.key === selectedBehaviorKey);
      await startBehavior(
        selectedBehaviorKey,
        selectedCharacter.id,
        selected?.requiresMarketOpen ? marketWait : undefined,
        queueMode,
        queueMode === "repeat-until" ? queueRepeatUntil : undefined
      );
      setNotice("Behavior queued.");
      setQueueModalOpen(false);
      await loadGameplay();
    } catch (err) {
      handleError(err);
    } finally {
      setActionBusy(false);
    }
  };

  const onCancelBehavior = async (behaviorId: number) => {
    if (!selectedCharacter?.id) {
      return;
    }

    setActionBusy(true);
    try {
      await cancelBehavior(behaviorId, selectedCharacter.id);
      setNotice("Behavior cancelled.");
      await loadGameplay();
    } catch (err) {
      handleError(err);
    } finally {
      setActionBusy(false);
    }
  };

  const onPurchaseUpgrade = async (upgradeKey: string) => {
    if (!selectedCharacter?.id) {
      return;
    }

    setActionBusy(true);
    try {
      await purchaseUpgrade(upgradeKey, selectedCharacter.id);
      setNotice("Upgrade purchased.");
      await loadGameplay();
    } catch (err) {
      handleError(err);
    } finally {
      setActionBusy(false);
    }
  };

  const onPlaceMarketOrder = async () => {
    if (!selectedCharacter?.id) {
      return;
    }
    const limitPrice = parsedLimitPrice;
    const quantity = selectedQuantityUnits;
    if (!limitPrice || limitPrice <= 0 || quantity <= 0) {
      pushToast("error", "Quantity and limit price must be positive values.");
      return;
    }

    if (marketSide === "sell" && quantity > playerMarketSymbolInventory) {
      pushToast("error", `Not enough ${marketSymbol} inventory for this order.`);
      return;
    }
    if (marketSide === "buy" && quantity*limitPrice > playerCoins) {
      pushToast("error", "Not enough coins for this buy order escrow.");
      return;
    }

    setActionBusy(true);
    try {
      await placeMarketOrder({
        itemKey: marketSymbol,
        side: marketSide,
        quantity,
        limitPrice,
        cancelAfter: marketCancelAfter
      }, selectedCharacter.id);
      setNotice("Market order placed.");
      await loadGameplay();
    } catch (err) {
      handleError(err);
    } finally {
      setActionBusy(false);
    }
  };

  const onCancelMarketOrder = async (orderId: number) => {
    if (!selectedCharacter?.id) {
      return;
    }

    setActionBusy(true);
    try {
      await cancelMarketOrder(orderId, selectedCharacter.id);
      setNotice("Market order cancelled.");
      await loadGameplay();
    } catch (err) {
      handleError(err);
    } finally {
      setActionBusy(false);
    }
  };

  const onAscend = async () => {
    if (!selectedCharacter?.id) {
      return;
    }
    setActionBusy(true);
    try {
      await ascend(undefined, selectedCharacter.id);
      setNotice("Ascension completed.");
      await loadGameplay();
    } catch (err) {
      handleError(err);
    } finally {
      setActionBusy(false);
    }
  };

  const onSendChat = async (event: FormEvent) => {
    event.preventDefault();
    if (!chatDraft.trim() || !selectedCharacter?.id || !hasSelectedChatChannel) {
      return;
    }

    setActionBusy(true);
    try {
      await postChatMessage(chatDraft.trim(), chatChannel, selectedCharacter.id);
      setChatDraft("");
      await loadChat();
    } catch (err) {
      handleError(err);
    } finally {
      setActionBusy(false);
    }
  };

  const onLogout = async () => {
    setActionBusy(true);
    try {
      await logout();
      resetClientState({ clearAccountContext: true });
      setNotice("Logged out.");
    } catch (err) {
      handleError(err);
    } finally {
      setActionBusy(false);
    }
  };

  const handleAdminChanged = useCallback(async () => {
    await Promise.all([loadChat(), loadGameplay(), loadAccountContext()]);
  }, [loadAccountContext, loadChat, loadGameplay]);

  if (bootstrapping) {
    return (
      <div className="new-shell">
        <div className="auth-card">
          <h1>Lived</h1>
          <p>Loading account context...</p>
        </div>
      </div>
    );
  }

  if (!isLoggedIn) {
    return (
      <div className="new-shell">
        <div className="auth-card">
          <h1>Lived</h1>
          <p>Sign in to continue your run, or create an account to begin.</p>

          <div className="segmented">
            <button className={authMode === "login" ? "active" : ""} onClick={() => setAuthMode("login")} type="button">Login</button>
            <button className={authMode === "register" ? "active" : ""} onClick={() => setAuthMode("register")} type="button">Register</button>
          </div>

          <form onSubmit={onAuthSubmit} className="column-form">
            <label>
              Username
              <input value={authUsername} onChange={(event) => setAuthUsername(event.target.value)} minLength={3} required />
            </label>
            <label>
              Password
              <input type="password" value={authPassword} onChange={(event) => setAuthPassword(event.target.value)} minLength={8} required />
            </label>
            <button type="submit" disabled={actionBusy}>{actionBusy ? "Working..." : authMode === "login" ? "Login" : "Create account"}</button>
          </form>
        </div>
      </div>
    );
  }

  return (
    <div className="new-shell">
      <div className="app-header-chrome">
        <header className="top-nav">
          <div>
            <h1>Lived</h1>
            <p>Web client {webPackage.version}</p>
          </div>

          <div className="top-controls">
            <label>
              Character
              <select
                value={selectedCharacter?.id ?? ""}
                onChange={(event) => onCharacterSwitch(parseNumber(event.target.value))}
              >
                {characters.map((character) => (
                  <option key={character.id} value={character.id}>
                    {character.name} · {formatRealmName(character.realmId)} {character.isPrimary ? "(primary)" : ""}
                  </option>
                ))}
              </select>
            </label>

            <div className="nav-tabs">
              <button className={view === "profile" ? "active" : ""} onClick={() => setView("profile")} type="button">Profile</button>
              {hasCharacterContext ? <button className={view === "gameplay" ? "active" : ""} onClick={() => setView("gameplay")} type="button">Gameplay</button> : null}
              {hasCharacterContext ? <button className={view === "chat" ? "active" : ""} onClick={() => setView("chat")} type="button">Chat</button> : null}
            </div>

            {isAdmin ? <button type="button" onClick={() => setAdminOpen(true)}>Admin Panel</button> : null}
            <button type="button" onClick={onLogout} disabled={actionBusy}>Logout</button>
          </div>
        </header>

        {loading || bootstrapping ? <div className="notice">Loading account context...</div> : null}
      </div>

      <div className="workspace-body">
        <aside className="snapshot-sidebar">
          <PlayerSnapshot
            name={selectedCharacter?.name ?? account?.username ?? "Player"}
            realmId={selectedCharacter?.realmId}
            tick={snapshotTick}
            day={snapshotDay}
            clock={snapshotClock}
            dayPart={snapshotDayPart}
            marketState={snapshotMarketState}
            streamStatus={stream.status}
            staminaCurrent={staminaCurrent}
            staminaMax={staminaMax}
            coins={snapshotCoins}
            queuedOrActive={liveStreamEvent?.player?.queuedOrActiveBehaviors ?? queuedOrActiveCount}
            behaviorBars={snapshotBehaviorBars}
            realmPausedMessage={realmPausedMessage}
          />
        </aside>

        <main className="page-grid main-scroll">
        {view === "profile" ? (
          <section className="panel">
            <h2>User Profile</h2>
            <div className="profile-grid">
              <div>
                <h3>Account</h3>
                <p><strong>ID:</strong> {account?.id}</p>
                <p><strong>Username:</strong> {account?.username}</p>
                <p><strong>Status:</strong> {account?.status}</p>
                <p><strong>Roles:</strong> {(account?.roles ?? []).join(", ")}</p>
              </div>

              <div>
                <h3>Characters</h3>
                <table className="mini-table">
                  <thead>
                    <tr>
                      <th>Name</th>
                      <th>Realm</th>
                      <th>Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {characters.map((character) => (
                      <tr key={character.id}>
                        <td>{character.name}{character.isPrimary ? " ⭐" : ""}</td>
                        <td>{formatRealmName(character.realmId)}</td>
                        <td>{character.status}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              <div>
                <h3>Onboard New Character</h3>
                <div className="column-form">
                  <label>
                    Name
                    <input value={newCharacterName} onChange={(event) => setNewCharacterName(event.target.value)} minLength={3} maxLength={64} />
                  </label>
                  <label>
                    Realm
                    <select value={selectedRealmId} onChange={(event) => setSelectedRealmId(Number(event.target.value) || 1)}>
                      {onboardingRealmOptions.map((realmId) => (
                        <option key={realmId} value={realmId}>{formatRealmName(realmId)}</option>
                      ))}
                    </select>
                  </label>
                  {selectedOnboardingRealm?.whitelistOnly ? (
                    <p className="muted">
                      {selectedOnboardingRealm.canCreateCharacter ? "This realm is whitelisted; your account is approved." : "This realm is whitelisted; request admin approval before creating a character."}
                    </p>
                  ) : null}
                  <button type="button" onClick={onStartOnboarding} disabled={actionBusy || onboardingBlockedByWhitelist}>Create character</button>
                </div>
              </div>
            </div>
          </section>
        ) : null}

        {view === "gameplay" ? (
          <section className="panel">
            <div className="panel-header-row">
              <h2>Gameplay</h2>
              <div className="nav-tabs">
                <button className={gameplayTab === "overview" ? "active" : ""} onClick={() => setGameplayTab("overview")} type="button">Overview</button>
                <button className={gameplayTab === "queue" ? "active" : ""} onClick={() => setGameplayTab("queue")} type="button">Queue</button>
                <button className={gameplayTab === "progression" ? "active" : ""} onClick={() => setGameplayTab("progression")} type="button">Progression</button>
                <button className={gameplayTab === "market" ? "active" : ""} onClick={() => setGameplayTab("market")} type="button">Market</button>
                <button className={gameplayTab === "market-overview" ? "active" : ""} onClick={() => setGameplayTab("market-overview")} type="button">Market Overview</button>
                <button className={gameplayTab === "inventory" ? "active" : ""} onClick={() => setGameplayTab("inventory")} type="button">Inventory</button>
                <button className={gameplayTab === "feed" ? "active" : ""} onClick={() => setGameplayTab("feed")} type="button">Feed</button>
              </div>
            </div>

            {gameplayTab === "overview" ? (
              <div className="game-overview-grid">
                <div className="info-card">
                  <h3>Character Snapshot</h3>
                  <p><strong>Name:</strong> {selectedCharacter?.name ?? "-"}</p>
                  <p><strong>Realm:</strong> {formatRealmName(selectedCharacter?.realmId)}</p>
                  <p><strong>Tick:</strong> {playerStatus?.simulationTick ?? 0}</p>
                  <p><strong>Ascensions:</strong> {playerStatus?.ascensionCount ?? 0}</p>
                  <p><strong>Wealth bonus:</strong> {playerStatus?.wealthBonusPct ?? 0}%</p>
                </div>

                <div className="info-card">
                  <h3>Stamina</h3>
                  <div className="progress-wrap" title={`${staminaCurrent} / ${staminaMax}`}>
                    <div className="progress-bar" style={{ width: `${staminaPercent}%` }} />
                  </div>
                  <p>{staminaCurrent} / {staminaMax}</p>
                  <button type="button" onClick={onAscend} disabled={actionBusy || !playerStatus?.ascension?.available}>Ascend</button>
                  {!playerStatus?.ascension?.available ? <p className="muted">{playerStatus?.ascension?.reason ?? "Ascension unavailable"}</p> : null}
                </div>

                <div className="info-card">
                  <h3>Market</h3>
                  <p><strong>Status:</strong> {marketStatus?.sessionState ?? "-"}</p>
                  <p><strong>Open:</strong> {marketStatus?.isOpen ? "Yes" : "No"}</p>
                  <p><strong>Opens:</strong> {formatMarketCountdown(marketStatus?.minutesToOpen)}</p>
                  <p><strong>Closes:</strong> {formatMarketCountdown(marketStatus?.minutesToClose)}</p>
                </div>

                <div className="info-card stat-display-card">
                  <h3>Attributes</h3>
                  <p className="muted stat-section-note" title="Attributes are trained directly through behaviors and progression.">
                    Trained directly. Endurance currently boosts max stamina and stamina recovery.
                  </p>
                  {coreStatRows.length === 0 ? (
                    <p className="muted">No stats available.</p>
                  ) : (
                    <div className="stat-grid stat-grid-core">
                      {coreStatRows.map((row) => {
                        const hasValue = typeof row.value === "number";
                        const hasMax = typeof row.max === "number";
                        const value = hasValue ? row.value : undefined;
                        const max = hasMax ? row.max : undefined;
                        const percent = value !== undefined && max !== undefined && max > 0
                          ? Math.min(100, Math.max(0, (value / max) * 100))
                          : undefined;
                        const context = statContextHint(row.id, "core");
                        return (
                          <article key={row.id} className="stat-tile stat-tile-core">
                            <div className="stat-head-row">
                              <span className="stat-label">{row.label}</span>
                              <span className="stat-badge stat-badge-core">trainable</span>
                            </div>
                            <div className="stat-values">
                              {value !== undefined ? <strong>{value}</strong> : <strong>-</strong>}
                              {max !== undefined ? <span className="muted">/ {max}</span> : null}
                            </div>
                            {context ? <p className="stat-context">{context}</p> : null}
                            {percent !== undefined ? (
                              <div className="progress-wrap stat-progress" title={`${value} / ${max}`}>
                                <div className="progress-bar" style={{ width: `${percent}%` }} />
                              </div>
                            ) : null}
                          </article>
                        );
                      })}
                    </div>
                  )}
                </div>

                <div className="info-card stat-display-card">
                  <h3>Vitals</h3>
                  <p className="muted stat-section-note" title="Vitals are derived from attributes and current state each tick.">
                    Derived values. Current endurance contribution: {enduranceValue}.
                  </p>
                  {derivedStatRows.length === 0 ? (
                    <p className="muted">No derived stats available.</p>
                  ) : (
                    <div className="stat-grid stat-grid-derived">
                      {derivedStatRows.map((row) => {
                        const hasValue = typeof row.value === "number";
                        const hasMax = typeof row.max === "number";
                        const value = hasValue ? row.value : undefined;
                        const max = hasMax ? row.max : undefined;
                        const percent = value !== undefined && max !== undefined && max > 0
                          ? Math.min(100, Math.max(0, (value / max) * 100))
                          : undefined;
                        const context = statContextHint(row.id, "derived");
                        return (
                          <article key={row.id} className="stat-tile stat-tile-derived">
                            <div className="stat-head-row">
                              <span className="stat-label">{row.label}</span>
                              <span className="stat-badge stat-badge-derived">derived</span>
                            </div>
                            <div className="stat-values">
                              {value !== undefined ? <strong>{value}</strong> : <strong>-</strong>}
                              {max !== undefined ? <span className="muted">/ {max}</span> : null}
                            </div>
                            {context ? <p className="stat-context">{context}</p> : null}
                            {percent !== undefined ? (
                              <div className="progress-wrap stat-progress" title={`${value} / ${max}`}>
                                <div className="progress-bar" style={{ width: `${percent}%` }} />
                              </div>
                            ) : null}
                          </article>
                        );
                      })}
                    </div>
                  )}
                </div>
              </div>
            ) : null}

            {gameplayTab === "queue" ? (
              <div className="queue-stack">
                <div className="queue-slot-strip">
                  <article className="queue-slot-card">
                    <span>Available Slots</span>
                    <strong>{queueSlotsAvailable !== undefined ? queueSlotsAvailable : "∞"}</strong>
                    <small>{queueSlotsTotal !== undefined ? `${queueSlotsUsed} used of ${queueSlotsTotal}` : "No slot cap yet; upgrade-ready UI"}</small>
                  </article>
                  <article className="queue-slot-card">
                    <span>Active Behaviors</span>
                    <strong>{activeQueueCount}</strong>
                    <small>Currently executing</small>
                  </article>
                  <article className="queue-slot-card">
                    <span>Queued Behaviors</span>
                    <strong>{queuedOnlyCount}</strong>
                    <small>Waiting to begin</small>
                  </article>
                  <article className="queue-slot-card">
                    <span>Ready to Queue</span>
                    <strong>{queueAvailableBehaviors.length}</strong>
                    <small>{inaccessibleQueueCount > 0 ? `${inaccessibleQueueCount} hidden for progression` : "All visible queue actions are ready"}</small>
                  </article>
                </div>

                <div className="split-grid">
                  <div className="queue-panel">
                    <div className="queue-actions">
                      <h3>Current Queue</h3>
                      <button type="button" onClick={() => setQueueModalOpen(true)}>Queue behavior</button>
                    </div>
                    {inaccessibleQueueCount > 0 ? (
                      <p className="muted queue-hidden-note">{inaccessibleQueueCount} inaccessible behaviors are hidden from queueing and reserved for the progression view.</p>
                    ) : null}
                    {queueActiveRows.length === 0 ? (
                      <p className="muted queue-empty">No queued or active behaviors right now.</p>
                    ) : (
                      <table className="mini-table">
                        <thead>
                          <tr>
                            <th>Key</th>
                            <th>Schedule</th>
                            <th>State</th>
                            <th>Progress</th>
                            <th>Start</th>
                            <th>Complete</th>
                            <th>Remaining</th>
                            <th>Action</th>
                          </tr>
                        </thead>
                        <tbody>
                          {queueActiveRows.map((behavior) => (
                            <tr key={behavior.id}>
                              <td title={behavior.key}>{displayBehaviorLabel(behavior.key)}</td>
                              <td>{formatBehaviorSchedule(behavior)}</td>
                              <td><span className={`queue-state queue-state-${behavior.state}`} title={queuedStateTitle(behavior)}>{behavior.state}</span></td>
                              <td>
                                <div className="queue-progress-wrap" title={`${behaviorProgressPct(queueCurrentTick, behavior)}%`}>
                                  <div className="queue-progress-fill" style={{ width: `${behaviorProgressPct(queueCurrentTick, behavior)}%` }} />
                                </div>
                              </td>
                              <td>{formatWorldTime(behavior.startedAtTick || behavior.scheduledAtTick)}</td>
                              <td>{formatWorldTime(behavior.completesAtTick)}</td>
                              <td>{formatRemainingMinutes(queueCurrentTick, behavior.completesAtTick)}</td>
                              <td>
                                <button type="button" onClick={() => onCancelBehavior(behavior.id)} disabled={actionBusy}>Cancel</button>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    )}
                  </div>

                  <div className="queue-panel">
                    <h3>Recent Results</h3>
                    {queueHistoryRows.length === 0 ? (
                      <p className="muted queue-empty">No completed, failed, or cancelled behaviors yet.</p>
                    ) : (
                      <table className="mini-table">
                        <thead>
                          <tr>
                            <th>Key</th>
                            <th>Schedule</th>
                            <th>State</th>
                            <th>Completed</th>
                            <th>Result</th>
                          </tr>
                        </thead>
                        <tbody>
                          {queueHistoryRows.map((behavior) => (
                            <tr key={behavior.id}>
                              <td title={behavior.key}>{displayBehaviorLabel(behavior.key)}</td>
                              <td>{formatBehaviorSchedule(behavior)}</td>
                              <td><span className={`queue-state queue-state-${behavior.state}`}>{behavior.state}</span></td>
                              <td>{formatWorldTime(behavior.completesAtTick)}</td>
                              <td className="queue-result">{compactResultMessage(behavior)}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    )}
                  </div>
                </div>
              </div>
            ) : null}

            {gameplayTab === "progression" ? (
              <div className="progression-grid">
                <div className="queue-panel">
                  <h3>Future and Locked Behaviors</h3>
                  {progressionLockedBehaviors.length === 0 ? (
                    <p className="muted">No locked behaviors. You are currently able to queue all known actions.</p>
                  ) : (
                    <div className="progression-list">
                      {progressionLockedBehaviors.map((behavior) => (
                        <article key={behavior.key} className="progression-card">
                          <div className="queue-catalog-head">
                            <strong>{behaviorDisplayLabel(behavior, behavior.key)}</strong>
                            <span className="queue-category-badge">{behaviorCategory(behavior, behavior.key)}</span>
                          </div>
                          {behavior.summary ? <p className="muted">{behavior.summary}</p> : null}
                          <div className="queue-catalog-meta">
                            <span>{behavior.durationMinutes}m</span>
                            {behavior.staminaCost ? <span>Stamina {behavior.staminaCost}</span> : <span>No stamina cost</span>}
                          </div>
                          {behavior.unavailableReason ? <p className="queue-catalog-warning">{behavior.unavailableReason}</p> : null}
                        </article>
                      ))}
                    </div>
                  )}
                </div>

                <div className="queue-panel">
                  <h3>Consumed This Ascension</h3>
                  {progressionConsumedBehaviors.length === 0 ? (
                    <p className="muted">No single-use behaviors consumed yet this ascension.</p>
                  ) : (
                    <div className="progression-list">
                      {progressionConsumedBehaviors.map((behavior) => (
                        <article key={behavior.key} className="progression-card progression-card-consumed">
                          <div className="queue-catalog-head">
                            <strong>{behaviorDisplayLabel(behavior, behavior.key)}</strong>
                            <span className="queue-category-badge">Consumed</span>
                          </div>
                          {behavior.summary ? <p className="muted">{behavior.summary}</p> : null}
                          <p className="muted">Returns next ascension after reset.</p>
                        </article>
                      ))}
                    </div>
                  )}
                </div>

                <div className="queue-panel progression-upgrades-panel">
                  <h3>Upgrade Tree</h3>
                  {sortedUpgradeCatalog.length === 0 ? (
                    <p className="muted">No upgrades are currently defined.</p>
                  ) : (
                    <div className="progression-list">
                      {sortedUpgradeCatalog.map((upgrade) => {
                        const maxPurchases = upgrade.maxPurchases ?? 0;
                        const purchaseCount = upgrade.purchaseCount ?? 0;
                        const maxed = maxPurchases > 0 && purchaseCount >= maxPurchases;
                        return (
                          <article key={upgrade.key} className="progression-card">
                            <div className="queue-catalog-head">
                              <strong>{upgrade.name ?? upgrade.key}</strong>
                              <span className="queue-category-badge">{upgrade.category ?? "Upgrade"}</span>
                            </div>
                            {upgrade.summary ? <p className="muted">{upgrade.summary}</p> : null}
                            <div className="queue-catalog-meta">
                              <span>Purchased: {purchaseCount}{maxPurchases > 0 ? `/${maxPurchases}` : ""}</span>
                              {upgrade.gateTypes && upgrade.gateTypes.length > 0 ? <span>Gates: {upgrade.gateTypes.join(", ")}</span> : null}
                              {upgrade.outputs?.queueSlotsDelta ? <span>Queue slots +{upgrade.outputs.queueSlotsDelta}</span> : null}
                              {upgrade.costScaling && upgrade.costScaling !== 1 ? <span>Cost x{upgrade.costScaling.toFixed(2)}</span> : null}
                              {upgrade.outputScaling && upgrade.outputScaling !== 1 ? <span>Output x{upgrade.outputScaling.toFixed(2)}</span> : null}
                            </div>
                            <p className="muted">{formatUpgradeProjection(upgrade)}</p>
                            {upgrade.unavailableReason ? <p className="queue-catalog-warning">{upgrade.unavailableReason}</p> : null}
                            <button
                              type="button"
                              onClick={() => onPurchaseUpgrade(upgrade.key)}
                              disabled={actionBusy || !upgrade.available || maxed}
                            >
                              {maxed ? "Maxed" : "Purchase"}
                            </button>
                          </article>
                        );
                      })}
                    </div>
                  )}
                </div>
              </div>
            ) : null}

            {gameplayTab === "market" ? (
              <div className="market-layout">
                <div className="queue-panel market-observe-panel">
                  <div className="panel-header-row">
                    <h3>Market Observatory ({marketSymbol})</h3>
                    <div className="button-row">
                      <label>
                        Candle Bucket
                        <select value={candleBucketTicks} onChange={(event) => setCandleBucketTicks(Number(event.target.value) || 30)}>
                          <option value={10}>10m</option>
                          <option value={30}>30m</option>
                          <option value={60}>1h</option>
                          <option value={180}>3h</option>
                        </select>
                      </label>
                      <label>
                        Candle Window
                        <select value={candleWindowRange} onChange={(event) => setCandleWindowRange(event.target.value)}>
                          <option value="12h">12h</option>
                          <option value="24h">24h</option>
                          <option value="3d">3d</option>
                          <option value="7d">7d</option>
                        </select>
                      </label>
                    </div>
                  </div>
                  <p className="muted">API-supplied candles, spread, flow, and depth update every market poll.</p>

                  <div className="market-candle-panel">
                    <div className="market-legend-row">
                      <span><strong>Candles</strong> body open-close, wick high-low</span>
                      <span><strong>Axis</strong> price range (left), time range (bottom)</span>
                      <span><strong>Applied Bucket</strong> {marketAppliedBucketTicks}m</span>
                    </div>
                    {visibleMarketCandles.length <= 0 ? (
                      <p className="muted">No history yet for candle rendering.</p>
                    ) : (
                      <>
                        <div className="market-candle-axis-wrap">
                          <div className="market-candle-y-axis" aria-hidden="true">
                            <span>{marketCandleRange.max}</span>
                            <span>{marketCandleRange.min}</span>
                          </div>
                          <div className="market-candle-chart" role="img" aria-label="Candlestick chart with price axis">
                            {visibleMarketCandles.map((candle) => {
                              const toY = (price: number) => ((marketCandleRange.max - price) / marketCandleRange.span) * 100;
                              const wickTop = toY(candle.high);
                              const wickBottom = toY(candle.low);
                              const bodyTop = Math.min(toY(candle.open), toY(candle.close));
                              const bodyBottom = Math.max(toY(candle.open), toY(candle.close));
                              const bodyHeight = Math.max(2, bodyBottom - bodyTop);
                              const isUp = candle.close >= candle.open;
                              return (
                                <div key={`candle-${candle.bucketStartTick}`} className="market-candle" title={`Tick ${candle.bucketStartTick} O:${candle.open} H:${candle.high} L:${candle.low} C:${candle.close}`}>
                                  <div className={`market-candle-wick ${isUp ? "up" : "down"}`} style={{ top: `${wickTop}%`, height: `${Math.max(2, wickBottom - wickTop)}%` }} />
                                  <div className={`market-candle-body ${isUp ? "up" : "down"}`} style={{ top: `${bodyTop}%`, height: `${bodyHeight}%` }} />
                                </div>
                              );
                            })}
                          </div>
                        </div>
                        <div className="market-candle-x-axis" aria-hidden="true">
                          <span>{formatWorldTime(visibleMarketCandles[0]?.bucketStartTick)}</span>
                          <span>{formatWorldTime(visibleMarketCandles[visibleMarketCandles.length - 1]?.bucketStartTick)}</span>
                        </div>
                      </>
                    )}
                  </div>
                  <div className="market-chart-card">
                    {marketCandleTrendPath ? (
                      <svg viewBox="0 0 100 36" className="market-sparkline" role="img" aria-label="Close-price trend line">
                        <polyline points={marketCandleTrendPath} className="market-sparkline-path" />
                      </svg>
                    ) : (
                      <p className="muted">Not enough candle points for trend line.</p>
                    )}
                  </div>
                  <div className="market-metrics-grid">
                    <article className="market-metric-card">
                      <span>Last</span>
                      <strong>{marketTicker?.price ?? "-"}</strong>
                      <small>Tick delta {formatSignedNumber(marketTicker?.delta)}</small>
                    </article>
                    <article className="market-metric-card">
                      <span>Spread</span>
                      <strong>{marketSpread ?? "-"}</strong>
                      <small>Bid {marketTopBid ?? "-"} Ask {marketTopAsk ?? "-"}</small>
                    </article>
                    <article className="market-metric-card">
                      <span>Mid</span>
                      <strong>{marketMidPrice ?? "-"}</strong>
                      <small>Top-of-book midpoint</small>
                    </article>
                    <article className="market-metric-card">
                      <span>VWAP</span>
                      <strong>{marketFlowSummary.vwap ? marketFlowSummary.vwap.toFixed(2) : "-"}</strong>
                      <small>Volume {marketFlowSummary.volume}</small>
                    </article>
                    <article className="market-metric-card">
                      <span>NPC Participation</span>
                      <strong>{formatPercent(marketMovementView?.npcTradeSharePct ?? marketFlowSummary.npcSharePct)}</strong>
                      <small>{marketMovementView?.npcParticipantTrades ?? marketFlowSummary.npcInvolved}/{marketMovementView?.trades ?? marketFlowSummary.trades} trades involved NPCs</small>
                    </article>
                    <article className="market-metric-card">
                      <span>Window Move</span>
                      <strong>{formatSignedNumber(marketMovementView?.windowChange ?? marketFlowSummary.lastMove)}</strong>
                      <small>Range {marketMovementView?.windowRange ?? "-"} over {marketMovementView?.windowTicks ?? "-"} ticks</small>
                    </article>
                    <article className="market-metric-card">
                      <span>Regime</span>
                      <strong className="market-metric-wrap">{marketRegime}</strong>
                      <small>Derived from 24h move + inventory utilization</small>
                    </article>
                    <article className="market-metric-card">
                      <span>Last Driver</span>
                      <strong className="market-metric-wrap">{marketTicker?.lastSource ?? "-"}</strong>
                      <small>Includes `npc_cycle` and orderbook events</small>
                    </article>
                    <article className="market-metric-card">
                      <span>Market Cap (Est.)</span>
                      <strong>{marketLiquidityView?.capEstimate ?? "-"}</strong>
                      <small>Price x NPC liquidity quantity</small>
                    </article>
                    <article className="market-metric-card">
                      <span>Inventory Utilization</span>
                      <strong>{formatPercent(marketLiquidityView?.utilizationPct)}</strong>
                      <small>Qty {marketLiquidityView?.quantity ?? "-"} baseline {marketLiquidityView?.baselineQuantity ?? "-"}</small>
                    </article>
                    <article className="market-metric-card">
                      <span>Inventory Pressure</span>
                      <strong>{formatSignedNumber(marketLiquidityView?.lastPressure)}</strong>
                      <small>Min {marketLiquidityView?.minQuantity ?? "-"} max {marketLiquidityView?.maxQuantity ?? "-"}</small>
                    </article>
                    <article className="market-metric-card">
                      <span>Driver Mix</span>
                      <strong>Story {marketMovementView?.storytellerMoves ?? 0} / NPC {marketMovementView?.npcCycleMoves ?? 0} / OB {marketMovementView?.orderbookMoves ?? 0}</strong>
                      <small>24h price moves by source (story, `npc_cycle`, orderbook)</small>
                    </article>
                  </div>

                  <div className="market-depth-grid">
                    <div>
                      <h4>Bid Ladder</h4>
                      {marketDepthView.buys.length === 0 ? <p className="muted">No bids.</p> : null}
                      {marketDepthView.buys.map((entry) => (
                        <div key={`depth-buy-${entry.price}-${entry.quantity}`} className="market-depth-row">
                          <span>{entry.price}</span>
                          <div className="market-depth-bar-wrap">
                            <div className="market-depth-bar buy" style={{ width: `${entry.widthPct}%` }} />
                          </div>
                          <span>{entry.quantity}</span>
                        </div>
                      ))}
                    </div>
                    <div>
                      <h4>Ask Ladder</h4>
                      {marketDepthView.sells.length === 0 ? <p className="muted">No asks.</p> : null}
                      {marketDepthView.sells.map((entry) => (
                        <div key={`depth-sell-${entry.price}-${entry.quantity}`} className="market-depth-row">
                          <span>{entry.price}</span>
                          <div className="market-depth-bar-wrap">
                            <div className="market-depth-bar sell" style={{ width: `${entry.widthPct}%` }} />
                          </div>
                          <span>{entry.quantity}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>

                <div className="queue-panel market-trade-panel">
                  <h3>Order Entry</h3>
                  <p className="muted">Realm-local orderbook. Quantities are placed in lots of <code>x100</code> units.</p>
                  <div className="queue-browser-tools">
                    <label>
                      Symbol
                      <select value={marketSymbol} onChange={(event) => setMarketSymbol(event.target.value)}>
                        {(marketStatus?.tickers ?? []).map((ticker) => (
                          <option key={ticker.symbol} value={ticker.symbol}>{ticker.symbol}</option>
                        ))}
                      </select>
                    </label>
                    <label>
                      Side
                      <select value={marketSide} onChange={(event) => setMarketSide(event.target.value as "buy" | "sell")}>
                        <option value="buy">Buy</option>
                        <option value="sell">Sell</option>
                      </select>
                    </label>
                    <label>
                      Quantity Lots (x100)
                      <select value={marketQuantityLots} onChange={(event) => setMarketQuantityLots(event.target.value)}>
                        <option value="1">1 (100)</option>
                        <option value="2">2 (200)</option>
                        <option value="3">3 (300)</option>
                        <option value="4">4 (400)</option>
                        <option value="5">5 (500)</option>
                        <option value="6">6 (600)</option>
                        <option value="7">7 (700)</option>
                        <option value="8">8 (800)</option>
                        <option value="9">9 (900)</option>
                        <option value="10">10 (1000)</option>
                      </select>
                    </label>
                    <label>
                      Limit Price
                      <input value={marketLimitPrice} onChange={(event) => setMarketLimitPrice(event.target.value)} placeholder="8" />
                    </label>
                    <label>
                      Auto-cancel
                      <select value={marketCancelAfter} onChange={(event) => setMarketCancelAfter(event.target.value)}>
                        <option value="12h">12h</option>
                        <option value="24h">24h</option>
                        <option value="72h">72h</option>
                        <option value="168h">168h</option>
                      </select>
                    </label>
                    <button type="button" onClick={onPlaceMarketOrder} disabled={actionBusy}>Place Order</button>
                  </div>
                  <p className="muted market-trade-holdings">
                    You have {playerMarketSymbolInventory} {marketSymbol} ({maxSellLots} lots) and {playerCoins} coins.
                    {marketSide === "buy" ? ` Max buy lots at current limit: ${maxBuyLots}.` : ` Max sell lots right now: ${maxSellLots}.`}
                    {parsedLimitPrice ? ` Selected order: ${selectedQuantityUnits} units at ${parsedLimitPrice} each.` : " Enter a limit price to preview escrow."}
                  </p>
                </div>

                <div className="queue-panel market-orders-panel">
                  <h3>My Open Orders</h3>
                  {myMarketOrders.length === 0 ? (
                    <p className="muted">No open market orders.</p>
                  ) : (
                    <table className="mini-table market-open-orders-table">
                      <thead>
                        <tr>
                          <th>Symbol</th>
                          <th>Side</th>
                          <th>Open</th>
                          <th>Limit</th>
                          <th>Expires In</th>
                          <th>Cancel</th>
                        </tr>
                      </thead>
                      <tbody>
                        {myMarketOrders.map((order) => (
                          <tr key={order.id}>
                            <td>{order.itemKey}</td>
                            <td>{order.side}</td>
                            <td>{order.quantityOpen}/{order.quantityTotal}</td>
                            <td>{order.limitPrice}</td>
                            <td title={formatWorldTime(order.cancelAfterTick)}>{formatRemainingMinutes(snapshotTick, order.cancelAfterTick)}</td>
                            <td><button type="button" onClick={() => onCancelMarketOrder(order.id)} disabled={actionBusy || order.state !== "open"}>Cancel</button></td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </div>

                <div className="queue-panel market-book-panel">
                  <h3>Order Book ({marketOrderBook?.symbol || marketSymbol})</h3>
                  <div className="split-grid">
                    <div>
                      <h4>Buys</h4>
                      <div className="table-scroll">
                        <table className="mini-table">
                          <thead><tr><th>Price</th><th>Qty</th></tr></thead>
                          <tbody>
                            {(marketOrderBook?.buys ?? []).slice(0, 10).map((order) => (
                              <tr key={`buy-${order.id}`}><td>{order.limitPrice}</td><td>{order.quantityOpen}</td></tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    </div>
                    <div>
                      <h4>Sells</h4>
                      <div className="table-scroll">
                        <table className="mini-table">
                          <thead><tr><th>Price</th><th>Qty</th></tr></thead>
                          <tbody>
                            {(marketOrderBook?.sells ?? []).slice(0, 10).map((order) => (
                              <tr key={`sell-${order.id}`}><td>{order.limitPrice}</td><td>{order.quantityOpen}</td></tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    </div>
                  </div>
                </div>

                <div className="queue-panel market-tape-panel">
                  <h3>Recent Trades</h3>
                  {marketTrades.length === 0 ? (
                    <p className="muted">No recent trades for this symbol.</p>
                  ) : (
                    <div className="table-scroll">
                      <table className="mini-table">
                        <thead>
                          <tr>
                            <th>Tick</th>
                            <th>Symbol</th>
                            <th>Price</th>
                            <th>Qty</th>
                            <th>Flow</th>
                            <th>Buyer</th>
                            <th>Seller</th>
                          </tr>
                        </thead>
                        <tbody>
                          {marketTrades.slice(0, 20).map((trade) => (
                            <tr key={trade.id}>
                              <td>{trade.tick}</td>
                              <td>{trade.itemKey}</td>
                              <td>{trade.price}</td>
                              <td>{trade.quantity}</td>
                              <td>{trade.buyerType === "npc" ? "NPC buy" : trade.sellerType === "npc" ? "NPC sell" : "Player vs Player"}</td>
                              <td><span className={`market-party-chip ${trade.buyerType === "npc" ? "npc" : "player"}`}>{trade.buyerType}</span></td>
                              <td><span className={`market-party-chip ${trade.sellerType === "npc" ? "npc" : "player"}`}>{trade.sellerType}</span></td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </div>
              </div>
            ) : null}

            {gameplayTab === "market-overview" ? (
              <div className="market-overview-layout">
                <div className="queue-panel market-overview-controls-panel">
                  <div className="panel-header-row">
                    <h3>Market Overview</h3>
                    <div className="button-row">
                      <label>
                        Sample Bucket
                        <select value={marketOverviewBucketTicks} onChange={(event) => setMarketOverviewBucketTicks(Number(event.target.value) || 60)}>
                          <option value={30}>30m</option>
                          <option value={60}>1h</option>
                          <option value={180}>3h</option>
                          <option value={360}>6h</option>
                        </select>
                      </label>
                      <label>
                        Time Window
                        <select value={marketOverviewWindowRange} onChange={(event) => setMarketOverviewWindowRange(event.target.value)}>
                          <option value="12h">12h</option>
                          <option value="24h">24h</option>
                          <option value="3d">3d</option>
                          <option value="7d">7d</option>
                        </select>
                      </label>
                    </div>
                  </div>
                  <p className="muted">Compare one or many symbols with a sampled close-price line graph over a configurable time range.</p>
                  <div className="market-overview-symbols">
                    {marketOverviewRows.map((row) => {
                      const selected = marketOverviewSelectedSymbols.includes(row.symbol);
                      return (
                        <label key={`symbol-select-${row.symbol}`} className={`market-overview-chip ${selected ? "active" : ""}`}>
                          <input
                            type="checkbox"
                            checked={selected}
                            onChange={(event) => {
                              const checked = event.target.checked;
                              setMarketOverviewSelectedSymbols((current) => {
                                if (checked) {
                                  if (current.includes(row.symbol)) {
                                    return current;
                                  }
                                  return [...current, row.symbol].slice(-6);
                                }
                                const next = current.filter((symbol) => symbol !== row.symbol);
                                return next;
                              });
                            }}
                          />
                          <span>{row.symbol}</span>
                          <small>{row.currentPrice}</small>
                        </label>
                      );
                    })}
                  </div>
                </div>

                <div className="queue-panel market-overview-graph-panel">
                  {marketOverviewSeries.length <= 0 ? (
                    <p className="muted">Select one or more symbols to draw trend lines.</p>
                  ) : (
                    <>
                      <div className="market-overview-legend">
                        {marketOverviewSeries.map((series, index) => {
                          const color = ["#52c3ff", "#ffb74d", "#58d39f", "#e784a7", "#9da2ff", "#f4ef77"][index % 6];
                          return (
                            <span key={`overview-legend-${series.symbol}`}>
                              <i style={{ backgroundColor: color }} />
                              {series.symbol}
                            </span>
                          );
                        })}
                      </div>
                      <div className="market-overview-chart-wrap">
                        <div className="market-overview-y-axis" aria-hidden="true">
                          <span>{marketOverviewChartBounds.max}</span>
                          <span>{marketOverviewChartBounds.min}</span>
                        </div>
                        <svg viewBox="0 0 100 44" className="market-overview-chart" role="img" aria-label="Market overview line graph with sample points">
                          {marketOverviewSeries.map((series, index) => {
                            const color = ["#52c3ff", "#ffb74d", "#58d39f", "#e784a7", "#9da2ff", "#f4ef77"][index % 6];
                            return (
                              <g key={`overview-line-${series.symbol}`}>
                                <polyline points={series.path} style={{ stroke: color }} className="market-overview-line" />
                                {series.points.map((point) => (
                                  <circle
                                    key={`overview-point-${series.symbol}-${point.tick}`}
                                    cx={point.x}
                                    cy={point.y}
                                    r={0.8}
                                    style={{ fill: color }}
                                  >
                                    <title>{`${series.symbol} tick ${point.tick} close ${point.price}`}</title>
                                  </circle>
                                ))}
                              </g>
                            );
                          })}
                        </svg>
                      </div>
                      <div className="market-candle-x-axis" aria-hidden="true">
                        <span>{formatWorldTime(marketOverviewVisibleRows[0]?.candles?.[0]?.bucketStartTick)}</span>
                        <span>{formatWorldTime(marketOverviewVisibleRows[0]?.candles?.[marketOverviewVisibleRows[0]?.candles.length - 1]?.bucketStartTick)}</span>
                      </div>
                    </>
                  )}
                </div>

                <div className="queue-panel market-overview-panel">
                  <h3>Symbol Snapshot</h3>
                  {marketOverviewRows.length <= 0 ? (
                    <p className="muted">No market symbols available.</p>
                  ) : (
                    <div className="market-overview-grid">
                      {marketOverviewRows.map((row) => (
                        <article key={`overview-${row.symbol}`} className="market-overview-card">
                          <div className="panel-header-row">
                            <strong>{row.symbol}</strong>
                            <span className="muted">Now {row.currentPrice}</span>
                          </div>
                          <p className="muted">Tick delta {formatSignedNumber(row.delta)} · Window move {formatSignedNumber(row.movement?.windowChange ?? row.historyChange)}</p>
                          <p className="muted">Cap est. {row.liquidity?.capEstimate ?? "-"} · NPC trades {formatPercent(row.movement?.npcTradeSharePct)}</p>
                          <p className="muted">Moves Story {row.movement?.storytellerMoves ?? 0} / NPC {row.movement?.npcCycleMoves ?? 0} / OB {row.movement?.orderbookMoves ?? 0}</p>
                          <button type="button" onClick={() => { setMarketSymbol(row.symbol); setGameplayTab("market"); }}>Trade This Symbol</button>
                        </article>
                      ))}
                    </div>
                  )}
                </div>
              </div>
            ) : null}

            {gameplayTab === "inventory" ? (
              <div>
                <h3>Inventory</h3>
                <table className="mini-table">
                  <thead>
                    <tr>
                      <th>Item</th>
                      <th>Qty</th>
                    </tr>
                  </thead>
                  <tbody>
                    {Object.entries(playerInventory?.inventory ?? {}).sort((a, b) => b[1] - a[1]).map(([item, qty]) => (
                      <tr key={item}>
                        <td>{item}</td>
                        <td>{qty}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}

            {gameplayTab === "feed" ? (
              <div className="feed-list">
                {feedEvents.map((event) => (
                  <article key={event.id} className="feed-row">
                    <div>
                      <strong>Day {event.day}</strong> · {event.clock} · tick {event.tick}
                    </div>
                    <div>{event.message}</div>
                    <div className="muted">{event.eventType}</div>
                  </article>
                ))}
              </div>
            ) : null}
          </section>
        ) : null}

        {view === "chat" ? (
          <ChatView
            channels={channels}
            selectedChannel={chatChannel}
            onSelectChannel={setChatChannel}
            messages={messages}
            draft={chatDraft}
            onDraftChange={setChatDraft}
            onSubmit={onSendChat}
            canCompose={hasSelectedChatChannel}
            disabled={actionBusy}
          />
        ) : null}
        </main>
      </div>

      {queueModalOpen ? (
        <div className="modal-backdrop" onClick={() => setQueueModalOpen(false)}>
          <div className="modal-card" onClick={(event) => event.stopPropagation()}>
            <h3>Queue Behavior</h3>
            <div className="queue-browser-tools">
              <label>
                Search
                <input
                  value={queueSearch}
                  onChange={(event) => setQueueSearch(event.target.value)}
                  placeholder="Find behaviors"
                />
              </label>
            </div>
            <div className="queue-category-tabs" role="tablist" aria-label="Behavior categories">
              {queueCategories.map((category) => (
                <button
                  key={category}
                  type="button"
                  className={queueCategoryFilter === category ? "active" : ""}
                  onClick={() => setQueueCategoryFilter(category)}
                >
                  {category}
                </button>
              ))}
            </div>
            {inaccessibleQueueCount > 0 ? (
              <p className="muted queue-hidden-note">{inaccessibleQueueCount} inaccessible behaviors are hidden here and will appear in progression.</p>
            ) : null}

            <div className="queue-catalog-grid" role="listbox" aria-label="Available behaviors">
              {filteredQueueBehaviors.length === 0 ? (
                <p className="muted">No available behaviors match your current filter.</p>
              ) : (
                filteredQueueBehaviors.map((behavior) => {
                  const conflict = queueConflictReasonForBehavior(behavior);
                  const selected = selectedBehaviorKey === behavior.key;
                  const blocked = conflict !== "";
                  return (
                    <button
                      key={behavior.key}
                      type="button"
                      className={`queue-catalog-card${selected ? " selected" : ""}${blocked ? " blocked" : ""}`}
                      onClick={() => setSelectedBehaviorKey(behavior.key)}
                    >
                      <div className="queue-catalog-head">
                        <strong>{behaviorDisplayLabel(behavior, behavior.key)}</strong>
                        <span className="queue-category-badge">{behaviorCategory(behavior, behavior.key)}</span>
                      </div>
                      {behavior.summary ? <p className="muted">{behavior.summary}</p> : null}
                      <div className="queue-catalog-meta">
                        <span>{behavior.durationMinutes}m</span>
                        {behavior.staminaCost ? <span>Stamina {behavior.staminaCost}</span> : <span>No stamina cost</span>}
                        {behavior.exclusiveGroup ? <span>Group: {formatExclusiveGroupLabel(behavior.exclusiveGroup)}</span> : null}
                        {behavior.requiresNight ? <span>Night only</span> : null}
                        {formatBehaviorRequirements(behavior) ? <span>Requires: {formatBehaviorRequirements(behavior)}</span> : null}
                      </div>
                      {blocked ? <p className="queue-catalog-warning">{conflict}</p> : null}
                    </button>
                  );
                })
              )}
            </div>

            {selectedQueueBehavior?.summary ? <p className="muted">{selectedQueueBehavior.summary}</p> : null}
            {selectedQueueConflictReason ? (
              <p className="muted">Exclusivity: {selectedQueueConflictReason}</p>
            ) : null}
            {selectedQueueBehavior?.singleUsePerAscension ? (
              <p className="muted">This behavior is single-use per ascension.</p>
            ) : null}
            {selectedQueueBehavior?.requiresMarketOpen ? (
              <p className="muted">This behavior requires an open market session.</p>
            ) : null}
            {selectedQueueBehavior?.requiresNight ? (
              <p className="muted">This behavior can only start at night.</p>
            ) : null}
            {formatBehaviorRequirements(selectedQueueBehavior) ? (
              <p className="muted">Requirements: {formatBehaviorRequirements(selectedQueueBehavior)}</p>
            ) : null}

            <label>
              Schedule mode
              <select value={queueMode} onChange={(event) => setQueueMode(event.target.value as "once" | "repeat" | "repeat-until")}>
                {selectedQueueModes.map((mode) => (
                  <option key={mode} value={mode}>{mode}</option>
                ))}
              </select>
            </label>

            {queueMode === "repeat-until" ? (
              <label>
                Repeat until
                <select value={queueRepeatUntil} onChange={(event) => setQueueRepeatUntil(event.target.value)}>
                  <option value="2h">2h</option>
                  <option value="6h">6h</option>
                  <option value="12h">12h</option>
                  <option value="1d">1d</option>
                  <option value="2d">2d</option>
                </select>
              </label>
            ) : null}

            {selectedQueueBehavior?.requiresMarketOpen ? (
              <label>
                Market wait
                <select value={marketWait} onChange={(event) => setMarketWait(event.target.value)}>
                  <option value="6h">6h</option>
                  <option value="12h">12h</option>
                  <option value="1d">1d</option>
                  <option value="2d">2d</option>
                </select>
              </label>
            ) : null}

            <div className="modal-actions">
              <button type="button" onClick={onQueueBehavior} disabled={actionBusy || !selectedBehaviorKey || !!selectedQueueConflictReason || queueSlotsExhausted}>Queue</button>
              <button type="button" onClick={() => setQueueModalOpen(false)}>Cancel</button>
            </div>
            {queueSlotsExhausted ? <p className="muted">All queue slots are currently in use.</p> : null}
          </div>
        </div>
      ) : null}

      {adminOpen && isAdmin ? (
        <AdminModal
          open={adminOpen && isAdmin}
          onClose={() => setAdminOpen(false)}
          onChanged={handleAdminChanged}
        />
      ) : null}

      {toasts.length > 0 ? (
        <div className="toast-stack" role="status" aria-live="polite">
          {toasts.map((toast) => (
            <div key={toast.id} className={`toast-item ${toast.type}`}>
              <span>{toast.message}</span>
              <button type="button" onClick={() => dismissToast(toast.id)} aria-label="Dismiss notification">✕</button>
            </div>
          ))}
        </div>
      ) : null}

      <footer className="app-footer panel">
        <div className="app-footer-row">
          <span><strong>Account:</strong> {account?.username ?? "-"}</span>
          <span><strong>Character:</strong> {selectedCharacter?.name ?? "-"}</span>
          <span><strong>Realm:</strong> {formatRealmName(selectedCharacter?.realmId)}</span>
          <span><strong>Tick:</strong> {snapshotTick}</span>
          <span><strong>Realtime:</strong> {streamSummary}</span>
          <span><strong>Market:</strong> {snapshotMarketState}</span>
        </div>
      </footer>
    </div>
  );
}
