// Proceso de la API: lee la configuración, escucha y se detiene con SIGINT
// o SIGTERM.
import { buildApp, trustHops } from "./app.js";
import { createPublicKey } from "node:crypto";
import { type Config, loadConfig } from "./config.js";
import { createGrpcHealthProbe } from "./grpc-health.js";
import { createHubStatsSource } from "./hub-stats.js";
import { createSystemStatus } from "./system-status.js";
import { createGrpcAccountDirectory } from "./grpc-account-directory.js";
import { createGrpcRankingSource } from "./grpc-ranking-source.js";
import { createGrpcReportSource } from "./grpc-report-source.js";
import { createPasswordService } from "./passwords.js";
import {
  ADMIN_LIMIT_PER_ADMIN,
  LIMITER_MAX_KEYS,
  LOGIN_LIMIT_PER_IP,
  LOGIN_LIMIT_PER_USER_IP,
  READ_LIMIT_PER_IP,
  REGISTER_LIMIT_PER_IP,
  SYSTEM_LIMIT_PER_IP,
  type RateLimit,
} from "./limits.js";
import { createAttemptLimiter } from "./rate-limit.js";
import { createTokenService, createTokenVerifier } from "./tokens.js";

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
const accounts = createGrpcAccountDirectory({
  address: config.persistenceAddr,
});
const passwords = await createPasswordService();
const tokenVerifier = createTokenVerifier({
  // La pública se deriva de la privada: no hay otra variable que configurar.
  publicKey: createPublicKey(config.tokenPrivateKey),
});
const tokenService = createTokenService({
  privateKey: config.tokenPrivateKey,
  ttlSeconds: config.tokenTtlSeconds,
  keyId: config.tokenKeyId,
});
const app = await buildApp(
  {
    logger: { level: config.logLevel },
    trustProxy: config.trustProxy === false ? false : trustHops(config.trustProxy),
  },
  {
    accounts,
    // Calcula el hash de relleno una sola vez, antes de escuchar.
    passwords,
    tokens: tokenService,
    // Límites de limits.ts; en memoria y por instancia.
    loginLimiter: limiter(LOGIN_LIMIT_PER_USER_IP),
    loginIpLimiter: limiter(LOGIN_LIMIT_PER_IP),
    registerIpLimiter: limiter(REGISTER_LIMIT_PER_IP),
  },
  {
    ranking: createGrpcRankingSource({ address: config.worldAddr }),
    reports: createGrpcReportSource({ address: config.persistenceAddr }),
    accounts,
    tokens: tokenVerifier,
    readLimiter: limiter(READ_LIMIT_PER_IP),
  },
  {
    accounts,
    passwords,
    tokens: tokenService,
    verifier: tokenVerifier,
    adminLimiter: limiter(ADMIN_LIMIT_PER_ADMIN),
  },
  {
    status: createSystemStatus({
      hub: createHubStatsSource({ address: config.hubStatsAddr }),
      world: createGrpcHealthProbe({
        address: config.worldAddr,
        service: "hydrovalley.world.v1.WorldService",
      }),
      // Persistencia marca SERVING en todos sus nombres solo con la base de
      // datos lista; el nombre vacío es el estado general del servidor.
      persistence: createGrpcHealthProbe({
        address: config.persistenceAddr,
        service: "",
      }),
    }),
    limiter: limiter(SYSTEM_LIMIT_PER_IP),
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
