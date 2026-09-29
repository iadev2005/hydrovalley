// Proceso de la API: lee la configuración, escucha y se detiene con SIGINT
// o SIGTERM.
import { buildApp, trustHops } from "./app.js";
import { type Config, loadConfig } from "./config.js";
import { createGrpcAccountDirectory } from "./grpc-account-directory.js";
import { createPasswordService } from "./passwords.js";
import {
  LIMITER_MAX_KEYS,
  LOGIN_LIMIT_PER_IP,
  LOGIN_LIMIT_PER_USER_IP,
  REGISTER_LIMIT_PER_IP,
  type RateLimit,
} from "./limits.js";
import { createAttemptLimiter } from "./rate-limit.js";
import { createTokenService } from "./tokens.js";

const limiter = ({ limit, windowMs }: RateLimit) =>
  createAttemptLimiter({ limit, windowMs, maxKeys: LIMITER_MAX_KEYS });

let config: Config;
try {
  config = loadConfig(process.env);
} catch (err) {
  // Error de configuración: una sola línea clara, sin traza, y salida con 1.
  console.error(`invalid configuration: ${err instanceof Error ? err.message : String(err)}`);
  process.exit(1);
}
const app = await buildApp(
  {
    logger: { level: config.logLevel },
    trustProxy: config.trustProxy === false ? false : trustHops(config.trustProxy),
  },
  {
    accounts: createGrpcAccountDirectory({ address: config.persistenceAddr }),
    // Calcula el hash de relleno una sola vez, antes de escuchar.
    passwords: await createPasswordService(),
    tokens: createTokenService({
      privateKey: config.tokenPrivateKey,
      ttlSeconds: config.tokenTtlSeconds,
      keyId: config.tokenKeyId,
    }),
    // Límites de limits.ts; en memoria y por instancia.
    loginLimiter: limiter(LOGIN_LIMIT_PER_USER_IP),
    loginIpLimiter: limiter(LOGIN_LIMIT_PER_IP),
    registerIpLimiter: limiter(REGISTER_LIMIT_PER_IP),
  },
);

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
