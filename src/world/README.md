# Simulador del valle

Servicio en Rust que es el escritor único del estado del mundo (regla 1 de `CLAUDE.md`, Capa 2). Sirve `WorldService.Step` y el servicio estándar de salud `grpc.health.v1.Health`. Es el esqueleto: aplica gotas, lleva la ventana de `action_id` recientes, avanza el tick, arranca desde la última instantánea y guarda una cada 50 ticks. El resto de la simulación llegará en otros pull requests (ver [Pendiente](#pendiente)).

## Estructura

| Ruta | Qué contiene |
| --- | --- |
| `src/domain/` | Núcleo puro, sin gRPC: `World` y su `step`, casillas, jugadores, comandos y cifras (`rules.rs`) |
| `src/codec.rs` | Traducción entre los mensajes del contrato y el dominio, y serialización de `WorldSnapshot` |
| `src/store.rs` | Puerto `SnapshotStore` y su adaptador gRPC contra `PersistenceService` (plazo y reintentos) |
| `src/simulator.rs` | Servicio de aplicación: carga al arrancar, `Step` con el mundo bloqueado e instantáneas en segundo plano |
| `src/grpc.rs` | Implementación de `WorldService`: solo traduce |
| `src/server.rs` | Montaje del servidor, salud y tarea de carga |
| `src/config.rs` | Variables de entorno |
| `src/main.rs` | Proceso: configuración, puerto y parada con SIGINT o SIGTERM |
| `build.rs` | Genera el código de Rust desde `../../proto` con `tonic-prost-build` |
| `tests/` | Prueba de integración: el servidor en el mismo proceso contra una persistencia falsa servida con tonic |
| `benches/` | Reservado para la comparativa Go vs. Rust (D-03) |

## Requisitos

- Rust 1.97 (`rustup` o el paquete del sistema). Las versiones de las dependencias están fijadas en `Cargo.toml` y `Cargo.lock` se versiona.
- Nada más: `build.rs` usa el `protoc` de `protoc-bin-vendored`, así que no hace falta instalar `protoc` ni `buf`. El código generado queda en `target/` y nunca se versiona.

## Variables de entorno

| Variable | Obligatoria | Valor por defecto | Uso |
| --- | --- | --- | --- |
| `PERSISTENCE_ADDR` | Sí | — | `host:puerto` de `PersistenceService`, p. ej. `persistence:50051` (plan B) o `10.68.17.x:50051` (plan A) |
| `WORLD_LISTEN` | No | `0.0.0.0:50051` | Dirección y puerto en los que escucha el gRPC |
| `LOG_LEVEL` | No | `info` | Filtro de registro de `tracing` (`debug`, `world=debug`…) |

## Pruebas

Desde `src/world/`:

```
cargo fmt --check
cargo clippy --all-targets -- -D warnings
cargo test
```

`cargo test` ejecuta las pruebas unitarias (dominio, traducción, reintentos, arranque e instantáneas) y la de integración de `tests/`, que levanta el servidor y una persistencia falsa en puertos locales efímeros. No necesitan Docker ni base de datos.

## Ejecutar en local

```
PERSISTENCE_ADDR=127.0.0.1:50051 WORLD_LISTEN=127.0.0.1:50061 cargo run
```

El simulador arranca aunque la persistencia no esté (regla 5 de portabilidad): el gRPC atiende desde el principio y una tarea llama a `LoadSnapshot` hasta obtener respuesta.

## Comportamiento

- **Arranque**: genera su `boot_id` (UUID v4) y llama a `LoadSnapshot`. Con `NOT_FOUND` empieza vacío en el tick 0 (`restored_tick = 0`); con una instantánea, restaura el mundo y `restored_tick` es su tick. Ante `UNAVAILABLE`, `DEADLINE_EXCEEDED` o cualquier otro fallo (también una instantánea ilegible) nunca arranca vacío: reintenta con esperas de 1 s, 2 s, 4 s y luego 5 s, sin límite. Mientras tanto, `Step` responde `UNAVAILABLE` y la salud está en `NOT_SERVING` (para `""` y para `hydrovalley.world.v1.WorldService`); al cargar pasa a `SERVING`.
- **Step**: aplica los comandos en orden con el número del tick nuevo y avanza el tick en 1, aunque la lista venga vacía. Todas las respuestas llevan `boot_id`, `restored_tick`, un resultado por comando en el orden de la petición y el `WorldState` completo.
- **Jugadores**: entran en el mundo con su primer comando **aplicado**, con 20 L de agua y 20 L de capacidad (cifras de D-01). Un comando rechazado, mal formado o no soportado no crea al jugador. El mundo admite como mucho 10.000 jugadores (`MAX_PLAYERS`); con el mundo lleno, el comando de un jugador nuevo → `REJECTED` con `INVALID_COMMAND` (el contrato no tiene un motivo «mundo lleno»; en el dominio es `WorldFull`), y los jugadores que ya están siguen jugando.
- **Gota** (D-01): fuera del modo demo cuesta 1 L y suma 25 de humedad a su casilla; en modo demo es gratis y suma 1. Las dos suman lo mismo a `humidity_contributed`. Coordenadas fuera del 32 × 32 → `REJECTED` con `OUT_OF_BOUNDS`; sin agua → `REJECTED` con `INSUFFICIENT_WATER`. Un rechazo no cobra nada.
- **Comandos mal formados**: sin tipo, o con `action_id` o `player_id` que no son UUID → `REJECTED` con `INVALID_COMMAND`. Como cualquier rechazo, no crean al jugador.
- **Otros comandos** (instalaciones, mejoras, modo demo y reinicio del valle): todavía no se aplican → `REJECTED` con `INVALID_COMMAND`, el motivo más cercano del contrato (no tiene uno para «no soportado»).
- **Idempotencia** (Capa 3): por jugador, se recuerdan los `action_id` aplicados en los últimos 600 ticks. Un `action_id` aplicado en el tick *t* es `DUPLICATE` hasta el tick *t* + 600 incluido. Los rechazados no se recuerdan. El conjunto viaja en la instantánea.
- **Instantánea** (Capa 5): cuando `tick % 50 == 0`, después de aplicar el tick, se copia el mundo y una tarea en segundo plano lo serializa como `WorldSnapshot` y llama a `SaveSnapshot`; `Step` nunca espera. Como mucho hay un guardado en curso: si el anterior sigue, esa instantánea se salta. Los errores se registran (`ALREADY_EXISTS` como error) y se vuelve a intentar en el siguiente tick de instantánea. `Snapshot.players` lleva los contadores de cada jugador. La tarea se lanza en el runtime de Tokio actual; si `Step` se llama fuera de un runtime, la instantánea se salta con un error en el registro (no hay pánico) y se reintenta en el siguiente tick de instantánea.
- **Parada** (SIGINT o SIGTERM): el servidor deja de aceptar peticiones y espera a las que están en curso. Después, con un plazo total de 5 s, el simulador espera al guardado en curso y, si el mundo está cargado y el tick avanzó desde la última instantánea guardada (o cargada), envía una instantánea final del tick actual, aunque no sea múltiplo de 50. Así, al reiniciar, se pierde como mucho lo que no llegó a guardarse en ese plazo.
- **Llamadas a la persistencia**: plazo de 2 s por intento y, al ser idempotentes, como mucho 3 reintentos tras 100, 200 y 400 ms, solo con `UNAVAILABLE` o `DEADLINE_EXCEEDED`. El canal se abre con la primera llamada y se reconecta solo.
- **GetRanking**: responde `UNIMPLEMENTED` por ahora.

## Pendiente

- Cargar las cifras con `CatalogService.GetCatalog` al arrancar y al reiniciar el valle. CatalogService todavía no existe: las cifras de D-01 están como constantes en `src/domain/rules.rs` y `catalog_version` va vacía.
- Crecimiento y hojas, recarga del depósito, evaporación y marchitamiento, aspersores y goteo, incendios con el generador con semilla (`rng_state` va vacío), gota más grande.
- Instalaciones, mejoras, modo demo, reinicio del valle y precios siguientes (`next_prices` va vacío).
- `GetRanking` (RF-10).

## Imagen de Docker

El contexto es la raíz del repositorio, porque `build.rs` necesita `proto/`:

```
docker build -f src/world/Dockerfile -t hydrovalley-world .
docker run --rm -e PERSISTENCE_ADDR=persistence:50051 -p 50051:50051 hydrovalley-world
```

Compila en `rust:1.97-slim-trixie` con cachés de BuildKit y copia solo el binario a `gcr.io/distroless/cc-debian13:nonroot`, que se ejecuta con un usuario sin privilegios. `Dockerfile.dockerignore` limita el contexto a `proto/` y a los archivos de este servicio (requiere BuildKit, el constructor por defecto de Docker).
