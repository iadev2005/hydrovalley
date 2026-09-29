# API

Servicio en TypeScript (Node 22 LTS) que atiende el registro, el acceso con token firmado, y atenderá el catálogo, el ranking y los reportes por REST, documentado con Swagger (RES-04). Por ahora expone la salud del proceso, el registro, el acceso y la página de Swagger (ver [Pendiente](#pendiente)).

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
| `src/account-directory.ts` | Puerto `AccountDirectory` y sus errores; las rutas dependen de él, no de gRPC |
| `src/grpc-account-directory.ts` | Adaptador gRPC de `AccountService`: plazo de 2 s y reintentos acotados |
| `src/passwords.ts` | Argon2id (`@node-rs/argon2`) y hash de relleno |
| `src/tokens.ts` | Firma del JWT EdDSA con `jose` |
| `src/rate-limit.ts` | Límite de intentos de acceso en memoria |
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
| `TOKEN_PRIVATE_KEY` | Sí | — | Clave privada Ed25519 en PKCS#8 con la que se firman los tokens. Se admite el PEM tal cual, con los saltos de línea escritos como `\n`, o **el base64 del PEM completo** (una sola línea, a salvo de las comillas de un `.env`). Si falta o no es Ed25519, el arranque se detiene con un mensaje que nombra la variable, sin repetir el valor |
| `TOKEN_TTL_SECONDS` | No | `43200` (12 h) | Vigencia del token, en segundos (entero positivo) |
| `TOKEN_KEY_ID` | No | — | Se escribe como `kid` en la cabecera del token |
| `TRUST_PROXY` | No | `false` | Con `true`, la IP del cliente sale de `X-Forwarded-For` (detrás de nginx). Con `false`, el límite de intentos vería siempre la IP del proxy |
| `LOG_LEVEL` | No | `info` | Nivel de registro de pino (`fatal`, `error`, `warn`, `info`, `debug`, `trace` o `silent`). Un valor distinto detiene el arranque con un mensaje que nombra la variable y los valores admitidos |

## Claves del token

La API firma con la clave privada; el hub verifica con la pública (diagrama 09). Para generar el par:

```
openssl genpkey -algorithm ed25519 -out token-private.pem
openssl pkey -in token-private.pem -pubout -out token-public.pem
# Para una variable de entorno de una sola línea:
base64 < token-private.pem | tr -d '\n'
```

`TOKEN_PRIVATE_KEY` recibe la privada (PEM o su base64) y el hub recibe la pública. Ninguna va en el repositorio y la API no expone claves por HTTP.

## Rutas

| Ruta | Respuesta |
| --- | --- |
| `GET /api/health` | `200 {"status": "ok"}` mientras el proceso atiende |
| `POST /api/auth/register` | `201 {"token", "expires_at", "player_id", "username"}` (ya con sesión, sin pasar por el acceso); `400` datos no válidos; `409` el nombre ya existe; `503` persistencia no disponible |
| `POST /api/auth/login` | `200 {"token", "expires_at"}` (ISO 8601 en UTC); `400` cuerpo mal formado; `401` usuario o contraseña incorrectos; `429` demasiados intentos; `503` persistencia no disponible |
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
- Token: JWT con `alg: EdDSA` (Ed25519) y, si se define, `kid`. Campos: `sub` y `player_id` (el UUID de la cuenta), `username` (con la grafía del registro, que se pide a `GetUsernames` porque `GetAccount` no la devuelve), `role` (`player` o `admin`), `iat` y `exp` (12 h por defecto). `expires_at` es el `exp` en ISO 8601.
- **Límite de intentos:** 10 por minuto por usuario (sin distinguir mayúsculas) e IP; el undécimo recibe `429` con `Retry-After`. Es una ventana fija **en memoria y por instancia**: se pierde al reiniciar y, con varias réplicas, cada una cuenta aparte. Guarda como mucho 10.000 claves y expulsa la más antigua. Cuenta todos los intentos, correctos o no.
- Ni las contraseñas ni los hashes se escriben en el registro ni se devuelven; las respuestas solo llevan los campos declarados en su esquema.

### Llamadas a la persistencia

Plazo de 2 s por llamada. `GetAccount` y `GetUsernames` (idempotentes) se reintentan hasta 3 veces, con esperas de 100, 200 y 400 ms, solo ante `UNAVAILABLE` o plazo agotado. `ALREADY_EXISTS` → `409`, `INVALID_ARGUMENT` → `400`, `UNAVAILABLE` o plazo agotado → `503`; cualquier otro error → `500` con un mensaje genérico.

## Código generado

El cliente gRPC usa el código de protobuf-es v2 que genera `scripts/gen-proto.sh` en `gen/ts` (no se versiona). Como debe resolver `@bufbuild/protobuf` desde `node_modules` de este servicio, `npm run gen` lo copia a `src/gen/` (también sin versionar). Desde la raíz del repositorio:

```
scripts/gen-proto.sh
cd src/api && npm run gen
```

Sin ese paso, `typecheck`, `test` y `build` no encuentran los módulos. La imagen de Docker lo genera sola desde `proto/`.

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

## Pendiente

- Catálogo, ranking (`WorldService.GetRanking` y `AccountService.GetUsernames`) y reportes.
