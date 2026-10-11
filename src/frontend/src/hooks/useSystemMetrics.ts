import { useCallback, useEffect, useRef, useState } from "react";
import type { FetchFn } from "../logic/auth.ts";
import { browserFetch } from "./browserFetch.ts";
import {
  createPollBackoff,
  loadSystem,
  type SystemView,
} from "../logic/system.ts";

export interface UseSystemMetricsOptions {
  fetchFn?: FetchFn;
}

export interface UseSystemMetricsReturn {
  metrics: SystemView | null;
  error: string | null;
  lastUpdated: string;
  refresh: () => void;
}

export function useSystemMetrics({
  fetchFn = browserFetch,
}: UseSystemMetricsOptions = {}): UseSystemMetricsReturn {
  const [metrics, setMetrics] = useState<SystemView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [lastUpdated, setLastUpdated] = useState<string>("Última actualización: —");

  const backoffRef = useRef(createPollBackoff());
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const runIdRef = useRef<number>(0);
  const pollRef = useRef<() => Promise<void>>(async () => {});

  const poll = useCallback(async () => {
    if (!fetchFn) return;
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }

    const currentRun = (runIdRef.current += 1);
    let wait: number;

    try {
      const result = await loadSystem(fetchFn);
      if (currentRun !== runIdRef.current) return;

      if (result.ok) {
        setMetrics(result.view);
        setError(null);
        const when = new Date(result.view.generatedAt);
        setLastUpdated(
          Number.isNaN(when.getTime())
            ? "Última actualización: —"
            : `Última actualización: ${when.toLocaleTimeString("es-VE")}`,
        );
      } else {
        setError(result.error.message);
      }
      wait = backoffRef.current.next(result);
    } catch (err) {
      console.error("no se pudo actualizar el panel del sistema", err);
      wait = backoffRef.current.next({ ok: false, error: { kind: "offline", waitMs: 0 } });
    }

    if (currentRun !== runIdRef.current) return;
    if (typeof document !== "undefined" && document.hidden) return;

    timerRef.current = setTimeout(() => {
      void pollRef.current();
    }, wait);
  }, [fetchFn]);

  useEffect(() => {
    pollRef.current = poll;
  }, [poll]);

  const refresh = useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    void pollRef.current();
  }, []);

  useEffect(() => {
    void poll();

    const handleVisibilityChange = () => {
      if (typeof document === "undefined") return;
      if (document.hidden) {
        if (timerRef.current !== null) {
          clearTimeout(timerRef.current);
          timerRef.current = null;
        }
        runIdRef.current += 1;
      } else if (timerRef.current === null) {
        void pollRef.current();
      }
    };

    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", handleVisibilityChange);
    }

    return () => {
      if (timerRef.current !== null) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      runIdRef.current += 1;
      if (typeof document !== "undefined") {
        document.removeEventListener("visibilitychange", handleVisibilityChange);
      }
    };
  }, [poll]);

  return {
    metrics,
    error,
    lastUpdated,
    refresh,
  };
}
