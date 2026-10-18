import { useEffect, useRef, useState, type FormEvent } from "react";
import type { ChatMessage } from "../logic/chat.ts";
import type { ConnectionStatus } from "../logic/play.ts";

export interface ChatPanelProps {
  hidden?: boolean;
  messages: ChatMessage[];
  status: ConnectionStatus;
  statusText: string;
  onSendMessage: (text: string) => boolean;
}

export function ChatPanel({
  hidden = false,
  messages,
  status,
  statusText,
  onSendMessage,
}: ChatPanelProps) {
  const [inputText, setInputText] = useState("");
  const messagesEndRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!hidden) {
      messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
    }
  }, [messages, hidden]);

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    const trimmed = inputText.trim();
    if (!trimmed) return;
    const ok = onSendMessage(trimmed);
    if (ok) {
      setInputText("");
    }
  };

  const formatTime = (isoString: string): string => {
    try {
      const d = new Date(isoString);
      return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
    } catch {
      return "";
    }
  };

  return (
    <section id="chat-panel" aria-labelledby="chat-title" hidden={hidden} className="panel-box">
      <div className="chat-header">
        <h2 id="chat-title">Chat del Valle</h2>
        <span
          id="chat-status"
          className={`chat-status chat-status-${status}`}
          role="status"
          title={`Estado de conexión: ${statusText}`}
        >
          {statusText}
        </span>
      </div>

      <div className="chat-messages-container" role="log" aria-live="polite" aria-label="Mensajes de chat">
        {messages.length === 0 ? (
          <p className="meta chat-empty-msg">No hay mensajes recientes en el valle.</p>
        ) : (
          messages.map((msg) => (
            <div
              key={msg.messageId}
              className={`chat-message chat-message-${msg.kind}`}
              data-seq={msg.seq}
            >
              <span className="chat-msg-time">[{formatTime(msg.sentAt)}]</span>{" "}
              {msg.kind === "system" ? (
                <span className="chat-msg-system-badge">[Sistema]</span>
              ) : (
                <strong className="chat-msg-author">{msg.username}:</strong>
              )}{" "}
              <span className="chat-msg-text">{msg.text}</span>
            </div>
          ))
        )}
        <div ref={messagesEndRef} />
      </div>

      <form id="chat-form" className="chat-form" onSubmit={handleSubmit}>
        <input
          id="chat-input"
          type="text"
          placeholder={status === "connected" ? "Escribe un mensaje..." : "Conectando al chat..."}
          value={inputText}
          onChange={(e) => setInputText(e.target.value)}
          maxLength={200}
          disabled={status !== "connected"}
          aria-label="Mensaje para el chat"
        />
        <button
          id="chat-submit"
          type="submit"
          disabled={status !== "connected" || !inputText.trim()}
        >
          Enviar
        </button>
      </form>
    </section>
  );
}
