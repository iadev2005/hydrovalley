import { useCallback, useRef, useState } from "react";
import { AdminPanel } from "../components/AdminPanel.tsx";
import { AuthPanel } from "../components/AuthPanel.tsx";
import { LogConsole } from "../components/LogConsole.tsx";
import { RankingTable } from "../components/RankingTable.tsx";
import { ReportPanel } from "../components/ReportPanel.tsx";
import { SessionBar } from "../components/SessionBar.tsx";
import { ValleyPhaser, type ValleyPhaserHandle } from "../components/ValleyPhaser.tsx";
import { ValleyControls } from "../components/ValleyControls.tsx";
import { useGameSocket, type UseGameSocketOptions } from "../hooks/useGameSocket.ts";
import { useReports, type UseReportsOptions } from "../hooks/useReports.ts";
import { useSession, type UseSessionOptions } from "../hooks/useSession.ts";
import { describeFrame } from "../logic/play.ts";

export interface GameAppProps {
  sessionOptions?: UseSessionOptions;
  socketOptions?: Omit<UseGameSocketOptions, "session" | "onSessionExpired">;
  reportsOptions?: Omit<UseReportsOptions, "session" | "onSessionExpired">;
}

export function GameApp({
  sessionOptions,
  socketOptions,
  reportsOptions,
}: GameAppProps = {}) {
  const [selected, setSelected] = useState<{ x: number; y: number } | null>(null);
  const phaserRef = useRef<ValleyPhaserHandle | null>(null);

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

      <section id="game" aria-labelledby="valley-title">
        <h2 id="valley-title">Valle</h2>
        <div className="layout">
          <ValleyPhaser
            ref={phaserRef}
            gameState={socketHook.gameState}
            selected={selected}
            onSelectTile={setSelected}
          />
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
