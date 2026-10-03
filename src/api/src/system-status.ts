// Agregador del panel del sistema (RNF-08, *Health Endpoint Monitoring*):
// junta el `/stats` del hub y el estado gRPC de world y persistence. Las tres
// consultas van en paralelo y cada una trae su propio plazo; una caída solo
// pone ese servicio en rojo, nunca tumba la respuesta. El resultado se guarda
// 1 s y las peticiones simultáneas comparten una sola agregación.
import type { ProbeResult } from "./grpc-health.js";
import type { HubResult, HubStats } from "./hub-stats.js";

export type { ProbeResult } from "./grpc-health.js";
export type { HubResult, HubStats } from "./hub-stats.js";

export interface ServiceStatus {
  readonly name: "nginx" | "api" | "hub" | "world" | "persistence";
  readonly status: "up" | "down";
  /** Motivo corto y seguro, solo si está caído. */
  readonly detail?: string;
}

export interface SystemSnapshot {
  /** ISO 8601 en UTC. */
  readonly generated_at: string;
  readonly services: readonly ServiceStatus[];
  /** Foto del hub, o `null` si no respondió. */
  readonly hub: HubStats | null;
}

export interface SystemStatus {
  get(): Promise<SystemSnapshot>;
}

export interface SystemStatusOptions {
  hub: () => Promise<HubResult>;
  world: () => Promise<ProbeResult>;
  persistence: () => Promise<ProbeResult>;
  now?: () => number;
  /** Vigencia de la copia en memoria (1 s por defecto). */
  cacheMs?: number;
}

const DEFAULT_CACHE_MS = 1000;
const FAILED: ProbeResult = { status: "down", detail: "error" };

const safe = <T>(call: () => Promise<T>, fallback: T): Promise<T> =>
  new Promise<T>((resolve) => {
    try {
      call().then(resolve, () => {
        resolve(fallback);
      });
    } catch {
      resolve(fallback);
    }
  });

const fromProbe = (name: "world" | "persistence", probe: ProbeResult): ServiceStatus =>
  probe.status === "up" ? { name, status: "up" } : { name, status: "down", detail: probe.detail };

export function createSystemStatus(options: SystemStatusOptions): SystemStatus {
  const now = options.now ?? Date.now;
  const cacheMs = options.cacheMs ?? DEFAULT_CACHE_MS;
  let cached: { snapshot: SystemSnapshot; at: number } | undefined;
  let inFlight: Promise<SystemSnapshot> | undefined;

  async function aggregate(): Promise<SystemSnapshot> {
    const [hub, world, persistence] = await Promise.all([
      safe<HubResult>(options.hub, { ok: false, detail: "error" }),
      safe(options.world, FAILED),
      safe(options.persistence, FAILED),
    ]);
    const generatedAt = now();
    return {
      generated_at: new Date(generatedAt).toISOString(),
      services: [
        // nginx y la API están arriba por construcción: la petición llegó
        // hasta aquí pasando por ambos.
        { name: "nginx", status: "up" },
        { name: "api", status: "up" },
        hub.ok
          ? { name: "hub", status: "up" }
          : { name: "hub", status: "down", detail: hub.detail },
        fromProbe("world", world),
        fromProbe("persistence", persistence),
      ],
      hub: hub.ok ? hub.stats : null,
    };
  }

  return {
    get() {
      if (cached && now() - cached.at < cacheMs) return Promise.resolve(cached.snapshot);
      inFlight ??= aggregate()
        .then((snapshot) => {
          cached = { snapshot, at: now() };
          return snapshot;
        })
        .finally(() => {
          inFlight = undefined;
        });
      return inFlight;
    },
  };
}
