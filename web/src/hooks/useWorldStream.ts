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
  const [sessionRevision, setSessionRevision] = useState(0);

  const reconnectTimerRef = useRef<number | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const attemptsRef = useRef(0);
  const gotAnyMessageRef = useRef(false);

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
      return;
    }

    let shouldReconnect = true;
    gotAnyMessageRef.current = false;
    attemptsRef.current = 0;
    setEvent(null);
    setLastError(null);

    const protocol = window.location.protocol === "https:" ? "wss" : "ws";
    const params = new URLSearchParams();
    if (characterId) {
      params.set("characterId", String(characterId));
    }

    const session = getSession();
    if (session?.accessToken) {
      params.set("accessToken", session.accessToken);
    }

    const connect = () => {
      setStatus("connecting");
      const url = `${protocol}://${window.location.host}/v1/stream/world${params.toString() ? `?${params.toString()}` : ""}`;
      const socket = new WebSocket(url);
      wsRef.current = socket;

      socket.onopen = () => {
        attemptsRef.current = 0;
      };

      socket.onmessage = (raw) => {
        try {
          const parsed = JSON.parse(raw.data) as WorldStreamEvent;
          if (parsed.type !== "world_snapshot") {
            return;
          }
          gotAnyMessageRef.current = true;
          setEvent(parsed);
          setStatus("live");
          setLastError(null);
        } catch {
          // keep stream alive
        }
      };

      socket.onerror = () => {
        setLastError("stream_error");
      };

      socket.onclose = () => {
        wsRef.current = null;
        if (!shouldReconnect) {
          return;
        }
        setStatus("fallback");

        attemptsRef.current += 1;
        if (!gotAnyMessageRef.current && attemptsRef.current >= 3) {
          setLastError("stream_unavailable");
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
    fallback
  };
}
