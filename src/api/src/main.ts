// Proceso de la API: lee la configuración, escucha y se detiene con SIGINT
// o SIGTERM.
import { buildApp } from "./app.js";
import { type Config, loadConfig } from "./config.js";

let config: Config;
try {
  config = loadConfig(process.env);
} catch (err) {
  // Error de configuración: una sola línea clara, sin traza, y salida con 1.
  console.error(`invalid configuration: ${err instanceof Error ? err.message : String(err)}`);
  process.exit(1);
}
const app = await buildApp({
  logger: { level: config.logLevel },
});

for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.once(signal, () => {
    app.log.info({ signal }, "shutting down");
    app.close().then(
      () => process.exit(0),
      (err: unknown) => {
        app.log.error(err, "shutdown failed");
        process.exit(1);
      },
    );
  });
}

await app.listen({ host: config.host, port: config.port });
