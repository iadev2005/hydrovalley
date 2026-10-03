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
- [ ] T2 · Bots: cliente común y escenario de vecinos.
- [ ] T3 · Bots: prueba de carga (CP-11) con métricas e informe.
- [ ] T4 · Bots: tramposo (CP-03) y bombero (CP-04).
- [ ] T5 · Despliegue: servicio con perfil, CI y e2e corta.

## Criterios de aceptación

- Los cuatro escenarios funcionan contra el sistema levantado con Docker Compose.
- La prueba de carga informa gotas/s, p50, p95 y si se cumplió el invariante del modo demo.
- CI verde en todos los jobs.

## Entrega

- Estrategia: `stacked-to-main` (la misma de las funcionalidades anteriores). Si una tarea queda por debajo del tamaño que pide revisión propia, se junta con la siguiente en el mismo PR.

## Progreso

- T1 en `feature/bots-provisioning` (ruta delegada).
