# world-go

Simulador del valle en Go: un port fiel del simulador en Rust (`src/world/`), con el mismo contrato (`WorldService.Step` y `GetRanking`), las mismas reglas y las mismas instantáneas. Las pruebas de Rust son la especificación.

## Para qué sirve

- **Plan B del simulador (D-03).** Si Rust falla, `world-go` retoma el valle desde la última instantánea y sustituye a Rust en Compose, sin tocar el hub. Escritor único (regla 1): nunca corre a la vez que Rust contra el mismo hub.
- **Comparativa Go vs. Rust.** Mide el mismo paso de simulación en los dos (`tests/bench`; resultados en la sección «Resultados de la comparativa» de D-03).

No se despliega por defecto: el simulador sigue siendo Rust.

## Servidor y reemplazo de Rust

El proceso (`cmd/world-go`) es un reemplazo directo de `src/world`: los mismos nombres de servicio gRPC (`hydrovalley.world.v1.WorldService` y `grpc.health.v1`), el mismo puerto y las mismas variables.

| Variable | Obligatoria | Por defecto | Uso |
| --- | --- | --- | --- |
| `PERSISTENCE_ADDR` | sí | | `host:puerto` de `PersistenceService` y `CatalogService` |
| `WORLD_LISTEN` | no | `0.0.0.0:50051` | dirección y puerto del gRPC (una IP, no un nombre) |
| `WORLD_SEED` | no | hora de arranque | semilla del azar (u64); solo cuenta si el valle empieza de cero |
| `LOG_LEVEL` | no | `info` | `trace`, `debug`, `info`, `warn` o `error`; un valor que no se entiende deja `info`, como en Rust |

- **Arranque.** Atiende desde el principio; la salud está en `NOT_SERVING` y `Step` y `GetRanking` responden `UNAVAILABLE` hasta cargar el catálogo (`GetCatalog`) y la última instantánea (`LoadSnapshot`). Solo con `NOT_FOUND` empieza en el tick 0; con `UNAVAILABLE` o cualquier otro fallo reintenta con esperas de 1 s a 5 s, así el tick nunca retrocede.
- **`Step`.** Un solo paso a la vez (un bloqueo sobre el mundo). Cada respuesta lleva `boot_id` (UUID v4 del proceso) y `restored_tick`. Con un `ResetValley` vuelve a pedir el catálogo con un plazo de 100 ms; si falla, el reinicio usa el vigente.
- **Instantáneas.** Cada 50 ticks, desde una goroutine con una copia del mundo; como mucho un guardado en curso (si el anterior sigue, esa instantánea se salta y la siguiente reintenta). Un fallo de la persistencia se registra y el juego sigue. Al parar con SIGTERM deja de aceptar llamadas, espera a las que están en curso, al guardado en curso y guarda una instantánea final.
- **Llamadas a la persistencia.** Plazo de 2 s y, solo ante `UNAVAILABLE` o `DEADLINE_EXCEEDED`, hasta 3 reintentos (100, 200 y 400 ms), con un `x-request-id` `world-<uuid>` por llamada.
- **Registro** (`slog`, texto): `request_id` en cada línea de una llamada; el camino caliente de `Step` solo escribe a `debug`.

Para usarlo en lugar de Rust, `deploy/compose.world-go.yml` (ver `deploy/README.md`, «Sustituir Rust por world-go»); `E2E_WORLD_IMPL=go tests/e2e/run.sh` corre toda la suite de punta a punta contra él.

## Estructura

```
src/world-go/
  go.mod                  módulo; replace a ../../gen/go (código generado, no versionado)
  cmd/world-go/           proceso: configuración del entorno, puerto y parada con SIGINT o SIGTERM
  internal/domain/        reglas puras, sin gRPC ni red (espejo de src/world/src/domain)
  internal/conformance/   prueba de conformidad con Rust (guiones en tests/conformance)
  internal/codec/         traducción entre el contrato (proto/) y el dominio (espejo de src/world/src/codec.rs)
  internal/store/         puerto y adaptador gRPC hacia la persistencia: SaveSnapshot, LoadSnapshot y GetCatalog (espejo de store.rs)
  internal/simulator/     carga, boot_id, instantáneas en segundo plano y parada (espejo de simulator.rs)
  internal/server/        WorldService (Step, GetRanking) y grpc.health.v1 (espejo de grpc.rs y server.rs)
  internal/config/        variables de entorno (espejo de config.rs)
  internal/requestid/     x-request-id de punta a punta (espejo de request_id.rs)
  internal/uuid/          UUID v4 para boot_id y los ids de las llamadas salientes
  internal/fakepersist/   persistencia falsa con gRPC de verdad, solo para pruebas
  Dockerfile              imagen distroless (build con buf + Go), con grpc_health_probe
```

El dominio no depende de los tipos generados desde `proto/`; la traducción vive en `internal/codec`: catálogo, comandos, resultados, `WorldState` y la instantánea (`EncodeSnapshot` y `DecodeSnapshot`).

## Instantáneas compatibles con Rust

`EncodeSnapshot` produce la fila `persistence.v1.Snapshot` con el `WorldSnapshot` serializado en `world_state`, y `DecodeSnapshot` la lee. Una instantánea escrita por Rust la restaura Go y al revés (plan B de D-03). `domain.Restore(catalogo, partes)` hace lo mismo que `World::restore` de Rust:

- el mundo restaurado aplica el catálogo vigente, no el de la instantánea;
- un `rng_state` vacío siembra el generador con el tick; uno que no mide 8 bytes se rechaza;
- `Parts()` y `Restore` copian: no comparten memoria con el mundo.

Los bytes de la fila de instantánea y del `WorldState` son idénticos a los de Rust: lo comprueba la prueba de conformidad.

## Propagación del fuego (B3)

`World.propagateFires` es el port de `propagate_fires` de Rust y corre en el mismo punto del tick: después del marchitamiento y antes de la ignición, nunca en modo demo. Recorre las casillas por filas y, por cada casilla elegible (`IsEligible`: con planta, humedad 0 y sin fuego) con al menos una vecina ortogonal que ardiera al empezar el paso, hace **un** sorteo con `fire.propagation_chance_ppm`; solo esas casillas gastan número del generador. Usa las dimensiones del mundo (`width`, `height`), no las del juego, para que la comparativa con otros tamaños funcione. Los guiones de conformidad (en especial `08-propagacion`) comprueban que Go y Rust dan los mismos bytes.

El resumen al volver (RF-25, B6) es el port de `OfflineSummary`: `runSprinklers` lo suma en el mismo punto y orden que Rust (una casilla cuenta una vez por riego), el siguiente comando aplicado lo borra, `PlayerRecord` lo escribe (campo 16) solo si no está vacío y lleva los acumulados de la época en los campos 17 y 18, y `PlayerState` lo reenvía. El guion `09-resumen-offline` lo comprueba byte a byte.

## Conformidad con Rust

`internal/conformance` ejecuta con el dominio y el codec de Go los guiones de `tests/conformance/scenarios/` y los compara con `tests/conformance/expected/`, que escribe el simulador en Rust. Comprueba, tick a tick, los resultados de los comandos, `humidity_contributed`, los fuegos y las huellas de las instantáneas; que las instantáneas finales son idénticas byte a byte; y que Go retoma el valle desde las instantáneas que escribió Rust y llega al mismo estado final (plan B de D-03). No necesita Rust: los esperados están versionados. Si Rust cambia a propósito, se regeneran (ver `tests/conformance/README.md`). La caché de `go test` no vigila `tests/conformance` (queda fuera del módulo): para repetir una prueba sin cambios en Go, `go test -count=1 ./...`.

## Comparativa

`cmd/world-bench` ejecuta una carga de `tests/bench/workloads/` y mide `World.Step`; `internal/bench` tiene la carga, el generador de comandos y las estadísticas. Para el tamaño del valle usa `domain.NewWorldWithSize`, que solo sirve para esto: el juego sigue siendo de 32 × 32 y `Restore` no acepta otro tamaño. Todo se orquesta con `tests/bench/run.sh` (ver `tests/bench/README.md`); a mano:

```sh
cd src/world-go
go run ./cmd/world-bench ../../tests/bench/workloads/valley-256.json
```

## Cómo probar

```sh
cd src/world-go
go test -race ./...
go vet ./...
test -z "$(gofmt -l .)"
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
```

El codec, el almacén y el servidor usan el código generado: antes de probar, `scripts/gen-proto.sh` (que ya copia `proto/go.mod.tmpl` a `gen/go/go.mod`), igual que el job `world-go` del CI.

## Estado por tarea

Plan y evidencia en `odd/tasks/b5-world-go.md`.

| Tarea | Alcance | Estado |
| --- | --- | --- |
| T1 | Módulo, tipos y catálogo, job de CI | hecho |
| T2 | Gotas, humedad, crecimiento, marchitamiento, evaporación, depósito, `Step` | hecho |
| T3 | Instalaciones, mejoras, precios, dueños inactivos | hecho |
| T4 | Incendios, propagación a vecinas (B3) y SplitMix64 | hecho |
| T5 | Modo demo, reinicio y épocas, ventana de `action_id`, ranking | hecho |
| T6 | Codificación de proto y de instantáneas (`internal/codec`, `Restore`/`Parts`) | hecho |
| T7 | Prueba de conformidad Rust ↔ Go (`internal/conformance`, `tests/conformance`) | hecho |
| T8 | Almacén, simulador, servidor gRPC, imagen y Compose | hecho |
| T9 | Comparativa y gráfico (`internal/bench`, `cmd/world-bench`, `tests/bench`) | hecho |
