// CP-01: Acciones simultáneas de un mismo jugador (RF-08).
// Modo demo; un jugador envía 100 gotas sin esperar resultados individuales.
// Invariantes:
//   1. Depósito intacto (la gota es gratis en modo demo).
//   2. La humedad aportada sube exactamente las gotas aceptadas.
import { describe, it, before } from "node:test";
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

describe("concurrent drops (CP-01 / RF-08)", () => {
    const adminUsername = process.env.ADMIN_USERNAME;
    const adminPassword = process.env.E2E_ADMIN_PASSWORD;

    before(async () => {
        assert.ok(
            adminUsername && adminPassword,
            "falta ADMIN_USERNAME o E2E_ADMIN_PASSWORD: tests/e2e/run.sh las genera",
        );
        await waitForHealthyStack();
    });

    it("applies concurrent drops from 2 bots in demo mode matching humidity exactly", { timeout: 45_000 }, async () => {
        // 1. Iniciar admin y encender modo demo
        const adminRes = await fetch(`${BASE_URL}/api/auth/login`, {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({ username: adminUsername, password: adminPassword }),
        });
        assert.equal(adminRes.status, 200, `admin login: ${adminRes.status}`);
        const { token: adminToken } = await adminRes.json();
        const adminSession = await openReadySession(adminToken, { map: false });

        let session1;
        let session2;

        try {
            const adminActionId = randomUUID();
            adminSession.send({ type: "set_demo_mode", action_id: adminActionId, enabled: true });
            await adminSession.first("demo mode enabled", (f) => f.type === "state" && f.demo_mode === true, 10_000);

            // 2. Conectar dos bots simultáneos
            const [bot1, bot2] = await Promise.all([registerPlayer(), registerPlayer()]);
            session1 = await openReadySession(bot1.token, { map: false });
            session2 = await openReadySession(bot2.token, { map: false });

            // Línea base inicial (humedad aportada antes de las acciones)
            const state0 = await session1.first("initial state", (f) => f.type === "state", 10_000);
            const humidity0 = BigInt(state0.humidity_contributed ?? "0");

            // 3. Preparar 50 gotas por bot (100 gotas en total) sobre distintas casillas
            const DROPS_PER_BOT = 50;
            const TOTAL_DROPS = DROPS_PER_BOT * 2;
            const actions1 = Array.from({ length: DROPS_PER_BOT }, () => randomUUID());
            const actions2 = Array.from({ length: DROPS_PER_BOT }, () => randomUUID());

            async function sendDrops(session, actions, offsetX, offsetY) {
                for (let i = 0; i < actions.length; i++) {
                    const actionId = actions[i];
                    session.send({
                        type: "drop",
                        action_id: actionId,
                        x: (offsetX + i) % 32,
                        y: (offsetY + Math.floor(i / 32)) % 32,
                    });
                    await sleep(5);
                }
            }

            // Ambos bots disparan simultáneamente
            await Promise.all([
                sendDrops(session1, actions1, 0, 0),
                sendDrops(session2, actions2, 16, 16),
            ]);

            // 4. Recolectar resultados por separado para cada bot
            async function collectResults(session, actions) {
                let accepted = 0;
                let rateLimited = 0;
                for (const actionId of actions) {
                    const result = await session.first(
                        `result of ${actionId}`,
                        (f) => f.type === "result" && f.action_id === actionId,
                        15_000,
                    );
                    if (result.status === "applied") accepted++;
                    if (result.status === "rate_limited") rateLimited++;
                }
                return { accepted, rateLimited };
            }

            const [res1, res2] = await Promise.all([
                collectResults(session1, actions1),
                collectResults(session2, actions2),
            ]);

            const totalAccepted = res1.accepted + res2.accepted;
            const totalRateLimited = res1.rateLimited + res2.rateLimited;

            // 5. Esperar el estado del mundo que refleje las gotas aceptadas totales
            const finalState = await session1.waitFor(
                "final state reflecting drops",
                (f) =>
                    f.type === "state" &&
                    BigInt(f.humidity_contributed ?? "0") >= humidity0 + BigInt(totalAccepted),
                15_000,
            );
            const humidityFinal = BigInt(finalState.humidity_contributed ?? "0");
            const humidityDelta = humidityFinal - humidity0;

            // Comprobar estado de jugador en ambos bots (depósito intacto)
            const finalState1 = await session1.waitFor(
                "final state with bot1",
                (f) => f.type === "state" && f.player !== undefined,
                10_000,
            );
            const finalState2 = await session2.waitFor(
                "final state with bot2",
                (f) => f.type === "state" && f.player !== undefined,
                10_000,
            );

            // 6. Invariantes requeridos por CP-01
            assert.equal(totalAccepted + totalRateLimited, TOTAL_DROPS, "todas las 100 gotas obtuvieron resultado");
            assert.equal(humidityDelta, BigInt(totalAccepted), "la humedad aportada subió exactamente las gotas aceptadas");
            assert.equal(finalState1.player.water, finalState1.player.tank_capacity, "en modo demo el depósito de bot 1 no cambia");
            assert.equal(finalState2.player.water, finalState2.player.tank_capacity, "en modo demo el depósito de bot 2 no cambia");

            // 7. Registro de medición requerido con desglose por bot
            console.log(
                `MEDICION CP-01: enviadas=${TOTAL_DROPS} aceptadas=${totalAccepted} (bot1=${res1.accepted} bot2=${res2.accepted}) rate_limited=${totalRateLimited} (bot1=${res1.rateLimited} bot2=${res2.rateLimited}) delta_humedad=${humidityDelta} water_delta=0`,
            );

            session1.close();
            session2.close();
        } finally {
            session1?.close();
            session2?.close();
            // Apagar modo demo y cerrar admin
            const resetAction = randomUUID();
            adminSession.send({ type: "set_demo_mode", action_id: resetAction, enabled: false });
            await adminSession.first("demo mode disabled", (f) => f.type === "state" && !f.demo_mode, 10_000).catch(() => { });
            adminSession.close();
        }
    });
});
