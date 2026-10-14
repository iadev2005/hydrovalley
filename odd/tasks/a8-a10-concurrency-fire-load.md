# A8, A9 y A10: CP-01, CP-04 real y prueba de carga

## Objetivo

Integrar el trabajo de Cristian Baczek (rama `A8-·-CP-01-de-verdad-100-gotas-simultáneas`) y corregir lo que encontró la revisión, para cerrar A8 (CP-01, RF-08), A9 (CP-04 con incendios reales) y A10 (carga en el CI y RNF-03) de `docs/tareas-equipo.md`.

## Problema y por qué

La rama estaba 50 commits por detrás de `dev` (sin TLS), rompía el smoke por el límite de registros, CP-01 no respetaba el límite de 15 gotas/s, CP-04 no se ejecutaba en el flujo normal, la medición de RNF-03 repetía RNF-02, `tests/load/run.sh` iba por HTTP y duplicaba la e2e, y había archivos personales y commits sin convención.

## Alcance

- Commit de Cristian, aplastado sobre `dev` con su autoría (sin `work.md`, `comandos para pruebas.md` ni `reports/`).
- Correcciones encima, con TDD donde hay lógica.

## Tareas

- [x] T0 · Integrar la rama sobre `dev` (conflictos en `run.sh` y `config_test.go`, resueltos conservando las dos partes).
- [x] T1 · Correcciones de la revisión. Evidencia: RED en Go (compilación y comportamiento); bots `go test -race`, vet, gofmt y staticcheck; actionlint; e2e 42/42 en 303 s (CP-04 al final en un stack limpio con el catálogo de prueba); `tests/load/run.sh` por HTTPS en 137 s. Mediciones locales (Mac, plan B): CP-01 8 jugadores, 100 enviadas y 100 aplicadas, Δ humedad = Δ gotas aceptadas = 100, agua sin cambios; CP-04 un incendio apagado, un bono; RNF-01 119,4 gotas/s; RNF-02 p95 98,2 ms; RNF-03 p95 97,7 ms con ranking frente a 98,2 ms sin él. Decisión pendiente del equipo: margen de RNF-03 del 20 %.
- [x] T2 · Avisos de la revisión: el catálogo vuelve a ir dentro de la imagen de persistencia (sin montar `catalog/` en `deploy/compose.yml`, regla 3) y el de prueba se monta solo en la fase CP-04 (`tests/e2e/compose.cp04.yml`); RNF-03 «no evaluable» sin al menos 10 consultas correctas y cero errores; línea base por ejecución; intervalo del ranking con una sola fuente; claves JSON en snake_case; contraseña por entorno; espera compartida en `tests/lib/stack.sh`. Evidencia: RED en Go; bots `go test -race`, vet, gofmt y staticcheck; actionlint; e2e completa en verde (CP-01 100/100, CP-04 un incendio apagado y un bono).

## Modo de trabajo

- TDD estricto en Go (`go test -race`); e2e con `tests/e2e/run.sh`.
- Ruta: delegada.

## Progreso

- T0 hecho (`44b66e2`, autor Cristian Baczek). T1 y T2 en `feature/a8-a10-concurrency-fire-load` (ruta delegada), revisados con 4 lentes y aprobados en dos tramos. Avisos menores pendientes: un comentario invertido en `report.go:212` y el mínimo fijo de 10 consultas de ranking (una prueba muy corta queda «no evaluable», a propósito).
