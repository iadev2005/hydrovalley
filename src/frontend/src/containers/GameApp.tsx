import { useCallback, useEffect, useRef, useState } from "react";
import { AdminPanel } from "../components/AdminPanel.tsx";
import { AuthPanel } from "../components/AuthPanel.tsx";
import { ChatPanel } from "../components/ChatPanel.tsx";
import { OfflineSummaryPanel } from "../components/OfflineSummaryPanel.tsx";
import { LogConsole } from "../components/LogConsole.tsx";
import { RankingTable } from "../components/RankingTable.tsx";
import { ReportPanel } from "../components/ReportPanel.tsx";
import { SessionBar } from "../components/SessionBar.tsx";
import { TacticalToasts } from "../components/TacticalToasts.tsx";
import { ValleyTelemetryHUD } from "../components/ValleyTelemetryHUD.tsx";
import { ValleyCanvas } from "../components/ValleyCanvas.tsx";
import { ValleyPhaser, type ValleyPhaserHandle } from "../components/ValleyPhaser.tsx";
import { ValleyControls } from "../components/ValleyControls.tsx";
import { useChatSocket, type UseChatSocketOptions } from "../hooks/useChatSocket.ts";
import { useGameSocket, type UseGameSocketOptions } from "../hooks/useGameSocket.ts";
import { useKeyboardShortcuts } from "../hooks/useKeyboardShortcuts.ts";
import { useReports, type UseReportsOptions } from "../hooks/useReports.ts";
import { useSession, type UseSessionOptions } from "../hooks/useSession.ts";
import { useSoundEffects, type UseSoundEffectsOptions } from "../hooks/useSoundEffects.ts";
import { useTacticalAlerts } from "../hooks/useTacticalAlerts.ts";
import { useValleyView, type UseValleyViewOptions } from "../hooks/useValleyView.ts";
import { describeFrame } from "../logic/play.ts";

export interface GameAppProps {
  sessionOptions?: UseSessionOptions;
  socketOptions?: Omit<UseGameSocketOptions, "session" | "onSessionExpired">;
  chatOptions?: Omit<UseChatSocketOptions, "session" | "onSessionExpired">;
  reportsOptions?: Omit<UseReportsOptions, "session" | "onSessionExpired">;
  valleyViewOptions?: UseValleyViewOptions;
  soundOptions?: UseSoundEffectsOptions;
}

export type CockpitTab = "chat" | "ranking" | "reports" | "logs" | "admin";

export function GameApp({
  sessionOptions,
  socketOptions,
  chatOptions,
  reportsOptions,
  valleyViewOptions,
  soundOptions,
}: GameAppProps = {}) {
  const [selected, setSelected] = useState<{ x: number; y: number } | null>(null);
  const [activeTab, setActiveTab] = useState<CockpitTab>("chat");
  const [dockOpen, setDockOpen] = useState(false);
  const [unreadChat, setUnreadChat] = useState(0);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const phaserRef = useRef<ValleyPhaserHandle | null>(null);
  const prevMessagesCountRef = useRef(0);

  const soundHook = useSoundEffects({
    storage: sessionOptions?.storage,
    ...soundOptions,
  });

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
      if (result.status === "extinguished" || result.status === "ok") {
        soundHook.playExtinguish();
      }
    },
    [soundHook],
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

  // Notificación de mensajes no leídos en chat si el dock está cerrado o en otra pestaña
  useEffect(() => {
    const currentCount = chatHook.messages.length;
    if (currentCount > prevMessagesCountRef.current) {
      if (activeTab !== "chat" || !dockOpen) {
        setUnreadChat((prev) => prev + (currentCount - prevMessagesCountRef.current));
      }
    }
    prevMessagesCountRef.current = currentCount;
  }, [chatHook.messages.length, activeTab, dockOpen]);

  // Manejo de estado de pantalla completa
  useEffect(() => {
    if (typeof document === "undefined") return;
    const handleFullscreenChange = () => {
      setIsFullscreen(Boolean(document.fullscreenElement));
    };
    document.addEventListener("fullscreenchange", handleFullscreenChange);
    return () => {
      document.removeEventListener("fullscreenchange", handleFullscreenChange);
    };
  }, []);

  const handleToggleFullscreen = useCallback(() => {
    soundHook.playClick();
    if (typeof document === "undefined") return;
    if (!document.fullscreenElement) {
      document.documentElement.requestFullscreen().catch(() => {});
    } else {
      document.exitFullscreen().catch(() => {});
    }
  }, [soundHook]);

  const handleSelectTab = useCallback(
    (tab: CockpitTab) => {
      soundHook.playClick();
      setActiveTab(tab);
      setDockOpen(true);
      if (tab === "chat") {
        setUnreadChat(0);
      }
    },
    [soundHook],
  );

  const handleToggleDock = useCallback(() => {
    soundHook.playDockToggle();
    setDockOpen((prev) => {
      const next = !prev;
      if (next && activeTab === "chat") {
        setUnreadChat(0);
      }
      return next;
    });
  }, [activeTab, soundHook]);

  const handleLogout = useCallback(() => {
    logout();
    setSelected(null);
    phaserRef.current?.clearPendingDrops();
  }, [logout]);

  const handleDrop = useCallback(() => {
    if (selected) {
      soundHook.playWaterDrop();
      socketHook.sendDrop(selected.x, selected.y);
    }
  }, [selected, socketHook, soundHook]);

  const handlePlaceInstallation = useCallback(
    (installation: string) => {
      if (selected) {
        soundHook.playInstall();
        socketHook.sendPlaceInstallation(installation, selected.x, selected.y);
      }
    },
    [selected, socketHook, soundHook],
  );

  const handleBuyUpgrade = useCallback(
    (upgrade: string) => {
      soundHook.playInstall();
      socketHook.sendBuyUpgrade(upgrade);
    },
    [socketHook, soundHook],
  );

  const handleToggleDemo = useCallback(() => {
    const next = !(socketHook.gameState?.demo_mode ?? false);
    socketHook.sendSetDemoMode(next);
  }, [socketHook]);

  const handleResetValley = useCallback(() => {
    socketHook.sendResetValley();
  }, [socketHook]);

  const handleFocusFire = useCallback(() => {
    soundHook.playFireAlert();
    const fires = socketHook.gameState?.fires;
    if (!fires || fires.length === 0) return;

    let nextIndex = 0;
    if (selected) {
      const currentIndex = fires.findIndex(
        (f) => f.x === selected.x && f.y === selected.y,
      );
      if (currentIndex !== -1) {
        nextIndex = (currentIndex + 1) % fires.length;
      }
    }

    const targetFire = fires[nextIndex];
    if (targetFire) {
      setSelected({ x: targetFire.x, y: targetFire.y });
      phaserRef.current?.centerOnTile(targetFire.x, targetFire.y);
    }
  }, [selected, socketHook.gameState?.fires, soundHook]);

  const handleCenterSelected = useCallback(() => {
    if (selected) {
      phaserRef.current?.centerOnTile(selected.x, selected.y);
    }
  }, [selected]);

  const handleResetCamera = useCallback(() => {
    phaserRef.current?.resetCamera();
  }, []);

  useKeyboardShortcuts({
    enabled: true,
    selected,
    onSelectTile: setSelected,
    onDrop: handleDrop,
    onPlaceInstallation: handlePlaceInstallation,
    onToggleView: valleyViewHook.toggleView,
    onFocusFire: handleFocusFire,
    onCenterSelected: handleCenterSelected,
    onResetCamera: handleResetCamera,
    canActOnTile: socketHook.isOpen && selected !== null,
  });

  const tacticalAlertsHook = useTacticalAlerts({
    gameState: socketHook.gameState,
    onFocusFire: handleFocusFire,
  });

  const stateText = socketHook.gameState
    ? describeFrame(socketHook.gameState)
    : "Sin estado todavía.";

  return (
    <main className={`cockpit-app ${session ? "session-active" : ""}`}>
      <header className="cockpit-header">
        <div className="cockpit-header-left">
          <h1 className="brand-title">hydrovalley</h1>
          {session ? (
            <SessionBar username={session.username} onLogout={handleLogout} />
          ) : null}
        </div>

        <div className="status-strip" aria-label="Estado de la partida" role="status" aria-live="polite">
          <p className="meta connection-status">
            Conexión: <span id="conn">{socketHook.statusText}</span>
          </p>
          <p className="meta world-status" id="state">
            {stateText}
          </p>
        </div>

        <div className="cockpit-header-right">
          <button
            type="button"
            id="btn-toggle-sound"
            className="btn-header-action"
            onClick={() => {
              const muted = soundHook.toggleMute();
              if (!muted) soundHook.playClick();
            }}
            title={soundHook.isMuted ? "Activar efectos de sonido" : "Silenciar efectos de sonido"}
            aria-label={soundHook.isMuted ? "Activar efectos de sonido" : "Silenciar efectos de sonido"}
          >
            {soundHook.isMuted ? "🔇 Silencio" : "🔊 Sonido"}
          </button>
          <button
            type="button"
            id="btn-fullscreen"
            className="btn-header-action"
            onClick={handleToggleFullscreen}
            title={isFullscreen ? "Salir de pantalla completa" : "Pantalla completa"}
          >
            {isFullscreen ? "✕ Salir" : "⛶ Pantalla completa"}
          </button>
          <a href="/sistema.html" className="btn-header-action btn-system-link" title="Ir al panel del sistema">
            ⚙️ Sistema
          </a>
        </div>
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

      <div
        className={`cockpit-workspace ${session ? "cockpit-active" : ""} ${
          dockOpen ? "dock-expanded" : "dock-collapsed"
        }`}
      >
        <div className="cockpit-viewport">
          <section
            id="game"
            className="section-shell valley-section"
            aria-labelledby="valley-title"
          >
            <h2 id="valley-title">Valle</h2>
            <ValleyTelemetryHUD
              gameState={socketHook.gameState}
              onFocusFire={handleFocusFire}
            />
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
                onFocusFire={handleFocusFire}
              />
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

            <div className="shortcuts-bar" aria-label="Atajos de teclado">
              <span className="shortcuts-title">⌨️ Atajos:</span>
              <span className="shortcut-item"><kbd>WASD</kbd> / <kbd>↑↓←→</kbd> Mover casilla</span>
              <span className="shortcut-item"><kbd>Espacio</kbd> Soltar gota</span>
              <span className="shortcut-item"><kbd>1</kbd> <kbd>2</kbd> <kbd>3</kbd> Instalar 3R</span>
              <span className="shortcut-item"><kbd>F</kbd> Enfocar fuego</span>
              <span className="shortcut-item"><kbd>C</kbd> Centrar cámara</span>
              <span className="shortcut-item"><kbd>V</kbd> Vista</span>
              <span className="shortcut-item"><kbd>R</kbd> Reset cámara</span>
              <span className="shortcut-item"><kbd>Esc</kbd> Deseleccionar</span>
            </div>
          </section>
        </div>

        <aside
          id="cockpit-dock"
          className="cockpit-dock"
          aria-label="Panel lateral interactivo"
          hidden={!session}
        >
          <div className="dock-header">
            <nav className="dock-tabs" aria-label="Pestañas de interacción">
              <button
                type="button"
                id="tab-btn-chat"
                className={`dock-tab ${activeTab === "chat" ? "active" : ""}`}
                onClick={() => handleSelectTab("chat")}
                title="Chat del Valle"
              >
                <span className="dock-tab-icon" aria-hidden="true">💬</span>
                <span className="dock-tab-label">Chat</span>
                {unreadChat > 0 ? (
                  <span className="dock-tab-badge" aria-label={`${unreadChat} mensajes nuevos`}>
                    {unreadChat}
                  </span>
                ) : null}
              </button>
              <button
                type="button"
                id="tab-btn-ranking"
                className={`dock-tab ${activeTab === "ranking" ? "active" : ""}`}
                onClick={() => handleSelectTab("ranking")}
                title="Ranking de jugadores"
              >
                <span className="dock-tab-icon" aria-hidden="true">🏆</span>
                <span className="dock-tab-label">Ranking</span>
              </button>
              <button
                type="button"
                id="tab-btn-reports"
                className={`dock-tab ${activeTab === "reports" ? "active" : ""}`}
                onClick={() => handleSelectTab("reports")}
                title="Reportes de sostenibilidad 3R"
              >
                <span className="dock-tab-icon" aria-hidden="true">📊</span>
                <span className="dock-tab-label">Reportes</span>
              </button>
              <button
                type="button"
                id="tab-btn-logs"
                className={`dock-tab ${activeTab === "logs" ? "active" : ""}`}
                onClick={() => handleSelectTab("logs")}
                title="Consola de eventos"
              >
                <span className="dock-tab-icon" aria-hidden="true">📜</span>
                <span className="dock-tab-label">Logs</span>
              </button>
              {isAdmin ? (
                <button
                  type="button"
                  id="tab-btn-admin"
                  className={`dock-tab ${activeTab === "admin" ? "active" : ""}`}
                  onClick={() => handleSelectTab("admin")}
                  title="Panel de administración"
                >
                  <span className="dock-tab-icon" aria-hidden="true">⚙️</span>
                  <span className="dock-tab-label">Admin</span>
                </button>
              ) : null}
            </nav>

            <button
              type="button"
              id="btn-toggle-dock"
              className="btn-toggle-dock"
              onClick={handleToggleDock}
              title={dockOpen ? "Colapsar panel lateral" : "Expandir panel lateral"}
              aria-label={dockOpen ? "Colapsar panel lateral" : "Expandir panel lateral"}
            >
              {dockOpen ? "▶" : "◀"}
            </button>
          </div>

          <div className="dock-content">
            <ChatPanel
              hidden={!session || activeTab !== "chat"}
              messages={chatHook.messages}
              status={chatHook.status}
              statusText={chatHook.statusText}
              onSendMessage={chatHook.sendMessage}
            />

            <RankingTable
              hidden={!session || activeTab !== "ranking"}
              ranking={reportsHook.ranking}
              statusText={reportsHook.rankingStatus}
              onRefresh={reportsHook.refresh}
              isRetryLocked={reportsHook.isRetryLocked}
            />

            <ReportPanel
              hidden={!session || activeTab !== "reports"}
              report={reportsHook.report}
              reportScope={reportsHook.reportScope}
              statusText={reportsHook.reportStatus}
              onSelectScope={reportsHook.setReportScope}
            />

            <LogConsole
              hidden={!session || activeTab !== "logs"}
              logs={socketHook.logs}
            />

            {isAdmin ? (
              <AdminPanel
                hidden={!session || activeTab !== "admin"}
                isOpen={socketHook.isOpen}
                demoMode={Boolean(socketHook.gameState?.demo_mode)}
                onToggleDemo={handleToggleDemo}
                onResetValley={handleResetValley}
              />
            ) : null}
          </div>
        </aside>
      </div>

      <TacticalToasts
        alerts={tacticalAlertsHook.alerts}
        onDismiss={tacticalAlertsHook.dismissAlert}
      />

      <footer className="site-footer">
        <a href="/sistema.html">Panel del sistema</a>
      </footer>
    </main>
  );
}
