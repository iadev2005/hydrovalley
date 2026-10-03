// Cliente gRPC de WorldService.GetRanking (`WORLD_ADDR`). Idempotente: usa la
// política de reintentos de grpc-retry.ts.
import { createClient } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";
import { WorldService } from "./gen/hydrovalley/world/v1/world_pb.js";
import { createGrpcCaller, type GrpcCallOptions } from "./grpc-retry.js";
import type { RankingSource } from "./ranking-source.js";
import { translateGrpcError } from "./source-errors.js";

export function createGrpcRankingSource(options: GrpcCallOptions): RankingSource {
  const { idempotent } = createGrpcCaller(options);
  const client = createClient(
    WorldService,
    createGrpcTransport({ baseUrl: `http://${options.address}` }),
  );

  return {
    async getRanking(limit) {
      try {
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
      } catch (err) {
        throw translateGrpcError(err, "world");
      }
    },
  };
}
