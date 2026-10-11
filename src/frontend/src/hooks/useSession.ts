import { useCallback, useEffect, useRef, useState } from "react";
import {
  clearSession,
  loadSession,
  login as apiLogin,
  register as apiRegister,
  retryDelayMs,
  saveSession,
  AuthError,
  type Credentials,
  type FetchFn,
  type Session,
  type StorageLike,
} from "../logic/auth.ts";
import { isAdminRole, tokenRole } from "../logic/valley.ts";

export interface UseSessionOptions {
  storage?: StorageLike;
  fetchFn?: FetchFn;
  now?: () => number;
}

export interface UseSessionReturn {
  session: Session | null;
  isAdmin: boolean;
  error: string;
  isBusy: boolean;
  isRetryLocked: boolean;
  login: (credentials: Credentials) => Promise<boolean>;
  register: (credentials: Credentials) => Promise<boolean>;
  logout: () => void;
  clearError: () => void;
  setSessionExpired: (message?: string) => void;
}

export function useSession({
  storage = typeof window !== "undefined" ? window.localStorage : undefined,
  fetchFn = typeof window !== "undefined" ? (window.fetch.bind(window) as FetchFn) : undefined,
  now = Date.now,
}: UseSessionOptions = {}): UseSessionReturn {
  const [session, setSession] = useState<Session | null>(() => {
    if (!storage) return null;
    return loadSession(storage, now());
  });
  const [error, setError] = useState<string>("");
  const [isBusy, setIsBusy] = useState<boolean>(false);
  const [isRetryLocked, setIsRetryLocked] = useState<boolean>(false);
  const retryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (retryTimerRef.current !== null) {
        clearTimeout(retryTimerRef.current);
      }
    };
  }, []);

  const clearError = useCallback(() => setError(""), []);

  const handleAuth = useCallback(
    async (authAction: (fn: FetchFn, creds: Credentials) => Promise<Session>, creds: Credentials) => {
      if (!fetchFn || isBusy || isRetryLocked) return false;
      if (creds.username.trim() === "" || creds.password === "") {
        setError("escribe el usuario y la contraseña");
        return false;
      }
      setIsBusy(true);
      setError("");
      try {
        const newSession = await authAction(fetchFn, creds);
        if (storage) {
          saveSession(storage, newSession);
        }
        setSession(newSession);
        setIsBusy(false);
        return true;
      } catch (err) {
        setIsBusy(false);
        const message = err instanceof AuthError ? err.message : "no se pudo contactar con el servidor";
        setError(message);
        const waitMs = retryDelayMs(err);
        if (waitMs > 0) {
          setIsRetryLocked(true);
          if (retryTimerRef.current !== null) clearTimeout(retryTimerRef.current);
          retryTimerRef.current = setTimeout(() => {
            setIsRetryLocked(false);
            retryTimerRef.current = null;
          }, waitMs);
        }
        return false;
      }
    },
    [fetchFn, isBusy, isRetryLocked, storage],
  );

  const login = useCallback(
    (creds: Credentials) => handleAuth(apiLogin, creds),
    [handleAuth],
  );

  const register = useCallback(
    (creds: Credentials) => handleAuth(apiRegister, creds),
    [handleAuth],
  );

  const logout = useCallback(() => {
    if (storage) {
      clearSession(storage);
    }
    setSession(null);
    setError("");
  }, [storage]);

  const setSessionExpired = useCallback(
    (message = "la sesión caducó, vuelve a entrar") => {
      if (storage) {
        clearSession(storage);
      }
      setSession(null);
      setError(message);
    },
    [storage],
  );

  const isAdmin = session !== null && isAdminRole(tokenRole(session.token));

  return {
    session,
    isAdmin,
    error,
    isBusy,
    isRetryLocked,
    login,
    register,
    logout,
    clearError,
    setSessionExpired,
  };
}
