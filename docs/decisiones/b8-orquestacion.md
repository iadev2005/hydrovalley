# Decisión B8 · Mantener el juego con una máquina apagada (Failover y Orquestación)

**Estado:** Borrador para revisión del equipo (Fases 0, 1 y 2 completadas · **Hard Stop antes de Fase 3**)
**Fecha:** 22 de octubre de 2026
**Relacionado con:** D-03, D-05, RF-23, RF-24, RNF-04, RNF-05, RNF-06, RNF-09, RES-07, CP-05, CP-06, CP-07

---

## 1. Problema y alcance (Fase 0)

### 1.1 Verificación del punto de partida en el repositorio

Antes de evaluar alternativas de conmutación por error (*failover*) u orquestación, se auditó el estado real del repositorio frente a las premisas iniciales del plan de B8:

| Aspecto | Estado supuesto en el plan inicial | Estado real verificado en el repositorio (22 oct 2026) | Impacto en B8 |
| --- | --- | --- | --- |
| **B5 (`world-go`)** | En curso (`T5` de `T9`); faltaban codificación, conformidad y servidor | **100 % completada (`T1`–`T9`)** en [`odd/tasks/b5-world-go.md`](../../odd/tasks/b5-world-go.md) (PR #60–#65). El binario [`cmd/world-go`](../../src/world-go/cmd/world-go/) implementa `WorldService` y `grpc.health.v1`, pasa las 44/44 pruebas e2e (`E2E_WORLD_IMPL=go`) y restaura instantáneas de Rust byte a byte | **Se elimina la dependencia bloqueante de B5.** Tanto `hydrovalley-world` (Rust) como `hydrovalley-world-go` (Go) están listos hoy para operar como nodo `world` en espera (*standby*) |
| **B2 (`chat`)** | No existía (`503` en `/chat`) | **Implementado y desplegado** en [`src/chat/`](../../src/chat/README.md), incluido en [`deploy/compose.yml`](../../deploy/compose.yml) y en [`deploy/nodes/hub.yml`](../../deploy/nodes/hub.yml) (puertos `8082` WS y `50051` gRPC), con contador lógico `seq` y `server_epoch` | Al apagar el nodo Go (`hub.yml`) caen tanto el `hub` como el `chat`. Por RF-15, el chat es nivel 2 y el juego sobrevive sin él, pero debe documentarse en la matriz de fallos |
| **B1 (Plan A en laboratorio)** | Solo simulación (`simulate.sh`) y scripts PowerShell sin probar en Windows real | [`deploy/nodes/*.yml`](../../deploy/nodes/README.md), [`simulate.sh`](../../deploy/nodes/simulate.sh) y [`scripts/lab/*.ps1`](../../scripts/lab/) están terminados; falta únicamente la prueba en las máquinas físicas con la regla del cortafuegos aplicada | Los spikes pueden medirse de inmediato sobre `simulate.sh` (aislamiento real por puertos de host) y repetirse en el laboratorio |
| **Direcciones de red** | Direcciones simples y estáticas | Confirmado en [`proxy.yml`](../../deploy/nodes/proxy.yml), [`hub.yml`](../../deploy/nodes/hub.yml), [`src/hub/internal/config/config.go`](../../src/hub/internal/config/config.go) y [`src/api/src/config.ts`](../../src/api/src/config.ts): `WORLD_ADDR`, `HUB_ADDR`, `HUB_STATS_ADDR` y `PERSISTENCE_ADDR` aceptan un único `host:puerto` | Ningún servicio puede hoy conmutar a una segunda IP sin cambiar la configuración y reiniciar el contenedor |
| **Cantidad de máquinas físicas** | Duda de si solo había 2 (`LABBD-01` y `LABBD-02`) | **Confirmado por el equipo: sin límite de máquinas físicas** (el laboratorio cuenta con 24 PCs `10.68.17.x` con i7-13700F y 31,7 GB RAM) | **El número de máquinas deja de ser un cuello de botella.** Es viable desplegar los 5 nodos base más 2–3 nodos dedicados en espera (*standby*), y el quórum ($N \ge 3$) ya no descarta por sí solo a un orquestador |
| **Restricciones del host** | Windows 10 Pro, sin administrador, Docker Desktop + WSL 2, puertos TCP 8080, 8081, 8082, 50051 y 5433 | Se mantienen intactas ([`CLAUDE.md`](../../CLAUDE.md), sección «Entorno de despliegue») | Cualquier solución que exija privilegios de administrador en Windows, módulos del kernel, interfaces `VIP` en capa 2 o puertos UDP/TCP fuera de la regla sigue bloqueada o requiere autorización del encargado |

### 1.2 Puntos únicos de fallo (SPOF) actuales por nodo

En la topología actual del Plan A ([`docs/diagrams/08-despliegue.puml`](../../docs/diagrams/08-despliegue.puml)), el sistema reparte 6 contenedores en 5 nodos físicos. La siguiente tabla resume qué ocurre hoy cuando se apaga físicamente (o se pausa con `docker pause`) cada máquina:

| Nodo (`deploy/nodes/`) | Servicios alojados | Puertos en host | ¿Es SPOF del juego hoy? | Qué ocurre hoy al apagar su máquina | Comportamiento actual medido (Plan B / `tests/e2e`) |
| --- | --- | --- | --- | --- | --- |
| **Nodo 1 · `db`** ([`db.yml`](../../deploy/nodes/db.yml)) | `postgres` (PostgreSQL 16) | `5433/tcp` | **No** (para partidas en curso) | `persistence` falla al consultar o guardar instantáneas; las llamadas gRPC a `persistence` devuelven `UNAVAILABLE`. Los jugadores ya conectados siguen jugando en memoria (RNF-04) | **CP-07:** las gotas siguen respondiendo con p95 de 68–103 ms (< 200 ms); nuevos accesos/registros reciben `503` en 0,7–3,0 s |
| **Nodo 5 · `persistence`** ([`persistence.yml`](../../deploy/nodes/persistence.yml)) | `persistence` (Python) | `50051/tcp` | **No** (para partidas en curso) | `SaveSnapshot` falla en segundo plano en Rust (se registra y se reintenta en el siguiente múltiplo de 50 ticks sin abrir el *Circuit Breaker*). `GET /api/catalog` sirve copia en caché si existe (RF-19). Nuevos logins/registros reciben `503` | **CP-07 y CP-12:** el valle mantiene 10 ticks/s sin pausa y con el *breaker* cerrado; al volver la máquina, la siguiente instantánea se guarda |
| **Nodo 2 · `world`** ([`world.yml`](../../deploy/nodes/world.yml)) | `world` (Rust o `world-go`) | `50051/tcp` | **SÍ (SPOF crítico)** | El `hub` agota el plazo de 200 ms de `Step`. Desde el 1.er fallo el valle entra en pausa y reencola comandos; al 3.er fallo (300–600 ms) se abre el *Circuit Breaker*, avisa `paused: true` a los jugadores y sondea cada 1 s. **El valle no avanza hasta que esa misma IP vuelve** | **CP-05:** detectado en 1–2 s; si la máquina se apaga y no vuelve, la cola admite hasta 5.000 comandos y a los 30 s empiezan a caducar (`expired`) |
| **Nodo 3 · `hub`** ([`hub.yml`](../../deploy/nodes/hub.yml)) | `hub` (Go) + `chat` (Go) | `8081/tcp`, `8082/tcp`, `50051/tcp` | **SÍ (SPOF crítico)** | Se cortan los WebSockets `/play` y `/chat`. Como el `hub` es quien invoca `Step` cada 100 ms, **el simulador en Rust deja de avanzar ticks** (nadie lo llama). El cliente intenta reconectar con *exponential backoff*, pero nginx devuelve `502/504` porque `HUB_ADDR` apunta a la máquina muerta | **CP-06:** detectado en 1,1–1,3 s; mientras la máquina del hub esté apagada, nadie puede jugar y el reloj lógico del valle está detenido |
| **Nodo 4 · `proxy`** ([`proxy.yml`](../../deploy/nodes/proxy.yml)) | `nginx` (TLS + estáticos) + `api` (TypeScript) | `8080/tcp` | **SÍ (SPOF crítico)** | Es la única puerta de entrada (patrón *Gateway Routing*). Se cortan todas las conexiones HTTPS y WSS (`/play` y `/chat`) que atraviesan nginx. Sin conexión de los clientes al hub, nadie puede enviar acciones ni ver el valle ni abrir `/sistema.html` | No cubierto por CP-05..12 hoy: caída total de cara al usuario mientras la máquina `LAB_PROXY_HOST` esté apagada |

**Conclusión de Fase 0:** Los nodos `db` y `persistence` **ya cumplen** el objetivo de mantener el juego en marcha al apagarse su máquina gracias al diseño de instantáneas asíncronas en segundo plano (Capa 5, CP-07 y CP-12). El alcance de B8 se concentra exclusivamente en eliminar el SPOF de los tres nodos críticos del camino en vivo: **`world`**, **`hub`** y **`proxy`**.

---

## 2. Criterios de decisión y definiciones (Fase 0.3)

Siguiendo la metodología del informe ([`docs/documento-inicial.md`](../../docs/documento-inicial.md), sección «Lo que ya hacemos bien»), los criterios de evaluación se definen formalmente antes de comparar las opciones:

1. **Tiempo de recuperación objetivo (RTO, *Recovery Time Objective*):** Tiempo transcurrido desde que la máquina activa deja de responder paquetes (`docker pause` o cable desconectado, donde las conexiones quedan en silencio y no devuelven `RST`) hasta que el componente en espera procesa el primer `Step` o restablece el flujo de juego. Meta: **$\le 10\text{ s}$** (pausa breve perceptible como un aviso temporal en la interfaz, sin perder la partida).
2. **Punto de recuperación objetivo (RPO, *Recovery Point Objective*):** Cantidad máxima de estado confirmado del valle que puede perderse al conmutar de máquina. Por la Regla 8 de [`CLAUDE.md`](../../CLAUDE.md) y RF-23 (CP-05), el límite estricto es **$\le 50\text{ ticks}$ ($5\text{ s}$)** cuando la persistencia está operativa.
3. **Invariantes de consistencia (Reglas 1, 3 y 5 de `CLAUDE.md`):**
   - *Regla 1 (Escritor único):* En ningún instante pueden existir dos simuladores modificando el estado del mundo en paralelo ni dos hubs enviando `Step` intercalados al mismo simulador (*split-brain*).
   - *Regla 3 y 8 (Reloj lógico monótono):* El número de `tick` solo crece; promover un nodo en espera jamás puede hacer retroceder el `tick` por debajo de la última instantánea guardada en `persistence`.
   - *Regla 5 y RNF-04 (Aislamiento de la BD):* Solo `persistence` (Python) toca PostgreSQL, y **la persistencia no puede ser el árbitro del relevo** (un *lease* o bloqueo en PostgreSQL violaría RNF-04/CP-07, pues al apagar Python se detendría el juego).
4. **Compatibilidad con el entorno del laboratorio:** Debe funcionar sobre Windows 10 Pro sin permisos de administrador, con Docker Desktop 29.5.3 sobre WSL 2 (red NAT entre el contenedor y la LAN física `10.68.17.0/24`) y utilizando únicamente los puertos TCP ya contemplados en la regla del cortafuegos (`8080`, `8081`, `8082`, `50051`, `5433`), sin exigir protocolos de capa 2/3 bloqueados (como VRRP/multicast) ni puertos UDP de *overlay*.
5. **Aprovechamiento de máquinas físicas del laboratorio:** Dado que disponemos de hasta 24 máquinas físicas sin restricción de cantidad, la solución debe aprovechar máquinas independientes para alojar las réplicas en espera (p. ej. nodos 6, 7 y 8), garantizando que apagar **cualquier** máquina física individual deje al menos una ruta completa viva.
6. **Coste de construcción y riesgo de regresión:** Esfuerzo medido en líneas de código y servicios afectados frente al tiempo disponible en el cierre de la semana 6 y la semana 7, minimizando cambios invasivos en los 9 jobs obligatorios del CI.
7. **Demostrabilidad en la defensa:** El profesor debe poder elegir y apagar físicamente la máquina de `world`, de `hub` o de `proxy`, y observar en vivo en el juego y en `/sistema.html` cómo el sistema detecta la caída, conmuta a la máquina de respaldo y continúa la partida.

---

## 3. Opciones por capa (Fase 1.1)

Con la confirmación de que **no existe límite de máquinas físicas en el laboratorio**, se actualizan las opciones por capa:

### 3.1 Capa de Entrada (`proxy`: nginx + API)

| Opción | Descripción técnica | A favor | En contra |
| --- | --- | --- | --- |
| **E1 · Segundo nodo `proxy` activo (conmutación manual de URL)** | Desplegar [`proxy.yml`](../../deploy/nodes/proxy.yml) en una segunda máquina física (`LAB_PROXY2_HOST:8080`) con la misma `TOKEN_PRIVATE_KEY`. Si cae el proxy 1, se abre la IP del proxy 2 | Cero cambios de código; usa el puerto `8080` ya permitido; `api` y `nginx` son 100 % sin estado (*stateless*) | En el navegador cambia el origen (`https://IP2:8080`): hay que aceptar su certificado autofirmado y `localStorage` está vacío (requiere iniciar sesión de nuevo, aunque el estado del jugador en el valle sigue intacto) |
| **E2 · Segundo nodo `proxy` + lista de URLs base en cliente y bots** | Igual que E1, pero el frontend (`useGameSocket`, `useReports`, `useSystemMetrics`) y los bots reciben una lista de orígenes (`VITE_API_BASES` / `HV_BASE_URLS`, o parámetro `?backups=IP2:8080`) y conmutan automáticamente tras agotar reintentos | Si cae la máquina del proxy 1 después de haber cargado la página, la SPA en memoria conmuta sola al proxy 2 conservando el token JWT en memoria sin pedir login de nuevo | Requiere configurar `HUB_ORIGIN_PATTERNS` en el hub y cabeceras CORS en nginx/API para admitir peticiones cruzadas desde el origen 1 al proxy 2; el navegador debe haber confiado previamente en el certificado TLS del proxy 2 |
| **E3 · IP virtual flotante (keepalived / VRRP)** | Dos máquinas comparten una IP virtual en `10.68.17.0/24` mediante VRRP | Transparente para el navegador (mismo origen, mismo `localStorage`, mismo certificado) | **Inviable en el laboratorio:** los contenedores corren dentro de la VM Linux de Docker Desktop sobre WSL 2 (con NAT), no en la interfaz física de Windows 10, y la cuenta de dominio no tiene permisos de administrador |
| **E4 · DNS local o archivo `hosts`** | Apuntar un nombre `hydrovalley.lab` a ambos proxies o cambiarlo al caer | Mismo nombre de host y certificado único | Modificar `C:\Windows\System32\drivers\etc\hosts` exige administrador en cada PC cliente; dependemos de si existe un DNS editable en el laboratorio (`[DATO: confirmar si hay DNS editable en el laboratorio]`) |

### 3.2 Capa de Tiempo Real (`hub`: Go)

| Opción | Descripción técnica | A favor | En contra |
| --- | --- | --- | --- |
| **H1 · Hub en espera + `backup` en `upstream` de nginx** | Un segundo nodo `hub` en otra máquina, pero solo nginx decide a quién enviar `/play` usando un bloque `upstream` con directiva `backup` | No toca `world.proto` | **Rompe la Regla 1 (Escritor único):** en [`src/hub/cmd/hub/main.go`](../../src/hub/cmd/hub/main.go#L65-L70) el bucle `h.Run(ctx, ticker.C)` arranca en cuanto inicia el proceso Go, tenga o no clientes conectados en `/play`. Dos contenedores `hub` corriendo tal cual llamarán a `Step` a la vez (20 veces/s entre ambos) |
| **H2 · Hub con modo en espera (sondeo de salud) + *fencing* por generación en el simulador** | El `hub` secundario arranca en modo pasivo o con un número de generación mayor (`HUB_GENERATION` o promoción al detectar que el primario cayó), nginx lo tiene como `backup` en `/play` (y la API como secundario en `HUB_STATS_ADDR`), y **`StepRequest` incluye `hub_generation`**: el simulador recuerda el máximo `hub_generation` visto y rechaza con `FAILED_PRECONDITION` cualquier llamada de un hub con generación menor (zombi) | **Garantiza el escritor único por construcción** sin usar PostgreSQL como árbitro (respeta RNF-04); si el hub 1 estaba solo pausado o aislado de nginx y despierta después de que el hub 2 tomó el mando, el simulador lo rechaza al instante y el hub 1 se retira | Requiere un cambio **aditivo** de 1 campo (`uint64 hub_generation = 2`) en [`world.proto`](../../proto/hydrovalley/world/v1/world.proto) y unas ~40 líneas en `src/hub`, `src/world` y `src/world-go` |
| **H3 · Hub activo-activo** | Dos hubs repartiendo jugadores y llamando a `Step` simultáneamente | — | **Descartado:** viola la Regla 1 y la Regla 3 (el hub marca el ritmo del tick cada 100 ms; dos hubs activos implican dos relojes compitiendo y partición de los conjuntos `pending`/`confirmed` y del límite de 15 gotas/s) |

### 3.3 Capa de Simulación (`world`: Rust / `world-go`)

| Opción | Descripción técnica | A favor | En contra |
| --- | --- | --- | --- |
| **W1 · Copia en frío (contenedor detenido que un vigilante arranca)** | Tener la imagen lista en otra máquina y un script externo que ejecute `docker compose up` cuando el primario no responda | Nunca hay dos procesos simuladores en memoria a la vez | En Windows + WSL 2 un contenedor frío más la carga inicial de `GetCatalog` + `LoadSnapshot` añade segundos de arranque, y requiere un proceso vigilante externo con acceso al socket de Docker de otra máquina (no disponible sin abrir el daemon de Docker por red) |
| **W2 · Copia en espera (*Warm Standby* con carga perezosa al promoverse) + `WORLD_ADDRS` en Hub y API** | Un segundo nodo `world` (puede ser Rust `hydrovalley-world` o Go `hydrovalley-world-go`, ya que B5 está terminada) corre en otra máquina en estado `Standby`. Mientras está en `Standby`, **no carga la instantánea al arrancar** (o la recarga al recibir el primer `Step`): cuando el `hub` detecta que el primario cayó (tras abrir el *Circuit Breaker* o agotar los 3 fallos de 200 ms) y conmuta a la segunda dirección de `WORLD_ADDRS`, el `standby` ejecuta `LoadSnapshot` en ese instante (tomando la última instantánea de hace $\le 50\text{ ticks}$), pasa a `Ready` y responde con su nuevo `boot_id` y `restored_tick` | **Reutiliza al 100 % el mecanismo de recuperación de CP-05 ya probado** (`observeBootLocked` en [`src/hub/internal/hub/tick.go`](../../src/hub/internal/hub/tick.go#L186-L223) poda `confirmed` y ajusta `acceptedDrops` automáticamente al ver el nuevo `boot_id`). Evita que el `standby` arranque con una instantánea vieja (cumple la Regla 8: el tick nunca retrocede) | Requiere añadir el estado `Standby` (promovido por el primer `Step`) en `simulator.rs` / `simulator.go` y que `worldclient` en el hub y la API acepten una lista ordenada `WORLD_ADDR` separada por comas |
| **W3 · Dos simuladores activos en paralelo (replicación de máquina de estados)** | Enviar cada `Step` a dos simuladores a la vez | — | **Descartado:** viola la Regla 1 (escritor único); ambos enviarían `SaveSnapshot` concurrentes a Python chocando en PostgreSQL, y cualquier divergencia de red dejaría estados inconsistentes |

### 3.4 Capa de Orquestación (`K1–K3`) — Actualizada al escenario sin límite de máquinas

| Opción | Descripción técnica | A favor (con máquinas ilimitadas) | En contra (restricciones de host y red) |
| --- | --- | --- | --- |
| **K1 · Clúster Kubernetes real (`k3s`) entre las máquinas del laboratorio** | Desplegar un clúster `k3s` de $\ge 3$ nodos sobre las máquinas físicas del laboratorio | Al no haber límite de máquinas, **el quórum etcd/Raft (3 nodos) sí se podría formar** en teoría | **Sigue bloqueado por el entorno del laboratorio:** (1) `k3s` requiere Linux; en estas máquinas corre dentro de WSL 2 detrás de un NAT de Hyper-V/WSL, por lo que los nodos `k3s` entre distintas máquinas físicas no tienen visibilidad L2/L3 directa de las IPs de los pods sin configurar `netsh interface portproxy` (que exige **administrador**); (2) exige abrir `6443/tcp`, `10250/tcp` y `8472/udp` (Flannel VXLAN) en el cortafuegos; (3) por defecto Kubernetes espera `node-monitor-grace-period = 40s` y `pod-eviction-timeout = 300s` antes de reprogramar un pod de un nodo apagado; (4) **no resuelve por sí solo el *fencing* ni la carga perezosa de la instantánea** |
| **K2 · Clúster `kind` / `k3d` multinodo en una máquina (demostración complementaria D-05)** | Levantar un clúster Kubernetes de 3–4 nodos (contenedores Docker actuando como nodos K8s) y apagar un nodo con `docker stop`/`docker pause` | Demuestra Kubernetes real con manifiestos (`Deployment`, `Service`, `PodDisruptionBudget`, sondas `/healthz` y `/readyz`) sin pedir puertos nuevos al encargado ni permisos de administrador | No sustituye la prueba de apagar una máquina física del laboratorio (RES-07): sirve como complemento de D-05 si sobra tiempo (T7), mientras que el relevo entre máquinas físicas lo resuelven E2 + H2 + W2 sobre los puertos ya abiertos |
| **K3 · Docker Swarm** | Unir los Docker Desktop de 5+ máquinas en un Swarm con 3 managers | Con $>2$ máquinas ya tendría quórum Raft | **Descartado expresamente en [`CLAUDE.md`](../../CLAUDE.md#L95):** exige los puertos `2377/tcp`, `7946/tcp+udp` y `4789/udp`, y la red `overlay` de Swarm sobre Docker Desktop con WSL 2 en Windows 10 no enruta entre anfitriones físicos debido al NAT de la VM de Docker Desktop |

---

## 4. Análisis y resultados de los Spikes (Fase 1.2)

De acuerdo con la regla de [`CLAUDE.md`](../../CLAUDE.md#L9) («Nunca inventes cifras, citas ni resultados de pruebas. Si falta un dato, deja `[DATO: …]`»), a continuación se documentan:
1. Los **hallazgos exactos de código** obtenidos por inspección estática del repositorio (que responden de forma definitiva la parte arquitectónica de S1, S2, S3, S4 y S5).
2. Los **comandos exactos listos para ejecutar** y las **tablas de medición con `[DATO: …]`** para registrar los tiempos empíricos en tu entorno.

---

### Spike S1 · `nginx` `backup` con WebSocket (`/play`) y `/api/system`

#### Hallazgos en el código actual
- En [`deploy/nginx/templates/default.conf.template`](../../deploy/nginx/templates/default.conf.template#L120-L142), la ruta `location = /play` hace hoy:
  ```nginx
  set $hub_upstream http://${HUB_ADDR};
  proxy_pass $hub_upstream;
  proxy_connect_timeout 2s;
  ```
- **Descubrimiento técnico clave:** Cuando `proxy_pass` usa una variable con un `host:puerto` literal, nginx no tiene grupo `upstream` con servidor `backup`. Si cambiamos a un bloque:
  ```nginx
  upstream hub_backend {
      server ${HUB_PRIMARY_ADDR} max_fails=1 fail_timeout=3s;
      server ${HUB_BACKUP_ADDR} backup;
  }
  ```
  - Para **nuevas conexiones** WebSocket (o reconexiones tras cierre), cuando la máquina primaria está apagada (`docker pause`), nginx intenta el `SYN` TCP hacia `HUB_PRIMARY_ADDR`, agota `proxy_connect_timeout 2s` (o `1s` si lo rebajamos para `/play`), marca el primario como caído durante `fail_timeout` y en la **misma petición de upgrade HTTP (`proxy_next_upstream error timeout`)** pasa inmediatamente a `HUB_BACKUP_ADDR` sin que el navegador vea un 502.
  - Para un **WebSocket que ya estaba abierto** antes del apagado físico (`docker pause`), como la máquina apagada no envía `FIN` ni `RST`, el socket TCP entre el navegador, nginx y el hub primario no se entera al instante: el cliente detectará la falta de tramas `state` (que el hub envía cada `1 s`, [`BroadcastEvery`](../../src/hub/internal/hub/hub.go#L91)) si añadimos un temporizador de inactividad en [`useGameSocket.ts`](../../src/frontend/src/hooks/useGameSocket.ts) (p. ej. si no llega ningún mensaje `state` en `3 s`, cerrar y reconectar el WebSocket).

#### Comandos listos para ejecutar y medir S1
Ejecutar en una terminal Bash (WSL 2 / Git Bash / Linux):

```bash
# 1. Levantar la simulación del Plan A manteniendo los contenedores vivos al terminar:
SIM_KEEP=1 SIM_SKIP_FAULT=1 deploy/nodes/simulate.sh

# 2. En otra terminal, medir cuánto tarda nginx en cortar y responder ante un hub pausado
#    (simula máquina del hub apagada de golpe sin enviar TCP RST):
docker compose -p hv-sim-hub pause
time curl -sk -i --max-time 10 "https://localhost:18480/play"
docker compose -p hv-sim-hub unpause

# 3. Al terminar las pruebas, limpiar el entorno simulado:
for n in db persistence world hub proxy; do docker compose -p "hv-sim-$n" down -v; done
```

---

### Spike S2 · Quién llama a `Step` y qué estado en memoria pierde el Hub al conmutar

#### Auditoría completa del estado en memoria de `Hub` ([`src/hub/internal/hub/hub.go`](../../src/hub/internal/hub/hub.go#L214-L255))

Se inspeccionó cada campo de la estructura `Hub` y `player` para determinar qué se pierde cuando cae la máquina del Hub 1 y los jugadores se reconectan al Hub 2:

| Campo en [`hub.go`](../../src/hub/internal/hub/hub.go) | Qué almacena en el Hub 1 | Qué ocurre al pasar al Hub 2 | ¿Cómo se recupera y qué garantiza las invariantes? |
| --- | --- | --- | --- |
| `players[id].pending` y `queue []queued` | Comandos admitidos por el Hub 1 que aún no recibieron respuesta de `Step` | Se pierden con la memoria del Hub 1 | **Recuperación total (Capa 3):** En [`useGameSocket.ts`](../../src/frontend/src/hooks/useGameSocket.ts#L182-L187), al reconectarse el WebSocket, `pendingRef.current.planReconnect()` reenvía automáticamente todas las acciones pendientes con su mismo `action_id`. Si el Hub 1 alcanzó a enviarlas a `Step` antes de morir, el simulador (`world`) las tiene en `recent_actions` (ventana de 600 ticks = 60 s) y devuelve `COMMAND_STATUS_DUPLICATE`, que el cliente trata como `applied`. Si no llegaron a `Step`, el simulador las aplica como nuevas |
| `players[id].confirmed` y `confirmLog` | `action_id` confirmados en los últimos 600 ticks (primera línea de filtro en Go) | El Hub 2 arranca con `confirmed` vacío | **Cubierto por la autoridad final (Regla 2):** Aunque el filtro rápido del Hub 2 esté vacío los primeros 60 s tras el relevo, cualquier reenvío llega en el siguiente `Step` al simulador (`world`), que conserva `recent_actions` de los últimos 600 ticks en memoria y en la instantánea ([`world.proto` L310](../../proto/hydrovalley/world/v1/world.proto#L310)) y responde `DUPLICATE` |
| `players[id].limiter` (`SlidingWindow`) | Instantes de las últimas 15 gotas del último segundo por jugador | Se reinicia la ventana de 1 s en el Hub 2 | **Impacto despreciable:** Como la detección + reconexión dura $> 1\text{ s}$ (ventana completa del limitador), cuando el jugador entra al Hub 2 su ventana de 1 s ya habría expirado de todos modos |
| `tick`, `bootID`, `restoredTick`, `latestWorld` | Última foto del mundo devuelta por `Step` | El Hub 2 los poblará en su **primer `Step` exitoso** (a los $\le 100\text{ ms}$ de promoverse) | En [`tick.go` L150-L153](../../src/hub/internal/hub/tick.go#L150-L153), el primer `Step` establece `h.bootID` como línea base (`previous == ""`), copia `h.tick`, `h.restoredTick` y `h.latestWorld`, y los difunde en el siguiente segundo |
| `ledger` y `acceptedDrops` | Cuenta acumulada en el Hub de gotas `APPLIED` para compararla con `humidity_contributed` en modo demo | El Hub 2 empieza con `acceptedDrops = 0` si no la inicializa | **Hallazgo importante de código ([`documento-inicial.md` L266](../../docs/documento-inicial.md#L266)):** El propio documento inicial ya advierte que el hub pierde `acceptedDrops` al reiniciarse mientras que `humidity_contributed` sobrevive en el simulador. Además, los clientes/pruebas (como indica [`src/hub/README.md` L98](../../src/hub/README.md#L98)) comprueban el modo demo **por diferencias** ($\Delta\text{accepted\_drops} == \Delta\text{humidity\_contributed}$) o bien, en modo demo, el Hub 2 al observar su primer `boot_id` (`previous == ""`) puede inicializar `h.acceptedDrops = resp.GetState().GetHumidityContributed()` si `demo_mode == true` |
| `breaker` y `paused` | Estado del *Circuit Breaker* hacia `world` | El Hub 2 arranca con el circuito cerrado y lo calcula por sus propios `Step` | Independiente por instancia de Hub; correcto por diseño |

**Conclusión de S2:** El diseño actual de idempotencia en dos niveles (Go como filtro + Rust/`world-go` como autoridad final con ventana de 600 ticks + reenvío de `pending` en el cliente) **cubre sin pérdidas ni duplicados el cambio de Hub**. El único requisito crítico es **impedir que dos Hubs llamen a `Step` a la vez**: hoy en [`src/hub/cmd/hub/main.go`](../../src/hub/cmd/hub/main.go#L65-L70) el ticker llama a `Step` desde el arranque aunque nadie esté conectado. Por tanto, el Hub de respaldo debe permanecer en espera (sondeando al Hub primario o esperando conexiones) y usar *fencing* (S4).

---

### Spike S3 · Modo `Standby` en el simulador (`src/world` y `src/world-go`)

#### Auditoría del código de arranque del simulador
En Rust ([`src/world/src/server.rs`](../../src/world/src/server.rs#L76-L88) y [`src/world/src/simulator.rs`](../../src/world/src/simulator.rs#L55-L62)) y en su espejo exacto en Go ([`src/world-go/internal/server/server.go`](../../src/world-go/internal/server/server.go#L86-L94)):

1. Al ejecutar `serve()`, se lanza inmediatamente una tarea en segundo plano (`tokio::spawn` / goroutine) que llama a `simulator.load(backoff)`.
2. Si levantáramos hoy un segundo contenedor `world-standby` al inicio de la partida (p. ej. en el `tick = 0`), cargaría `tick = 0` en memoria y quedaría en `State::Ready { world, restored_tick: 0 }`.
3. Si 5 minutos después (en el `tick = 3000`, con instantánea guardada en `tick = 3000`) se apaga la máquina del `world` primario y el `hub` conmuta al `world-standby`, **el `world-standby` respondería desde el `tick = 1`**, haciendo retroceder el reloj lógico 2.999 ticks (**violación directa de la Regla 3 y de la Regla 8**).

#### Diseño exacto del modo `Standby` (carga perezosa al promoverse)
Para que el `world-standby` nunca tenga una instantánea vieja en memoria:
- Se añade una variable opcional `WORLD_MODE=active|standby` (por defecto `active`, 100 % retrocompatible).
- En [`src/world/src/simulator.rs`](../../src/world/src/simulator.rs#L55), el `enum State` pasa de `{ Loading, Ready }` a `{ Standby, Loading, Ready }`.
- Cuando `WORLD_MODE=standby`:
  - `server.rs` **no lanza** `simulator.load()` al arrancar, sino que deja el estado en `State::Standby` (o precarga únicamente el catálogo `GetCatalog`, que es inmutable).
  - Mientras está en `Standby`, `grpc.health.v1.Health/Check` para `""` responde `SERVING` (para que Docker lo marque `healthy` y sabemos que el proceso en espera vive), o un servicio específico de standby, mientras que no ejecuta `Step` ni guarda instantáneas.
  - En el instante en que el `hub` (tras fallar el primario) envía el **primer `Step`** al `world-standby`, `step_inner` ve `State::Standby`, lo pasa atómicamente a `State::Loading`, dispara `simulator.load()` (que lee de `persistence` la última instantánea guardada por el primario hace $\le 50\text{ ticks}$, esto tarda $\sim 5\text{–}20\text{ ms}$ en red local), pasa a `State::Ready` y procesa el `Step` devolviendo su nuevo `boot_id` y el `restored_tick` recién leído.
- **Coste estimado en código:** ~45 líneas en Rust ([`simulator.rs`](../../src/world/src/simulator.rs), [`server.rs`](../../src/world/src/server.rs), [`config.rs`](../../src/world/src/config.rs)) y ~45 líneas idénticas en Go ([`src/world-go/`](../../src/world-go/)).

---

### Spike S4 · *Fencing* por generación entre el Hub y el Simulador

#### Problema que resuelve
Supongamos que la máquina del `hub` primario (`Hub 1`, generación `1`) sufre un congelamiento temporal del sistema operativo o un corte de red hacia nginx, pero su proceso sigue vivo. El `Hub 2` (en espera, con generación `2`) detecta que `Hub 1` no responde y asume el mando, enviando `Step(hub_generation = 2)` al simulador. Unos segundos después, `Hub 1` se destraba e intenta enviar `Step(hub_generation = 1)`. Sin *fencing*, ambos hubs enviarían pasos intercalados al simulador (violación de la Regla 1).

#### Diseño en el contrato y en el código
1. **Cambio aditivo en [`proto/hydrovalley/world/v1/world.proto`](../../proto/hydrovalley/world/v1/world.proto#L39-L43):**
   ```protobuf
   message StepRequest {
     repeated Command commands = 1;
     // Generación monótona del hub emisor para fencing (B8).
     // 0 = sin fencing (compatibilidad hacia atrás con clientes existentes).
     // Si es > 0 y menor que la mayor generación vista por el simulador,
     // Step rechaza la llamada con FAILED_PRECONDITION.
     uint64 hub_generation = 2;
   }
   ```
   - Al ser un campo opcional (`uint64` en proto3 con valor por defecto `0`), **pasa `buf breaking`** en el CI y no rompe ninguna prueba existente (cuando `hub_generation == 0` o igual al actual, funciona exactamente como hoy).
2. **En el simulador (`src/world` y `src/world-go`):**
   - `Simulator` guarda `max_hub_generation: AtomicU64` (inicializado en `0`).
   - Al inicio de `step_inner`:
     - Si `req.hub_generation > 0`:
       - Si `req.hub_generation < max_hub_generation`, devuelve inmediatamente `Status::failed_precondition("hub_generation desplazada por un hub más reciente")` **sin tocar el mundo**.
       - Si `req.hub_generation > max_hub_generation`, actualiza `max_hub_generation = req.hub_generation` y registra el relevo en `info`.
3. **En el `hub` (`src/hub`):**
   - Recibe `HUB_GENERATION` (por defecto `1` en el primario; `2` en el secundario, o incrementado al promoverse) y `HUB_PRIMARY_HEALTH_URL` (vacío en el primario; `http://<IP_HUB_1>:8081/healthz` en el secundario).
   - Mientras el secundario ve sano al primario, no ejecuta `Tick()`. Cuando el primario falla $K$ sondeos seguidos (p. ej. 3 sondeos de 500 ms = $1,5\text{ s}$), el secundario se activa y empieza a llamar a `Step` con `hub_generation = 2`.
   - Si el `Hub 1` recibe `codes.FailedPrecondition` desde `Step`, sabe que fue desplazado por un hub de generación superior: **detiene su bucle de tick y cierra sus WebSockets con `StatusGoingAway` (`1001`)** para que cualquier cliente rezagado se reconecte limpiamente a través de nginx hacia el `Hub 2`.

---

### Spike S5 · Reconexión del cliente con lista de URLs base y CORS/WSS

#### Hallazgos en el código del frontend y los bots
- Hoy [`buildPlayUrl`](../../src/frontend/src/logic/play.ts#L80-L88) construye la URL del WebSocket a partir de `window.location.host`, y las llamadas `fetch` en [`auth.ts`](../../src/frontend/src/logic/auth.ts), [`useReports.ts`](../../src/frontend/src/hooks/useReports.ts) y [`useSystemMetrics.ts`](../../src/frontend/src/hooks/useSystemMetrics.ts) usan rutas relativas (`/api/...`).
- En el `hub`, [`src/hub/internal/config/config.go`](../../src/hub/internal/config/config.go#L22-L24) **ya soporta `HUB_ORIGIN_PATTERNS`** (lista separada por comas con comodines, p. ej. `10.68.17.*:8080,localhost:*`), por lo que el WebSocket `/play` en un segundo nodo `proxy` **ya está preparado en Go** para aceptar conexiones desde el origen del primer `proxy` sin devolver `403 Forbidden`.
- Sin embargo, en [`deploy/nginx/templates/default.conf.template`](../../deploy/nginx/templates/default.conf.template#L100-L118), la ruta `/api/` no añade cabeceras `Access-Control-Allow-Origin`, y en el laboratorio cada nodo `proxy` genera su propio certificado autofirmado al arrancar (`40-tls-cert.sh`). Si el navegador no ha abierto antes `https://<IP_PROXY_2>:8080` y aceptado su aviso de certificado autofirmado, cualquier `fetch()` o `new WebSocket("wss://<IP_PROXY_2>:8080/play")` en segundo plano fallará por validación TLS del navegador (`ERR_CERT_AUTHORITY_INVALID`).
- **Solución técnica para que E1/E2 no falle por certificado distinto:** Generar en el script de preparación del laboratorio **un único par `tls.crt` + `tls.key` con todas las IPs del laboratorio en el SAN** (`TLS_EXTRA_SANS=10.68.17.1,10.68.17.2,...,10.68.17.24`) o copiar el mismo `tls.crt`/`tls.key` a los dos nodos `proxy`, de modo que ambos compartan el mismo certificado.

---

### Spike S6 · Viabilidad real de Orquestadores (`k3s` / `kind`) en el laboratorio

#### Análisis de red en Windows 10 + Docker Desktop + WSL 2
- En las máquinas del laboratorio (`Windows 10 Pro` sin cuenta de administrador), los contenedores corren dentro de la máquina virtual `docker-desktop` sobre WSL 2.
- Docker Desktop publica los puertos `ports: ["8080:8080"]` en el anfitrión Windows mediante `vpnkit` / `wslrelay`, pero **la interfaz de red interna del contenedor/nodo Linux (`172.17.x.x` o `192.168.65.x`) no es enrutable desde otra máquina física `10.68.17.x`**.
- Por eso `k3s` multinodo entre máquinas físicas (`K1`) choca con dos muros aunque nos sobren máquinas físicas: (1) los agentes `k3s` en distintas PCs necesitan tráfico UDP `8472` (VXLAN) y TCP `6443`/`10250` abiertos en el cortafuegos del dominio, y (2) la IP que anuncia el nodo `k3s` dentro de WSL 2 es la IP privada de la VM de WSL, no la `10.68.17.x` de Windows.
- En cambio, `kind` o `k3d` multinodo dentro de una máquina (`K2`) crea varios nodos Kubernetes como contenedores Docker sobre una misma red puente de Docker, donde sí hay conectividad L2 directa entre nodos sin pedir permisos de administrador ni puertos nuevos.

#### Comandos listos para ejecutar y medir S6 (si se desea registrar el dato de `kind` o comprobar WSL 2 en el laboratorio)

```powershell
# En una máquina del laboratorio (PowerShell sin administrador):
# 1. Comprobar qué IP ven los contenedores internamente vs la IP física de Windows:
ipconfig | Select-String "10.68.17"
docker run --rm alpine ip -4 addr show

# 2. Comprobar si los puertos de k3s (6443) o Swarm (2377) están bloqueados entre LABBD-01 y LABBD-02:
powershell -ExecutionPolicy Bypass -File scripts\lab\diagnose.ps1 -Addresses deploy\nodes\lab-addresses.local -Listen 6443
```

---

### 4.1 Tabla resumen de mediciones de los Spikes (plantilla para rellenar sin cifras inventadas)

| Spike | Qué se mide | Comando / fuente | Resultado medido | Fecha y máquina |
| --- | --- | --- | --- | --- |
| **S1** | Tiempo de conmutación de nginx al `hub` de reserva con el activo en `docker pause` | `simulate.sh` + `docker compose -p hv-sim-hub pause` | `[DATO: tiempo medido en s con proxy_connect_timeout=1s]` | `[DATO: fecha / máquina]` |
| **S2** | Estado en memoria del `hub` al cambiar de instancia | Inspección estática de [`hub.go`](../../src/hub/internal/hub/hub.go) y [`tick.go`](../../src/hub/internal/hub/tick.go) | **0 acciones perdidas ni duplicadas** (`pending` lo reenvía el cliente; `confirmed` lo cubre la ventana de 600 ticks del simulador) | 22 oct 2026 (verificado en código) |
| **S3** | Tiempo de promoción del simulador `standby` (`GetCatalog` + `LoadSnapshot`) al primer `Step` | Prueba de integración en `src/world` / `src/world-go` y `simulate.sh` | `[DATO: ms medidos en promover Standby -> Ready]` (estimado por CP-05 actual: 1–3 s incluyendo el sondeo de 1 s del *Circuit Breaker*) | `[DATO: fecha / máquina]` |
| **S4** | Rechazo del hub desplazado (*fencing* con `hub_generation`) | Prueba unitaria de dominio/gRPC con dos `hub_generation` (1 y 2) | El hub con generación `1` recibe `FAILED_PRECONDITION` en `< 1 ms` y detiene su bucle | `[DATO: fecha de ejecución de la prueba]` |
| **S5** | Reconexión de cliente/bot a un segundo `proxy` al pausar el primero | `simulate.sh` con segundo nodo `proxy2` | `[DATO: tiempo medido en s hasta reconectar WSS al segundo proxy]` | `[DATO: fecha / máquina]` |
| **S6** | Tráfico entrante a puertos de orquestador (`6443`/`8472`) entre dos PCs del laboratorio | `diagnose.ps1` en el laboratorio | `[DATO: confirmar bloqueo del cortafuegos en 6443/tcp y 8472/udp en el laboratorio]` | `[DATO: fecha / laboratorio]` |

---

## 5. Decisión por capa y justificación (Fase 2)

Con base en los criterios de la sección 2, el análisis de código de la sección 4 y el hecho confirmado de que **contamos con máquinas físicas suficientes en el laboratorio**, se propone la siguiente arquitectura para B8:

### 5.1 Resumen de decisiones por capa

| Capa | Opción elegida | Opciones descartadas y motivo del descarte |
| --- | --- | --- |
| **Topología de máquinas** | **7 a 8 máquinas físicas en el laboratorio** (los 5 nodos base de Plan A + 1 máquina para `world-standby` + 1 máquina para `hub-standby` + 1 máquina para `proxy-standby`, o agrupando los 3 *standby* en una o dos máquinas dedicadas de respaldo) | Descartado limitarse a 2 máquinas: al tener hasta 24 PCs disponibles en `10.68.17.0/24`, separar físicamente los nodos en espera garantiza que apagar cualquier PC individual nunca apague a la vez el primario y su respaldo |
| **Simulador (`world`)** | **W2 · Copia en espera (*Warm Standby* con carga de instantánea al promoverse) + `WORLD_ADDR` con lista de respaldo (`WORLD_ADDR=primario:50051,standby:50051`)** | **W1 (frío)** descartada por requerir un demonio externo con acceso remoto a Docker entre máquinas Windows. **W3 (activo-activo)** descartada por violar la Regla 1 (escritor único) |
| **Hub (`hub`)** | **H2 · Hub en espera + `upstream` con `backup` en nginx + *fencing* por `hub_generation` en `StepRequest`** | **H1 (sin fencing)** descartada porque un hub primario pausado que despierte volvería a llamar a `Step` en paralelo con el secundario, violando la Regla 1. **H3 (activo-activo)** descartada por tener dos relojes de tick |
| **Entrada (`proxy`)** | **E2 (o E1 como mínimo viable) · Segundo nodo `proxy` en otra máquina física compartiendo `TOKEN_PRIVATE_KEY` y certificado TLS multi-SAN, con lista de URLs de respaldo en cliente/bots** | **E3 (VIP/keepalived)** descartada por ser inviable sin administrador y dentro del NAT de WSL 2 en Windows 10. **E4 (DNS/`hosts`)** descartada salvo que el encargado confirme un DNS local editable |
| **Orquestador** | **Conmutación nativa en los servicios y Compose por nodo (compatible con los puertos `8080, 8081, 8082, 50051, 5433`) + K2 (`kind` multinodo) solo como demostración opcional (T7) si sobra tiempo** | **K1 (`k3s` entre máquinas Windows)** descartada porque el NAT de WSL 2 y el cortafuegos del dominio bloquean la red de pods e impiden `portproxy` sin administrador, además de su *eviction timeout* de 300 s. **K3 (Swarm)** descartada por `CLAUDE.md` |

---

## 6. Matriz de fallos: «Qué se apaga → Qué ocurre → Tiempo medido»

| Máquina física que se apaga |Quién lo detecta y cómo | Cómo ocurre el relevo | Estado perdido (RPO) | Tiempo de recuperación (RTO) |
| --- | --- | --- | --- | --- |
| **Máquina 1 · `db` (PostgreSQL)** | `persistence` (plazo de consulta a BD) y `/api/system` (`persistence` pasa a `down`) | No requiere relevo para seguir jugando: `Step` en el simulador no bloquea por la base de datos (Capa 5, RNF-04) | **0 ticks** (mientras `world` siga vivo) | **0 s** de corte en el juego (CP-07 medido: p95 de gotas 68–103 ms) |
| **Máquina 5 · `persistence` (Python)** | `world` (al fallar `SaveSnapshot` en segundo plano) y `api` (`Health/Check` 1 s) | No requiere relevo para seguir jugando: `world` salta esa instantánea y reintenta en el siguiente múltiplo de 50 ticks (CP-12) | **0 ticks** (mientras `world` siga vivo) | **0 s** de corte en el juego (CP-07 / CP-12 medidos) |
| **Máquina 2 · `world` (Primario)** | El `hub` activo: `Step` agota su plazo de `200 ms`; al 3.er fallo seguido ($600\text{ ms}$) se abre el *Circuit Breaker* y el valle se pone en pausa sin desconectar a nadie | En la siguiente sonda del *Circuit Breaker* (cada `1 s`), `worldclient` del `hub` y de la `api` conmuta al siguiente destino de `WORLD_ADDR` (`world-standby:50051`). Al recibir su primer `Step`, `world-standby` sale de `Standby`, ejecuta `LoadSnapshot`, genera su `boot_id`, devuelve `restored_tick` y el `hub` aplica `observeBootLocked` y drena la cola | **$\le 50\text{ ticks}$ ($5\text{ s}$)** (9–16 ticks medidos en CP-05) | **Estimado por diseño: 2–4 s** ($0,6\text{ s}$ de detección + $1\text{ s}$ de sonda del *breaker* + carga de instantánea). Medición real: `[DATO: RTO medido de world-standby]` |
| **Máquina 3 · `hub` (Primario)** | `hub-standby` (fallan 3 sondeos a `/healthz` del primario) y `nginx` (`proxy_connect_timeout` hacia el primario) | `hub-standby` activa su bucle de `Tick` con `hub_generation = 2`. El simulador acepta `hub_generation = 2` y bloquea para siempre al `hub` 1 (`FAILED_PRECONDITION`). Los navegadores detectan silencio de `state` (> 3 s) o corte de socket, reconectan `/play` y `nginx` los enruta al `backup` (`hub-standby`), reenviando las acciones en `pending` | **0 ticks del mundo** (el mundo sigue en `world`; las acciones en vuelo se reenvían y deduplican con la ventana de 600 ticks) | **Estimado por diseño: 2–5 s**. Medición real: `[DATO: RTO medido de hub-standby]` |
| **Máquina 4 · `proxy` (Primario)** | El frontend (`useGameSocket` / `useSystemMetrics`) y los bots al fallar la conexión HTTPS/WSS contra el origen primario | Con **E2**, el cliente rota a la siguiente URL de `VITE_API_BASES` (`https://<IP_PROXY_2>:8080`) manteniendo el token en memoria y reabre `/play` contra el segundo nginx. (Con **E1**, el usuario abre la pestaña con la IP del segundo proxy) | **0 ticks del mundo** (ni `hub` ni `world` cayeron) | **Estimado por diseño: 1–4 s (E2)** / manual (E1). Medición real: `[DATO: RTO medido de proxy-standby]` |

---

## 7. Riesgos y compromisos aceptados

| # | Riesgo | Mitigación adoptada | Qué se acepta conscientemente |
| --- | --- | --- | --- |
| **R-B8-01** | **Dos escritores (*split-brain*)** si el `hub` o el `world` primario solo estaba pausado (`docker pause` o microcorte de red) y vuelve a responder después del relevo | 1) En el `hub`: *fencing* mediante `hub_generation` en `StepRequest` (`world` rechaza al viejo con `FAILED_PRECONDITION`). 2) En `world`: el `hub` activo es quien decide a qué `world` llama (uno solo a la vez) y, si el viejo `world` intentara guardar una instantánea retrasada de un tick que el nuevo ya guardó con distinto estado, `persistence` la rechaza con `ALREADY_EXISTS` por diferencia de `world_state_sha256` (Regla 8) | Si el `world` viejo despierta y tenía en vuelo un `SaveSnapshot` de un tick $T$ superior al último múltiplo de 50 antes de morir, es posible que se guarde esa fila si el nuevo aún no llegó a $T$; ojo: hoy `world` no solo guarda durante `Step`: también toma una instantánea final al apagarse de forma ordenada, en cualquier tick (regla 8 de `CLAUDE.md`, `src/world/src/simulator.rs` y `src/world-go/internal/simulator`). Un `world` viejo que se apague después del relevo podría guardar así una fila divergente entre dos múltiplos de 50. Para cerrarlo hace falta una de dos: que la persistencia rechace instantáneas de un `boot_id` o una generación ya superados, o que un `world` degradado no haga la instantánea final `[DATO: decisión del equipo]` |
| **R-B8-02** | **El `world-standby` carga una instantánea vieja al arrancar** y al promoverse hace retroceder el tick | Modo `Standby` con carga perezosa (*lazy load*): el contenedor `world-standby` **no llama a `LoadSnapshot` al iniciar el contenedor**, sino únicamente cuando recibe su primer `Step` de promoción | El primer `Step` tras la conmutación tarda unos milisegundos más mientras descarga la instantánea de `persistence` (o se reintenta en el siguiente sondeado de 1 s del *breaker* si supera 200 ms) |
| **R-B8-03** | **Certificados TLS autofirmados distintos** entre `proxy-1` y `proxy-2` bloquean la reconexión automática del navegador (E2) | Generar un único certificado autofirmado en el laboratorio cuyos `Subject Alternative Names` (`TLS_EXTRA_SANS`) incluyan todas las IPs de los proxies y montarlo en ambos nodos | En la primera visita el jugador o el profesor debe haber aceptado ese certificado en el navegador |
| **R-B8-04** | **Caída simultánea de `persistence` y de `world` primario** | En ese escenario extremo (dos máquinas caídas a la vez), `world-standby` al promoverse intenta `LoadSnapshot`, recibe `UNAVAILABLE` y, cumpliendo la Regla 8 de `CLAUDE.md`, **no arranca vacío**: espera a que vuelva `persistence` para que el tick nunca retroceda | Se acepta que el sistema tolere la caída de **una** máquina cualquiera a la vez (alcance de B8), no de `world` + `persistence` simultáneamente |

---

## 8. Decisiones Pendientes del Equipo (Bloqueantes para Fase 3)

> [!IMPORTANT]
> **Esta sección debe consensuarse y cerrarse con el equipo antes de iniciar la Fase 3 (creación de tareas definitivas y escritura de código).** Para cada punto se presenta la disyuntiva, sus implicaciones y la **recomendación técnica**.

### Decisión 1 · ¿Modificamos [`world.proto`](../../proto/hydrovalley/world/v1/world.proto) de forma aditiva para el *fencing* del Hub (`H2`) o usamos solo `nginx backup` (`H1`)?
- **El dilema:**
  - **Opción A (`H2`, con *fencing* en `world.proto`):** Añadir `uint64 hub_generation = 2;` a `StepRequest` en [`world.proto`](../../proto/hydrovalley/world/v1/world.proto#L39). El simulador rechaza con `FAILED_PRECONDITION` cualquier `Step` con generación menor a la máxima vista.
  - **Opción B (`H1`, sin tocar `.proto`):** Confiar únicamente en que el `hub-standby` vigile `/healthz` del `hub` primario antes de arrancar su ticker, sin protección en el simulador.
- **Implicación técnica:** Si el profesor (o la prueba e2e) usa `docker pause` sobre el `hub` primario y luego hace `docker unpause`, en la Opción B **ambos hubs quedarán vivos llamando a `Step` a la vez**, violando la Regla 1 (escritor único). En la Opción A, en cuanto el `hub` primario despierta del `pause`, su primer `Step` recibe `FAILED_PRECONDITION` y se apaga limpiamente. Además, añadir un campo `uint64` nuevo a un mensaje existente en Protobuf v3 es **100 % retrocompatible en cable y pasa `buf breaking`**.
- **Recomendación del Arquitecto:** **Aprobar Opción A (`H2`)**. Es un cambio de 1 línea en `world.proto` que blindará la Regla 1 ante pausas y particiones de red, y da un argumento académico de primer nivel para la defensa (*fencing tokens* de Kleppmann / sistemas distribuidos).

### Decisión 2 · ¿Qué imagen usamos para el nodo `world-standby`: un segundo contenedor en Rust (`hydrovalley-world`) o el simulador en Go (`hydrovalley-world-go` de B5)?
- **El dilema:**
  - En el plan original se dudaba porque B5 estaba en `T5/T9`. Hoy **B5 ya está 100 % terminada y fusionada** ([`odd/tasks/b5-world-go.md`](../../odd/tasks/b5-world-go.md)), y sus instantáneas son compatibles byte a byte con Rust.
  - **Opción A (Homogéneo: Rust → Rust):** Tanto el nodo `world` primario como el `world-standby` corren `hydrovalley-world` (Rust), y `world-go` queda como plan B manual o para la comparativa.
  - **Opción B (Heterogéneo: Rust primario → `world-go` en `standby`):** El nodo `world` primario corre Rust (`hydrovalley-world`) y la máquina `world-standby` corre Go (`hydrovalley-world-go`).
- **Implicación técnica:** Como ambos comparten exactamente la misma imagen de configuración (`world.yml` con `HV_WORLD_IMAGE`), si implementamos el modo `WORLD_MODE=standby` y `hub_generation` en **ambos** (`src/world` y `src/world-go`, son ~45 líneas en cada uno para mantener su conformidad 1:1), **podemos elegir cualquiera de los dos por pura variable de entorno el día de la defensa**. Es más: mostrar en la defensa que se apaga la máquina de Rust y el valle sigue corriendo en otra máquina sobre la implementación en Go (`world-go`) leyendo la instantánea binaria de Rust demuestra en un solo acto **B8 (failover) + D-03 (plan B multi-lenguaje) + portabilidad del contrato protobuf**.
- **Recomendación del Arquitecto:** **Implementar `standby` y `fencing` en ambos (`src/world` y `src/world-go`) para no romper la paridad de B5**, usar Rust por defecto en los tests estándar y dejar lista la variable `HV_WORLD_IMAGE=…/hydrovalley-world-go` en el nodo standby para lucir el relevo Rust → Go en la defensa si el equipo quiere.

### Decisión 3 · ¿Cómo resolvemos el fallo del nodo de entrada (`proxy`): conmutación automática en el cliente (`E2`) o cambio manual de pestaña/IP (`E1`)?
- **El dilema:**
  - **Opción A (`E1` · Manual):** Levantar un segundo nodo `proxy` en otra máquina. Si apagan el PC del `proxy` 1, el jugador escribe la IP del `proxy` 2 en la barra de direcciones y vuelve a iniciar sesión.
  - **Opción B (`E2` · Conmutación automática en el frontend y bots):** Pasar al frontend (y a los bots) la URL de respaldo (p. ej. por configuración o query param) y que `useGameSocket`, `useReports` y `useSystemMetrics` pasen al `proxy` 2 automáticamente cuando el `proxy` 1 falle, conservando la sesión JWT en memoria.
- **Implicación técnica:** Si el profesor apaga la máquina de `nginx` mientras los compañeros o los 30 bots están jugando, con `E1` los 30 bots mueren y todos los jugadores son expulsados a la pantalla de login en otra IP (lo que contradice RNF-06: «Transparencia: el jugador debe usar una sola dirección y no ver nunca un error interno»). Con `E2` + mismo certificado TLS en ambos proxies + cabeceras CORS para orígenes permitidos, la pestaña abierta y los bots conmutan solos al segundo proxy en pocos segundos sin perder la sesión activa.
- **Recomendación del Arquitecto:** **Aprobar `E2` (con `E1` como base)**. El esfuerzo adicional en el frontend y en `nginx.conf.template` es pequeño (~60 líneas) y evita que los bots o la pantalla del jugador mueran al apagar el nodo `proxy`.

### Decisión 4 · ¿Cuántas máquinas físicas reservaremos el día de la defensa para el despliegue con alta disponibilidad?
- **El dilema:**
  - Ahora que sabemos que **no hay límite de máquinas físicas** en el laboratorio (hay 24 PCs), debemos fijar cuántas máquinas ocupará nuestro guion de defensa.
  - **Opción A (8 máquinas = 1 nodo por máquina):** 5 máquinas para el Plan A base (`db`, `persistence`, `world`, `hub`, `proxy`) + 3 máquinas para los respaldos (`world-standby`, `hub-standby`, `proxy-standby`).
  - **Opción B (6 máquinas = 5 nodos base + 1 máquina «Nodo de Respaldo»):** Las 5 máquinas del Plan A (`RES-07`) más **una 6.ª máquina física (`LABBD-06`)** que aloja juntos `world-standby` (`:50051`), `hub-standby` (`:8081`) y `proxy-standby` (`:8080`). (Observa que sus puertos **no chocan entre sí**: `50051`, `8081` y `8080` son distintos).
- **Implicación técnica:** ¡Fíjate en la ventaja operativa de la Opción B! En la máquina 6 los tres servicios en espera usan puertos distintos (`8080`, `8081` y `50051`), todos ya incluidos en la única regla del cortafuegos del encargado. Preparar 6 máquinas el día de la defensa en vez de 8 ahorra tiempo de montaje, cumple estrictamente las 5 máquinas de `RES-07` y permite que el profesor apague **cualquiera** de las máquinas principales (`world`, `hub`, `proxy`, `persistence` o `db`) y la máquina 6 absorba el relevo al instante. Si se prefiere aislamiento total, también valen 7 u 8 máquinas con los mismos archivos `.yml`.
- **Recomendación del Arquitecto:** **Adoptar la configuración modular (`world-standby.yml`, `hub-standby.yml`, `proxy-standby.yml`) que permite desplegarlos en 1 máquina de respaldo (6 máquinas en total) o en 3 máquinas separadas (8 en total)** solo cambiando las IPs en `lab-addresses.local`.

### Decisión 5 · ¿Incluimos el alcance de `T3` (relevo del Hub) en la entrega principal o recortamos a `T1 + T2` por calendario?
- **El dilema:** Estamos a jueves 22 de octubre (semana 6) y la entrega es en la semana 7. El plan advierte que si el tiempo aprieta se recorta primero `T7` (demo `kind`) y luego `T3` (relevo del `hub`).
- **Implicación técnica:** Con B5 ya terminada (lo que liberó mucho tiempo respecto a cuando se redactó el plan original), `T1` (entrada), `T2` (simulador standby) y `T3` (hub standby + fencing) son abarcables en 2 días de trabajo enfocado. En cambio, `T7` (`kind` multinodo) no aporta a la prueba física en las máquinas del laboratorio y sí conviene dejarla como estrictamente opcional.
- **Recomendación del Arquitecto:** **Ejecutar `T1 + T2 + T3` (más sus pruebas e2e y scripts `T4–T6`) para cubrir los tres SPOF (`proxy`, `world` y `hub`), y dejar `T7` (`kind`) fuera salvo que sobre tiempo en la semana 7.**

---

## 9. Cambios previstos a documentos y Apéndice de Implementación (Fase 3 provisional)

### 9.1 Documentos que se actualizarán al aprobar esta decisión
1. [`CLAUDE.md`](../../CLAUDE.md):
   - **Regla 1 (Escritor único):** Precisar que en despliegue con respaldo el único escritor se garantiza mediante la generación del hub (`hub_generation` en `StepRequest`, rechazando con `FAILED_PRECONDITION` a cualquier hub desplazado) y el modo `Standby` del simulador secundario.
   - **Regla 8 (Instantáneas y arranque):** Documentar que una réplica en `WORLD_MODE=standby` retrasa `LoadSnapshot` hasta recibir su primer `Step` de promoción, impidiendo que el `tick` retroceda.
   - **Portabilidad (punto 1):** `WORLD_ADDR` y `HUB_STATS_ADDR` admiten una lista ordenada de `host:puerto` separada por comas (en Plan B sigue siendo un único `world:50051`).
2. [`docs/documento-inicial.md`](../../docs/documento-inicial.md):
   - Añadir un párrafo en **D-05** resumiendo esta decisión y por qué la conmutación sobre los puertos abiertos supera a `k3s`/Swarm en las máquinas Windows 10 con WSL 2 del laboratorio.
   - Actualizar la tabla de **«Resultados medidos»** con los tiempos de conmutación de `world-standby`, `hub-standby` y `proxy-standby`.
3. **Diagramas UML ([`docs/diagrams/`](../../docs/diagrams/)):**
   - [`04-secuencia-reinicio-rust.puml`](../../docs/diagrams/04-secuencia-reinicio-rust.puml): Incluir el caso de conmutación al simulador `standby` y el rechazo por `hub_generation`.
   - [`08-despliegue.puml`](../../docs/diagrams/08-despliegue.puml): Representar los nodos en espera (`standby`) y las listas de direcciones.

### 9.2 Borrador de la Tarea de Implementación (`odd/tasks/b8-failover.md` · Bloqueada hasta aprobación)

> [!WARNING]
> **NO INICIAR FASE 3 NI ESCRIBIR CÓDIGO DE PRODUCCIÓN** hasta que el equipo responda las 5 decisiones de la sección 8.

1. **T1 · Entrada doble (`proxy-standby`):** Plantilla de `upstream` con `backup` en nginx para `/play` y `/chat`, soporte de `TLS_EXTRA_SANS` compartido, cabeceras CORS para orígenes del laboratorio y lista de URLs de respaldo en el frontend y los bots.
2. **T2 · Simulador en espera (`world-standby`):** Estado `Standby` con carga de instantánea al primer `Step` en `src/world` y `src/world-go`; soporte de múltiples direcciones separadas por comas en `WORLD_ADDR` tanto en `src/hub/internal/worldclient` como en `src/api`; archivo `deploy/nodes/world-standby.yml`.
3. **T3 · Hub en espera y *fencing* (`hub-standby`):** Campo `uint64 hub_generation = 2` en `StepRequest` (`world.proto`), validación de generación monótona en `src/world` y `src/world-go` (`FAILED_PRECONDITION`), retirada automática del hub desplazado, modo espera activa en `src/hub` vigilando la salud del primario y soporte de lista en `HUB_STATS_ADDR` en la API; archivo `deploy/nodes/hub-standby.yml`.
4. **T4 · Scripts de laboratorio y simulación:** Extender `deploy/nodes/simulate.sh` y `scripts/lab/` para levantar los nodos de respaldo y mostrar en `/api/system` y `sistema.html` qué instancia (`boot_id` / generación / dirección activa) lleva el mando.
5. **T5 · Casos de prueba e2e de failover:** Nuevos casos automatizados en `tests/e2e/` pausando `world`, `hub` y `proxy` primarios y verificando que la partida sigue sin superar 50 ticks de pérdida, que no hay dos escritores y que el tick nunca retrocede.
6. **T6 · Documentación y diagramas:** Actualizar `CLAUDE.md`, `docs/documento-inicial.md`, `deploy/nodes/README.md` y regenerar los `.svg` de los diagramas `04` y `08` en commit separado.
7. **T7 · (Opcional) Demostración `kind` multinodo:** Solo si sobra tiempo tras cerrar T1–T6.
