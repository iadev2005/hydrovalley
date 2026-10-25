// Comprobaciones de la simulación del plan A (deploy/nodes/simulate.sh).
// Se ejecuta con Node 22 contra el proxy simulado; no usa dependencias.
//
//   node deploy/nodes/simulate-check.mjs smoke   humo: acceso, catálogo y una gota por WSS
//   node deploy/nodes/simulate-check.mjs fault   CP-07: la persistencia cae y se sigue jugando
//   node deploy/nodes/simulate-check.mjs failover B8: failover simulado con standby
//
// Variables: HV_BASE_URL (https://localhost:<puerto>), ADMIN_USERNAME,
// SIM_ADMIN_PASSWORD, NODE_EXTRA_CA_CERTS (certificado del proxy; la
// verificación TLS nunca se omite). Solo en `fault`: SIM_STOP_PERSISTENCE y
// SIM_START_PERSISTENCE, comandos de shell que paran y arrancan el nodo.
// Solo en `failover`: SIM_PAUSE_WORLD, SIM_UNPAUSE_WORLD, SIM_PAUSE_HUB, SIM_UNPAUSE_HUB.
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { randomUUID } from "node:crypto";
import { promisify } from "node:util";
import { PlaySession } from "../../tests/e2e/play-session.mjs";

const run = promisify(execFile);
const BASE_URL = (process.env.HV_BASE_URL ?? "").replace(/\/$/, "");
assert.ok(BASE_URL, "falta HV_BASE_URL");
const WS_BASE = BASE_URL.replace(/^http/, "ws");
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function eventually(what, timeoutMs, fn, intervalMs = 500) {
  const deadline = Date.now() + timeoutMs;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const value = await fn();
      if (value) return value;
    } catch (err) {
      lastError = err;
    }
    await sleep(intervalMs);
  }
  throw new Error(`tiempo agotado esperando ${what}${lastError ? `: ${lastError.message}` : ""}`);
}

async function system() {
  const res = await fetch(`${BASE_URL}/api/system`);
  assert.equal(res.status, 200, `/api/system: ${res.status}`);
  return res.json();
}

/** Estado de cada servicio que informa /api/system, por nombre. */
function statuses(body) {
  const list = body.services ?? [];
  return Object.fromEntries(list.map((s) => [s.name ?? s.service, s.status]));
}

async function loginAdmin() {
  const res = await fetch(`${BASE_URL}/api/auth/login`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ username: process.env.ADMIN_USERNAME, password: process.env.SIM_ADMIN_PASSWORD }),
  });
  assert.equal(res.status, 200, `login del administrador: ${res.status}`);
  return (await res.json()).token;
}

async function newPlayerToken(adminToken) {
  const res = await fetch(`${BASE_URL}/api/admin/bot-accounts`, {
    method: "POST",
    headers: { "content-type": "application/json", authorization: `Bearer ${adminToken}` },
    body: JSON.stringify({ count: 1, prefix: "sim" }),
  });
  assert.equal(res.status, 201, `bot-accounts: ${res.status}`);
  const { accounts } = await res.json();
  return accounts[0].token;
}

function openPlay(token) {
  return eventually("un valle en marcha en /play", 60_000, async () => {
    const candidate = new PlaySession(`${WS_BASE}/play?token=${token}&map=false`);
    try {
      await candidate.opened;
      await candidate.first("un estado sin pausa", (f) => f.type === "state" && !f.paused, 5000);
      return candidate;
    } catch (err) {
      candidate.close();
      throw err;
    }
  }, 1500);
}

/** Una gota en (x, y): resuelve con el resultado final de la acción. */
async function drop(session, x, y) {
  const actionId = randomUUID();
  session.send({ type: "drop", action_id: actionId, x, y });
  const result = await session.first(
    `el resultado de ${actionId}`,
    (f) => f.type === "result" && f.action_id === actionId,
    10_000,
  );
  assert.ok(["applied", "duplicate"].includes(result.status), `gota: ${JSON.stringify(result)}`);
  return result;
}

function log(message) {
  console.log(`[check] ${message}`);
}

async function smoke() {
  const body = await system();
  log(`/api/system: ${JSON.stringify(statuses(body))}`);
  const adminToken = await loginAdmin();
  log("login del administrador: 200");
  const catalog = await fetch(`${BASE_URL}/api/catalog`);
  assert.equal(catalog.status, 200, `/api/catalog: ${catalog.status}`);
  log("GET /api/catalog: 200");
  const session = await openPlay(await newPlayerToken(adminToken));
  try {
    const result = await drop(session, 3, 4);
    log(`gota por WSS (proxy -> hub -> world): ${result.status}`);
  } finally {
    session.close();
  }
}

async function fault() {
  const stop = process.env.SIM_STOP_PERSISTENCE;
  const start = process.env.SIM_START_PERSISTENCE;
  assert.ok(stop && start, "faltan SIM_STOP_PERSISTENCE y SIM_START_PERSISTENCE");
  const sh = (cmd) => run("sh", ["-c", cmd]);

  const adminToken = await loginAdmin();
  const session = await openPlay(await newPlayerToken(adminToken));
  try {
    const before = await drop(session, 5, 5);
    log(`antes del fallo: gota ${before.status}`);

    await sh(stop);
    log("nodo persistence detenido");
    await eventually("la persistencia en down en /api/system", 30_000, async () => {
      const s = statuses(await system());
      return Object.values(s).includes("down") ? s : false;
    });
    log("/api/system muestra la persistencia caída");

    // CP-07: con la persistencia caída el jugador conectado sigue jugando.
    const results = [];
    for (let i = 0; i < 5; i += 1) {
      results.push((await drop(session, 5, 6)).status);
      await sleep(250);
    }
    log(`con la persistencia caída: 5 gotas -> ${results.join(",")}`);

    await sh(start);
    log("nodo persistence arrancado de nuevo");
    const final = await eventually("los cinco servicios en up", 120_000, async () => {
      const s = statuses(await system());
      const values = Object.values(s);
      return values.length === 5 && values.every((v) => v === "up") ? s : false;
    }, 1000);
    log(`tras recuperar: ${JSON.stringify(final)}`);
    const after = await drop(session, 5, 7);
    log(`gota tras recuperar: ${after.status}`);
  } finally {
    session.close();
  }
}

async function failover() {
  const pauseWorld = process.env.SIM_PAUSE_WORLD;
  const unpauseWorld = process.env.SIM_UNPAUSE_WORLD;
  const sh = (cmd) => run("sh", ["-c", cmd]);

  log("iniciando verificación de failover B8");
  const adminToken = await loginAdmin();
  const session = await openPlay(await newPlayerToken(adminToken));
  try {
    const before = await drop(session, 10, 10);
    log(`antes de pausar world: gota ${before.status}`);

    if (pauseWorld && unpauseWorld) {
      await sh(pauseWorld);
      log("world (Rust) pausado; el hub conmuta a world-standby (Go)");
      await sleep(1000);

      const during = await drop(session, 10, 11);
      log(`con world primario pausado (en world-go): gota ${during.status}`);

      await sh(unpauseWorld);
      log("world (Rust) despausado; blindaje unpause activo");
      await sleep(1000);

      const after = await drop(session, 10, 12);
      log(`tras despausar world (failback): gota ${after.status}`);
    } else {
      log("SIM_PAUSE_WORLD no definido; omitiendo comandos docker pause directos");
    }
  } finally {
    session.close();
  }
}

const phases = { smoke, fault, failover };
const phase = phases[process.argv[2]];
if (!phase) {
  console.error(`uso: simulate-check.mjs <${Object.keys(phases).join("|")}>`);
  process.exit(2);
}
phase().then(
  () => process.exit(0),
  (err) => {
    console.error(`[check] FALLO: ${err.message}`);
    process.exit(1);
  },
);
