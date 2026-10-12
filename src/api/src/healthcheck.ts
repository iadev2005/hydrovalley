// Comprobación para el HEALTHCHECK de Docker: sale con 0 si /api/health
// responde 2xx dentro del propio contenedor (la imagen no trae curl). La
// lógica vive en health-probe.ts, que tiene pruebas.
import { loadListenConfig } from "./config.js";
import { probeHealth } from "./health-probe.js";

let code: 0 | 1;
try {
  code = await probeHealth(loadListenConfig(process.env));
} catch {
  // Configuración inválida: el contenedor no está sano.
  code = 1;
}
process.exit(code);
