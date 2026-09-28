// Conexión a /play para las pruebas de punta a punta: guarda los mensajes y
// deja esperar por uno. Separada de smoke.test.mjs para probarla con un
// WebSocket falso (play-session.test.mjs).

export class PlaySession {
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
    this.ws.addEventListener("message", (event) => this.#onMessage(String(event.data)));
  }

  #onMessage(raw) {
    let frame;
    try {
      frame = JSON.parse(raw);
    } catch {
      // Un mensaje ilegible del hub es un fallo de la prueba, no del
      // proceso: se guarda y se hace fallar a quien estaba esperando.
      this.unparseable.push(raw);
      const error = new Error(`unparseable frame from /play: ${JSON.stringify(raw)}`);
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
   * Sirve para lo que basta con haber visto alguna vez (un estado sin pausa,
   * el resultado de una acción con su `action_id`).
   */
  first(what, match, timeoutMs) {
    const seen = this.frames.find(match);
    if (seen) return Promise.resolve(seen);
    return this.#wait(what, match, timeoutMs);
  }

  /**
   * Primer mensaje que cumple `match` y llega a partir de `fromIndex` (por
   * defecto, a partir de esta llamada). Los mensajes anteriores no cuentan:
   * sirve para comprobar un estado posterior a un hecho.
   */
  waitFor(what, match, timeoutMs, { fromIndex = this.frames.length } = {}) {
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

  close() {
    this.ws.close();
  }
}
