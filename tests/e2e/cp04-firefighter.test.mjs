// CP-04: Extinción de incendios por varios bomberos (RF-29, RF-30).
//
// Con un catálogo de prueba con fire.ignition_chance_ppm alto (sin tocar
// catalog/catalog.json, montando catalog.test-fire.json y apuntando
// CATALOG_PATH a él):
//   1. Un jugador siembra una planta con 4 gotas (100 de humedad).
//   2. La casilla sube a césped (etapa 2) y su humedad se reinicia a 0.
//   3. Con ignition_chance_ppm alto (500.000 ppm = 50% por tick), la casilla arde.
//   4. Se corre `bots firefighter` (3 bomberos): detectan el fuego y lo apagan.
//   5. Se comprueba que el fuego se apaga una vez, hay un solo bono y nada se pierde.
//
// Ejecución:
//   sudo CATALOG_PATH=/app/catalog/catalog.test-fire.json E2E_SKIP_FAULTS=1 HV_HTTP_PORT=8085 tests/e2e/run.sh tests/e2e/cp04-firefighter.test.mjs

import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { randomUUID } from "node:crypto";
import { promisify } from "node:util";
import { PlaySession } from "./play-session.mjs";

const run = promisify(execFile);
const BASE_URL = (process.env.HV_BASE_URL ?? "http://localhost:8080").replace(/\/$/, "");
const WS_BASE = BASE_URL.replace(/^http/, "ws");
const READY_TIMEOUT_MS = Number(process.env.E2E_READY_TIMEOUT_S ?? 180) * 1000;
const PASSWORD = "e2e-password-1";

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function eventually(what, timeoutMs, fn, intervalMs = 1000) {
  const deadline = Date.now() + timeoutMs;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const value = await fn();
      if (value !== undefined && value !== false) return value;
    } catch (err) {
      lastError = err;
    }
    await sleep(intervalMs);
  }
  throw new Error(`timed out waiting for ${what}${lastError ? `: ${lastError.message}` : ""}`);
}

async function persistenceHealthy() {
  const { stdout } = await run("docker", ["compose", "ps", "--format", "{{.Health}}", "persistence"]);
  return stdout.trim() === "healthy";
}

async function waitForHealthyStack() {
  await eventually("GET /api/health through nginx", READY_TIMEOUT_MS, async () => {
    const res = await fetch(`${BASE_URL}/api/health`);
    return res.ok && (await res.json()).status === "ok";
  });
}

async function registerPlayer() {
  await eventually("persistence healthy", 120_000, persistenceHealthy);
  const username = `e2e-${randomUUID().slice(0, 8)}`;
  const res = await fetch(`${BASE_URL}/api/auth/register`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ username, password: PASSWORD }),
  });
  assert.equal(res.status, 201, `register ${username}: ${res.status}`);
  const body = await res.json();
  assert.equal(typeof body.token, "string");
  return { ...body, username };
}

function openReadySession(token, { map }) {
  return eventually("a running valley on /play", READY_TIMEOUT_MS, async () => {
    const candidate = new PlaySession(`${WS_BASE}/play?token=${token}&map=${map}`);
    try {
      await candidate.opened;
      await candidate.first("a state frame without pause", (f) => f.type === "state" && !f.paused, 3000);
      return candidate;
    } catch (err) {
      candidate.close();
      throw err;
    }
  }, 2000);
}

async function dropAndWait(player, x, y) {
  const actionId = randomUUID();
  player.send({ type: "drop", action_id: actionId, x, y });
  const result = await player.first(
    `the result of ${actionId}`,
    (f) => f.type === "result" && f.action_id === actionId,
    10_000,
  );
  assert.equal(result.status, "applied", JSON.stringify(result));
}

const tileIndex = (state, x, y) => y * state.width + x;

describe("firefighter bots against real fires (CP-04)", () => {
  const adminUsername = process.env.ADMIN_USERNAME;
  const adminPassword = process.env.E2E_ADMIN_PASSWORD;

  before(async () => {
    assert.ok(
      adminUsername && adminPassword,
      "falta ADMIN_USERNAME o E2E_ADMIN_PASSWORD: tests/e2e/run.sh las genera",
    );
    await waitForHealthyStack();
    await eventually("persistence healthy", 120_000, persistenceHealthy);
  });

  it("plants, catches fire, and firefighter bots extinguish it with single bonus", { timeout: 120_000 }, async () => {
    // 1. Sembrar una planta: un jugador riega la casilla (10, 10) con 4 gotas.
    const planter = await registerPlayer();
    const session = await openReadySession(planter.token, { map: true });

    const targetX = 10;
    const targetY = 10;

    // 4 gotas = 4 × 25 = 100 de humedad, lo necesario para subir a césped (etapa 2).
    // Al alcanzar 100, la humedad se consume y queda en 0.
    for (let i = 0; i < 4; i++) {
      await dropAndWait(session, targetX, targetY);
    }

    // 2. Esperar a que la planta suba y, al quedar en humedad 0 con alta probabilidad, arda.
    console.log(`esperando a que la casilla (${targetX}, ${targetY}) arda...`);
    const fireState = await session.waitFor(
      "the tile to ignite",
      (f) => f.type === "state" && f.fires?.some((fire) => fire.x === targetX && fire.y === targetY),
      20_000,
    );
    assert.ok(fireState.fires?.some((f) => f.x === targetX && f.y === targetY), "la casilla no ardió");
    console.log(`¡fuego encendido en (${targetX}, ${targetY}) en el tick ${fireState.tick}!`);

    // Cerramos la sesión del sembrador para dejar actuar a los bomberos.
    session.close();

    // 3. Correr `bots firefighter` para apagar el incendio activo.
    const { stdout } = await run(
      "docker",
      [
        "compose",
        "--profile",
        "bots",
        "run",
        "--rm",
        "-T",
        "bots",
        "firefighter",
      ],
      {
        env: {
          ...process.env,
          HV_ADMIN_USERNAME: adminUsername,
          HV_ADMIN_PASSWORD: adminPassword,
          BOTS_COUNT: "3",
          BOTS_DURATION: "20s",
        },
        timeout: 60_000,
      },
    );

    console.log(stdout);

    // 4. Comprobaciones de CP-04:
    // Resultado CUMPLE
    assert.match(stdout, /^Resultado: CUMPLE$/m, stdout);

    // El fuego se apaga una vez (1 incendio visto y 1 apagado)
    assert.match(stdout, /Incendios vistos: 1 · apagados: 1 · consumidos: 0 · sin apagar: 0/);

    // Bono único de 5 hojas asignado a un solo bot
    assert.match(stdout, /Bono único \(indicativo\): sin señales de repetirse/);

    console.log("CP-04: CUMPLE (el fuego se apagó una vez, hubo un solo bono y nada se perdió)");
  });
});
