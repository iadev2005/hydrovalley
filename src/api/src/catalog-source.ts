// Puerto hacia el catálogo de cifras del juego (D-01). Las rutas dependen de
// esta interfaz, no de gRPC. La forma es la de `catalog.proto` sin los campos
// internos del código generado: los enteros de 64 bits son `bigint` y los de
// 32 bits, `number`.
import type { Catalog as CatalogMessage } from "./gen/hydrovalley/persistence/v1/catalog_pb.js";

/** Un mensaje de protobuf sin los campos del código generado (`$typeName`…). */
export type Plain<T> = T extends bigint | number | string | boolean
  ? T
  : { [K in keyof T as K extends `$${string}` ? never : K]-?: Plain<NonNullable<T[K]>> };

export type Catalog = Plain<CatalogMessage>;

export interface CatalogSource {
  /** CatalogService.GetCatalog (idempotente). */
  getCatalog(): Promise<Catalog>;
}
