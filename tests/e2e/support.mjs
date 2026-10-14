// Utilidades compartidas por los casos CP-01 y CP-04: esperas, acceso del
// administrador efímero (que crea run.sh), cuentas de jugador vía
// POST /api/admin/bot-accounts y sesiones de /play.
//
// Los jugadores se crean con la API de administración y no con
// /api/auth/register: el registro público admite 5 cuentas por IP cada 10
// minutos y smoke.test.mjs ya consume casi todo ese cupo.
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { PlaySession } from "./play-session.mjs";

const run = promisify(execFile);
export const BASE_URL = (process.env.HV_BASE_URL ?? "http://localhost:8080").replace(/\/$/, "");
export const WS_BASE = BASE_URL.replace(/^http/, "ws");
export const READY_TIMEOUT_MS = Number(process.env.E2E_READY_TIMEOUT_S ?? 180) * 1000;

export const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

export async function eventually(what, timeoutMs, fn, intervalMs = 1000) {
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

/** Espera a que la API responda por nginx y la persistencia esté sana. */
export async function waitForHealthyStack() {
  await eventually("GET /api/health through nginx", READY_TIMEOUT_MS, async () => {
    const res = await fetch(`${BASE_URL}/api/health`);
    return res.ok && (await res.json()).status === "ok";
  });
  await eventually("persistence healthy", 120_000, persistenceHealthy);
}

/** Credenciales del administrador efímero de run.sh (falla si faltan). */
export function adminCredentials() {
  const username = process.env.ADMIN_USERNAME;
  const password = process.env.E2E_ADMIN_PASSWORD;
  assert.ok(username && password, "falta ADMIN_USERNAME o E2E_ADMIN_PASSWORD: tests/e2e/run.sh las genera");
  return { username, password };
}

export async function loginAdmin() {
  const res = await fetch(`${BASE_URL}/api/auth/login`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(adminCredentials()),
  });
  assert.equal(res.status, 200, `admin login: ${res.status}`);
  return (await res.json()).token;
}

/** Crea `count` jugadores con POST /api/admin/bot-accounts (no usa el registro público). */
export async function provisionPlayers(adminToken, count, prefix = "e2e") {
  const res = await fetch(`${BASE_URL}/api/admin/bot-accounts`, {
    method: "POST",
    headers: { "content-type": "application/json", authorization: `Bearer ${adminToken}` },
    body: JSON.stringify({ count, prefix }),
  });
  assert.equal(res.status, 201, `bot-accounts: ${res.status}`);
  const { accounts } = await res.json();
  assert.equal(accounts.length, count);
  return accounts;
}

export function openReadySession(token, { map }) {
  return eventually(
    "a running valley on /play",
    READY_TIMEOUT_MS,
    async () => {
      const candidate = new PlaySession(`${WS_BASE}/play?token=${token}&map=${map}`);
      try {
        await candidate.opened;
        await candidate.first("a state frame without pause", (f) => f.type === "state" && !f.paused, 3000);
        return candidate;
      } catch (err) {
        candidate.close();
        throw err;
      }
    },
    2000,
  );
}
