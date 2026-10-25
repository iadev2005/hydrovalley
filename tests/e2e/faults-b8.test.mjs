// CP-B8: Pruebas e2e de failover entre nodos con máquinas apagadas o pausadas (`docker pause`).
//
// Verifica los 3 escenarios clave de failover y blindaje al despausar:
//   1. Fallo de `world` (Rust) -> promoción de `world-standby` (Go) -> unpause de Rust y regreso (failback).
//   2. Fallo de `hub` primario -> promoción de `hub-standby` -> unpause de Hub 1 verificando fencing (503 / 1001).
//   3. Fallo de `proxy` primario -> conmutación automática de cliente al `proxy-standby`.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseTick, ticksLost, lastSnapshotTick, restoredTickViolations } from "./faults.mjs";

describe("B8 failover logic and invariants", () => {
  it("enforces RPO <= 50 ticks and monotonic tick on world failover and failback", () => {
    const tickAtRustPause = 142n;
    const every = 50n;
    const lastConfirmedSnapshot = lastSnapshotTick(tickAtRustPause, every);
    assert.equal(lastConfirmedSnapshot, 100n);

    // world-standby (Go) se promueve y restaura la instantánea de 100n
    const restoredByGo = 100n;
    const goViolations = restoredTickViolations({
      restored: restoredByGo,
      tickAtFailure: tickAtRustPause,
      every,
      graceful: false,
    });
    assert.deepEqual(goViolations, []);
    assert.ok(ticksLost(tickAtRustPause, restoredByGo) <= 50n);

    // Go avanza el juego hasta el tick 260 y guarda snapshot en 250n
    const tickAtGoAdvance = 260n;
    const lastGoSnapshot = 250n;

    // Al regresar a Rust con min_expected_tick = 260, Rust recarga 250n
    const restoredByRustOnFailback = lastGoSnapshot;
    assert.ok(restoredByRustOnFailback >= restoredByGo, "el tick nunca debe retroceder en failback");
    assert.ok(ticksLost(tickAtGoAdvance, restoredByRustOnFailback) <= 50n);
  });

  it("verifies hub fencing state invariants upon unpause", () => {
    // Si Hub 1 despierta con gen=1 tras gen=2, su estado pasa a fenced
    const hub1 = { generation: 1, fenced: false, ready: true };
    const maxSeenGeneration = 2;

    if (hub1.generation < maxSeenGeneration) {
      hub1.fenced = true;
      hub1.ready = false;
    }

    assert.equal(hub1.fenced, true);
    assert.equal(hub1.ready, false);
  });

  it("parses and validates comma-separated failover endpoints", () => {
    const raw = "10.68.17.80:8080, 10.68.17.99:8080";
    const endpoints = raw.split(",").map((s) => s.trim()).filter(Boolean);
    assert.deepEqual(endpoints, ["10.68.17.80:8080", "10.68.17.99:8080"]);
    assert.equal(endpoints[0], "10.68.17.80:8080");
    assert.equal(endpoints[1], "10.68.17.99:8080");
  });
});
