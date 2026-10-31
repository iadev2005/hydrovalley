// Cliente gRPC de ReportService.GetReport (`PERSISTENCE_ADDR`). Idempotente:
// usa la política de reintentos de grpc-retry.ts.
import { createClient } from "@connectrpc/connect";
import { ReportService } from "./gen/hydrovalley/persistence/v1/report_pb.js";
import { createGrpcCaller, type GrpcCallOptions } from "./grpc-retry.js";
import type { ReportSource } from "./report-source.js";
import { translateGrpcError } from "./source-errors.js";
import { createGrpcTransportFor } from "./request-id.js";

export function createGrpcReportSource(options: GrpcCallOptions): ReportSource {
  const { idempotent } = createGrpcCaller(options);
  const client = createClient(
    ReportService,
    createGrpcTransportFor(options.address),
  );

  return {
    async getReport({ playerId, fromTick, toTick }) {
      try {
        const res = await idempotent((timeout) =>
          client.getReport(
            {
              playerId: playerId ?? "",
              ...(fromTick === undefined ? {} : { fromTick }),
              ...(toTick === undefined ? {} : { toTick }),
            },
            { timeoutMs: timeout },
          ),
        );
        const t = res.totals;
        return {
          playerId: res.playerId,
          epoch: res.epoch,
          fromTick: res.fromTick,
          toTick: res.toTick,
          totals: {
            waterReduced: t?.waterReduced ?? 0n,
            waterReused: t?.waterReused ?? 0n,
            waterRecycled: t?.waterRecycled ?? 0n,
            waterRefilled: t?.waterRefilled ?? 0n,
            waterSpent: t?.waterSpent ?? 0n,
            leavesEarned: t?.leavesEarned ?? 0n,
            leavesSpent: t?.leavesSpent ?? 0n,
          },
        };
      } catch (err) {
        throw translateGrpcError(err, "persistence");
      }
    },
  };
}
