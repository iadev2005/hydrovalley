import { useCallback, useEffect, useRef, useState } from "react";
import { AdminPanel } from "../components/AdminPanel.tsx";
import { AuthPanel } from "../components/AuthPanel.tsx";
import { ChatPanel } from "../components/ChatPanel.tsx";
import { LogConsole } from "../components/LogConsole.tsx";
import { RankingTable } from "../components/RankingTable.tsx";
import { ReportPanel } from "../components/ReportPanel.tsx";
import { SessionBar } from "../components/SessionBar.tsx";
import { ValleyCanvas } from "../components/ValleyCanvas.tsx";
import { ValleyPhaser, type ValleyPhaserHandle } from "../components/ValleyPhaser.tsx";
import { ValleyControls } from "../components/ValleyControls.tsx";
import { useChatSocket, type UseChatSocketOptions } from "../hooks/useChatSocket.ts";
import { useGameSocket, type UseGameSocketOptions } from "../hooks/useGameSocket.ts";
import { useReports, type UseReportsOptions } from "../hooks/useReports.ts";
import { useSession, type UseSessionOptions } from "../hooks/useSession.ts";
import { useValleyView, type UseValleyViewOptions } from "../hooks/useValleyView.ts";
import { describeFrame, type OfflineSummary } from "../logic/play.ts";
import type { StorageLike } from "../logic/auth.ts";

const OFFLINE_SUMMARY_STORAGE_PREFIX = "hydrovalley.offline-summary.";

function summaryStorage(storage: StorageLike | undefined): StorageLike | null {
  if (storage) return storage;
  if (typeof window !== "undefined") return window.localStorage;
  return null;
}

function summaryKey(username: string): string {
  return `${OFFLINE_SUMMARY_STORAGE_PREFIX}${username.toLowerCase()}`;
}

function readSummary(storage: StorageLike | null, key: string): OfflineSummary | null {
  if (!storage) return null;
  try {
    const raw = storage.getItem(key);
    if (!raw) return null;
    const value = JSON.parse(raw) as Record<string, unknown>;
    return {
      leaves_earned: BigInt(String(value.leaves_earned ?? "0")),
      water_reused: BigInt(String(value.water_reused ?? "0")),
      fires_extinguished: Number(value.fires_extinguished) || 0,
      tiles_grown: Number(value.tiles_grown) || 0,
    };
  } catch {
    return null;
  }
}

function writeSummary(storage: StorageLike | null, key: string, summary: OfflineSummary): void {
  if (!storage) return;
  try {
    storage.setItem(
      key,
      JSON.stringify({
        leaves_earned: summary.leaves_earned.toString(),
        water_reused: summary.water_reused.toString(),
        fires_extinguished: summary.fires_extinguished,
        tiles_grown: summary.tiles_grown,
      }),
    );
  } catch {
    // El resumen sigue funcionando aunque el navegador no permita storage.
  }
}

function summaryDelta(current: OfflineSummary, previous: OfflineSummary): OfflineSummary {
  return {
    leaves_earned: current.leaves_earned > previous.leaves_earned
      ? current.leaves_earned - previous.leaves_earned
      : 0n,
    water_reused: current.water_reused > previous.water_reused
      ? current.water_reused - previous.water_reused
      : 0n,
    fires_extinguished: Math.max(0, current.fires_extinguished - previous.fires_extinguished),
    tiles_grown: Math.max(0, current.tiles_grown - previous.tiles_grown),
  };
}

function hasSummaryActivity(summary: OfflineSummary): boolean {
  return summary.leaves_earned > 0n || summary.water_reused > 0n || summary.fires_extinguished > 0 || summary.tiles_grown > 0;
}

export interface GameAppProps {
  sessionOptions?: UseSessionOptions;
  socketOptions?: Omit<UseGameSocketOptions, "session" | "onSessionExpired">;
  chatOptions?: Omit<UseChatSocketOptions, "session" | "onSessionExpired">;
  reportsOptions?: Omit<UseReportsOptions, "session" | "onSessionExpired">;
  valleyViewOptions?: UseValleyViewOptions;
}

export function GameApp({
  sessionOptions,
  socketOptions,
  chatOptions,
  reportsOptions,
  valleyViewOptions,
}: GameAppProps = {}) {
  const [selected, setSelected] = useState<{ x: number; y: number } | null>(null);
  const [offlineSummaryDismissed, setOfflineSummaryDismissed] = useState(false);
  const [offlineSummaryToShow, setOfflineSummaryToShow] = useState<OfflineSummary | null>(null);
  const previousSocketStatus = useRef("disconnected");
  const awaitingInitialState = useRef(false);
  const summaryBaseline = useRef<OfflineSummary | null>(null);
  const phaserRef = useRef<ValleyPhaserHandle | null>(null);

  const valleyViewHook = useValleyView({
    storage: sessionOptions?.storage,
    ...valleyViewOptions,
  });

  const sessionHook = useSession(sessionOptions);
  const { session, isAdmin, error, isBusy, isRetryLocked, login, register, logout, clearError, setSessionExpired } =
    sessionHook;

  const handleSessionExpired = useCallback(
    (reason: string) => {
      setSessionExpired(reason);
      setSelected(null);
      phaserRef.current?.clearPendingDrops();
    },
    [setSessionExpired],
  );

  const handleActionSent = useCallback(
    (action: { actionId: string; type: string; x?: number; y?: number }) => {
      if (action.type === "drop" && action.x !== undefined && action.y !== undefined) {
        phaserRef.current?.spawnPendingDrop(action.actionId, action.x, action.y);
      }
    },
    [],
  );

  const handleActionResult = useCallback(
    (result: { actionId: string; status: string }) => {
      phaserRef.current?.resolvePendingDrop(result.actionId, result.status);
    },
    [],
  );

  const handleResetPending = useCallback(() => {
    phaserRef.current?.clearPendingDrops();
  }, []);

  const socketHook = useGameSocket({
    ...socketOptions,
    session,
    onSessionExpired: handleSessionExpired,
    onActionSent: handleActionSent,
    onActionResult: handleActionResult,
    onResetPending: handleResetPending,
  });

  const reportsHook = useReports({
    ...reportsOptions,
    session,
    onSessionExpired: handleSessionExpired,
  });

  const chatHook = useChatSocket({
    ...chatOptions,
    session,
    onSessionExpired: handleSessionExpired,
  });

  const handleLogout = useCallback(() => {
    logout();
    setSelected(null);
    phaserRef.current?.clearPendingDrops();
  }, [logout]);

  const handleDrop = useCallback(() => {
    if (selected) {
      socketHook.sendDrop(selected.x, selected.y);
    }
  }, [selected, socketHook]);

  const handlePlaceInstallation = useCallback(
    (installation: string) => {
      if (selected) {
        socketHook.sendPlaceInstallation(installation, selected.x, selected.y);
      }
    },
    [selected, socketHook],
  );

  const handleBuyUpgrade = useCallback(
    (upgrade: string) => {
      socketHook.sendBuyUpgrade(upgrade);
    },
    [socketHook],
  );

  const handleToggleDemo = useCallback(() => {
    const next = !(socketHook.gameState?.demo_mode ?? false);
    socketHook.sendSetDemoMode(next);
  }, [socketHook]);

  const handleResetValley = useCallback(() => {
    socketHook.sendResetValley();
  }, [socketHook]);

  const stateText = socketHook.gameState
    ? describeFrame(socketHook.gameState)
    : "Sin estado todavía.";
  useEffect(() => {
    const becameConnected =
      socketHook.status === "connected" && previousSocketStatus.current !== "connected";
    if (becameConnected) {
      awaitingInitialState.current = true;
      setOfflineSummaryToShow(null);
      setOfflineSummaryDismissed(false);
      summaryBaseline.current = readSummary(
        summaryStorage(sessionOptions?.storage),
        summaryKey(session?.username ?? ""),
      );
    }
    previousSocketStatus.current = socketHook.status;
  }, [socketHook.status]);

  useEffect(() => {
    if (socketHook.status !== "connected" || !socketHook.gameState) {
      return;
    }
    const summary = socketHook.gameState.player?.offline_summary;
    if (!summary) {
      awaitingInitialState.current = false;
      return;
    }
    if (awaitingInitialState.current) {
      const previous = summaryBaseline.current;
      if (previous) {
        const delta = summaryDelta(summary, previous);
        if (hasSummaryActivity(delta)) setOfflineSummaryToShow(delta);
      }
      awaitingInitialState.current = false;
    }
    writeSummary(
      summaryStorage(sessionOptions?.storage),
      summaryKey(session?.username ?? ""),
      summary,
    );
  }, [session, sessionOptions?.storage, socketHook.gameState, socketHook.status]);

  return (
    <main>
      <h1>hydrovalley</h1>

      <AuthPanel
        hidden={Boolean(session)}
        onLogin={login}
        onRegister={register}
        isBusy={isBusy}
        isRetryLocked={isRetryLocked}
        error={error}
        onClearError={clearError}
      />

      {session ? (
        <SessionBar username={session.username} onLogout={handleLogout} />
      ) : null}

      <p className="meta">
        Conexión: <span id="conn">{socketHook.statusText}</span>
      </p>
      <p className="meta" id="state">
        {stateText}
      </p>

      {offlineSummaryToShow && !offlineSummaryDismissed ? (
        <div className="offline-summary-backdrop">
          <section
            className="offline-summary-modal"
            aria-labelledby="offline-summary-title"
            aria-modal="true"
            role="dialog"
          >
            <p className="offline-summary-kicker">Resumen de regreso</p>
            <h2 id="offline-summary-title">Mientras no estabas</h2>
            <p>
              Tus aspersores aportaron {offlineSummaryToShow.water_reused.toString()} puntos de humedad,
              hicieron crecer {offlineSummaryToShow.tiles_grown} casillas y apagaron {offlineSummaryToShow.fires_extinguished} incendios.
            </p>
            <p>Ganaste {offlineSummaryToShow.leaves_earned.toString()} hojas.</p>
            <button type="button" onClick={() => setOfflineSummaryDismissed(true)}>
              Continuar al valle
            </button>
          </section>
        </div>
      ) : null}

      <section id="game" aria-labelledby="valley-title">
        <h2 id="valley-title">Valle</h2>
        <div className="layout">
          {valleyViewHook.viewMode === "isometric" ? (
            <ValleyPhaser
              ref={phaserRef}
              gameState={socketHook.gameState}
              selected={selected}
              onSelectTile={setSelected}
              onError={valleyViewHook.reportPhaserError}
              toolbarExtra={
                <div className="view-switch-controls">
                  <button
                    type="button"
                    id="btn-toggle-view"
                    className="btn-toggle-view"
                    onClick={valleyViewHook.toggleView}
                    title="Cambiar a vista cenital (cuadrícula 2D)"
                  >
                    🗺️ Vista cenital
                  </button>
                  {valleyViewHook.isFallbackActive ? (
                    <span
                      className="view-emergency-badge"
                      role="status"
                      title="Phaser no pudo iniciarse; se activó la salida de emergencia"
                    >
                      (Salida de emergencia)
                    </span>
                  ) : null}
                </div>
              }
            />
          ) : (
            <ValleyCanvas
              gameState={socketHook.gameState}
              selected={selected}
              onSelectTile={setSelected}
              toolbarExtra={
                <div className="view-switch-controls">
                  <button
                    type="button"
                    id="btn-toggle-view"
                    className="btn-toggle-view"
                    onClick={valleyViewHook.toggleView}
                    title="Cambiar a vista isométrica"
                  >
                    ⛰️ Vista isométrica
                  </button>
                  {valleyViewHook.isFallbackActive ? (
                    <span
                      className="view-emergency-badge"
                      role="status"
                      title="Phaser no pudo iniciarse; se activó la salida de emergencia"
                    >
                      (Salida de emergencia activa)
                    </span>
                  ) : null}
                </div>
              }
            />
          )}
          <ValleyControls
            gameState={socketHook.gameState}
            selected={selected}
            isOpen={socketHook.isOpen}
            pendingCount={socketHook.pendingCount}
            actionMessage={socketHook.actionMessage}
            onDrop={handleDrop}
            onPlaceInstallation={handlePlaceInstallation}
            onBuyUpgrade={handleBuyUpgrade}
          >
            {isAdmin ? (
              <AdminPanel
                isOpen={socketHook.isOpen}
                demoMode={Boolean(socketHook.gameState?.demo_mode)}
                onToggleDemo={handleToggleDemo}
                onResetValley={handleResetValley}
              />
            ) : null}
          </ValleyControls>
        </div>

        <ul className="legend" aria-label="Leyenda">
          <li>
            <span className="swatch" style={{ background: "#8d6e4a" }}></span>
            tierra seca
          </li>
          <li>
            <span className="swatch" style={{ background: "#9ccc65" }}></span>
            pasto
          </li>
          <li>
            <span className="swatch" style={{ background: "#558b2f" }}></span>
            arbusto
          </li>
          <li>
            <span className="swatch" style={{ background: "#f06292" }}></span>
            flores
          </li>
          <li>
            <span className="swatch" style={{ background: "#1b5e20" }}></span>
            árbol
          </li>
          <li>
            <span className="swatch" style={{ background: "#e53935" }}></span>
            fuego
          </li>
          <li>A aspersor · G goteo · P planta</li>
        </ul>
      </section>

      <RankingTable
        hidden={!session}
        ranking={reportsHook.ranking}
        statusText={reportsHook.rankingStatus}
        onRefresh={reportsHook.refresh}
        isRetryLocked={reportsHook.isRetryLocked}
      />

      <ChatPanel
        hidden={!session}
        messages={chatHook.messages}
        status={chatHook.status}
        statusText={chatHook.statusText}
        onSendMessage={chatHook.sendMessage}
      />

      <ReportPanel
        hidden={!session}
        report={reportsHook.report}
        reportScope={reportsHook.reportScope}
        statusText={reportsHook.reportStatus}
        onSelectScope={reportsHook.setReportScope}
      />

      <LogConsole logs={socketHook.logs} />

      <footer className="site-footer">
        <a href="/sistema.html">Panel del sistema</a>
      </footer>
    </main>
  );
}
