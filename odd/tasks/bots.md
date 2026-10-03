# Bots

## Objetivo

Jugadores simulados que hagan visible el sistema distribuido y midan lo que el documento promete: vecinos que dan vida al valle, la prueba de carga de CP-11 (RNF-01 y RNF-02), el tramposo (RF-05, RF-09, RF-06, CP-03) y el bombero (RF-29, RF-30, CP-04). Sección «Los bots» del documento.

## Problema y por qué

Sin bots no hay forma de demostrar la concurrencia ni medir el rendimiento: con cuatro o cinco personas el valle está casi vacío y CP-11 no se puede ejecutar.

## Alcance

- API: `POST /api/admin/bot-accounts` (solo con token de administrador) que crea N cuentas `bot-…` y devuelve sus tokens, sin pasar por el límite público de registro (5 cada 10 min por IP). Las cuentas son jugadores normales.
- `src/bots` (Go): cliente común (acceso como administrador, aprovisionamiento, WebSocket `/play`, marcos JSON) y los cuatro escenarios: vecinos, carga, tramposo y bombero.
- La prueba de carga mide gotas aceptadas por segundo y latencia (p50 y p95, desde el envío hasta el resultado final) y comprueba el invariante del modo demo; deja un informe en JSON.
- Despliegue: servicio `bots` en `compose.yml` bajo un perfil (no arranca por defecto), job de CI y una prueba de extremo a extremo corta.

Fuera de alcance: k6 (el bot de carga es el nuestro, en Go), el panel del sistema (va después), el despliegue repartido.

## Restricciones

- Los bots son clientes: solo hablan por `/api/*` y `/play` a través de nginx, como cualquier jugador. Nunca llaman por gRPC a los servicios internos.
- Configuración por variables de entorno (dirección base, credenciales del administrador, número de bots, duración).
- Las cifras de RNF-01 y RNF-02 son objetivos del documento; el informe dice lo medido, sin inventar resultados.

## Modo de trabajo

- TDD estricto. Ejecutores: vitest (API), `go test -race` (bots), `node --test` (e2e).
- Ruta: delegada (cada tarea toca 2 o más archivos no triviales).

## Tareas

- [x] T1 · API: `POST /api/admin/bot-accounts`. Evidencia: RED 24/24; API 217 pruebas; lint/typecheck/build ok; e2e 12/12. Solo administradores (401/403), 10 peticiones/min por administrador, hasta 100 cuentas, Argon2id con concurrencia 4, `Register` nunca se reintenta; ante un fallo, 503 con `created` y sin tokens.
- [x] T2 · Bots: cliente común y escenario de vecinos. Evidencia: RED por compilación; `go test -race` ok (35 funciones); vet/staticcheck/gofmt limpios; `docker build` ok. Prueba real de 30 s con 3 vecinos contra el hub: 21 gotas enviadas, 21 `applied`. Revisión: riesgo medio, aprobada sin hallazgos (commit fb1588a).
- [x] T3 · Bots: prueba de carga (CP-11) con métricas e informe. Evidencia: RED por compilación (hub y bots); `go test -race` ok en ambos módulos; vet/staticcheck/gofmt limpios; `buf lint` ok. Ejecución real (Mac, despliegue de demostración, no el laboratorio): 30 bots · 60 s, 7.156 enviadas y 7.156 aceptadas, 119,3 gotas/s, mediana 53,2 ms, p95 99,3 ms, humedad aportada 7.156 = gotas aceptadas del hub. RNF-02 se evalúa como fracción de gotas resueltas en < 200 ms (las no resueltas cuentan como lentas).
- [x] T4 · Bots: tramposo (CP-03) y bombero (CP-04). Evidencia: RED por compilación; `go test -race`, vet, staticcheck y gofmt limpios. Ejecución real del tramposo: 501 enviadas en 10 s, 150 admitidas (15,0/s, máximo 15 en un segundo), 351 `rate_limited`; RF-09 aplicada 1 vez con 2 `duplicate`; RF-06 `invalid_message` y `unknown_type` sin cortar la conexión. Bombero: 120 s con 3 bots (y 40 s sembrando plantas) sin ningún incendio en la ventana; CP-04 solo queda cubierto por las pruebas con mundo simulado. Pendiente: ejercitarlo contra un valle con casillas secas con planta.
- [x] T5 · Despliegue: servicio con perfil, CI y e2e corta. Evidencia: RED del alias `load-test`; `go test -race`, vet, staticcheck, gofmt y actionlint limpios; `docker compose config` sin `bots` por defecto y con él bajo `--profile bots`; e2e 13/13 (nuevo caso del tramposo, `Resultado: CUMPLE`). Hallazgo: `ci.yml` aún no publica imágenes en `ghcr.io`, aunque `CLAUDE.md` lo da por hecho; queda como tarea aparte.

## Criterios de aceptación

- Los cuatro escenarios funcionan contra el sistema levantado con Docker Compose.
- La prueba de carga informa gotas/s, p50, p95 y si se cumplió el invariante del modo demo.
- CI verde en todos los jobs.

## Entrega

- Estrategia: `stacked-to-main` (la misma de las funcionalidades anteriores). Si una tarea queda por debajo del tamaño que pide revisión propia, se junta con la siguiente en el mismo PR.

## Progreso

- T1 (PR #34) y T2 (PR #35) fusionadas en `dev`. T3 en `feature/bots-load` (ruta delegada: hub, contrato y bots); revisión de riesgo medio aprobada sin hallazgos (PR #36, fusionada). T4 fusionada (PR #37), revisión aprobada sin hallazgos. T5 en `feature/bots-deploy` (ruta delegada); revisión de riesgo alto con 4 lentes aprobada (una sugerencia informativa de legibilidad en `tests/e2e/run.sh`); al fusionar, añadir `bots` a las comprobaciones obligatorias.
- Decisión T3: el bot de carga es cliente y no ve `humidity_contributed` ni las gotas aceptadas del hub (solo se registraban en el log). Se añaden ambos al mensaje `state` de `/play` (campos nuevos, aditivos); el panel del sistema también los necesita. El invariante se comprueba por diferencias entre el inicio y el final de la prueba.
