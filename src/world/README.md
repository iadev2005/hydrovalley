# Simulador del valle

Servicio en Rust que es el escritor único del estado del mundo (regla 1 de `CLAUDE.md`, Capa 2). Sirve `WorldService.Step` y el servicio estándar de salud `grpc.health.v1.Health`. Aplica las reglas base del valle (gotas, recarga del depósito, crecimiento, hojas, evaporación y marchitamiento) y los comandos de administración (modo demo y reinicio). Carga el catálogo, lleva la ventana de `action_id` recientes, avanza el tick, arranca desde la última instantánea y guarda una cada 50 ticks. El resto de la simulación llegará en otros pull requests (ver [Pendiente](#pendiente)).

## Estructura

| Ruta | Qué contiene |
| --- | --- |
| `src/domain/` | Núcleo puro, sin gRPC: `World` y su `step`, casillas, jugadores y comandos. `growth.rs`: crecimiento, evaporación y marchitamiento de una casilla. `limits.rs`: constantes de estructura (tamaño del valle, ventana de `action_id`, ticks por segundo) |
| `src/codec.rs` | Traducción entre los mensajes del contrato y el dominio (incluida la validación del catálogo), y serialización de `WorldSnapshot` |
| `src/store.rs` | Puerto `PersistenceStore` y su adaptador gRPC contra `PersistenceService` y `CatalogService` (plazo y reintentos) |
| `src/simulator.rs` | Servicio de aplicación: carga al arrancar, `Step` con el mundo bloqueado e instantáneas en segundo plano |
| `src/grpc.rs` | Implementación de `WorldService`: solo traduce |
| `src/server.rs` | Montaje del servidor, salud y tarea de carga |
| `src/config.rs` | Variables de entorno |
| `src/main.rs` | Proceso: configuración, puerto y parada con SIGINT o SIGTERM |
| `build.rs` | Genera el código de Rust desde `../../proto` con `tonic-prost-build` |
| `tests/` | Pruebas de integración: el servidor y el adaptador gRPC contra una persistencia falsa (con `CatalogService`) servida con tonic en el mismo proceso |
| `benches/` | Vacía por ahora (solo `.gitkeep`): reservada para la comparativa Go vs. Rust (D-03, nivel 2). Ya entra en el contexto de la imagen para cuando tenga pruebas de rendimiento |

## Requisitos

- Rust 1.97 (`rustup` o el paquete del sistema). Las versiones de las dependencias están fijadas en `Cargo.toml` y `Cargo.lock` se versiona.
- Nada más: `build.rs` usa el `protoc` de `protoc-bin-vendored`, así que no hace falta instalar `protoc` ni `buf`. El código generado queda en `target/` y nunca se versiona.

## Variables de entorno

| Variable | Obligatoria | Valor por defecto | Uso |
| --- | --- | --- | --- |
| `PERSISTENCE_ADDR` | Sí | — | `host:puerto` de `PersistenceService` y `CatalogService` (la misma dirección), p. ej. `persistence:50051` (plan B) o `10.68.17.x:50051` (plan A) |
| `WORLD_LISTEN` | No | `0.0.0.0:50051` | Dirección y puerto en los que escucha el gRPC |
| `LOG_LEVEL` | No | `info` | Filtro de registro de `tracing` (`debug`, `world=debug`…) |

## Pruebas

Desde `src/world/`:

```
cargo fmt --check
cargo clippy --all-targets -- -D warnings
cargo test
```

`cargo test` ejecuta las pruebas unitarias (dominio, traducción y validación del catálogo, reintentos, arranque e instantáneas) y la de integración de `tests/`, que levanta el servidor y una persistencia falsa en puertos locales efímeros. No necesitan Docker ni base de datos.

## Ejecutar en local

```
PERSISTENCE_ADDR=127.0.0.1:50051 WORLD_LISTEN=127.0.0.1:50061 cargo run
```

El simulador arranca aunque la persistencia no esté (regla 5 de portabilidad): el gRPC atiende desde el principio y una tarea llama a `GetCatalog` y a `LoadSnapshot` hasta obtener respuesta.

## Comportamiento

- **Catálogo**: lo primero que hace al arrancar es llamar a `CatalogService.GetCatalog` (plazo de 2 s y hasta 3 reintentos, por ser idempotente) y validarlo (`codec::catalog_from_proto`): todos los grupos de cifras presentes, intervalos, áreas, divisores y umbrales mayores que 0 y `catalog_version` no vacía. Sin catálogo no hay cifras para simular, así que, como con la instantánea, reintenta con las mismas esperas (1 s, 2 s, 4 s y luego 5 s) sin límite, no consulta la instantánea y `Step` responde `UNAVAILABLE`. El mundo aplica el catálogo vigente (`World::new` y `World::restore` lo reciben): si la instantánea se tomó con otra versión, se avisa en el registro y se usa el vigente; la siguiente instantánea ya lleva su `catalog_version`, tanto en la fila como dentro del `WorldSnapshot`. Las cifras no están en el código, tampoco la humedad de la gota en modo demo (`drop.demo_humidity`): solo quedan como constantes de estructura (`domain/limits.rs`) el tamaño del valle (32 × 32), la ventana de 600 ticks de `action_id` y los 10 ticks por segundo. Al reiniciar el valle (RF-31) se vuelve a pedir el catálogo: ver [Comandos de administración](#comandos-de-administración-rf-31).
- **Arranque**: genera su `boot_id` (UUID v4), carga el catálogo y llama a `LoadSnapshot`. Con `NOT_FOUND` empieza vacío en el tick 0 (`restored_tick = 0`); con una instantánea, restaura el mundo y `restored_tick` es su tick. Ante `UNAVAILABLE`, `DEADLINE_EXCEEDED` o cualquier otro fallo (también una instantánea ilegible) nunca arranca vacío: reintenta con esperas de 1 s, 2 s, 4 s y luego 5 s, sin límite. Mientras tanto (catálogo o instantánea), `Step` responde `UNAVAILABLE` y la salud está en `NOT_SERVING` (para `""` y para `hydrovalley.world.v1.WorldService`); al cargar pasa a `SERVING`.
- **Step**: aplica los comandos en orden con el número del tick nuevo y avanza el tick en 1, aunque la lista venga vacía. Todas las respuestas llevan `boot_id`, `restored_tick`, un resultado por comando en el orden de la petición y el `WorldState` completo.
- **Jugadores**: entran en el mundo con su primer comando **aplicado**, con el depósito lleno: `tank.capacity` litros de agua y de capacidad (20). Ese depósito inicial cuenta como recargado (`water_refilled`), para que se cumpla RF-27: `water = water_refilled − water_spent` y `leaves = leaves_earned − leaves_spent`. Un comando rechazado, mal formado o no soportado no crea al jugador. El mundo admite como mucho 10.000 jugadores (`MAX_PLAYERS`); con el mundo lleno, el comando de un jugador nuevo → `REJECTED` con `INVALID_COMMAND` (el contrato no tiene un motivo «mundo lleno»; en el dominio es `WorldFull`), y los jugadores que ya están siguen jugando.
- **Gota** (D-01, cifras del catálogo): fuera del modo demo cuesta `drop.water_cost` litros (1) y suma `drop.humidity` de humedad (25) a su casilla; en modo demo es gratis y suma `drop.demo_humidity` (1). Las dos suman lo mismo a `humidity_contributed`, que no depende del crecimiento ni de la evaporación (la humedad de la casilla sí puede quedar limitada o gastarse, la aportada no). Si con la gota la casilla llega al umbral, crece y las hojas son para quien soltó esa gota. Coordenadas fuera del 32 × 32 → `REJECTED` con `OUT_OF_BOUNDS`; sin agua → `REJECTED` con `INSUFFICIENT_WATER`. Un rechazo no cobra nada.
- **Comandos mal formados**: sin tipo, o con `action_id` o `player_id` que no son UUID → `REJECTED` con `INVALID_COMMAND`. Como cualquier rechazo, no crean al jugador.
- **Otros comandos** (instalaciones y mejoras): todavía no se aplican → `REJECTED` con `INVALID_COMMAND`, el motivo más cercano del contrato (no tiene uno para «no soportado»).
- **Reglas por tick**: después de los comandos, en este orden (`t` es el número del tick que se calcula; `World::step` lo documenta en el código):

  | Orden | Regla | Cuándo | Qué hace |
  | --- | --- | --- | --- |
  | 1 | Olvidar `action_id` | Todos los ticks | Descarta los aplicados hace más de 600 ticks |
  | 2 | Comandos | Todos los ticks | En orden de admisión. Una gota suma humedad y el crecimiento se resuelve en ese mismo momento |
  | 3 | Recarga del depósito | `t` múltiplo de `tank.refill_interval_seconds × 10` (2 s → cada 20 ticks) | Cada jugador recibe `tank.refill_amount` (1 L) sin pasar de su capacidad; `water_refilled` solo suma lo que cupo |
  | 4 | Evaporación | `t` múltiplo de 10 (una vez por segundo), salvo en modo demo | Cada casilla pierde `evaporation.humidity_per_second` (2) sin bajar de 0. La cifra es por segundo y se aplica entera, sin fracciones |
  | 5 | Marchitamiento | Todos los ticks, salvo en modo demo | A cada planta (no a la tierra seca) con humedad 0 le suma un tick a `dry_ticks`; cualquier humedad lo pone a 0. Al llegar a `evaporation.wither_after_ticks` (100) baja una etapa y cuenta desde 0 |
  | 6 | Avance del tick | Todos los ticks | `tick = t` |

  - **Crecimiento**: al sumar humedad, si la casilla llega a `growth.humidity_per_stage` (100) sube una etapa y gasta esa humedad (el resto se queda). Las hojas de la etapa alcanzada (`leaves_grass`…`leaves_tree`: 1, 2, 4, 8) van al jugador cuya gota la empujó al umbral, en el orden de admisión del tick; suman a `leaves` y a `leaves_earned`. Con las cifras de D-01 una gota sube como mucho una etapa; si el catálogo permitiera más, sube todas las que alcance y paga cada una. En árbol la humedad queda limitada a `growth.tree_max_humidity` (100).
  - **Crecer antes de evaporar**: como el crecimiento se resuelve con la gota, una casilla que llega a 100 en un tick de evaporación crece antes de perder nada.
  - **Modo demo**: sin evaporación ni marchitamiento; `dry_ticks` queda congelado (y, cuando lleguen, sin incendios).
  - **Casilla ardiendo** (gancho para los incendios; todavía no hay fuego): no crece, no se evapora y su `dry_ticks` ni avanza ni se reinicia. Las gotas le siguen sumando humedad.
  - Todo es aritmética entera sin azar: mismo estado y mismos comandos dan el mismo valle (pruebas de repetición y de repetición con restauración intermedia).

- **Comandos de administración (RF-31)**: el hub solo los admite con rol de administrador; Rust los aplica sin mirar el rol.
  - `SetDemoMode { enabled }`: activa o desactiva el modo demo desde ese comando (los siguientes comandos del mismo tick ya lo ven). Repetirlo con el mismo valor se aplica igual.
  - `ResetValley`: devuelve las casillas al estado inicial (tierra seca, humedad 0, sin fuego ni instalaciones); cada jugador vuelve al depósito lleno del catálogo, con `leaves`, `leaves_earned`, `leaves_spent`, `water_spent`, las 3R, las compras y las mejoras a cero y `water_refilled` igual a la capacidad (RF-27 sigue cuadrando). Incrementa `epoch`. **No** toca el tick, el modo demo, `rng_state`, `humidity_contributed` (solo crece), los jugadores (las cuentas) ni su ventana de `action_id` (un reintento del propio reinicio es `DUPLICATE`). Los comandos anteriores del mismo tick quedan borrados por el reinicio y los posteriores se aplican sobre el valle nuevo. Antes de tomar el bloqueo del mundo, `Simulator::catalog_for` vuelve a pedir el catálogo a la persistencia con un solo intento y un plazo de 100 ms (la mitad del de `Step`); si falla o tarda, el reinicio usa el catálogo vigente y se registra un aviso.

- **Idempotencia** (Capa 3): por jugador, se recuerdan los `action_id` aplicados en los últimos 600 ticks. Un `action_id` aplicado en el tick *t* es `DUPLICATE` hasta el tick *t* + 600 incluido. Los rechazados no se recuerdan. El conjunto viaja en la instantánea.
- **Instantánea** (Capa 5): cuando `tick % 50 == 0`, después de aplicar el tick, se copia el mundo y una tarea en segundo plano lo serializa como `WorldSnapshot` y llama a `SaveSnapshot`; `Step` nunca espera. Como mucho hay un guardado en curso: si el anterior sigue, esa instantánea se salta. Los errores se registran (`ALREADY_EXISTS` como error) y se vuelve a intentar en el siguiente tick de instantánea. `Snapshot.players` lleva los contadores de cada jugador. La tarea se lanza en el runtime de Tokio actual; si `Step` se llama fuera de un runtime, la instantánea se salta con un error en el registro (no hay pánico) y se reintenta en el siguiente tick de instantánea.
- **Parada** (SIGINT o SIGTERM): el servidor deja de aceptar peticiones y espera a las que están en curso. Después, con un plazo total de 5 s, el simulador espera al guardado en curso y, si el mundo está cargado y el tick avanzó desde la última instantánea guardada (o cargada), envía una instantánea final del tick actual, aunque no sea múltiplo de 50. Así, al reiniciar, se pierde como mucho lo que no llegó a guardarse en ese plazo.
- **Llamadas a la persistencia**: plazo de 2 s por intento y, al ser idempotentes, como mucho 3 reintentos tras 100, 200 y 400 ms, solo con `UNAVAILABLE` o `DEADLINE_EXCEEDED`. El plazo lo impone solo el cliente, alrededor de cada intento (un plazo vencido cuenta como `DEADLINE_EXCEEDED` y se reintenta); no se envía `grpc-timeout` ni se fija plazo en el canal, porque tonic los resuelve como `CANCELLED`, que no se reintenta. El canal se abre con la primera llamada y se reconecta solo.
- **GetRanking**: responde `UNIMPLEMENTED` por ahora.

## Pendiente

- Usar el resto de cifras del catálogo (instalaciones, mejoras e incendios), que ya viajan en `Catalog` pero todavía no se aplican.
- Aspersores y goteo (el goteo dividirá la cantidad de la evaporación de su área), planta de tratamiento, gota más grande, incendios con el generador con semilla (`rng_state` va vacío) y hojas para quien apaga el fuego.
- Instalaciones, mejoras y precios siguientes (`next_prices` va vacío).
- `GetRanking` (RF-10).

## Imagen de Docker

El contexto es la raíz del repositorio, porque `build.rs` necesita `proto/`:

```
docker build -f src/world/Dockerfile -t hydrovalley-world .
docker run --rm -e PERSISTENCE_ADDR=persistence:50051 -p 50051:50051 hydrovalley-world
```

Compila en `rust:1.97-slim-trixie` con cachés de BuildKit y copia solo el binario a `gcr.io/distroless/cc-debian13:nonroot`, que se ejecuta con un usuario sin privilegios. `Dockerfile.dockerignore` limita el contexto a `proto/` y a los archivos de este servicio (requiere BuildKit, el constructor por defecto de Docker).
