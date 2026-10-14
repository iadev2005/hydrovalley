# Bots

Jugadores simulados en Go. Un bot **se hace pasar por un jugador**: entra por el proxy (`/api/*` y `/play`), como cualquier navegador, y nunca llama por gRPC a los servicios internos. Por eso lo que aguantan los bots lo aguanta el sistema de verdad. Contexto: sección «Los bots» de `docs/documento-inicial.md`.

Estado: están el cliente común y los cuatro escenarios: `neighbors` (vecinos), `load` (prueba de carga, CP-11), `cheater` (tramposo, CP-03) y `firefighter` (bomberos, CP-04). Están en `deploy/compose.yml` (perfil `bots`, ver `deploy/README.md`), con su job de CI y un caso de extremo a extremo.

## Estructura

| Ruta | Qué contiene |
| --- | --- |
| `cmd/bots/` | Proceso y subcomandos; parada con SIGINT o SIGTERM |
| `internal/config/` | Variables de entorno |
| `internal/client/` | `API` (acceso del administrador y aprovisionamiento de cuentas) y `Session` (WebSocket `/play`: marcos, pendientes, latencia, reconexión) |
| `internal/scenario/neighbors/` | Decisiones puras (`Decide`, `PickTile`, `NextDelay`) y el bucle de los bots |
| `internal/scenario/load/` | Prueba de carga: partes puras (`Percentile`, `BuildReport`, `FormatSummary`) y `Run` |
| `internal/scenario/cheater/` | Tramposo: partes puras (`BuildFlood`, `BuildDuplicates`, `BuildInvalid`, `FormatSummary`) y `Run` |
| `internal/scenario/firefighter/` | Bomberos: partes puras (`NextTarget`, `Classify`, `BonusBots`, `BuildReport`) y `Run` |

## Variables de entorno

| Variable | Obligatoria | Valor por defecto | Uso |
| --- | --- | --- | --- |
| `HV_BASE_URL` | Sí | — | Dirección del proxy, p. ej. `https://nginx:8080` (plan B) o `https://10.68.17.x:8080` (plan A). `https` usa `wss` para `/play` |
| `HV_CA_CERT` | No | — | Ruta de un certificado PEM en que confiar (además de los del sistema), p. ej. el autofirmado del laboratorio. Preferible a `HV_TLS_INSECURE`. Excluyente con ella |
| `HV_TLS_INSECURE` | No | `false` | `true` omite la verificación del certificado del proxy, para el autofirmado del laboratorio. No usar fuera de pruebas. Con `true`, cada escenario registra un WARN al arrancar |
| `HV_ADMIN_USERNAME`, `HV_ADMIN_PASSWORD` | Sí, para aprovisionar | — | Cuenta de administrador; solo sirve para crear las cuentas de bots. La contraseña nunca se registra |
| `BOTS_COUNT` | No | `3` (`30` en `load`) | Número de bots. `cheater` usa siempre uno |
| `BOTS_DURATION` | No | `0` (`60s` en `load`, `10s` en `cheater`, `120s` en `firefighter`) | Duración (`30s`, `5m`); en `neighbors`, `0` = hasta que se detenga. En `load` es el tiempo de envío; en `cheater`, el de la inundación. En esos tres, `0` cae al valor por defecto |
| `BOTS_RESET` | No | `true` | Solo `load`: al terminar reinicia el valle con `reset_valley`. Con `false` se conserva el valle para inspeccionarlo, pero el modo demo se apaga igual |
| `BOTS_REPORT_PATH` | No | — | Solo `load`, `cheater` y `firefighter`: archivo donde escribir el informe JSON |
| `BOTS_LOAD_RANKING` | No | `false` | Solo `load`: durante el envío consulta `GET /api/ranking` en bucle (con el token del administrador) para medir RNF-03 |
| `BOTS_LOAD_RANKING_INTERVAL` | No | `2s` | Solo `load` con `BOTS_LOAD_RANKING`: espera entre consultas de ranking (`1s`, `500ms`). Debe ser positiva. 2 s deja unas 30 lecturas por minuto, por debajo del límite de la API (60 por minuto y por IP, `src/api/src/limits.ts`): con menos de 1 s, las consultas recibirían 429 |
| `BOTS_LOAD_BASELINE_REPORT` | No | — | Solo `load` con `BOTS_LOAD_RANKING`: ruta del informe JSON de una ejecución **sin** ranking, cuyo p95 es la línea base de RNF-03. Sin ella, RNF-03 se informa como «no se pudo evaluar» y la ejecución no cumple |
| `BOTS_FIREFIGHTER_SEED_PLANTS` | No | `false` | Solo `firefighter`: al empezar, cada bot riega 3 casillas propias con 4 gotas cada una para que haya plantas que puedan arder |
| `BOTS_FIREFIGHTER_BONUS` | No | `5` | Solo `firefighter`: hojas de la extinción que se esperan ver (`catalog.json`: `fire.extinguish_bonus_leaves`); solo sirve para reconocer la subida en las hojas del bot |
| `BOTS_SEED` | No | aleatoria (se registra al arrancar) | Semilla de las decisiones: misma semilla, mismas elecciones de casilla y mismos tiempos de espera. El orden de llegada de los resultados sigue dependiendo de la red |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn` o `error` |

## Uso

```
HV_BASE_URL=https://localhost:8080 HV_TLS_INSECURE=true HV_ADMIN_USERNAME=admin HV_ADMIN_PASSWORD=... \
BOTS_COUNT=3 BOTS_DURATION=30s go run ./cmd/bots neighbors
```

Al terminar registra un resumen: acciones enviadas, resultados por estado (`applied`, `duplicate`, `rejected:<motivo>`, `rate_limited`...), las que quedaron sin resultado y los turnos sin conexión.

Prueba de carga (CP-11), 30 bots durante 60 s y con informe:

```
HV_BASE_URL=https://localhost:8080 HV_TLS_INSECURE=true HV_ADMIN_USERNAME=admin HV_ADMIN_PASSWORD=... \
BOTS_REPORT_PATH=load-report.json go run ./cmd/bots load
```

## Cómo funciona

1. **Acceso y cuentas.** `POST /api/auth/login` con el administrador y `POST /api/admin/bot-accounts` (lotes de hasta 100, nombres `bot-<8 hex>`). Ante un `429` espera lo que diga `Retry-After` y repite (hasta 6 veces); ante otro error se detiene, porque la API ya indica el fallo.
2. **Sesión `/play`.** `ws(s)://…/play?token=…&map=true|false`. Los marcos son JSON plano de `proto/hydrovalley/play/v1/play.proto`; se escriben a mano (`internal/client/frames.go`) en vez de generarlos, porque son pocos y planos y así el módulo no necesita buf, `gen/` ni una etapa de generación en el Dockerfile. Los enteros de 64 bits llegan como texto y se leen con `U64`.
3. **Pendientes y latencia.** Cada acción lleva un `action_id` (UUID) y queda pendiente hasta su resultado final. La latencia va desde el primer envío hasta ese resultado. `duplicate` cuenta como `applied`. Un `error` del hub resuelve la acción con estado `error`.
4. **Cierre 4401.** Token inválido o vencido: la sesión termina con `ErrUnauthorized` y el bot se detiene sin reconectar.
5. **Reconexión.** Cualquier otra caída se reintenta con espera creciente (de 250 ms a 10 s, con azar). Al volver se reenvían solo las pendientes de menos de 30 s y que no sean de administración; las más viejas se descartan con un resultado local `expired`. Los comandos de administración (`set_demo_mode`, `reset_valley`) nunca se reenvían. Enviar sin conexión devuelve `ErrNotConnected` y no deja nada pendiente.

## Escenario `neighbors`

Cada bot abre `/play` con `map=true` y, cada pocos segundos (4 s ± 50 %, derivados de la semilla), elige una casilla con probabilidad proporcional a un peso: la tierra seca (4) y las casillas con humedad que aún no son árbol (6) pesan más que las demás (2); un árbol pesa 1. Con hojas suficientes para el siguiente aspersor, a veces (25 %) compra y coloca uno en una casilla libre. Si sabe que su depósito está vacío, espera. Los pesos y las probabilidades son decisiones de estos bots, no del documento.

## Escenario `load` (CP-11)

Mide lo que promete el documento inicial: al menos 100 gotas aceptadas por segundo (RNF-01), el 95 % de las acciones con resultado en menos de 200 ms (RNF-02) y la humedad aportada igual a las gotas aceptadas (RF-30). Los umbrales son constantes de `internal/scenario/load/report.go`; el informe dice lo medido, nunca cifras de ejemplo.

1. Entra como administrador y aprovisiona `BOTS_COUNT` bots.
2. Con la sesión del administrador envía `set_demo_mode` y espera su resultado y un estado ya en modo demo. Ese estado da el tamaño del valle y los contadores iniciales.
3. Cada bot suelta una gota sobre una casilla al azar cada 250 ms ± 50 ms (unas 4 por segundo) durante `BOTS_DURATION`. La latencia va desde el primer envío hasta el resultado final.
4. Al terminar el envío espera hasta 5 s los resultados pendientes; los que sigan sin resultado cuentan como «sin resolver» y como lentos para RNF-02. Después espera 1,5 s para leer un estado posterior al último tick.
5. Compara por **diferencias** (final menos inicial) `humidity_contributed` y `accepted_drops`, los dos campos que el hub difunde en `state` desde esta tarea (texto en JSON, también con `map=false`). Las gotas que vieron aplicadas los propios bots (`bots_applied`) se informan, pero solo igualan a `accepted_drops` si nadie más juega, así que no hacen fallar la prueba. Un hub que no informe los contadores no puede pasar: el cero se omite en el JSON y se lee como 0, así que unas gotas aplicadas con 0 aceptadas se marcan como no comprobables.
6. Apaga el modo demo y, con `BOTS_RESET=true`, ejecuta `reset_valley`. Esto ocurre también si la prueba falla o se interrumpe con Ctrl-C; si algún paso falla queda en `notes`.

Cuenta como «aceptada» lo que el hub resolvió como `applied` o `duplicate`. Salida: el resumen en español por la salida estándar y, con `BOTS_REPORT_PATH`, el JSON con `bots`, `duration_s` (ventana real de envío), `sent`, `results`, `unresolved`, `drops_per_second` (aceptadas por los bots entre esa ventana), `latency_ms` (`p50`, `p95`, `max`), `latency_within_limit_ratio`, `humidity_contributed` y `accepted_drops` (diferencias), `invariant_ok`, `rnf01_ok` y `rnf02_ok` (y `ranking` con `BOTS_LOAD_RANKING`, ver abajo). Código de salida: 0 si pasan las tres comprobaciones (y RNF-03 con ranking), 1 si falla alguna o la prueba no pudo completarse.

### RNF-03: el ranking no debe subir la latencia

RNF-03 pide que el recálculo del ranking no haga pasar a las acciones de los 200 ms de RNF-02. Se mide comparando dos ejecuciones de la misma carga:

1. **Línea base:** `load` sin `BOTS_LOAD_RANKING`, con `BOTS_REPORT_PATH` (su informe es la línea base).
2. **Con ranking:** `load` con `BOTS_LOAD_RANKING=true` y `BOTS_LOAD_BASELINE_REPORT` apuntando al informe anterior. Mientras los bots sueltan gotas, un bucle pide `GET /api/ranking?limit=100` cada `BOTS_LOAD_RANKING_INTERVAL`.

RNF-03 cumple si el **p95 de las acciones** con el ranking en bucle es **<= 200 ms** y **<= p95 de la línea base × (1 + `RNF03Margin`)**. `RNF03Margin` es 0,20 (`internal/scenario/load/report.go`). **Es un umbral elegido por el equipo, no del documento inicial** (que no fija cuánto empeorar es aceptable): queda como decisión por confirmar. Para que RNF-03 sea evaluable hacen falta además al menos `RNF03MinRankingQueries` (10) consultas de ranking exitosas y ninguna con error; si no, o si falta la línea base, se marca `rnf03_evaluated: false` y `rnf03_ok: false` (con una nota que dice el motivo) y la ejecución no cumple: no se da por cumplido lo que no se midió. Una consulta de ranking en vuelo cuando termina la ventana de envío la cancela la propia prueba y no cuenta (ni como enviada ni como error); los errores reales (por ejemplo, un 429) sí. El JSON añade `ranking` con `queries_sent`, `queries_ok`, `errors`, `latency_ms`, `baseline_p95_ms`, `rnf03_evaluated` y `rnf03_ok`. Todas las claves del informe van en snake_case; el resumen del workflow (`jq`) lee `ranking.rnf03_evaluated` y `ranking.rnf03_ok`.

### Cómo correr la prueba de carga

- **En local** (Docker y `openssl`): `tests/load/run.sh`. Levanta el stack por HTTPS con un certificado efímero (como `tests/e2e/run.sh`, comparten `tests/lib/stack.sh`), corre la línea base y después la variante con ranking (con la línea base que acaba de medir, escrita en un directorio propio de la ejecución: nunca un informe viejo de `reports/`; antes de correr borra los `load-baseline.json` y `load-ranking.json` anteriores), y deja `reports/load-baseline.json` y `reports/load-ranking.json` (`reports/` está en `.gitignore`: son artefactos, no resultados versionados). Variables: `LOAD_BOTS` (30), `LOAD_DURATION` (60s), `LOAD_VARIANT` (`both`, `baseline` o `ranking`; esta última necesita `LOAD_BASELINE_REPORT`, la ruta de un informe de línea base previo, y sin ella RNF-03 no se evalúa), `LOAD_RANKING_INTERVAL`, `REPORTS_DIR`, `HV_HTTP_PORT` (si el 8080 está ocupado), `LOAD_SKIP_BUILD=1`, `LOAD_KEEP=1`. Sale con 1 si alguna variante no cumple.
- **En GitHub:** pestaña *Actions*, workflow `load-test`, *Run workflow* (solo a demanda, con `workflow_dispatch`; no corre en pull requests ni en push). Pide duración, número de bots y variante, y publica en el resumen del job las cifras junto con la máquina del *runner*, y los informes JSON como artefacto `load-test-reports`. Las cifras de un *runner* compartido no son las del laboratorio: al citarlas hay que decir dónde se midieron.

Los bots de carga abren `/play` con `map=false`: solo necesitan el tamaño del valle y los contadores.

## Escenario `cheater` (CP-03, RF-05, RF-06, RF-09)

Un solo bot, una sola conexión y tres fases. Sale con 0 si pasan las tres y con 1 si falla alguna.

1. **Acción repetida (RF-09).** Suelta una gota y reenvía su mismo `action_id` una vez mientras sigue pendiente (el hub la ignora, regla 2 de `CLAUDE.md`) y dos veces después de su resultado final. Pasa si hubo exactamente un `applied` y las dos reenviadas tras el resultado recibieron `duplicate`. Los resultados sin acción pendiente llegan por `Session.Unmatched()`.
2. **Mensajes inválidos (RF-06).** Envía un JSON mal formado y un marco de `type` desconocido. Pasa si el hub responde `invalid_message` y `unknown_type` y una gota posterior recibe su resultado por la misma conexión.
3. **Inundación (CP-03, RF-05).** 50 gotas por segundo durante `BOTS_DURATION` (10 s), con horario fijo y casillas al azar. Pasa si en ningún segundo de la ventana el hub admitió más de 15 + 1 gotas, hubo alguna `rate_limited` y todas las acciones tuvieron resultado.

Decisiones:

- **Orden.** El documento describe primero la inundación; aquí va al final porque, fuera del modo demo, cada gota cuesta 1 L y el depósito (20 L) se vacía: sin agua, la fase de acción repetida no vería su `applied`. No se activa el modo demo.
- **Qué cuenta como admitida.** `applied`, `duplicate`, `rejected` (también `insufficient_water`: el límite la dejó pasar, y CP-03 prueba el límite, no la economía) y `expired`. Informa `accepted_per_second` (admitidas entre la ventana) y `rate_limited`.
- **Tolerancia de +1.** El hub usa una ventana deslizante (`src/hub/internal/ratelimit`): nunca admite más de 15 en cualquier intervalo de 1 s con su propio reloj. El bot mide con su reloj de envío y en cubos fijos de 1 s, así que la red puede mover una gota al cubo vecino.
- **Entre fases** espera 1,1 s para que la ventana del límite quede vacía.

## Escenario `firefighter` (CP-04, RF-29, RF-30)

`BOTS_COUNT` bomberos (3 por defecto) vigilan `state.fires` durante `BOTS_DURATION` (120 s). Cuando aparece un incendio, todos sueltan **a la vez** una gota sobre la misma casilla (una ronda; se coordinan en el proceso) y repiten con cada estado nuevo mientras el fuego siga en el estado. Si hay varios, atienden uno por vez (el de menor fila y columna). No usan cifras del catálogo para decidir: solo reaccionan al estado.

- **Cómo terminó.** Con el mapa (`map=true`): si la casilla vuelve a tierra seca (etapa 1) el fuego se consumió (`burn_out_ticks`); si conserva su planta, se apagó.
- **Bono (indicativo).** Cada bot solo ve sus hojas. Se cuenta cuántos subieron al menos el bono entre que apareció el incendio y que todos recibieron un estado posterior a su desaparición. Debe ser uno. Es una heurística, porque las hojas también suben por crecimiento: cero bots solo da una nota, y más de uno hace fallar. En `tests/e2e/cp04-firefighter.test.mjs` el valle es nuevo y las gotas caen en la casilla en llamas, así que las hojas solo suben por el bono y el caso exige **exactamente un** bot con la subida.
- **Humedad.** Informa las diferencias de `humidity_contributed` y `accepted_drops`; fuera del modo demo son solo informativas (la igualdad exacta vale en demo).
- **Código de salida.** 0 si no hubo incendios (lo dice: «no hubo incendios en la ventana») o si todos los vistos se apagaron. 1 si un incendio atacado se consumió o seguía ardiendo al terminar, o si el bono parece repetido. Un incendio que aparece al final sin tiempo para atacarlo no hace fallar.
- **Incendios raros.** Solo arden casillas con planta y humedad 0 (50 ppm por tick, fuera del modo demo), así que en una ventana corta puede no haber ninguno. `BOTS_FIREFIGHTER_SEED_PLANTS=true` riega casillas al empezar para que existan plantas, sin garantizar un incendio.

## Pruebas

Desde `src/bots/`:

```
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
go test -race ./...
```

No necesitan Docker ni el sistema: el cliente se prueba contra un servidor `httptest` que simula la API y `/play` (acceso, aprovisionamiento con 429, marcos, pendientes y latencia con reloj falso, 4401, reconexión y reenvío); las decisiones de los vecinos, como funciones puras con generador con semilla; la prueba de carga contra un hub simulado (modo demo, restauración también al interrumpir, resultados sin respuesta, invariante roto) y sus umbrales con casos límite; el tramposo, contra un hub simulado con ventana deslizante, `pending`/`confirmed` y errores (y variantes sin límite, sin control de duplicados y mudo); los bomberos, contra un mundo simulado con un incendio (apagado entre varios bots, con uno solo, consumido, que no se apaga y con bono repetido).

## Imagen de Docker

El contexto es la raíz del repositorio:

```
docker build -f src/bots/Dockerfile -t hydrovalley-bots .
docker run --rm -e HV_BASE_URL=https://host:8080 -e HV_TLS_INSECURE=true -e HV_ADMIN_USERNAME=admin -e HV_ADMIN_PASSWORD=... hydrovalley-bots neighbors
```

Binario estático en `golang:1.26` sobre `gcr.io/distroless/static-debian13:nonroot`. En Compose es el servicio `bots`, bajo el perfil del mismo nombre: `docker compose -f deploy/compose.yml --profile bots run --rm bots load-test` (`load-test` es alias de `load`).
