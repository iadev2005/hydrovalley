// Página mínima: abre /play, suelta gotas y muestra lo que llega del hub.
// La lógica de los mensajes vive en play.js (probada con node:test).
import {
  buildDropFrame,
  buildPlayUrl,
  describeFrame,
  loadOrCreatePlayerId,
  newUuid,
  parseFrame,
} from "./play.js";

const MAX_LOG_LINES = 100;
const RECONNECT_DELAY_MS = 1000;
const GRID_SIZE = 32;

const $ = (id) => document.getElementById(id);
const playerId = loadOrCreatePlayerId(window.localStorage);
$("player-id").textContent = playerId;

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

function connect() {
  const ws = new WebSocket(buildPlayUrl(window.location, playerId));
  socket = ws;
  $("conn").textContent = "conectando…";

  ws.addEventListener("open", () => {
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
    $("conn").textContent = "desconectado (reintentando)";
    $("drop").disabled = true;
    // TODO: reenviar las acciones pendientes al reconectar (CLAUDE.md, regla 2).
    setTimeout(connect, RECONNECT_DELAY_MS);
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
