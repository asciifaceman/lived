import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AdminAuditEntry,
  AdminChatChannelEntry,
  AdminCharacterEntry,
  AdminRealm,
  AdminRealmAccessEntry,
  AdminWordRule,
  adminCreateRealm,
  adminGrantRealmAccess,
  adminChatListChannels,
  adminApplyRealmAction,
  adminAuditList,
  adminChatDisableChannel,
  adminChatAttachChannel,
  adminChatCreateChannel,
  adminChatEditChannel,
  adminChatFlushChannel,
  adminChatModerateChannel,
  adminChatSystemMessage,
  adminChatWordlistAdd,
  adminChatWordlistList,
  adminChatWordlistRemove,
  adminGetRealms,
  adminGetStats,
  adminListRealmAccess,
  adminListCharacters,
  adminModerateAccount,
  adminModerateCharacter,
  adminRevokeRealmAccess,
  adminSetRealmConfig,
  adminSetAccountRole,
  adminSetAccountStatus
} from "../api";

type AdminTab = "dashboard" | "realm" | "accounts" | "characters" | "chat" | "wordlist" | "audit";

type AdminModalProps = {
  open: boolean;
  onClose: () => void;
  onChanged?: () => Promise<void> | void;
};

type AdminDataSegment = "realms" | "stats" | "chatChannels" | "realmAccess" | "wordlist" | "audit" | "characters";

const adminTabDataDependencies: Record<AdminTab, AdminDataSegment[]> = {
  dashboard: ["realms", "stats"],
  realm: ["realms", "realmAccess"],
  accounts: [],
  characters: ["characters"],
  chat: ["chatChannels", "wordlist", "realms"],
  wordlist: ["wordlist"],
  audit: ["audit"]
};

const adminDataSegmentLabels: Record<AdminDataSegment, string> = {
  realms: "realms",
  stats: "stats",
  chatChannels: "chat channels",
  realmAccess: "realm access",
  wordlist: "wordlist",
  audit: "audit",
  characters: "characters"
};

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

type RealmActionSummary = {
  targetRealmIds: number[];
  succeededRealmIds: number[];
  failed: Array<{ realmId: number; reason: string }>;
};

function formatRealmActionSummary(baseMessage: string, summary: RealmActionSummary | null): string {
  if (!summary) {
    return baseMessage;
  }

  const total = summary.targetRealmIds.length;
  const succeeded = summary.succeededRealmIds.length;
  const failed = summary.failed.length;
  if (failed <= 0) {
    return `${baseMessage} ${succeeded}/${total} realm(s) succeeded.`;
  }

  const failedRealmList = summary.failed.map((entry) => entry.realmId).join(", ");
  const failureDetails = summary.failed
    .map((entry) => `realm ${entry.realmId}: ${entry.reason}`)
    .join(" | ");
  return `${baseMessage} ${succeeded}/${total} realm(s) succeeded; failed realms: ${failedRealmList}. Details: ${failureDetails}`;
}

const dashboardSummaryKeys = new Set([
  "activeAccounts",
  "activeCharacters",
  "activeSessions",
  "queuedOrActive",
  "publicWorldEvents",
  "tickBudget"
]);

type AuditSlice = {
  label: string;
  count: number;
  color: string;
};

type CountRow = {
  label: string;
  count: number;
};

type TickBudgetSummary = {
  targetTickMs: number;
  avgTickMs: number;
  budgetDeltaMs: number;
  budgetRatio: number;
  totalRuns: number;
  totalFailures: number;
  failureRate: number;
  avgAdvanceMinutes: number;
  lastTickMs: number;
  lastAdvanceMinutes: number;
};

function asFiniteNumber(value: unknown): number | null {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return null;
  }
  return value;
}

function parseTickBudgetSummary(stats: Record<string, unknown> | null): TickBudgetSummary | null {
  if (!stats) {
    return null;
  }

  const raw = stats["tickBudget"];
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return null;
  }

  const tickBudget = raw as Record<string, unknown>;
  const targetTickMs = asFiniteNumber(tickBudget.targetTickMs);
  const avgTickMs = asFiniteNumber(tickBudget.avgTickMs);
  const budgetDeltaMs = asFiniteNumber(tickBudget.budgetDeltaMs);
  const budgetRatio = asFiniteNumber(tickBudget.budgetRatio);
  const totalRuns = asFiniteNumber(tickBudget.totalRuns);
  const totalFailures = asFiniteNumber(tickBudget.totalFailures);
  const failureRate = asFiniteNumber(tickBudget.failureRate);
  const avgAdvanceMinutes = asFiniteNumber(tickBudget.avgAdvanceMinutes);
  const lastTickMs = asFiniteNumber(tickBudget.lastTickMs);
  const lastAdvanceMinutes = asFiniteNumber(tickBudget.lastAdvanceMinutes);

  if (
    targetTickMs === null ||
    avgTickMs === null ||
    budgetDeltaMs === null ||
    budgetRatio === null ||
    totalRuns === null ||
    totalFailures === null ||
    failureRate === null ||
    avgAdvanceMinutes === null ||
    lastTickMs === null ||
    lastAdvanceMinutes === null
  ) {
    return null;
  }

  return {
    targetTickMs,
    avgTickMs,
    budgetDeltaMs,
    budgetRatio,
    totalRuns,
    totalFailures,
    failureRate,
    avgAdvanceMinutes,
    lastTickMs,
    lastAdvanceMinutes
  };
}

function formatNumber(value: number, decimals: number = 2): string {
  return value.toLocaleString(undefined, {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals
  });
}

const auditChartColors = [
  "#4aa8ff",
  "#34d399",
  "#fbbf24",
  "#f87171",
  "#a78bfa",
  "#22d3ee",
  "#fb7185"
];

function compactAuditRows(rows: CountRow[], maxSlices: number = 6): { total: number; slices: AuditSlice[] } {
  const total = rows.reduce((sum, row) => sum + row.count, 0);
  if (rows.length <= maxSlices) {
    return {
      total,
      slices: rows.map((row, index) => ({
        ...row,
        color: auditChartColors[index % auditChartColors.length]
      }))
    };
  }

  const topRows = rows.slice(0, maxSlices - 1);
  const otherCount = rows.slice(maxSlices - 1).reduce((sum, row) => sum + row.count, 0);
  const compact = [...topRows, { label: "other", count: otherCount }];
  return {
    total,
    slices: compact.map((row, index) => ({
      ...row,
      color: auditChartColors[index % auditChartColors.length]
    }))
  };
}

function distributionBy(
  entries: AdminAuditEntry[],
  selector: (entry: AdminAuditEntry) => string,
  maxSlices: number = 6
): { total: number; slices: AuditSlice[] } {
  const counts = new Map<string, number>();
  for (const entry of entries) {
    const key = selector(entry).trim() || "unknown";
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }

  const rows = Array.from(counts.entries())
    .map(([label, count]) => ({ label, count }))
    .sort((left, right) => right.count - left.count);

  return compactAuditRows(rows, maxSlices);
}

function aggregateDistributionFromStats(
  stats: Record<string, unknown> | null,
  aggregateKey: "byAction" | "byRealm",
  labelResolver: (row: Record<string, unknown>) => string,
  maxSlices: number = 6
): { total: number; slices: AuditSlice[] } | null {
  const adminAuditValue = stats?.["adminAudit"];
  if (!adminAuditValue || typeof adminAuditValue !== "object" || Array.isArray(adminAuditValue)) {
    return null;
  }

  const adminAudit = adminAuditValue as Record<string, unknown>;
  const aggregate = adminAudit[aggregateKey];
  if (!Array.isArray(aggregate)) {
    return null;
  }

  const rows: CountRow[] = [];
  for (const entry of aggregate) {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
      continue;
    }
    const row = entry as Record<string, unknown>;
    const countValue = row.count;
    if (typeof countValue !== "number" || !Number.isFinite(countValue) || countValue <= 0) {
      continue;
    }
    const label = labelResolver(row).trim() || "unknown";
    rows.push({ label, count: countValue });
  }

  if (rows.length === 0) {
    return null;
  }

  rows.sort((left, right) => right.count - left.count);
  return compactAuditRows(rows, maxSlices);
}

function donutGradient(slices: AuditSlice[], total: number): string {
  if (total <= 0 || slices.length === 0) {
    return "conic-gradient(#1e2d45 0 360deg)";
  }

  let start = 0;
  const segments: string[] = [];
  for (const slice of slices) {
    const portion = (slice.count / total) * 360;
    const end = start + portion;
    segments.push(`${slice.color} ${start}deg ${end}deg`);
    start = end;
  }
  return `conic-gradient(${segments.join(", ")})`;
}

function summaryStatsCards(stats: Record<string, unknown> | null): Array<{ label: string; value: string | number }> {
  if (!stats) {
    return [];
  }

  const cards: Array<{ label: string; value: string | number }> = [];
  const numberValue = (key: string): number | undefined => {
    const value = stats[key];
    return typeof value === "number" ? value : undefined;
  };

  const pushIfNumber = (key: string, label: string) => {
    const value = numberValue(key);
    if (value !== undefined) {
      cards.push({ label, value });
    }
  };

  pushIfNumber("activeAccounts", "Active Accounts");
  pushIfNumber("activeCharacters", "Active Characters");
  pushIfNumber("activeSessions", "Active Sessions");
  pushIfNumber("queuedOrActive", "Queued/Active Behaviors");
  pushIfNumber("publicWorldEvents", "Public World Events");

  for (const [key, value] of Object.entries(stats)) {
    if (dashboardSummaryKeys.has(key) || key === "adminAudit") {
      continue;
    }
    if (typeof value === "number") {
      cards.push({ label: key, value });
    }
  }

  const adminAudit = stats["adminAudit"];
  if (adminAudit && typeof adminAudit === "object" && !Array.isArray(adminAudit)) {
    const auditRecord = adminAudit as Record<string, unknown>;
    if (typeof auditRecord.total === "number") {
      cards.push({ label: "Audit Total", value: auditRecord.total });
    }
    if (typeof auditRecord.windowTotal === "number") {
      cards.push({ label: "Audit In Window", value: auditRecord.windowTotal });
    }

    const windowValue = auditRecord.window;
    if (windowValue && typeof windowValue === "object" && !Array.isArray(windowValue)) {
      const windowRecord = windowValue as Record<string, unknown>;
      if (typeof windowRecord.windowTicks === "number") {
        cards.push({ label: "Window Ticks", value: windowRecord.windowTicks });
      }
      if (typeof windowRecord.currentTick === "number") {
        cards.push({ label: "Current Tick", value: windowRecord.currentTick });
      }
    }
  }

  return cards;
}

function hasUnrenderedComplexDashboardStats(stats: Record<string, unknown> | null): boolean {
  if (!stats) {
    return false;
  }
  return Object.entries(stats).some(([key, value]) => {
    if (dashboardSummaryKeys.has(key) || key === "adminAudit") {
      return false;
    }
    if (typeof value === "number") {
      return false;
    }
    if (value === null || value === undefined) {
      return false;
    }
    return true;
  });
}

export function AdminModal(props: AdminModalProps) {
  const [adminTab, setAdminTab] = useState<AdminTab>("dashboard");
  const [actionBusy, setActionBusy] = useState(false);
  const [loadBusy, setLoadBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const [adminRealms, setAdminRealms] = useState<AdminRealm[]>([]);
  const [adminStats, setAdminStats] = useState<Record<string, unknown> | null>(null);
  const [adminWordRules, setAdminWordRules] = useState<AdminWordRule[]>([]);
  const [adminAudit, setAdminAudit] = useState<AdminAuditEntry[]>([]);
  const [adminCharacters, setAdminCharacters] = useState<AdminCharacterEntry[]>([]);
  const [adminChatChannels, setAdminChatChannels] = useState<AdminChatChannelEntry[]>([]);
  const [realmAccessEntries, setRealmAccessEntries] = useState<AdminRealmAccessEntry[]>([]);
  const [adminSegmentErrors, setAdminSegmentErrors] = useState<Partial<Record<AdminDataSegment, string>>>({});
  const [adminSegmentBusy, setAdminSegmentBusy] = useState<Partial<Record<AdminDataSegment, boolean>>>({});

  const [realmActionRealm, setRealmActionRealm] = useState("1");
  const [realmActionType, setRealmActionType] = useState("realm_pause");
  const [realmActionReason, setRealmActionReason] = useState("ops");
  const [realmActionNote, setRealmActionNote] = useState("");
  const [realmActionItemKey, setRealmActionItemKey] = useState("");
  const [realmActionPrice, setRealmActionPrice] = useState("");
  const [realmCreateName, setRealmCreateName] = useState("");
  const [realmCreateWhitelistOnly, setRealmCreateWhitelistOnly] = useState(false);
  const [realmCreateReason, setRealmCreateReason] = useState("realm_create");
  const [realmCreateNote, setRealmCreateNote] = useState("");
  const [realmConfigName, setRealmConfigName] = useState("Realm 1");
  const [realmConfigWhitelistOnly, setRealmConfigWhitelistOnly] = useState(false);
  const [realmAccessAccountId, setRealmAccessAccountId] = useState("");
  const [realmAccessReason, setRealmAccessReason] = useState("realm_access");
  const [realmAccessNote, setRealmAccessNote] = useState("");

  const [accountTargetId, setAccountTargetId] = useState("");
  const [accountReason, setAccountReason] = useState("policy");
  const [accountNote, setAccountNote] = useState("");
  const [accountRoleKey, setAccountRoleKey] = useState("moderator");
  const [accountStatus, setAccountStatus] = useState<"active" | "locked">("active");

  const [characterFilterName, setCharacterFilterName] = useState("");
  const [characterEditId, setCharacterEditId] = useState("");
  const [characterEditName, setCharacterEditName] = useState("");
  const [characterEditStatus, setCharacterEditStatus] = useState<"active" | "locked">("active");
  const [characterEditPrimary, setCharacterEditPrimary] = useState(false);
  const [characterEditReason, setCharacterEditReason] = useState("moderation");

  const [chatCreateRealmId, setChatCreateRealmId] = useState("1");
  const [chatCreateKey, setChatCreateKey] = useState("");
  const [chatCreateName, setChatCreateName] = useState("");
  const [chatCreateSubject, setChatCreateSubject] = useState("");
  const [chatCreateDescription, setChatCreateDescription] = useState("");
  const [chatEditBinding, setChatEditBinding] = useState("global");
  const [chatEditName, setChatEditName] = useState("");
  const [chatEditSubject, setChatEditSubject] = useState("");
  const [chatEditDescription, setChatEditDescription] = useState("");
  const [chatAssignmentChannelKey, setChatAssignmentChannelKey] = useState("global");
  const [chatAssignmentRealmId, setChatAssignmentRealmId] = useState("1");
  const [chatAdminOpRealmId, setChatAdminOpRealmId] = useState("1");
  const [chatAdminAccountId, setChatAdminAccountId] = useState("");
  const [chatAdminAction, setChatAdminAction] = useState<"ban" | "unban" | "kick">("ban");
  const [chatAdminDuration, setChatAdminDuration] = useState("30");
  const [chatAdminReason, setChatAdminReason] = useState("chat_policy");
  const [chatAdminNote, setChatAdminNote] = useState("");
  const [chatAdminSystemMessage, setChatAdminSystemMessage] = useState("");

  const [wordTerm, setWordTerm] = useState("");
  const [wordReason, setWordReason] = useState("wordlist");
  const [wordNote, setWordNote] = useState("");

  const [auditActionFilter, setAuditActionFilter] = useState("");
  const [auditUserFilter, setAuditUserFilter] = useState("");

  const knownChannelBindings = useMemo(() => {
    if (adminChatChannels.length <= 0) {
      return [{ value: "1:global", key: "global", realmId: 1, label: "global · Realm 1" }];
    }

    return adminChatChannels
      .map((entry) => {
        const realm = adminRealms.find((realmEntry) => realmEntry.realmId === entry.realmId);
        const realmLabel = realm?.name || `Realm ${entry.realmId}`;
        return {
          value: `${entry.realmId}:${entry.key.toLowerCase()}`,
          key: entry.key,
          realmId: entry.realmId,
          label: `${entry.key} · ${realmLabel}`
        };
      })
      .sort((left, right) => {
        const keyCmp = left.key.localeCompare(right.key);
        if (keyCmp !== 0) {
          return keyCmp;
        }
        return left.realmId - right.realmId;
      });
  }, [adminChatChannels, adminRealms]);

  const knownChannelKeys = useMemo(() => {
    const keys = new Map<string, { key: string; realmId: number }>();
    for (const entry of knownChannelBindings) {
      const normalized = entry.key.toLowerCase();
      if (!keys.has(normalized)) {
        keys.set(normalized, { key: entry.key, realmId: entry.realmId });
      }
    }
    return Array.from(keys.values()).sort((left, right) => left.key.localeCompare(right.key));
  }, [knownChannelBindings]);

  const selectedEditBinding = useMemo(() => {
    const normalized = chatEditBinding.trim().toLowerCase();
    return knownChannelBindings.find((entry) => entry.key.toLowerCase() === normalized);
  }, [chatEditBinding, knownChannelBindings]);

  const selectedAssignmentChannel = useMemo(() => {
    const normalized = chatAssignmentChannelKey.trim().toLowerCase();
    return adminChatChannels.find((entry) => entry.key.toLowerCase() === normalized);
  }, [adminChatChannels, chatAssignmentChannelKey]);

  const activeChannelKey = selectedEditBinding?.key || "global";

  useEffect(() => {
    if (knownChannelKeys.length <= 0) {
      return;
    }

    if (!knownChannelKeys.some((entry) => entry.key.toLowerCase() === chatEditBinding.toLowerCase())) {
      setChatEditBinding(knownChannelKeys[0].key.toLowerCase());
    }
  }, [chatEditBinding, knownChannelKeys]);

  useEffect(() => {
    if (knownChannelKeys.length <= 0) {
      return;
    }

    if (!knownChannelKeys.some((entry) => entry.key.toLowerCase() === chatAssignmentChannelKey.toLowerCase())) {
      setChatAssignmentChannelKey(knownChannelKeys[0].key.toLowerCase());
    }
  }, [chatAssignmentChannelKey, knownChannelKeys]);

  useEffect(() => {
    if (!selectedEditBinding) {
      return;
    }

    const entry = adminChatChannels.find((channel) => channel.realmId === selectedEditBinding.realmId && channel.key.toLowerCase() === selectedEditBinding.key.toLowerCase());
    if (!entry) {
      return;
    }

    setChatEditName(entry.name || selectedEditBinding.key);
    setChatEditSubject(entry.subject ?? "");
    setChatEditDescription(entry.description ?? "");
    setChatAdminOpRealmId(String(entry.realmId));
  }, [adminChatChannels, selectedEditBinding]);

  const availableRealmIds = adminRealms
    .map((entry) => entry.realmId)
    .filter((realmId) => Number.isInteger(realmId) && realmId > 0)
    .sort((left, right) => left - right);

  const formatRealmOptionLabel = useCallback((realmId: number): string => {
    const realm = adminRealms.find((entry) => entry.realmId === realmId);
    if (!realm) {
      return `Realm ${realmId}`;
    }
    const scope = realm.whitelistOnly ? "whitelisted" : "open";
    return `${realm.name || `Realm ${realmId}`} · ${realm.activeCharacters} active · ${scope}`;
  }, [adminRealms]);

  const selectedRealmConfig = useMemo(() => {
    const realmId = parseNumber(realmActionRealm);
    if (!realmId) {
      return undefined;
    }
    return adminRealms.find((entry) => entry.realmId === realmId);
  }, [adminRealms, realmActionRealm]);

  useEffect(() => {
    if (!selectedRealmConfig) {
      return;
    }
    setRealmConfigName(selectedRealmConfig.name || `Realm ${selectedRealmConfig.realmId}`);
    setRealmConfigWhitelistOnly(!!selectedRealmConfig.whitelistOnly);
  }, [selectedRealmConfig]);

  useEffect(() => {
    if (!props.open) {
      return;
    }

    const realmId = parseNumber(realmActionRealm) ?? 1;
    void (async () => {
      try {
        const access = await adminListRealmAccess(realmId);
        setRealmAccessEntries(access.entries ?? []);
      } catch {
        // keep current access rows if background refresh fails
      }
    })();
  }, [props.open, realmActionRealm]);

  const runChatActionForOperationRealm = useCallback(async (runner: (realmId: number) => Promise<void>): Promise<RealmActionSummary> => {
    const targetRealmId = parseNumber(chatAdminOpRealmId) ?? 1;
    const summary: RealmActionSummary = {
      targetRealmIds: [targetRealmId],
      succeededRealmIds: [],
      failed: []
    };

    try {
      await runner(targetRealmId);
      summary.succeededRealmIds.push(targetRealmId);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      summary.failed.push({ realmId: targetRealmId, reason: message });
    }

    return summary;
  }, [chatAdminOpRealmId]);

  useEffect(() => {
    if (availableRealmIds.length === 0) {
      if (chatCreateRealmId !== "1") {
        setChatCreateRealmId("1");
      }
      return;
    }

    const current = parseNumber(chatCreateRealmId);
    if (!current || !availableRealmIds.includes(current)) {
      setChatCreateRealmId(String(availableRealmIds[0]));
    }
  }, [availableRealmIds, chatCreateRealmId]);

  useEffect(() => {
    if (availableRealmIds.length === 0) {
      if (chatAssignmentRealmId !== "1") {
        setChatAssignmentRealmId("1");
      }
      return;
    }

    const current = parseNumber(chatAssignmentRealmId);
    if (!current || !availableRealmIds.includes(current)) {
      setChatAssignmentRealmId(String(availableRealmIds[0]));
    }
  }, [availableRealmIds, chatAssignmentRealmId]);

  useEffect(() => {
    if (availableRealmIds.length === 0) {
      if (chatAdminOpRealmId !== "1") {
        setChatAdminOpRealmId("1");
      }
      return;
    }

    const current = parseNumber(chatAdminOpRealmId);
    if (!current || !availableRealmIds.includes(current)) {
      setChatAdminOpRealmId(String(availableRealmIds[0]));
    }
  }, [availableRealmIds, chatAdminOpRealmId]);

  const auditActionAggregate = aggregateDistributionFromStats(
    adminStats,
    "byAction",
    (row) => {
      const action = row.actionKey;
      if (typeof action === "string" && action.trim()) {
        return action;
      }
      const generic = row.action;
      if (typeof generic === "string" && generic.trim()) {
        return generic;
      }
      return "unknown";
    }
  );

  const auditActionDistribution = auditActionAggregate ?? distributionBy(adminAudit, (entry) => entry.actionKey || "unknown");
  const auditActionSourceLabel = auditActionAggregate ? "Aggregated" : "Fallback";

  const auditRealmAggregate = aggregateDistributionFromStats(
    adminStats,
    "byRealm",
    (row) => {
      const realmId = row.realmId;
      if (typeof realmId === "number" && Number.isFinite(realmId)) {
        return `realm ${realmId}`;
      }
      const realm = row.realm;
      if (typeof realm === "string" && realm.trim()) {
        return realm;
      }
      return "unknown";
    }
  );

  const auditRealmDistribution = auditRealmAggregate ?? distributionBy(adminAudit, (entry) => `realm ${entry.realmId}`);
  const auditRealmSourceLabel = auditRealmAggregate ? "Aggregated" : "Fallback";
  const tickBudgetSummary = parseTickBudgetSummary(adminStats);
  const tickBudgetUsagePercent = tickBudgetSummary
    ? Math.max(0, Math.min(100, tickBudgetSummary.budgetRatio * 100))
    : 0;
  const tickBudgetDeltaLabel = tickBudgetSummary
    ? tickBudgetSummary.budgetDeltaMs <= 0
      ? `${formatNumber(Math.abs(tickBudgetSummary.budgetDeltaMs))} ms under budget`
      : `${formatNumber(tickBudgetSummary.budgetDeltaMs)} ms over budget`
    : "";

  const loadAdminSegment = useCallback(async (segment: AdminDataSegment): Promise<void> => {
    const selectedRealmID = parseNumber(realmActionRealm) ?? 1;
    switch (segment) {
      case "realms": {
        const realms = await adminGetRealms();
        setAdminRealms(realms.realms ?? []);
        return;
      }
      case "stats": {
        const stats = await adminGetStats();
        setAdminStats(stats);
        return;
      }
      case "chatChannels": {
        const channels = await adminChatListChannels({ includeInactive: true });
        setAdminChatChannels(channels.channels ?? []);
        return;
      }
      case "realmAccess": {
        const access = await adminListRealmAccess(selectedRealmID);
        setRealmAccessEntries(access.entries ?? []);
        return;
      }
      case "wordlist": {
        const wordlist = await adminChatWordlistList();
        setAdminWordRules(wordlist.rules ?? []);
        return;
      }
      case "audit": {
        const audit = await adminAuditList({ limit: 50 });
        setAdminAudit(audit.entries ?? []);
        return;
      }
      case "characters": {
        const characters = await adminListCharacters({ limit: 50 });
        setAdminCharacters(characters.entries ?? []);
        return;
      }
      default:
        return;
    }
  }, [realmActionRealm]);

  const loadAdmin = useCallback(async () => {
    const segments: AdminDataSegment[] = ["realms", "stats", "chatChannels", "realmAccess", "wordlist", "audit", "characters"];
    setAdminSegmentBusy((prev) => {
      const next = { ...prev };
      for (const segment of segments) {
        next[segment] = true;
      }
      return next;
    });

    const results = await Promise.allSettled(segments.map((segment) => loadAdminSegment(segment)));

    const nextErrors: Partial<Record<AdminDataSegment, string>> = {};
    for (let index = 0; index < segments.length; index++) {
      const segment = segments[index];
      const result = results[index];
      if (result.status === "rejected") {
        const message = result.reason instanceof Error ? result.reason.message : String(result.reason);
        nextErrors[segment] = message;
      }
    }
    setAdminSegmentErrors(nextErrors);

    setAdminSegmentBusy((prev) => {
      const next = { ...prev };
      for (const segment of segments) {
        next[segment] = false;
      }
      return next;
    });

    const failures = Object.keys(nextErrors).map((key) => adminDataSegmentLabels[key as AdminDataSegment]);
    if (failures.length > 0) {
      setError(`Some admin data failed to load: ${failures.join(", ")}. You can still use other tabs.`);
      return;
    }

    setError(null);
  }, [loadAdminSegment]);

  const retryAdminSegment = useCallback(async (segment: AdminDataSegment) => {
    setAdminSegmentBusy((prev) => ({ ...prev, [segment]: true }));
    try {
      await loadAdminSegment(segment);
      setAdminSegmentErrors((prev) => {
        const next = { ...prev };
        delete next[segment];
        return next;
      });
      setNotice(`Reloaded ${adminDataSegmentLabels[segment]}.`);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setAdminSegmentErrors((prev) => ({ ...prev, [segment]: message }));
      setError(`Failed to reload ${adminDataSegmentLabels[segment]}: ${message}`);
    } finally {
      setAdminSegmentBusy((prev) => ({ ...prev, [segment]: false }));
    }
  }, [loadAdminSegment]);

  const refreshAdmin = useCallback(async () => {
    setLoadBusy(true);
    setError(null);
    try {
      await loadAdmin();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoadBusy(false);
    }
  }, [loadAdmin]);

  useEffect(() => {
    if (!props.open) {
      return;
    }

    void (async () => {
      await refreshAdmin();
    })();
  }, [props.open, refreshAdmin]);

  const runAdminAction = async (
    runner: () => Promise<void>,
    successMessage: string | (() => string),
    afterSuccess?: () => void
  ) => {
    setActionBusy(true);
    setError(null);
    try {
      await runner();
      afterSuccess?.();
      setNotice(typeof successMessage === "function" ? successMessage() : successMessage);
      await loadAdmin();
      await props.onChanged?.();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setActionBusy(false);
    }
  };

  const renderTabDataStatus = useCallback((segments: AdminDataSegment[]) => {
    if (segments.length <= 0) {
      return null;
    }

    const busySegments = segments.filter((segment) => !!adminSegmentBusy[segment]);
    const failedSegments = segments.filter((segment) => !!adminSegmentErrors[segment]);

    if (busySegments.length <= 0 && failedSegments.length <= 0) {
      return null;
    }

    return (
      <div className="column-form">
        <h3>Tab data status</h3>
        {busySegments.length > 0 ? <p className="muted">Loading: {busySegments.map((segment) => adminDataSegmentLabels[segment]).join(", ")}</p> : null}
        {failedSegments.length > 0 ? <p className="notice error">Failed: {failedSegments.map((segment) => adminDataSegmentLabels[segment]).join(", ")}</p> : null}
        {failedSegments.length > 0 ? (
          <div className="button-row">
            {failedSegments.map((segment) => (
              <button
                key={segment}
                type="button"
                onClick={() => void retryAdminSegment(segment)}
                disabled={actionBusy || !!adminSegmentBusy[segment]}
              >
                {adminSegmentBusy[segment] ? `Retrying ${adminDataSegmentLabels[segment]}...` : `Retry ${adminDataSegmentLabels[segment]}`}
              </button>
            ))}
          </div>
        ) : null}
      </div>
    );
  }, [actionBusy, adminSegmentBusy, adminSegmentErrors, retryAdminSegment]);

  const activeTabSegments = adminTabDataDependencies[adminTab];
  const activeTabSegmentFailures = activeTabSegments.filter((segment) => !!adminSegmentErrors[segment]);
  const activeTabStatusText = activeTabSegments.length === 0
    ? "No tab-specific data dependencies."
    : activeTabSegments
        .map((segment) => `${adminDataSegmentLabels[segment]}:${adminSegmentErrors[segment] ? "error" : "ok"}`)
        .join(" · ");

  if (!props.open) {
    return null;
  }

  return (
    <div className="modal-backdrop" onClick={props.onClose}>
      <div className="admin-modal" onClick={(event) => event.stopPropagation()}>
        <div className="admin-top-chrome">
          <div className="panel-header-row">
            <h2>Admin Panel</h2>
            <div className="button-row">
              <button type="button" onClick={() => void refreshAdmin()} disabled={actionBusy || loadBusy}>{loadBusy ? "Refreshing..." : "Refresh"}</button>
              <button type="button" onClick={props.onClose}>Close</button>
            </div>
          </div>

          {notice ? <div className="notice success">{notice}</div> : null}
          {error ? <div className="notice error">{error}</div> : null}

          <div className="nav-tabs wrap admin-tabs-row">
            <button className={adminTab === "dashboard" ? "active" : ""} onClick={() => setAdminTab("dashboard")} type="button">Dashboard</button>
            <button className={adminTab === "realm" ? "active" : ""} onClick={() => setAdminTab("realm")} type="button">Realm Actions</button>
            <button className={adminTab === "accounts" ? "active" : ""} onClick={() => setAdminTab("accounts")} type="button">Accounts</button>
            <button className={adminTab === "characters" ? "active" : ""} onClick={() => setAdminTab("characters")} type="button">Characters</button>
            <button className={adminTab === "chat" ? "active" : ""} onClick={() => setAdminTab("chat")} type="button">Chat Ops</button>
            <button className={adminTab === "wordlist" ? "active" : ""} onClick={() => setAdminTab("wordlist")} type="button">Wordlist</button>
            <button className={adminTab === "audit" ? "active" : ""} onClick={() => setAdminTab("audit")} type="button">Audit</button>
          </div>
          <p className="muted">Tab data health: {activeTabStatusText}</p>
          {activeTabSegmentFailures.length > 0 ? (
            <div className="button-row">
              {activeTabSegmentFailures.map((segment) => (
                <button
                  key={segment}
                  type="button"
                  onClick={() => void retryAdminSegment(segment)}
                  disabled={actionBusy || !!adminSegmentBusy[segment]}
                >
                  {adminSegmentBusy[segment] ? `Retrying ${adminDataSegmentLabels[segment]}...` : `Retry ${adminDataSegmentLabels[segment]}`}
                </button>
              ))}
            </div>
          ) : null}
        </div>

        <div className="admin-body">
          {renderTabDataStatus(activeTabSegments)}

          {adminTab === "dashboard" ? (
            <div className="split-grid">
              <div>
                <h3>Stats</h3>
                <div className="admin-stats-cards">
                  {summaryStatsCards(adminStats).map((entry) => (
                    <div key={entry.label} className="admin-stats-card">
                      <span>{entry.label}</span>
                      <strong>{entry.value}</strong>
                    </div>
                  ))}
                </div>
                {tickBudgetSummary ? (
                  <div className="info-card">
                    <h4>Tick Budget</h4>
                    <div className="stat-grid">
                      <div className="stat-tile">
                        <span className="stat-label">Average Tick</span>
                        <div className="stat-values"><strong>{formatNumber(tickBudgetSummary.avgTickMs)} ms</strong></div>
                      </div>
                      <div className="stat-tile">
                        <span className="stat-label">Target Tick</span>
                        <div className="stat-values"><strong>{formatNumber(tickBudgetSummary.targetTickMs)} ms</strong></div>
                      </div>
                      <div className="stat-tile">
                        <span className="stat-label">Avg Advance</span>
                        <div className="stat-values"><strong>{formatNumber(tickBudgetSummary.avgAdvanceMinutes)} min</strong></div>
                      </div>
                      <div className="stat-tile">
                        <span className="stat-label">Failure Rate</span>
                        <div className="stat-values"><strong>{formatNumber(tickBudgetSummary.failureRate * 100)}%</strong></div>
                      </div>
                    </div>
                    <div className="progress-wrap stat-progress" aria-label="tick budget usage">
                      <div className="progress-bar" style={{ width: `${tickBudgetUsagePercent}%` }} />
                    </div>
                    <p className="muted">
                      Usage {formatNumber(tickBudgetSummary.budgetRatio * 100)}% · {tickBudgetDeltaLabel} · last tick {formatNumber(tickBudgetSummary.lastTickMs)} ms · runs {formatNumber(tickBudgetSummary.totalRuns, 0)} · failures {formatNumber(tickBudgetSummary.totalFailures, 0)}
                    </p>
                  </div>
                ) : null}
                {hasUnrenderedComplexDashboardStats(adminStats) ? (
                  <p className="muted">Additional complex dashboard stats are present but not first-class rendered yet.</p>
                ) : (
                  <p className="muted">All currently reported dashboard stats are rendered in cards or dedicated tabs.</p>
                )}
              </div>
              <div>
                <h3>Realms</h3>
                <table className="mini-table">
                  <thead>
                    <tr><th>Realm</th><th>Policy</th><th>Active chars</th></tr>
                  </thead>
                  <tbody>
                    {adminRealms.map((realm) => (
                      <tr key={realm.realmId}>
                        <td>{realm.name || `Realm ${realm.realmId}`}</td>
                        <td>{realm.whitelistOnly ? "whitelisted" : "open"}</td>
                        <td>{realm.activeCharacters}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          ) : null}

        {adminTab === "realm" ? (
          <div className="split-grid">
            <div className="column-form">
              <label>
                Existing Realm
                <select value={realmActionRealm} onChange={(event) => setRealmActionRealm(event.target.value)}>
                  {availableRealmIds.length === 0 ? <option value="1">Realm 1</option> : null}
                  {availableRealmIds.map((realmId) => (
                    <option key={realmId} value={String(realmId)}>{formatRealmOptionLabel(realmId)}</option>
                  ))}
                </select>
              </label>

              <h3>Create Realm</h3>
              <label>Display Name<input value={realmCreateName} onChange={(event) => setRealmCreateName(event.target.value)} placeholder="Realm name (optional)" /></label>
              <label>
                <input type="checkbox" checked={realmCreateWhitelistOnly} onChange={(event) => setRealmCreateWhitelistOnly(event.target.checked)} />
                Whitelist character creation
              </label>
              <label>Reason<input value={realmCreateReason} onChange={(event) => setRealmCreateReason(event.target.value)} /></label>
              <label>Note<input value={realmCreateNote} onChange={(event) => setRealmCreateNote(event.target.value)} /></label>
              <button type="button" onClick={() => {
                let createdRealmId = 0;
                return runAdminAction(async () => {
                  const created = await adminCreateRealm({
                    name: realmCreateName.trim() || undefined,
                    whitelistOnly: realmCreateWhitelistOnly,
                    reasonCode: realmCreateReason,
                    note: realmCreateNote || undefined
                  });
                  createdRealmId = created.realmId;
                  setRealmActionRealm(String(created.realmId));
                }, () => createdRealmId > 0 ? `Realm ${createdRealmId} created.` : "Realm created.", () => {
                  setRealmCreateName("");
                  setRealmCreateWhitelistOnly(false);
                  setRealmCreateNote("");
                });
              }} disabled={actionBusy}>Create realm</button>

              <h3>Realm Metadata</h3>
              <label>Display Name<input value={realmConfigName} onChange={(event) => setRealmConfigName(event.target.value)} /></label>
              <label>
                <input type="checkbox" checked={realmConfigWhitelistOnly} onChange={(event) => setRealmConfigWhitelistOnly(event.target.checked)} />
                Whitelist character creation
              </label>
              <button type="button" onClick={() => runAdminAction(async () => {
                const realmId = parseNumber(realmActionRealm);
                if (!realmId) {
                  throw new Error("realm id is required");
                }
                await adminSetRealmConfig(realmId, { command: "edit", name: realmConfigName.trim(), whitelistOnly: realmConfigWhitelistOnly });
              }, "Realm metadata edited.")} disabled={actionBusy}>Edit metadata</button>

              <h3>Realm Actions</h3>
              <label>
                Action
                <select value={realmActionType} onChange={(event) => setRealmActionType(event.target.value)}>
                  <option value="realm_pause">realm_pause</option>
                  <option value="realm_resume">realm_resume</option>
                  <option value="realm_decommission">realm_decommission</option>
                  <option value="realm_recommission">realm_recommission</option>
                  <option value="realm_delete">realm_delete</option>
                  <option value="market_reset_defaults">market_reset_defaults</option>
                  <option value="market_set_price">market_set_price</option>
                </select>
              </label>
              <label>Reason<input value={realmActionReason} onChange={(event) => setRealmActionReason(event.target.value)} /></label>
              <label>Note<input value={realmActionNote} onChange={(event) => setRealmActionNote(event.target.value)} /></label>
              {realmActionType === "market_set_price" ? (
                <>
                  <label>Item key (for market_set_price)<input value={realmActionItemKey} onChange={(event) => setRealmActionItemKey(event.target.value)} /></label>
                  <label>Price (for market_set_price)<input value={realmActionPrice} onChange={(event) => setRealmActionPrice(event.target.value)} /></label>
                </>
              ) : null}
              <button type="button" onClick={() => runAdminAction(async () => {
                const realmId = parseNumber(realmActionRealm);
                if (!realmId) {
                  throw new Error("realm id is required");
                }
                await adminApplyRealmAction(realmId, {
                  action: realmActionType,
                  reasonCode: realmActionReason,
                  note: realmActionNote || undefined,
                  itemKey: realmActionItemKey || undefined,
                  price: parseNumber(realmActionPrice)
                });
              }, "Realm action applied.", () => {
                setRealmActionNote("");
                setRealmActionItemKey("");
                setRealmActionPrice("");
              })} disabled={actionBusy}>Apply action</button>
            </div>

            <div className="column-form">
              <h3>Whitelist Access</h3>
              <label>Account ID<input value={realmAccessAccountId} onChange={(event) => setRealmAccessAccountId(event.target.value)} /></label>
              <label>Reason<input value={realmAccessReason} onChange={(event) => setRealmAccessReason(event.target.value)} /></label>
              <label>Note<input value={realmAccessNote} onChange={(event) => setRealmAccessNote(event.target.value)} /></label>
              <div className="button-row">
                <button type="button" onClick={() => runAdminAction(async () => {
                  const realmId = parseNumber(realmActionRealm);
                  const accountId = parseNumber(realmAccessAccountId);
                  if (!realmId) throw new Error("realm id is required");
                  if (!accountId) throw new Error("account id is required");
                  await adminGrantRealmAccess(realmId, { accountId, reasonCode: realmAccessReason, note: realmAccessNote || undefined });
                }, "Realm access granted.")} disabled={actionBusy}>Grant access</button>
                <button type="button" onClick={() => runAdminAction(async () => {
                  const realmId = parseNumber(realmActionRealm);
                  const accountId = parseNumber(realmAccessAccountId);
                  if (!realmId) throw new Error("realm id is required");
                  if (!accountId) throw new Error("account id is required");
                  await adminRevokeRealmAccess(realmId, { accountId, reasonCode: realmAccessReason, note: realmAccessNote || undefined });
                }, "Realm access revoked.")} disabled={actionBusy}>Revoke access</button>
              </div>
              <table className="mini-table">
                <thead>
                  <tr><th>Account</th><th>Reason</th><th>Updated</th></tr>
                </thead>
                <tbody>
                  {realmAccessEntries.length === 0 ? (
                    <tr><td colSpan={3}>No active access grants.</td></tr>
                  ) : (
                    realmAccessEntries.map((entry) => (
                      <tr key={entry.id}>
                        <td>{entry.accountUsername} ({entry.accountId})</td>
                        <td>{entry.reasonCode}</td>
                        <td>{new Date(entry.updatedAt).toLocaleString()}</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        ) : null}

        {adminTab === "accounts" ? (
          <div className="split-grid">
            <div className="column-form">
              <label>Account ID<input value={accountTargetId} onChange={(event) => setAccountTargetId(event.target.value)} /></label>
              <label>Reason<input value={accountReason} onChange={(event) => setAccountReason(event.target.value)} /></label>
              <label>Note<input value={accountNote} onChange={(event) => setAccountNote(event.target.value)} /></label>
              <div className="button-row">
                <button type="button" onClick={() => runAdminAction(async () => {
                  const accountId = parseNumber(accountTargetId);
                  if (!accountId) throw new Error("account id is required");
                  await adminModerateAccount(accountId, "lock", accountReason, accountNote || undefined);
                }, "Account locked.")} disabled={actionBusy}>Lock</button>
                <button type="button" onClick={() => runAdminAction(async () => {
                  const accountId = parseNumber(accountTargetId);
                  if (!accountId) throw new Error("account id is required");
                  await adminModerateAccount(accountId, "unlock", accountReason, accountNote || undefined);
                }, "Account unlocked.")} disabled={actionBusy}>Unlock</button>
              </div>
              <label>
                Status
                <select value={accountStatus} onChange={(event) => setAccountStatus(event.target.value as "active" | "locked")}>
                  <option value="active">active</option>
                  <option value="locked">locked</option>
                </select>
              </label>
              <button type="button" onClick={() => runAdminAction(async () => {
                const accountId = parseNumber(accountTargetId);
                if (!accountId) throw new Error("account id is required");
                await adminSetAccountStatus(accountId, {
                  command: "set_status",
                  status: accountStatus,
                  reasonCode: accountReason,
                  note: accountNote || undefined,
                  revokeSessions: accountStatus === "locked"
                });
              }, "Account status command applied.")} disabled={actionBusy}>Apply status command</button>
            </div>

            <div className="column-form">
              <h3>Role moderation</h3>
              <label>Role key<input value={accountRoleKey} onChange={(event) => setAccountRoleKey(event.target.value)} /></label>
              <div className="button-row">
                <button type="button" onClick={() => runAdminAction(async () => {
                  const accountId = parseNumber(accountTargetId);
                  if (!accountId) throw new Error("account id is required");
                  await adminSetAccountRole(accountId, { command: "set_role", roleKey: accountRoleKey, action: "grant", reasonCode: accountReason, note: accountNote || undefined });
                }, "Role granted.")} disabled={actionBusy}>Grant role</button>
                <button type="button" onClick={() => runAdminAction(async () => {
                  const accountId = parseNumber(accountTargetId);
                  if (!accountId) throw new Error("account id is required");
                  await adminSetAccountRole(accountId, { command: "set_role", roleKey: accountRoleKey, action: "revoke", reasonCode: accountReason, note: accountNote || undefined });
                }, "Role revoked.")} disabled={actionBusy}>Revoke role</button>
              </div>
            </div>
          </div>
        ) : null}

        {adminTab === "characters" ? (
          <div className="split-grid">
            <div>
              <div className="button-row">
                <input value={characterFilterName} onChange={(event) => setCharacterFilterName(event.target.value)} placeholder="name filter" />
                <button type="button" onClick={() => runAdminAction(async () => {
                  const result = await adminListCharacters({ nameLike: characterFilterName || undefined, limit: 100 });
                  setAdminCharacters(result.entries ?? []);
                }, "Character list refreshed.")} disabled={actionBusy}>Search</button>
              </div>
              <table className="mini-table">
                <thead><tr><th>ID</th><th>Name</th><th>Realm</th><th>Status</th></tr></thead>
                <tbody>
                  {adminCharacters.map((entry) => (
                    <tr key={entry.id} onClick={() => {
                      setCharacterEditId(String(entry.id));
                      setCharacterEditName(entry.name);
                      setCharacterEditStatus(entry.status === "locked" ? "locked" : "active");
                      setCharacterEditPrimary(entry.isPrimary);
                    }}>
                      <td>{entry.id}</td><td>{entry.name}</td><td>{entry.realmId}</td><td>{entry.status}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div className="column-form">
              <label>Character ID<input value={characterEditId} onChange={(event) => setCharacterEditId(event.target.value)} /></label>
              <label>Name<input value={characterEditName} onChange={(event) => setCharacterEditName(event.target.value)} /></label>
              <label>
                Status
                <select value={characterEditStatus} onChange={(event) => setCharacterEditStatus(event.target.value as "active" | "locked")}>
                  <option value="active">active</option>
                  <option value="locked">locked</option>
                </select>
              </label>
              <label className="checkbox-row"><input type="checkbox" checked={characterEditPrimary} onChange={(event) => setCharacterEditPrimary(event.target.checked)} />Set primary</label>
              <label>Reason<input value={characterEditReason} onChange={(event) => setCharacterEditReason(event.target.value)} /></label>
              <button type="button" onClick={() => runAdminAction(async () => {
                const characterId = parseNumber(characterEditId);
                if (!characterId) throw new Error("character id is required");
                await adminModerateCharacter(characterId, {
                  command: "edit",
                  name: characterEditName || undefined,
                  status: characterEditStatus,
                  isPrimary: characterEditPrimary,
                  reasonCode: characterEditReason
                });
                const result = await adminListCharacters({ limit: 100 });
                setAdminCharacters(result.entries ?? []);
              }, "Character edit command applied.")} disabled={actionBusy}>Apply edit command</button>
            </div>
          </div>
        ) : null}

        {adminTab === "chat" ? (
          <div className="split-grid">
            <div className="column-form">
              <h3>Create channel</h3>
              <label>
                Realm
                <select value={chatCreateRealmId} onChange={(event) => setChatCreateRealmId(event.target.value)}>
                  {availableRealmIds.length === 0 ? <option value="1">Realm 1</option> : null}
                  {availableRealmIds.map((realmId) => (
                    <option key={realmId} value={String(realmId)}>{formatRealmOptionLabel(realmId)}</option>
                  ))}
                </select>
              </label>
              <label>Channel Key<input value={chatCreateKey} onChange={(event) => setChatCreateKey(event.target.value)} placeholder="new-channel-key" /></label>
              <label>Name<input value={chatCreateName} onChange={(event) => setChatCreateName(event.target.value)} /></label>
              <label>Subject<input value={chatCreateSubject} onChange={(event) => setChatCreateSubject(event.target.value)} /></label>
              <label>Description<input value={chatCreateDescription} onChange={(event) => setChatCreateDescription(event.target.value)} /></label>
              <button type="button" onClick={() => runAdminAction(async () => {
                const realmId = parseNumber(chatCreateRealmId);
                const key = chatCreateKey.trim();
                const name = chatCreateName.trim();
                if (!realmId) throw new Error("create realm is required");
                if (!key) throw new Error("channel key is required");
                if (!name) throw new Error("channel name is required");

                await adminChatCreateChannel({
                  realmId,
                  key,
                  name,
                  subject: chatCreateSubject.trim() || undefined,
                  description: chatCreateDescription.trim() || undefined
                });

                setChatEditBinding(key.toLowerCase());
                setChatAssignmentChannelKey(key.toLowerCase());
              }, "Channel created.", () => {
                setChatCreateKey("");
                setChatCreateName("");
                setChatCreateSubject("");
                setChatCreateDescription("");
              })} disabled={actionBusy}>Create channel key + binding</button>

              <h3>Edit channel</h3>
              <p className="muted">Channel metadata (name/subject/description) is global per channel key across realm bindings.</p>
              <label>
                Channel key
                <select value={chatEditBinding} onChange={(event) => setChatEditBinding(event.target.value)}>
                  {knownChannelKeys.map((entry) => (
                    <option key={entry.key} value={entry.key.toLowerCase()}>{entry.key}</option>
                  ))}
                </select>
              </label>
              <label>Name<input value={chatEditName} onChange={(event) => setChatEditName(event.target.value)} /></label>
              <label>Subject<input value={chatEditSubject} onChange={(event) => setChatEditSubject(event.target.value)} /></label>
              <label>Description<input value={chatEditDescription} onChange={(event) => setChatEditDescription(event.target.value)} /></label>
              <button type="button" onClick={() => runAdminAction(async () => {
                if (!selectedEditBinding) {
                  throw new Error("select a channel key to edit");
                }
                const name = chatEditName.trim();
                if (!name) {
                  throw new Error("channel name is required");
                }

                await adminChatEditChannel({
                  realmId: selectedEditBinding.realmId,
                  key: selectedEditBinding.key,
                  name,
                  subject: chatEditSubject.trim() || undefined,
                  description: chatEditDescription.trim() || undefined
                });
              }, "Channel metadata updated.")} disabled={actionBusy}>Save global metadata</button>

              <h3>Realm membership</h3>
              <label>
                Source channel key
                <select value={chatAssignmentChannelKey} onChange={(event) => setChatAssignmentChannelKey(event.target.value)}>
                  {knownChannelKeys.map((entry) => (
                    <option key={entry.key} value={entry.key.toLowerCase()}>{entry.key}</option>
                  ))}
                </select>
              </label>
              <label>
                Target realm
                <select value={chatAssignmentRealmId} onChange={(event) => setChatAssignmentRealmId(event.target.value)}>
                  {availableRealmIds.length === 0 ? <option value="1">Realm 1</option> : null}
                  {availableRealmIds.map((realmId) => (
                    <option key={realmId} value={String(realmId)}>{formatRealmOptionLabel(realmId)}</option>
                  ))}
                </select>
              </label>
              <div className="button-row">
                <button type="button" onClick={() => runAdminAction(async () => {
                  const targetRealmId = parseNumber(chatAssignmentRealmId);
                  if (!targetRealmId) {
                    throw new Error("target realm is required");
                  }
                  if (!selectedAssignmentChannel) {
                    throw new Error("source channel key is required");
                  }

                  await adminChatAttachChannel({
                    realmId: targetRealmId,
                    key: selectedAssignmentChannel.key
                  });
                }, "Channel binding added to realm.")} disabled={actionBusy}>Attach key to realm</button>
                <button type="button" onClick={() => runAdminAction(async () => {
                  const targetRealmId = parseNumber(chatAssignmentRealmId);
                  if (!targetRealmId) {
                    throw new Error("target realm is required");
                  }
                  if (!selectedAssignmentChannel) {
                    throw new Error("source channel key is required");
                  }

                  await adminChatDisableChannel(selectedAssignmentChannel.key, { realmId: targetRealmId });
                }, "Channel binding removed from realm.")} disabled={actionBusy}>Detach key from realm</button>
              </div>

              <div>
                <h4>Active channel bindings</h4>
                <table className="mini-table">
                  <thead><tr><th>Channel</th><th>Realm</th><th>Name</th></tr></thead>
                  <tbody>
                    {adminChatChannels.filter((entry) => entry.active !== false).length === 0 ? (
                      <tr><td colSpan={3}>No active channel bindings.</td></tr>
                    ) : (
                      adminChatChannels.filter((entry) => entry.active !== false).map((entry) => (
                        <tr key={`${entry.realmId}:${entry.key}`}>
                          <td>{entry.key}</td>
                          <td>{entry.realmId}</td>
                          <td>{entry.name}</td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </div>

            <div className="column-form">
              <h3>Moderation + system message</h3>
              <label>
                Channel key
                <select value={chatEditBinding} onChange={(event) => setChatEditBinding(event.target.value)}>
                  {knownChannelKeys.map((entry) => (
                    <option key={entry.key} value={entry.key.toLowerCase()}>{entry.key}</option>
                  ))}
                </select>
              </label>
              <label>
                Operation realm
                <select value={chatAdminOpRealmId} onChange={(event) => setChatAdminOpRealmId(event.target.value)}>
                  {availableRealmIds.length === 0 ? <option value="1">Realm 1</option> : null}
                  {availableRealmIds.map((realmId) => (
                    <option key={realmId} value={String(realmId)}>{formatRealmOptionLabel(realmId)}</option>
                  ))}
                </select>
              </label>
              <button type="button" onClick={() => {
                let summary: RealmActionSummary | null = null;
                return runAdminAction(async () => {
                  summary = await runChatActionForOperationRealm(async (realmId) => {
                    await adminChatFlushChannel(activeChannelKey, { realmId, reasonCode: chatAdminReason, note: chatAdminNote || undefined });
                  });
                  if (summary.succeededRealmIds.length <= 0) {
                    throw new Error(`Channel flush failed for operation realm: ${summary.failed.map((entry) => `${entry.realmId} (${entry.reason})`).join("; ")}`);
                  }
                }, () => formatRealmActionSummary("Channel flush completed.", summary), () => {
                  setChatAdminNote("");
                });
              }} disabled={actionBusy}>Flush channel messages</button>
              <label>Account ID<input value={chatAdminAccountId} onChange={(event) => setChatAdminAccountId(event.target.value)} /></label>
              <label>
                Action
                <select value={chatAdminAction} onChange={(event) => setChatAdminAction(event.target.value as "ban" | "unban" | "kick")}>
                  <option value="ban">ban</option>
                  <option value="unban">unban</option>
                  <option value="kick">kick</option>
                </select>
              </label>
              <label>Duration minutes<input value={chatAdminDuration} onChange={(event) => setChatAdminDuration(event.target.value)} /></label>
              <label>Reason<input value={chatAdminReason} onChange={(event) => setChatAdminReason(event.target.value)} /></label>
              <label>Note<input value={chatAdminNote} onChange={(event) => setChatAdminNote(event.target.value)} /></label>
              <button type="button" onClick={() => {
                let summary: RealmActionSummary | null = null;
                return runAdminAction(async () => {
                  const accountId = parseNumber(chatAdminAccountId);
                  if (!accountId) throw new Error("account id is required");
                  summary = await runChatActionForOperationRealm(async (realmId) => {
                    await adminChatModerateChannel(activeChannelKey, {
                      realmId,
                      accountId,
                      action: chatAdminAction,
                      durationMinutes: parseNumber(chatAdminDuration),
                      reasonCode: chatAdminReason,
                      note: chatAdminNote || undefined
                    });
                  });
                  if (summary.succeededRealmIds.length <= 0) {
                    throw new Error(`Chat moderation failed for all target realms: ${summary.failed.map((entry) => `${entry.realmId} (${entry.reason})`).join("; ")}`);
                  }
                }, () => formatRealmActionSummary("Chat moderation completed.", summary));
              }} disabled={actionBusy}>Apply moderation</button>

              <label>System message<textarea value={chatAdminSystemMessage} onChange={(event) => setChatAdminSystemMessage(event.target.value)} /></label>
              <button type="button" onClick={() => {
                let summary: RealmActionSummary | null = null;
                return runAdminAction(async () => {
                  summary = await runChatActionForOperationRealm(async (realmId) => {
                    await adminChatSystemMessage(activeChannelKey, {
                      realmId,
                      message: chatAdminSystemMessage,
                      reasonCode: chatAdminReason,
                      note: chatAdminNote || undefined
                    });
                  });
                  if (summary.succeededRealmIds.length <= 0) {
                    throw new Error(`System message publish failed for all target realms: ${summary.failed.map((entry) => `${entry.realmId} (${entry.reason})`).join("; ")}`);
                  }
                }, () => formatRealmActionSummary("System message publish completed.", summary), () => {
                  setChatAdminSystemMessage("");
                  setChatAdminNote("");
                });
              }} disabled={actionBusy || !chatAdminSystemMessage.trim()}>Publish system message</button>
            </div>
          </div>
        ) : null}

        {adminTab === "wordlist" ? (
          <div className="split-grid">
            <div className="column-form">
              <h3>Add word rule</h3>
              <label>Term<input value={wordTerm} onChange={(event) => setWordTerm(event.target.value)} /></label>
              <label>Reason<input value={wordReason} onChange={(event) => setWordReason(event.target.value)} /></label>
              <label>Note<input value={wordNote} onChange={(event) => setWordNote(event.target.value)} /></label>
              <button type="button" onClick={() => runAdminAction(async () => {
                await adminChatWordlistAdd({ term: wordTerm, reasonCode: wordReason, note: wordNote || undefined, matchMode: "contains" });
                const wordlist = await adminChatWordlistList();
                setAdminWordRules(wordlist.rules ?? []);
                setWordTerm("");
              }, "Wordlist rule added.")} disabled={actionBusy || wordTerm.trim().length < 2}>Add rule</button>
            </div>

            <div>
              <h3>Current rules</h3>
              <table className="mini-table">
                <thead><tr><th>ID</th><th>Term</th><th>Mode</th><th /></tr></thead>
                <tbody>
                  {adminWordRules.map((rule) => (
                    <tr key={rule.id}>
                      <td>{rule.id}</td>
                      <td>{rule.term}</td>
                      <td>{rule.matchMode}</td>
                      <td>
                        <button type="button" onClick={() => runAdminAction(async () => {
                          await adminChatWordlistRemove(rule.id);
                          const wordlist = await adminChatWordlistList();
                          setAdminWordRules(wordlist.rules ?? []);
                        }, "Wordlist rule removed.")} disabled={actionBusy}>Remove</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        ) : null}

        {adminTab === "audit" ? (
          <div>
            <div className="audit-viz-grid">
              <section className="audit-viz-card">
                <h3>
                  By Action
                  <span className="audit-viz-source">{auditActionSourceLabel}</span>
                </h3>
                <div className="audit-donut-row">
                  <div className="audit-donut" style={{ background: donutGradient(auditActionDistribution.slices, auditActionDistribution.total) }}>
                    <strong>{auditActionDistribution.total}</strong>
                    <span>entries</span>
                  </div>
                  <div className="audit-bars">
                    {auditActionDistribution.slices.map((slice) => {
                      const percent = auditActionDistribution.total > 0 ? (slice.count / auditActionDistribution.total) * 100 : 0;
                      return (
                        <div key={slice.label} className="audit-bar-row">
                          <div className="audit-bar-label">
                            <span className="audit-color" style={{ background: slice.color }} />
                            <span>{slice.label}</span>
                            <strong>{slice.count}</strong>
                          </div>
                          <div className="audit-bar-track">
                            <div className="audit-bar-fill" style={{ width: `${percent}%`, background: slice.color }} />
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
              </section>

              <section className="audit-viz-card">
                <h3>
                  By Realm
                  <span className="audit-viz-source">{auditRealmSourceLabel}</span>
                </h3>
                <div className="audit-donut-row">
                  <div className="audit-donut" style={{ background: donutGradient(auditRealmDistribution.slices, auditRealmDistribution.total) }}>
                    <strong>{auditRealmDistribution.total}</strong>
                    <span>entries</span>
                  </div>
                  <div className="audit-bars">
                    {auditRealmDistribution.slices.map((slice) => {
                      const percent = auditRealmDistribution.total > 0 ? (slice.count / auditRealmDistribution.total) * 100 : 0;
                      return (
                        <div key={slice.label} className="audit-bar-row">
                          <div className="audit-bar-label">
                            <span className="audit-color" style={{ background: slice.color }} />
                            <span>{slice.label}</span>
                            <strong>{slice.count}</strong>
                          </div>
                          <div className="audit-bar-track">
                            <div className="audit-bar-fill" style={{ width: `${percent}%`, background: slice.color }} />
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
              </section>
            </div>

            <div className="button-row">
              <input value={auditActionFilter} onChange={(event) => setAuditActionFilter(event.target.value)} placeholder="action filter" />
              <input value={auditUserFilter} onChange={(event) => setAuditUserFilter(event.target.value)} placeholder="actor username" />
              <button type="button" onClick={() => runAdminAction(async () => {
                const result = await adminAuditList({ actionKey: auditActionFilter || undefined, actorUsername: auditUserFilter || undefined, includeRawJson: false, limit: 100 });
                setAdminAudit(result.entries ?? []);
              }, "Audit reloaded.")} disabled={actionBusy}>Search</button>
            </div>
            <div className="audit-list">
              {adminAudit.map((entry) => (
                <article key={entry.id} className="feed-row">
                  <div>
                    <strong>#{entry.id}</strong> · {entry.actionKey} · realm {entry.realmId}
                  </div>
                  <div>
                    actor {(entry.actorUsername || "").trim() || "unknown"} · account #{entry.actorAccountId || "?"} · tick {entry.occurredTick}
                  </div>
                  <div className="muted">reason: {entry.reasonCode}{entry.note ? ` · ${entry.note}` : ""}</div>
                </article>
              ))}
            </div>
          </div>
        ) : null}
        </div>
      </div>
    </div>
  );
}
