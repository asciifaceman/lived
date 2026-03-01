import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AdminAuditEntry,
  AdminCharacterEntry,
  AdminWordRule,
  adminApplyRealmAction,
  adminAuditList,
  adminChatDisableChannel,
  adminChatFlushChannel,
  adminChatModerateChannel,
  adminChatSystemMessage,
  adminChatUpsertChannel,
  adminChatWordlistAdd,
  adminChatWordlistList,
  adminChatWordlistRemove,
  adminGetRealms,
  adminGetStats,
  adminListCharacters,
  adminModerateAccount,
  adminModerateCharacter,
  adminSetAccountRole,
  adminSetAccountStatus
} from "../api";

type AdminTab = "dashboard" | "realm" | "accounts" | "characters" | "chat" | "wordlist" | "audit";

type AdminModalProps = {
  open: boolean;
  onClose: () => void;
  onChanged?: () => Promise<void> | void;
  knownChannelKeys?: string[];
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

function parseRealmList(value: string): { realmIds: number[]; allRealms: boolean } {
  const tokens = value
    .split(",")
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0);

  if (tokens.length === 0) {
    return { realmIds: [], allRealms: false };
  }

  const ids = new Set<number>();
  let allRealms = false;
  for (const token of tokens) {
    if (token === "*") {
      allRealms = true;
      continue;
    }

    const parsed = Number(token);
    if (!Number.isInteger(parsed) || parsed <= 0) {
      throw new Error(`invalid realm id in list: ${token}`);
    }
    ids.add(parsed);
  }

  return { realmIds: Array.from(ids.values()).sort((left, right) => left - right), allRealms };
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
  "publicWorldEvents"
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
	const [chatRealmTargetMode, setChatRealmTargetMode] = useState<"all" | "single" | "list">("single");
  const [adminTab, setAdminTab] = useState<AdminTab>("dashboard");
  const [actionBusy, setActionBusy] = useState(false);
  const [loadBusy, setLoadBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const [adminRealms, setAdminRealms] = useState<Array<{ realmId: number; activeCharacters: number }>>([]);
  const [adminStats, setAdminStats] = useState<Record<string, unknown> | null>(null);
  const [adminWordRules, setAdminWordRules] = useState<AdminWordRule[]>([]);
  const [adminAudit, setAdminAudit] = useState<AdminAuditEntry[]>([]);
  const [adminCharacters, setAdminCharacters] = useState<AdminCharacterEntry[]>([]);

  const [realmActionRealm, setRealmActionRealm] = useState("1");
  const [realmActionType, setRealmActionType] = useState("realm_create");
  const [realmActionReason, setRealmActionReason] = useState("ops");
  const [realmActionNote, setRealmActionNote] = useState("");
  const [realmActionItemKey, setRealmActionItemKey] = useState("");
  const [realmActionPrice, setRealmActionPrice] = useState("");

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

  const [chatAdminChannel, setChatAdminChannel] = useState("global");
  const [chatChannelInputMode, setChatChannelInputMode] = useState<"known" | "new">("known");
  const [chatAdminKnownChannel, setChatAdminKnownChannel] = useState("global");
  const [chatAdminNewChannel, setChatAdminNewChannel] = useState("");
  const [chatAdminRealmId, setChatAdminRealmId] = useState("1");
  const [chatAdminRealmList, setChatAdminRealmList] = useState("");
  const [chatAdminName, setChatAdminName] = useState("Global");
  const [chatAdminSubject, setChatAdminSubject] = useState("");
  const [chatAdminDescription, setChatAdminDescription] = useState("");
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

  const knownChannelOptions = useMemo(() => {
    const source = props.knownChannelKeys ?? [];
    const normalized = source
      .map((entry) => entry.trim())
      .filter((entry) => entry.length > 0);

    const withGlobal = normalized.includes("global") ? normalized : ["global", ...normalized];
    return Array.from(new Set(withGlobal)).sort((left, right) => left.localeCompare(right));
  }, [props.knownChannelKeys]);

  useEffect(() => {
    if (knownChannelOptions.length === 0) {
      if (chatAdminKnownChannel !== "global") {
        setChatAdminKnownChannel("global");
      }
      return;
    }

    if (!knownChannelOptions.includes(chatAdminKnownChannel)) {
      setChatAdminKnownChannel(knownChannelOptions[0]);
    }
  }, [chatAdminKnownChannel, knownChannelOptions]);

  useEffect(() => {
    const selected = chatChannelInputMode === "known" ? chatAdminKnownChannel : chatAdminNewChannel;
    setChatAdminChannel(selected.trim() || "global");
  }, [chatAdminKnownChannel, chatAdminNewChannel, chatChannelInputMode]);

  const availableRealmIds = adminRealms
    .map((entry) => entry.realmId)
    .filter((realmId) => Number.isInteger(realmId) && realmId > 0)
    .sort((left, right) => left - right);

  const resolveChatRealmTargets = useCallback((): number[] => {
    const knownRealmIds = availableRealmIds.length > 0 ? availableRealmIds : [1];

    if (chatRealmTargetMode === "all") {
      return knownRealmIds;
    }

    if (chatRealmTargetMode === "single") {
      const singleRealmId = parseNumber(chatAdminRealmId);
      if (!singleRealmId || singleRealmId <= 0) {
        throw new Error("select a valid realm");
      }
      return [singleRealmId];
    }

    const parsed = parseRealmList(chatAdminRealmList);
    if (parsed.allRealms) {
      return knownRealmIds;
    }

    if (parsed.realmIds.length === 0) {
      throw new Error("enter at least one realm id in the realm list");
    }

    return parsed.realmIds;
  }, [availableRealmIds, chatAdminRealmId, chatAdminRealmList, chatRealmTargetMode]);

  const runChatActionAcrossRealms = useCallback(async (runner: (realmId: number) => Promise<void>): Promise<RealmActionSummary> => {
    const targets = resolveChatRealmTargets();
    const summary: RealmActionSummary = {
      targetRealmIds: targets,
      succeededRealmIds: [],
      failed: []
    };

    for (const realmId of targets) {
      try {
        await runner(realmId);
        summary.succeededRealmIds.push(realmId);
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        summary.failed.push({ realmId, reason: message });
      }
    }

    return summary;
  }, [resolveChatRealmTargets]);

  useEffect(() => {
    if (chatRealmTargetMode !== "single") {
      return;
    }

    if (availableRealmIds.length === 0) {
      if (chatAdminRealmId !== "1") {
        setChatAdminRealmId("1");
      }
      return;
    }

    const current = parseNumber(chatAdminRealmId);
    if (!current || !availableRealmIds.includes(current)) {
      setChatAdminRealmId(String(availableRealmIds[0]));
    }
  }, [availableRealmIds, chatAdminRealmId, chatRealmTargetMode]);

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

  const loadAdmin = useCallback(async () => {
    const results = await Promise.allSettled([
      adminGetRealms(),
      adminGetStats(),
      adminChatWordlistList(),
      adminAuditList({ limit: 50 }),
      adminListCharacters({ limit: 50 })
    ]);

    const [realmsResult, statsResult, wordsResult, auditResult, characterResult] = results;
    const failures: string[] = [];

    if (realmsResult.status === "fulfilled") {
      setAdminRealms(realmsResult.value.realms ?? []);
    } else {
      failures.push("realms");
    }

    if (statsResult.status === "fulfilled") {
      setAdminStats(statsResult.value);
    } else {
      failures.push("stats");
    }

    if (wordsResult.status === "fulfilled") {
      setAdminWordRules(wordsResult.value.rules ?? []);
    } else {
      failures.push("wordlist");
    }

    if (auditResult.status === "fulfilled") {
      setAdminAudit(auditResult.value.entries ?? []);
    } else {
      failures.push("audit");
    }

    if (characterResult.status === "fulfilled") {
      setAdminCharacters(characterResult.value.entries ?? []);
    } else {
      failures.push("characters");
    }

    if (failures.length > 0) {
      setError(`Some admin data failed to load: ${failures.join(", ")}. You can still use other tabs.`);
      return;
    }

    setError(null);
  }, []);

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
        </div>

        <div className="admin-body">
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
                    <tr><th>Realm</th><th>Active chars</th></tr>
                  </thead>
                  <tbody>
                    {adminRealms.map((realm) => (
                      <tr key={realm.realmId}><td>{realm.realmId}</td><td>{realm.activeCharacters}</td></tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          ) : null}

        {adminTab === "realm" ? (
          <div className="column-form">
            <label>Realm ID<input value={realmActionRealm} onChange={(event) => setRealmActionRealm(event.target.value)} /></label>
            <label>
              Action
              <select value={realmActionType} onChange={(event) => setRealmActionType(event.target.value)}>
                <option value="realm_create">realm_create</option>
                <option value="realm_pause">realm_pause</option>
                <option value="realm_resume">realm_resume</option>
                <option value="market_reset_defaults">market_reset_defaults</option>
                <option value="market_set_price">market_set_price</option>
              </select>
            </label>
            <label>Reason<input value={realmActionReason} onChange={(event) => setRealmActionReason(event.target.value)} /></label>
            <label>Note<input value={realmActionNote} onChange={(event) => setRealmActionNote(event.target.value)} /></label>
            <label>Item key (for market_set_price)<input value={realmActionItemKey} onChange={(event) => setRealmActionItemKey(event.target.value)} /></label>
            <label>Price (for market_set_price)<input value={realmActionPrice} onChange={(event) => setRealmActionPrice(event.target.value)} /></label>
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
                  status: accountStatus,
                  reasonCode: accountReason,
                  note: accountNote || undefined,
                  revokeSessions: accountStatus === "locked"
                });
              }, "Account status updated.")} disabled={actionBusy}>Set status</button>
            </div>

            <div className="column-form">
              <h3>Role moderation</h3>
              <label>Role key<input value={accountRoleKey} onChange={(event) => setAccountRoleKey(event.target.value)} /></label>
              <div className="button-row">
                <button type="button" onClick={() => runAdminAction(async () => {
                  const accountId = parseNumber(accountTargetId);
                  if (!accountId) throw new Error("account id is required");
                  await adminSetAccountRole(accountId, { roleKey: accountRoleKey, action: "grant", reasonCode: accountReason, note: accountNote || undefined });
                }, "Role granted.")} disabled={actionBusy}>Grant role</button>
                <button type="button" onClick={() => runAdminAction(async () => {
                  const accountId = parseNumber(accountTargetId);
                  if (!accountId) throw new Error("account id is required");
                  await adminSetAccountRole(accountId, { roleKey: accountRoleKey, action: "revoke", reasonCode: accountReason, note: accountNote || undefined });
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
                  name: characterEditName || undefined,
                  status: characterEditStatus,
                  isPrimary: characterEditPrimary,
                  reasonCode: characterEditReason
                });
                const result = await adminListCharacters({ limit: 100 });
                setAdminCharacters(result.entries ?? []);
              }, "Character updated.")} disabled={actionBusy}>Apply character moderation</button>
            </div>
          </div>
        ) : null}

        {adminTab === "chat" ? (
          <div className="split-grid">
            <div className="column-form">
              <h3>Channel management</h3>
              <label>
                Channel Input
                <select value={chatChannelInputMode} onChange={(event) => setChatChannelInputMode(event.target.value as "known" | "new")}>
                  <option value="known">Known channel</option>
                  <option value="new">New channel key</option>
                </select>
              </label>
              {chatChannelInputMode === "known" ? (
                <label>
                  Known channel key
                  <select value={chatAdminKnownChannel} onChange={(event) => setChatAdminKnownChannel(event.target.value)}>
                    {knownChannelOptions.map((channelKey) => (
                      <option key={channelKey} value={channelKey}>{channelKey}</option>
                    ))}
                  </select>
                </label>
              ) : (
                <label>
                  New channel key
                  <input value={chatAdminNewChannel} onChange={(event) => setChatAdminNewChannel(event.target.value)} placeholder="new-channel-key" />
                </label>
              )}
              <label>
                Realm Target
                <select value={chatRealmTargetMode} onChange={(event) => setChatRealmTargetMode(event.target.value as "all" | "single" | "list")}>
                  <option value="all">* all realms</option>
                  <option value="single">specific realm</option>
                  <option value="list">list of realms</option>
                </select>
              </label>
              {chatRealmTargetMode === "single" ? (
                <label>
                  Realm
                  <select value={chatAdminRealmId} onChange={(event) => setChatAdminRealmId(event.target.value)}>
                    {availableRealmIds.length === 0 ? <option value="1">realm 1</option> : null}
                    {availableRealmIds.map((realmId) => (
                      <option key={realmId} value={String(realmId)}>realm {realmId}</option>
                    ))}
                  </select>
                </label>
              ) : null}
              {chatRealmTargetMode === "list" ? (
                <label>
                  Realm List
                  <input
                    value={chatAdminRealmList}
                    onChange={(event) => setChatAdminRealmList(event.target.value)}
                    placeholder="1,2,3 or *"
                  />
                </label>
              ) : null}
              <label>Name<input value={chatAdminName} onChange={(event) => setChatAdminName(event.target.value)} /></label>
              <label>Subject<input value={chatAdminSubject} onChange={(event) => setChatAdminSubject(event.target.value)} /></label>
              <label>Description<input value={chatAdminDescription} onChange={(event) => setChatAdminDescription(event.target.value)} /></label>
              <div className="button-row">
                <button type="button" onClick={() => {
                  let summary: RealmActionSummary | null = null;
                  return runAdminAction(async () => {
                    summary = await runChatActionAcrossRealms(async (realmId) => {
                      await adminChatUpsertChannel({ realmId, key: chatAdminChannel, name: chatAdminName, subject: chatAdminSubject || undefined, description: chatAdminDescription || undefined });
                    });
                    if (summary.succeededRealmIds.length <= 0) {
                      throw new Error(`Channel upsert failed for all target realms: ${summary.failed.map((entry) => `${entry.realmId} (${entry.reason})`).join("; ")}`);
                    }
                  }, () => formatRealmActionSummary("Channel upsert completed.", summary), () => {
                    setChatAdminSubject("");
                    setChatAdminDescription("");
                  });
                }} disabled={actionBusy}>Upsert</button>
                <button type="button" onClick={() => {
                  let summary: RealmActionSummary | null = null;
                  return runAdminAction(async () => {
                    summary = await runChatActionAcrossRealms(async (realmId) => {
                      await adminChatDisableChannel(chatAdminChannel, { realmId });
                    });
                    if (summary.succeededRealmIds.length <= 0) {
                      throw new Error(`Channel disable failed for all target realms: ${summary.failed.map((entry) => `${entry.realmId} (${entry.reason})`).join("; ")}`);
                    }
                  }, () => formatRealmActionSummary("Channel disable completed.", summary));
                }} disabled={actionBusy}>Disable</button>
              </div>
              <button type="button" onClick={() => {
                let summary: RealmActionSummary | null = null;
                return runAdminAction(async () => {
                  summary = await runChatActionAcrossRealms(async (realmId) => {
                    await adminChatFlushChannel(chatAdminChannel, { realmId, reasonCode: chatAdminReason, note: chatAdminNote || undefined });
                  });
                  if (summary.succeededRealmIds.length <= 0) {
                    throw new Error(`Channel flush failed for all target realms: ${summary.failed.map((entry) => `${entry.realmId} (${entry.reason})`).join("; ")}`);
                  }
                }, () => formatRealmActionSummary("Channel flush completed.", summary), () => {
                  setChatAdminNote("");
                });
              }} disabled={actionBusy}>Flush messages</button>
            </div>

            <div className="column-form">
              <h3>Moderation + system message</h3>
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
                  summary = await runChatActionAcrossRealms(async (realmId) => {
                    await adminChatModerateChannel(chatAdminChannel, {
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
                  summary = await runChatActionAcrossRealms(async (realmId) => {
                    await adminChatSystemMessage(chatAdminChannel, {
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
