# B7 · Prometheus y Grafana (Panel Profesional)

## Objetivo

Implementar el panel profesional de observabilidad (nivel 2, RNF-08) mediante Prometheus y Grafana, proporcionando series temporales e histórico para el sistema distribuido Hydrovalley, complementando la vista instantánea de `/sistema.html`.

## Problema y por qué

`/sistema.html` ofrece una visión instantánea consultada cada 2 s sin retención ni gráficos históricos. Durante las pruebas de carga (CP-11), fallos simulados (CP-05 a CP-08) y la defensa ante el profesor, es crucial observar la evolución temporal: picos de latencia, comportamiento del *Circuit Breaker*, acumulación de gotas y persistencia de métricas.

## Alcance

- **Hub en tiempo real (`src/hub`):** Endpoint `GET /metrics` en formato texto OpenMetrics/Prometheus (`:8081`), calculando métricas $O(1)$ sin frenar el bucle del tick. Expone sesiones, goroutines, acciones admitidas/aplicadas (totales y tasas), latencias p50/p95, duración y fallos de `Step`, tick, cola, pausa, *breaker*, humedad, gotas y fuegos.
- **API (`src/api`):** Endpoint `GET /metrics` (`:3000`) exponiendo el estado de las sondas de los 5 servicios (`hydrovalley_service_status`) y métricas de proceso.
- **Infraestructura (`deploy/`):**
  - Servicios `prometheus` y `grafana` en `deploy/compose.yml` bajo el perfil `monitoring`.
  - Configuración de scrape `deploy/monitoring/prometheus/prometheus.yml` cada 2 s.
  - Aprovisionamiento automático de datasource Prometheus y dashboard en Grafana (`deploy/monitoring/grafana/`).
  - Tablero versionado en JSON (`deploy/monitoring/grafana/dashboards/hydrovalley-overview.json`) con las 5 secciones de `/sistema.html` e histórico temporal.

## Restricciones

- **Aislamiento por perfil:** los contenedores de monitorización solo arrancan al indicar `--profile monitoring`. No afectan el arranque regular ni la prueba de humo del CI (`tests/e2e/run.sh`).
- **Medir no frena el tick:** secciones críticas atómicas o lecturas cortas ya preparadas en memoria por `h.Stats()`.
- **Acceso sin fricción:** Grafana configurado con autenticación anónima con rol Admin para la demostración en el laboratorio.

## Tareas

- [x] T1 · Hub: métricas Prometheus en `GET /metrics` (`src/hub/internal/transport/metrics.go`) y pruebas unitarias (`metrics_test.go`).
- [x] T2 · API: exposición de métricas y sondas en `GET /metrics` (`src/api/src/app.ts`) y prueba (`src/api/test/metrics.test.ts`).
- [x] T3 · Despliegue: configuración de Prometheus, datasources, dashboards y servicios en `deploy/compose.yml` bajo el perfil `monitoring`.
- [x] T4 · Tablero Grafana: tablero completo versionado en `deploy/monitoring/grafana/dashboards/hydrovalley-overview.json`.
- [x] T5 · Documentación: actualización de `deploy/README.md`, `deploy/.env.example` y `docs/tareas-equipo.md`.

## Criterios de aceptación

- El tablero de Grafana refleja con exactitud todas las cifras mostradas en `/sistema.html`.
- Ofrece perspectiva histórica con gráficos de líneas temporales para latencia, concurrencia, tasa de acciones, duración de `Step` e incendios.
- `docker compose -f deploy/compose.yml --profile monitoring up -d` arranca el sistema de monitorización sin errores de aprovisionamiento.
