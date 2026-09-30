# Persistencia

Servicio en Python que es la única puerta a PostgreSQL (regla 5 de `CLAUDE.md`). Por ahora sirve `PersistenceService` (`SaveSnapshot` y `LoadSnapshot`, Capa 5), `AccountService` (`Register`, `GetAccount` y `GetUsernames`), `CatalogService` (`GetCatalog`) y el servicio estándar de salud `grpc.health.v1.Health`. `ReportService` llegará en otro pull request: todavía no se registra, así que gRPC responde `UNIMPLEMENTED` a sus métodos.

## Estructura

| Ruta | Qué contiene |
| --- | --- |
| `persistence/domain.py` | `Snapshot`, resultados y errores del dominio |
| `persistence/catalog.py` | Catálogo del juego: lectura de `catalog/catalog.json`, validación de cada cifra y `catalog_version` (función pura) |
| `persistence/ports.py` | Puertos `SnapshotRepository`, `AccountRepository` y `CatalogRepository` |
| `persistence/retention.py` | Regla de retención (función pura) |
| `persistence/adapters/postgres.py` | Adaptador de PostgreSQL (psycopg 3) de los tres puertos, con reconexión en segundo plano y siembra del administrador y del catálogo |
| `persistence/adapters/memory.py` | Repositorios en memoria para las pruebas |
| `persistence/servicer.py` | Servidor gRPC de `PersistenceService`: traduce mensajes y errores a códigos de gRPC |
| `persistence/account_servicer.py` | Servidor gRPC de `AccountService`: validación y traducción de errores |
| `persistence/catalog_servicer.py` | Servidor gRPC de `CatalogService`: traduce las cifras validadas a los mensajes de `catalog.proto` |
| `persistence/server.py` | Montaje del servidor y del servicio de salud |
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

- **SaveSnapshot**: `INSERT ... ON CONFLICT (tick) DO NOTHING` en una transacción. Si la fila ya existía, compara `epoch` y `world_state_sha256`: iguales → OK (reintento); distintos → `ALREADY_EXISTS`. Si la fila que choca desaparece antes de leerla (la poda la borró a la vez), se reintenta el `INSERT` una vez; si vuelve a pasar → `ABORTED`. Antes de tocar la base de datos se valida la petición: sin `snapshot`, con `epoch` mayor que 2^31 − 1 (la columna es `INTEGER`), con `catalog_version` de más de 128 caracteres o con el carácter NUL → `INVALID_ARGUMENT`. `catalog_version` vacía también se admite (no se valida contra `catalog_entries`; el simulador envía la versión del catálogo que cargó). `tick` es `uint64` en el contrato y la columna admite todo su rango. Tras insertar una fila nueva, poda la retención en otra transacción; si la poda falla, se registra y el guardado sigue siendo correcto.
- **LoadSnapshot**: la instantánea de mayor tick, o `NOT_FOUND` si la tabla está vacía.
- **Errores**: sin conexión, caída o plazo agotado → `UNAVAILABLE`; datos que el esquema no admite (`DataError`, `IntegrityError` de psycopg) → `INVALID_ARGUMENT`; conflicto de contenido → `ALREADY_EXISTS`; cambio concurrente no resuelto → `ABORTED`; cualquier otro error → `INTERNAL`, que se registra con su traza. Nunca `UNKNOWN`.
- **Register no es idempotente**: el identificador de la cuenta lo genera el servidor, así que quien llama no debe reintentarlo automáticamente; un reintento tras `UNAVAILABLE` puede devolver `ALREADY_EXISTS` para una cuenta que en realidad sí se creó.
- **Retención**: todas las instantáneas de la última hora (tick mayor que el último menos 36.000, es decir, 720) y, de las anteriores, la primera de cada hora de tick (`tick // 36.000`).
- **Migraciones**: cada archivo de `migrations/` es una versión; las aplicadas se anotan en `schema_migrations`. Se aplican al conectar, en una transacción y bajo un cerrojo consultivo, así que repetir el arranque no cambia nada. Esa transacción no usa el `statement_timeout` de 2 s de la conexión del servicio: lleva su propio plazo (`SET LOCAL` de `statement_timeout` y `lock_timeout`, 10 min por defecto) para las sentencias y para esperar el cerrojo.
- Existen las tablas `snapshots`, `accounts` y `catalog_entries`; `snapshot_players` (y el campo `players` del contrato) llegará con `ReportService`; su `player_id` ya puede referenciar a `accounts`.

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
- **Validación** (`persistence/catalog.py`): al arrancar, antes de conectar con la base, cada cifra debe ser un entero sin signo dentro del rango de su campo en `catalog.proto` (`uint32` o `uint64`; sin decimales, booleanos ni textos), no puede faltar ni sobrar ninguna, y los intervalos, áreas, divisores y umbrales no pueden ser 0. Solo `fire.ignition_chance_ppm` admite `null` (D-01 todavía no la fija) y no pasa de 1.000.000. Los mensajes de error nombran la cifra, nunca su valor.
- **Siembra**: justo después de las migraciones y del administrador, y antes de pasar a `SERVING`, una sola transacción deja `catalog_entries` igual que el archivo: inserta las filas nuevas, actualiza solo las que cambiaron (así `updated_at` marca el último cambio real) y borra las que ya no están. Repetir el arranque no cambia nada. Para aplicar un cambio de cifras hay que reiniciar la persistencia y luego el simulador.
- **`catalog_version`**: SHA-256, en hexadecimal minúscula, del JSON canónico del catálogo: los tres grupos del archivo con claves ordenadas, sin espacios y solo ASCII. No depende del formato del archivo ni del orden de las filas; cualquier cambio de cifra la cambia. `GetCatalog` la calcula sobre lo que hay en la base, no sobre el archivo, así que coincide con lo que sirve.
- **GetCatalog**: lee todas las filas, las revalida y las traduce a `Catalog`. Una cifra `null` viaja como 0 (el contrato no admite nulos), así que sin probabilidad definida no aparece ningún incendio. Errores: sin conexión o plazo agotado → `UNAVAILABLE` (`base de datos no disponible`); un catálogo guardado que no valida, o cualquier fallo inesperado → `INTERNAL` (`error interno`). Nunca `UNKNOWN`, y el mensaje no cita SQL ni cifras. La salud de `CatalogService` sigue a la de la base de datos, igual que la de los otros servicios.

## Imagen de Docker

El contexto es la raíz del repositorio. La imagen genera el código de Python desde `proto/` en una etapa con `bufbuild/buf:1.73.0` (misma versión que `scripts/gen-proto.sh` y el CI; necesita acceso a buf.build), copia `catalog/catalog.json` a `/app/catalog/` (la variable `CATALOG_PATH` ya apunta ahí), instala las dependencias de `uv.lock` sin las de desarrollo y ejecuta el servicio con un usuario sin privilegios. `Dockerfile.dockerignore` limita el contexto a `proto/`, al catálogo y a los archivos de este servicio (requiere BuildKit, el constructor por defecto de Docker).

```
docker build -f src/persistence/Dockerfile -t hydrovalley-persistence .
docker run --rm -p 50051:50051 -e DATABASE_URL=postgresql://usuario:clave@db:5432/hydrovalley hydrovalley-persistence
```
