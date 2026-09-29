import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  AuthError,
  clearSession,
  loadSession,
  login,
  register,
  retryDelayMs,
  saveSession,
  SESSION_KEY,
} from "../js/auth.js";

function memoryStorage(initial = {}) {
  const data = new Map(Object.entries(initial));
  return {
    getItem: (k) => (data.has(k) ? data.get(k) : null),
    setItem: (k, v) => data.set(k, String(v)),
    removeItem: (k) => data.delete(k),
    data,
  };
}

/** fetch falso que devuelve una respuesta fija y recuerda la petición. */
function fakeFetch(status, body, headers = {}) {
  const calls = [];
  const fn = async (url, init) => {
    calls.push({ url, init });
    return {
      status,
      ok: status >= 200 && status < 300,
      headers: { get: (name) => headers[name.toLowerCase()] ?? null },
      json: async () => {
        if (body === undefined) throw new SyntaxError("no body");
        return body;
      },
    };
  };
  fn.calls = calls;
  return fn;
}

const creds = { username: "ana", password: "s3cret-pass" };
const OK = {
  token: "tok.en.sig",
  expires_at: "2026-10-01T12:00:00.000Z",
  player_id: "0b9f1c2e-8d3a-4c1b-9e2f-1a2b3c4d5e6f",
  username: "Ana",
};

function jwt(payload) {
  const b64 = (o) => Buffer.from(JSON.stringify(o)).toString("base64url");
  return `${b64({ alg: "EdDSA" })}.${b64(payload)}.sig`;
}

describe("register", () => {
  it("posts JSON to /api/auth/register and returns the session", async () => {
    const f = fakeFetch(201, OK);
    const session = await register(f, creds);
    assert.equal(f.calls[0].url, "/api/auth/register");
    assert.equal(f.calls[0].init.method, "POST");
    assert.equal(f.calls[0].init.headers["Content-Type"], "application/json");
    assert.deepEqual(JSON.parse(f.calls[0].init.body), creds);
    assert.deepEqual(session, {
      token: OK.token,
      expiresAt: Date.parse(OK.expires_at),
      username: "Ana",
    });
  });

  it("maps 409 to a Spanish message", async () => {
    await assert.rejects(register(fakeFetch(409, { error: "x" }), creds), {
      name: "AuthError",
      status: 409,
      message: "el nombre de usuario ya existe",
    });
  });

  it("shows the API message on 400", async () => {
    await assert.rejects(
      register(fakeFetch(400, { error: "username too short" }), creds),
      { status: 400, message: "username too short" },
    );
  });

  it("falls back to a generic message on 400 without body", async () => {
    await assert.rejects(register(fakeFetch(400), creds), {
      status: 400,
      message: "los datos no son válidos",
    });
  });
});

describe("login", () => {
  it("posts to /api/auth/login", async () => {
    const f = fakeFetch(200, OK);
    await login(f, creds);
    assert.equal(f.calls[0].url, "/api/auth/login");
  });

  it("completes the username from the token when the body omits it (login)", async () => {
    const token = jwt({ player_id: OK.player_id, username: "Ana", role: "player" });
    const s = await login(fakeFetch(200, { token, expires_at: OK.expires_at }), creds);
    assert.equal(s.username, "Ana");
  });

  it("does not expose a playerId in the session", async () => {
    const token = jwt({ player_id: OK.player_id, username: "Ana", role: "player" });
    const s = await login(fakeFetch(200, { token, expires_at: OK.expires_at }), creds);
    assert.ok(!("playerId" in s));
    assert.ok(!("playerId" in (await register(fakeFetch(201, OK), creds))));
  });

  it("does not decode the token when the response already has the username", async () => {
    const token = jwt({ username: "FromToken" });
    const s = await register(fakeFetch(201, { ...OK, token }), creds);
    assert.equal(s.username, "Ana");
  });

  it("falls back to the typed username when nothing else is available", async () => {
    const s = await login(fakeFetch(200, { token: "opaque", expires_at: OK.expires_at }), creds);
    assert.equal(s.username, "ana");
  });

  it("maps 401", async () => {
    await assert.rejects(login(fakeFetch(401, { error: "x" }), creds), {
      status: 401,
      message: "usuario o contraseña incorrectos",
    });
  });

  it("maps 429 with the Retry-After seconds", async () => {
    await assert.rejects(login(fakeFetch(429, {}, { "retry-after": "42" }), creds), {
      status: 429,
      retryAfter: 42,
      message: "demasiados intentos, espera 42 s",
    });
  });

  it("maps 429 without Retry-After", async () => {
    await assert.rejects(login(fakeFetch(429, {}), creds), {
      status: 429,
      message: "demasiados intentos, espera unos segundos",
    });
  });

  it("maps 503", async () => {
    await assert.rejects(login(fakeFetch(503, {}), creds), {
      status: 503,
      message: "el servicio no está disponible, prueba en unos segundos",
    });
  });

  it("reports network failures", async () => {
    const boom = async () => {
      throw new TypeError("failed to fetch");
    };
    await assert.rejects(login(boom, creds), (err) => {
      assert.ok(err instanceof AuthError);
      assert.equal(err.status, 0);
      assert.equal(err.message, "no se pudo contactar con el servidor");
      return true;
    });
  });

  it("rejects a success response without token", async () => {
    await assert.rejects(login(fakeFetch(200, {}), creds), { status: 200 });
  });

  it("never sends the password anywhere but the request body", async () => {
    const f = fakeFetch(200, OK);
    await login(f, creds);
    assert.ok(!f.calls[0].url.includes(creds.password));
  });
});

describe("session storage", () => {
  const session = { token: "t", expiresAt: 2_000, username: "Ana" };

  it("saves and loads a session that has not expired", () => {
    const storage = memoryStorage();
    saveSession(storage, session);
    assert.deepEqual(loadSession(storage, 1_000), session);
  });

  it("never stores a playerId", () => {
    const storage = memoryStorage();
    saveSession(storage, { ...session, playerId: "pid" });
    assert.ok(!storage.getItem(SESSION_KEY).includes("pid"));
  });

  it("still loads a session saved by an older version (with playerId)", () => {
    const storage = memoryStorage({
      [SESSION_KEY]: JSON.stringify({ ...session, playerId: "pid" }),
    });
    assert.deepEqual(loadSession(storage, 1_000), session);
  });

  it("never stores a password", () => {
    const storage = memoryStorage();
    saveSession(storage, { ...session, password: "nope" });
    assert.ok(!storage.getItem(SESSION_KEY).includes("nope"));
  });

  it("returns null when missing", () => {
    assert.equal(loadSession(memoryStorage(), 0), null);
  });

  it("returns null and clears an expired session", () => {
    const storage = memoryStorage();
    saveSession(storage, session);
    assert.equal(loadSession(storage, 2_000), null);
    assert.equal(storage.getItem(SESSION_KEY), null);
  });

  it("returns null and clears malformed data", () => {
    const storage = memoryStorage({ [SESSION_KEY]: "{oops" });
    assert.equal(loadSession(storage, 0), null);
    assert.equal(storage.getItem(SESSION_KEY), null);
  });

  it("clears sessions with missing fields", () => {
    const storage = memoryStorage({ [SESSION_KEY]: JSON.stringify({ token: "t" }) });
    assert.equal(loadSession(storage, 0), null);
    assert.equal(storage.getItem(SESSION_KEY), null);
  });

  it("clearSession removes it", () => {
    const storage = memoryStorage();
    saveSession(storage, session);
    clearSession(storage);
    assert.equal(storage.getItem(SESSION_KEY), null);
  });

  it("tolerates blocked storage", () => {
    const blocked = {
      getItem() {
        throw new Error("blocked");
      },
      setItem() {
        throw new Error("blocked");
      },
      removeItem() {
        throw new Error("blocked");
      },
    };
    assert.equal(loadSession(blocked, 0), null);
    assert.doesNotThrow(() => saveSession(blocked, session));
    assert.doesNotThrow(() => clearSession(blocked));
  });
});

describe("retryDelayMs", () => {
  it("uses the Retry-After seconds of a 429, in milliseconds", () => {
    assert.equal(retryDelayMs(new AuthError("x", 429, 42)), 42_000);
  });

  it("uses a short default for a 429 without Retry-After", () => {
    assert.equal(retryDelayMs(new AuthError("x", 429)), 5_000);
  });

  it("is 0 for other errors and for non-AuthError values", () => {
    assert.equal(retryDelayMs(new AuthError("x", 401)), 0);
    assert.equal(retryDelayMs(new Error("boom")), 0);
    assert.equal(retryDelayMs(undefined), 0);
  });

  it("caps an absurd Retry-After", () => {
    assert.equal(retryDelayMs(new AuthError("x", 429, 86_400)), 600_000);
  });
});
