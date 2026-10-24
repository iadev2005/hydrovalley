# Despliegue

| Ruta | Qué contiene |
| --- | --- |
| `compose.yml` | Plan B: todo en una máquina, un contenedor por servicio (diagrama `08-despliegue-plan-b`) |
| `.env.example` | Plantilla de `deploy/.env` (credenciales de PostgreSQL, claves del token, administrador, puerto del host y etiqueta de las imágenes). `.env` nunca se versiona |
| `nginx/` | Proxy de entrada: plantilla de configuración, la imagen (que incluye el frontend) y el script que prepara el certificado TLS |
| `nodes/` | Plan A: un archivo por máquina (pendiente) |

## Plan B

### Desarrollo local (con construcción)

```bash
cp deploy/.env.example deploy/.env      # y rellenar los valores (ver «Claves y variables»)
docker compose -f deploy/compose.yml up --build
```

### Despliegue en el laboratorio (con imágenes del pipeline, sin `--build`)

En las máquinas del laboratorio no se compila nada: se descargan las imágenes publicadas por el CI en `ghcr.io` para `linux/amd64` (cubriendo RF-21 y RES-06):

```bash
cp deploy/.env.example deploy/.env      # y rellenar los valores (HV_TAG=dev o la versión deseada)
HV_TAG=dev docker compose -f deploy/compose.yml pull
HV_TAG=dev docker compose -f deploy/compose.yml up -d
```

Para incluir la imagen de los bots en la descarga:

```bash
HV_TAG=dev docker compose -f deploy/compose.yml --profile bots pull
```

Luego, https://localhost:8080 (certificado autofirmado: el navegador avisa; ver «TLS»). Solo nginx publica un puerto (`HV_HTTP_PORT`, 8080 por defecto); PostgreSQL no se publica. Los servicios se encuentran por su nombre (`world:50051`, `persistence:50051`…) mediante variables de entorno.

- **Arranque**: no se usa `depends_on`. Cada servicio arranca aunque los demás no estén y se reconecta solo (regla 5); `restart: unless-stopped` cubre las caídas. Hasta que la persistencia conecta con PostgreSQL y el simulador carga su instantánea, el hub tiene el valle en pausa.
- **Healthchecks**: van en cada imagen, para que el plan A los herede. Hub: `hub healthcheck` (`/healthz`). Simulador y persistencia: `grpc_health_probe` contra `grpc.health.v1` (SERVING solo con la instantánea cargada o la base conectada). API: `node dist/healthcheck.js`. nginx: `wget` a `/healthz` por HTTP en `127.0.0.1:8079` (solo bucle local, sin publicar; ver «TLS»). PostgreSQL: `pg_isready`, en `compose.yml`.

### Sustituir Rust por world-go (plan B de D-03)

Si el simulador en Rust falla, `world-go` (Go) ocupa su lugar sin tocar el hub ni la API: el mismo servicio `world`, el mismo puerto y las mismas variables. Es solo configuración (reglas 1 y 6 de portabilidad); no se despliega por defecto.

```bash
docker compose -f deploy/compose.yml -f deploy/compose.world-go.yml up -d --build   # o sin --build, con las imágenes del pipeline
```

`deploy/compose.world-go.yml` cambia solo `image` y `build` del servicio `world`. Con las imágenes del pipeline: `HV_TAG=dev docker compose -f deploy/compose.yml -f deploy/compose.world-go.yml pull world`. Reglas:

- **Escritor único (regla 1).** Un solo simulador contra el hub: `world-go` sustituye a Rust, nunca corre a la vez. Para volver a Rust, se levanta otra vez sin el segundo archivo (`docker compose -f deploy/compose.yml up -d world`).
- **El valle sigue donde estaba.** `world-go` carga la última instantánea que guardó Rust (y al revés): los dos escriben el mismo formato, byte a byte (prueba de conformidad, `src/world-go/README.md`). El tick no retrocede; lo único que se pierde son los ticks posteriores a esa instantánea (≤ 50, 5 s), como en cualquier reinicio de Rust.
- **Mismo contrato.** Variables `WORLD_LISTEN`, `PERSISTENCE_ADDR`, `WORLD_SEED` y `LOG_LEVEL`; salud con `grpc_health_probe` (SERVING solo con el mundo cargado); `boot_id` y `restored_tick` en cada `Step`; instantánea cada 50 ticks y una final al parar.
- **Pruebas.** `E2E_WORLD_IMPL=go tests/e2e/run.sh` corre toda la suite de punta a punta contra `world-go` (ver «Prueba de humo»).

## Imágenes en ghcr.io (pipeline de CI)

El job `publish` de `.github/workflows/ci.yml` construye con `docker buildx bake` para la plataforma `linux/amd64` y publica automáticamente las seis imágenes del sistema, más la de `world-go` (el plan B del simulador, que no está en `compose.yml`):

| Servicio Compose | Imagen publicada | Descripción |
| --- | --- | --- |
| `persistence` | `ghcr.io/iadev2005/hydrovalley-persistence:${HV_TAG}` | Base de datos PostgreSQL (Python) |
| `world` | `ghcr.io/iadev2005/hydrovalley-world:${HV_TAG}` | Simulador del valle (Rust) |
| `hub` | `ghcr.io/iadev2005/hydrovalley-hub:${HV_TAG}` | Hub WebSocket en tiempo real (Go) |
| `api` | `ghcr.io/iadev2005/hydrovalley-api:${HV_TAG}` | API REST de autenticación y catálogo (TypeScript) |
| `nginx` | `ghcr.io/iadev2005/hydrovalley-proxy:${HV_TAG}` | Proxy inverso y frontend compilado (nginx) |
| `bots` | `ghcr.io/iadev2005/hydrovalley-bots:${HV_TAG}` | Bots simuladores y pruebas de carga (Go, perfil `bots`) |
| `world` (con `compose.world-go.yml`) | `ghcr.io/iadev2005/hydrovalley-world-go:${HV_TAG}` | Simulador del valle en Go, plan B de D-03: sustituye a la imagen de Rust |

> Nota: PostgreSQL no se compila en el repositorio; usa la imagen oficial `postgres:16` de Docker Hub.

### Etiquetas publicadas

- `:dev`: Se publica en cada `push` a la rama `dev` tras fusionar un PR. Usada para pruebas continuas y ensayos de integración en el laboratorio.
- `:vX.Y` (ej. `:v0.1`): Se publica al crear y subir una etiqueta Git `v*` sobre un commit de `main`. Garantiza una versión fija, inmutable y reproducible para la defensa y entrega final.

### Visibilidad de los paquetes y acceso en el laboratorio (Decisión D1)

Al ser un repositorio privado (`iadev2005/hydrovalley`), los paquetes en GitHub Packages nacen como privados por defecto. Existen dos formas de descargarlos en el laboratorio:

1. **Opción recomendada (Paquetes públicos):** Tras la primera publicación por el CI, el administrador del repositorio cambia la visibilidad de los 6 paquetes a *Público* en la interfaz de GitHub (*Package settings → Danger Zone → Change visibility*). El código fuente del repositorio permanece completamente privado, pero las máquinas del laboratorio pueden hacer `docker compose pull` directamente sin configurar credenciales ni iniciar sesión.
2. **Opción con autenticación (Paquetes privados):** Si se mantienen privados, cada integrante debe generar un Personal Access Token (PAT) clásico con permiso exclusivo `read:packages`. En la máquina del laboratorio, iniciar sesión antes de descargar:
   ```bash
   echo "$PAT" | docker login ghcr.io -u <usuario_github> --password-stdin
   ```
   *Seguridad en máquinas compartidas:* Al terminar la sesión de trabajo o ensayo en el laboratorio, cerrar sesión con:
   ```bash
   docker logout ghcr.io
   ```

## Claves y variables

`deploy/.env` (nunca se versiona; `.gitignore` lo excluye) alimenta a `compose.yml`. Sin las obligatorias, `docker compose` se detiene con un mensaje que nombra la variable.

| Variable | Obligatoria | Quién la lee | Qué es |
| --- | --- | --- | --- |
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | Sí | PostgreSQL y persistencia | Credenciales de la base. Compose arma con ellas `DATABASE_URL` para la persistencia, así que no se define aparte; la clave no debe llevar `@ : / ? #` |
| `TOKEN_PRIVATE_KEY` | Sí | API | Clave privada Ed25519 con la que se firman los tokens (base64 del PEM, una línea) |
| `TOKEN_PUBLIC_KEY` | Sí | Hub | Su pareja pública, para verificar el token de `/play` (base64 del PEM, una línea) |
| `ADMIN_USERNAME`, `ADMIN_PASSWORD_HASH` | No, van juntas | Persistencia | Cuenta de administrador que se siembra al arrancar (RF-31). Sin ellas no hay administrador; con una sola, la persistencia no arranca |
| `TOKEN_TTL_SECONDS` | No | API | Vigencia del token: 43200 (12 h) por defecto, máximo 604800 |

La pareja de claves se genera de una vez y se pasa a una sola línea:

```
openssl genpkey -algorithm ed25519 -out priv.pem && openssl pkey -in priv.pem -pubout -out pub.pem
echo "TOKEN_PRIVATE_KEY=$(base64 < priv.pem | tr -d '\n')"
echo "TOKEN_PUBLIC_KEY=$(base64 < pub.pem | tr -d '\n')"
rm priv.pem pub.pem
```

Las dos líneas que imprime van a `deploy/.env`. La privada solo la tiene la API; el hub recibe únicamente la pública. Cambiar el par invalida los tokens ya emitidos (los jugadores vuelven a entrar). En Windows sin `openssl` ni `base64`, se puede generar en WSL o con Git Bash.

El hash Argon2id del administrador usa los mismos parámetros que la API (`m=19456,t=2,p=1`). Con la imagen de la API (la contraseña entra por la entrada estándar, así no queda en el historial del shell ni en la lista de procesos):

```
read -rs -p "Contraseña del administrador: " pw; echo
printf '%s' "$pw" | docker run --rm -i ghcr.io/iadev2005/hydrovalley-api:dev node --input-type=module -e '
import { hash } from "@node-rs/argon2";
let pw = ""; for await (const c of process.stdin) pw += c;
console.log(await hash(pw, { algorithm: 2, memoryCost: 19456, timeCost: 2, parallelism: 1 }));'
```

Con Node y `@node-rs/argon2` instalados (por ejemplo en `src/api/` tras `npm ci`) sirve el mismo programa sin `docker run`. El resultado empieza por `$argon2id$v=19$…` y va **entre comillas simples** en `deploy/.env` (lleva `$`, que Compose interpretaría). La contraseña en claro no se guarda en ninguna parte.

Líneas que el encargado debe añadir a `deploy/.env.example` (solo marcadores: ningún valor real):

```
# Claves del token (obligatorias). Se generan con el comando de deploy/README.md
# («Claves y variables»); base64 del PEM, en una sola línea.
TOKEN_PRIVATE_KEY=
TOKEN_PUBLIC_KEY=

# Administrador (opcionales, van juntas). El hash Argon2id va entre comillas simples.
ADMIN_USERNAME=
ADMIN_PASSWORD_HASH=

# Vigencia del token en segundos (opcional; 43200 = 12 h, máximo 604800).
# TOKEN_TTL_SECONDS=43200

# TLS de nginx (opcional, ver «TLS»): nombres o IP adicionales que se suman al
# SAN del certificado autofirmado, separados por comas. Deben ser los nombres o
# las IP por los que los clientes entran a nginx (en el laboratorio, la IP
# 10.68.17.x de la máquina); si no coinciden, la verificación falla. No afecta
# a un certificado montado.
# TLS_EXTRA_SANS=

# Bots (opcionales; solo con el perfil «bots», ver «Bots»). La contraseña va
# en claro y es la del administrador de ADMIN_USERNAME, no su hash.
# HV_TLS_INSECURE=false   # por defecto es true: sin verificar, solo dentro de la red de Compose; en el laboratorio, false y HV_CA_CERT
# HV_CA_CERT=
# HV_ADMIN_USERNAME=
# HV_ADMIN_PASSWORD=
# BOTS_COUNT=
# BOTS_DURATION=
# BOTS_SEED=
# BOTS_RESET=
# BOTS_REPORT_PATH=
# BOTS_LOAD_RANKING=
# BOTS_LOAD_RANKING_INTERVAL=
# BOTS_LOAD_BASELINE_REPORT=
```

`tests/e2e/run.sh` no lee estas claves de `.env.example`: genera un par efímero en cada ejecución.

## Bots

El servicio `bots` (`src/bots/`) está en `compose.yml` bajo el perfil `bots`: `up` no lo arranca. Corre, imprime su resumen y termina; habla solo con nginx dentro de la red de Compose (`HV_BASE_URL=https://nginx:8080`), como cualquier jugador. Como nginx usa un certificado autofirmado, el servicio `bots` de `compose.yml` omite la verificación por defecto (`HV_TLS_INSECURE=true`) y lo avisa con un WARN al arrancar; eso solo es aceptable dentro de la red de Compose del plan B, donde el tráfico no sale de la máquina. En el laboratorio, usar `HV_CA_CERT`. Para verificar de verdad: `HV_TLS_INSECURE=false` y `HV_CA_CERT` con la ruta, dentro del contenedor, del certificado en que confiar (montado con un volumen). Necesita un administrador (RF-31) para crear sus cuentas: `HV_ADMIN_USERNAME` es el `ADMIN_USERNAME` sembrado y `HV_ADMIN_PASSWORD` su contraseña en claro (no el hash). El stack tiene que estar levantado.

```
docker compose -f deploy/compose.yml --profile bots run --rm bots neighbors     # vecinos (hasta Ctrl-C si BOTS_DURATION=0)
docker compose -f deploy/compose.yml --profile bots run --rm bots load-test     # CP-11: 30 bots, 60 s (también `load`)
docker compose -f deploy/compose.yml --profile bots run --rm bots cheater       # CP-03: un tramposo, 10 s de inundación
docker compose -f deploy/compose.yml --profile bots run --rm bots firefighter   # CP-04: 3 bomberos, 120 s
```

Las variables (`HV_ADMIN_USERNAME`, `HV_ADMIN_PASSWORD`, `BOTS_COUNT`, `BOTS_DURATION`, `BOTS_SEED`, `BOTS_RESET`, `BOTS_REPORT_PATH`, y solo para `load-test`: `BOTS_LOAD_RANKING`, `BOTS_LOAD_RANKING_INTERVAL` y `BOTS_LOAD_BASELINE_REPORT`) salen de `deploy/.env` o del entorno del comando; vacías, cada escenario usa su valor por defecto (ver `src/bots/README.md`). Sin las del administrador el binario se detiene con un mensaje que las nombra. Salida: 0 si el escenario cumple, 1 si no. `load-test`, `cheater` y `firefighter` imprimen `Resultado: CUMPLE` o `Resultado: NO CUMPLE`.

El informe JSON (`BOTS_REPORT_PATH`) se escribe dentro del contenedor, que no tiene carpeta compartida (regla 3) y se borra con `--rm`: para conservarlo, quitar `--rm` y copiarlo con `docker cp`, o dejarse con el resumen por la salida estándar.

### Prueba de carga (CP-11 y RNF-03)

`tests/load/run.sh` levanta el stack por HTTPS con un certificado efímero y claves y administrador efímeros (las mismas piezas que `tests/e2e/run.sh`, en `tests/lib/stack.sh`) y corre `load-test` dos veces: sin ranking (línea base) y con `BOTS_LOAD_RANKING=true`, que consulta `GET /api/ranking` cada `BOTS_LOAD_RANKING_INTERVAL` (2 s por defecto). Los informes quedan en `reports/` (ignorada por git); la variante con ranking usa siempre la línea base medida en esa misma ejecución (`LOAD_VARIANT=ranking` solo la toma de `LOAD_BASELINE_REPORT`), y la contraseña del administrador viaja por el entorno del proceso, no por los argumentos. La espera a que el stack esté listo (`stack_wait_all_up`) es la misma de `tests/e2e/run.sh`, en `tests/lib/stack.sh`. RNF-03 compara el p95 de las acciones con ranking contra el de la línea base (`BOTS_LOAD_BASELINE_REPORT`): debe ser <= 200 ms y <= línea base + 20 %; ese 20 % es un umbral del equipo, no del documento inicial. Variables, detalle y cifras en `src/bots/README.md`.

```
tests/load/run.sh                              # 30 bots, 60 s, las dos variantes
LOAD_DURATION=20s LOAD_BOTS=10 HV_HTTP_PORT=18091 tests/load/run.sh   # versión corta
```

En GitHub se lanza a mano desde *Actions › load-test › Run workflow* (`workflow_dispatch`); el resumen del job trae las cifras y la máquina del *runner*, y los JSON salen como artefacto `load-test-reports`.

## Proxy (nginx)

| Ruta | Destino |
| --- | --- |
| `/` | Archivos estáticos del frontend, dentro de la misma imagen |
| `/api/` | `API_ADDR` (plan B: `api:3000`), sin reescribir la ruta; Swagger en `/api/docs` |
| `/play` | `HUB_ADDR` (plan B: `hub:8081`), con las cabeceras de WebSocket y `Host $http_host` (el hub compara `Origin` con `Host`, puerto incluido) |
| `/chat` | 503 hasta que exista el chat (nivel 2) |
| `/healthz` | 200, para el healthcheck |

**Registros sin el token**: `/play?token=…` lleva el token firmado en la consulta de la URL. nginx usa el formato `hv_safe`, que registra `$uri` (la ruta, sin consulta) en lugar de `$request` o `$request_uri`, y nunca `$args`, `$query_string` ni el `Referer`. El registro de errores copia la línea de la petición completa, así que en `/play` solo se registran los errores críticos (`error_log … crit`); los fallos del hub se ven en el estado del registro de acceso y en los registros del hub. Quien añada un `log_format` o un `access_log` debe mantener esa regla. `/healthz` no se registra.

Los destinos se resuelven en cada petición con el DNS de Docker (`NGINX_RESOLVER`, 127.0.0.11), así nginx arranca aunque la API o el hub todavía no estén.

## TLS (RNF-12)

nginx termina TLS en el 8080, el único puerto publicado: `https://…:8080` para la página y la API, `wss://…:8080/play` para el juego (el frontend elige `wss:` cuando la página es `https:`). El HTTP plano que llegue a ese mismo puerto recibe un `308` (conserva el método de un POST) hacia `https://` con el mismo host, puerto, ruta y consulta. TLS 1.2 y 1.3, sin HSTS (el certificado es autofirmado y el navegador no debe recordar un aviso que no puede resolver). El tráfico interno (gRPC, HTTP entre servicios) sigue sin cifrar, riesgo aceptado en `CLAUDE.md`.

**Certificado.** `deploy/nginx/docker-entrypoint.d/40-tls-cert.sh` corre antes de nginx:

- Si en `TLS_CERT_FILE` y `TLS_KEY_FILE` (por defecto `/etc/nginx/tls/tls.crt` y `/etc/nginx/tls/tls.key`) ya hay un par, lo usa tal cual (el de un volumen montado nunca se toca).
- Si no, genera un certificado autofirmado (ECDSA P-256, 825 días, `CN=hydrovalley-proxy`) en esas rutas. Nace al crearse el contenedor: ni la clave ni el certificado van en el repositorio ni en la imagen publicada en `ghcr.io`. Un contenedor reiniciado lo reutiliza, y lo renueva si le quedan menos de 30 días; recrearlo (o cambiar `TLS_EXTRA_SANS`) genera uno nuevo.
- SAN: `localhost`, `nginx` y `127.0.0.1`, más lo de `TLS_EXTRA_SANS` (lista separada por comas; lo que parece una IP va como IP y el resto como nombre). En el laboratorio hay que incluir la IP por la que se entra, para que el nombre coincida:

```
TLS_EXTRA_SANS=10.68.17.1,labbd-01 docker compose -f deploy/compose.yml up -d
```

**Un certificado propio (el del laboratorio).** Montar una carpeta con `tls.crt` y `tls.key` en `/etc/nginx/tls`, de solo lectura (el usuario 101 de nginx debe poder leerla):

```yaml
services:
  nginx:
    volumes:
      - ./tls:/etc/nginx/tls:ro
```

Otras rutas: `TLS_CERT_FILE` y `TLS_KEY_FILE`. `tests/e2e/compose.tls.yml` es un ejemplo completo.

**Healthcheck.** Un segundo `server`, en HTTP y solo en `127.0.0.1:8079`, atiende `/healthz` para el `HEALTHCHECK` de la imagen; no se publica. El `/healthz` del 8080 sigue ahí (por HTTPS).

**Aviso del navegador y confianza.** Con un certificado autofirmado el navegador avisa («la conexión no es privada»): se acepta una vez por navegador. Para evitarlo, importar `tls.crt` como autoridad de confianza (`docker compose cp nginx:/etc/nginx/tls/tls.crt .`) o usar `curl --cacert tls.crt`. Las herramientas de línea de comandos: `curl -k` (solo para probar), `NODE_EXTRA_CA_CERTS=tls.crt` en Node, `HV_CA_CERT` o `HV_TLS_INSECURE=true` en los bots. El nombre al que se entra debe estar en el SAN: `localhost` y `nginx` siempre; el resto, con `TLS_EXTRA_SANS`.

## Identificador de petición (RNF-07)

Cada petición se puede seguir de punta a punta por un identificador, sin cambiar de herramienta:

- **REST (`/api`).** nginx toma el `X-Request-Id` del cliente si es válido (UUID o 32 caracteres hexadecimales, de 64 como máximo) y, si falta o no lo es, usa su `$request_id`. Lo reenvía a la API, lo devuelve en la respuesta (también en un 502 o 504 que genere nginx) y lo registra en su línea de acceso como `rid=<id>` (como siempre, sin la consulta de la URL). La API lo valida otra vez (genera un UUID si no sirve), lo pone en cada línea de registro de esa petición (campo `reqId`) y lo envía como metadato gRPC `x-request-id` en sus llamadas a persistencia y al simulador. Persistencia lo escribe en la línea de cada llamada (`request_id=<id>`) y el simulador en la de `GetRanking` (`get_ranking{request_id=<id>}`); `-` si no viene.
- **Llamadas internas del simulador a persistencia** (`GetCatalog`, `LoadSnapshot`, `SaveSnapshot`): el simulador genera un id por llamada (`world-<uuid>`) y lo envía como `x-request-id`; el mismo id aparece en su registro y en el de persistencia.
- **Juego (`/play`).** El identificador es el `action_id` de cada acción. El hub lo registra a nivel `debug` (admisión y resultado final; con `LOG_LEVEL=debug`) para no inundar el registro `info` con cien gotas por segundo. En cada `Step` envía `x-request-id: tick-<n>` (`n` es el tick), y el simulador lo escribe en su registro `debug` del `Step` y de cada comando con su `action_id`, así que un `Step` se encuentra en los dos.

Seguir una petición:

```
curl -sk -H 'X-Request-Id: 3f2b8c1e-5a47-4d0e-9b6a-1c2d3e4f5a6b' \
  -H 'Content-Type: application/json' -d '{"username":"ana","password":"..."}' \
  https://localhost:8080/api/auth/login
docker compose logs nginx api persistence | grep 3f2b8c1e-5a47-4d0e-9b6a-1c2d3e4f5a6b
```

Si no se envía la cabecera, la respuesta trae la que se generó (`curl -i`). Para una acción del juego: `docker compose logs hub world | grep <action_id>` (con `LOG_LEVEL=debug`). Nunca se registran tokens, contraseñas ni la consulta de `/play`.

## Prueba de humo

```
tests/e2e/run.sh
```

Levanta el plan B con el proyecto `hydrovalley-e2e` y los valores de `.env.example`, con un certificado TLS efímero que genera con `openssl` y monta en nginx (`tests/e2e/compose.tls.yml`; las pruebas confían en él con `NODE_EXTRA_CA_CERTS` y los bots con `HV_CA_CERT`, nunca sin verificar), por HTTPS y WSS, registra jugadores y entra con ellos, suelta una gota por nginx, comprueba que un jugador sin rol de administrador recibe `forbidden`, comprueba el crecimiento de una casilla y las hojas tras varias gotas, siembra un administrador efímero (contraseña aleatoria y su hash Argon2id, calculado con la imagen de la API; nada se guarda en disco) y prueba el modo demo y el reinicio del valle, espera al tick 50, comprueba que hay una fila en `snapshots`, corre el bot tramposo (CP-03, 5 s, imagen de los bots) con ese mismo administrador y lo baja todo con `down -v`. `request-id.test.mjs` sigue un identificador de petición en los registros de nginx, la API, la persistencia y el simulador. `tls.test.mjs` comprueba además la redirección de HTTP a HTTPS (308), TLS 1.3, el rechazo de TLS 1.1 por el servidor (con `openssl s_client`), la ausencia de HSTS, el healthcheck de nginx y, con una instancia suelta de la imagen sin certificado montado, el certificado autogenerado y su SAN (incluido `TLS_EXTRA_SANS`).

**Contra world-go.** `E2E_WORLD_IMPL=go tests/e2e/run.sh` corre la misma suite completa (humo, CP-01, chat, TLS, `request-id`, casos de fallos y CP-04) con `deploy/compose.world-go.yml` sumado a `COMPOSE_FILE`: el servicio `world` es `world-go` y todo lo demás, igual. Es la prueba de que el plan B de D-03 es un reemplazo directo. El CI no la corre (duplicaría el tiempo del job `e2e`); se lanza a mano antes de cambiar el contrato del simulador o `world-go`.

### CP-01 y CP-04

Dentro del flujo por defecto, `run.sh` corre en este orden: el humo; **CP-01** (`tests/e2e/cp01-concurrent-drops.test.mjs`, en su propia invocación de `node --test`); TLS y `request-id`; los casos de fallos; y, al final, **CP-04**.

- **CP-01 (RF-08, 100 gotas simultáneas).** En modo demo, 8 jugadores sueltan 100 gotas en total sobre 100 casillas distintas, cada uno de una sola ráfaga (12 o 13 gotas, por debajo del límite de 15 por segundo de cada jugador), todos a la vez. Comprueba que las 100 terminan `applied` (ninguna `rate_limited`), que la humedad aportada y las gotas aceptadas del hub suben exactamente 100 (se vuelve a leer tras una espera de asentamiento, con estados posteriores a las gotas) y que el depósito de nadie cambia. Imprime `MEDICION CP-01: …` y apaga el modo demo al terminar. Los jugadores salen de `POST /api/admin/bot-accounts` con el administrador efímero, porque el registro público (5 cuentas por IP cada 10 minutos) ya lo gasta casi entero el humo, y por eso va aparte (`node --test` corre los archivos a la vez).
- **CP-04 (RF-29, RF-30, incendios reales).** Es una fase aparte, al final: baja el stack con sus volúmenes y lo levanta de nuevo sumando `tests/e2e/compose.cp04.yml`, que monta de solo lectura `tests/e2e/fixtures/catalog.test-fire.json` (`fire.ignition_chance_ppm` alto) junto al catálogo de la imagen y fija `CATALOG_PATH` en la persistencia, de modo que el valle es nuevo y solo hay una planta. Es solo de pruebas: `deploy/compose.yml` no monta nada en la persistencia, que usa el catálogo que lleva su imagen (`/app/catalog/catalog.json`; así el plan A funciona sin copia del repositorio en la máquina), y `catalog/catalog.json` no se toca. El simulador pide el catálogo al arrancar. Un jugador siembra la casilla (10, 10), arde, `bots firefighter` (3 bots, 20 s) la apaga una vez, y exactamente un bot recibe el bono. `E2E_SKIP_CP04=1` omite la fase.

### Casos de fallos (CP-05, CP-06, CP-07, CP-08 y CP-12)

Después de la prueba de humo, `run.sh` corre `tests/e2e/faults.test.mjs` (CP-05) y `tests/e2e/faults-hub-persistence.test.mjs` (CP-06, CP-07 y CP-12) con el mismo stack y, al final por ser el más disruptivo, `tests/e2e/faults-startup.test.mjs` (CP-08). Provocan cada fallo con Docker (parar o pausar un servicio unos 10 s; `pause` simula la máquina apagada: la conexión queda sin respuesta y todo depende de los plazos) y comprueban, solo con `/api/system`, `/api/auth/login` y `/play` por nginx:

**CP-05, world** (`kill`: SIGKILL sin instantánea final; `stop`: parada ordenada; `pause`):

- el valle queda en pausa, el tick no avanza y el *Circuit Breaker* pasa a `open` o `half_open`; `/api/system` sigue en 200 con `world` en `down`;
- una gota enviada durante el fallo se admite, espera en la cola y recibe `applied` al volver;
- el panel y `step_ms.last` respetan los plazos (`/api/system` ~1 s y `Step` ~200 ms con el servicio pausado);
- al volver, el circuito se cierra, el tick avanza de nuevo y cambia el `boot_id` (salvo tras la pausa, donde el proceso es el mismo);
- `restored_tick` (lo expone el hub en `/stats` y la API en `/api/system`, en la misma foto que el nuevo `boot_id`) es exacto: con `kill`, un múltiplo de 50, no posterior al tick del fallo y a no más de 50 ticks de él; con `stop`, el de la instantánea final; con `pause`, no cambia;
- una acción aplicada antes de una instantánea guardada y reenviada en una conexión nueva recibe `duplicate` y no suma gotas ni humedad (se compara en un estado posterior a su resultado); la sesión de `/play` sobrevive.

**CP-06, hub** (`stop` y `pause`): `/api/system` sigue en 200 con `hub` en `down` y `hub: null` (con el hub pausado tarda ~1 s, su plazo) y `world` y la persistencia en `up`; Rust no se reinicia (mismo `boot_id` y `restored_tick`) y el tick no retrocede; el jugador se reconecta con una sesión nueva y reenvía sus acciones pendientes: la gota enviada justo antes del fallo cuenta una sola vez (`applied` o `duplicate`, nunca dos) y una acción ya confirmada recibe `duplicate`. Al reiniciarse el hub su cuenta de gotas aceptadas empieza en 0, por eso la humedad aportada (la de Rust) es la que se compara.

**CP-07, persistencia** (`persistence` parada, `persistence` pausada y `postgres` parado): `/api/system` muestra la persistencia en `down` (`unavailable`, `timeout` y `not_serving` respectivamente); 20 gotas a 4 por segundo durante el fallo reciben `applied` con un p95 menor de 200 ms; el valle no se pausa, el circuito sigue cerrado y no falla ningún `Step`; `POST /api/auth/login` responde 503 con un mensaje claro dentro del plazo de la API (3 s de presupuesto); al volver todo está en `up` y el acceso funciona.

**CP-12, persistencia durante una instantánea** (`persistence` parada ~10 s a partir de justo después de una instantánea, así que cruza al menos una): `Step` mantiene el ritmo de 100 ms (~10 ticks/s, `step_ms.last` bajo 100 ms y sin fallos), el valle no entra en pausa y el circuito sigue cerrado. Que la siguiente instantánea se guarda al volver la persistencia se comprueba de punta a punta: se abate world y su `restored_tick` es una instantánea posterior al fallo (a no más de 50 ticks del tick en que cayó).

**CP-08, arranque desordenado** (RNF-09): se paran todos los servicios con `docker compose stop` (sin borrar volúmenes, para conservar el administrador efímero y las claves) y se levantan de uno en uno con 2 s entre cada uno, primero en orden inverso al de dependencia (nginx, api, hub, world, persistence, postgres) y luego, como contraste, en orden directo. No se vuelve a tocar nada. «Todo conectado» es: los cinco servicios `up` en `/api/system`, circuito cerrado y tick avanzando, y un jugador que inicia sesión, abre `/play` y recibe `applied` en una gota. Se mide desde el último y desde el primer arranque, con una cota de 120 s (si algo no conecta, el caso falla). Además se comprueba con `docker inspect` que ningún contenedor se reinició por la política `restart: unless-stopped` (mismo `RestartCount`, mismo `StartedAt` tras el arranque manual): los servicios reintentan hasta encontrarse en vez de caerse.

**RNF-10** (levantar desde cero en menos de 5 minutos): `run.sh` mide el tiempo de arranque y exige que el sistema completo esté activo en menos de 300 s (falla con `exit 1` si se supera). Imprime `MEDICION RNF-10 …` con el tiempo de `compose up` y hasta que `/api/system` da los cinco servicios `up`. Con `E2E_SKIP_BUILD=1` (imágenes ya cargadas, como en el CI) es la cifra de «levantar desde las imágenes»; con `E2E_NO_CACHE=1` construye sin caché (`--no-cache`) para medir el levantamiento real desde cero; sin variables incluye la construcción con la caché de Docker.

Cada caso imprime una línea `MEDICION …` con los tiempos y los valores medidos. Los jugadores son el administrador efímero (inicio de sesión), para no gastar los 5 registros por IP de la prueba de humo; CP-06, CP-07 y CP-12 usan el modo demo (la gota es gratis y suma exactamente 1 a la humedad aportada) y lo apagan al terminar. Se hacen solo dos accesos por caso (durante el fallo y tras volver) para no agotar el limitador de acceso.

`E2E_SKIP_FAULTS=1 tests/e2e/run.sh` los omite. La lógica pura del arnés (`tests/e2e/faults.mjs`) se prueba sin el stack: `node --test tests/e2e/faults.unit.test.mjs`. Cada caso tarda entre 12 y 28 s (los fallos completos, unos 3 min); el CI no necesita más de los 45 min que ya tiene el job.

## Monitorización: Prometheus y Grafana (Nivel 2, B7)

Los servicios de monitorización profesional (`prometheus` y `grafana`) se encuentran en `deploy/compose.yml` bajo el perfil **`monitoring`**, de modo que el `docker compose up` regular no los inicia a menos que se invoque explícitamente dicho perfil.

### Puesta en marcha

Con el juego ya levantado (o al levantarlo en conjunto):

```bash
docker compose -f deploy/compose.yml --profile monitoring up -d
```

### Puertos y accesos

Ambos puertos se publican solo en `127.0.0.1` (en el plan B, el único puerto abierto a la red sigue siendo el 8080). Para abrirlos a otra interfaz, por ejemplo en el laboratorio, se cambia el enlace con `HV_PROMETHEUS_BIND` o `HV_GRAFANA_BIND` (p. ej. `0.0.0.0`).

| Herramienta | URL por defecto | Variables de entorno | Acceso |
| --- | --- | --- | --- |
| **Grafana** | `http://127.0.0.1:3001` | `HV_GRAFANA_PORT`, `HV_GRAFANA_BIND`, `GRAFANA_ADMIN_PASSWORD` | Anónimo de solo lectura (Viewer). Para editar, usuario `admin` y la contraseña de `GRAFANA_ADMIN_PASSWORD` |
| **Prometheus** | `http://127.0.0.1:9090` | `HV_PROMETHEUS_PORT`, `HV_PROMETHEUS_BIND` | Sin autenticación (solo local por defecto) |

`GRAFANA_ADMIN_PASSWORD` no tiene valor por defecto en el repositorio y no es obligatoria (así `up` sin el perfil no la exige). Defínela antes de levantar el perfil; si queda vacía, no hay una cuenta admin utilizable.

### Destinos de scrape (plan A)

Prometheus no expande variables de entorno en su configuración. Por eso el contenedor arranca con un `sed` que convierte la plantilla `deploy/monitoring/prometheus/prometheus.yml.tmpl` en la configuración real (la imagen es busybox: trae `sh` y `sed`, no `envsubst`). Las variables son `HUB_METRICS_ADDR` (por defecto `hub:8081`) y `API_METRICS_ADDR` (por defecto `api:3000`); en el plan A se apuntan a las máquinas del hub y de la API, por ejemplo `HUB_METRICS_ADDR=10.68.17.x:8081`.

### Cobertura

El hub y la API exponen `/metrics`. El simulador (Rust) y la persistencia (Python) no tienen un exportador propio: su estado llega a Prometheus por las sondas de la API (`hydrovalley_service_status`) y por las cifras del hub (duración y fallos de `Step`, tick, humedad).

### Tablero preaprovisionado

Grafana carga automáticamente el origen de datos Prometheus (`http://prometheus:9090`) y el tablero versionado desde `deploy/monitoring/grafana/dashboards/hydrovalley-overview.json`.

El tablero refleja con fidelidad idéntica todas las secciones del panel mínimo (`/sistema.html`) sumando la profundidad de las series temporales:
1. **Estado de los Servicios:** Disponibilidad instantánea e histórica de Nginx, API, Hub, World y Persistencia.
2. **Concurrencia y Sesiones:** Sesiones activas, goroutines del hub y tamaño de la cola de comandos en el tiempo.
3. **Rendimiento de Acciones:** Tasas por segundo (admitidas vs. aplicadas) y curvas de latencia de acción (p50 y p95).
4. **Simulador y Ciclo de Tick:** Avance del tick, estado del *Circuit Breaker*, indicador de pausa y duración de `WorldService.Step` frente al umbral crítico de 200 ms.
5. **Ecología del Valle:** Humedad total, humedad aportada acumulada vs. gotas aceptadas (verificación del modo demo) y curva de incendios activos.

### Detener la monitorización

```bash
docker compose -f deploy/compose.yml --profile monitoring stop
```
