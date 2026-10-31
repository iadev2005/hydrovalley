import { useCallback, useEffect, useRef, useState } from "react";
import { AccountMenu } from "../components/AccountMenu.tsx";
import { AdminPanel } from "../components/AdminPanel.tsx";
import { AuthPanel } from "../components/AuthPanel.tsx";
import { ChatPanel } from "../components/ChatPanel.tsx";
import { OfflineSummaryPanel } from "../components/OfflineSummaryPanel.tsx";
import { LogConsole } from "../components/LogConsole.tsx";
import { RankingTable } from "../components/RankingTable.tsx";
import { ReportPanel } from "../components/ReportPanel.tsx";
import { ResourceHud } from "../components/ResourceHud.tsx";
import { ToolPalette } from "../components/ToolPalette.tsx";
import { ValleyCanvas } from "../components/ValleyCanvas.tsx";
import { ValleyPhaser, type ValleyPhaserHandle } from "../components/ValleyPhaser.tsx";
import { useChatSocket, type UseChatSocketOptions } from "../hooks/useChatSocket.ts";
import { useGameSocket, type UseGameSocketOptions } from "../hooks/useGameSocket.ts";
import { useReports, type UseReportsOptions } from "../hooks/useReports.ts";
import { useSession, type UseSessionOptions } from "../hooks/useSession.ts";
import { useValleyView, type UseValleyViewOptions } from "../hooks/useValleyView.ts";
import { describeFrame } from "../logic/play.ts";
import {
  hasWaterForDrop,
  interactionFor,
  isInstallationTool,
  placementFor,
  tileTooltip,
  toolName,
  type ToolId,
} from "../logic/tools.ts";
import { playerPanel } from "../logic/valley.ts";

export interface GameAppProps {
  sessionOptions?: UseSessionOptions;
  socketOptions?: Omit<UseGameSocketOptions, "session" | "onSessionExpired">;
  chatOptions?: Omit<UseChatSocketOptions, "session" | "onSessionExpired">;
  reportsOptions?: Omit<UseReportsOptions, "session" | "onSessionExpired">;
  valleyViewOptions?: UseValleyViewOptions;
}

type DrawerTab = "ranking" | "chat" | "report";

const DRAWER_TABS: { id: DrawerTab; label: string }[] = [
  { id: "ranking", label: "Ranking" },
  { id: "chat", label: "Chat" },
  { id: "report", label: "Mi aporte" },
];

interface Hover {
  x: number;
  y: number;
  clientX: number;
  clientY: number;
}

/** Ancho del tooltip de casilla (styles.css, `.tile-tooltip`). */
const TOOLTIP_WIDTH = 240;

export function GameApp({
  sessionOptions,
  socketOptions,
  chatOptions,
  reportsOptions,
  valleyViewOptions,
}: GameAppProps = {}) {
  const [selected, setSelected] = useState<{ x: number; y: number } | null>(null);
  const [tool, setTool] = useState<ToolId>("interact");
  const [hover, setHover] = useState<Hover | null>(null);
  const [drawer, setDrawer] = useState<DrawerTab | null>("ranking");
  // Aviso local (clic sin acción posible); lo sustituye el resultado de la siguiente acción.
  const [notice, setNotice] = useState<string | null>(null);
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

  // Esc suelta la herramienta, como en un programa de dibujo.
  useEffect(() => {
    const handleKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setTool("interact");
    };
    window.addEventListener("keydown", handleKey);
    return () => window.removeEventListener("keydown", handleKey);
  }, []);

  const handleLogout = useCallback(() => {
    logout();
    setSelected(null);
    phaserRef.current?.clearPendingDrops();
  }, [logout]);

  const { gameState, sendDrop, sendCleanTrash, sendPlaceInstallation, clearActionMessage } = socketHook;

  /** Un clic en una casilla: la única acción posible, o colocar la herramienta elegida. */
  const handleTileClick = useCallback(
    (coord: { x: number; y: number }) => {
      setSelected(coord);
      if (isInstallationTool(tool)) {
        const placement = placementFor(gameState, coord, tool);
        if (!placement.ok) {
          clearActionMessage();
          setNotice(placement.label);
          return;
        }
        setNotice(null);
        sendPlaceInstallation(tool, coord.x, coord.y);
        return;
      }
      const action = interactionFor(gameState, coord);
      if (action.kind === "none") {
        clearActionMessage();
        setNotice(`${action.label}: aquí no hay nada que hacer.`);
        return;
      }
      if (action.kind === "drop" && !hasWaterForDrop(gameState)) {
        clearActionMessage();
        setNotice("Tu depósito está vacío: espera a que se recargue.");
        return;
      }
      setNotice(null);
      if (action.kind === "clean_trash") sendCleanTrash(coord.x, coord.y);
      else sendDrop(coord.x, coord.y);
    },
    [tool, gameState, sendDrop, sendCleanTrash, sendPlaceInstallation, clearActionMessage],
  );

  const handleSelectTool = useCallback((next: ToolId) => {
    setTool((current) => (current === next ? "interact" : next));
  }, []);

  const handleCancelTool = useCallback(() => setTool("interact"), []);

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

  const stateText = gameState ? describeFrame(gameState) : "Sin estado todavía.";
  const panel = playerPanel(gameState?.player ?? null);
  const painting = isInstallationTool(tool);
  const tip = hover ? tileTooltip(gameState, hover, tool) : null;

  const message = notice
    ? { text: notice, ok: false }
    : socketHook.actionMessage;

  const viewToggle = (
    <div className="view-switch-controls">
      <button
        type="button"
        id="btn-toggle-view"
        className="btn-toggle-view"
        onClick={valleyViewHook.toggleView}
        title={
          valleyViewHook.viewMode === "isometric"
            ? "Cambiar a vista cenital (cuadrícula 2D)"
            : "Cambiar a vista isométrica"
        }
      >
        {valleyViewHook.viewMode === "isometric" ? "🗺️ Vista cenital" : "⛰️ Vista isométrica"}
      </button>
      {valleyViewHook.isFallbackActive ? (
        <span
          className="view-emergency-badge"
          role="status"
          title="Phaser no pudo iniciarse; se activó la salida de emergencia"
        >
          {valleyViewHook.viewMode === "isometric"
            ? "(Salida de emergencia)"
            : "(Salida de emergencia activa)"}
        </span>
      ) : null}
    </div>
  );

  return (
    <main className="game-shell">
      <header className="topbar">
        <h1 className="brand-title">hydrovalley</h1>
        {session ? <ResourceHud panel={panel} /> : null}
        <div className="topbar-spacer" />
        <p className="meta connection-status" role="status" aria-live="polite">
          <span id="conn">{socketHook.statusText}</span>
        </p>
        {session ? (
          <>
            <nav className="drawer-buttons" aria-label="Paneles">
              {DRAWER_TABS.map((tab) => (
                <button
                  key={tab.id}
                  type="button"
                  className={drawer === tab.id ? "pill-button pill-button-on" : "pill-button"}
                  aria-pressed={drawer === tab.id}
                  onClick={() => setDrawer((current) => (current === tab.id ? null : tab.id))}
                >
                  {tab.label}
                </button>
              ))}
            </nav>
            <AccountMenu username={session.username} onLogout={handleLogout}>
              {isAdmin ? (
                <AdminPanel
                  isOpen={socketHook.isOpen}
                  demoMode={Boolean(gameState?.demo_mode)}
                  onToggleDemo={handleToggleDemo}
                  onResetValley={handleResetValley}
                />
              ) : null}
              <LogConsole logs={socketHook.logs} />
            </AccountMenu>
          </>
        ) : null}
      </header>

      <AuthPanel
        hidden={Boolean(session)}
        onLogin={login}
        onRegister={register}
        isBusy={isBusy}
        isRetryLocked={isRetryLocked}
        error={error}
        onClearError={clearError}
      />

      <OfflineSummaryPanel
        summary={socketHook.offlineSummary}
        offlineDurationMs={socketHook.offlineDurationMs}
        onDismiss={socketHook.dismissOfflineSummary}
      />

      <div className="game-body">
        <ToolPalette
          tool={tool}
          prices={panel.prices}
          leaves={gameState?.player?.leaves ?? null}
          owned={panel.owned}
          isOpen={socketHook.isOpen}
          onSelectTool={handleSelectTool}
          onBuyUpgrade={handleBuyUpgrade}
        />

        <section id="game" className="valley-stage" aria-labelledby="valley-title">
          <h2 id="valley-title" className="visually-hidden">
            Valle
          </h2>
          <div className={painting ? "mode-bar mode-bar-paint" : "mode-bar"} role="status">
            <strong>{painting ? `Colocando: ${toolName(tool)}` : "Interactuar"}</strong>
            <span>
              {painting
                ? "Clic en varias casillas para colocar más · Esc o clic derecho para terminar."
                : "Clic en una casilla: riega, recoge basura o apaga el fuego según lo que haya."}
            </span>
            {painting ? (
              <button type="button" className="pill-button" onClick={handleCancelTool}>
                Terminar (Esc)
              </button>
            ) : null}
          </div>

          <div className={`layout tool-${painting ? "paint" : "interact"}${tip && !tip.ok ? " tile-blocked" : ""}`}>
            {valleyViewHook.viewMode === "isometric" ? (
              <ValleyPhaser
                ref={phaserRef}
                gameState={gameState}
                selected={selected}
                onSelectTile={handleTileClick}
                onHoverTile={setHover}
                onCancelTool={handleCancelTool}
                placementTool={painting ? tool : null}
                onError={valleyViewHook.reportPhaserError}
                toolbarExtra={viewToggle}
              />
            ) : (
              <ValleyCanvas
                gameState={gameState}
                selected={selected}
                onSelectTile={handleTileClick}
                toolbarExtra={viewToggle}
              />
            )}
          </div>

          {tip && hover ? (
            <div
              className="tile-tooltip"
              role="tooltip"
              style={{
                left:
                  hover.clientX + 16 + TOOLTIP_WIDTH > window.innerWidth
                    ? hover.clientX - 16 - TOOLTIP_WIDTH
                    : hover.clientX + 16,
                top: hover.clientY + 16,
              }}
            >
              <strong>{tip.title}</strong>
              <span className="tile-tooltip-coord">Casilla {tip.coord}</span>
              {tip.humidity !== null ? (
                <span className="tile-tooltip-row">Humedad {tip.humidity}</span>
              ) : null}
              {tip.detail ? <span className="tile-tooltip-row">{tip.detail}</span> : null}
              <span className={tip.ok ? "tile-tooltip-action ok" : "tile-tooltip-action blocked"}>
                {tip.action}
              </span>
            </div>
          ) : null}

          <div className="stage-footer">
            <p
              className={message ? (message.ok ? "action-toast ok" : "action-toast error") : "action-toast"}
              id="action-message"
              role="status"
            >
              {message?.text ?? "Pasa el ratón por una casilla para ver qué hay."}
            </p>
            <p className="meta pending-status">
              Pendientes: <span id="pending">{socketHook.pendingCount}</span>
            </p>
            <p className="meta world-status" id="state">
              {stateText}
            </p>
          </div>
        </section>

        {session && drawer ? (
          <aside className="side-drawer" aria-label="Paneles del jugador">
            <div className="drawer-tabs" role="tablist">
              {DRAWER_TABS.map((tab) => (
                <button
                  key={tab.id}
                  type="button"
                  role="tab"
                  aria-selected={drawer === tab.id}
                  className={drawer === tab.id ? "drawer-tab drawer-tab-on" : "drawer-tab"}
                  onClick={() => setDrawer(tab.id)}
                >
                  {tab.label}
                </button>
              ))}
              <button
                type="button"
                className="drawer-close"
                aria-label="Cerrar panel"
                onClick={() => setDrawer(null)}
              >
                ×
              </button>
            </div>
            <div className="drawer-body">
              <RankingTable
                hidden={drawer !== "ranking"}
                ranking={reportsHook.ranking}
                statusText={reportsHook.rankingStatus}
                onRefresh={reportsHook.refresh}
                isRetryLocked={reportsHook.isRetryLocked}
              />
              <ChatPanel
                hidden={drawer !== "chat"}
                messages={chatHook.messages}
                status={chatHook.status}
                statusText={chatHook.statusText}
                onSendMessage={chatHook.sendMessage}
              />
              <ReportPanel
                hidden={drawer !== "report"}
                report={reportsHook.report}
                reportScope={reportsHook.reportScope}
                statusText={reportsHook.reportStatus}
                onSelectScope={reportsHook.setReportScope}
              />
            </div>
          </aside>
        ) : null}
      </div>
    </main>
  );
}
