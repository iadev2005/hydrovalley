# Simulador del valle

Servicio en Rust que es el escritor único del estado del mundo (regla 1 de `CLAUDE.md`, Capa 2). Sirve `WorldService.Step` y el servicio estándar de salud `grpc.health.v1.Health`. Aplica las reglas del valle (gotas, recarga del depósito, crecimiento, hojas, evaporación, marchitamiento e incendios con un generador de azar con semilla), las instalaciones de las 3R y las dos mejoras, y los comandos de administración (modo demo y reinicio). Carga el catálogo, lleva la ventana de `action_id` recientes, avanza el tick, arranca desde la última instantánea y guarda una cada 50 ticks. El resto de la simulación llegará en otros pull requests (ver [Pendiente](#pendiente)).

## Estructura

| Ruta | Qué contiene |
| --- | --- |
| `src/domain/` | Núcleo puro, sin gRPC: `World` y su `step` (comandos, instalaciones y reglas por tick), casillas, jugadores, comandos y precios (`Catalog::installation_price`). `growth.rs`: crecimiento, evaporación y marchitamiento de una casilla. `fire.rs`: reglas de una casilla que arde (elegibilidad, gotas, consumo). `rng.rs`: SplitMix64, el generador de azar con semilla. `limits.rs`: constantes de estructura (tamaño del valle, ventana de `action_id`, ticks por segundo) |
| `src/codec.rs` | Traducción entre los mensajes del contrato y el dominio (incluida la validación del catálogo), y serialización de `WorldSnapshot` |
| `src/store.rs` | Puerto `PersistenceStore` y su adaptador gRPC contra `PersistenceService` y `CatalogService` (plazo y reintentos) |
| `src/simulator.rs` | Servicio de aplicación: carga al arrancar, `Step` con el mundo bloqueado e instantáneas en segundo plano |
| `src/request_id.rs` | Identificador de petición (RNF-07): lee y valida el metadato `x-request-id` y genera el de las llamadas salientes |
| `src/grpc.rs` | Implementación de `WorldService`: solo traduce |
| `src/server.rs` | Montaje del servidor, salud y tarea de carga |
| `src/config.rs` | Variables de entorno |
| `src/main.rs` | Proceso: configuración, puerto y parada con SIGINT o SIGTERM |
| `build.rs` | Genera el código de Rust desde `../../proto` con `tonic-prost-build` |
| `tests/` | Pruebas de integración: el servidor y el adaptador gRPC contra una persistencia falsa (con `CatalogService`) servida con tonic en el mismo proceso |
| `tests/conformance.rs` | Prueba de conformidad con `world-go`: ejecuta los guiones de `../../tests/conformance/scenarios` y exige que `../../tests/conformance/expected` sea lo que da Rust (ver [Conformidad con Go](#conformidad-con-go)) |
| `benches/` | Vacía por ahora (solo `.gitkeep`): reservada para la comparativa Go vs. Rust (D-03, nivel 2). Ya entra en el contexto de la imagen para cuando tenga pruebas de rendimiento |

## Requisitos

- Rust 1.97 (`rustup` o el paquete del sistema). Las versiones de las dependencias están fijadas en `Cargo.toml` y `Cargo.lock` se versiona.
- Nada más: `build.rs` usa el `protoc` de `protoc-bin-vendored`, así que no hace falta instalar `protoc` ni `buf`. El código generado queda en `target/` y nunca se versiona.

## Variables de entorno

| Variable | Obligatoria | Valor por defecto | Uso |
| --- | --- | --- | --- |
| `PERSISTENCE_ADDR` | Sí | — | `host:puerto` de `PersistenceService` y `CatalogService` (la misma dirección), p. ej. `persistence:50051` (plan B) o `10.68.17.x:50051` (plan A) |
| `WORLD_LISTEN` | No | `0.0.0.0:50051` | Dirección y puerto en los que escucha el gRPC |
| `WORLD_SEED` | No | derivada de la hora de arranque | Semilla (`u64` decimal) del azar de los incendios. Solo cuenta cuando el valle empieza de cero (`LoadSnapshot` → `NOT_FOUND`); si hay instantánea manda el estado del generador guardado en ella. Se registra siempre al arrancar (`seed=…`), para poder repetir una partida con `WORLD_SEED=<esa cifra>` |
| `LOG_LEVEL` | No | `info` | Filtro de registro de `tracing` (`debug`, `world=debug`…) |

## Pruebas

Desde `src/world/`:

```
cargo fmt --check
cargo clippy --all-targets -- -D warnings
cargo test
```

`cargo test` ejecuta las pruebas unitarias (dominio, traducción y validación del catálogo, reintentos, arranque e instantáneas) y la de integración de `tests/`, que levanta el servidor y una persistencia falsa en puertos locales efímeros. No necesitan Docker ni base de datos.

### Conformidad con Go

`tests/conformance.rs` ejecuta con el dominio y el codec los guiones de `tests/conformance/scenarios/` (semilla, catálogo y comandos por tick) y compara el resultado con los archivos de `tests/conformance/expected/`. `src/world-go` lee los mismos archivos: es la prueba de que los dos simuladores dan el mismo estado y de que una instantánea de Rust la retoma Go (plan B de D-03). Si cambias el comportamiento de Rust a propósito, esta prueba falla hasta que regeneres los esperados:

```
CONFORMANCE_REGENERATE=1 cargo test --locked --test conformance
```

Revisa el diff de `tests/conformance/expected/` antes de confirmarlo. Los guiones, lo que se compara y cuándo regenerar están en `tests/conformance/README.md`. No añade dependencias: el JSON se lee con un analizador mínimo dentro de la prueba.

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
- **Gota** (D-01, cifras del catálogo): fuera del modo demo cuesta `drop.water_cost` litros (1) y suma `drop.humidity` de humedad (25) a su casilla; en modo demo es gratis y suma `drop.demo_humidity` (1). Las dos suman a `humidity_contributed` la humedad que la gota entrega (humedad × casillas del valle alcanzadas), que no depende del crecimiento ni de la evaporación (la humedad de la casilla sí puede quedar limitada o gastarse, la aportada no). Con la mejora de gota más grande, ver [Mejoras](#mejoras-rf-07). Si con la gota la casilla llega al umbral, crece y las hojas son para quien soltó esa gota. Coordenadas fuera del 32 × 32 → `REJECTED` con `OUT_OF_BOUNDS`; sin agua → `REJECTED` con `INSUFFICIENT_WATER`. Un rechazo no cobra nada.
- **Comandos mal formados**: sin tipo, o con `action_id` o `player_id` que no son UUID → `REJECTED` con `INVALID_COMMAND`. Como cualquier rechazo, no crean al jugador.
- **Reglas por tick**: después de los comandos, en este orden (`t` es el número del tick que se calcula; `World::step` lo documenta en el código):

  | Orden | Regla | Cuándo | Qué hace |
  | --- | --- | --- | --- |
  | 1 | Olvidar `action_id` | Todos los ticks | Descarta los aplicados hace más de 600 ticks |
  | 2 | Comandos | Todos los ticks | En orden de admisión. Una gota suma humedad y el crecimiento se resuelve en ese mismo momento; las compras se cobran y se colocan al momento |
  | 3 | Recarga del depósito | `t` múltiplo de `tank.refill_interval_seconds × 10` (2 s → cada 20 ticks) | Cada jugador recibe `tank.refill_amount` (1 L) sin pasar de su capacidad; `water_refilled` solo suma lo que cupo |
  | 4 | Plantas de tratamiento (Reciclar) | `t` múltiplo de `treatment_plant.interval_seconds × 10` (cada 20 ticks) | Cada planta activa suma `treatment_plant.refill_amount` (1 L) al depósito de su dueño, sin pasar de la capacidad. Ocurre **después** de la recarga base: si esta ya llenó el depósito, la planta no añade nada |
  | 5 | Aspersores (Reutilizar) | `t` múltiplo de `sprinkler.interval_seconds × 10` (cada 20 ticks) | Cada aspersor activo suma `sprinkler.humidity` (25) a cada casilla de su cuadrado de 3 × 3. Si una crece, las hojas son de su dueño. Apaga los fuegos de su área (ver «Incendios» más abajo). También en modo demo |
  | 6 | Fuego | Todos los ticks, también en modo demo | Cada casilla que arde suma un tick a `burning_ticks`; al llegar a `fire.burn_out_ticks` (300) se consume: sin fuego, etapa **Tierra seca**, `dry_ticks` a 0 y la humedad y la instalación se quedan como estaban |
  | 7 | Evaporación | `t` múltiplo de 10 (una vez por segundo), salvo en modo demo | Cada casilla pierde `evaporation.humidity_per_second` (2) sin bajar de 0, **también la que arde**; en el cuadrado de 5 × 5 de un goteo activo pierde `humidity_per_second / drip.evaporation_divisor` (división entera, 2 / 2 = 1). La cifra es por segundo y se aplica entera, sin fracciones |
  | 8 | Marchitamiento | Todos los ticks, salvo en modo demo | A cada planta (no a la tierra seca) con humedad 0 le suma un tick a `dry_ticks`; cualquier humedad lo pone a 0. Al llegar a `evaporation.wither_after_ticks` (100) baja una etapa y cuenta desde 0. Una casilla que arde queda congelada |
  | 9 | Ignición | Todos los ticks, salvo en modo demo | Recorre las casillas por filas (`y * 32 + x`, de menor a mayor) y, por cada una elegible, hace **un** sorteo del generador con semilla: arde con probabilidad `fire.ignition_chance_ppm` / 1.000.000 |
  | 10 | Avance del tick | Todos los ticks | `tick = t` |

  - **Aspersores antes de evaporar**: como con las gotas, una casilla que el aspersor lleva al umbral crece antes de perder humedad en el mismo tick. Orden de varias instalaciones del mismo tipo: por casilla, fila a fila.
  - **Crecimiento**: al sumar humedad, si la casilla llega a `growth.humidity_per_stage` (100) sube una etapa y gasta esa humedad (el resto se queda). Las hojas de la etapa alcanzada (`leaves_grass`…`leaves_tree`: 1, 2, 4, 8) van al jugador cuya gota la empujó al umbral, en el orden de admisión del tick; suman a `leaves` y a `leaves_earned`. Con las cifras de D-01 una gota sube como mucho una etapa; si el catálogo permitiera más, sube todas las que alcance y paga cada una. En árbol la humedad queda limitada a `growth.tree_max_humidity` (100).
  - **Crecer antes de evaporar**: como el crecimiento se resuelve con la gota, una casilla que llega a 100 en un tick de evaporación crece antes de perder nada.
  - **Modo demo**: sin evaporación ni marchitamiento; `dry_ticks` queda congelado y no aparecen incendios nuevos (los que ya ardían siguen su curso hasta resolverse). Las instalaciones **siguen actuando** (D-01 solo quita la evaporación y los incendios): los aspersores riegan y las plantas recargan; el goteo no tiene qué frenar y no ahorra nada. Comprar cuesta hojas igual que fuera del modo demo.
  - **Casilla ardiendo**: no crece (la humedad se acumula pero la etapa queda fija) y su `dry_ticks` ni avanza ni se reinicia, pero **sí se evapora** (el goteo también ahorra ahí), como dice el diagrama 05. Las gotas y los aspersores le siguen sumando humedad.
  - Todo es aritmética entera y el único azar es el sorteo de la ignición, con el generador con semilla del mundo: mismo estado y mismos comandos dan el mismo valle (pruebas de repetición, de repetición con restauración intermedia y CP-10 con incendios).

- **Incendios (RF-28, RF-29, RF-30; diagrama 05, región «Fuego»)**: cifras de `fire.*` del catálogo.
  - **Azar**: SplitMix64 (`domain/rng.rs`), sin dependencias: su estado es un `u64`, viaja en `WorldSnapshot.rng_state` como 8 bytes little-endian y la secuencia solo depende de la semilla (probado contra los vectores de referencia de Vigna). `SplitMix64::chance_ppm` consume siempre exactamente un número. El generador es del mundo, no del tick: el reinicio del valle no lo toca. Solo la ignición lo usa.
  - **Semilla**: `WORLD_SEED` (`u64`) cuando el valle empieza de cero; sin ella, se deriva de la hora de arranque. Siempre se registra (`seed=…`). Con instantánea manda su `rng_state` (se continúa la misma secuencia). Una instantánea anterior a los incendios, con `rng_state` vacío, se siembra con su tick; un `rng_state` de otra longitud que 8 bytes es una instantánea ilegible.
  - **Ignición** (paso 9): casilla elegible = con planta (no tierra seca), humedad 0 y sin fuego. Un sorteo por casilla elegible, por filas, aunque la probabilidad sea 0 (así la secuencia no depende de la cifra); arde con probabilidad `ignition_chance_ppm` por millón (50 es un valor inicial elegido por el equipo). Va al final del tick: una casilla que acaba de marchitarse a tierra seca ya no cuenta, y una recién encendida empieza a contar sus ticks en el siguiente. No hay incendios nuevos en modo demo.
  - **Gotas**: una gota que llega a una casilla ardiendo le suma la humedad (sin crecer), cuenta para `humidity_contributed` y suma 1 a `drops_received`. Con la que llega a `drops_to_extinguish` (3), de cualquier jugador, el fuego se apaga, el crecimiento pendiente se resuelve en ese momento (hojas para ese jugador) y el jugador gana `extinguish_bonus_leaves` (5, a `leaves` y a `leaves_earned`). Los comandos se aplican en orden de admisión: con varios jugadores en el mismo tick (CP-04) el fuego se apaga una sola vez, hay un solo bono y es de quien soltó la tercera; las siguientes gotas ya no encuentran fuego. Con la gota más grande, cada casilla ardiendo que alcanza cuenta una gota. Si así completa la tercera gota de varios fuegos a la vez, paga el bono una vez por cada fuego apagado (cada uno es una extinción distinta).
  - **Aspersor**: al regar (paso 5), apaga todo fuego de su área **sin bono** (el bono es de quien suelta la tercera gota y el aspersor no cuenta gotas), con la misma humedad que a las demás casillas; si el fuego dejó crecimiento pendiente, las hojas de crecer son de su dueño. Solo apaga si su dueño sigue activo (RF-25) y cuando le toca regar (cada 20 ticks). No hay contador de «fuegos apagados».
  - **Consumirse**: cada tick, `burning_ticks` suma 1 (paso 6, después de los comandos y los aspersores); al llegar a `burn_out_ticks` (300) la casilla pasa a Tierra seca desde cualquier etapa. La humedad se queda como estaba, `dry_ticks` vuelve a 0 y **la instalación se conserva** (D-01 solo cambia la etapa). Una casilla encendida en el tick `t` con 0 ticks se consume en el `t + 300`; una gota de ese mismo tick aún llega a tiempo, porque los comandos van antes. Un fuego se consume también en modo demo.

- **Instalaciones (RF-04, RF-07)**: `PlaceInstallation { kind, x, y }` compra y coloca en un solo paso. Se comprueba en este orden y un rechazo no cobra nada ni da de alta al jugador: casilla fuera del valle → `OUT_OF_BOUNDS`; casilla con instalación (de cualquiera) → `TILE_OCCUPIED`; mundo lleno para un jugador nuevo → `INVALID_COMMAND`; hojas menores que el precio → `INSUFFICIENT_LEAVES`. Al aplicarse resta el precio de `leaves`, lo suma a `leaves_spent`, suma una compra de ese tipo y deja la instalación en la casilla con su dueño (`owner_id`). Se puede colocar sobre cualquier casilla (tierra o planta). Un tipo de instalación desconocido llega como comando sin tipo → `INVALID_COMMAND`.
  - **Precio** (`Catalog::installation_price`): el del catálogo para la primera compra de ese tipo; cada compra siguiente cuesta el precio anterior más `installation_price_increase_percent` (15 %), **redondeado hacia arriba** al entero: aspersor 10, 12, 14, 17, 20, 23…; goteo 25, 29, 34, 40…; planta 40, 46, 53… El encadenado se hace sobre el precio entero anterior (lo que ve el jugador) y nunca queda por debajo del 15 %. Se cuenta por jugador y por tipo; se calcula con 128 bits y satura en `u64::MAX`. El reinicio del valle pone las compras a cero. `StateMessage.next_prices` sale de aquí: `WorldState.players[].next_prices` lleva, en este orden, aspersor, goteo y planta (el hub solo lo reenvía).
  - **Instalaciones de un jugador ausente (RF-25)**: trabajan mientras `tick - last_action_tick <= offline_installation_seconds × 10` (7.200 s = 72.000 ticks, ese límite incluido). Pasado el tope se paran (no se borran) hasta que el dueño aplique otro comando. La recarga base del depósito no depende de esto.
  - **Áreas**: cuadrado de `area_side` casillas de lado centrado en la casilla de la instalación y recortado por los bordes del valle. Con un lado par, `(lado - 1) / 2` casillas antes del centro y `lado / 2` después (lado 4 → 1 y 2).
  - **Contadores de las 3R** (por jugador, viajan en la instantánea; el reinicio los pone a cero):
    - `water_reused` (Reutilizar): humedad que sus aspersores entregan = `sprinkler.humidity × casillas del valle alcanzadas` en cada riego, crezcan o no (el tope del árbol no la descuenta).
    - `water_reduced` (Reducir): humedad que sus goteos evitan perder = por casilla cubierta y evaporación, `min(humedad, evaporación) − min(humedad, evaporación reducida)`. Una casilla seca, ardiendo o en modo demo no ahorra nada. Si varios goteos cubren una casilla, el divisor se aplica una sola vez (no se acumulan) y el ahorro se acredita al primero en orden de casilla.
    - `water_recycled` (Reciclar): litros que sus plantas añaden al depósito **que caben** (los mismos que suman a `water_refilled`, así que RF-27 cuadra).
    - Unidades: `water_reused` y `water_reduced` son puntos de humedad; `water_recycled`, litros. D-01 no fija una equivalencia litro ↔ humedad para los reportes: ver [Pendiente](#pendiente).
  - **Efectos**: el aspersor no gasta agua del depósito ni suma a `humidity_contributed` (que es solo de las gotas); el goteo, la planta y el aspersor usan solo cifras del catálogo.

- **Mejoras (RF-07)**: `BuyUpgrade { kind }`, única por jugador. Orden de comprobación: ya comprada → `UPGRADE_ALREADY_OWNED`; mundo lleno para un jugador nuevo → `INVALID_COMMAND`; hojas insuficientes → `INSUFFICIENT_LEAVES`. Al aplicarse resta el precio como las instalaciones y la guarda en `upgrades`.
  - **Depósito mayor** (`bigger_tank`): suma `extra_capacity` (10) a `tank_capacity`. El espacio nuevo no se llena de golpe; se llena con la recarga.
  - **Gota más grande** (`bigger_drop`): una gota sigue costando `drop.water_cost` (1 L) pero humedece el cuadrado de `bigger_drop.area_side` (3 × 3) centrado en la casilla, recortado por los bordes; **cada** casilla recibe la humedad entera de la gota (`drop.humidity`, como el aspersor suma 25 a cada casilla de su área). El centro debe estar dentro del valle (si no, `OUT_OF_BOUNDS`). Las hojas de todas las casillas que crezcan son de quien soltó la gota. `humidity_contributed` suma `humedad × casillas del valle alcanzadas` (hasta 9 × 25 por gota).
  - **Modo demo**: la gota es de una sola casilla aunque el jugador tenga la mejora (D-01: «de una sola casilla y suma 1»), así que cada gota aceptada suma exactamente `demo_humidity` a `humidity_contributed` y la comprobación contra las gotas del hub (RF-30, CP-01, CP-11) sigue exacta con cualquier mejora.

- **Comandos de administración (RF-31)**: el hub solo los admite con rol de administrador; Rust los aplica sin mirar el rol.
  - `SetDemoMode { enabled }`: activa o desactiva el modo demo desde ese comando (los siguientes comandos del mismo tick ya lo ven). Repetirlo con el mismo valor se aplica igual.
  - `ResetValley`: devuelve las casillas al estado inicial (tierra seca, humedad 0, sin fuego ni instalaciones); cada jugador vuelve al depósito lleno del catálogo, con `leaves`, `leaves_earned`, `leaves_spent`, `water_spent`, las 3R, las compras y las mejoras a cero y `water_refilled` igual a la capacidad (RF-27 sigue cuadrando). Incrementa `epoch`. **No** toca el tick, el modo demo, `rng_state`, `humidity_contributed` (solo crece), los jugadores (las cuentas) ni su ventana de `action_id` (un reintento del propio reinicio es `DUPLICATE`). Los comandos anteriores del mismo tick quedan borrados por el reinicio y los posteriores se aplican sobre el valle nuevo. Antes de tomar el bloqueo del mundo, `Simulator::catalog_for` vuelve a pedir el catálogo a la persistencia con un solo intento y un plazo de 100 ms (la mitad del de `Step`); si falla o tarda, el reinicio usa el catálogo vigente y se registra un aviso. El mundo adopta el catálogo nuevo solo si el reinicio se aplica (`World::step_with_reset_catalog`): uno duplicado o rechazado no lo cambia.

- **Idempotencia** (Capa 3): por jugador, se recuerdan los `action_id` aplicados en los últimos 600 ticks. Un `action_id` aplicado en el tick *t* es `DUPLICATE` hasta el tick *t* + 600 incluido. Los rechazados no se recuerdan. El conjunto viaja en la instantánea.
- **Instantánea** (Capa 5): cuando `tick % 50 == 0`, después de aplicar el tick, se copia el mundo y una tarea en segundo plano lo serializa como `WorldSnapshot` y llama a `SaveSnapshot`; `Step` nunca espera. Como mucho hay un guardado en curso: si el anterior sigue, esa instantánea se salta. Los errores se registran (`ALREADY_EXISTS` como error) y se vuelve a intentar en el siguiente tick de instantánea. `Snapshot.players` lleva los contadores de cada jugador. La tarea se lanza en el runtime de Tokio actual; si `Step` se llama fuera de un runtime, la instantánea se salta con un error en el registro (no hay pánico) y se reintenta en el siguiente tick de instantánea.
- **Parada** (SIGINT o SIGTERM): el servidor deja de aceptar peticiones y espera a las que están en curso. Después, con un plazo total de 5 s, el simulador espera al guardado en curso y, si el mundo está cargado y el tick avanzó desde la última instantánea guardada (o cargada), envía una instantánea final del tick actual, aunque no sea múltiplo de 50. Así, al reiniciar, se pierde como mucho lo que no llegó a guardarse en ese plazo.
- **Llamadas a la persistencia**: plazo de 2 s por intento y, al ser idempotentes, como mucho 3 reintentos tras 100, 200 y 400 ms, solo con `UNAVAILABLE` o `DEADLINE_EXCEEDED`. El plazo lo impone solo el cliente, alrededor de cada intento (un plazo vencido cuenta como `DEADLINE_EXCEEDED` y se reintenta); no se envía `grpc-timeout` ni se fija plazo en el canal, porque tonic los resuelve como `CANCELLED`, que no se reintenta. El canal se abre con la primera llamada y se reconecta solo.
- **GetRanking** (RF-10, RF-11): solo lee. Con el bloqueo del mundo copia las hojas ganadas (`leaves_earned`) de cada jugador y el tick; ordena y recorta después, ya sin el bloqueo, así `Step` no espera y el ranking se recalcula en cada llamada sin pausar el juego. Mientras el mundo no está cargado responde `UNAVAILABLE`, como `Step`. La lógica vive en `domain/ranking.rs` (función pura).
  - Orden: `leaves_earned` de mayor a menor; el documento inicial no define los empates, así que se desempata por `player_id` ascendente (resultado determinista).
  - Posición («competition ranking»): los empatados comparten posición y la siguiente salta (1, 2, 2, 4). Se calcula sobre el ranking completo, así que `limit` nunca la cambia.
  - `limit` = 0 devuelve todos; en cualquier caso, como mucho 1.000 entradas (`MAX_RANKING_ENTRIES`) para acotar la respuesta. Los jugadores sin hojas también aparecen.

## Identificador de petición (RNF-07)

- **Entrante.** `Step` (el hub envía `x-request-id: tick-<n>`) y `GetRanking` (la API reenvía el id de la petición HTTP) leen el metadato `x-request-id`; un valor ausente o no seguro (más de 64 caracteres o fuera de `[A-Za-z0-9-]`) se registra como `-`. El id es el campo `request_id` del *span* de la llamada, así que sus líneas salen como `step{request_id=tick-7}: …`. `Step` registra solo a nivel `debug` (el paso y cada comando con su `action_id` y su resultado); con `LOG_LEVEL=info` no escribe nada por gota. `GetRanking` registra una línea a nivel `info`.
- **Saliente.** Cada llamada a la persistencia (`GetCatalog`, `LoadSnapshot`, `SaveSnapshot`) genera un id `world-<uuid>` (el mismo en sus reintentos) y lo envía como `x-request-id`; el simulador lo registra a nivel `debug` (a `warn` si la llamada falla del todo) y el de persistencia lo escribe en su línea de la llamada.
- Cómo seguirlo: `deploy/README.md`, «Identificador de petición».

## Pendiente

- Propagación del fuego a las casillas vecinas (nivel 2).
- `[DATO: equivalencia litro ↔ humedad]` para los reportes de las 3R (RF-12): `water_reused` y `water_reduced` cuentan puntos de humedad y `water_recycled`, litros. D-01 no dice cómo sumarlos ni si un litro de la gota equivale a los 25 puntos que suma. Hasta que se decida, el reporte debe mostrarlos por separado.
- D-01 no dice si el goteo se acumula al solaparse, cómo se redondea la evaporación dividida ni cuánta humedad reparte cada casilla de la gota más grande: las decisiones tomadas están arriba y se ajustan en un solo sitio (`Catalog` y `World`).

## Imagen de Docker

El contexto es la raíz del repositorio, porque `build.rs` necesita `proto/`:

```
docker build -f src/world/Dockerfile -t hydrovalley-world .
docker run --rm -e PERSISTENCE_ADDR=persistence:50051 -p 50051:50051 hydrovalley-world
```

Compila en `rust:1.97-slim-trixie` con cachés de BuildKit y copia solo el binario a `gcr.io/distroless/cc-debian13:nonroot`, que se ejecuta con un usuario sin privilegios. `Dockerfile.dockerignore` limita el contexto a `proto/` y a los archivos de este servicio (requiere BuildKit, el constructor por defecto de Docker).
