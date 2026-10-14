// Copia en memoria del catálogo. El catálogo solo cambia al reiniciar el valle
// con otro archivo, así que se guarda unos segundos y las peticiones
// simultáneas comparten una sola consulta. Si la persistencia falla y hay una
// copia (aunque haya vencido), se sirve esa: el valle sigue mostrando precios
// durante una caída (RF-19).
import type { Catalog, CatalogSource } from "./catalog-source.js";

export interface CatalogEntry {
  readonly catalog: Catalog;
  /** Validador HTTP fuerte, derivado del hash del catálogo. */
  readonly etag: string;
}

export interface CatalogResult {
  readonly entry: CatalogEntry;
  /** La copia venció y no se pudo renovar: se sirve la anterior. */
  readonly stale: boolean;
  /** Motivo de la renovación fallida, solo si `stale`. */
  readonly error?: unknown;
}

export interface CachedCatalog {
  /** Lanza el error de la fuente solo si no hay ninguna copia que servir. */
  get(): Promise<CatalogResult>;
}

export interface CachedCatalogOptions {
  source: CatalogSource;
  /** Vigencia de la copia, en milisegundos. */
  ttlMs: number;
  now?: () => number;
}

export function createCachedCatalog(options: CachedCatalogOptions): CachedCatalog {
  const now = options.now ?? Date.now;
  let cached: { entry: CatalogEntry; at: number } | undefined;
  let inFlight: Promise<CatalogResult> | undefined;

  async function refresh(): Promise<CatalogResult> {
    try {
      const catalog = await options.source.getCatalog();
      const entry = { catalog, etag: `"${catalog.catalogVersion}"` };
      cached = { entry, at: now() };
      return { entry, stale: false };
    } catch (error) {
      if (cached) return { entry: cached.entry, stale: true, error };
      throw error;
    }
  }

  return {
    get() {
      if (cached && now() - cached.at < options.ttlMs) {
        return Promise.resolve({ entry: cached.entry, stale: false });
      }
      inFlight ??= refresh().finally(() => {
        inFlight = undefined;
      });
      return inFlight;
    },
  };
}
