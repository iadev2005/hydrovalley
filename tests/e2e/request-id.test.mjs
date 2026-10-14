// RNF-07: un identificador de petición permite seguir una petición de punta a
// punta en los registros de nginx, la API, la persistencia y el simulador.
//
// Contra el stack que levanta run.sh: se envía `X-Request-Id` conocido a
// `POST /api/auth/login` (nginx → API → persistencia) y a `GET /api/ranking`
// (nginx → API → simulador); se comprueba que la respuesta lo devuelve y que
// `docker compose logs` lo trae en todos los servicios del camino. También se
// comprueba que, sin cabecera o con una inválida, nginx genera uno, y que el
// id inválido (con saltos de línea o demasiado largo) no llega a los registros.
//
// Usa el administrador efímero de run.sh (ADMIN_USERNAME, E2E_ADMIN_PASSWORD):
// una sola entrada, para no gastar el límite de registros por IP (5 por 10
// minutos, src/api/src/limits.ts) que usan los demás casos.
import { describe, it, before } from "node:test";
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { randomUUID } from "node:crypto";
import { promisify } from "node:util";

const run = promisify(execFile);
const BASE_URL = (process.env.HV_BASE_URL ?? "https://localhost:8080").replace(/\/$/, "");
const READY_TIMEOUT_MS = Number(process.env.E2E_READY_TIMEOUT_S ?? 180) * 1000;
const HEADER = "x-request-id";

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function eventually(what, timeoutMs, fn, intervalMs = 500) {
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

// Líneas de `docker compose logs` de los servicios dados que contienen `id`.
async function logLines(services, id) {
  const { stdout } = await run("docker", ["compose", "logs", "--no-color", ...services], {
    maxBuffer: 64 * 1024 * 1024,
  });
  return stdout.split("\n").filter((line) => line.includes(id));
}

// Espera a que cada servicio tenga al menos una línea con `id` y las devuelve.
async function linesPerService(services, id) {
  return eventually(`the id ${id} in the logs of ${services.join(", ")}`, 15_000, async () => {
    const found = {};
    for (const service of services) {
      const lines = await logLines([service], id);
      if (lines.length === 0) return false;
      found[service] = lines;
    }
    return found;
  });
}

async function login(headers) {
  return fetch(`${BASE_URL}/api/auth/login`, {
    method: "POST",
    headers: { "content-type": "application/json", ...headers },
    body: JSON.stringify({
      username: process.env.ADMIN_USERNAME,
      password: process.env.E2E_ADMIN_PASSWORD,
    }),
  });
}

describe("identificador de petición de punta a punta (RNF-07)", () => {
  let token;

  before(async () => {
    assert.ok(process.env.ADMIN_USERNAME && process.env.E2E_ADMIN_PASSWORD, "run.sh define el administrador efímero");
    await eventually("GET /api/health through nginx", READY_TIMEOUT_MS, async () => {
      const res = await fetch(`${BASE_URL}/api/health`);
      return res.ok;
    });
    await eventually("persistence healthy", 120_000, async () => {
      const { stdout } = await run("docker", ["compose", "ps", "--format", "{{.Health}}", "persistence"]);
      return stdout.trim() === "healthy";
    });
  });

  it("follows a login through nginx, the API and persistence", async () => {
    const id = randomUUID();
    const res = await login({ "X-Request-Id": id });
    assert.equal(res.status, 200);
    assert.equal(res.headers.get(HEADER), id, "la respuesta devuelve el mismo id");
    token = (await res.json()).token;

    const found = await linesPerService(["nginx", "api", "persistence"], id);
    // El ejemplo de la traza, para el informe.
    for (const [service, lines] of Object.entries(found)) console.log(`# ${service}: ${lines[0].trim()}`);
    assert.match(found.nginx[0], new RegExp(`"POST /api/auth/login HTTP/[\\d.]+" 200 .* rid=${id}`));
    assert.ok(found.api.some((l) => l.includes(`"reqId":"${id}"`)), "la API lo lleva en reqId");
    assert.ok(
      found.persistence.some((l) => l.includes(`request_id=${id}`) && l.includes("AccountService/GetAccount")),
      "persistencia lo lleva en la línea de GetAccount",
    );
  });

  it("follows a ranking request through nginx, the API and the simulator", async () => {
    assert.ok(token, "depende del acceso anterior");
    const id = randomUUID().replaceAll("-", ""); // 32 hexadecimales también valen
    const res = await fetch(`${BASE_URL}/api/ranking`, {
      headers: { authorization: `Bearer ${token}`, "X-Request-Id": id },
    });
    assert.equal(res.status, 200);
    assert.equal(res.headers.get(HEADER), id);

    const found = await linesPerService(["nginx", "api", "world"], id);
    assert.ok(found.world.some((l) => l.includes("GetRanking") && l.includes(`request_id=${id}`)));
  });

  it("generates an id in nginx when the header is missing and echoes it once", async () => {
    const res = await fetch(`${BASE_URL}/api/health`);
    const id = res.headers.get(HEADER);
    assert.match(id, /^[0-9a-f]{32}$/, "nginx usa su $request_id");
    assert.equal([...res.headers].filter(([k]) => k === HEADER).length, 1);
    const found = await linesPerService(["nginx", "api"], id);
    assert.ok(found.nginx.length > 0);
  });

  it("replaces an invalid id and never writes it to the logs", async () => {
    const bad = `<script>${"x".repeat(80)}`;
    const res = await fetch(`${BASE_URL}/api/health`, { headers: { "X-Request-Id": bad } });
    const id = res.headers.get(HEADER);
    assert.notEqual(id, bad);
    assert.match(id, /^[0-9a-f]{32}$/);
    await linesPerService(["nginx", "api"], id);
    assert.equal((await logLines(["nginx", "api"], "<script>")).length, 0, "el id inválido no se registra");
  });

  it("keeps any query string out of the nginx logs (the /play token rule)", async () => {
    const secret = `secret-${randomUUID()}`;
    await fetch(`${BASE_URL}/api/health?token=${secret}`);
    await sleep(500);
    assert.equal((await logLines(["nginx"], secret)).length, 0);
  });
});
