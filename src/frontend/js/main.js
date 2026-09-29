// Página mínima: acceso (registro y entrada), abre /play con el token,
// suelta gotas y muestra lo que llega del hub. La lógica vive en auth.js y
// play.js (probadas con node:test); aquí solo hay DOM y WebSocket.
import { AuthError, clearSession, loadSession, login, register, saveSession } from "./auth.js";
import {
  buildDropFrame,
  buildPlayUrl,
  createReconnectBackoff,
  describeFrame,
  GRID_SIZE,
  newUuid,
  parseFrame,
  shouldReconnect,
} from "./play.js";

const MAX_LOG_LINES = 100;

const $ = (id) => document.getElementById(id);

// El tamaño del valle se define una sola vez, en play.js; los límites reales
// los decide el servidor (32 × 32 según D-01).
for (const id of ["x", "y"]) $(id).max = String(GRID_SIZE - 1);
const randomCoord = () => Math.floor(Math.random() * GRID_SIZE);
$("x").value = String(randomCoord());
$("y").value = String(randomCoord());

function log(line) {
  const item = document.createElement("li");
  item.textContent = `${new Date().toISOString().slice(11, 19)} ${line}`;
  $("log").prepend(item);
  while ($("log").childElementCount > MAX_LOG_LINES) $("log").lastElementChild.remove();
}

/** Conexión activa y su espera de reconexión; `null` si no hay sesión. */
let socket = null;
let reconnectTimer = null;
let backoff = createReconnectBackoff();

function showError(message) {
  $("auth-error").textContent = message;
  $("auth-error").hidden = message === "";
}

function showAccessPanel(message = "") {
  $("access-panel").hidden = false;
  $("session-panel").hidden = true;
  $("password").value = "";
  showError(message);
}

function showSession(session) {
  $("access-panel").hidden = true;
  $("session-panel").hidden = false;
  $("session-user").textContent = session.username;
  showError("");
}

/** Cierra la conexión sin que se reintente. */
function disconnect() {
  clearTimeout(reconnectTimer);
  reconnectTimer = null;
  const ws = socket;
  socket = null;
  if (ws) ws.close();
  $("conn").textContent = "desconectado";
  $("drop").disabled = true;
}

function connect(session) {
  // El token no se escribe en la consola ni en el registro de la página.
  const ws = new WebSocket(buildPlayUrl(window.location, session.token));
  socket = ws;
  $("conn").textContent = "conectando…";

  ws.addEventListener("open", () => {
    backoff.opened();
    $("conn").textContent = "conectado";
    $("drop").disabled = false;
  });
  ws.addEventListener("message", (event) => {
    const frame = parseFrame(String(event.data));
    const line = describeFrame(frame);
    if (frame.type === "state") $("state").textContent = line;
    log(line);
  });
  ws.addEventListener("close", (event) => {
    if (socket !== ws) return; // cierre provocado por «Salir» o por un 4401 ya tratado
    backoff.closed();
    $("drop").disabled = true;
    if (!shouldReconnect(event.code)) {
      // Token inválido o caducado: no se reintenta, hay que volver a entrar.
      socket = null;
      clearSession(window.localStorage);
      $("conn").textContent = "desconectado";
      showAccessPanel("la sesión caducó, vuelve a entrar");
      return;
    }
    const delay = backoff.nextDelay();
    $("conn").textContent = `desconectado (reintento en ${(delay / 1000).toFixed(1)} s)`;
    // TODO: reenviar las acciones pendientes al reconectar (CLAUDE.md, regla 2).
    reconnectTimer = setTimeout(() => connect(session), delay);
  });
}

function start(session) {
  saveSession(window.localStorage, session);
  showSession(session);
  disconnect();
  backoff = createReconnectBackoff();
  connect(session);
}

async function submitAccess(action) {
  const username = $("username").value.trim();
  const password = $("password").value;
  if (username === "" || password === "") {
    showError("escribe el usuario y la contraseña");
    return;
  }
  $("register").disabled = true;
  $("login").disabled = true;
  try {
    start(await action(window.fetch.bind(window), { username, password }));
    $("password").value = "";
  } catch (err) {
    showError(err instanceof AuthError ? err.message : "no se pudo completar la solicitud");
  } finally {
    $("register").disabled = false;
    $("login").disabled = false;
  }
}

$("auth-form").addEventListener("submit", (event) => {
  event.preventDefault();
  submitAccess(login);
});
$("register").addEventListener("click", () => submitAccess(register));
$("logout").addEventListener("click", () => {
  disconnect();
  clearSession(window.localStorage);
  showAccessPanel();
});

$("drop-form").addEventListener("submit", (event) => {
  event.preventDefault();
  if (!socket || socket.readyState !== WebSocket.OPEN) return;
  const actionId = newUuid();
  try {
    socket.send(buildDropFrame({ x: Number($("x").value), y: Number($("y").value), actionId }));
    log(`drop ${actionId} enviado (pendiente)`);
  } catch (err) {
    log(`no se envió: ${err.message}`);
  }
});

const saved = loadSession(window.localStorage, Date.now());
if (saved) {
  showSession(saved);
  connect(saved);
} else {
  showAccessPanel();
}
