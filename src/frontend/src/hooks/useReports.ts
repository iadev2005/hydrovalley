import { useCallback, useEffect, useRef, useState } from "react";
import type { FetchFn, Session } from "../logic/auth.ts";
import { browserFetch } from "./browserFetch.ts";
import {
  loadRanking,
  loadReport,
  REFRESH_MS,
  type RankingView,
  type ReportScope,
  type ReportView,
} from "../logic/reports.ts";

export interface UseReportsOptions {
  session: Session | null;
  onSessionExpired?: (reason: string) => void;
  fetchFn?: FetchFn;
}

export interface UseReportsReturn {
  ranking: RankingView | null;
  rankingStatus: string;
  report: ReportView | null;
  reportStatus: string;
  reportScope: ReportScope;
  setReportScope: (scope: ReportScope) => void;
  refresh: () => void;
  isLoading: boolean;
  isRetryLocked: boolean;
}

export function useReports({
  session,
  onSessionExpired,
  fetchFn = browserFetch,
}: UseReportsOptions): UseReportsReturn {
  const [ranking, setRanking] = useState<RankingView | null>(null);
  const [rankingStatus, setRankingStatus] = useState<string>("");
  const [report, setReport] = useState<ReportView | null>(null);
  const [reportStatus, setReportStatus] = useState<string>("");
  const [reportScope, setReportScopeState] = useState<ReportScope>("me");
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [isRetryLocked, setIsRetryLocked] = useState<boolean>(false);

  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const retryLockTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const runIdRef = useRef<number>(0);
  const performFetchRef = useRef<(scope: ReportScope) => Promise<void>>(async () => {});

  const performFetch = useCallback(
    async (scope: ReportScope) => {
      if (!session || !fetchFn) return;
      const currentRun = (runIdRef.current += 1);
      setIsLoading(true);

      let wait = 0;
      try {
        const [rankingRes, reportRes] = await Promise.all([
          loadRanking(fetchFn, session.token, session.username),
          loadReport(fetchFn, session.token, scope),
        ]);

        if (currentRun !== runIdRef.current) return;

        const sessionError = [rankingRes, reportRes].find(
          (r) => !r.ok && r.error.kind === "session",
        );
        if (sessionError && !sessionError.ok) {
          onSessionExpired?.(sessionError.error.message);
          return;
        }

        if (rankingRes.ok) {
          setRanking(rankingRes.view);
          setRankingStatus(`Tick ${rankingRes.view.tick}`);
        } else {
          setRankingStatus(rankingRes.error.message);
        }

        if (reportRes.ok) {
          setReport(reportRes.view);
          setReportStatus("");
        } else {
          setReport(null);
          setReportStatus(reportRes.error.message);
        }

        wait = Math.max(
          0,
          rankingRes.ok ? 0 : rankingRes.error.waitMs,
          reportRes.ok ? 0 : reportRes.error.waitMs,
        );

        if (wait > 0) {
          setIsRetryLocked(true);
          if (retryLockTimerRef.current !== null) clearTimeout(retryLockTimerRef.current);
          retryLockTimerRef.current = setTimeout(() => {
            setIsRetryLocked(false);
            retryLockTimerRef.current = null;
          }, wait);
        }
      } catch (err) {
        console.error("error al actualizar ranking y reporte", err);
      } finally {
        if (currentRun === runIdRef.current) {
          setIsLoading(false);
        }
      }

      if (currentRun !== runIdRef.current) return;
      if (typeof document !== "undefined" && document.hidden) return;

      const nextDelay = Math.max(REFRESH_MS, wait);
      if (timerRef.current !== null) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => {
        void performFetchRef.current(scope);
      }, nextDelay);
    },
    [session, fetchFn, onSessionExpired],
  );

  useEffect(() => {
    performFetchRef.current = performFetch;
  }, [performFetch]);

  const refresh = useCallback(() => {
    if (isRetryLocked) return;
    if (timerRef.current !== null) clearTimeout(timerRef.current);
    void performFetchRef.current(reportScope);
  }, [reportScope, isRetryLocked]);

  const setReportScope = useCallback(
    (newScope: ReportScope) => {
      if (newScope === reportScope) return;
      setReportScopeState(newScope);
      if (timerRef.current !== null) clearTimeout(timerRef.current);
      void performFetchRef.current(newScope);
    },
    [reportScope],
  );

  useEffect(() => {
    if (!session) return;

    void performFetch(reportScope);

    const handleVisibilityChange = () => {
      if (typeof document === "undefined") return;
      if (document.hidden) {
        if (timerRef.current !== null) {
          clearTimeout(timerRef.current);
          timerRef.current = null;
        }
        runIdRef.current += 1;
      } else if (timerRef.current === null) {
        void performFetch(reportScope);
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
      if (retryLockTimerRef.current !== null) {
        clearTimeout(retryLockTimerRef.current);
        retryLockTimerRef.current = null;
      }
      runIdRef.current += 1;
      if (typeof document !== "undefined") {
        document.removeEventListener("visibilitychange", handleVisibilityChange);
      }
    };
  }, [session, reportScope, performFetch]);

  const effectiveRanking = session ? ranking : null;
  const effectiveRankingStatus = session ? rankingStatus : "";
  const effectiveReport = session ? report : null;
  const effectiveReportStatus = session ? reportStatus : "";

  return {
    ranking: effectiveRanking,
    rankingStatus: effectiveRankingStatus,
    report: effectiveReport,
    reportStatus: effectiveReportStatus,
    reportScope,
    setReportScope,
    refresh,
    isLoading,
    isRetryLocked,
  };
}
