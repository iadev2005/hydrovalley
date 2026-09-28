// Página mínima: abre /play, suelta gotas y muestra lo que llega del hub.
// La lógica de los mensajes vive en play.js (probada con node:test).
import {
  buildDropFrame,
  buildPlayUrl,
  createReconnectBackoff,
  describeFrame,
  GRID_SIZE,
  loadOrCreatePlayerId,
  newUuid,
  parseFrame,
} from "./play.js";

const MAX_LOG_LINES = 100;

const $ = (id) => document.getElementById(id);
const playerId = loadOrCreatePlayerId(window.localStorage);
$("player-id").textContent = playerId;

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

let socket = null;
const backoff = createReconnectBackoff();

function connect() {
  const ws = new WebSocket(buildPlayUrl(window.location, playerId));
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
  ws.addEventListener("close", () => {
    backoff.closed();
    const delay = backoff.nextDelay();
    $("conn").textContent = `desconectado (reintento en ${(delay / 1000).toFixed(1)} s)`;
    $("drop").disabled = true;
    // TODO: reenviar las acciones pendientes al reconectar (CLAUDE.md, regla 2).
    setTimeout(connect, delay);
  });
}

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

connect();
