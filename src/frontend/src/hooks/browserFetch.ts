import type { FetchFn } from "../logic/auth.ts";

/**
 * `fetch` del navegador con identidad estable. Se usa como valor por defecto
 * de los hooks: si se creara con `window.fetch.bind(window)` dentro de la
 * firma de cada hook, sería una función nueva en cada render, haría cambiar las
 * dependencias de `useCallback`/`useEffect` y reiniciaría el sondeo sin parar.
 * Llama a `window.fetch` en cada petición, así que respeta cualquier sustituto
 * instalado después de cargar el módulo.
 */
export const browserFetch: FetchFn | undefined =
  typeof window !== "undefined"
    ? (((...args: Parameters<typeof fetch>) => window.fetch(...args)) as FetchFn)
    : undefined;
