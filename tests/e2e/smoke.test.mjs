// CP-01-lite: el esqueleto camina de punta a punta.
//
// Contra un stack ya levantado (run.sh lo levanta y lo baja): un jugador se
// registra por nginx y la API, abre /play con el token que recibe, y su gota
// llega al hub, pasa por Step en Rust y vuelve como "applied"; con un token
// inválido /play se cierra con el código 4401; y el mundo llega a PostgreSQL
// por la persistencia en el tick 50.
//
// Variables:
//   HV_BASE_URL          raíz de nginx (por defecto http://localhost:8080)
//   E2E_READY_TIMEOUT_S  espera máxima a que el stack esté listo (180)
// La sesión de /play está en play-session.mjs, con pruebas propias que no
// necesitan el stack (play-session.test.mjs).
// La consulta a PostgreSQL usa `docker compose exec`, con COMPOSE_PROJECT_NAME,
// COMPOSE_FILE y COMPOSE_ENV_FILES definidos por run.sh.
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
// Instantánea del mundo cada 50 ticks (`tick % 50 == 0`): regla 8 de
// CLAUDE.md y src/world/README.md ("guarda una cada 50 ticks").
const SNAPSHOT_EVERY = 50n;

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

async function snapshotRows() {
  const { stdout } = await run("docker", [
    "compose",
    "exec",
    "-T",
    "postgres",
    "sh",
    "-c",
    'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tA -c "SELECT count(*) || \',\' || coalesce(max(tick), -1) FROM snapshots"',
  ]);
  const [count, maxTick] = stdout.trim().split(",").map(Number);
  return { count, maxTick };
}

// Los servicios arrancan sin esperar a sus dependencias (RNF-09), así que la
// API puede estar lista antes de que la persistencia conecte con PostgreSQL y
// responder 503. No se reintenta el registro, porque tiene un límite de
// intentos por IP: se espera a que el healthcheck de la persistencia (SERVING
// solo con la base de datos conectada) la marque como sana.
async function persistenceHealthy() {
  const { stdout } = await run("docker", ["compose", "ps", "--format", "{{.Health}}", "persistence"]);
  return stdout.trim() === "healthy";
}

const PASSWORD = "e2e-password-1";

// Registra una cuenta nueva (cada ejecución usa un nombre distinto) y devuelve
// lo que responde la API: el token firmado y el player_id.
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

describe("walking skeleton (CP-01-lite)", () => {
  let account;
  let session;

  before(async () => {
    await eventually("GET /api/health through nginx", READY_TIMEOUT_MS, async () => {
      const res = await fetch(`${BASE_URL}/api/health`);
      return res.ok && (await res.json()).status === "ok";
    });
  });

  after(() => session?.close());

  it("serves the static page, the Swagger page and a 503 for /chat", async () => {
    const page = await fetch(`${BASE_URL}/`);
    assert.equal(page.status, 200);
    assert.match(await page.text(), /Soltar gota/);

    const docs = await fetch(`${BASE_URL}/api/docs/`);
    assert.equal(docs.status, 200);
    assert.match(docs.headers.get("content-type") ?? "", /text\/html/);

    const chat = await fetch(`${BASE_URL}/chat`);
    assert.equal(chat.status, 503);
  });

  it("closes /play with 4401 when the token is missing or not valid", async () => {
    const { token } = await registerPlayer();
    const tampered = `${token.slice(0, -20)}${token.at(-20) === "A" ? "B" : "A"}${token.slice(-19)}`;
    const cases = {
      "no token": "map=false",
      "garbage token": "token=abc.def.ghi&map=false",
      "tampered signature": `token=${tampered}&map=false`,
    };
    for (const [name, query] of Object.entries(cases)) {
      const rejected = new PlaySession(`${WS_BASE}/play?${query}`);
      const closed = await Promise.race([
        rejected.closed,
        sleep(5000).then(() => {
          rejected.close();
          return { code: "timeout" };
        }),
      ]);
      assert.equal(closed.code, 4401, `${name}: ${JSON.stringify(closed)}`);
      assert.equal(rejected.frames.length, 0, `${name}: no frame may reach an unauthenticated socket`);
    }
  });

  it("applies a drop sent through nginx and the hub", async () => {
    account = await registerPlayer();
    // El hub está listo cuando difunde un estado sin pausa: Step ya responde,
    // o sea, el simulador cargó su instantánea desde la persistencia.
    session = await eventually("a running valley on /play", READY_TIMEOUT_MS, async () => {
      const candidate = new PlaySession(`${WS_BASE}/play?token=${account.token}&map=false`);
      try {
        await candidate.opened;
        await candidate.first("a state frame without pause", (f) => f.type === "state" && !f.paused, 3000);
        return candidate;
      } catch (err) {
        candidate.close();
        throw err;
      }
    }, 2000);

    const actionId = randomUUID();
    session.send({ type: "drop", action_id: actionId, x: 3, y: 4 });
    const result = await session.first(
      `the result of ${actionId}`,
      (f) => f.type === "result" && f.action_id === actionId,
      10_000,
    );
    assert.equal(result.status, "applied", JSON.stringify(result));
    console.log(`drop ${actionId} -> ${result.status}`);

    // La gota cuesta 1 L de los 20 iniciales (D-01). Solo cuenta un estado
    // que llegue después del resultado: los anteriores aún tienen 20 L.
    const state = await session.waitFor(
      "a state with the player",
      (f) => f.type === "state" && f.player !== undefined,
      5000,
    );
    assert.equal(state.player.water, "19");
    assert.equal(state.player.tank_capacity, "20");
    assert.ok(account.player_id, "the API returns the player_id of the token");
  });

  it("logs in with the account just registered and the token opens /play", async () => {
    const { username, player_id: playerId } = await registerPlayer();
    const res = await fetch(`${BASE_URL}/api/auth/login`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ username, password: PASSWORD }),
    });
    assert.equal(res.status, 200, `login ${username}: ${res.status}`);
    const body = await res.json();
    assert.equal(typeof body.token, "string");
    assert.ok(!Number.isNaN(Date.parse(body.expires_at)), "expires_at is an ISO 8601 date");

    const wrong = await fetch(`${BASE_URL}/api/auth/login`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ username, password: "not-the-password" }),
    });
    assert.equal(wrong.status, 401);

    const login = new PlaySession(`${WS_BASE}/play?token=${body.token}&map=false`);
    try {
      await login.opened;
      const state = await login.first("a state frame for the logged-in player", (f) => f.type === "state", 15_000);
      assert.ok(state, "the login token is accepted by /play");
      assert.ok(playerId, "register returned the player_id the login token carries");
    } finally {
      login.close();
    }
  });

  it("answers forbidden to set_demo_mode from a non-admin player (RF-31)", async () => {
    const { token } = await registerPlayer();
    const player = new PlaySession(`${WS_BASE}/play?token=${token}&map=false`);
    try {
      await player.opened;
      const actionId = randomUUID();
      player.send({ type: "set_demo_mode", action_id: actionId, enabled: true });
      const result = await player.first(
        `the result of ${actionId}`,
        (f) => f.type === "result" && f.action_id === actionId,
        10_000,
      );
      assert.equal(result.status, "forbidden", JSON.stringify(result));
    } finally {
      player.close();
    }
  });

  it("snapshots the world to PostgreSQL through the persistence", async () => {
    assert.ok(session, "needs the /play session of the previous test");
    const state = await session.first(
      `a state with tick >= ${SNAPSHOT_EVERY}`,
      (f) => f.type === "state" && BigInt(f.tick ?? "0") >= SNAPSHOT_EVERY,
      30_000,
    );
    const rows = await eventually("a row in snapshots", 15_000, async () => {
      const r = await snapshotRows();
      return r.count >= 1 ? r : undefined;
    });
    assert.ok(rows.maxTick >= Number(SNAPSHOT_EVERY), JSON.stringify(rows));
    assert.equal(rows.maxTick % Number(SNAPSHOT_EVERY), 0, JSON.stringify(rows));
    console.log(`tick ${state.tick}: ${rows.count} snapshot row(s), latest tick ${rows.maxTick}`);
  });
});
