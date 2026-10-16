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
```

El dominio no depende de los tipos generados desde `proto/`; la traducción llega en T6.

## Cómo probar

```sh
cd src/world-go
go test -race ./...
go vet ./...
test -z "$(gofmt -l .)"
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
```

Hasta T6 no hace falta el código generado. A partir de ahí: `scripts/gen-proto.sh` y `cp proto/go.mod.tmpl gen/go/go.mod`, igual que el job `world-go` del CI.

## Estado por tarea

Plan y evidencia en `odd/tasks/b5-world-go.md`.

| Tarea | Alcance | Estado |
| --- | --- | --- |
| T1 | Módulo, tipos y catálogo, job de CI | hecho |
| T2 | Gotas, humedad, crecimiento, marchitamiento, evaporación, depósito, `Step` | hecho |
| T3 | Instalaciones, mejoras, precios, dueños inactivos | hecho |
| T4 | Incendios y SplitMix64 | pendiente |
| T5 | Modo demo, reinicio y épocas, ventana de `action_id`, ranking | pendiente |
| T6 | Codificación de proto y de instantáneas | pendiente |
| T7 | Prueba de conformidad Rust ↔ Go | pendiente |
| T8 | Almacén, simulador, servidor gRPC, imagen y Compose | pendiente |
| T9 | Comparativa y gráfico | pendiente |
