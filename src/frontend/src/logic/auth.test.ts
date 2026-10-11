import { describe, it, expect } from "vitest";
import {
  AuthError,
  clearSession,
  loadSession,
  login,
  register,
  retryDelayMs,
  saveSession,
  SESSION_KEY,
  type FetchFn,
  type StorageLike,
} from "./auth.ts";

function memoryStorage(initial: Record<string, string> = {}): StorageLike & { data: Map<string, string> } {
  const data = new Map(Object.entries(initial));
  return {
    getItem: (k: string) => (data.has(k) ? (data.get(k) ?? null) : null),
    setItem: (k: string, v: string) => data.set(k, String(v)),
    removeItem: (k: string) => {
      data.delete(k);
    },
    data,
  };
}

interface CallRecord {
  url: string;
  init?: RequestInit;
}

/** fetch falso que devuelve una respuesta fija y recuerda la petición. */
function fakeFetch(
  status: number,
  body?: unknown,
  headers: Record<string, string> = {},
): FetchFn & { calls: CallRecord[] } {
  const calls: CallRecord[] = [];
  const fn: FetchFn = async (url: string, init?: RequestInit) => {
    calls.push({ url, init });
    return {
      status,
      ok: status >= 200 && status < 300,
      headers: { get: (name: string) => headers[name.toLowerCase()] ?? null },
      json: async () => {
        if (body === undefined) throw new SyntaxError("no body");
        return body;
      },
    };
  };
  return Object.assign(fn, { calls });
}

const creds = { username: "ana", password: "s3cret-pass" };
const OK = {
  token: "tok.en.sig",
  expires_at: "2026-10-01T12:00:00.000Z",
  player_id: "0b9f1c2e-8d3a-4c1b-9e2f-1a2b3c4d5e6f",
  username: "Ana",
};

function jwt(payload: Record<string, unknown>): string {
  const b64 = (o: unknown) =>
    btoa(JSON.stringify(o))
      .replace(/\+/g, "-")
      .replace(/\//g, "_")
      .replace(/=+$/, "");
  return `${b64({ alg: "EdDSA" })}.${b64(payload)}.sig`;
}

describe("register", () => {
  it("posts JSON to /api/auth/register and returns the session", async () => {
    const f = fakeFetch(201, OK);
    const session = await register(f, creds);
    expect(f.calls[0]?.url).toBe("/api/auth/register");
    expect(f.calls[0]?.init?.method).toBe("POST");
    expect((f.calls[0]?.init?.headers as Record<string, string>)?.["Content-Type"]).toBe("application/json");
    expect(JSON.parse(f.calls[0]?.init?.body as string)).toEqual(creds);
    expect(session).toEqual({
      token: OK.token,
      expiresAt: Date.parse(OK.expires_at),
      username: "Ana",
    });
  });

  it("maps 409 to a Spanish message", async () => {
    await expect(register(fakeFetch(409, { error: "x" }), creds)).rejects.toMatchObject({
      name: "AuthError",
      status: 409,
      message: "el nombre de usuario ya existe",
    });
  });

  it("shows the API message on 400", async () => {
    await expect(
      register(fakeFetch(400, { error: "username too short" }), creds),
    ).rejects.toMatchObject({
      status: 400,
      message: "username too short",
    });
  });

  it("falls back to a generic message on 400 without body", async () => {
    await expect(register(fakeFetch(400), creds)).rejects.toMatchObject({
      status: 400,
      message: "los datos no son válidos",
    });
  });
});

describe("login", () => {
  it("posts to /api/auth/login", async () => {
    const f = fakeFetch(200, OK);
    await login(f, creds);
    expect(f.calls[0]?.url).toBe("/api/auth/login");
  });

  it("completes the username from the token when the body omits it (login)", async () => {
    const token = jwt({ player_id: OK.player_id, username: "Ana", role: "player" });
    const s = await login(fakeFetch(200, { token, expires_at: OK.expires_at }), creds);
    expect(s.username).toBe("Ana");
  });

  it("does not expose a playerId in the session", async () => {
    const token = jwt({ player_id: OK.player_id, username: "Ana", role: "player" });
    const s = await login(fakeFetch(200, { token, expires_at: OK.expires_at }), creds);
    expect("playerId" in s).toBe(false);
    expect("playerId" in (await register(fakeFetch(201, OK), creds))).toBe(false);
  });

  it("does not decode the token when the response already has the username", async () => {
    const token = jwt({ username: "FromToken" });
    const s = await register(fakeFetch(201, { ...OK, token }), creds);
    expect(s.username).toBe("Ana");
  });

  it("falls back to the typed username when nothing else is available", async () => {
    const s = await login(fakeFetch(200, { token: "opaque", expires_at: OK.expires_at }), creds);
    expect(s.username).toBe("ana");
  });

  it("maps 401", async () => {
    await expect(login(fakeFetch(401, { error: "x" }), creds)).rejects.toMatchObject({
      status: 401,
      message: "usuario o contraseña incorrectos",
    });
  });

  it("maps 429 with the Retry-After seconds", async () => {
    await expect(login(fakeFetch(429, {}, { "retry-after": "42" }), creds)).rejects.toMatchObject({
      status: 429,
      retryAfter: 42,
      message: "demasiados intentos, espera 42 s",
    });
  });

  it("maps 429 without Retry-After", async () => {
    await expect(login(fakeFetch(429, {}), creds)).rejects.toMatchObject({
      status: 429,
      message: "demasiados intentos, espera unos segundos",
    });
  });

  it("maps 503", async () => {
    await expect(login(fakeFetch(503, {}), creds)).rejects.toMatchObject({
      status: 503,
      message: "el servicio no está disponible, prueba en unos segundos",
    });
  });

  it("reports network failures", async () => {
    const boom: FetchFn = async () => {
      throw new TypeError("failed to fetch");
    };
    await expect(login(boom, creds)).rejects.toSatisfy((err: unknown) => {
      expect(err).toBeInstanceOf(AuthError);
      const authErr = err as AuthError;
      expect(authErr.status).toBe(0);
      expect(authErr.message).toBe("no se pudo contactar con el servidor");
      return true;
    });
  });

  it("rejects a success response without token", async () => {
    await expect(login(fakeFetch(200, {}), creds)).rejects.toMatchObject({ status: 200 });
  });

  it("never sends the password anywhere but the request body", async () => {
    const f = fakeFetch(200, OK);
    await login(f, creds);
    expect(f.calls[0]?.url.includes(creds.password)).toBe(false);
  });
});

describe("session storage", () => {
  const session = { token: "t", expiresAt: 2_000, username: "Ana" };

  it("saves and loads a session that has not expired", () => {
    const storage = memoryStorage();
    saveSession(storage, session);
    expect(loadSession(storage, 1_000)).toEqual(session);
  });

  it("never stores a playerId", () => {
    const storage = memoryStorage();
    saveSession(storage, { ...session, ...{ playerId: "pid" } });
    expect(storage.getItem(SESSION_KEY)?.includes("pid")).toBe(false);
  });

  it("still loads a session saved by an older version (with playerId)", () => {
    const storage = memoryStorage({
      [SESSION_KEY]: JSON.stringify({ ...session, playerId: "pid" }),
    });
    expect(loadSession(storage, 1_000)).toEqual(session);
  });

  it("never stores a password", () => {
    const storage = memoryStorage();
    saveSession(storage, { ...session, ...{ password: "nope" } });
    expect(storage.getItem(SESSION_KEY)?.includes("nope")).toBe(false);
  });

  it("returns null when missing", () => {
    expect(loadSession(memoryStorage(), 0)).toBeNull();
  });

  it("returns null and clears an expired session", () => {
    const storage = memoryStorage();
    saveSession(storage, session);
    expect(loadSession(storage, 2_000)).toBeNull();
    expect(storage.getItem(SESSION_KEY)).toBeNull();
  });

  it("returns null and clears malformed data", () => {
    const storage = memoryStorage({ [SESSION_KEY]: "{oops" });
    expect(loadSession(storage, 0)).toBeNull();
    expect(storage.getItem(SESSION_KEY)).toBeNull();
  });

  it("clears sessions with missing fields", () => {
    const storage = memoryStorage({ [SESSION_KEY]: JSON.stringify({ token: "t" }) });
    expect(loadSession(storage, 0)).toBeNull();
    expect(storage.getItem(SESSION_KEY)).toBeNull();
  });

  it("clearSession removes it", () => {
    const storage = memoryStorage();
    saveSession(storage, session);
    clearSession(storage);
    expect(storage.getItem(SESSION_KEY)).toBeNull();
  });

  it("tolerates blocked storage", () => {
    const blocked: StorageLike = {
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
    expect(loadSession(blocked, 0)).toBeNull();
    expect(() => saveSession(blocked, session)).not.toThrow();
    expect(() => clearSession(blocked)).not.toThrow();
  });
});

describe("retryDelayMs", () => {
  it("uses the Retry-After seconds of a 429, in milliseconds", () => {
    expect(retryDelayMs(new AuthError("x", 429, 42))).toBe(42_000);
  });

  it("uses a short default for a 429 without Retry-After", () => {
    expect(retryDelayMs(new AuthError("x", 429))).toBe(5_000);
  });

  it("is 0 for other errors and for non-AuthError values", () => {
    expect(retryDelayMs(new AuthError("x", 401))).toBe(0);
    expect(retryDelayMs(new Error("boom"))).toBe(0);
    expect(retryDelayMs(undefined)).toBe(0);
  });

  it("caps an absurd Retry-After", () => {
    expect(retryDelayMs(new AuthError("x", 429, 86_400))).toBe(600_000);
  });
});
