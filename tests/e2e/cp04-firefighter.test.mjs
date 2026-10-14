// CP-04: Extinción de incendios por varios bomberos (RF-29, RF-30).
//
// Necesita un valle nuevo con el catálogo de prueba
// tests/e2e/fixtures/catalog.test-fire.json (fire.ignition_chance_ppm alto;
// la imagen y catalog/catalog.json no se tocan). tests/e2e/run.sh lo ejecuta
// al final, en una fase aparte que baja el stack, lo levanta de nuevo sumando
// tests/e2e/compose.cp04.yml (monta ese catálogo y fija CATALOG_PATH en la
// persistencia) y corre solo este archivo:
//   1. Un jugador siembra una planta con 4 gotas (100 de humedad).
//   2. La casilla sube a pasto y su humedad se reinicia a 0.
//   3. Con ignition_chance_ppm alto (500.000 ppm = 50% por tick), la casilla arde.
//   4. Se corre `bots firefighter` (3 bomberos): detectan el fuego y lo apagan.
//   5. Se comprueba que el fuego se apaga una vez, que un solo bot recibe el
//      bono y que nada se pierde.
// Se ejecuta con `tests/e2e/run.sh` sin argumentos (fase final) y se omite con
// E2E_SKIP_CP04=1. No corre contra un stack cualquiera: con argumentos, run.sh
// levanta el stack por defecto (catálogo de la imagen) y este caso fallaría;
// necesita el valle nuevo y el catálogo de prueba que prepara esa fase.

import { describe, it, before } from "node:test";
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { randomUUID } from "node:crypto";
import { promisify } from "node:util";
import {
  loginAdmin,
  adminCredentials,
  openReadySession,
  provisionPlayers,
  waitForHealthyStack,
} from "./support.mjs";

const run = promisify(execFile);

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

describe("firefighter bots against real fires (CP-04)", () => {
  before(async () => {
    await waitForHealthyStack();
  });

  it("siembra, arde y los bomberos lo apagan una vez con un solo bono", { timeout: 120_000 }, async () => {
    const { username: adminUsername, password: adminPassword } = adminCredentials();
    // 1. Sembrar una planta: un jugador riega la casilla (10, 10) con 4 gotas.
    const [planter] = await provisionPlayers(await loginAdmin(), 1, "e2e-planter");
    const session = await openReadySession(planter.token, { map: true });

    const targetX = 10;
    const targetY = 10;

    // 4 gotas = 4 × 25 = 100 de humedad, lo necesario para subir a pasto (etapa 2).
    for (let i = 0; i < 4; i++) {
      await dropAndWait(session, targetX, targetY);
    }

    // 2. Esperar a que la planta suba y arda.
    console.log(`esperando a que la casilla (${targetX}, ${targetY}) arda...`);
    const fireState = await session.waitFor(
      "the tile to ignite",
      (f) => f.type === "state" && f.fires?.some((fire) => fire.x === targetX && fire.y === targetY),
      20_000,
    );
    console.log(`fuego encendido en (${targetX}, ${targetY}) en el tick ${fireState.tick}`);
    session.close();

    // 3. Correr `bots firefighter` para apagar el incendio activo.
    const { stdout } = await run(
      "docker",
      ["compose", "--profile", "bots", "run", "--rm", "-T", "bots", "firefighter"],
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

    // 4. Comprobaciones de CP-04.
    assert.match(stdout, /^Resultado: CUMPLE$/m, stdout);
    assert.match(stdout, /Incendios vistos: 1 · apagados: 1 · consumidos: 0 · sin apagar: 0/);
    // Bono único: en este valle de prueba las hojas de los bots solo suben por
    // el bono de extinción (las gotas caen en la casilla en llamas, que no
    // crece), así que exactamente un bot debe mostrar la subida y ninguno más.
    const bonus = stdout.match(/bots con subida de hojas del tamaño del bono \(indicativo\): \[([^\]]*)\]/);
    assert.ok(bonus, `el resumen no indica qué bots recibieron el bono:\n${stdout}`);
    const bonusBots = bonus[1].trim().split(/\s+/).filter(Boolean);
    assert.equal(bonusBots.length, 1, `exactamente un bot recibe el bono de extinción, y fueron: [${bonus[1]}]`);
    assert.match(stdout, /Bono único \(indicativo\): sin señales de repetirse/);

    console.log(`MEDICION CP-04: incendios=1 apagados=1 bots_con_bono=${bonusBots.length} resultado=CUMPLE`);
  });
});
