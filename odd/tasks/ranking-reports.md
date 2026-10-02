# Ranking y reportes de las 3R

## Objetivo

Que el jugador vea quién más ha hecho crecer el valle (RF-10, RF-11) y cuánta agua ha reducido, reutilizado y reciclado, él y el valle entero (RF-12).

## Problema y por qué

Los datos ya existen en el simulador (hojas ganadas, contadores de las 3R y de RF-27), pero nadie los expone: `WorldService.GetRanking` y `ReportService.GetReport` responden `UNIMPLEMENTED`, y la persistencia no guarda los contadores por jugador de cada instantánea.

## Alcance

- Rust: `WorldService.GetRanking` (hojas ganadas, de mayor a menor, con `limit`).
- Persistencia: guardar `snapshot_players` en cada `SaveSnapshot` (diagrama 07) y servir `ReportService.GetReport` por jugador y global, dentro de la época actual.
- API: `GET /api/ranking` (Rust + `AccountService.GetUsernames`) y `GET /api/reports/me` y `/api/reports/global` con el token del jugador; Swagger.
- Página: paneles de ranking y de reporte; e2e.

Fuera de alcance: la equivalencia litro ↔ humedad (sigue siendo `[DATO]`; los contadores se muestran por separado), gráficos.

## Restricciones

- Las del `CLAUDE.md`: solo Python toca la base de datos; el ranking lo completa la API con `GetUsernames`; reintentos solo en llamadas idempotentes (las tres lo son).
- Los reportes salen de las instantáneas: pueden ir hasta 5 s por detrás del valle.

## Modo de trabajo

- TDD estricto. Ejecutores: `cargo test`, pytest, vitest, `node --test`.
- Ruta: delegada (cada tarea toca 2 o más archivos no triviales).

## Tareas

- [x] T1 · Rust: `GetRanking`. Evidencia: RED (9 de dominio + 4 de integración); world 231 pruebas; clippy/fmt limpios. Empates por `player_id` y rango 1, 2, 2, 4; tope de 1.000 entradas; jugadores con 0 hojas incluidos.
- [ ] T2 · Persistencia: `snapshot_players` y `ReportService.GetReport`.
- [ ] T3 · API: `/api/ranking` y `/api/reports/*` autenticados.
- [ ] T4 · Página y e2e.

## Criterios de aceptación

- El ranking ordena por hojas ganadas y muestra nombres de usuario.
- Los totales del reporte cuadran con las acciones ejecutadas (verificación de RF-12).
- CI verde en los ocho jobs.

## Entrega

- Estrategia: `stacked-to-main` (la misma de las funcionalidades anteriores): cada tarea en su rama desde `dev` y su PR contra `dev`.

## Progreso

- T1 en `feature/ranking-world` (ruta delegada).
