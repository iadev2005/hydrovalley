import { Code, ConnectError } from "@connectrpc/connect";
import { afterEach, describe, expect, it } from "vitest";
import { WorldService } from "../src/gen/hydrovalley/world/v1/world_pb.js";
import { ReportService } from "../src/gen/hydrovalley/persistence/v1/report_pb.js";
import { createGrpcRankingSource } from "../src/grpc-ranking-source.js";
import { createGrpcReportSource } from "../src/grpc-report-source.js";
import {
  SourceInvalidRequestError,
  SourceNotFoundError,
  SourceUnavailableError,
} from "../src/source-errors.js";
import { TestGrpcServer } from "./support/grpc-server.js";

const server = new TestGrpcServer();
const server2 = new TestGrpcServer();
const sleeps: number[] = [];
let clockOffset = 0;

afterEach(async () => {
  sleeps.length = 0;
  clockOffset = 0;
  await Promise.all([server.stop(), server2.stop()]);
});

const callOptions = (address: string, timeoutMs = 2000) => ({
  address,
  timeoutMs,
  random: () => 0,
  now: () => Date.now() + clockOffset,
  sleep: (ms: number) => {
    sleeps.push(ms);
    clockOffset += ms;
    return Promise.resolve();
  },
});

const MAX_U64 = 18_446_744_073_709_551_615n;

describe("gRPC ranking source", () => {
  it("maps the entries and keeps u64 values as bigint", async () => {
    const seen: number[] = [];
    const address = await server.start(WorldService, {
      getRanking: (req) => {
        seen.push(req.limit);
        return {
          tick: 1234n,
          entries: [
            { rank: 1, playerId: "p1", leavesEarned: MAX_U64 },
            { rank: 2, playerId: "p2", leavesEarned: 5n },
          ],
        };
      },
    });

    const ranking = await createGrpcRankingSource(callOptions(address)).getRanking(20);

    expect(seen).toEqual([20]);
    expect(ranking).toEqual({
      tick: 1234n,
      entries: [
        { rank: 1, playerId: "p1", leavesEarned: MAX_U64 },
        { rank: 2, playerId: "p2", leavesEarned: 5n },
      ],
    });
  });

  it("retries on UNAVAILABLE with 100/200/400 ms backoff, then succeeds", async () => {
    let calls = 0;
    const address = await server.start(WorldService, {
      getRanking: () => {
        calls++;
        if (calls < 4) throw new ConnectError("down", Code.Unavailable);
        return { tick: 1n, entries: [] };
      },
    });

    const ranking = await createGrpcRankingSource(callOptions(address)).getRanking(10);

    expect(ranking.entries).toEqual([]);
    expect(calls).toBe(4);
    expect(sleeps).toEqual([100, 200, 400]);
  });

  it("reports unavailability after the retries are exhausted", async () => {
    let calls = 0;
    const address = await server.start(WorldService, {
      getRanking: () => {
        calls++;
        throw new ConnectError("down", Code.Unavailable);
      },
    });

    await expect(
      createGrpcRankingSource(callOptions(address)).getRanking(10),
    ).rejects.toBeInstanceOf(SourceUnavailableError);
    expect(calls).toBe(4);
  });

  it("does not retry INVALID_ARGUMENT and maps it to a domain error", async () => {
    let calls = 0;
    const address = await server.start(WorldService, {
      getRanking: () => {
        calls++;
        throw new ConnectError("bad", Code.InvalidArgument);
      },
    });

    await expect(
      createGrpcRankingSource(callOptions(address)).getRanking(10),
    ).rejects.toBeInstanceOf(SourceInvalidRequestError);
    expect(calls).toBe(1);
  });

  it("treats a missed deadline and a refused connection as unavailable", async () => {
    const slow = await server.start(WorldService, {
      getRanking: async () => {
        await new Promise((resolve) => setTimeout(resolve, 300));
        return { tick: 1n, entries: [] };
      },
    });
    await expect(
      createGrpcRankingSource({
        ...callOptions(slow, 50),
        retryDelaysMs: [],
      }).getRanking(1),
    ).rejects.toBeInstanceOf(SourceUnavailableError);

    await server.stop();
    await expect(createGrpcRankingSource(callOptions(slow)).getRanking(1)).rejects.toBeInstanceOf(
      SourceUnavailableError,
    );
  });

  it("selects the node with the highest advancing tick in a multi-address setup", async () => {
    let calls1 = 0;
    let calls2 = 0;
    let tick1 = 100n;
    let tick2 = 200n;

    const addr1 = await server.start(WorldService, {
      getRanking: () => {
        calls1++;
        return {
          tick: tick1,
          entries: [{ rank: 1, playerId: "p1", leavesEarned: 10n }],
        };
      },
    });

    const addr2 = await server2.start(WorldService, {
      getRanking: () => {
        calls2++;
        return {
          tick: tick2,
          entries: [{ rank: 1, playerId: "p2", leavesEarned: 20n }],
        };
      },
    });

    const source = createGrpcRankingSource(callOptions(`${addr1},${addr2}`));

    // First call: probes both because tick was unknown, discovers server2 has higher tick (200n > 100n)
    const first = await source.getRanking(5);
    expect(first.tick).toBe(200n);
    expect(first.entries[0]?.playerId).toBe("p2");
    expect(calls1).toBe(1);
    expect(calls2).toBe(1);

    // Second call: server2 is active and advancing, fast path taken without probing server1
    tick2 = 250n;
    const second = await source.getRanking(5);
    expect(second.tick).toBe(250n);
    expect(second.entries[0]?.playerId).toBe("p2");
    expect(calls1).toBe(1); // server1 was not called!
    expect(calls2).toBe(2);

    // Failover: server2 freezes at 250n, server1 resumes and reaches 300n
    tick1 = 300n;
    const third = await source.getRanking(5);
    expect(third.tick).toBe(300n);
    expect(third.entries[0]?.playerId).toBe("p1");
    expect(calls1).toBe(2);
    expect(calls2).toBe(3);

    // Fourth call: server1 is now active and advancing (350n > 300n), fast path taken
    tick1 = 350n;
    const fourth = await source.getRanking(5);
    expect(fourth.tick).toBe(350n);
    expect(fourth.entries[0]?.playerId).toBe("p1");
    expect(calls1).toBe(3);
    expect(calls2).toBe(3); // server2 was not called!
  });

  it("fails over to secondary node when active node is unavailable in multi-address setup", async () => {
    let calls1 = 0;
    let calls2 = 0;

    const addr1 = await server.start(WorldService, {
      getRanking: () => {
        calls1++;
        throw new ConnectError("down", Code.Unavailable);
      },
    });

    const addr2 = await server2.start(WorldService, {
      getRanking: () => {
        calls2++;
        return {
          tick: 500n,
          entries: [{ rank: 1, playerId: "p2", leavesEarned: 50n }],
        };
      },
    });

    const source = createGrpcRankingSource({
      ...callOptions(`${addr1},${addr2}`),
      retryDelaysMs: [],
    });

    const res = await source.getRanking(5);
    expect(res.tick).toBe(500n);
    expect(res.entries[0]?.playerId).toBe("p2");
    expect(calls1).toBeGreaterThanOrEqual(1);
    expect(calls2).toBe(1);
  });
});

describe("gRPC report source", () => {
  const totals = {
    waterReduced: 1n,
    waterReused: 2n,
    waterRecycled: 3n,
    waterRefilled: 4n,
    waterSpent: 5n,
    leavesEarned: MAX_U64,
    leavesSpent: 7n,
  };

  it("sends the player and the period and maps the totals", async () => {
    const seen: {
      playerId: string;
      fromTick: bigint | undefined;
      toTick: bigint | undefined;
    }[] = [];
    const address = await server.start(ReportService, {
      getReport: (req) => {
        seen.push({
          playerId: req.playerId,
          fromTick: req.fromTick,
          toTick: req.toTick,
        });
        return {
          playerId: req.playerId,
          epoch: 2,
          fromTick: 50n,
          toTick: 150n,
          totals,
        };
      },
    });
    const source = createGrpcReportSource(callOptions(address));

    const report = await source.getReport({
      playerId: "p1",
      fromTick: 50n,
      toTick: 150n,
    });

    expect(seen).toEqual([{ playerId: "p1", fromTick: 50n, toTick: 150n }]);
    expect(report).toEqual({
      playerId: "p1",
      epoch: 2,
      fromTick: 50n,
      toTick: 150n,
      totals,
    });
  });

  it("leaves the optional ticks unset and asks for the global report with an empty player", async () => {
    const seen: { playerId: string; hasFrom: boolean; hasTo: boolean }[] = [];
    const address = await server.start(ReportService, {
      getReport: (req) => {
        seen.push({
          playerId: req.playerId,
          hasFrom: req.fromTick !== undefined,
          hasTo: req.toTick !== undefined,
        });
        return { playerId: "", epoch: 1, fromTick: 0n, toTick: 100n, totals };
      },
    });

    const report = await createGrpcReportSource(callOptions(address)).getReport({});

    expect(seen).toEqual([{ playerId: "", hasFrom: false, hasTo: false }]);
    expect(report.playerId).toBe("");
  });

  it("maps NOT_FOUND, INVALID_ARGUMENT and UNAVAILABLE", async () => {
    const address = await server.start(ReportService, {
      getReport: (req) => {
        const code =
          req.playerId === "nf"
            ? Code.NotFound
            : req.playerId === "bad"
              ? Code.InvalidArgument
              : Code.Unavailable;
        throw new ConnectError("x", code);
      },
    });
    const source = createGrpcReportSource({
      ...callOptions(address),
      retryDelaysMs: [],
    });

    await expect(source.getReport({ playerId: "nf" })).rejects.toBeInstanceOf(SourceNotFoundError);
    await expect(source.getReport({ playerId: "bad" })).rejects.toBeInstanceOf(
      SourceInvalidRequestError,
    );
    await expect(source.getReport({ playerId: "down" })).rejects.toBeInstanceOf(
      SourceUnavailableError,
    );
  });

  it("retries transient failures and never retries NOT_FOUND", async () => {
    let calls = 0;
    const address = await server.start(ReportService, {
      getReport: () => {
        calls++;
        if (calls === 1) throw new ConnectError("down", Code.DeadlineExceeded);
        return { playerId: "", epoch: 1, fromTick: 0n, toTick: 1n, totals };
      },
    });
    await createGrpcReportSource(callOptions(address)).getReport({});
    expect(calls).toBe(2);
    expect(sleeps).toEqual([100]);

    await server.stop();
    calls = 0;
    const notFound = await server.start(ReportService, {
      getReport: () => {
        calls++;
        throw new ConnectError("none", Code.NotFound);
      },
    });
    await expect(
      createGrpcReportSource(callOptions(notFound)).getReport({}),
    ).rejects.toBeInstanceOf(SourceNotFoundError);
    expect(calls).toBe(1);
  });
});
