// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useSession } from "./useSession.ts";
import { SESSION_KEY, type FetchFn, type StorageLike } from "../logic/auth.ts";

function memoryStorage(initial: Record<string, string> = {}): StorageLike {
  const data = new Map(Object.entries(initial));
  return {
    getItem: (k: string) => (data.has(k) ? (data.get(k) ?? null) : null),
    setItem: (k: string, v: string) => data.set(k, String(v)),
    removeItem: (k: string) => {
      data.delete(k);
    },
  };
}

const OK_BODY = {
  token: "tok.en.sig",
  expires_at: "2026-10-01T12:00:00.000Z",
  player_id: "p1",
  username: "Ana",
};

function fakeFetch(status: number, body?: unknown, headers: Record<string, string> = {}): FetchFn {
  return async () => ({
    status,
    ok: status >= 200 && status < 300,
    headers: { get: (name: string) => headers[name.toLowerCase()] ?? null },
    json: async () => {
      if (body === undefined) throw new SyntaxError("no body");
      return body;
    },
  });
}

describe("useSession", () => {
  it("initializes with null if storage is empty", () => {
    const storage = memoryStorage();
    const { result } = renderHook(() => useSession({ storage }));
    expect(result.current.session).toBeNull();
    expect(result.current.isAdmin).toBe(false);
  });

  it("loads an active session from storage", () => {
    const expiresAt = Date.now() + 100_000;
    const storage = memoryStorage({
      [SESSION_KEY]: JSON.stringify({ token: "t", expiresAt, username: "Ana" }),
    });
    const { result } = renderHook(() => useSession({ storage }));
    expect(result.current.session).toEqual({ token: "t", expiresAt, username: "Ana" });
  });

  it("logs in successfully and saves to storage", async () => {
    const storage = memoryStorage();
    const fetchFn = fakeFetch(200, OK_BODY);
    const { result } = renderHook(() => useSession({ storage, fetchFn }));

    let success: boolean = false;
    await act(async () => {
      success = await result.current.login({ username: "ana", password: "pwd" });
    });

    expect(success).toBe(true);
    expect(result.current.session?.username).toBe("Ana");
    expect(storage.getItem(SESSION_KEY)).toContain("tok.en.sig");
  });

  it("sets error message when login fails with 401", async () => {
    const storage = memoryStorage();
    const fetchFn = fakeFetch(401, { error: "bad creds" });
    const { result } = renderHook(() => useSession({ storage, fetchFn }));

    let success: boolean = true;
    await act(async () => {
      success = await result.current.login({ username: "ana", password: "pwd" });
    });

    expect(success).toBe(false);
    expect(result.current.session).toBeNull();
    expect(result.current.error).toBe("usuario o contraseña incorrectos");
  });

  it("clears session on logout", async () => {
    const expiresAt = Date.now() + 100_000;
    const storage = memoryStorage({
      [SESSION_KEY]: JSON.stringify({ token: "t", expiresAt, username: "Ana" }),
    });
    const { result } = renderHook(() => useSession({ storage }));

    act(() => {
      result.current.logout();
    });

    expect(result.current.session).toBeNull();
    expect(storage.getItem(SESSION_KEY)).toBeNull();
  });

  it("setSessionExpired clears session and shows custom message", () => {
    const expiresAt = Date.now() + 100_000;
    const storage = memoryStorage({
      [SESSION_KEY]: JSON.stringify({ token: "t", expiresAt, username: "Ana" }),
    });
    const { result } = renderHook(() => useSession({ storage }));

    act(() => {
      result.current.setSessionExpired("tu sesión ha expirado");
    });

    expect(result.current.session).toBeNull();
    expect(result.current.error).toBe("tu sesión ha expirado");
    expect(storage.getItem(SESSION_KEY)).toBeNull();
  });

  it("locks retry when 429 occurs", async () => {
    vi.useFakeTimers();
    const storage = memoryStorage();
    const fetchFn = fakeFetch(429, {}, { "retry-after": "5" });
    const { result } = renderHook(() => useSession({ storage, fetchFn }));

    await act(async () => {
      await result.current.login({ username: "ana", password: "pwd" });
    });

    expect(result.current.isRetryLocked).toBe(true);
    expect(result.current.error).toContain("5 s");

    act(() => {
      vi.advanceTimersByTime(5000);
    });

    expect(result.current.isRetryLocked).toBe(false);
    vi.useRealTimers();
  });
});
