// Cliente gRPC de CatalogService.GetCatalog (`PERSISTENCE_ADDR`). Idempotente:
// usa la política de reintentos de grpc-retry.ts.
import { createClient } from "@connectrpc/connect";
import { CatalogService } from "./gen/hydrovalley/persistence/v1/catalog_pb.js";
import type { Catalog } from "./catalog-source.js";
import type { CatalogSource } from "./catalog-source.js";
import { createGrpcCaller, type GrpcCallOptions } from "./grpc-retry.js";
import { createGrpcTransportFor } from "./request-id.js";
import { SourceUnavailableError, translateGrpcError } from "./source-errors.js";

/** Copia un valor del mensaje sin los campos `$…` del código generado. */
function plain(value: unknown): unknown {
  if (typeof value !== "object" || value === null) return value;
  return Object.fromEntries(
    Object.entries(value)
      .filter(([key]) => !key.startsWith("$"))
      .map(([key, inner]) => [key, plain(inner)]),
  );
}

/** Mensajes anidados que debe traer el catálogo (proto3 los deja opcionales). */
const GROUPS = [
  "drop",
  "tank",
  "growth",
  "evaporation",
  "fire",
  "sprinkler",
  "drip",
  "treatmentPlant",
  "trashBin",
  "terrain",
  "urban",
  "biggerDrop",
  "biggerTank",
] as const;

export function createGrpcCatalogSource(options: GrpcCallOptions): CatalogSource {
  const { idempotent } = createGrpcCaller(options);
  const client = createClient(CatalogService, createGrpcTransportFor(options.address));

  return {
    async getCatalog() {
      let res;
      try {
        res = await idempotent((timeout) => client.getCatalog({}, { timeoutMs: timeout }));
      } catch (err) {
        throw translateGrpcError(err, "persistence");
      }
      const catalog = res.catalog;
      if (!catalog || GROUPS.some((group) => catalog[group] === undefined)) {
        // La persistencia respondió, pero sin todas las cifras: no se sirve a medias.
        throw new SourceUnavailableError("persistence returned an incomplete catalog");
      }
      return plain(catalog) as Catalog;
    },
  };
}
