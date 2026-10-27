import { useCallback, useEffect, useRef, useState } from "react";
import type { Session } from "../logic/auth.ts";
import {
  buildBuyUpgradeFrame,
  buildDropFrame,
  buildPlaceInstallationFrame,
  buildPlayUrl,
  buildResetValleyFrame,
  buildSetDemoModeFrame,
  createEndpointRotator,
  createReconnectBackoff,
  decideOnClose,
  describeFrame,
  newUuid,
  parseFrame,
  STATE_SILENCE_MS,
  type EndpointRotator,
  type PlayFrame,
  type OfflineSummary,
  type StateFrame,
} from "../logic/play.ts";
import {
  createPendingActions,
  describeActionResult,
  type PendingActionsManager,
} from "../logic/valley.ts";
import { sharedEndpointRotator } from "./browserFetch.ts";

export type ConnectionStatus = "disconnected" | "connecting" | "connected";

export interface ActionMessage {
  text: string;
  ok: boolean;
}

export interface UseGameSocketOptions {
  session: Session | null;
  onSessionExpired?: (reason: string) => void;
  onActionSent?: (action: { actionId: string; type: string; x?: number; y?: number }) => void;
  onActionResult?: (result: { actionId: string; status: string }) => void;
  onResetPending?: () => void;
  random?: () => number;
  now?: () => number;
  location?: { protocol: string; host: string };
  backupHosts?: readonly string[];
  rotator?: EndpointRotator;
  silenceTimeoutMs?: number;
}

export interface UseGameSocketReturn {
  status: ConnectionStatus;
  statusText: string;
  gameState: StateFrame | null;
  /** Resumen al volver (RF-25) todavía sin cerrar, o null. */
  offlineSummary: OfflineSummary | null;
  /** Tiempo que el WebSocket estuvo sin conexión (no la ausencia del jugador), en ms. Se borra al cerrar el resumen. */
  offlineDurationMs: number | null;
  actionMessage: ActionMessage | null;
  pendingCount: number;
  logs: string[];
  isOpen: boolean;
  sendAction: (label: string, build: (actionId: string) => string) => boolean;
  sendDrop: (x: number, y: number) => boolean;
  sendPlaceInstallation: (installation: string, x: number, y: number) => boolean;
  sendBuyUpgrade: (upgrade: string) => boolean;
  sendSetDemoMode: (enabled: boolean) => boolean;
  sendResetValley: () => boolean;
  clearActionMessage: () => void;
  dismissOfflineSummary: () => void;
}

const MAX_LOG_LINES = 100;

export function useGameSocket({
  session,
  onSessionExpired,
  onActionSent,
  onActionResult,
  onResetPending,
  random = Math.random,
  now = Date.now,
  location = typeof window !== "undefined" ? window.location : { protocol: "http:", host: "localhost:8080" },
  backupHosts,
  rotator,
  silenceTimeoutMs = STATE_SILENCE_MS,
}: UseGameSocketOptions): UseGameSocketReturn {
  const [status, setStatus] = useState<ConnectionStatus>("disconnected");
  const [statusText, setStatusText] = useState<string>("desconectado");
  const [gameState, setGameState] = useState<StateFrame | null>(null);
  const [offlineSummary, setOfflineSummary] = useState<OfflineSummary | null>(null);
  const [offlineDurationMs, setOfflineDurationMs] = useState<number | null>(null);
  const [actionMessage, setActionMessage] = useState<ActionMessage | null>(null);
  const [pendingCount, setPendingCount] = useState<number>(0);
  const [logs, setLogs] = useState<string[]>([]);

  const socketRef = useRef<WebSocket | null>(null);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const silenceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const disconnectedAtRef = useRef<number | null>(null);
  const pendingRef = useRef<PendingActionsManager>(createPendingActions({ now }));
  const backoffRef = useRef(createReconnectBackoff({ random, now }));
  const backupHostsKey = backupHosts ? backupHosts.join(",") : "";
  const rotatorRef = useRef<EndpointRotator>(
    rotator ??
      (backupHosts
        ? createEndpointRotator(location.host, backupHosts)
        : sharedEndpointRotator),
  );

  const addLog = useCallback((line: string) => {
    const time = new Date().toISOString().slice(11, 19);
    setLogs((prev) => [`${time} ${line}`, ...prev.slice(0, MAX_LOG_LINES - 1)]);
  }, []);

  const clearActionMessage = useCallback(() => setActionMessage(null), []);
  const dismissOfflineSummary = useCallback(() => {
    setOfflineSummary(null);
    setOfflineDurationMs(null);
  }, []);

  const sendAction = useCallback(
    (label: string, build: (actionId: string) => string): boolean => {
      const ws = socketRef.current;
      if (!ws || ws.readyState !== WebSocket.OPEN) return false;
      const actionId = newUuid();
      let frame: string;
      try {
        frame = build(actionId);
      } catch (err) {
        addLog(`no se envió: ${err instanceof Error ? err.message : String(err)}`);
        return false;
      }
      pendingRef.current.add(actionId, frame, label);
      setPendingCount(pendingRef.current.size);
      ws.send(frame);
      addLog(`${label} ${actionId} enviado (pendiente)`);
      setActionMessage({ text: `${label}: pendiente…`, ok: true });
      try {
        const parsed = JSON.parse(frame) as { type?: string; x?: number; y?: number };
        if (parsed.type && onActionSent) {
          onActionSent({ actionId, type: parsed.type, x: parsed.x, y: parsed.y });
        }
      } catch {
        // ignore parse error
      }
      return true;
    },
    [addLog, onActionSent],
  );

  const sendDrop = useCallback(
    (x: number, y: number) =>
      sendAction(`Soltar gota (${x}, ${y})`, (actionId) => buildDropFrame({ x, y, actionId })),
    [sendAction],
  );

  const sendPlaceInstallation = useCallback(
    (installation: string, x: number, y: number) =>
      sendAction(`Colocar ${installation} (${x}, ${y})`, (actionId) =>
        buildPlaceInstallationFrame({ installation, x, y, actionId }),
      ),
    [sendAction],
  );

  const sendBuyUpgrade = useCallback(
    (upgrade: string) =>
      sendAction(`Comprar ${upgrade}`, (actionId) => buildBuyUpgradeFrame({ upgrade, actionId })),
    [sendAction],
  );

  const sendSetDemoMode = useCallback(
    (enabled: boolean) =>
      sendAction(enabled ? "Activar modo demo" : "Desactivar modo demo", (actionId) =>
        buildSetDemoModeFrame({ enabled, actionId }),
      ),
    [sendAction],
  );

  const sendResetValley = useCallback(
    () => sendAction("Reiniciar valle", (actionId) => buildResetValleyFrame({ actionId })),
    [sendAction],
  );

  useEffect(() => {
    if (!session) return;

    let isEffectActive = true;
    pendingRef.current = createPendingActions({ now });
    backoffRef.current = createReconnectBackoff({ random, now });
    rotatorRef.current =
      rotator ??
      (backupHostsKey
        ? createEndpointRotator(location.host, backupHostsKey)
        : typeof window !== "undefined" && location.host === window.location.host
          ? sharedEndpointRotator
          : createEndpointRotator(location.host));

    const currentSession = session;

    function clearSilenceTimer() {
      if (silenceTimerRef.current !== null) {
        clearTimeout(silenceTimerRef.current);
        silenceTimerRef.current = null;
      }
    }

    function armSilenceTimer(ws: WebSocket) {
      clearSilenceTimer();
      if (silenceTimeoutMs <= 0) return;
      silenceTimerRef.current = setTimeout(() => {
        if (!isEffectActive || socketRef.current !== ws) return;
        addLog(`sin estado tras ${(silenceTimeoutMs / 1000).toFixed(1)} s: cerrando conexión para conmutar`);
        ws.close(4000);
      }, silenceTimeoutMs);
    }

    function connect() {
      if (!isEffectActive || !currentSession) return;
      if (reconnectTimerRef.current !== null) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      clearSilenceTimer();

      setStatus("connecting");
      setStatusText("conectando…");

      const activeHost = rotatorRef.current.currentHost() || location.host;
      const playUrl = buildPlayUrl({ protocol: location.protocol, host: activeHost }, currentSession.token);
      const ws = new WebSocket(playUrl);
      socketRef.current = ws;

      ws.addEventListener("open", () => {
        if (!isEffectActive || socketRef.current !== ws) return;
        backoffRef.current.opened();
        if (disconnectedAtRef.current !== null) {
          setOfflineDurationMs(Math.max(0, now() - disconnectedAtRef.current));
          disconnectedAtRef.current = null;
        }
        setStatus("connected");
        setStatusText("conectado");
        armSilenceTimer(ws);

        const plan = pendingRef.current.planReconnect();
        setPendingCount(pendingRef.current.size);
        for (const entry of plan.resend) {
          ws.send(entry.frame);
          addLog(`${entry.label} ${entry.actionId} reenviado (resultado desconocido)`);
        }
        const notes = [
          ...plan.resend.map((e) => `${e.label}: resultado desconocido`),
          ...plan.expired.map((e) => `${e.label}: expiró`),
          ...plan.adminDropped.map((e) => `${e.label}: no se reenvía sola tras reconectar; vuelve a intentarlo`),
        ];
        if (notes.length > 0) {
          setActionMessage({ text: notes.join(" · "), ok: false });
        }
      });

      ws.addEventListener("message", (event) => {
        if (!isEffectActive || socketRef.current !== ws) return;
        armSilenceTimer(ws);
        const frame: PlayFrame = parseFrame(String(event.data));
        const line = describeFrame(frame);
        addLog(line);

        if (frame.type === "state") {
          setGameState(frame);
          // El hub lo manda una sola vez, en el primer estado de la conexión:
          // se guarda hasta que el jugador lo cierre y los estados siguientes
          // (sin resumen) no lo borran.
          const incoming = frame.player?.offline_summary;
          if (incoming) setOfflineSummary(incoming);
        } else if (frame.type === "result") {
          const entry = pendingRef.current.resolve(frame);
          setPendingCount(pendingRef.current.size);
          if (entry) {
            const outcome = describeActionResult(frame);
            setActionMessage({ text: `${entry.label}: ${outcome.text}`, ok: outcome.ok });
            onActionResult?.({ actionId: frame.action_id, status: frame.status });
          }
        } else if (frame.type === "error") {
          const settled = pendingRef.current.settle(frame.action_id);
          setPendingCount(pendingRef.current.size);
          if (settled) {
            setActionMessage({ text: `mensaje rechazado por el hub (${frame.code})`, ok: false });
            onActionResult?.({ actionId: frame.action_id, status: "error" });
          }
        }
      });

      ws.addEventListener("close", (event) => {
        if (!isEffectActive) return;
        const isCurrent = socketRef.current === ws;
        const decision = decideOnClose({
          code: event.code,
          current: isCurrent,
          expiresAt: currentSession.expiresAt,
          now: now(),
        });

        if (decision === "ignore") return;
        clearSilenceTimer();
        backoffRef.current.closed();
        onResetPending?.();
        setStatus("disconnected");
        setOfflineSummary(null);

        if (decision !== "reconnect") {
          socketRef.current = null;
          setStatusText("desconectado");
          onSessionExpired?.("la sesión caducó, vuelve a entrar");
          return;
        }

        disconnectedAtRef.current = now();
        rotatorRef.current.advance();
        const delay = backoffRef.current.nextDelay();
        setStatusText(`desconectado (reintento en ${(delay / 1000).toFixed(1)} s)`);
        reconnectTimerRef.current = setTimeout(connect, delay);
      });
    }

    connect();

    return () => {
      isEffectActive = false;
      clearSilenceTimer();
      if (reconnectTimerRef.current !== null) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      if (socketRef.current) {
        const ws = socketRef.current;
        socketRef.current = null;
        ws.close();
      }
    };
  }, [session, location.protocol, location.host, backupHostsKey, rotator, silenceTimeoutMs, random, now, onSessionExpired, addLog, onActionResult, onResetPending]);

  const effectiveStatus = session ? status : "disconnected";
  const effectiveStatusText = session ? statusText : "desconectado";
  const effectiveGameState = session ? gameState : null;
  const effectiveOfflineSummary = session ? offlineSummary : null;
  const effectiveOfflineDurationMs = session ? offlineDurationMs : null;
  const effectivePendingCount = session ? pendingCount : 0;

  return {
    status: effectiveStatus,
    statusText: effectiveStatusText,
    gameState: effectiveGameState,
    offlineSummary: effectiveOfflineSummary,
    offlineDurationMs: effectiveOfflineDurationMs,
    actionMessage,
    pendingCount: effectivePendingCount,
    logs,
    isOpen: effectiveStatus === "connected",
    sendAction,
    sendDrop,
    sendPlaceInstallation,
    sendBuyUpgrade,
    sendSetDemoMode,
    sendResetValley,
    clearActionMessage,
    dismissOfflineSummary,
  };
}
