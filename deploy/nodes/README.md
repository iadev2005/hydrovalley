# Plan A: despliegue repartido en el laboratorio

Guía paso a paso para el día del laboratorio. Cada máquina corre un nodo (diagrama `docs/diagrams/08-despliegue.puml`) con las **mismas imágenes** de `ghcr.io` que el plan B; solo cambian direcciones y puertos (regla 6 de portabilidad, `CLAUDE.md`). Nada se construye en el laboratorio.

| Nodo | Archivo | Servicios | Puerto que publica | Secretos que recibe |
| --- | --- | --- | --- | --- |
| `db` | `db.yml` | PostgreSQL | 5433 (al 5432 del contenedor) | `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` |
| `persistence` | `persistence.yml` | Persistencia (Python) | 50051 | los de PostgreSQL y `ADMIN_USERNAME`, `ADMIN_PASSWORD_HASH` |
| `world` | `world.yml` | Simulador (Rust; o `world-go` con `HV_WORLD_IMAGE`) | 50051 | ninguno |
| `hub` | `hub.yml` | Hub (Go), chat y, a demanda, bots | 8081, 8082, 50051 | `TOKEN_PUBLIC_KEY` (y, para los bots, `HV_ADMIN_*`) |
| `proxy` | `proxy.yml` | nginx (HTTPS) y API | 8080 | `TOKEN_PRIVATE_KEY` |
| `standby` | `standby.yml` | 6.ª máquina: proxy-standby, hub-standby y world-standby (Go) | 8080, 8081, 50051 | `TOKEN_PRIVATE_KEY`, `TOKEN_PUBLIC_KEY` |

El chat (8082 WS, 50051 gRPC) está desplegado junto al hub y corre también en el nodo de respaldo.

Los puertos se pueden cambiar con `HV_DB_PORT`, `HV_PERSISTENCE_PORT`, `HV_WORLD_PORT`, `HV_HUB_PORT`, `HV_PROXY_PORT`, `HV_STANDBY_WORLD_PORT`, `HV_STANDBY_HUB_PORT` y `HV_STANDBY_PROXY_PORT` en el archivo de direcciones. Esa misma variable es el puerto que publica el nodo y el que usan los demás para llamarlo.

## Antes de ir al laboratorio

1. **Cortafuegos (lo hace el encargado, una vez por máquina).** Esta cuenta no es administradora y no puede abrir puertos. Que el encargado ejecute como administrador en cada máquina que use el equipo:

   ```powershell
   New-NetFirewallRule -DisplayName "hydrovalley (proyecto Sistemas Distribuidos)" -Direction Inbound -Protocol TCP -LocalPort 8080,8081,8082,50051,5433 -RemoteAddress 10.68.17.0/24 -Action Allow -Profile Domain,Private
   ```

   Sin esa regla las máquinas no se ven entre sí (primer reconocimiento) y el plan A no puede funcionar; el plan B (una sola máquina, `deploy/compose.yml`) sigue disponible. Una directiva de grupo del dominio podría anular la regla local: si la prueba sigue fallando con la regla creada, preguntarlo.
2. **Imágenes.** El pipeline las publica al fusionar en `dev` (etiqueta `dev`) y al etiquetar `main` (`vX.Y`). Si los paquetes de `ghcr.io` son públicos no hace falta nada; si son privados, ver `deploy/README.md` («Imágenes en ghcr.io») y el paso de `docker login` más abajo.
3. **Archivos de variables**, en un equipo con `openssl`, `bash` y Docker (WSL, Git Bash o un Mac):

   ```bash
   cp deploy/nodes/lab-addresses.example deploy/nodes/lab-addresses.local
   cp deploy/nodes/secrets.example deploy/nodes/secrets.local
   ```

   - `lab-addresses.local`: sustituir cada `[DATO: IP de la máquina del nodo …]` por la IP real. LABBD-01 es `10.68.17.1` y LABBD-02 `10.68.17.2`; qué nodo va en cuál se decide ese día. Dos nodos pueden compartir máquina si sus puertos no chocan (`world` y `persistence` usan ambos el 50051).
   - `secrets.local`: ver «Secretos». Ambos archivos están en `.gitignore`.

## Secretos

Se generan una vez y no van en el repositorio (RNF-13). Detalle y motivos en `deploy/README.md` («Claves y variables»).

```bash
# Pareja de claves del token (base64 del PEM, una línea cada una)
openssl genpkey -algorithm ed25519 -out priv.pem && openssl pkey -in priv.pem -pubout -out pub.pem
echo "TOKEN_PRIVATE_KEY=$(base64 < priv.pem | tr -d '\n')"
echo "TOKEN_PUBLIC_KEY=$(base64 < pub.pem | tr -d '\n')"
rm priv.pem pub.pem

# Hash Argon2id del administrador (la contraseña entra por la entrada estándar)
read -rs -p "Contraseña del administrador: " pw; echo
printf '%s' "$pw" | docker run --rm -i ghcr.io/iadev2005/hydrovalley-api:dev node --input-type=module -e '
import { hash } from "@node-rs/argon2";
let pw = ""; for await (const c of process.stdin) pw += c;
console.log(await hash(pw, { algorithm: 2, memoryCost: 19456, timeCost: 2, parallelism: 1 }));'
```

El hash va **entre comillas simples** en `secrets.local` (lleva `$`). La contraseña de PostgreSQL no debe llevar `@ : / ? #`.

**Cada máquina recibe solo lo suyo.** El archivo de direcciones es igual en las cinco. El de secretos se recorta por nodo: la clave privada solo en el proxy, la pública solo en el hub, las credenciales de PostgreSQL solo en `db` y `persistence`, el hash solo en `persistence`. Los `.yml` solo pasan a cada contenedor las variables que declaran, pero lo mejor es que el resto ni esté en el disco.

## Qué copiar a cada máquina

La carpeta `deploy/nodes/` y la carpeta `scripts/lab/` (con la misma estructura de carpetas), más los dos archivos de variables. No hace falta el resto del repositorio, ni Git, ni compilar nada. Puede ser un `git clone` o un USB; si falta internet para las imágenes, `docker save` y `docker load` (documento inicial, «Qué hacer en la próxima visita»).

## El día del laboratorio

Windows 10, cuenta de dominio sin permisos de administrador, Docker Desktop con WSL 2. Todos los comandos son de PowerShell y se ejecutan desde la carpeta que contiene `deploy` y `scripts`. `-ExecutionPolicy Bypass` vale solo para ese proceso y no necesita administrador.

1. **Abrir Docker Desktop** en cada máquina y esperar a que el motor diga *Engine running* (en cada visita está apagado).
2. **Comprobar la red** desde cada máquina:

   ```powershell
   powershell -ExecutionPolicy Bypass -File scripts\lab\diagnose.ps1 -Addresses deploy\nodes\lab-addresses.local
   ```

   Muestra una tabla con cada nodo, IP y puerto. Antes de levantar nada, `BLOQUEADO` puede ser el cortafuegos o que aún no escucha nadie. Para probar solo el cortafuegos, en la máquina de destino: `diagnose.ps1 -Addresses … -Listen 8081` abre ese puerto cinco minutos, y se prueba desde otra máquina. Si el cortafuegos bloquea, el script imprime la regla del paso previo para el encargado.
3. **Levantar cada nodo**, en su máquina y **en cualquier orden** (RNF-09: cada servicio arranca sin los demás y se reconecta solo; un nodo puede quedar «sin salud» un rato hasta que aparezcan los otros):

   ```powershell
   powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node db          -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Tag dev
   powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node persistence -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Tag dev
   powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node world       -Addresses deploy\nodes\lab-addresses.local -Tag dev
   powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node hub         -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Tag dev
   powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node proxy       -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Tag dev
   ```

   Cada ejecución descarga solo las imágenes de ese nodo, hace `up -d` y espera a que los contenedores estén sanos (código de salida 2 si siguen sin salud: es normal si faltan nodos). Si la descarga da `denied` o `unauthorized`, los paquetes son privados: el script pide `docker login ghcr.io` (usuario de GitHub y un PAT clásico con solo `read:packages`) y repite. El equivalente manual de un nodo es `docker compose -p hv-<nodo> -f deploy/nodes/<nodo>.yml --env-file deploy/nodes/lab-addresses.local --env-file deploy/nodes/secrets.local up -d`.
4. **Comprobar el conjunto** desde cualquier máquina:

   ```powershell
   powershell -ExecutionPolicy Bypass -File scripts\lab\health.ps1 -Addresses deploy\nodes\lab-addresses.local
   ```

   Consulta `https://<IP del proxy>:8080/api/system` y muestra los cinco servicios; sale con 0 si todos están en `up` (el certificado autofirmado se acepta sin verificar, solo para leer el estado).

## Jugar desde cualquier PC del laboratorio

Abrir `https://<IP del proxy>:8080` en el navegador. El certificado es autofirmado (la IP del nodo va en su SAN, `TLS_EXTRA_SANS`), así que el navegador avisará: *Avanzado › Continuar*. Crear una cuenta o entrar con el administrador. El panel del sistema está en `https://<IP del proxy>:8080/sistema.html`. El puerto 8080 es el único que usan los navegadores; el 8081 es interno entre nginx, la API y el hub.

## Plan B del simulador: world-go en el nodo `world`

Si Rust falla en la máquina del nodo `world`, `world-go` ocupa su lugar con la misma configuración (diagrama 08, D-03): mismo servicio, mismo puerto (`HV_WORLD_PORT`, 50051) y mismas variables, así que el hub y la API no cambian. Es solo configuración, nunca código. En `lab-addresses.local`, con los valores escritos (el archivo de variables no interpola):

```bash
HV_WORLD_IMAGE=ghcr.io/iadev2005/hydrovalley-world-go:dev    # o :vX.Y; la etiqueta va escrita aquí (-Tag no la cambia)
```

y se vuelve a levantar el nodo (`deploy-node.ps1 -Node world …`, que descarga la imagen indicada). Sin `HV_WORLD_IMAGE` sigue la imagen de Rust. Antes hay que bajar el nodo `world` en marcha: solo un simulador contra el hub (escritor único, regla 1). `world-go` retoma el valle de la última instantánea (la de Rust o la suya) y la persistencia no se entera. Para el resto del plan B ver `deploy/README.md` («Sustituir Rust por world-go»).

## Bots y prueba de carga

Desde la máquina del nodo `hub` (los bots entran por el proxy como un navegador). Necesitan `HV_ADMIN_USERNAME` y `HV_ADMIN_PASSWORD` (la contraseña **en claro**, no el hash) en `secrets.local` y la imagen `hydrovalley-bots`:

```powershell
$compose = @('compose','-p','hv-hub','-f','deploy\nodes\hub.yml','--env-file','deploy\nodes\lab-addresses.local','--env-file','deploy\nodes\secrets.local','--profile','bots')
docker @compose pull bots
docker @compose run --rm bots load-test      # CP-11: 30 bots, 60 s
docker @compose run --rm bots cheater        # CP-03
docker @compose run --rm bots firefighter    # CP-04
```

Los escenarios y variables (`BOTS_COUNT`, `BOTS_DURATION`…) están en `deploy/README.md` («Bots») y `src/bots/README.md`. Por defecto no verifican el certificado autofirmado (`HV_TLS_INSECURE=true`, lo avisan al arrancar): aceptable en la red cerrada del laboratorio. Para verificarlo, hay que montar el certificado del proxy en el contenedor y fijar `HV_TLS_INSECURE=false` y `HV_CA_CERT`.

## Demostración de fallos

Con el juego en marcha y un jugador conectado, mostrar el panel del sistema (`/sistema.html`) y apagar un nodo: parar sus contenedores (`deploy-node.ps1 -Node <nodo> … -Down`) o apagar la máquina de verdad.

| Nodo que cae | Qué debe verse |
| --- | --- |
| `persistence` o `db` | La persistencia pasa a `down` en el panel; el jugador conectado **sigue jugando** (CP-07). El acceso y el registro nuevos responden 503 con un mensaje claro. Al volver, todo `up` |
| `world` | El hub pausa el valle, reencola los comandos y abre el *Circuit Breaker* tras 3 fallos; al volver, el simulador recupera la última instantánea y el hub drena la cola (CP-05, CP-06) |
| `hub` | Las conexiones de los jugadores se cierran; los clientes se reconectan solos al volver (CP-06) |
| `proxy` | Nadie entra hasta que vuelva: es la única puerta |

Una máquina apagada de verdad deja las conexiones sin respuesta, no rechazadas: todo depende de los plazos (`Step`, 200 ms; el resto, 2 s). Anotar las cifras medidas (tiempos de detección y recuperación) en [«Resultados medidos»](../../docs/documento-inicial.md#resultados-medidos) del documento inicial, que sustituye su `[DATO: resultados en las máquinas del laboratorio]`. Los mismos casos están automatizados contra el plan B en `deploy/README.md` («Casos de fallos»).

## Al terminar

Las máquinas son compartidas: dejarlas como estaban. `-Down` también necesita `-Secrets`: Compose exige las variables obligatorias del archivo incluso para bajarlo.

```powershell
powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node <nodo> -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Down          # conserva los datos
powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node <nodo> -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Down -Volumes # en db, borra la base de datos
docker logout ghcr.io          # si se inició sesión con un PAT
```

Borrar de la máquina `lab-addresses.local` y `secrets.local`, y desconectarse de Docker Desktop. No dejar ningún PAT guardado: `docker logout ghcr.io` es obligatorio en una máquina compartida.

## Simular el plan A en casa

`simulate.sh` levanta los cinco nodos como cinco proyectos de Compose (`hv-sim-<nodo>`), cada uno con su red, que se hablan **solo por puertos publicados del anfitrión** (`host.docker.internal`), como si fueran máquinas distintas. Usa claves y administrador efímeros, construye las imágenes de esta copia (`docker buildx bake`, `HV_TAG=local`) y comprueba: los cinco servicios en `up` en `/api/system`, el acceso del administrador, `GET /api/catalog`, una gota por WSS y el caso CP-07 (para el nodo `persistence`, un jugador conectado sigue jugando, y al volver todo está en `up`). Después lo baja todo.

```bash
deploy/nodes/simulate.sh
SIM_SKIP_BUILD=1 HV_TAG=dev deploy/nodes/simulate.sh   # con imágenes ya descargadas
```

Variables: `HV_TAG`, `SIM_SKIP_BUILD`, `SIM_SKIP_FAULT`, `SIM_KEEP` y los puertos `SIM_PORT_DB`, `SIM_PORT_PERSISTENCE`, `SIM_PORT_WORLD`, `SIM_PORT_HUB`, `SIM_PORT_PROXY` (por defecto 15433, 50071, 50061, 18081 y 18480, para no chocar con el 8080 del plan B). Requiere Docker con Compose v2 y buildx, `openssl`, `curl` y Node 22. Solo toca proyectos `hv-sim-*`. Limitaciones: no prueba el cortafuegos, la latencia real, Windows ni PowerShell; un nodo parado rechaza la conexión en vez de dejarla sin respuesta como una máquina apagada.

## Notas sobre los scripts de `scripts/lab/`

- Son compatibles con Windows PowerShell 5.1. Sus mensajes van sin tildes a propósito: PowerShell 5.1 lee como ANSI un archivo UTF-8 sin BOM y las desfiguraría. Los saltos de línea LF (`.gitattributes`) funcionan en PowerShell 5.1; no hace falta CRLF.
- Están probados solo con el analizador de PowerShell 7 y una ejecución parcial en Linux; **no se han ejecutado en Windows 10 ni en el laboratorio**. Si `diagnose.ps1` o `deploy-node.ps1` fallan, anotar el mensaje exacto.
