# Despliegue

| Ruta | Qué contiene |
| --- | --- |
| `compose.yml` | Plan B: todo en una máquina, un contenedor por servicio (diagrama `08-despliegue-plan-b`) |
| `.env.example` | Plantilla de `deploy/.env` (credenciales de PostgreSQL, claves del token, administrador, puerto del host y etiqueta de las imágenes). `.env` nunca se versiona |
| `nginx/` | Proxy de entrada: plantilla de configuración y la imagen, que incluye el frontend |
| `nodes/` | Plan A: un archivo por máquina (pendiente) |

## Plan B

```
cp deploy/.env.example deploy/.env      # y rellenar los valores (ver «Claves y variables»)
docker compose -f deploy/compose.yml up --build
```

Luego, http://localhost:8080. Solo nginx publica un puerto (`HV_HTTP_PORT`, 8080 por defecto); PostgreSQL no se publica. Los servicios se encuentran por su nombre (`world:50051`, `persistence:50051`…) mediante variables de entorno.

- **Arranque**: no se usa `depends_on`. Cada servicio arranca aunque los demás no estén y se reconecta solo (regla 5); `restart: unless-stopped` cubre las caídas. Hasta que la persistencia conecta con PostgreSQL y el simulador carga su instantánea, el hub tiene el valle en pausa.
- **Healthchecks**: van en cada imagen, para que el plan A los herede. Hub: `hub healthcheck` (`/healthz`). Simulador y persistencia: `grpc_health_probe` contra `grpc.health.v1` (SERVING solo con la instantánea cargada o la base conectada). API: `node dist/healthcheck.js`. nginx: `wget` a `/healthz`. PostgreSQL: `pg_isready`, en `compose.yml`.

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

# Bots (opcionales; solo con el perfil «bots», ver «Bots»). La contraseña va
# en claro y es la del administrador de ADMIN_USERNAME, no su hash.
# HV_ADMIN_USERNAME=
# HV_ADMIN_PASSWORD=
# BOTS_COUNT=
# BOTS_DURATION=
# BOTS_SEED=
# BOTS_RESET=
# BOTS_REPORT_PATH=
```

`tests/e2e/run.sh` no lee estas claves de `.env.example`: genera un par efímero en cada ejecución.

## Bots

El servicio `bots` (`src/bots/`) está en `compose.yml` bajo el perfil `bots`: `up` no lo arranca. Corre, imprime su resumen y termina; habla solo con nginx dentro de la red de Compose (`HV_BASE_URL=http://nginx:8080`), como cualquier jugador. Necesita un administrador (RF-31) para crear sus cuentas: `HV_ADMIN_USERNAME` es el `ADMIN_USERNAME` sembrado y `HV_ADMIN_PASSWORD` su contraseña en claro (no el hash). El stack tiene que estar levantado.

```
docker compose -f deploy/compose.yml --profile bots run --rm bots neighbors     # vecinos (hasta Ctrl-C si BOTS_DURATION=0)
docker compose -f deploy/compose.yml --profile bots run --rm bots load-test     # CP-11: 30 bots, 60 s (también `load`)
docker compose -f deploy/compose.yml --profile bots run --rm bots cheater       # CP-03: un tramposo, 10 s de inundación
docker compose -f deploy/compose.yml --profile bots run --rm bots firefighter   # CP-04: 3 bomberos, 120 s
```

Las variables (`HV_ADMIN_USERNAME`, `HV_ADMIN_PASSWORD`, `BOTS_COUNT`, `BOTS_DURATION`, `BOTS_SEED`, `BOTS_RESET`, `BOTS_REPORT_PATH`) salen de `deploy/.env` o del entorno del comando; vacías, cada escenario usa su valor por defecto (ver `src/bots/README.md`). Sin las del administrador el binario se detiene con un mensaje que las nombra. Salida: 0 si el escenario cumple, 1 si no. `load-test`, `cheater` y `firefighter` imprimen `Resultado: CUMPLE` o `Resultado: NO CUMPLE`.

El informe JSON (`BOTS_REPORT_PATH`) se escribe dentro del contenedor, que no tiene carpeta compartida (regla 3) y se borra con `--rm`: para conservarlo, quitar `--rm` y copiarlo con `docker cp`, o dejarse con el resumen por la salida estándar.

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

**TLS (RNF-12), pendiente**: por ahora HTTP en el 8080. En el laboratorio, HTTPS en ese mismo puerto con un certificado autofirmado.

## Prueba de humo

```
tests/e2e/run.sh
```

Levanta el plan B con el proyecto `hydrovalley-e2e` y los valores de `.env.example`, registra jugadores y entra con ellos, suelta una gota por nginx, comprueba que un jugador sin rol de administrador recibe `forbidden`, comprueba el crecimiento de una casilla y las hojas tras varias gotas, siembra un administrador efímero (contraseña aleatoria y su hash Argon2id, calculado con la imagen de la API; nada se guarda en disco) y prueba el modo demo y el reinicio del valle, espera al tick 50, comprueba que hay una fila en `snapshots`, corre el bot tramposo (CP-03, 5 s, imagen de los bots) con ese mismo administrador y lo baja todo con `down -v`.
