# Persistencia

Servicio en Python que es la única puerta a PostgreSQL (regla 5 de `CLAUDE.md`). Por ahora sirve solo `PersistenceService` (`SaveSnapshot` y `LoadSnapshot`, Capa 5) y el servicio estándar de salud `grpc.health.v1.Health`. `AccountService`, `CatalogService` y `ReportService` llegarán en otros pull requests: todavía no se registran, así que gRPC responde `UNIMPLEMENTED` a sus métodos.

## Estructura

| Ruta | Qué contiene |
| --- | --- |
| `persistence/domain.py` | `Snapshot`, resultados y errores del dominio |
| `persistence/ports.py` | Puerto `SnapshotRepository` |
| `persistence/retention.py` | Regla de retención (función pura) |
| `persistence/adapters/postgres.py` | Adaptador de PostgreSQL (psycopg 3), con reconexión en segundo plano |
| `persistence/adapters/memory.py` | Repositorio en memoria para las pruebas |
| `persistence/servicer.py` | Servidor gRPC: traduce mensajes y errores a códigos de gRPC |
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
| `DATABASE_URL` | Sí | — | Conexión a PostgreSQL, p. ej. `postgresql://usuario:clave@db:5432/hydrovalley`. Sin valor por defecto: las credenciales nunca van en el código (RNF-13) |
| `PERSISTENCE_LISTEN` | No | `0.0.0.0:50051` | Dirección y puerto en los que escucha el gRPC |
| `LOG_LEVEL` | No | `INFO` | Nivel de registro |

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
PYTHONPATH=.:../../gen/python DATABASE_URL=postgresql://usuario:clave@127.0.0.1:55432/hv uv run python -m persistence
```

En Windows (PowerShell), el separador de `PYTHONPATH` es `;`.

El servicio arranca aunque PostgreSQL no esté (regla 5 de portabilidad): el gRPC atiende desde el principio y un hilo se conecta con esperas crecientes (0,5 s, 1 s, 2 s… hasta 5 s). Mientras no hay base de datos, `SaveSnapshot` y `LoadSnapshot` responden `UNAVAILABLE` y la salud está en `NOT_SERVING`; al conectar se aplican las migraciones pendientes y la salud pasa a `SERVING`. Si luego se pierde la conexión, vuelve a `NOT_SERVING` y se reconecta solo. El hilo que conecta registra cualquier excepción (no solo las de red, también un fallo del aviso de salud) y sigue reintentando: nunca muere en silencio.

## Comportamiento

- **SaveSnapshot**: `INSERT ... ON CONFLICT (tick) DO NOTHING` en una transacción. Si la fila ya existía, compara `epoch` y `world_state_sha256`: iguales → OK (reintento); distintos → `ALREADY_EXISTS`. Si la fila que choca desaparece antes de leerla (la poda la borró a la vez), se reintenta el `INSERT` una vez; si vuelve a pasar → `ABORTED`. Antes de tocar la base de datos se valida la petición: sin `snapshot`, con `epoch` mayor que 2^31 − 1 (la columna es `INTEGER`), con `catalog_version` de más de 128 caracteres o con el carácter NUL → `INVALID_ARGUMENT`. `catalog_version` vacía se admite mientras no exista `CatalogService` (el simulador la envía así). `tick` es `uint64` en el contrato y la columna admite todo su rango. Tras insertar una fila nueva, poda la retención en otra transacción; si la poda falla, se registra y el guardado sigue siendo correcto.
- **LoadSnapshot**: la instantánea de mayor tick, o `NOT_FOUND` si la tabla está vacía.
- **Errores**: sin conexión, caída o plazo agotado → `UNAVAILABLE`; datos que el esquema no admite (`DataError`, `IntegrityError` de psycopg) → `INVALID_ARGUMENT`; conflicto de contenido → `ALREADY_EXISTS`; cambio concurrente no resuelto → `ABORTED`; cualquier otro error → `INTERNAL`, que se registra con su traza. Nunca `UNKNOWN`.
- **Retención**: todas las instantáneas de la última hora (tick mayor que el último menos 36.000, es decir, 720) y, de las anteriores, la primera de cada hora de tick (`tick // 36.000`).
- **Migraciones**: cada archivo de `migrations/` es una versión; las aplicadas se anotan en `schema_migrations`. Se aplican al conectar, en una transacción y bajo un cerrojo consultivo, así que repetir el arranque no cambia nada. Esa transacción no usa el `statement_timeout` de 2 s de la conexión del servicio: lleva su propio plazo (`SET LOCAL` de `statement_timeout` y `lock_timeout`, 10 min por defecto) para las sentencias y para esperar el cerrojo.
- Por ahora solo existe la tabla `snapshots`; `snapshot_players` (y el campo `players` del contrato) llegará con las cuentas, porque su `player_id` referencia a `accounts`.

## Imagen de Docker

El contexto es la raíz del repositorio. La imagen genera el código de Python desde `proto/` en una etapa con `bufbuild/buf:1.73.0` (misma versión que `scripts/gen-proto.sh` y el CI; necesita acceso a buf.build), instala las dependencias de `uv.lock` sin las de desarrollo y ejecuta el servicio con un usuario sin privilegios. `Dockerfile.dockerignore` limita el contexto a `proto/` y a los archivos de este servicio (requiere BuildKit, el constructor por defecto de Docker).

```
docker build -f src/persistence/Dockerfile -t hydrovalley-persistence .
docker run --rm -p 50051:50051 -e DATABASE_URL=postgresql://usuario:clave@db:5432/hydrovalley hydrovalley-persistence
```
