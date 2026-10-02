// Página mínima: acceso (registro y entrada), abre /play con el token, dibuja
// el valle, suelta gotas, coloca instalaciones, compra mejoras y, con rol de
// administrador, activa el modo demo o reinicia el valle. La lógica vive en
// auth.js, play.js y valley.js (probadas con node:test); aquí solo hay DOM y
// WebSocket.
import {
  AuthError,
  clearSession,
  loadSession,
  login,
  register,
  retryDelayMs,
  saveSession,
} from "./auth.js";
import {
  buildBuyUpgradeFrame,
  buildDropFrame,
  buildPlaceInstallationFrame,
  buildPlayUrl,
  buildResetValleyFrame,
  buildSetDemoModeFrame,
  createReconnectBackoff,
  decideOnClose,
  describeFrame,
  GRID_SIZE,
  INSTALLATIONS,
  newUuid,
  parseFrame,
  UPGRADES,
} from "./play.js";
import {
  buildTiles,
  createPendingActions,
  describeActionResult,
  isAdminRole,
  playerPanel,
  selectionView,
  tileAt,
  tokenRole,
} from "./valley.js";

const MAX_LOG_LINES = 100;

const $ = (id) => document.getElementById(id);

// El tamaño del valle se define una sola vez, en play.js; los límites reales
// los decide el servidor (32 × 32 según D-01).
const CELL = 16; // píxeles por casilla: 32 × 16 = 512
const STAGE_COLOURS = ["#8d6e4a", "#9ccc65", "#558b2f", "#f06292", "#1b5e20"];
const FIRE_COLOUR = "#e53935";
const INSTALLATION_LETTER = { sprinkler: "A", drip: "G", treatment_plant: "P" };

/** @type {{x: number, y: number} | null} */
let selected = null;
let lastState = null;
let admin = false;
let pending = createPendingActions(); // por sesión: se descarta al salir o entrar de nuevo

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

function setSessionRole(session) {
  admin = isAdminRole(tokenRole(session.token));
  refreshControls();
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
  refreshControls();
}

const isOpen = () => socket !== null && socket.readyState === WebSocket.OPEN;

/** Activa o desactiva los botones según la conexión, la casilla y el rol. */
function refreshControls() {
  const open = isOpen();
  const tileActions = open && selected !== null;
  for (const id of ["drop", "place-sprinkler", "place-drip", "place-treatment_plant"]) {
    $(id).disabled = !tileActions;
  }
  const panel = playerPanel(lastState?.player ?? null);
  $("buy-bigger_drop").disabled = !open || panel.owned.bigger_drop;
  $("buy-bigger_tank").disabled = !open || panel.owned.bigger_tank;
  $("admin-panel").hidden = !admin;
  $("demo-toggle").disabled = !open;
  $("reset").disabled = !open;
  $("pending").textContent = String(pending.size);
}

function drawValley() {
  const canvas = $("valley");
  const ctx = canvas.getContext("2d");
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  ctx.font = `${CELL - 4}px sans-serif`;
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  const tiles = buildTiles(lastState);
  for (const tile of tiles) {
    ctx.fillStyle = STAGE_COLOURS[tile.stage - 1] ?? STAGE_COLOURS[0];
    ctx.fillRect(tile.x * CELL, tile.y * CELL, CELL, CELL);
    if (tile.fire) {
      ctx.fillStyle = FIRE_COLOUR;
      ctx.fillRect(tile.x * CELL + 3, tile.y * CELL + 3, CELL - 6, CELL - 6);
    }
    if (tile.installation) {
      ctx.fillStyle = "#0d47a1";
      ctx.fillText(
        INSTALLATION_LETTER[tile.installation.kind] ?? "?",
        tile.x * CELL + CELL / 2,
        tile.y * CELL + CELL / 2 + 1,
      );
    }
  }
  ctx.strokeStyle = "rgba(0,0,0,0.12)";
  ctx.lineWidth = 1;
  for (let i = 0; i <= GRID_SIZE; i += 1) {
    ctx.beginPath();
    ctx.moveTo(i * CELL + 0.5, 0);
    ctx.lineTo(i * CELL + 0.5, GRID_SIZE * CELL);
    ctx.moveTo(0, i * CELL + 0.5);
    ctx.lineTo(GRID_SIZE * CELL, i * CELL + 0.5);
    ctx.stroke();
  }
  if (selected) {
    ctx.strokeStyle = "#ffd600";
    ctx.lineWidth = 2;
    ctx.strokeRect(selected.x * CELL + 1, selected.y * CELL + 1, CELL - 2, CELL - 2);
  }
  return tiles;
}

function renderState() {
  drawValley();
  const panel = playerPanel(lastState?.player ?? null);
  $("water").textContent = panel.water;
  $("water-bar").value = panel.waterRatio;
  $("leaves").textContent = panel.leaves;
  for (const [kind, price] of Object.entries(panel.prices)) $(`price-${kind}`).textContent = price;
  $("demo-toggle").textContent = lastState?.demo_mode ? "Desactivar modo demo" : "Activar modo demo";
  const view = selectionView(lastState, selected);
  $("selected").textContent = view.label;
  $("tile-info").textContent = view.info;
  refreshControls();
}

function setActionMessage(text, ok) {
  const el = $("action-message");
  el.textContent = text;
  el.className = ok ? "meta ok" : "meta error";
}

/** Envía un comando y lo deja pendiente hasta su resultado final. */
function sendAction(label, build) {
  if (!isOpen()) return;
  const actionId = newUuid();
  let frame;
  try {
    frame = build(actionId);
  } catch (err) {
    log(`no se envió: ${err.message}`);
    return;
  }
  pending.add(actionId, frame, label);
  socket.send(frame);
  log(`${label} ${actionId} enviado (pendiente)`);
  setActionMessage(`${label}: pendiente…`, true);
  refreshControls();
}

function connect(session) {
  // El token no se escribe en la consola ni en el registro de la página.
  const ws = new WebSocket(buildPlayUrl(window.location, session.token));
  socket = ws;
  $("conn").textContent = "conectando…";

  ws.addEventListener("open", () => {
    backoff.opened();
    $("conn").textContent = "conectado";
    // Regla 2 de CLAUDE.md: al reconectar solo se reenvían las pendientes. El
    // hub ignora las que siguen en cola y responde `duplicate` a las aplicadas.
    const plan = pending.planReconnect();
    for (const entry of plan.resend) ws.send(entry.frame);
    const notes = [
      ...plan.expired.map((e) => `${e.label}: expiró`),
      ...plan.adminDropped.map((e) => `${e.label}: no se reenvía sola tras reconectar; vuelve a intentarlo`),
    ];
    if (notes.length > 0) setActionMessage(notes.join(" · "), false);
    refreshControls();
  });
  ws.addEventListener("message", (event) => {
    const frame = parseFrame(String(event.data));
    const line = describeFrame(frame);
    if (frame.type === "state") {
      $("state").textContent = line;
      lastState = frame;
      renderState();
    } else if (frame.type === "result") {
      const entry = pending.resolve(frame);
      if (entry) {
        const outcome = describeActionResult(frame);
        setActionMessage(`${entry.label}: ${outcome.text}`, outcome.ok);
        refreshControls();
      }
    } else if (frame.type === "error" && pending.settle(frame.action_id)) {
      setActionMessage(`mensaje rechazado por el hub (${frame.code})`, false);
      refreshControls();
    }
    log(line);
  });
  ws.addEventListener("close", (event) => {
    const action = decideOnClose({
      code: event.code,
      current: socket === ws, // «Salir» o una sesión nueva ya soltaron este socket
      expiresAt: session.expiresAt,
      now: Date.now(),
    });
    if (action === "ignore") return;
    backoff.closed();
    refreshControls();
    if (action !== "reconnect") {
      // Token inválido o sesión vencida: no se reintenta, hay que volver a entrar.
      socket = null;
      clearSession(window.localStorage);
      $("conn").textContent = "desconectado";
      showAccessPanel("la sesión caducó, vuelve a entrar");
      return;
    }
    const delay = backoff.nextDelay();
    $("conn").textContent = `desconectado (reintento en ${(delay / 1000).toFixed(1)} s)`;
    reconnectTimer = setTimeout(() => connect(session), delay);
  });
}

function start(session) {
  saveSession(window.localStorage, session);
  showSession(session);
  setSessionRole(session);
  disconnect();
  pending = createPendingActions();
  backoff = createReconnectBackoff();
  connect(session);
}

let accessTimer = null;

function setAccessDisabled(disabled) {
  $("register").disabled = disabled;
  $("login").disabled = disabled;
}

async function submitAccess(action) {
  if ($("login").disabled) return; // solicitud en curso o espera tras un 429
  const username = $("username").value.trim();
  const password = $("password").value;
  if (username === "" || password === "") {
    showError("escribe el usuario y la contraseña");
    return;
  }
  setAccessDisabled(true);
  let waitMs = 0;
  try {
    start(await action(window.fetch.bind(window), { username, password }));
    $("password").value = "";
  } catch (err) {
    showError(err instanceof AuthError ? err.message : "no se pudo completar la solicitud");
    waitMs = retryDelayMs(err);
  }
  // Tras un 429 los botones siguen desactivados hasta que pase el Retry-After.
  if (waitMs > 0) {
    clearTimeout(accessTimer);
    accessTimer = setTimeout(() => setAccessDisabled(false), waitMs);
  } else {
    setAccessDisabled(false);
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
  admin = false;
  pending = createPendingActions();
  lastState = null;
  selected = null;
  renderState();
  showAccessPanel();
});

$("valley").addEventListener("click", (event) => {
  const rect = $("valley").getBoundingClientRect();
  const scale = $("valley").width / rect.width; // el CSS puede reducir el lienzo
  selected =
    tileAt((event.clientX - rect.left) * scale, (event.clientY - rect.top) * scale, CELL, GRID_SIZE) ??
    selected;
  renderState();
});

$("drop").addEventListener("click", () => {
  if (!selected) return;
  const { x, y } = selected;
  sendAction(`Soltar gota (${x}, ${y})`, (actionId) => buildDropFrame({ x, y, actionId }));
});
for (const installation of INSTALLATIONS) {
  $(`place-${installation}`).addEventListener("click", () => {
    if (!selected) return;
    const { x, y } = selected;
    sendAction(`Colocar ${installation} (${x}, ${y})`, (actionId) =>
      buildPlaceInstallationFrame({ installation, x, y, actionId }),
    );
  });
}
for (const upgrade of UPGRADES) {
  $(`buy-${upgrade}`).addEventListener("click", () =>
    sendAction(`Comprar ${upgrade}`, (actionId) => buildBuyUpgradeFrame({ upgrade, actionId })),
  );
}
$("demo-toggle").addEventListener("click", () => {
  const enabled = !(lastState?.demo_mode ?? false);
  sendAction(enabled ? "Activar modo demo" : "Desactivar modo demo", (actionId) =>
    buildSetDemoModeFrame({ enabled, actionId }),
  );
});
// Confirmación dentro de la página (no window.confirm) antes de reiniciar.
$("reset").addEventListener("click", () => {
  $("reset-confirm").hidden = false;
});
$("reset-no").addEventListener("click", () => {
  $("reset-confirm").hidden = true;
});
$("reset-yes").addEventListener("click", () => {
  $("reset-confirm").hidden = true;
  sendAction("Reiniciar valle", (actionId) => buildResetValleyFrame({ actionId }));
});

const saved = loadSession(window.localStorage, Date.now());
if (saved) {
  showSession(saved);
  setSessionRole(saved);
  connect(saved);
} else {
  showAccessPanel();
}
