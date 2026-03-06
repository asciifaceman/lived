import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AccountData,
  BehaviorCatalogEntry,
  BehaviorView,
  ChatChannel,
  ChatMessage,
  FeedEvent,
  MarketStatus,
  MeData,
  OnboardingStatusData,
  PlayerInventoryData,
  PlayerStatusData,
  ascend,
  clearSession,
  getBehaviorCatalog,
  getChatChannels,
  getChatMessages,
  getFeedPublic,
  getMarketStatus,
  getMe,
  getOnboardingStatus,
  getPlayerBehaviors,
  getPlayerInventory,
  getPlayerStatus,
  getSession,
  getSystemVersion,
  login,
  logout,
  postChatMessage,
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
type GameplayTab = "overview" | "queue" | "inventory" | "feed";
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
const chatRefreshMs = 2500;
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
  if (state === "completed") {
    return 3;
  }
  return 9;
}

function compactResultMessage(behavior: BehaviorView): string {
  if (behavior.state === "failed") {
    return behavior.failureReason || "Behavior failed.";
  }
  return behavior.resultMessage || "Behavior completed.";
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
  const [feedEvents, setFeedEvents] = useState<FeedEvent[]>([]);
  const [marketStatus, setMarketStatus] = useState<MarketStatus | null>(null);

  const [queueModalOpen, setQueueModalOpen] = useState(false);
  const [selectedBehaviorKey, setSelectedBehaviorKey] = useState("");
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
    setFeedEvents([]);
    setMarketStatus(null);
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
      .filter((entry) => entry.state === "completed" || entry.state === "failed")
      .sort((left, right) => {
        if (left.completesAtTick !== right.completesAtTick) {
          return right.completesAtTick - left.completesAtTick;
        }
        return right.id - left.id;
      })
      .slice(0, 12);
  }, [playerBehaviors]);
  const coreStatRows = useMemo(() => buildStatDisplayRows(coreStats), [coreStats]);
  const derivedStatRows = useMemo(() => buildStatDisplayRows(derivedStats), [derivedStats]);
  const stream = useWorldStream(selectedCharacter?.id, isLoggedIn && !!selectedCharacter?.id);
  const streamErrorLabel = stream.lastError
    ? stream.lastError.length > 140
      ? `${stream.lastError.slice(0, 140)}...`
      : stream.lastError
    : null;

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

  const queueableBehaviors = useMemo(() => {
    const catalogByKey = new Map(behaviorCatalog.map((entry) => [entry.key, entry]));
    return behaviorCatalog
      .filter((entry) => entry.queueVisible ?? entry.available)
      .sort((left, right) => {
        if (left.available !== right.available) {
          return left.available ? -1 : 1;
        }
        const leftLabel = behaviorDisplayLabel(catalogByKey.get(left.key), left.key);
        const rightLabel = behaviorDisplayLabel(catalogByKey.get(right.key), right.key);
        return leftLabel.localeCompare(rightLabel);
      });
  }, [behaviorCatalog]);

  const behaviorCatalogByKey = useMemo(() => {
    return new Map(behaviorCatalog.map((entry) => [entry.key, entry]));
  }, [behaviorCatalog]);

  const selectedQueueBehavior = useMemo(() => {
    if (!selectedBehaviorKey) {
      return undefined;
    }
    return behaviorCatalogByKey.get(selectedBehaviorKey);
  }, [behaviorCatalogByKey, selectedBehaviorKey]);

  const selectedQueueConflictReason = useMemo(() => {
    if (!selectedQueueBehavior?.exclusiveGroup) {
      return "";
    }

    const selectedGroup = selectedQueueBehavior.exclusiveGroup.trim().toLowerCase();
    if (!selectedGroup) {
      return "";
    }

    const conflicting = playerBehaviors.find((behavior) => {
      if (behavior.state !== "active") {
        return false;
      }

      const activeCatalog = behaviorCatalogByKey.get(behavior.key);
      const activeGroup = (activeCatalog?.exclusiveGroup ?? "").trim().toLowerCase();
      if (!activeGroup) {
        return false;
      }
      if (activeGroup != selectedGroup) {
        return false;
      }

      return behavior.key !== selectedQueueBehavior.key;
    });

    if (!conflicting) {
      return "";
    }

    const conflictingCatalog = behaviorCatalogByKey.get(conflicting.key);
    return `${behaviorDisplayLabel(conflictingCatalog, conflicting.key)} is active and blocks this behavior right now.`;
    }, [behaviorCatalogByKey, playerBehaviors, selectedQueueBehavior]);

  const displayBehaviorLabel = useCallback((key: string) => {
    return behaviorDisplayLabel(behaviorCatalogByKey.get(key), key);
  }, [behaviorCatalogByKey]);

  useEffect(() => {
    if (!selectedBehaviorKey && queueableBehaviors.length > 0) {
      setSelectedBehaviorKey(queueableBehaviors[0].key);
    }
  }, [queueableBehaviors, selectedBehaviorKey]);

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
    if (!isLoggedIn || !selectedCharacter?.id) {
      return;
    }

    try {
      const [status, inventory, behaviors, catalog, feed, market] = await Promise.all([
        getPlayerStatus(selectedCharacter.id),
        getPlayerInventory(selectedCharacter.id),
        getPlayerBehaviors(selectedCharacter.id),
        getBehaviorCatalog(selectedCharacter.id),
        getFeedPublic(selectedCharacter.id, 30),
        getMarketStatus(selectedCharacter.realmId, selectedCharacter.id)
      ]);
      setPlayerStatus(status);
      setPlayerInventory(inventory);
      setPlayerBehaviors(behaviors.behaviors);
      setBehaviorCatalog(catalog);
      setFeedEvents(feed.events ?? []);
      setMarketStatus(market);
      setRealmPausedMessage(null);
    } catch (err) {
      handleError(err);
    }
  }, [handleError, isLoggedIn, selectedCharacter]);

  const loadChat = useCallback(async () => {
    if (!isLoggedIn || !selectedCharacter?.id) {
      return;
    }

    try {
      const [channelResult, messageResult] = await Promise.all([
        getChatChannels(selectedCharacter.id),
        getChatMessages(chatChannel, selectedCharacter.id, 100)
      ]);
      setChannels(channelResult.channels ?? []);
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
  }, [chatChannel, handleError, isLoggedIn, pushToast, selectedCharacter]);

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
    if (!isLoggedIn || !selectedCharacter?.id) {
      return;
    }

    void loadGameplay();
    const timer = window.setInterval(() => {
      void loadGameplay();
    }, gameplayRefreshMs);

    return () => window.clearInterval(timer);
  }, [isLoggedIn, loadGameplay, selectedCharacter]);

  useEffect(() => {
    if (!isLoggedIn || !selectedCharacter?.id) {
      return;
    }

    void loadChat();
    const timer = window.setInterval(() => {
      void loadChat();
    }, chatRefreshMs);

    return () => window.clearInterval(timer);
  }, [isLoggedIn, loadChat, selectedCharacter]);

  useEffect(() => {
    const tick = stream.event?.tick;
    if (!tick || !isLoggedIn || !selectedCharacter?.id) {
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
  }, [isLoggedIn, loadChat, loadGameplay, selectedCharacter, stream.event?.tick, view]);

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
      const selected = queueableBehaviors.find((entry) => entry.key === selectedBehaviorKey);
      await startBehavior(
        selectedBehaviorKey,
        selectedCharacter.id,
        selected?.requiresMarketOpen ? marketWait : undefined
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
    if (!chatDraft.trim() || !selectedCharacter?.id) {
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
          <p>New MMO frontend shell (legacy UI preserved in LegacyApp.reference.tsx.txt)</p>

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
            <h1>Lived MMO Console</h1>
            <p>Frontend {webPackage.version} · API-aware rebuild</p>
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
              <button className={view === "gameplay" ? "active" : ""} onClick={() => setView("gameplay")} type="button" disabled={!hasCharacterContext}>Gameplay</button>
              <button className={view === "chat" ? "active" : ""} onClick={() => setView("chat")} type="button" disabled={!hasCharacterContext}>Chat</button>
            </div>

            {isAdmin ? <button type="button" onClick={() => setAdminOpen(true)}>Admin Panel</button> : null}
            <button type="button" onClick={onLogout} disabled={actionBusy}>Logout</button>
          </div>
        </header>

        {loading || bootstrapping ? <div className="notice">Loading account context...</div> : null}

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
      </div>

      <main className="page-grid">
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
              <div className="split-grid">
                <div className="queue-panel">
                  <div className="queue-actions">
                    <h3>Current Queue</h3>
                    <button type="button" onClick={() => setQueueModalOpen(true)}>Queue behavior</button>
                  </div>
                  {queueActiveRows.length === 0 ? (
                    <p className="muted queue-empty">No queued or active behaviors right now.</p>
                  ) : (
                    <table className="mini-table">
                      <thead>
                        <tr>
                          <th>Key</th>
                          <th>State</th>
                          <th>Progress</th>
                          <th>Start</th>
                          <th>Complete</th>
                          <th>Remaining</th>
                        </tr>
                      </thead>
                      <tbody>
                        {queueActiveRows.map((behavior) => (
                          <tr key={behavior.id}>
                            <td title={behavior.key}>{displayBehaviorLabel(behavior.key)}</td>
                            <td><span className={`queue-state queue-state-${behavior.state}`}>{behavior.state}</span></td>
                            <td>
                              <div className="queue-progress-wrap" title={`${behaviorProgressPct(queueCurrentTick, behavior)}%`}>
                                <div className="queue-progress-fill" style={{ width: `${behaviorProgressPct(queueCurrentTick, behavior)}%` }} />
                              </div>
                            </td>
                            <td>{formatWorldTime(behavior.startedAtTick || behavior.scheduledAtTick)}</td>
                            <td>{formatWorldTime(behavior.completesAtTick)}</td>
                            <td>{formatRemainingMinutes(queueCurrentTick, behavior.completesAtTick)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </div>

                <div className="queue-panel">
                  <h3>Recent Results</h3>
                  {queueHistoryRows.length === 0 ? (
                    <p className="muted queue-empty">No completed or failed behaviors yet.</p>
                  ) : (
                    <table className="mini-table">
                      <thead>
                        <tr>
                          <th>Key</th>
                          <th>State</th>
                          <th>Completed</th>
                          <th>Result</th>
                        </tr>
                      </thead>
                      <tbody>
                        {queueHistoryRows.map((behavior) => (
                          <tr key={behavior.id}>
                            <td title={behavior.key}>{displayBehaviorLabel(behavior.key)}</td>
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
            disabled={actionBusy}
          />
        ) : null}
      </main>

      {queueModalOpen ? (
        <div className="modal-backdrop" onClick={() => setQueueModalOpen(false)}>
          <div className="modal-card" onClick={(event) => event.stopPropagation()}>
            <h3>Queue Behavior</h3>
            <label>
              Behavior
              <select value={selectedBehaviorKey} onChange={(event) => setSelectedBehaviorKey(event.target.value)}>
                {queueableBehaviors.map((behavior) => (
                  <option key={behavior.key} value={behavior.key}>
                    {displayBehaviorLabel(behavior.key)} · {behavior.durationMinutes}m {behavior.staminaCost ? `· stamina ${behavior.staminaCost}` : ""}{behavior.available ? "" : " · unavailable"}
                  </option>
                ))}
              </select>
            </label>
            {selectedQueueBehavior?.summary ? <p className="muted">{selectedQueueBehavior.summary}</p> : null}
            {!selectedQueueBehavior?.available && selectedQueueBehavior?.unavailableReason ? (
              <p className="muted">Current requirement: {selectedQueueBehavior.unavailableReason}</p>
            ) : null}
            {selectedQueueConflictReason ? (
              <p className="muted">Exclusivity: {selectedQueueConflictReason}</p>
            ) : null}
            {selectedQueueBehavior?.requiresMarketOpen ? (
              <p className="muted">This behavior requires an open market session.</p>
            ) : null}

            <label>
              Market wait
              <select value={marketWait} onChange={(event) => setMarketWait(event.target.value)}>
                <option value="6h">6h</option>
                <option value="12h">12h</option>
                <option value="1d">1d</option>
                <option value="2d">2d</option>
              </select>
            </label>

            <div className="modal-actions">
              <button type="button" onClick={onQueueBehavior} disabled={actionBusy || !selectedBehaviorKey || !!selectedQueueConflictReason}>Queue</button>
              <button type="button" onClick={() => setQueueModalOpen(false)}>Cancel</button>
            </div>
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
          <span><strong>Stream:</strong> {stream.status}</span>
          <span><strong>Stream Attempts:</strong> {stream.attempts}</span>
          {streamErrorLabel ? <span><strong>Stream Error:</strong> {streamErrorLabel}</span> : null}
          <span><strong>Market:</strong> {snapshotMarketState}</span>
        </div>
      </footer>
    </div>
  );
}
