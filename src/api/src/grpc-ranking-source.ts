// Cliente gRPC de WorldService.GetRanking (`WORLD_ADDR`). Idempotente: usa la
// política de reintentos de grpc-retry.ts y admite lista de direcciones
// separadas por comas con afinidad activa (B8 W2).
import { createClient } from "@connectrpc/connect";
import { parseAddressList } from "./config.js";
import { WorldService } from "./gen/hydrovalley/world/v1/world_pb.js";
import { createGrpcCaller, type GrpcCallOptions } from "./grpc-retry.js";
import type { RankingSource } from "./ranking-source.js";
import { translateGrpcError } from "./source-errors.js";
import { createGrpcTransportFor } from "./request-id.js";

export function createGrpcRankingSource(options: GrpcCallOptions): RankingSource {
  const { idempotent } = createGrpcCaller(options);
  const addresses = parseAddressList("WORLD_ADDR", options.address);
  const clients = addresses.map((addr) =>
    createClient(WorldService, createGrpcTransportFor(addr)),
  );
  let active = 0;

  return {
    async getRanking(limit) {
      let lastErr: unknown;
      for (let i = 0; i < clients.length; i++) {
        const idx = (active + i) % clients.length;
        const client = clients[idx];
        if (!client) continue;
        try {
          const res = await idempotent((timeout) =>
            client.getRanking({ limit }, { timeoutMs: timeout }),
          );
          active = idx;
          return {
            tick: res.tick,
            entries: res.entries.map((e) => ({
              rank: e.rank,
              playerId: e.playerId,
              leavesEarned: e.leavesEarned,
            })),
          };
        } catch (err) {
          lastErr = err;
        }
      }
      throw translateGrpcError(lastErr, "world");
    },
  };
}
