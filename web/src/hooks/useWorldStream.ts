import { useEffect, useMemo, useRef, useState } from "react";
import { getSession, subscribeSessionChanges } from "../api";

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
  eventId?: string;
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

type StreamStatus = "connecting" | "live" | "fallback" | "offline";

const localHostNames = new Set(["localhost", "127.0.0.1", "::1"]);

function normalizeOrigin(raw: string, wsProtocol: "ws" | "wss"): string | null {
  const trimmed = raw.trim();
  if (!trimmed) {
    return null;
  }

  try {
    if (trimmed.startsWith("ws://") || trimmed.startsWith("wss://")) {
      const parsed = new URL(trimmed);
      return `${parsed.protocol}//${parsed.host}`;
    }
    if (trimmed.startsWith("http://") || trimmed.startsWith("https://")) {
      const parsed = new URL(trimmed);
      const protocol = parsed.protocol === "https:" ? "wss:" : "ws:";
      return `${protocol}//${parsed.host}`;
    }

    const parsed = new URL(`${wsProtocol}://${trimmed}`);
    return `${parsed.protocol}//${parsed.host}`;
  } catch {
    return null;
  }
}

function streamOriginCandidates(): string[] {
  const wsProtocol: "ws" | "wss" = window.location.protocol === "https:" ? "wss" : "ws";
  const candidates: string[] = [];
  const seen = new Set<string>();

  const pushCandidate = (raw: string | undefined | null) => {
    if (!raw) {
      return;
    }
    const normalized = normalizeOrigin(raw, wsProtocol);
    if (!normalized) {
      return;
    }
    if (seen.has(normalized)) {
      return;
    }
    seen.add(normalized);
    candidates.push(normalized);
  };

  const envOrigin = (import.meta as ImportMeta & { env?: Record<string, string | undefined> }).env?.VITE_STREAM_ORIGIN;
  pushCandidate(envOrigin);
  pushCandidate(`${wsProtocol}://${window.location.host}`);

  const currentPort = window.location.port;
  if (localHostNames.has(window.location.hostname) && currentPort !== "8080") {
    pushCandidate(`${wsProtocol}://${window.location.hostname}:8080`);
  }

  return candidates;
}

function sanitizeStreamURL(rawURL: string): string {
  try {
    const parsed = new URL(rawURL);
    return `${parsed.origin}${parsed.pathname}`;
  } catch {
    return "/v1/stream/world";
  }
}

function toClock(minuteOfDay: number): string {
  const normalized = ((minuteOfDay % (24 * 60)) + (24 * 60)) % (24 * 60);
  const hour = Math.floor(normalized / 60);
  const minute = normalized % 60;
  return `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`;
}

export function useWorldStream(characterId?: number, enabled: boolean = true) {
  const [event, setEvent] = useState<WorldStreamEvent | null>(null);
  const [status, setStatus] = useState<StreamStatus>("offline");
  const [lastError, setLastError] = useState<string | null>(null);
  const [attempts, setAttempts] = useState(0);
  const [sessionRevision, setSessionRevision] = useState(0);

  const reconnectTimerRef = useRef<number | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const attemptsRef = useRef(0);
  const gotAnyMessageRef = useRef(false);
  const endpointCandidatesRef = useRef<string[]>([]);
  const endpointIndexRef = useRef(0);
  const lastEventIdRef = useRef<string | null>(null);

  const fallback = useMemo(() => {
    return {
      tick: event?.tick,
      day: event?.day,
      clock: event?.clock ?? (event ? toClock(event.minuteOfDay) : undefined),
      dayPart: event?.dayPart,
      marketState: event?.marketState
    };
  }, [event]);

  useEffect(() => {
    return subscribeSessionChanges(() => {
      setSessionRevision((value) => value + 1);
    });
  }, []);

  useEffect(() => {
    if (!enabled) {
      setEvent(null);
      setStatus("offline");
      setLastError(null);
      setAttempts(0);
      return;
    }

    let shouldReconnect = true;
    gotAnyMessageRef.current = false;
    attemptsRef.current = 0;
    endpointIndexRef.current = 0;
    setEvent(null);
    setLastError(null);
    setAttempts(0);
    lastEventIdRef.current = null;

    endpointCandidatesRef.current = streamOriginCandidates();

    const connect = () => {
      const params = new URLSearchParams();
      if (characterId) {
        params.set("characterId", String(characterId));
      }

      const session = getSession();
      if (lastEventIdRef.current) {
        params.set("lastEventId", lastEventIdRef.current);
      }

      const origin = endpointCandidatesRef.current[endpointIndexRef.current];
      if (!origin) {
        setStatus("fallback");
        setLastError("stream_unavailable:no_endpoint_candidates");
        return;
      }

      setStatus("connecting");
      const url = `${origin}/v1/stream/world${params.toString() ? `?${params.toString()}` : ""}`;
      const safeURL = sanitizeStreamURL(url);
      const protocols = ["lived.v1"];
      if (session?.accessToken) {
        protocols.push(`bearer.${session.accessToken}`);
      }
      const socket = new WebSocket(url, protocols);
      wsRef.current = socket;

      socket.onopen = () => {
        attemptsRef.current = 0;
        setAttempts(0);
        setLastError(null);
      };

      socket.onmessage = (raw) => {
        try {
          const parsed = JSON.parse(raw.data) as WorldStreamEvent;
          if (parsed.type !== "world_snapshot") {
            return;
          }
          gotAnyMessageRef.current = true;
          if (parsed.eventId) {
            lastEventIdRef.current = parsed.eventId;
          }
          setEvent(parsed);
          setStatus("live");
          setLastError(null);
        } catch {
          // keep stream alive
        }
      };

      socket.onerror = () => {
        setLastError(`stream_error:${safeURL}`);
      };

      socket.onclose = (closeEvent) => {
        wsRef.current = null;
        if (!shouldReconnect) {
          return;
        }
        setStatus("fallback");

        attemptsRef.current += 1;
        setAttempts(attemptsRef.current);

        const closeReason = closeEvent.reason?.trim() || "no_reason";
        const closeMeta = `code=${closeEvent.code};reason=${closeReason};clean=${closeEvent.wasClean}`;

        if (!gotAnyMessageRef.current && attemptsRef.current >= 2 && endpointIndexRef.current+1 < endpointCandidatesRef.current.length) {
          endpointIndexRef.current += 1;
          const nextOrigin = endpointCandidatesRef.current[endpointIndexRef.current];
          setLastError(`stream_endpoint_unreachable:${safeURL};${closeMeta};switching_to=${nextOrigin}/v1/stream/world`);
          reconnectTimerRef.current = window.setTimeout(connect, 300);
          return;
        }

        if (!gotAnyMessageRef.current && attemptsRef.current >= 3) {
          setLastError(`stream_unavailable:${safeURL};${closeMeta}`);
          return;
        }
        const delay = Math.min(20_000, 1_000 * attemptsRef.current);
        reconnectTimerRef.current = window.setTimeout(connect, delay);
      };
    };

    connect();

    return () => {
      shouldReconnect = false;
      if (reconnectTimerRef.current !== null) {
        window.clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      const socket = wsRef.current;
      wsRef.current = null;
      socket?.close();
    };
  }, [characterId, enabled, sessionRevision]);

  return {
    event,
    status,
    lastError,
    attempts,
    fallback
  };
}
