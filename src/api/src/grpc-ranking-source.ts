// Cliente gRPC de WorldService.GetRanking (`WORLD_ADDR`). Idempotente: usa la
// política de reintentos de grpc-retry.ts y admite lista de direcciones
// separadas por comas con afinidad activa y selección por tick lógico (B8 W2).
import { createClient } from "@connectrpc/connect";
import { parseAddressList } from "./config.js";
import { WorldService } from "./gen/hydrovalley/world/v1/world_pb.js";
import { createGrpcCaller, type GrpcCallOptions } from "./grpc-retry.js";
import type { Ranking, RankingSource } from "./ranking-source.js";
import { translateGrpcError } from "./source-errors.js";
import { createGrpcTransportFor } from "./request-id.js";

export function createGrpcRankingSource(options: GrpcCallOptions): RankingSource {
  const { idempotent } = createGrpcCaller(options);
  const addresses = parseAddressList("WORLD_ADDR", options.address);
  const clients = addresses.map((addr) =>
    createClient(WorldService, createGrpcTransportFor(addr)),
  );
  let active = 0;
  let lastSeenTick: bigint | null = null;

  async function fetchWithRetry(idx: number, limit: number): Promise<Ranking> {
    const client = clients[idx];
    if (!client) throw new Error("no client");
    const res = await idempotent((timeout) =>
      client.getRanking({ limit }, { timeoutMs: timeout }),
    );
    return {
      tick: res.tick,
      entries: res.entries.map((e) => ({
        rank: e.rank,
        playerId: e.playerId,
        leavesEarned: e.leavesEarned,
      })),
    };
  }

  async function probeSingle(idx: number, limit: number): Promise<Ranking> {
    const client = clients[idx];
    if (!client) throw new Error("no client");
    const probeTimeout = Math.min(options.timeoutMs ?? 2000, 500);
    const res = await client.getRanking({ limit }, { timeoutMs: probeTimeout });
    return {
      tick: res.tick,
      entries: res.entries.map((e) => ({
        rank: e.rank,
        playerId: e.playerId,
        leavesEarned: e.leavesEarned,
      })),
    };
  }

  return {
    async getRanking(limit) {
      if (clients.length === 1) {
        try {
          return await fetchWithRetry(0, limit);
        } catch (err) {
          throw translateGrpcError(err, "world");
        }
      }

      let activeResult: Ranking | null = null;
      let lastErr: unknown = null;

      try {
        activeResult = await fetchWithRetry(active, limit);
      } catch (err) {
        lastErr = err;
      }

      if (activeResult !== null && lastSeenTick !== null && activeResult.tick > lastSeenTick) {
        lastSeenTick = activeResult.tick;
        return activeResult;
      }

      let bestResult = activeResult;
      let bestIdx = activeResult !== null ? active : -1;

      for (let i = 1; i < clients.length; i++) {
        const idx = (active + i) % clients.length;
        try {
          const cand =
            activeResult !== null
              ? await probeSingle(idx, limit)
              : await fetchWithRetry(idx, limit);
          if (bestResult === null || cand.tick > bestResult.tick) {
            bestResult = cand;
            bestIdx = idx;
          }
        } catch (err) {
          lastErr = err;
        }
      }

      if (bestResult !== null) {
        active = bestIdx;
        lastSeenTick = bestResult.tick;
        return bestResult;
      }

      throw translateGrpcError(lastErr, "world");
    },
  };
}
