// Conexión a /chat para las pruebas de punta a punta: guarda los mensajes y
// deja esperar por mensajes de chat (message, synced, etc.).
import { randomUUID } from "node:crypto";

export class ChatSession {
  /**
   * @param {string} url
   * @param {{createSocket?: (url: string) => WebSocket}} [options]
   */
  constructor(url, { createSocket = (u) => new WebSocket(u) } = {}) {
    /** Mensajes decodificados, en orden de llegada. */
    this.frames = [];
    /** Mensajes que no eran JSON, tal como llegaron. */
    this.unparseable = [];
    this.waiters = [];
    this.ws = createSocket(url);
    this.opened = new Promise((resolve, reject) => {
      this.ws.addEventListener("open", resolve, { once: true });
      this.ws.addEventListener("error", () => reject(new Error("websocket error")), { once: true });
      this.ws.addEventListener("close", (e) => reject(new Error(`websocket closed (${e.code})`)), {
        once: true,
      });
    });
    this.opened.catch(() => {});
    /** Código y motivo con los que se cerró la conexión (p. ej. 4401). */
    this.closed = new Promise((resolve) => {
      this.ws.addEventListener("close", (e) => resolve({ code: e.code, reason: e.reason }), { once: true });
    });
    this.ws.addEventListener("message", (event) => this.#onMessage(String(event.data)));
  }

  #onMessage(raw) {
    let frame;
    try {
      frame = JSON.parse(raw);
    } catch {
      this.unparseable.push(raw);
      const error = new Error(`unparseable frame from /chat: ${JSON.stringify(raw)}`);
      const waiters = this.waiters;
      this.waiters = [];
      for (const waiter of waiters) waiter.reject(error);
      return;
    }
    this.frames.push(frame);
    const remaining = [];
    for (const waiter of this.waiters) {
      if (waiter.match(frame)) {
        waiter.resolve(frame);
      } else {
        remaining.push(waiter);
      }
    }
    this.waiters = remaining;
  }

  /**
   * Primer mensaje que cumple `match`, incluidos los que ya llegaron.
   */
  first(what, match, timeoutMs = 10_000) {
    const seen = this.frames.find(match);
    if (seen) return Promise.resolve(seen);
    return this.#wait(what, match, timeoutMs);
  }

  /**
   * Primer mensaje que cumple `match` y llega a partir de `fromIndex`.
   */
  waitFor(what, match, timeoutMs = 10_000, { fromIndex = this.frames.length } = {}) {
    const seen = this.frames.slice(fromIndex).find(match);
    if (seen) return Promise.resolve(seen);
    return this.#wait(what, match, timeoutMs);
  }

  #wait(what, match, timeoutMs) {
    return new Promise((resolve, reject) => {
      const waiter = {
        match,
        resolve: (frame) => {
          clearTimeout(timer);
          resolve(frame);
        },
        reject: (err) => {
          clearTimeout(timer);
          reject(err);
        },
      };
      const timer = setTimeout(() => {
        this.waiters = this.waiters.filter((w) => w !== waiter);
        reject(new Error(`timed out waiting for ${what}`));
      }, timeoutMs);
      this.waiters.push(waiter);
    });
  }

  send(message) {
    this.ws.send(JSON.stringify(message));
  }

  sendMessage(text, messageId = randomUUID()) {
    const frame = { type: "send", text, message_id: messageId };
    this.send(frame);
    return messageId;
  }

  sync(afterSeq = 0) {
    this.send({ type: "sync", after_seq: Number(afterSeq) });
  }

  close() {
    this.ws.close();
  }
}
