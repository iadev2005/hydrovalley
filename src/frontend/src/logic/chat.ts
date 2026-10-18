// Mensajes del WebSocket /chat (proto/hydrovalley/chat/v1/chat.proto)
// y utilidades sin DOM para el panel de chat.

import { newUuid } from "./play.ts";

export interface ChatMessage {
  seq: number;
  messageId: string;
  senderId: string;
  username: string;
  text: string;
  kind: "player" | "system";
  sentAt: string;
  serverEpoch: string;
}

export interface SendFrame {
  type: "send";
  message_id: string;
  text: string;
}

export interface SyncFrame {
  type: "sync";
  after_seq: string;
}

export interface MessageFrame {
  type: "message";
  seq: string;
  message_id: string;
  sender_id: string;
  username: string;
  text: string;
  kind: "player" | "system";
  sent_at: string;
  server_epoch: string;
}

export interface SyncedFrame {
  type: "synced";
  server_epoch: string;
  last_seq: string;
}

export type ChatFrame = MessageFrame | SyncedFrame | { type: string };

/**
 * Construye la URL de conexión al WebSocket de /chat a partir del protocolo, host y token.
 */
export function buildChatUrl({
  protocol,
  host,
  token,
}: {
  protocol: string;
  host: string;
  token: string;
}): string {
  const wsProtocol = protocol === "https:" ? "wss:" : "ws:";
  return `${wsProtocol}//${host}/chat?token=${encodeURIComponent(token)}`;
}

/**
 * Construye el payload JSON de SendFrame con un nuevo UUID si no se proporciona.
 */
export function buildSendFrame(text: string, messageId = newUuid()): string {
  return JSON.stringify({
    type: "send",
    message_id: messageId,
    text,
  });
}

/**
 * Construye el payload JSON de SyncFrame para pedir mensajes perdidos.
 */
export function buildSyncFrame(afterSeq: number | string): string {
  return JSON.stringify({
    type: "sync",
    after_seq: String(afterSeq),
  });
}

/**
 * Parsea un frame JSON entrante del WebSocket /chat.
 */
export function parseChatFrame(raw: string): ChatFrame {
  try {
    const data = JSON.parse(raw) as { type?: unknown };
    if (!data || typeof data !== "object" || typeof data.type !== "string") {
      return { type: "unknown" };
    }
    return data as ChatFrame;
  } catch {
    return { type: "unknown" };
  }
}
