# Panel del sistema (versión mínima)

## Objetivo

Una página para el profesor que muestre en vivo lo que pasa dentro del sistema (sección «El panel del sistema» del documento, RNF-08, *Health Endpoint Monitoring*): jugadores conectados y goroutines del hub, acciones por segundo y latencia, tick, duración de cada `Step`, humedad total, humedad aportada, incendios activos y el estado de cada servicio (verde si responde, rojo si no).

## Problema y por qué

Hoy el hub solo expone `/readyz` (tick, pausa, *breaker*, cola) y no mide acciones por segundo, latencia ni duración de `Step`; world y persistence solo tienen `grpc.health.v1`, que nadie consulta; y nada de eso pasa por nginx, la única puerta.

## Alcance

- Hub: métricas en memoria (sesiones, goroutines, acciones admitidas y aplicadas por segundo en ventana móvil, latencia de admisión a resultado final p50/p95, duración del último `Step` y su p95, humedad total e incendios activos del último estado) y `GET /stats` (JSON, puerto interno 8081).
- API: `GET /api/system` (lectura, sin token, con límite por IP) que agrega `/stats` del hub y el `grpc.health.v1` de world y persistence con plazos cortos; un servicio que no responde sale en rojo, nunca tumba la respuesta. Swagger.
- Página: sección del panel en el frontend (enlace propio), refresco periódico, verde y rojo por servicio; e2e.

Fuera de alcance: Prometheus y Grafana (nivel 2), históricos y gráficos.

## Restricciones

- Las direcciones por variables de entorno (`HUB_STATS_ADDR`), nunca `localhost` entre servicios; toda llamada con plazo.
- Solo lectura y sin datos personales: cifras agregadas, sin nombres ni tokens.
- Medir no puede frenar el tick: contadores baratos, sin bloqueos largos en el camino del `Step`.
- u64 como texto en JSON.

## Modo de trabajo

- TDD estricto. Ejecutores: `go test -race` (hub), vitest (API), `node --test` (frontend y e2e).
- Ruta: delegada (cada tarea toca 2 o más archivos no triviales).

## Tareas

- [x] T1 · Hub: métricas y `GET /stats`. Evidencia: RED por compilación y `/stats = 404`; `go test -race`, vet, staticcheck y gofmt limpios. Real (despliegue de demostración, 5 vecinos): `sessions` 6, `goroutines` 25, 1,1 acciones/s, latencia p50 77,8 ms y p95 107,3 ms, `Step` p95 2,57 ms, sin fallos. Ventanas: acciones y latencia, 10 s; `Step`, últimas 600 llamadas.
- [x] T2 · API: `GET /api/system` (agregador con estado por servicio). Evidencia: RED (5 archivos); `npm test` 263; lint, typecheck y build limpios. Real (despliegue de demostración): los cinco servicios en verde; con world parado, world `down: unavailable` y el hub en pausa con el *breaker* abierto, el resto sigue respondiendo; al arrancar world, todo vuelve a verde y el *breaker* se cierra. Sin dependencias nuevas; `HUB_STATS_ADDR` obligatorio, como `WORLD_ADDR`. Revisión de riesgo medio aprobada; aviso informativo (cuerpo sin consumir cuando el hub responde distinto de 200) que se corrige al empezar la T3. Fusionada (PR #40). T3 en `feature/system-panel-page` (ruta delegada).
- [ ] T3 · Página del panel y e2e.

## Criterios de aceptación

- Con el sistema levantado, el panel muestra todas las cifras de la sección del documento y el estado de los cinco servicios.
- Con world parado, world sale en rojo y el panel sigue funcionando.
- CI verde en todos los jobs.

## Entrega

- Estrategia: `stacked-to-main`; si una tarea queda por debajo del tamaño que pide revisión propia, se junta con la siguiente.

## Progreso

- T1 en `feature/system-panel-hub` (ruta delegada); revisión de riesgo medio aprobada (una sugerencia informativa en una prueba del transporte); fusionada (PR #39). T2 en `feature/system-panel-api` (ruta delegada).
