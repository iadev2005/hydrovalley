# API

Servicio en TypeScript (Node 22 LTS) que atiende el registro, el acceso con token firmado, y atiende el ranking y el reporte de las 3R por REST, documentado con Swagger (RES-04); falta el catálogo. Por ahora expone la salud del proceso, el registro, el acceso, el ranking, el reporte, el estado del sistema y la página de Swagger (ver [Pendiente](#pendiente)).

## Por qué Fastify

- `inject` prueba las rutas sin abrir un puerto, así que las pruebas son rápidas y deterministas.
- Cada ruta declara su esquema JSON: valida la entrada, serializa la salida y alimenta el documento OpenAPI con `@fastify/swagger`, sin mantener la documentación aparte. Express necesitaría varias piezas de terceros para lo mismo.
- Tipos de TypeScript propios y buen rendimiento con poco código.

## Estructura

| Ruta | Qué contiene |
| --- | --- |
| `src/app.ts` | Montaje de la aplicación: rutas y documentación OpenAPI. No escucha: las pruebas la usan con `inject` |
| `src/config.ts` | Variables de entorno; valida la clave del token al arrancar |
| `src/auth-routes.ts` | `POST /api/auth/register` y `POST /api/auth/login`, con sus esquemas OpenAPI y la traducción de errores |
| `src/admin-routes.ts` | `POST /api/admin/bot-accounts`: token de administrador, límite por administrador, aprovisionamiento de cuentas de bots |
| `src/read-routes.ts` | `GET /api/ranking` y `GET /api/report`: token, límite por IP, esquemas OpenAPI y traducción de errores |
| `src/account-directory.ts` | Puerto `AccountDirectory` y sus errores; las rutas dependen de él, no de gRPC |
| `src/ranking-source.ts`, `src/report-source.ts`, `src/source-errors.ts` | Puertos `RankingSource` y `ReportSource` y sus errores de dominio (`SourceUnavailableError`, `SourceNotFoundError`, `SourceInvalidRequestError`) |
| `src/grpc-retry.ts` | Plazo, reintentos y presupuesto compartidos por todos los adaptadores gRPC |
| `src/grpc-account-directory.ts` | Adaptador gRPC de `AccountService` |
| `src/grpc-ranking-source.ts` | Adaptador gRPC de `WorldService.GetRanking` (`WORLD_ADDR`) |
| `src/grpc-report-source.ts` | Adaptador gRPC de `ReportService.GetReport` (`PERSISTENCE_ADDR`) |
| `src/system-routes.ts` | `GET /api/system`: límite por IP, esquema OpenAPI y respuesta del panel del sistema |
| `src/system-status.ts` | Agregador del panel: consultas en paralelo, copia de 1 s y una sola agregación en curso |
| `src/hub-stats.ts` | Cliente de `GET /stats` del hub (`HUB_STATS_ADDR`): valida la forma y copia solo los campos conocidos |
| `src/grpc-health.ts` | Cliente de `grpc.health.v1.Health/Check` descrito a mano (sin código generado), con plazo de 1 s y sin reintento |
| `src/passwords.ts` | Argon2id (`@node-rs/argon2`) y hash de relleno |
| `src/tokens.ts` | Firma y verificación del JWT EdDSA con `jose` |
| `src/rate-limit.ts` | Limitador de intentos en memoria (ventana fija) |
| `src/limits.ts` | Constantes de los límites (acceso, registro, panel, vigencia máxima del token); las usan `main.ts`, OpenAPI y este README |
| `src/gen/` | Copia local del código generado desde `proto/` (no se versiona; ver [Código generado](#código-generado)) |
| `src/main.ts` | Proceso: escucha y se detiene con SIGINT o SIGTERM |
| `src/healthcheck.ts` | Comprobación del `HEALTHCHECK` de Docker (la imagen no trae curl) |
| `src/health-probe.ts` | Lógica probada de esa comprobación: URL de bucle local, corchetes IPv6, plazo y código de salida |
| `test/` | Pruebas con Vitest |

## Variables de entorno

| Variable | Obligatoria | Valor por defecto | Uso |
| --- | --- | --- | --- |
| `API_LISTEN` | No | `0.0.0.0:3000` | Dirección y puerto del HTTP (`host:puerto`, `:puerto` o `[ipv6]:puerto`) |
| `PERSISTENCE_ADDR` | Sí | — | `host:puerto` del gRPC de la persistencia (plan B: `persistence:50051`; plan A: `10.68.17.x:50051`). Sin valor por defecto: ninguna dirección va en el código |
| `WORLD_ADDR` | Sí | — | `host:puerto` del gRPC del simulador, para el ranking (plan B: `world:50051`; plan A: `10.68.17.x:50051`). Sin valor por defecto: ninguna dirección va en el código |
| `HUB_STATS_ADDR` | Sí | — | `host:puerto` del HTTP interno del hub (`/stats`), para `GET /api/system` (plan B: `hub:8081`; plan A: `10.68.17.x:8081`). Sin valor por defecto: ninguna dirección va en el código |
| `TOKEN_PRIVATE_KEY` | Sí | — | Clave privada Ed25519 en PKCS#8 con la que se firman los tokens. Se admite el PEM tal cual, con los saltos de línea escritos como `\n`, o **el base64 del PEM completo** (una sola línea, a salvo de las comillas de un `.env`). Si falta o no es Ed25519, el arranque se detiene con un mensaje que nombra la variable, sin repetir el valor |
| `TOKEN_TTL_SECONDS` | No | `43200` (12 h) | Vigencia del token, en segundos: entero positivo de como máximo `604800` (7 días); un valor mayor detiene el arranque |
| `TOKEN_KEY_ID` | No | — | Se escribe como `kid` en la cabecera del token |
| `TRUST_PROXY` | No | `false` | `true` confía en **un solo salto** de proxy (nginx): la IP del cliente es la última entrada de `X-Forwarded-For`, porque nginx la sobrescribe con `$remote_addr` y una entrada anterior inventada por el cliente se ignora. También admite un número de saltos (1 a 10). Con `false`, el límite de intentos vería siempre la IP del proxy. `deploy/compose.yml` lo activa |
| `LOG_LEVEL` | No | `info` | Nivel de registro de pino (`fatal`, `error`, `warn`, `info`, `debug`, `trace` o `silent`). Un valor distinto detiene el arranque con un mensaje que nombra la variable y los valores admitidos |

## Claves del token

La API firma con la clave privada; el hub verifica con la pública (diagrama 09). Para generar el par:

```
openssl genpkey -algorithm ed25519 -out token-private.pem
openssl pkey -in token-private.pem -pubout -out token-public.pem
# Para una variable de entorno de una sola línea:
base64 < token-private.pem | tr -d '\n'
```

`TOKEN_PRIVATE_KEY` recibe la privada (PEM o su base64) y el hub recibe la pública. Ninguna va en el repositorio y la API no expone claves por HTTP. Para verificar el token de `/api/ranking` y `/api/report`, la API **deriva la pública de su propia privada** (`createPublicKey`): no hay otra variable que configurar.

## Rutas

| Ruta | Respuesta |
| --- | --- |
| `GET /api/health` | `200 {"status": "ok"}` mientras el proceso atiende |
| `POST /api/auth/register` | `201 {"token", "expires_at", "player_id", "username"}` (ya con sesión, sin pasar por el acceso); `400` datos no válidos; `409` el nombre ya existe; `503` persistencia no disponible |
| `POST /api/auth/login` | `200 {"token", "expires_at"}` (ISO 8601 en UTC); `400` cuerpo mal formado; `401` usuario o contraseña incorrectos; `429` demasiados intentos; `503` persistencia no disponible |
| `GET /api/ranking?limit=N` | `200 {"tick", "entries": [{"rank", "player_id", "username", "leaves_earned"}]}`; `400` parámetros no válidos; `401` sin token o inválido; `429` demasiadas consultas; `503` el simulador no está disponible |
| `GET /api/report?scope=me\|global&from_tick=&to_tick=` | `200 {"scope", "player_id" (solo `me`), "epoch", "from_tick", "to_tick", "totals": {…}}`; `400` parámetros no válidos; `401`; `404` sin instantáneas o sin cuenta; `429`; `503` |
| `GET /api/system` | `200 {"generated_at", "services": [{"name", "status": "up"\|"down", "detail"?}], "hub": {…} \| null}`; sin token; `429` demasiadas consultas. Ver [Estado del sistema](#estado-del-sistema) |
| `POST /api/admin/bot-accounts` | `201 {"accounts": [{"player_id", "username", "token", "expires_at"}]}`; `400` datos no válidos; `401` sin token o inválido; `403` el token no es de administrador; `429` demasiadas solicitudes; `413` cuerpo demasiado grande; `500`/`503 {"error", "created"}` fallo a mitad (sin tokens) |
| `GET /api/docs` | Página de Swagger UI |
| `GET /api/docs/json` | Documento OpenAPI 3.1 |

Todas cuelgan de `/api` porque nginx reenvía `/api/` sin reescribir la ruta. Los errores llevan la forma `{"error": "mensaje"}`.

### Registro

- `username`: de 3 a 32 caracteres `A-Z a-z 0-9 _ . -`, único sin distinguir mayúsculas (las mismas reglas que la persistencia). `password`: de 8 a 128 caracteres.
- La API calcula el hash **Argon2id** y solo el hash viaja a la persistencia. Parámetros: los mínimos de OWASP (Password Storage Cheat Sheet), memoria 19456 KiB (19 MiB), 2 iteraciones y 1 hilo; salida en formato PHC (`$argon2id$v=19$m=19456,t=2,p=1$…`). Están fijos en `src/passwords.ts`; si se suben, los hashes antiguos siguen verificándose porque los parámetros van dentro del hash.
- Tras crear la cuenta, la API firma el token con el `player_id` y el rol que devuelve `Register` (`player`), sin otra llamada a la persistencia; el token es el mismo del acceso.
- `Register` **no se reintenta** nunca (no es idempotente): un reintento tras un fallo podría chocar con una cuenta que sí se creó. El fallo se devuelve como `503`.

### Acceso

- Busca la cuenta con `GetAccount`. Si no existe, la contraseña se verifica contra un **hash de relleno** (mismos parámetros, calculado una vez al arrancar) y se responde el mismo `401` que para una contraseña incorrecta, de modo que ni el mensaje ni el tiempo delatan si el usuario existe.
- Token: JWT con `alg: EdDSA` (Ed25519) y, si se define, `kid`. Campos: `sub` y `player_id` (el UUID de la cuenta), `username` (con la grafía del registro, que se pide a `GetUsernames` porque `GetAccount` no la devuelve), `role` (`player` o `admin`), `iss` (`hydrovalley-api`), `aud` (`hydrovalley-play`), `iat` y `exp` (12 h por defecto). `iss` y `aud` son las constantes `TOKEN_ISSUER` y `TOKEN_AUDIENCE` de `src/tokens.ts`; el hub rechaza un token que no las lleve. `expires_at` es el `exp` en ISO 8601.
- **Límites de intentos** (429 con `Retry-After`; valores en `src/limits.ts`, que es la única fuente): acceso, 10 por minuto por usuario (sin distinguir mayúsculas) e IP y 30 por minuto por IP entre todos los usuarios; registro, 5 cada 10 minutos por IP. Son ventanas fijas **en memoria y por instancia**: se pierden al reiniciar y, con varias réplicas, cada una cuenta aparte. Cada limitador guarda hasta 10.000 claves: al llenarse solo se descartan las ventanas caducadas, nunca una clave bloqueada o dentro de su ventana, y si sigue lleno las claves nuevas reciben 429 (falla cerrado) hasta que caduque la más antigua.
- Si `GetUsernames` falla después de verificar la contraseña, el acceso no se rechaza: el token lleva el nombre que escribió el jugador y se registra un aviso.
- **Validación:** el registro aplica las reglas de nombre y contraseña; el acceso solo exige usuario (1 a 64 caracteres) y contraseña (1 a 128), a propósito, para que un nombre que nunca pudo registrarse reciba el mismo 401 que uno inexistente.
- Ni las contraseñas ni los hashes se escriben en el registro ni se devuelven; las respuestas solo llevan los campos declarados en su esquema.

### Ranking y reporte

Las dos rutas siguen el documento inicial (`GET /api/ranking`, `GET /api/report`; la tarea hablaba de `/api/reports/me` y `/api/reports/global`, que se resuelven con `scope`). Ambas exigen `Authorization: Bearer <token>` (el mismo del acceso) y comparten un límite por IP.

- **Token:** se verifica con la clave pública derivada de `TOKEN_PRIVATE_KEY`, solo `EdDSA`, y se exigen `iss` (`hydrovalley-api`), `aud` (`hydrovalley-play`), `exp` y los campos `player_id`, `username` y `role`, como hace el hub. Cualquier fallo es `401` con `WWW-Authenticate: Bearer`, sin decir el motivo.
- **Ranking:** `limit` de 1 a 100 (por defecto 20; cualquier otro valor es `400`). El orden, las posiciones con empate (1, 2, 2, 4) y el tope los decide el simulador (`WorldService.GetRanking`); la API solo completa `username` con **una** llamada a `AccountService.GetUsernames` por petición. Si un nombre no aparece, o la llamada falla, el ranking sale igual: el nombre es `jugador-` y los 8 primeros caracteres del identificador, y el fallo se registra como aviso.
- **Reporte:** `scope=me` (por defecto) toma el `player_id` del token, no de la petición, así que nadie consulta el reporte de otro jugador; `scope=global` suma a todos. `from_tick` y `to_tick` (enteros sin signo de hasta 64 bits, como texto) son opcionales; `from_tick` mayor que `to_tick` es `400`. Totales: `water_reduced` (Reducir, goteo), `water_reused` (Reutilizar, aspersores), `water_recycled` (Reciclar, planta) y los saldos de RF-27 (`water_refilled`, `water_spent`, `leaves_earned`, `leaves_spent`). Las semánticas (época actual, "en o antes de", cuenta sin filas con ceros) son las de `ReportService` (ver el README de la persistencia); el reporte puede ir hasta 5 s por detrás del valle.
- **Enteros de 64 bits:** `tick`, `leaves_earned`, `from_tick`, `to_tick` y los totales viajan como **texto** en el JSON (regla 7 de CLAUDE.md); `rank` y `epoch` son números normales.
- **Errores:** `NOT_FOUND` → `404`, `INVALID_ARGUMENT` → `400`, `UNAVAILABLE` o plazo agotado → `503`; cualquier otro → `500` con un mensaje genérico.
- **Límite por IP:** 60 consultas por minuto (`READ_LIMIT_PER_IP` en `src/limits.ts`), compartido entre las dos rutas, en memoria y por instancia, con `429` y `Retry-After`. Se comprueba antes que el token, así que una IP sin sesión tampoco puede forzar verificaciones de firma sin tope.
- Las tres llamadas (`GetRanking`, `GetReport`, `GetUsernames`) son idempotentes y usan la política de abajo.

### Cuentas de bots

`POST /api/admin/bot-accounts` crea jugadores normales (rol `player`) para los bots de `src/bots` sin pasar por el límite público de registro (5 cada 10 minutos por IP). Cuerpo: `{"count": 1..100, "prefix": "bot"}`; `prefix` es opcional (1 a 23 caracteres `A-Z a-z 0-9 _ . -`, para que `<prefix>-<8 hex>` quepa en 32 caracteres).

- **Autenticación:** `Authorization: Bearer <token>` verificado como en el ranking (clave pública derivada de `TOKEN_PRIVATE_KEY`, `EdDSA`, `iss`, `aud`, `exp`). Sin token o inválido: `401`; token válido de rol `player`: `403`. El token de administrador se obtiene con el acceso normal.
- **Cuentas:** cada una se llama `<prefix>-<8 hex aleatorios>` y lleva una contraseña aleatoria de 192 bits que solo existe en memoria: se convierte en hash Argon2id (mismos parámetros que el registro), nunca se devuelve ni se registra. Los bots entran con el token de la respuesta, no con contraseña. Cada token lleva los mismos campos y la misma vigencia (`TOKEN_TTL_SECONDS`) que el del acceso, con `role: player`.
- **`Register` no se reintenta** con el mismo nombre. Solo si responde `ALREADY_EXISTS` se genera otro nombre, hasta 5 intentos por cuenta (`BOT_NAME_ATTEMPTS`).
- **Fallo a mitad:** al primer error se deja de crear cuentas (las que ya iban en curso terminan) y se responde `{"error", "created": n}` **sin tokens**: `created` cuenta las cuentas ya registradas en la persistencia (incluida una cuyo token no llegó a firmarse). `503` solo si la persistencia no estuvo disponible (`UNAVAILABLE` o plazo agotado); un fallo de hash, de firma, de nombres agotados (5 `ALREADY_EXISTS` seguidos) o cualquier otro es `500` con mensaje genérico y se registra como error. Las `n` cuentas quedan huérfanas pero no estorban, porque los nombres son aleatorios; el cliente repite la solicitud completa. Otros errores 4xx del framework (p. ej. `413`) conservan su código con un mensaje genérico.
- **Límite:** 10 solicitudes por minuto por administrador (`ADMIN_LIMIT_PER_ADMIN` en `src/limits.ts`), en memoria y por instancia, con `429` y `Retry-After`. Se aplica después de verificar el token, así que un token inválido no gasta cupo.
- **Latencia esperada:** los hashes se calculan con concurrencia 4 (`BOT_HASH_CONCURRENCY`; cada uno usa ~19 MiB). `[DATO: tiempo medido de 100 cuentas en el laboratorio]`; en la práctica, con `count` 100 la respuesta tarda varios segundos, así que el cliente debe usar un plazo generoso (decenas de segundos).

### Llamadas a los demás servicios

Plazo de 2 s por intento, dentro de un presupuesto total de 3 s por llamada: el plazo de cada intento se recorta a lo que queda y no se empieza una espera que lo agotaría. `GetAccount`, `GetUsernames`, `GetRanking` y `GetReport` (idempotentes) se reintentan hasta 3 veces, con esperas de 100, 200 y 400 ms más hasta un 25 % de azar, solo ante `UNAVAILABLE` o plazo agotado. `ALREADY_EXISTS` → `409`, `INVALID_ARGUMENT` → `400`, `UNAVAILABLE` o plazo agotado → `503`; cualquier otro error → `500` con un mensaje genérico.

## Código generado

El cliente gRPC usa el código de protobuf-es v2 que genera `scripts/gen-proto.sh` en `gen/ts` (no se versiona). Como debe resolver `@bufbuild/protobuf` desde `node_modules` de este servicio, `npm run gen` lo copia a `src/gen/` (también sin versionar). Desde la raíz del repositorio:

```
scripts/gen-proto.sh
cd src/api && npm run gen
```

Sin ese paso, `typecheck`, `test` y `build` no encuentran los módulos. La imagen de Docker lo genera sola desde `proto/`.

Tanto la imagen como el CI (y `scripts/gen-proto.sh`) generan con plugins **remotos** de buf (`buf.build/...`), así que necesitan red. La versión de buf (1.73.0) está fijada en **tres sitios que deben coincidir**: `scripts/gen-proto.sh`, la etapa `proto` de `src/api/Dockerfile` (y las de los demás servicios) y el job `api` de `.github/workflows/ci.yml`. Al subirla, se cambian los tres.

## Pruebas

Desde `src/api/`, con Node 22 o superior, después de generar el código (ver arriba):

```
npm ci
npm run lint
npm run typecheck
npm test
```

## Ejecutar en local

```
npm run dev        # tsx, sin compilar
# o
npm run build && API_LISTEN=127.0.0.1:3000 npm start
```

## Imagen de Docker

El contexto es la raíz del repositorio, como en los demás servicios:

```
docker build -f src/api/Dockerfile -t hydrovalley-api .
docker run --rm -p 3000:3000 \
  -e PERSISTENCE_ADDR=persistence:50051 \
  -e TOKEN_PRIVATE_KEY="$(base64 < token-private.pem | tr -d '\n')" \
  hydrovalley-api
```

Genera el código de `proto/` con `bufbuild/buf` (necesita acceso a buf.build), compila con `node:22-trixie-slim`, instala solo las dependencias de producción y se ejecuta con el usuario `node`, sin privilegios. `HEALTHCHECK` ejecuta `node dist/healthcheck.js`. `Dockerfile.dockerignore` limita el contexto a los archivos de este servicio.

## Estado del sistema

`GET /api/system` alimenta el panel del sistema (RNF-08, *Health Endpoint Monitoring*). Es de solo lectura, no pide token y comparte nginx con el resto de `/api`. Siempre responde `200`: lo que no contesta sale en rojo, nunca tumba la respuesta.

- **Servicios:** `nginx` y `api` salen `up` por construcción (la petición llegó hasta aquí). `hub` sale `down` si `GET http://HUB_STATS_ADDR/stats` agota 1 s, responde distinto de 200 o devuelve un JSON que no encaja. `world` y `persistence` salen `down` si `grpc.health.v1.Health/Check` da `NOT_SERVING`, un error o agota 1 s (sin reintento). Se consulta el servicio `hydrovalley.world.v1.WorldService` en `WORLD_ADDR` y el estado general (`""`) en `PERSISTENCE_ADDR`, que la persistencia marca `SERVING` solo con la base de datos lista.
- **`detail`:** motivo corto y seguro, solo si está caído: `timeout`, `unavailable`, `not_serving`, `error`, `http_503`, `invalid_response`. Nunca trazas ni direcciones.
- **`hub`:** los campos de `/stats` del hub (ver su README), validados y copiados uno a uno (incluidos `boot_id` y `restored_tick`, el tick del que Rust se recuperó en el arranque que identifica `boot_id`; `null` si el hub es anterior a ese campo, para no marcarlo caído en un despliegue escalonado); el resto de los campos es obligatorio y los enteros de 64 bits siguen siendo texto. Es `null` si el hub está caído.
- **Copia de 1 s:** las tres consultas van en paralelo; el resultado se guarda 1 s en memoria y las peticiones simultáneas comparten una sola agregación, así que muchos visores no multiplican las llamadas internas.
- **Límite:** 120 por minuto por IP (`SYSTEM_LIMIT_PER_IP` en `src/limits.ts`; el panel consulta cada 2 s), en memoria y por instancia, con `429` y `Retry-After`.

Ejemplo (world parado):

```json
{
  "generated_at": "2026-10-03T12:00:00.000Z",
  "services": [
    { "name": "nginx", "status": "up" },
    { "name": "api", "status": "up" },
    { "name": "hub", "status": "up" },
    { "name": "world", "status": "down", "detail": "unavailable" },
    { "name": "persistence", "status": "up" }
  ],
  "hub": { "tick": "1234", "paused": true, "breaker": "open", "queue": 12, "sessions": 6, "goroutines": 25,
    "actions_per_second": { "admitted": 1.1, "applied": 0, "window_seconds": 10 },
    "latency_ms": { "p50": 77.8, "p95": 107.3, "samples": 11 },
    "step_ms": { "last": 200, "p95": 200, "failures_total": 3 },
    "humidity_total": "12345", "humidity_contributed": "99", "accepted_drops": "42",
    "active_fires": 2, "boot_id": "6f1c2f6e-0000-4000-8000-000000000001", "restored_tick": "50", "generated_at": "2026-10-03T12:00:00Z" }
}
```

## Pendiente

- Catálogo (`GET /api/catalog`).
