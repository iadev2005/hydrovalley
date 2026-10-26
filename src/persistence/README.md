# Persistencia

Servicio en Python que es la única puerta a PostgreSQL (regla 5 de `CLAUDE.md`). Por ahora sirve `PersistenceService` (`SaveSnapshot` y `LoadSnapshot`, Capa 5), `AccountService` (`Register`, `GetAccount` y `GetUsernames`), `CatalogService` (`GetCatalog`), `ReportService` (`GetReport`, RF-12) y el servicio estándar de salud `grpc.health.v1.Health`.

## Estructura

| Ruta | Qué contiene |
| --- | --- |
| `persistence/domain.py` | `Snapshot`, `SnapshotPlayer`, `Report`, resultados y errores del dominio |
| `persistence/catalog.py` | Catálogo del juego: lectura de `catalog/catalog.json`, validación de cada cifra y `catalog_version` (función pura) |
| `persistence/ports.py` | Puertos `SnapshotRepository`, `AccountRepository`, `CatalogRepository` y `ReportRepository` |
| `persistence/retention.py` | Regla de retención (función pura) |
| `persistence/adapters/postgres.py` | Adaptador de PostgreSQL (psycopg 3) de los cuatro puertos, con reconexión en segundo plano y siembra del administrador y del catálogo |
| `persistence/adapters/memory.py` | Repositorios en memoria para las pruebas |
| `persistence/servicer.py` | Servidor gRPC de `PersistenceService`: traduce mensajes y errores a códigos de gRPC |
| `persistence/account_servicer.py` | Servidor gRPC de `AccountService`: validación y traducción de errores |
| `persistence/reports.py` | Elección de instantáneas de un reporte (función pura) |
| `persistence/report_servicer.py` | Servidor gRPC de `ReportService`: validación y traducción de errores |
| `persistence/catalog_servicer.py` | Servidor gRPC de `CatalogService`: traduce las cifras validadas a los mensajes de `catalog.proto` |
| `persistence/server.py` | Montaje del servidor y del servicio de salud |
| `persistence/request_id.py` | Identificador de petición (RNF-07): interceptor del servidor, variable de contexto y filtro de logging |
| `migrations/` | Migraciones SQL versionadas (`NNNN_descripcion.sql`) |
| `tests/` | Pruebas unitarias y de integración |

## Requisitos

- [uv](https://docs.astral.sh/uv/) 0.12 o superior. uv descarga Python 3.12 si no está (`.python-version`).
- El código generado desde `proto/`, que no se versiona. Desde la raíz del repositorio:

  ```
  scripts/gen-proto.sh
  ```

  Deja el código de Python en `gen/python`. Las pruebas lo encuentran solas (`pythonpath` en `pyproject.toml`); para ejecutar el servicio hay que añadirlo a `PYTHONPATH` (ver abajo).

## Variables de entorno

| Variable | Obligatoria | Valor por defecto | Uso |
| --- | --- | --- | --- |
| `CATALOG_PATH` | Sí (la imagen de Docker la fija) | — | Ruta de `catalog/catalog.json`, las cifras del juego. Se lee y valida al arrancar; si falta o no es válido, el servicio no arranca (código de salida 2). En la imagen: `/app/catalog/catalog.json` |
| `DATABASE_URL` | Sí | — | Conexión a PostgreSQL, p. ej. `postgresql://usuario:clave@db:5432/hydrovalley`. Sin valor por defecto: las credenciales nunca van en el código (RNF-13) |
| `PERSISTENCE_LISTEN` | No | `0.0.0.0:50051` | Dirección y puerto en los que escucha el gRPC |
| `LOG_LEVEL` | No | `INFO` | Nivel de registro |
| `ADMIN_USERNAME` | No, pero va con `ADMIN_PASSWORD_HASH` | — | Nombre de la cuenta de administrador que se siembra al arrancar |
| `ADMIN_PASSWORD_HASH` | No, pero va con `ADMIN_USERNAME` | — | Hash Argon2id (formato PHC) de esa cuenta. Nunca la contraseña en claro; el servicio tampoco lo escribe en el registro |

Si solo se define una de las dos variables `ADMIN_*`, o el valor no cumple las reglas de validación, el servicio no arranca y el mensaje nombra las dos variables.

## Pruebas

Desde `src/persistence/`:

```
uv sync
uv run pytest -m "not integration"      # unitarias, sin base de datos
```

Las de integración necesitan un PostgreSQL 16 **desechable**: borran y recrean las tablas del servicio. Solo corren si `TEST_DATABASE_URL` está definida; si no, se saltan.

```
docker run -d --rm --name hv-pg-test -e POSTGRES_PASSWORD=test -e POSTGRES_DB=hv_test -p 127.0.0.1:55432:5432 postgres:16
TEST_DATABASE_URL=postgresql://postgres:test@127.0.0.1:55432/hv_test uv run pytest -m integration
docker stop hv-pg-test
```

Sin `-m`, `uv run pytest` ejecuta todas.

## Ejecutar en local

Desde `src/persistence/`, con `gen/python` en `PYTHONPATH`:

```
PYTHONPATH=.:../../gen/python DATABASE_URL=postgresql://usuario:clave@127.0.0.1:55432/hv CATALOG_PATH=../../catalog/catalog.json uv run python -m persistence
```

En Windows (PowerShell), el separador de `PYTHONPATH` es `;`.

El servicio arranca aunque PostgreSQL no esté (regla 5 de portabilidad): el gRPC atiende desde el principio y un hilo se conecta con esperas crecientes (0,5 s, 1 s, 2 s… hasta 5 s). Mientras no hay base de datos, `SaveSnapshot` y `LoadSnapshot` responden `UNAVAILABLE` y la salud está en `NOT_SERVING`; al conectar se aplican las migraciones pendientes y la salud pasa a `SERVING`. Si luego se pierde la conexión, vuelve a `NOT_SERVING` y se reconecta solo. Mientras está conectado, el mismo hilo comprueba la conexión cada 5 s con un `SELECT 1`, así que una conexión que el servidor o la red cortaron por inactividad se detecta aunque nadie llame al servicio. El aviso de cambio de salud se ejecuta fuera del cerrojo del repositorio y siempre publica el estado vigente. El hilo que conecta registra cualquier excepción (no solo las de red, también un fallo del aviso de salud) y sigue reintentando: nunca muere en silencio.

## Comportamiento

- **SaveSnapshot**: `INSERT ... ON CONFLICT (tick) DO NOTHING` en una transacción, que incluye las filas de `snapshot_players` (ver Reportes). Si la fila ya existía, compara `epoch` y `world_state_sha256`: iguales → OK (reintento); distintos → `ALREADY_EXISTS`. Si la fila que choca desaparece antes de leerla (la poda la borró a la vez), se reintenta el `INSERT` una vez; si vuelve a pasar → `ABORTED`. Antes de tocar la base de datos se valida la petición: sin `snapshot`, con `epoch` mayor que 2^31 − 1 (la columna es `INTEGER`), con `catalog_version` de más de 128 caracteres o con el carácter NUL → `INVALID_ARGUMENT`. `catalog_version` vacía también se admite (no se valida contra `catalog_entries`; el simulador envía la versión del catálogo que cargó). `tick` es `uint64` en el contrato y la columna admite todo su rango. Tras insertar una fila nueva, poda la retención en otra transacción; si la poda falla, se registra y el guardado sigue siendo correcto.
- **LoadSnapshot**: la instantánea de mayor tick, o `NOT_FOUND` si la tabla está vacía.
- **Errores**: sin conexión, caída o plazo agotado → `UNAVAILABLE`; datos que el esquema no admite (`DataError`, `IntegrityError` de psycopg) → `INVALID_ARGUMENT`; conflicto de contenido → `ALREADY_EXISTS`; cambio concurrente no resuelto → `ABORTED`; cualquier otro error → `INTERNAL`, que se registra con su traza. Nunca `UNKNOWN`.
- **Register no es idempotente**: el identificador de la cuenta lo genera el servidor, así que quien llama no debe reintentarlo automáticamente; un reintento tras `UNAVAILABLE` puede devolver `ALREADY_EXISTS` para una cuenta que en realidad sí se creó.
- **Retención**: todas las instantáneas de la última hora (tick mayor que el último menos 36.000, es decir, 720) y, de las anteriores, la primera de cada hora de tick (`tick // 36.000`).
- **Migraciones**: cada archivo de `migrations/` es una versión; las aplicadas se anotan en `schema_migrations`. Se aplican al conectar, en una transacción y bajo un cerrojo consultivo, así que repetir el arranque no cambia nada. Esa transacción no usa el `statement_timeout` de 2 s de la conexión del servicio: lleva su propio plazo (`SET LOCAL` de `statement_timeout` y `lock_timeout`, 10 min por defecto) para las sentencias y para esperar el cerrojo.
- Existen las tablas `snapshots`, `snapshot_players`, `accounts` y `catalog_entries`.

## Identificador de petición (RNF-07)

Un interceptor del servidor lee el metadato gRPC `x-request-id` (que envían la API en cada llamada y el simulador en las suyas), lo deja en una variable de contexto y registra una línea por llamada (`<método> <código> <ms>`, a nivel INFO; las llamadas de salud, a DEBUG). Todas las líneas de esa llamada llevan `request_id=<id>` en el formato de registro; sin metadato, o con uno que no sea seguro de escribir (más de 64 caracteres o fuera de `[A-Za-z0-9-]`), sale `-`. No se registra el contenido de las peticiones. Cómo seguirlo: `deploy/README.md`, «Identificador de petición».

## Cuentas (`AccountService`)

Solo guarda y devuelve el hash Argon2id que calcula la API; la contraseña nunca llega a este servicio (RNF-12).

- **Tabla `accounts`** (`migrations/0002_create_accounts.sql`): `id` UUID (es el `player_id` del token y del mundo), `username`, `password_hash`, `role` (`player` o `admin`, con `CHECK`) y `created_at`.
- **Mayúsculas y minúsculas**: el nombre de usuario es único **sin distinguir mayúsculas** (índice único sobre `lower(username)`): `Ana` y `ana` son la misma cuenta. Se conserva la grafía con la que se registró, y `GetAccount` busca sin distinguir mayúsculas. El alfabeto es solo ASCII para que `lower()` no dependa de la configuración regional.
- **Validación de `username`**: de 3 a 32 caracteres entre `A-Z`, `a-z`, `0-9`, `_`, `.` y `-`. En `Register` incumplirlo es `INVALID_ARGUMENT`.
- **Validación de `password_hash`**: debe empezar por `$argon2id$`, seguir con ASCII imprimible sin espacios y no pasar de 512 caracteres; si no, `INVALID_ARGUMENT`. Solo se comprueba la forma (verificarlo es cosa de la API) y el mensaje de error nunca repite el valor recibido.
- **Register**: crea una cuenta `player` con un UUID nuevo. Nombre repetido → `ALREADY_EXISTS`. No se reintenta.
- **GetAccount**: `account_id`, `password_hash` y `role`, o `NOT_FOUND`. Un nombre que no cumple las reglas de validación nunca pudo registrarse, así que también responde `NOT_FOUND` (no `INVALID_ARGUMENT`): la API trata igual todas las búsquedas fallidas.
- **GetUsernames**: mapa `player_id → username` con los identificadores que existen; los desconocidos se omiten, los repetidos se ignoran y las claves salen en la forma canónica del UUID (minúsculas). Un UUID mal formado, o más de 1.000 identificadores, es `INVALID_ARGUMENT`.
- **Administrador**: al conectar con la base de datos, justo después de las migraciones y antes de pasar a `SERVING`, si `ADMIN_USERNAME` y `ADMIN_PASSWORD_HASH` están definidas se crea esa cuenta con rol `admin` solo si no existe (`INSERT ... ON CONFLICT DO NOTHING`). Es idempotente y nunca modifica una cuenta existente: si el nombre ya lo tiene un jugador, no lo asciende y lo avisa en el registro. El registro nunca incluye el hash. Si la siembra falla, se reintenta como cualquier fallo de conexión.
- **Errores**: los mismos que el servicio de instantáneas: sin conexión o plazo agotado → `UNAVAILABLE` (la salud de `AccountService` también pasa a `NOT_SERVING`); datos que el esquema no admite → `INVALID_ARGUMENT`; cualquier otro error → `INTERNAL`. Nunca `UNKNOWN`.

## Catálogo (`CatalogService`)

Las cifras del juego (D-01) viven en `catalog/catalog.json` (forma y significado de cada clave en `catalog/README.md`); la persistencia las guarda en `catalog_entries` y las sirve con `GetCatalog`.

- **Tabla `catalog_entries`** (`migrations/0003_create_catalog_entries.sql`): `key` (clave primaria), `kind` (`rule`, `installation` o `upgrade`, con `CHECK`), `value` (`JSONB`) y `updated_at`. Una fila por elemento: `drop`, `tank`, `growth`, `evaporation`, `fire`, `sprinkler`, `drip`, `treatment_plant`, `bigger_drop` y `bigger_tank`, más dos cifras sueltas (`installation_price_increase_percent` y `offline_installation_seconds`).
- **Validación** (`persistence/catalog.py`): al arrancar, antes de conectar con la base, cada cifra debe ser un entero sin signo dentro del rango de su campo en `catalog.proto` (`uint32` o `uint64`; sin decimales, booleanos ni textos), no puede faltar ni sobrar ninguna, y los intervalos, áreas, divisores y umbrales no pueden ser 0. `fire.ignition_chance_ppm` y `fire.propagation_chance_ppm` son obligatorias como las demás (0 = no hay incendios ni propagación) y no pasan de 1.000.000. Los mensajes de error nombran la cifra, nunca su valor.
- **Siembra**: justo después de las migraciones y del administrador, y antes de pasar a `SERVING`, una sola transacción deja `catalog_entries` igual que el archivo: inserta las filas nuevas, actualiza solo las que cambiaron (así `updated_at` marca el último cambio real) y borra las que ya no están. Repetir el arranque no cambia nada. Para aplicar un cambio de cifras hay que reiniciar la persistencia y luego el simulador.
- **`catalog_version`**: SHA-256, en hexadecimal minúscula, del JSON canónico del catálogo: los tres grupos del archivo con claves ordenadas, sin espacios y solo ASCII. No depende del formato del archivo ni del orden de las filas; cualquier cambio de cifra la cambia. `GetCatalog` la calcula sobre lo que hay en la base, no sobre el archivo, así que coincide con lo que sirve.
- **GetCatalog**: lee todas las filas, las revalida y las traduce a `Catalog`. Errores: sin conexión o plazo agotado → `UNAVAILABLE` (`base de datos no disponible`); un catálogo guardado que no valida, o cualquier fallo inesperado → `INTERNAL` (`error interno`). Nunca `UNKNOWN`, y el mensaje no cita SQL ni cifras. La salud de `CatalogService` sigue a la de la base de datos, igual que la de los otros servicios.

## Reportes (`ReportService`)

`GetReport` (RF-12) no tiene tabla propia: lee `snapshot_players`, la copia en columnas de los contadores de cada jugador que Rust envía en `Snapshot.players` (diagrama 07). Solo usa las instantáneas de la **época actual**, la de la instantánea de mayor tick: cada reinicio del valle sube la época y pone los contadores a cero. Un reporte puede ir hasta 5 s por detrás del valle.

- **Tabla `snapshot_players`** (`migrations/0004_create_snapshot_players.sql`): clave `(tick, player_id)`; nueve contadores `NUMERIC(20,0)` con `CHECK` del rango u64, más `fires_extinguished` y `tiles_grown` (`migrations/0005_add_offline_stats_to_snapshot_players.sql`, mismo tipo y `CHECK`: fuegos apagados y casillas crecidas por los aspersores del jugador en la época, RF-25); clave foránea a `snapshots(tick)` con `ON DELETE CASCADE` (la poda de retención arrastra sus filas) y a `accounts(id)`.
- **Guardado atómico**: la fila de `snapshots` y todas las de `snapshot_players` van en la misma transacción. Un reintento idéntico (mismo tick, época y `world_state`) responde OK sin insertar filas de jugadores otra vez; un valor fuera de rango deshace toda la instantánea (`INVALID_ARGUMENT`). En `SaveSnapshot`, un `player_id` que no sea un UUID de 36 caracteres, o repetido dentro de la misma instantánea, es `INVALID_ARGUMENT`.
- **Jugadores sin cuenta**: si una instantánea trae un `player_id` sin fila en `accounts`, esa fila se **omite con un aviso** en el registro (con el identificador) y el resto de la instantánea se guarda. Así un jugador huérfano no hace perder las instantáneas, pero sus contadores no entran en los reportes (ni en el global).
- **Instantáneas usadas ("en o antes de")**: un tick pedido se resuelve a la instantánea de mayor tick menor o igual a él dentro de la época actual. Sin `to_tick`, la última; sin `from_tick` (o con 0), el inicio de la época, con los contadores a cero. `from_tick` y `to_tick` de la respuesta son los ticks de las instantáneas realmente usadas (`from_tick` = 0: inicio de la época).
- **Totales**: por jugador, su fila; global, la suma de las filas de todos los jugadores; con periodo, la diferencia entre la instantánea final y la inicial (un jugador que no estaba en la inicial cuenta desde cero; nunca baja de 0). Son los siete contadores de `ReportTotals` (3R y saldos de RF-27).
- **Errores**: sin ninguna instantánea → `NOT_FOUND`; `player_id` con una cuenta que no existe → `NOT_FOUND`; `player_id` que no es un UUID, `from_tick` mayor que `to_tick`, o un tick anterior a la primera instantánea de la época actual (incluye ticks de épocas anteriores) → `INVALID_ARGUMENT`; sin conexión → `UNAVAILABLE`; cualquier otro → `INTERNAL`. Los detalles son fijos y nunca citan SQL.
- **Cuenta sin filas**: un jugador cuya cuenta existe pero que aún no está en las instantáneas (se registró y no ha jugado) recibe **ceros**, no `NOT_FOUND`: así la API no tiene que tratar ese caso como un error.
- **Lectura coherente**: `GetReport` lee en una sola transacción `REPEATABLE READ` de solo lectura, así que una poda concurrente no deja el reporte a medias. Con la retención, un tick muy antiguo puede resolverse a una instantánea anterior a la pedida.
- `player_id` de la respuesta sale en minúsculas (forma canónica); vacío en el global.

## Imagen de Docker

El contexto es la raíz del repositorio. La imagen genera el código de Python desde `proto/` en una etapa con `bufbuild/buf:1.73.0` (misma versión que `scripts/gen-proto.sh` y el CI; necesita acceso a buf.build), copia `catalog/catalog.json` a `/app/catalog/` (la variable `CATALOG_PATH` ya apunta ahí), instala las dependencias de `uv.lock` sin las de desarrollo y ejecuta el servicio con un usuario sin privilegios. `Dockerfile.dockerignore` limita el contexto a `proto/`, al catálogo y a los archivos de este servicio (requiere BuildKit, el constructor por defecto de Docker).

```
docker build -f src/persistence/Dockerfile -t hydrovalley-persistence .
docker run --rm -p 50051:50051 -e DATABASE_URL=postgresql://usuario:clave@db:5432/hydrovalley hydrovalley-persistence
```
