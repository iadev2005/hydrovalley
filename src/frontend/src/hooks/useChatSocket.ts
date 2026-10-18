import { useCallback, useEffect, useRef, useState } from "react";
import type { Session } from "../logic/auth.ts";
import {
  buildChatUrl,
  buildSendFrame,
  buildSyncFrame,
  parseChatFrame,
  type ChatMessage,
  type MessageFrame,
  type SyncedFrame,
} from "../logic/chat.ts";
import {
  AUTH_CLOSE_CODE,
  createReconnectBackoff,
  type ConnectionStatus,
} from "../logic/play.ts";

export interface UseChatSocketOptions {
  session: Session | null;
  onSessionExpired?: (reason: string) => void;
  location?: { protocol: string; host: string };
  random?: () => number;
  now?: () => number;
}

export interface UseChatSocketReturn {
  status: ConnectionStatus;
  statusText: string;
  messages: ChatMessage[];
  isOpen: boolean;
  sendMessage: (text: string) => boolean;
}

export function useChatSocket({
  session,
  onSessionExpired,
  location = typeof window !== "undefined" ? window.location : { protocol: "http:", host: "localhost:8080" },
  random = Math.random,
  now = Date.now,
}: UseChatSocketOptions): UseChatSocketReturn {
  const [status, setStatus] = useState<ConnectionStatus>("disconnected");
  const [statusText, setStatusText] = useState<string>("desconectado");
  const [messages, setMessages] = useState<ChatMessage[]>([]);

  const socketRef = useRef<WebSocket | null>(null);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastSeqRef = useRef<number>(0);
  const serverEpochRef = useRef<string | null>(null);
  const backoffRef = useRef(createReconnectBackoff({ random, now }));

  const clearReconnectTimer = useCallback(() => {
    if (reconnectTimerRef.current !== null) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
  }, []);

  const sendMessage = useCallback((text: string): boolean => {
    const trimmed = text.trim();
    const ws = socketRef.current;
    if (!trimmed || !ws || ws.readyState !== WebSocket.OPEN) {
      return false;
    }
    try {
      ws.send(buildSendFrame(trimmed));
      return true;
    } catch {
      return false;
    }
  }, []);

  useEffect(() => {
    if (!session) {
      clearReconnectTimer();
      if (socketRef.current) {
        socketRef.current.close();
        socketRef.current = null;
      }
      setStatus("disconnected");
      setStatusText("desconectado");
      setMessages([]);
      lastSeqRef.current = 0;
      serverEpochRef.current = null;
      backoffRef.current = createReconnectBackoff({ random, now });
      return;
    }

    let isDisposed = false;

    const connect = () => {
      if (isDisposed) return;
      clearReconnectTimer();

      const url = buildChatUrl({
        protocol: location.protocol,
        host: location.host,
        token: session.token,
      });

      setStatus("connecting");
      setStatusText("conectando...");

      const ws = new WebSocket(url);
      socketRef.current = ws;

      ws.onopen = () => {
        if (isDisposed || socketRef.current !== ws) {
          ws.close();
          return;
        }
        setStatus("connected");
        setStatusText("conectado");
        backoffRef.current.opened();

        // Tras abrir o reconectar, pedir historial o mensajes perdidos con sync
        const afterSeq = lastSeqRef.current;
        ws.send(buildSyncFrame(afterSeq));
      };

      ws.onmessage = (event) => {
        if (isDisposed || socketRef.current !== ws) return;
        const frame = parseChatFrame(String(event.data));

        if (frame.type === "message") {
          const mf = frame as MessageFrame;
          const msgEpoch = mf.server_epoch;

          // Si server_epoch cambió, el servicio se reinició: descartar historial y resincronizar
          if (serverEpochRef.current && msgEpoch !== serverEpochRef.current) {
            serverEpochRef.current = msgEpoch;
            lastSeqRef.current = 0;
            setMessages([]);
            ws.send(buildSyncFrame(0));
            return;
          }

          serverEpochRef.current = msgEpoch;
          const seq = parseInt(mf.seq, 10);
          if (seq > lastSeqRef.current) {
            lastSeqRef.current = seq;
          }

          const newMsg: ChatMessage = {
            seq,
            messageId: mf.message_id,
            senderId: mf.sender_id,
            username: mf.username,
            text: mf.text,
            kind: mf.kind === "system" ? "system" : "player",
            sentAt: mf.sent_at,
            serverEpoch: mf.server_epoch,
          };

          setMessages((prev) => {
            if (prev.some((m) => m.messageId === newMsg.messageId)) {
              return prev;
            }
            const updated = [...prev, newMsg];
            updated.sort((a, b) => a.seq - b.seq);
            return updated.slice(-200);
          });
        } else if (frame.type === "synced") {
          const sf = frame as SyncedFrame;
          if (serverEpochRef.current && sf.server_epoch !== serverEpochRef.current) {
            serverEpochRef.current = sf.server_epoch;
            lastSeqRef.current = 0;
            setMessages([]);
            ws.send(buildSyncFrame(0));
          } else {
            serverEpochRef.current = sf.server_epoch;
          }
        }
      };

      ws.onclose = (event) => {
        if (isDisposed || socketRef.current !== ws) return;
        socketRef.current = null;
        backoffRef.current.closed();
        setStatus("disconnected");

        if (event.code === AUTH_CLOSE_CODE) {
          setStatusText("sesión inválida o expirada");
          onSessionExpired?.("sesión inválida o expirada");
          return;
        }

        setStatusText("desconectado (reintentando...)");
        const delay = backoffRef.current.nextDelay();
        reconnectTimerRef.current = setTimeout(connect, delay);
      };

      ws.onerror = () => {
        // El fallo provocará onclose y la política de reconexión
      };
    };

    connect();

    return () => {
      isDisposed = true;
      clearReconnectTimer();
      if (socketRef.current) {
        const ws = socketRef.current;
        socketRef.current = null;
        ws.close();
      }
    };
  }, [session, location.protocol, location.host, clearReconnectTimer, onSessionExpired, random, now]);

  const effectiveStatus = session ? status : "disconnected";
  const effectiveStatusText = session ? statusText : "";

  return {
    status: effectiveStatus,
    statusText: effectiveStatusText,
    messages,
    isOpen: effectiveStatus === "connected",
    sendMessage,
  };
}
