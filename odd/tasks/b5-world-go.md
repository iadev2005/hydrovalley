# B5: tick en Go (`world-go`) y comparativa Go vs. Rust

## Objetivo

Un simulador en Go, `src/world-go`, con el mismo contrato que el de Rust (`WorldService.Step` y `GetRanking`) y las mismas reglas, que lea y escriba las mismas instantáneas; y una comparativa medida del tiempo de `Step` de los dos (D-03), con su gráfico para el informe.

## Problema y por qué

- D-03 pide medir el mismo paso de simulación en Go y en Rust; hoy `src/world-go` y `tests/bench` están vacíos.
- Plan B (documento, D-03 y «Riesgos»): si Rust falla, Go lleva el mundo. Con un port fiel que lee las instantáneas de Rust, `world-go` puede retomar el valle desde la última instantánea; es la base de B8 (mantener el juego con una máquina apagada).
- Decisión del usuario (15 oct 2026): port fiel, no la versión simplificada.

## Alcance

- Reglas completas: gotas, humedad, crecimiento y marchitamiento, evaporación, depósito, instalaciones (aspersor, goteo, planta), mejoras, precios +15 %, instalaciones activas 2 h, incendios con SplitMix64 (mismo orden de sorteo), modo demo, reinicio del valle y épocas, ventana de `action_id` de 600 ticks, ranking.
- Codificación de `WorldSnapshot` compatible byte a byte en lo que importe (Rust ↔ Go).
- Servidor gRPC con salud, `boot_id`, `restored_tick`, instantánea cada 50 ticks en segundo plano y final al apagarse, `LoadSnapshot` con reintentos, `GetCatalog`, identificador de petición.
- Prueba de conformidad: el mismo guion (semilla, catálogo, comandos) en Rust y en Go da el mismo estado.
- Comparativa en `tests/bench` (valle de 256 × 256, misma carga) y gráfico.
- Imagen, job de CI y sustitución de Rust por `world-go` en Compose sin tocar el hub.

Fuera de alcance: desplegarlo por defecto (sigue siendo Rust), B8.

## Restricciones

- Escritor único (regla 1): `world-go` sustituye a Rust, nunca corre a la vez contra el mismo hub.
- El azar solo con el generador con semilla (regla 6); mismo orden de sorteo que Rust para que CP-10 valga entre implementaciones.
- u64 sin signo (regla 7).
- Estructura Go oficial: `cmd/` + `internal/`; código generado en `gen/go` (no versionado).
- Las pruebas de Rust son la especificación: se portan a Go.

## Modo de trabajo

- TDD estricto (`go test -race`); conformidad Rust ↔ Go como red de seguridad.
- Ruta: delegada. Entrega `stacked-to-main`, una rama y un PR por tramo de ~400–500 líneas.

## Tareas

- [x] T1 · Módulo, tipos y catálogo (+ job de CI `world-go`). Evidencia: precios probados por mutación (no TDD estricto en ese punto); `go test -race`, vet, gofmt, staticcheck y actionlint limpios.
- [x] T2 · Gotas, humedad, crecimiento y marchitamiento, evaporación, depósito y esqueleto de `Step`. Evidencia: RED por compilación; unas 60 pruebas portadas de Rust (`growth.rs` 18, `fire.rs` 4, `world.rs` 36, precios y etapas) más casos propios; 78 casos en verde. Incluye las funciones de incendio por casilla (las necesita la gota) y la detección de duplicados; quedan en T3–T5 las marcas TODO con la línea de Rust correspondiente.
- [x] T3 · Instalaciones, mejoras, precios y dueños inactivos. Evidencia: RED por compilación; 33 pruebas de Rust portadas.
- [x] T4 · Incendios y SplitMix64. Evidencia: RED por compilación; `rng.rs` 7/7 e incendios 22/26 (los 4 restantes dependen de restaurar instantáneas, T6); determinismo con la misma semilla.
- [x] T5 · Modo demo, reinicio y épocas, ventana de `action_id`, ranking. Evidencia: RED por compilación; `ranking.rs` 9/9 y 20 de `world.rs` (ventana, demo, reinicio, catálogo del reinicio, determinismo). En total 168 pruebas con `-race`; vet, gofmt y staticcheck limpios. Pendientes para T6: 6 pruebas de restauración e instantáneas.
- [x] T6 · Codificación de proto y de instantáneas. Evidencia: RED por compilación; 10 pruebas de restauración e instantáneas portadas (incluidas las 6 pendientes de T4–T5); `Restore` usa el catálogo vigente como Rust.
- [x] T7 · Prueba de conformidad Rust ↔ Go. Evidencia: 7 guiones de 300–1.000 ticks (catálogo real, incendios a 20.000 ppm, demo y reinicios, inválidos y duplicados, cifras propias, restauraciones con fuego, cifras extremas). Go coincide con Rust en todos a la primera; las instantáneas son idénticas byte a byte y Go retoma el valle desde 9 instantáneas hechas por Rust (comprobación del plan B). La prueba de Go detecta 6 mutaciones introducidas a propósito y la de Rust detecta un cambio en `rng.rs`. Los datos generados (guiones y salidas esperadas, ~1,8 MB) van en el último commit, sin revisión automática, como los SVG.
- [ ] T8 · Almacén, simulador (`boot_id`, reintentos, instantáneas) y servidor gRPC con salud; imagen y Compose.
- [ ] T9 · Comparativa en `tests/bench` y gráfico.

## Progreso

- T1 y T2 en `feature/b5-world-go-core` (ruta delegada); revisión de riesgo alto con 4 lentes aprobada. Avisos esperados mientras falten T3–T5: los comandos sin portar responden `invalid_command`. Fusionada (PR #60); `world-go` es comprobación obligatoria en `dev` y `main`. T3–T5 en `feature/b5-world-go-rules` (ruta delegada); revisión de riesgo medio aprobada sin hallazgos. Fusionada (PR #61). T6–T7 en `feature/b5-world-go-codec` (ruta delegada). Revisión de riesgo alto con 4 lentes sobre el código sin los datos generados: un hallazgo bloqueante confirmado (la prueba de conformidad falla sin sus datos en el commit revisado, consecuencia de separar los datos). La corrección intentada (omitir la prueba sin datos) la rechazó el validador con razón: debilitaba la prueba; se descartó. Revisión escalada, sin aprobar: se entrega con los datos incluidos en el último commit y el CI ejecuta la conformidad completa.
