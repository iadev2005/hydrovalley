import type { FetchFn } from "../logic/auth.ts";
import { createEndpointRotator, createFailoverFetch, type EndpointRotator } from "../logic/play.ts";

const BACKUPS_STORAGE_KEY = "hv.proxy.backups";

function readInitialBackups(): string[] {
  const sources: string[] = [];
  if (typeof window !== "undefined") {
    try {
      const params = new URLSearchParams(window.location.search);
      const fromQuery = params.get("backups") ?? params.get("backup");
      if (fromQuery) sources.push(fromQuery);
    } catch {
      // ignore URLSearchParams errors
    }
    try {
      const fromStorage = window.localStorage?.getItem(BACKUPS_STORAGE_KEY);
      if (fromStorage) sources.push(fromStorage);
      if (fromQueryAndStorageNeedsSave(sources)) {
        window.localStorage?.setItem(BACKUPS_STORAGE_KEY, sources[0] ?? "");
      }
    } catch {
      // ignore localStorage errors
    }
  }
  const envBackups = (import.meta as { env?: Record<string, string | undefined> }).env?.["VITE_BACKUP_HOSTS"];
  if (envBackups) sources.push(envBackups);
  return sources;
}

function fromQueryAndStorageNeedsSave(sources: string[]): boolean {
  return sources.length > 0 && Boolean(sources[0]);
}

function persistDiscoveredBackups(headerValue: string): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage?.setItem(BACKUPS_STORAGE_KEY, headerValue);
  } catch {
    // ignore storage errors
  }
}

/**
 * Rotador compartido de puntos de entrada (proxy primario y respaldos) para la
 * sesión del navegador: tanto `browserFetch` como `useGameSocket` y
 * `useChatSocket` consultan y avanzan esta lista cuando cae el proxy activo.
 */
export const sharedEndpointRotator: EndpointRotator = createEndpointRotator(
  typeof window !== "undefined" ? window.location.host : "localhost:8080",
  ...readInitialBackups(),
);

/**
 * `fetch` del navegador con identidad estable y conmutación automática a los
 * proxies de respaldo (`X-Proxy-Backups`, `?backups=...` o `VITE_BACKUP_HOSTS`).
 * Se usa como valor por defecto de los hooks: si se creara con
 * `window.fetch.bind(window)` dentro de la firma de cada hook, sería una
 * función nueva en cada render, haría cambiar las dependencias de
 * `useCallback`/`useEffect` y reiniciaría el sondeo sin parar.
 * Llama a `window.fetch` en cada petición, así que respeta cualquier sustituto
 * instalado después de cargar el módulo.
 */
export const browserFetch: FetchFn | undefined =
  typeof window !== "undefined"
    ? (createFailoverFetch({
        baseFetch: (...args: Parameters<typeof fetch>) => window.fetch(...args),
        rotator: sharedEndpointRotator,
        protocol: window.location.protocol,
        onBackupsDiscovered: persistDiscoveredBackups,
      }) as FetchFn)
    : undefined;
