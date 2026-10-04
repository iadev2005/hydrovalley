// Pruebas unitarias de la parte pura del arnés de fallos (faults.mjs): no
// necesitan el stack. Se ejecutan con `node --test tests/e2e/faults.unit.test.mjs`.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  waitFor,
  parseTick,
  ticksLost,
  isStalled,
  lastSnapshotTick,
  composeArgs,
  serviceStatus,
  formatMeasurement,
} from "./faults.mjs";

describe("waitFor", () => {
  it("returns the first truthy value of the predicate", async () => {
    let calls = 0;
    const value = await waitFor(() => (++calls === 3 ? "ready" : false), { timeoutMs: 1000, intervalMs: 5 });
    assert.equal(value, "ready");
    assert.equal(calls, 3);
  });

  it("awaits asynchronous predicates", async () => {
    const value = await waitFor(async () => 42, { timeoutMs: 1000, intervalMs: 5 });
    assert.equal(value, 42);
  });

  it("treats a thrown error as 'not yet' and retries", async () => {
    let calls = 0;
    const value = await waitFor(
      () => {
        if (++calls < 3) throw new Error("not up");
        return true;
      },
      { timeoutMs: 1000, intervalMs: 5 },
    );
    assert.equal(value, true);
  });

  it("times out with the description and the last error", async () => {
    await assert.rejects(
      waitFor(
        () => {
          throw new Error("refused");
        },
        { timeoutMs: 40, intervalMs: 5, description: "the hub" },
      ),
      /timed out.*the hub.*refused/s,
    );
  });

  it("times out when the predicate never holds", async () => {
    await assert.rejects(waitFor(() => false, { timeoutMs: 30, intervalMs: 5 }), /timed out/);
  });
});

describe("parseTick", () => {
  it("reads the 64-bit tick that travels as text", () => {
    assert.equal(parseTick("1234"), 1234n);
    assert.equal(parseTick(1234), 1234n);
    assert.equal(parseTick("18446744073709551615"), 18446744073709551615n);
  });

  it("returns null for anything that is not a tick", () => {
    for (const bad of [undefined, null, "", "abc", "-1", "1.5", {}]) {
      assert.equal(parseTick(bad), null, String(bad));
    }
  });
});

describe("ticksLost", () => {
  it("counts the ticks the valley went back", () => {
    assert.equal(ticksLost(1000n, 962n), 38n);
  });

  it("is zero when the valley did not go back", () => {
    assert.equal(ticksLost(1000n, 1001n), 0n);
    assert.equal(ticksLost(1000n, 1000n), 0n);
  });
});

describe("lastSnapshotTick", () => {
  it("rounds a tick down to the snapshot that holds it", () => {
    assert.equal(lastSnapshotTick(324n, 50n), 300n);
    assert.equal(lastSnapshotTick(300n, 50n), 300n);
    assert.equal(lastSnapshotTick(49n, 50n), 0n);
  });
});

describe("isStalled", () => {
  it("is true when every sample is the same tick", () => {
    assert.equal(isStalled([10n, 10n, 10n]), true);
  });

  it("is false when the tick advanced", () => {
    assert.equal(isStalled([10n, 10n, 11n]), false);
  });

  it("is false without at least two samples", () => {
    assert.equal(isStalled([]), false);
    assert.equal(isStalled([10n]), false);
  });
});

describe("composeArgs", () => {
  it("builds the docker compose invocation for a service", () => {
    assert.deepEqual(composeArgs("stop", "world"), ["compose", "stop", "world"]);
    assert.deepEqual(composeArgs("pause", "hub"), ["compose", "pause", "hub"]);
    assert.deepEqual(composeArgs("kill", "world"), ["compose", "kill", "world"]);
  });

  it("rejects an action outside the allowed ones", () => {
    assert.throws(() => composeArgs("down", "world"), /action/);
  });

  it("rejects an empty service name", () => {
    assert.throws(() => composeArgs("stop", ""), /service/);
  });
});

describe("serviceStatus", () => {
  const system = { services: [{ name: "world", status: "down", detail: "timeout" }, { name: "hub", status: "up" }] };

  it("finds a service in the /api/system body", () => {
    assert.equal(serviceStatus(system, "world").status, "down");
    assert.equal(serviceStatus(system, "hub").status, "up");
  });

  it("returns undefined for an unknown service", () => {
    assert.equal(serviceStatus(system, "chat"), undefined);
  });
});

describe("formatMeasurement", () => {
  it("prints one compact line with the measured values", () => {
    const line = formatMeasurement("CP-05 stop", { detect_s: 1.2345, recover_s: 12, ticks_lost: 38n });
    assert.equal(line, "MEDICION CP-05 stop: detect_s=1.2 recover_s=12.0 ticks_lost=38");
  });

  it("marks a missing value instead of inventing it", () => {
    assert.equal(formatMeasurement("x", { detect_s: undefined }), "MEDICION x: detect_s=n/d");
  });
});
