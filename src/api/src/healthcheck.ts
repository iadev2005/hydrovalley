// Comprobación para el HEALTHCHECK de Docker: sale con 0 si /api/health
// responde 200 dentro del propio contenedor (la imagen no trae curl).
import { loadConfig } from "./config.js";

const { host, port } = loadConfig(process.env);
const target = host === "0.0.0.0" || host === "::" ? "127.0.0.1" : host;
const url = `http://${target.includes(":") ? `[${target}]` : target}:${String(port)}/api/health`;

try {
  const res = await fetch(url, { signal: AbortSignal.timeout(2000) });
  process.exit(res.ok ? 0 : 1);
} catch {
  process.exit(1);
}
