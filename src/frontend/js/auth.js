// Acceso a la API (registro y entrada) y sesión guardada en el navegador.
// Funciones sin DOM: `fetch`, el almacenamiento y el reloj se inyectan para
// poder probarlas con node:test.

/** Clave de `localStorage` donde se guarda la sesión. Nunca contiene la contraseña. */
export const SESSION_KEY = "hydrovalley.session";

const REGISTER_PATH = "/api/auth/register";
const LOGIN_PATH = "/api/auth/login";

/** Error de acceso con un mensaje ya listo para mostrar al usuario. */
export class AuthError extends Error {
  /**
   * @param {string} message
   * @param {number} status estado HTTP (0 si no hubo respuesta)
   * @param {number} [retryAfter] segundos indicados por `Retry-After` (429)
   */
  constructor(message, status, retryAfter) {
    super(message);
    this.name = "AuthError";
    this.status = status;
    if (retryAfter !== undefined) this.retryAfter = retryAfter;
  }
}

async function readJson(response) {
  try {
    return await response.json();
  } catch {
    return null;
  }
}

/** Mensaje para el usuario según el estado HTTP de un fallo. */
function failure(response, body) {
  switch (response.status) {
    case 400: {
      const apiMessage = body && typeof body.error === "string" ? body.error : "";
      return new AuthError(apiMessage || "los datos no son válidos", 400);
    }
    case 401:
      return new AuthError("usuario o contraseña incorrectos", 401);
    case 409:
      return new AuthError("el nombre de usuario ya existe", 409);
    case 429: {
      const seconds = Number.parseInt(response.headers.get("retry-after") ?? "", 10);
      if (Number.isInteger(seconds) && seconds >= 0) {
        return new AuthError(`demasiados intentos, espera ${seconds} s`, 429, seconds);
      }
      return new AuthError("demasiados intentos, espera unos segundos", 429);
    }
    case 503:
      return new AuthError("el servicio no está disponible, prueba en unos segundos", 503);
    default:
      return new AuthError(`error inesperado (${response.status})`, response.status);
  }
}

/** Lee la carga útil (sin verificar la firma: eso lo hace el hub) de un JWT. */
function tokenClaims(token) {
  try {
    const payload = token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/");
    const bytes = Uint8Array.from(atob(payload), (c) => c.charCodeAt(0));
    const claims = JSON.parse(new TextDecoder().decode(bytes));
    return claims && typeof claims === "object" ? claims : {};
  } catch {
    return {};
  }
}

async function authenticate(fetchFn, path, { username, password }) {
  let response;
  try {
    response = await fetchFn(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    });
  } catch {
    throw new AuthError("no se pudo contactar con el servidor", 0);
  }
  const body = await readJson(response);
  if (!response.ok) throw failure(response, body);

  const expiresAt = Date.parse(body?.expires_at);
  if (typeof body?.token !== "string" || body.token === "" || Number.isNaN(expiresAt)) {
    throw new AuthError("respuesta no válida del servidor", response.status);
  }
  // El login solo devuelve `token` y `expires_at`: el resto sale del token.
  const claims = tokenClaims(body.token);
  const text = (...values) => values.find((v) => typeof v === "string" && v !== "") ?? "";
  return {
    token: body.token,
    expiresAt,
    username: text(body.username, claims.username, username),
    playerId: text(body.player_id, claims.player_id),
  };
}

/**
 * Crea la cuenta y devuelve la sesión.
 * @param {typeof fetch} fetchFn
 * @param {{username: string, password: string}} credentials
 */
export function register(fetchFn, credentials) {
  return authenticate(fetchFn, REGISTER_PATH, credentials);
}

/**
 * Entra con una cuenta existente y devuelve la sesión.
 * @param {typeof fetch} fetchFn
 * @param {{username: string, password: string}} credentials
 */
export function login(fetchFn, credentials) {
  return authenticate(fetchFn, LOGIN_PATH, credentials);
}

/**
 * Guarda la sesión. Solo se persisten cuatro campos, nunca la contraseña.
 * @param {{setItem(k: string, v: string): void}} storage
 * @param {{token: string, expiresAt: number, username: string, playerId: string}} session
 */
export function saveSession(storage, { token, expiresAt, username, playerId }) {
  try {
    storage.setItem(SESSION_KEY, JSON.stringify({ token, expiresAt, username, playerId }));
  } catch {
    // Almacenamiento bloqueado: la sesión dura solo lo que dure la página.
  }
}

/**
 * Borra la sesión guardada.
 * @param {{removeItem(k: string): void}} storage
 */
export function clearSession(storage) {
  try {
    storage.removeItem(SESSION_KEY);
  } catch {
    // Nada que borrar si el almacenamiento está bloqueado.
  }
}

/**
 * Devuelve la sesión guardada, o `null` (y la borra) si falta, está
 * corrupta o caducó.
 * @param {{getItem(k: string): string | null, removeItem(k: string): void}} storage
 * @param {number} now milisegundos desde la época
 */
export function loadSession(storage, now) {
  let raw = null;
  try {
    raw = storage.getItem(SESSION_KEY);
  } catch {
    return null;
  }
  if (raw === null) return null;
  try {
    const s = JSON.parse(raw);
    if (
      typeof s?.token === "string" &&
      s.token !== "" &&
      Number.isFinite(s.expiresAt) &&
      s.expiresAt > now &&
      typeof s.username === "string" &&
      typeof s.playerId === "string"
    ) {
      return { token: s.token, expiresAt: s.expiresAt, username: s.username, playerId: s.playerId };
    }
  } catch {
    // Datos corruptos: se descartan abajo.
  }
  clearSession(storage);
  return null;
}
