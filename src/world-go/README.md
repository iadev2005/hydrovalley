# world-go

Simulador del valle en Go: un port fiel del simulador en Rust (`src/world/`), con el mismo contrato (`WorldService.Step` y `GetRanking`), las mismas reglas y las mismas instantáneas. Las pruebas de Rust son la especificación.

## Para qué sirve

- **Plan B del simulador (D-03).** Si Rust falla, `world-go` retoma el valle desde la última instantánea y sustituye a Rust en Compose, sin tocar el hub. Escritor único (regla 1): nunca corre a la vez que Rust contra el mismo hub.
- **Comparativa Go vs. Rust.** Mide el mismo paso de simulación en los dos (`tests/bench`).

No se despliega por defecto: el simulador sigue siendo Rust.

## Estructura

```
src/world-go/
  go.mod                  módulo; replace a ../../gen/go (código generado, no versionado)
  cmd/world-go/           servidor (T8)
  internal/domain/        reglas puras, sin gRPC ni red (espejo de src/world/src/domain)
  internal/codec/         traducción entre el contrato (proto/) y el dominio (espejo de src/world/src/codec.rs)
```

El dominio no depende de los tipos generados desde `proto/`; la traducción vive en `internal/codec`: catálogo, comandos, resultados, `WorldState` y la instantánea (`EncodeSnapshot` y `DecodeSnapshot`).

## Instantáneas compatibles con Rust

`EncodeSnapshot` produce la fila `persistence.v1.Snapshot` con el `WorldSnapshot` serializado en `world_state`, y `DecodeSnapshot` la lee. Una instantánea escrita por Rust la restaura Go y al revés (plan B de D-03). `domain.Restore(catalogo, partes)` hace lo mismo que `World::restore` de Rust:

- el mundo restaurado aplica el catálogo vigente, no el de la instantánea;
- un `rng_state` vacío siembra el generador con el tick; uno que no mide 8 bytes se rechaza;
- `Parts()` y `Restore` copian: no comparten memoria con el mundo.

Los bytes de `world_state` coinciden con los de Rust (ver `tests/conformance/`).

## Cómo probar

```sh
cd src/world-go
go test -race ./...
go vet ./...
test -z "$(gofmt -l .)"
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
```

El codec usa el código generado: antes de probar, `scripts/gen-proto.sh` (que ya copia `proto/go.mod.tmpl` a `gen/go/go.mod`), igual que el job `world-go` del CI.

## Estado por tarea

Plan y evidencia en `odd/tasks/b5-world-go.md`.

| Tarea | Alcance | Estado |
| --- | --- | --- |
| T1 | Módulo, tipos y catálogo, job de CI | hecho |
| T2 | Gotas, humedad, crecimiento, marchitamiento, evaporación, depósito, `Step` | hecho |
| T3 | Instalaciones, mejoras, precios, dueños inactivos | hecho |
| T4 | Incendios y SplitMix64 | hecho |
| T5 | Modo demo, reinicio y épocas, ventana de `action_id`, ranking | hecho |
| T6 | Codificación de proto y de instantáneas (`internal/codec`, `Restore`/`Parts`) | hecho |
| T7 | Prueba de conformidad Rust ↔ Go | pendiente |
| T8 | Almacén, simulador, servidor gRPC, imagen y Compose | pendiente |
| T9 | Comparativa y gráfico | pendiente |
