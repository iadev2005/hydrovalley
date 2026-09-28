// Lógica de la comprobación de salud, separada del proceso para poder
// probarla sin abrir puertos ni terminar el proceso.
import type { Config } from "./config.js";

/** Tiempo máximo de la petición de salud. */
export const HEALTH_TIMEOUT_MS = 2000;

/**
 * URL de `/api/health` vista desde dentro del contenedor. Si la API escucha
 * en todas las interfaces (`0.0.0.0` o `::`), se pregunta por la de bucle
 * local; las direcciones IPv6 van entre corchetes.
 */
export function healthUrl(host: string, port: number): string {
  const target = host === "0.0.0.0" || host === "::" ? "127.0.0.1" : host;
  const authority = target.includes(":") ? `[${target}]` : target;
  return `http://${authority}:${String(port)}/api/health`;
}

type FetchLike = (url: string, init: { signal: AbortSignal }) => Promise<Response>;

export interface ProbeOptions {
  fetch?: FetchLike;
  timeoutMs?: number;
}

/**
 * Devuelve el código de salida del `HEALTHCHECK`: 0 si `/api/health`
 * responde 2xx dentro del plazo; 1 ante cualquier otra respuesta, error de
 * red o plazo vencido.
 */
export async function probeHealth(
  config: Pick<Config, "host" | "port">,
  { fetch: fetchFn = fetch, timeoutMs = HEALTH_TIMEOUT_MS }: ProbeOptions = {},
): Promise<0 | 1> {
  try {
    const res = await fetchFn(healthUrl(config.host, config.port), {
      signal: AbortSignal.timeout(timeoutMs),
    });
    return res.ok ? 0 : 1;
  } catch {
    return 1;
  }
}
