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
- [ ] T1 · Correcciones de la revisión (CP-01, CP-04, carga por HTTPS, RNF-03, formato, documentación).

## Modo de trabajo

- TDD estricto en Go (`go test -race`); e2e con `tests/e2e/run.sh`.
- Ruta: delegada.

## Progreso

- T0 hecho (`44b66e2`, autor Cristian Baczek). T1 en `feature/a8-a10-concurrency-fire-load` (ruta delegada).
