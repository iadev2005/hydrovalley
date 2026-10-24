# Registro y Guía del Despliegue Repartido en el Laboratorio (Plan A)

Documento de registro y guía operativa del ensayo del 23 de octubre de 2026 (semana 6). Consolida la topología de las 5 PCs del laboratorio (RES-07), el procedimiento paso a paso que se utilizó para levantar el sistema, la resolución de incidencias y las pruebas pendientes a ejecutar.

> Este documento no contiene secretos: los valores reales se sustituyeron por marcadores antes de fusionar. Las claves, el hash y las contraseñas viven únicamente en `secrets.local` (ignorado por git, RNF-13).

---

## 1. Estado Actual del Despliegue

El despliegue repartido en las cinco máquinas está **montado y configurado**, listo para su validación funcional, pruebas de carga y escenarios de tolerancia a fallos.

- **Conectividad:** Los 5 nodos tienen sus puertos abiertos y alcanzables a través del cortafuegos de Windows (verificado con `diagnose.ps1`).
- **Pruebas de juego y carga:** **Aún no se han ejecutado.** Quedan pendientes las mediciones de rendimiento real y pruebas de tolerancia a fallos en la red del laboratorio.

---

## 2. Mapa de Distribución en las 5 PCs

Fuente: `ip.txt` del equipo y `deploy/nodes/lab-addresses.local`. Subred `10.68.17.0/24`, adaptador **Ethernet** (no las redes de VirtualBox `192.168.56.x` ni de WSL `172.x`).

| PC del laboratorio | IP (Ethernet) | Nodo | Archivo Compose | Servicios que corre | Puertos publicados | Secretos requeridos (`secrets.local`) |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **compu 3** | `10.68.17.3` | `db` | `db.yml` | PostgreSQL 16 | **5433** *(5432 lo ocupa Postgres nativo)* | `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` |
| **compu 9** | `10.68.17.9` | `persistence` | `persistence.yml` | Persistencia (Python) | **50051** (gRPC) | los de BD + `ADMIN_USERNAME`, `ADMIN_PASSWORD_HASH` |
| **compu 10 (`labbd-10`)** | `10.68.17.10` | `world` | `world.yml` | Simulador (Rust / world-go) | **50051** (gRPC) | *Ninguno* |
| **compu 12** | `10.68.17.12` | `hub` | `hub.yml` | Hub (Go), Chat (Go) y Bots *(a demanda)* | **8081** (WS), **8082** (Chat), **50051** (gRPC) | `TOKEN_PUBLIC_KEY` *(+ `HV_ADMIN_*` para bots)* |
| **compu 11** | `10.68.17.80` | `proxy` | `proxy.yml` | Nginx (HTTPS, Frontend) y API (Node/TS) | **8080** (HTTPS / WSS única entrada) | `TOKEN_PRIVATE_KEY` |

> **Nota de red:** La **compu 11** tiene la IP `10.68.17.80` (no `.11`). Siempre debe verificarse con `ipconfig`.

### URLs de acceso al sistema
- **Juego:** `https://10.68.17.80:8080`
- **Panel del sistema (salud y observabilidad):** `https://10.68.17.80:8080/sistema.html`
- **Documentación Swagger API:** `https://10.68.17.80:8080/api/docs`

*(El certificado TLS de Nginx es autofirmado e incluye automáticamente la IP `10.68.17.80` en su SAN: el navegador avisará una sola vez; hacer clic en Avanzado › Continuar).*

---

## 3. Guía Paso a Paso Utilizada para el Despliegue

### Paso 0: Cortafuegos de Windows (Prerrequisito)
Las cuentas de estudiante no son administradoras. El encargado del laboratorio ejecutó este comando como Administrador en PowerShell en cada una de las 5 PCs:
```powershell
New-NetFirewallRule -DisplayName "hydrovalley (proyecto Sistemas Distribuidos)" -Direction Inbound -Protocol TCP -LocalPort 8080,8081,8082,50051,5433 -RemoteAddress 10.68.17.0/24 -Action Allow -Profile Domain,Private
```

### Paso 1: Configurar Direcciones (`deploy/nodes/lab-addresses.local`)
Este archivo es **idéntico en las 5 máquinas** (copiado a partir de `lab-addresses.example`):
```ini
LAB_DB_HOST=10.68.17.3
LAB_PERSISTENCE_HOST=10.68.17.9
LAB_WORLD_HOST=10.68.17.10
LAB_HUB_HOST=10.68.17.12
LAB_PROXY_HOST=10.68.17.80

HV_DB_PORT=5433
HV_PERSISTENCE_PORT=50051
HV_WORLD_PORT=50051
HV_HUB_PORT=8081
HV_PROXY_PORT=8080

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

# Nodo proxy
TOKEN_PRIVATE_KEY=<clave privada Ed25519 en base64; ver deploy/README.md>

# Nodo hub
TOKEN_PUBLIC_KEY=<clave pública Ed25519 en base64>

# Bots (en nodo hub)
HV_ADMIN_USERNAME=admin
HV_ADMIN_PASSWORD=<contraseña del administrador, en claro>
```

**Distribución de secretos por máquina (principio de menor privilegio):**
- **Compu 3 (`db`):** Únicamente variables `POSTGRES_*`.
- **Compu 9 (`persistence`):** Variables `POSTGRES_*`, `ADMIN_USERNAME` y `ADMIN_PASSWORD_HASH`.
- **Compu 10 (`world`):** No requiere archivo `secrets.local`.
- **Compu 12 (`hub`):** `TOKEN_PUBLIC_KEY`, `HV_ADMIN_USERNAME`, `HV_ADMIN_PASSWORD`.
- **Compu 11 (`proxy`):** `TOKEN_PRIVATE_KEY`.

### Paso 3: Encendido de Docker Desktop
En cada una de las 5 PCs, abrir **Docker Desktop** y aguardar hasta que indique *Engine running*.

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

### Paso 5: Comprobación de Conectividad
Desde cualquier PC:
```powershell
powershell -ExecutionPolicy Bypass -File scripts\lab\diagnose.ps1 -Addresses deploy\nodes\lab-addresses.local
```
Todos los nodos deben reflejar el estado **`abierto`**.

---

## 4. Incidencias Detectadas y Soluciones Aplicadas

| Incidencia | Causa Raíz | Solución Aplicada |
| :--- | :--- | :--- |
| `diagnose.ps1` reportaba el puerto 8080 del proxy como `BLOQUEADO` | El proxy aún no estaba levantado o no coincidía la IP del adaptador Ethernet | Se confirmó la IP real con `ipconfig` en la compu 11 (`10.68.17.80`), se corrigió el archivo y se levantó el nodo `proxy`. |
| Error `unauthorized` al descargar imágenes de `ghcr.io` | Paquetes en GitHub Packages marcados como privados (D1) | `deploy-node.ps1` solicita `docker login ghcr.io` con usuario y un Personal Access Token (PAT) con permiso `read:packages`. |
| Escritura bloqueada de `secrets.local` desde scripts locales | Protección de carpetas de usuario / permisos en Windows 10 | Se crearon/pegaron los valores de los secretos de forma manual en disco. |
| Puertos marcados como `BLOQUEADO` antes de levantar servicios | `diagnose.ps1` no distingue entre firewall cerrado y puerto sin servicio escuchando | Para probar puramente el cortafuegos antes de levantar un servicio, se usa el modo listener: `diagnose.ps1 -Listen <puerto>`. |

---

## 5. Pruebas Pendientes a Realizar en el Laboratorio

Una vez con el sistema desplegado, se deben ejecutar y registrar estas pruebas para completar los requisitos del informe:

### A. Verificación Integral de Servicios (`health.ps1`)
Desde cualquier máquina:
```powershell
powershell -ExecutionPolicy Bypass -File scripts\lab\health.ps1 -Addresses deploy\nodes\lab-addresses.local
```
Debe confirmar que los 5 servicios (`db`, `persistence`, `world`, `hub`, `api`) están en `up`, el tick avanzando y el Circuit Breaker en `closed`.

### B. Prueba de Carga con Bots (CP-11 / RNF-01 / RNF-02)
En la **compu 12 (`hub`)**, ejecutar:
```powershell
$compose = @('compose','-p','hv-hub','-f','deploy\nodes\hub.yml','--env-file','deploy\nodes\lab-addresses.local','--env-file','deploy\nodes\secrets.local','--profile','bots')
docker @compose pull bots
docker @compose run --rm bots load-test
```
*Criterio de aceptación:* 30 bots durante 60 segundos manteniendo $\ge 100\text{ gotas/s}$ y $p95 < 200\text{ ms}$.

### C. Escenarios de Tolerancia a Fallos
Con el panel `https://10.68.17.80:8080/sistema.html` abierto y un jugador activo:
1. **Caída de Persistencia (CP-07):** Detener `persistence` en compu 9 (`docker stop hv-persistence-persistence-1`) durante 10 s. Comprobar que en el panel pasa a `down`, pero los jugadores pueden seguir jugando fluidamente sin pausa del valle. Al reiniciar el contenedor, vuelve a `up`.
2. **Caída del Simulador (CP-05):** Detener `world` en compu 10 (`docker stop hv-world-world-1`) durante 10 s. Comprobar que el hub entra en pausa, abre el Circuit Breaker tras 3 fallos y acumula las gotas. Al reiniciar `world`, el simulador carga la última instantánea de PostgreSQL y drena la cola sin duplicar acciones.
3. **Caída del Hub (CP-06):** Reiniciar `hub` en compu 12. Verificar reconexión automática de los navegadores.

---

## 6. Procedimiento de Cierre y Limpieza

Al terminar la sesión en el laboratorio, en cada una de las 5 PCs:
```powershell
# Bajar contenedores (conservando datos de BD):
powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node <nodo> -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Down

# Si se usó autenticación con PAT:
docker logout ghcr.io

# Eliminar variables de entorno locales:
Remove-Item deploy\nodes\lab-addresses.local, deploy\nodes\secrets.local -ErrorAction SilentlyContinue
```