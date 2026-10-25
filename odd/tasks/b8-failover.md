# B8: mantener el juego con una máquina apagada (failover y relevo Rust ↔ Go)

## Objetivo

Mantener el juego en marcha cuando se apaga o se pausa (`docker pause`) cualquiera de las máquinas del despliegue repartido en el laboratorio (`world`, `hub`, `proxy`, `persistence` o `db`), con una pausa breve ($\le 10\text{ s}$) y sin perder más de 50 ticks ($5\text{ s}$), mediante una 6.ª máquina física de respaldo (`standby`) que aloja las réplicas en espera (`proxy-standby`, `hub-standby` y `world-standby` ejecutando `world-go`), garantizando por construcción el escritor único, el reloj lógico monótono y el blindaje cuando la máquina original se despausa (`docker unpause`) o retoma el mando (*failback* `world-go → Rust`).

Documento de decisión aprobado: [`docs/decisiones/b8-orquestacion.md`](../../docs/decisiones/b8-orquestacion.md).

## Problema y por qué

- En la topología actual del Plan A (montada en las 5 PCs del laboratorio el 23 de octubre de 2026: `10.68.17.3` `db`, `10.68.17.9` `persistence`, `10.68.17.10` `world`, `10.68.17.12` `hub`, `10.68.17.80` `proxy`), apagar `db` o `persistence` ya permite seguir jugando (CP-07 y CP-12), pero apagar `world`, `hub` o `proxy` detiene la partida porque sus direcciones son únicas (`WORLD_ADDR`, `HUB_ADDR`, `HUB_STATS_ADDR`) y no hay réplicas en espera.
- Dos simuladores o dos hubs activos a la vez violarían la Regla 1 (escritor único) y la Regla 3 (reloj lógico). Además, si un contenedor primario solo estaba pausado (`docker pause`) y luego se despausa (`docker unpause`), seguiría ejecutándose con estado obsoleto si no existe un mecanismo de *fencing* y de invalidación de memoria.
- Del mismo modo, si un simulador en espera cargara la instantánea al arrancar el contenedor en lugar de hacerlo al promoverse, arrancaría con un `tick` viejo y al recibir tráfico haría retroceder el reloj lógico (violando la Regla 8).

## Alcance

- **Contrato (`world.proto`, cambio aditivo):** Añadir en `StepRequest` los campos opcionales `uint64 hub_generation = 2` (para *fencing* de hubs) y `uint64 min_expected_tick = 3` (último `tick` confirmado que conoce el hub, para blindar el despausado y el regreso `world-go → Rust`).
- **T1 · Entrada doble y conmutación transparente (`E2`):**
  - Bloque `upstream` en `deploy/nginx/templates/default.conf.template` con servidor primario (`HUB_ADDR`) y `backup` (`HUB_BACKUP_ADDR`) para `/play` y `/chat`, más cabeceras CORS para orígenes autorizados del laboratorio.
  - Certificado TLS autofirmado compartido o con `TLS_EXTRA_SANS` cubriendo las IPs del proxy primario (`10.68.17.80`) y del `proxy-standby`.
  - Lista de URLs de entrada en el frontend (`useGameSocket`, `useReports`, `useSystemMetrics`, `auth`) y en los bots (`HV_BASE_URL` admitiendo varias URLs separadas por comas), más temporizador de silencio en el WebSocket (`3,5 s` sin recibir `state`) para detectar máquinas apagadas sin `TCP RST` y rotar automáticamente al `proxy-standby` sin perder la sesión JWT ni cambiar de IP a mano.
- **T2 · Simulador en espera (`W2`, relevo `Rust → world-go` y regreso `world-go → Rust`):**
  - Modo `WORLD_MODE=active|standby` (por defecto `active`) tanto en `src/world` (Rust) como en `src/world-go` (Go). En `standby`, el simulador no ejecuta `LoadSnapshot` al arrancar, sino en el instante en que recibe su primer `Step` de promoción (pasa `Standby → Loading → Ready`, genera su `boot_id` y devuelve `restored_tick`).
  - **Blindaje al despausar y regreso a Rust (*failback*):** Cuando `Step` recibe `min_expected_tick > world.tick()`, el simulador detecta que su proceso estuvo pausado mientras otro simulador avanzó el valle; descarta su estado en memoria, cancela/ignora cualquier guardado viejo, genera un nuevo `boot_id` y recarga la última instantánea desde `persistence` (escrita por `world-go`) antes de aplicar el paso.
  - `WORLD_ADDR` en `src/hub` (`worldclient`) y en `src/api` acepta una lista ordenada `primario:50051,respaldo:50051` con afinidad al nodo activo (*sticky*) y conmutación automática cuando se abre el *Circuit Breaker* o falla la llamada.
- **T3 · Hub en espera y *fencing* con blindaje al despausar (`H2`):**
  - Validación de `hub_generation` monótona en `src/world` y `src/world-go`: cualquier `Step` con `hub_generation > 0` y menor que el máximo visto recibe `FAILED_PRECONDITION` sin tocar el mundo.
  - En `src/hub`: soporte para `HUB_GENERATION` (por defecto `1`; `2` en el respaldo) y `HUB_PRIMARY_HEALTH_URL`. El `hub-standby` vigila `/healthz` del primario y solo activa su bucle de `Tick` cuando el primario acumula fallos consecutivos.
  - **Blindaje al despausar el Hub primario:** Si un hub recibe `FAILED_PRECONDITION` de `Step`, entra en estado `fenced` (retirado), detiene su bucle de tick, hace que `/healthz` y `/readyz` respondan `503` y cierra todos sus WebSockets abiertos con `StatusGoingAway (1001)` para que los clientes migren al hub activo.
  - `HUB_STATS_ADDR` en `src/api` acepta lista ordenada (`primario:8081,respaldo:8081`) y expone en `GET /api/system` qué instancia de `hub` y de `world` (`boot_id`, `hub_generation`, dirección activa) lleva el mando.
- **T4 · Nodo de respaldo (6.ª máquina) y scripts de laboratorio:**
  - Archivo `deploy/nodes/standby.yml` para la 6.ª máquina física del laboratorio (`LAB_STANDBY_HOST`), alojando `proxy-standby` (`:8080`), `hub-standby` (`:8081`) y `world-standby` con la imagen `hydrovalley-world-go` (`:50051`).
  - Actualización de `deploy/nodes/lab-addresses.example`, `deploy/nodes/simulate.sh` y `scripts/lab/` (`diagnose.ps1`, `deploy-node.ps1`, `health.ps1`).
- **T5 · Casos de prueba e2e de failover y despausado:**
  - Nuevos casos en `tests/e2e/` verificando con `docker pause` y `docker unpause`:
    1. Fallo de `world` (Rust) → promoción de `world-standby` (`world-go`) → `unpause` de Rust y regreso (*failback*) de `world-go` a Rust sin retroceder el tick ni perder más de 50 ticks.
    2. Fallo de `hub` primario → promoción de `hub-standby` → `unpause` del `hub` primario verificando que recibe `FAILED_PRECONDITION`, se retira y no hay dos escritores.
    3. Fallo de `proxy` primario → reconexión automática del cliente al `proxy-standby` conservando su sesión.
- **T6 · Documentación y diagramas:**
  - Actualizar `CLAUDE.md`, `docs/documento-inicial.md` (D-05 y «Resultados medidos»), `deploy/nodes/README.md` y los diagramas `04-secuencia-reinicio-rust.puml` y `08-despliegue.puml` (con sus `.svg` en un commit aparte).

Fuera de alcance: `T7` (demostración opcional con `kind` multinodo), descartada por decisión del equipo (D-B8-5) para priorizar el cierre y las mediciones en las máquinas reales del laboratorio.

## Restricciones

- Escritor único (Regla 1): en ningún instante dos hubs o dos simuladores modifican el mundo a la vez.
- El tick nunca retrocede (Reglas 3 y 8): ni al promover `world-go` ni al regresar a Rust tras despausar.
- La persistencia nunca es el árbitro del relevo (RNF-04, CP-07): con `persistence` y `db` caídos se sigue jugando.
- Solo se usan los puertos TCP ya abiertos en el cortafuegos del laboratorio (`8080, 8081, 8082, 50051, 5433`) y una cuenta sin permisos de administrador sobre Windows 10 + Docker Desktop + WSL 2.
- Los cambios en `world.proto` son estrictamente aditivos (`buf lint` y `buf breaking` en verde).

## Modo de trabajo

- TDD estricto en Go (`go test -race ./...`), Rust (`cargo test --locked`) y TypeScript (`npm test` / `pnpm test`): primero la prueba que falla para el modo `standby`, el *fencing* por `hub_generation`, la recarga por `min_expected_tick` tras `unpause` y la rotación de direcciones; después la implementación.
- Conformidad Rust ↔ Go (`tests/conformance`) siempre en verde.
- Una rama y un PR por tramo lógico desde `dev`.

## Tareas

- [x] **T1 · Entrada doble (`proxy-standby`) y conmutación automática en cliente y bots (`E2`).**
  - Archivos: `deploy/nginx/Dockerfile`, `deploy/nginx/templates/default.conf.template`, `deploy/nodes/proxy.yml`, `src/frontend/src/logic/play.ts`, `src/frontend/src/hooks/browserFetch.ts`, `src/frontend/src/hooks/useGameSocket.ts`, `src/frontend/src/hooks/useChatSocket.ts`, `src/bots/`.
  - Qué verificar: pruebas unitarias en Vitest (`src/frontend`) de rotación de URL base y del *watchdog* de silencio de `state`; pruebas en `src/bots` con lista de URLs.
- [x] **T2 · Simulador en espera (`world-standby` con `world-go`), lista `WORLD_ADDR` y regreso a Rust tras `unpause` (`W2`).**
  - Archivos: `proto/hydrovalley/world/v1/world.proto`, `src/world/` (`config.rs`, `simulator.rs`, `grpc.rs`, `server.rs`, `main.rs`), `src/world-go/` (`internal/config`, `internal/simulator`, `internal/server`, `cmd/world-go/main.go`), `src/hub/internal/worldclient/`, `src/hub/internal/hub/tick.go`, `src/api/src/` (`config.ts`, `grpc-ranking-source.ts`, `grpc-health.ts`).
  - Qué verificar: pruebas unitarias y de integración en Rust y en `world-go` de: (a) arranque en `WORLD_MODE=standby` sin cargar instantánea hasta el primer `Step`; (b) recepción de `min_expected_tick > world.tick()` tras una pausa que fuerza recarga de instantánea y rotación de `boot_id` sin que el tick retroceda; (c) conmutación en `worldclient` del hub y de la API.
- [x] **T3 · Hub en espera (`hub-standby`), *fencing* por `hub_generation` y retirada automática al despausar (`H2`).**
  - Archivos: `src/world/src/simulator.rs`, `src/world-go/internal/simulator/simulator.go`, `src/hub/internal/config/`, `src/hub/internal/hub/`, `src/hub/internal/transport/`, `src/hub/cmd/hub/main.go`, `src/api/src/hub-stats.ts`, `src/api/src/system-status.ts`.
  - Qué verificar: (a) el simulador rechaza con `FAILED_PRECONDITION` un `Step` con `hub_generation` menor que la máxima vista; (b) el hub que recibe `FAILED_PRECONDITION` al despausarse detiene su bucle, responde `503` en `/healthz` y `/readyz` y cierra sus WebSockets con `StatusGoingAway`; (c) el `hub-standby` permanece pasivo mientras el primario responde `/healthz` y se promueve al fallar este; (d) `/api/system` consulta `HUB_STATS_ADDR` en orden y reporta el hub activo.
- [ ] **T4 · Configuración de la 6.ª máquina (`standby.yml`), `simulate.sh` y scripts PowerShell del laboratorio.**
  - Archivos: `deploy/nodes/standby.yml`, `deploy/nodes/proxy.yml`, `deploy/nodes/hub.yml`, `deploy/nodes/lab-addresses.example`, `deploy/nodes/simulate.sh`, `scripts/lab/diagnose.ps1`, `scripts/lab/deploy-node.ps1`, `scripts/lab/health.ps1`.
  - Qué verificar: `simulate.sh` levanta los 5 nodos principales más el 6.º nodo `standby` (`proxy-standby`, `hub-standby` y `world-standby` con `world-go`) y verifica la salud del conjunto.
- [ ] **T5 · Casos de prueba e2e de failover (`world`, `hub`, `proxy`) y blindaje de `unpause`.**
  - Archivos: `tests/e2e/`, `deploy/nodes/simulate-check.mjs`.
  - Qué verificar: con `docker pause` sobre `world` (Rust), el juego sigue en `world-go` perdiendo $\le 50\text{ ticks}$; al hacer `unpause` de Rust y pausar `world-go`, el juego regresa a Rust sin que el tick retroceda; con `docker pause` y luego `unpause` sobre `hub` primario, `hub-standby` lleva la partida y el primario se retira al despausarse; con `docker pause` sobre `proxy` primario, el cliente conmuta al `proxy-standby`.
- [ ] **T6 · Documentación y diagramas UML.**
  - Archivos: `CLAUDE.md`, `docs/documento-inicial.md`, `deploy/nodes/README.md`, `docs/diagrams/04-secuencia-reinicio-rust.puml`, `docs/diagrams/08-despliegue.puml` (y sus `.svg` en commit aparte).
  - Qué verificar: regeneración limpia con `plantuml/plantuml:1.2026.8` y coherencia con contratos y código.

## Criterios de aceptación

- Apagar o pausar (`docker pause`) la máquina de `world` (Rust), de `hub` o de `proxy` mantiene la partida en curso con una pausa breve y $\le 50\text{ ticks}$ perdidos.
- Al despausar (`docker unpause`) la máquina original de `hub` o de `world`, nunca hay dos escritores ni retrocede el `tick`.
- Tras el relevo de Rust a `world-go`, el sistema puede regresar a ejecutarse en Rust conservando el estado que avanzó `world-go`.
- El jugador no necesita cambiar manualmente de IP ni volver a iniciar sesión cuando cae la máquina del `proxy` primario.
- El panel del sistema (`/sistema.html`) muestra qué instancia está activa.

## Progreso

- **Fases 0, 1 y 2 completadas (22–25 oct 2026):** Auditoría del repositorio, análisis de spikes, incorporación del registro real de despliegue en las 5 PCs del laboratorio (`10.68.17.3`, `.9`, `.10`, `.12`, `.80`) y cierre de las 5 decisiones del equipo en [`docs/decisiones/b8-orquestacion.md`](../../docs/decisiones/b8-orquestacion.md).
- **Fase 3 completada (25 oct 2026):** Creación de esta tarea de implementación (`odd/tasks/b8-failover.md`) y actualización de `docs/tareas-equipo.md`.
- **Fase 4 · T1 completada (25 oct 2026):** Implementación TDD de `STATE_SILENCE_MS` (`3,5 s`), `parseBackupHosts`, `createEndpointRotator` y `createFailoverFetch` en `src/frontend` (`play.ts`, `browserFetch.ts`, `useGameSocket.ts`, `useChatSocket.ts`); soporte de `HV_BASE_URL` con múltiples URLs separadas por comas y `ReadTimeout` de silencio en `src/bots` (`config.go`, `api.go`, `session.go`, `main.go`); y soporte en `deploy/nginx/` (`Dockerfile`, `default.conf.template`, `deploy/nodes/proxy.yml`) para `HUB_BACKUP_ADDR`, `CHAT_BACKUP_ADDR`, `PROXY_BACKUP_URLS` (`X-Proxy-Backups`) y cabeceras CORS/Origin entre proxies del laboratorio.
- **Fase 4 · T2 completada (25 oct 2026):** Ampliación aditiva de `StepRequest` en `world.proto` (`hub_generation`, `min_expected_tick`); implementación TDD de `WORLD_MODE=active|standby` y blindaje `ensure_active_and_fresh` / `EnsureActiveAndFresh` (recarga de instantánea y rotación de `boot_id` cuando `min_expected_tick > world.tick()`, permitindo el relevo `Rust → world-go` y el regreso `world-go → Rust` tras `unpause`) tanto en `src/world` como en `src/world-go`; y soporte de lista `WORLD_ADDR` con conmutación en `src/hub/internal/worldclient` y `src/api`.
- **Fase 4 · T3 completada (25 oct 2026):** Implementación TDD de validación monótona de `hub_generation` (`check_hub_generation` / `CheckHubGeneration` devolviendo `FAILED_PRECONDITION`) en `src/world` y `src/world-go`; soporte en `src/hub` para `HUB_GENERATION`, vigilancia activa del primario mediante `HUB_PRIMARY_HEALTH_URL` (`WatchPrimary`) y autodegradación inmediata al recibir `FAILED_PRECONDITION` (estado `fenced`, parada del bucle `Run`, respuesta `503` en `/healthz`, `/readyz` y `/stats`, y cierre de WebSockets con `StatusGoingAway 1001`); y rotación con afinidad pegajosa sobre `HUB_STATS_ADDR` en `src/api/src/hub-stats.ts`. A la espera de autorización para iniciar `T4`.
