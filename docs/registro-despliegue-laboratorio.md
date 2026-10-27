# Registro y Guía del Despliegue Repartido en el Laboratorio (Plan A y Plan A extendido)

Documento de registro y guía operativa del ensayo del 23 de octubre de 2026 (semana 6) y su extensión de alta disponibilidad B8. Consolida la topología de las 5 PCs principales del laboratorio (RES-07) más la 6.ª PC opcional de respaldo (nodo `standby`), el procedimiento paso a paso que se utilizó para levantar el sistema, la resolución de incidencias, la matriz de consecuencias ante el apagado de cada máquina y las pruebas a ejecutar.

> Este documento no contiene secretos: los valores reales se sustituyeron por marcadores antes de fusionar. Las claves, el hash y las contraseñas viven únicamente en `secrets.local` (ignorado por git, RNF-13).

---

## 1. Estado Actual del Despliegue

El despliegue repartido en las máquinas está **montado y configurado**, listo para su validación funcional, pruebas de carga y escenarios de tolerancia a fallos. Con la integración de la tarea **B8 (Orquestación, relevo Rust ↔ Go y conmutación por fallo / CP-12)**, la infraestructura admite tanto el despliegue estándar de 5 nodos como la incorporación de una **6.ª PC para el nodo standby** (`standby.yml`).

- **Conectividad:** Los nodos tienen sus puertos abiertos y alcanzables a través del cortafuegos de Windows (verificado con `diagnose.ps1`).
- **Orquestación B8:** El Hub principal soporta conmutación activa y relevo automático a simuladores secundarios (`WORLD_ADDR` multidestino con fencing por `hub_generation` y `boot_id`).
- **Pruebas de juego y carga:** Quedan pendientes las mediciones de rendimiento real y pruebas de tolerancia a fallos en la red del laboratorio.

---

## 2. Mapa de Distribución en las PCs del Laboratorio

Fuente: `ip.txt` del equipo y `deploy/nodes/lab-addresses.local`. Subred `10.68.17.0/24`, adaptador **Ethernet** (no las redes de VirtualBox `192.168.56.x` ni de WSL `172.x`).

| PC del laboratorio | IP (Ethernet) | Nodo | Archivo Compose | Servicios que corre | Puertos publicados | Secretos requeridos (`secrets.local`) |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **compu 3** | `10.68.17.3` | `db` | `db.yml` | PostgreSQL 16 | **5433** *(5432 lo ocupa Postgres nativo)* | `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` *(no usa clave asimétrica)* |
| **compu 9** | `10.68.17.9` | `persistence` | `persistence.yml` | Persistencia (Python) | **50051** (gRPC) | los de BD + `ADMIN_USERNAME`, `ADMIN_PASSWORD_HASH` |
| **compu 10 (`labbd-10`)** | `10.68.17.10` | `world` | `world.yml` | Simulador (Rust / world-go) | **50051** (gRPC) | *Ninguno* |
| **compu 12** | `10.68.17.12` | `hub` | `hub.yml` | Hub (Go), Chat (Go) y Bots *(a demanda)* | **8081** (WS), **8082** (Chat), **50051** (gRPC) | `TOKEN_PUBLIC_KEY` *(+ `HV_ADMIN_*` para bots)* |
| **compu 11** | `10.68.17.11` | `proxy` | `proxy.yml` | Nginx (HTTPS, Frontend) y API (Node/TS) | **8080** (HTTPS / WSS única entrada principal) | `TOKEN_PRIVATE_KEY` *(firma tokens JWT)* |
| **compu 2 (Opcional - B8)** | `10.68.17.2` | `standby` | `standby.yml` | Proxy standby, Hub standby (gen 2), world-go (standby) | **8080** (HTTPS), **8081** (WS), **50051** (gRPC) | `TOKEN_PRIVATE_KEY`, `TOKEN_PUBLIC_KEY` |

> **Nota de red:** La **compu 11** tiene la IP `10.68.17.11` (confirmada en el adaptador Ethernet). Siempre debe verificarse con `ipconfig`.

### URLs de acceso al sistema
- **Juego (Primario):** `https://10.68.17.11:8080`
- **Panel del sistema (salud y observabilidad primario):** `https://10.68.17.11:8080/sistema.html`
- **Documentación Swagger API:** `https://10.68.17.11:8080/api/docs`
- **Juego / Panel (Respaldo en compu 2 si el proxy principal cae):** `https://10.68.17.2:8080` (y `/sistema.html`)

*(Los certificados TLS de Nginx son autofirmados e incluyen automáticamente las IPs de los nodos en su SAN: el navegador avisará una sola vez; hacer clic en Avanzado › Continuar).*

---

## 3. Guía Paso a Paso Utilizada para el Despliegue

### Paso 0: Cortafuegos de Windows (Prerrequisito)
Las cuentas de estudiante no son administradoras. El encargado del laboratorio ejecutó este comando como Administrador en PowerShell en cada una de las PCs (incluyendo la 6.ª si se usa standby):
```powershell
New-NetFirewallRule -DisplayName "hydrovalley (proyecto Sistemas Distribuidos)" -Direction Inbound -Protocol TCP -LocalPort 8080,8081,8082,50051,5433 -RemoteAddress 10.68.17.0/24 -Action Allow -Profile Domain,Private
```

### Paso 1: Configurar Direcciones (`deploy/nodes/lab-addresses.local`)
Este archivo es **idéntico en todas las máquinas** (copiado a partir de `lab-addresses.example`):
```ini
LAB_DB_HOST=10.68.17.3
LAB_PERSISTENCE_HOST=10.68.17.9
LAB_WORLD_HOST=10.68.17.10
LAB_HUB_HOST=10.68.17.12
LAB_PROXY_HOST=10.68.17.11
# 6.ª máquina opcional para B8 (Compu 2 en 10.68.17.2):
LAB_STANDBY_HOST=10.68.17.2

HV_DB_PORT=5433
HV_PERSISTENCE_PORT=50051
HV_WORLD_PORT=50051
HV_HUB_PORT=8081
HV_PROXY_PORT=8080

# Puertos publicados por la 6.ª máquina (nodo standby)
HV_STANDBY_WORLD_PORT=50051
HV_STANDBY_HUB_PORT=8081
HV_STANDBY_PROXY_PORT=8080

HV_CHAT_PORT=8082
HV_CHAT_GRPC_PORT=50051

HV_REGISTRY=ghcr.io/iadev2005
HV_TAG=dev
TLS_EXTRA_SANS=
```

### Paso 2: Generar Claves y Secretos (`deploy/nodes/secrets.local`)
Se generó el par de claves Ed25519 (base64 de una sola línea) y el hash Argon2id con los parámetros exigidos por la API (`m=19456, t=2, p=1`).

**Plantilla general de secretos generados:**
```ini
# Nodo db y nodo persistence
POSTGRES_USER=hydrovalley
POSTGRES_PASSWORD=<contraseña de la base de datos>
POSTGRES_DB=hydrovalley

# Nodo persistence
ADMIN_USERNAME=admin
ADMIN_PASSWORD_HASH='<hash Argon2id del administrador; ver deploy/README.md>'

# Nodo proxy (y nodo standby si se usa la 6.ª PC)
TOKEN_PRIVATE_KEY=<clave privada Ed25519 en base64; ver deploy/README.md>

# Nodo hub (y nodo standby si se usa la 6.ª PC)
TOKEN_PUBLIC_KEY=<clave pública Ed25519 en base64>

# Bots (en nodo hub)
HV_ADMIN_USERNAME=admin
HV_ADMIN_PASSWORD=<contraseña del administrador, en claro>
```

**Distribución de secretos por máquina (principio de menor privilegio):**
- **Compu 3 (`db`):** Únicamente variables `POSTGRES_*` (usuario, contraseña y base de datos relacional; la base de datos **no utiliza ni conoce claves criptográficas asimétricas**).
- **Compu 9 (`persistence`):** Variables `POSTGRES_*`, `ADMIN_USERNAME` y `ADMIN_PASSWORD_HASH`.
- **Compu 10 (`world`):** No requiere archivo `secrets.local`.
- **Compu 12 (`hub`):** `TOKEN_PUBLIC_KEY`, `HV_ADMIN_USERNAME`, `HV_ADMIN_PASSWORD` (el Hub solo valida firmas con la clave pública, nunca firma tokens).
- **Compu 11 (`proxy`):** `TOKEN_PRIVATE_KEY` (la API en este nodo es el emisor principal que firma los tokens JWT con la clave privada Ed25519).
- **Compu 2 (`standby` - 6.ª PC opcional):** Requiere **ambas** claves: `TOKEN_PRIVATE_KEY` (para su réplica local de la API por si asume la entrada) y `TOKEN_PUBLIC_KEY` (para su réplica local del Hub que valida jugadores).

### Paso 3: Encendido de Docker Desktop
En cada una de las PCs participantes, abrir **Docker Desktop** y aguardar hasta que indique *Engine running*.

### Paso 4: Levantar cada Nodo (`deploy-node.ps1`)
Gracias a la propiedad **RNF-09** (arranque desordenado y reconexión automática sin `depends_on`), los nodos se levantan en cualquier orden:

- **En Compu 3 (`db`):**
  ```powershell
  powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node db -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Tag dev
  ```
- **En Compu 9 (`persistence`):**
  ```powershell
  powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node persistence -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Tag dev
  ```
- **En Compu 10 (`world`):**
  ```powershell
  powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node world -Addresses deploy\nodes\lab-addresses.local -Tag dev
  ```
- **En Compu 12 (`hub`):**
  ```powershell
  powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node hub -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Tag dev
  ```
- **En Compu 11 (`proxy`):**
  ```powershell
  powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node proxy -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Tag dev
  ```
- **En Compu 2 (`standby` - 6.ª PC opcional para B8 / CP-12):**
  ```powershell
  powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node standby -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Tag dev
  ```
  *Nota:* Este comando levanta en un solo paso los tres componentes de respaldo en la máquina secundaria (`proxy-standby` en `:8080`, `hub-standby` en `:8081` con `HUB_GENERATION=2`, y `world-standby` en `:50051` con la imagen `world-go` en modo `standby`).

### Paso 5: Comprobación de Conectividad
Desde cualquier PC:
```powershell
powershell -ExecutionPolicy Bypass -File scripts\lab\diagnose.ps1 -Addresses deploy\nodes\lab-addresses.local
```
Todos los nodos configurados deben reflejar el estado **`abierto`** (si la 6.ª máquina no se configuró, aparecerá como `NO CONFIGURADO` sin romper el diagnóstico).

---

## 4. Incidencias Detectadas y Soluciones Aplicadas

| Incidencia | Causa Raíz | Solución Aplicada |
| :--- | :--- | :--- |
| `diagnose.ps1` reportaba el puerto 8080 del proxy como `BLOQUEADO` | El proxy aún no estaba levantado o no coincidía la IP del adaptador Ethernet | Se confirmó la IP real con `ipconfig` en la compu 11 (`10.68.17.11`), se corrigió el archivo y se levantó el nodo `proxy`. |
| Error `unauthorized` al descargar imágenes de `ghcr.io` | Paquetes en GitHub Packages marcados como privados (D1) | `deploy-node.ps1` solicita `docker login ghcr.io` con usuario y un Personal Access Token (PAT) con permiso `read:packages`. |
| Escritura bloqueada de `secrets.local` desde scripts locales | Protección de carpetas de usuario / permisos en Windows 10 | Se crearon/pegaron los valores de los secretos de forma manual en disco. |
| Puertos marcados como `BLOQUEADO` antes de levantar servicios | `diagnose.ps1` no distingue entre firewall cerrado y puerto sin servicio escuchando | Para probar puramente el cortafuegos antes de levantar un servicio, se usa el modo listener: `diagnose.ps1 -Listen <puerto>`. |

---

## 5. Pruebas Pendientes a Realizar en el Laboratorio

Una vez con el sistema desplegado, se deben ejecutar y registrar estas pruebas para completar los requisitos del informe:

### A. Verificación Integral de Servicios (`health.ps1`)
Desde cualquier máquina:
- **Estado de los 5 servicios primarios:**
  ```powershell
  powershell -ExecutionPolicy Bypass -File scripts\lab\health.ps1 -Addresses deploy\nodes\lab-addresses.local
  ```
  Debe confirmar que los 5 servicios (`db`, `persistence`, `world`, `hub`, `api`) están en `up`, el tick avanzando y el Circuit Breaker en `closed`.

- **Estado de la réplica Standby (6.ª máquina):**
  ```powershell
  powershell -ExecutionPolicy Bypass -File scripts\lab\health.ps1 -Addresses deploy\nodes\lab-addresses.local -Standby
  ```
  Debe consultar al proxy de respaldo (`https://<LAB_STANDBY_HOST>:8080/api/system`) y confirmar el estado de sus servicios.

### B. Prueba de Carga con Bots (CP-11 / RNF-01 / RNF-02)
En la **compu 12 (`hub`)**, ejecutar:
```powershell
$compose = @('compose','-p','hv-hub','-f','deploy\nodes\hub.yml','--env-file','deploy\nodes\lab-addresses.local','--env-file','deploy\nodes\secrets.local','--profile','bots')
docker @compose pull bots
docker @compose run --rm bots load-test
```
*Criterio de aceptación:* 30 bots durante 60 segundos manteniendo $\ge 100\text{ gotas/s}$ y $p95 < 200\text{ ms}$.

### C. Escenarios de Tolerancia a Fallos
Con el panel `https://10.68.17.11:8080/sistema.html` abierto y un jugador activo:
1. **Caída de Persistencia (CP-07):** Detener `persistence` en compu 9 (`docker stop hv-persistence-persistence-1`) durante 10 s. Comprobar que en el panel pasa a `down`, pero los jugadores pueden seguir jugando fluidamente sin pausa del valle. Al reiniciar el contenedor, vuelve a `up`.
2. **Caída del Simulador (CP-05):** Detener `world` en compu 10 (`docker stop hv-world-world-1`) durante 10 s (o simular caída abrupta con `kill -s KILL`). Comprobar que el hub entra en pausa, abre el Circuit Breaker tras 3 fallos y acumula las gotas. Al reiniciar `world`, el simulador carga la última instantánea de PostgreSQL y drena la cola sin duplicar acciones ni entrar en bucle de recarga (invariante B8 `just_loaded_same_window`).
3. **Caída del Hub (CP-06):** Reiniciar `hub` en compu 12. Verificar reconexión automática de los navegadores.
4. **Relevo y Conmutación por Fallo del Simulador (CP-12 / B8):**
   - *Escenario A (Pausa de world):* Pausar el contenedor de `world` (`docker pause hv-world-world-1`). Si se configuró réplica secundaria (`standby.yml` o destino Go alternativo en `WORLD_ADDR`), el Hub conmuta tras expirar el timeout (~1 s), promueve la réplica secundaria y sigue avanzando ticks sin pérdida de estado. Al despausar el primario, su estado desfasado se invalida y recarga limpiamente desde PostgreSQL.
   - *Escenario B (Conmutación con Fencing de Hub):* Si el Hub principal se congela o detiene y un Hub secundario asume el control (`HUB_GENERATION=2`), el simulador acepta la nueva generación y rechaza con `fencing` (`FAILED_PRECONDITION`) cualquier intento posterior del Hub anterior (`HUB_GENERATION=1`).

---

## 6. Comportamiento del Sistema al Apagar o Perder Cada Máquina

En una arquitectura distribuida, la pérdida de un nodo físico o el apagado de una máquina tiene impactos diferenciados según la criticidad del servicio en el camino de datos:

| Máquina física apagada | Nodo y Servicios afectados | Comportamiento del sistema y experiencia del usuario | Mecanismo de recuperación y garantías |
| :--- | :--- | :--- | :--- |
| **Compu 3 (`db`)** | PostgreSQL 16 | **El juego en tiempo real NO se detiene.** Las gotas y el autómata celular siguen avanzando en el simulador. Sin embargo: <br>• La persistencia no puede guardar nuevas instantáneas (se omiten periódicamente).<br>• Nuevos registros o inicios de sesión (`/api/auth/login`) fallan limpiamente con `503 Service Unavailable` y un mensaje claro al usuario (RNF-06 / CP-07).<br>• Si la caída dura menos de lo que tarda el simulador en reiniciarse, **cero pérdida de datos** en memoria. | Al encender la máquina o reiniciar el contenedor, la persistencia reintenta su conexión gRPC/Postgres con retroceso exponencial (`backoff`) y reanuda el guardado de instantáneas automáticamente (RNF-09). |
| **Compu 9 (`persistence`)** | Persistencia Python (gRPC) | **Idéntico al fallo de `db`.** El camino crítico del juego (`navegador ↔ proxy ↔ hub ↔ world`) permanece completamente desacoplado (RNF-04). Los jugadores activos siguen jugando con latencia $< 200\text{ ms}$. La API responde `503` en endpoints de autenticación y ranking. | Al restaurarse el servicio, el simulador retoma el guardado periódico cada 50 ticks y la API vuelve a atender consultas de cuentas de inmediato. |
| **Compu 10 (`world`)** | Simulador (Rust / Go principal) | **El valle se congela temporalmente.** El Hub detecta la falta de respuesta en `Step`: <br>• Abre el *Circuit Breaker* tras 3 intentos fallidos.<br>• Mantiene el WebSocket `/play` abierto y encola las gotas entrantes (hasta su límite de búfer).<br>• **Con nodo Standby activo (B8):** El Hub realiza *failover* automático tras ~1 s a la réplica Go (`world-go`), promoviéndola de Standby a Ready; el juego continúa sin interrupción perceptible para los jugadores. | Al volver a encender la máquina 10:<br>• Sin standby: `world` carga la última instantánea de PostgreSQL, drena la cola del Hub y reanuda el tick.<br>• Con standby: Al reactivarse, detecta que el tick avanzó en el secundario (`min_expected_tick > restored_tick`), rota su `boot_id`, recarga la última instantánea y queda listo para un *failback* limpio sin conflicto de estados. |
| **Compu 12 (`hub`)** | Hub (Go WebSocket) y Chat | **Desconexión temporal del WebSocket en clientes.** <br>• Los navegadores detectan el cierre del socket `/play` y `/chat` y entran en reintento automático de reconexión con backoff.<br>• El estado del valle en el simulador (`world`) permanece **intacto y congelado en memoria** (no avanza ticks en vacío).<br>• **Con nodo Standby activo (B8):** El Hub secundario (generación 2) asume la atención a través del proxy de respaldo. | Al encenderse la máquina 12, los navegadores se reconectan automáticamente (CP-06). El Hub se enlaza de nuevo con `world`, comprueba la salud del simulador y reanuda el avance del reloj lógico (ticks). |
| **Compu 11 (`proxy`)** | Nginx (HTTPS) y API (TypeScript) | **Pérdida de la puerta de entrada única.** Ni la web (`https://...`), ni la API, ni los WebSockets son alcanzables desde el exterior.<br>• Sin embargo, el estado interno (`db`, `persistence`, `world`, `hub`) se conserva en sus respectivas máquinas sin corrupción.<br>• **Con nodo Standby activo (B8):** Si los clientes o el balanceador apuntan a la IP del `proxy-standby` (`10.68.17.2`), la navegación se restablece conectando al backend vivo. | Al reiniciar la máquina 11, Nginx vuelve a levantar su certificado TLS autofirmado, la API reestablece las conexiones gRPC con persistencia y simulador, y el tráfico vuelve a fluir con normalidad. |
| **Compu 2 (`standby` - Opcional B8)** | Réplicas Standby (Proxy, Hub gen 2, world-go) | **CERO impacto en la operación activa.** El sistema principal de las 5 PCs sigue funcionando al 100%. Únicamente se pierde la capacidad de conmutación por fallo automática ante una segunda caída concurrente de otro nodo. | Al encenderse la máquina, los contenedores arrancan en modo espera (`standby`) sin competir ni escribir en la base de datos hasta ser requeridos. |

---

## 7. Procedimiento de Cierre y Limpieza

Al terminar la sesión en el laboratorio, en cada una de las PCs participantes (las 5 principales o 6 con standby):
```powershell
# Bajar contenedores (conservando datos de BD):
powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node <nodo> -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Down

# Si se usó autenticación con PAT:
docker logout ghcr.io

# Eliminar variables de entorno locales:
Remove-Item deploy\nodes\lab-addresses.local, deploy\nodes\secrets.local -ErrorAction SilentlyContinue
```