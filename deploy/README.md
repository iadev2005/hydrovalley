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

**RNF-10** (levantar desde cero en menos de 5 minutos): `run.sh` imprime `MEDICION RNF-10 …` con el tiempo de `compose up` y hasta que `/api/system` da los cinco servicios `up`. Con `E2E_SKIP_BUILD=1` (imágenes ya cargadas, como en el CI) es la cifra de «levantar desde las imágenes»; sin esa variable incluye la construcción, que con la caché de Docker caliente no es representativa.

Cada caso imprime una línea `MEDICION …` con los tiempos y los valores medidos. Los jugadores son el administrador efímero (inicio de sesión), para no gastar los 5 registros por IP de la prueba de humo; CP-06, CP-07 y CP-12 usan el modo demo (la gota es gratis y suma exactamente 1 a la humedad aportada) y lo apagan al terminar. Se hacen solo dos accesos por caso (durante el fallo y tras volver) para no agotar el limitador de acceso.

`E2E_SKIP_FAULTS=1 tests/e2e/run.sh` los omite. La lógica pura del arnés (`tests/e2e/faults.mjs`) se prueba sin el stack: `node --test tests/e2e/faults.unit.test.mjs`. Cada caso tarda entre 12 y 28 s (los fallos completos, unos 3 min); el CI no necesita más de los 45 min que ya tiene el job.
