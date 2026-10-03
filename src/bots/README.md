# Bots

Jugadores simulados en Go. Un bot **se hace pasar por un jugador**: entra por el proxy (`/api/*` y `/play`), como cualquier navegador, y nunca llama por gRPC a los servicios internos. Por eso lo que aguantan los bots lo aguanta el sistema de verdad. Contexto: sección «Los bots» de `docs/documento-inicial.md`.

Estado: están el cliente común y los escenarios `neighbors` (vecinos) y `load` (prueba de carga, CP-11). `cheater` (CP-03) y `firefighter` (CP-04) llegan en tareas siguientes (`odd/tasks/bots.md`).

## Estructura

| Ruta | Qué contiene |
| --- | --- |
| `cmd/bots/` | Proceso y subcomandos; parada con SIGINT o SIGTERM |
| `internal/config/` | Variables de entorno |
| `internal/client/` | `API` (acceso del administrador y aprovisionamiento de cuentas) y `Session` (WebSocket `/play`: marcos, pendientes, latencia, reconexión) |
| `internal/scenario/neighbors/` | Decisiones puras (`Decide`, `PickTile`, `NextDelay`) y el bucle de los bots |
| `internal/scenario/load/` | Prueba de carga: partes puras (`Percentile`, `BuildReport`, `FormatSummary`) y `Run` |

## Variables de entorno

| Variable | Obligatoria | Valor por defecto | Uso |
| --- | --- | --- | --- |
| `HV_BASE_URL` | Sí | — | Dirección del proxy, p. ej. `http://nginx:8080` (plan B) o `http://10.68.17.x:8080` (plan A). `https` usa `wss` para `/play` |
| `HV_ADMIN_USERNAME`, `HV_ADMIN_PASSWORD` | Sí, para aprovisionar | — | Cuenta de administrador; solo sirve para crear las cuentas de bots. La contraseña nunca se registra |
| `BOTS_COUNT` | No | `3` (`30` en `load`) | Número de bots |
| `BOTS_DURATION` | No | `0` (`60s` en `load`) | Duración (`30s`, `5m`); `0` = hasta que se detenga. En `load` es el tiempo de envío y `0` cae a 60 s |
| `BOTS_RESET` | No | `true` | Solo `load`: al terminar reinicia el valle con `reset_valley`. Con `false` se conserva el valle para inspeccionarlo, pero el modo demo se apaga igual |
| `BOTS_REPORT_PATH` | No | — | Solo `load`: archivo donde escribir el informe JSON |
| `BOTS_SEED` | No | aleatoria (se registra al arrancar) | Semilla de las decisiones: misma semilla, mismas elecciones de casilla y mismos tiempos de espera. El orden de llegada de los resultados sigue dependiendo de la red |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn` o `error` |

## Uso

```
HV_BASE_URL=http://localhost:8080 HV_ADMIN_USERNAME=admin HV_ADMIN_PASSWORD=... \
BOTS_COUNT=3 BOTS_DURATION=30s go run ./cmd/bots neighbors
```

Al terminar registra un resumen: acciones enviadas, resultados por estado (`applied`, `duplicate`, `rejected:<motivo>`, `rate_limited`...), las que quedaron sin resultado y los turnos sin conexión.

Prueba de carga (CP-11), 30 bots durante 60 s y con informe:

```
HV_BASE_URL=http://localhost:8080 HV_ADMIN_USERNAME=admin HV_ADMIN_PASSWORD=... \
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

Cuenta como «aceptada» lo que el hub resolvió como `applied` o `duplicate`. Salida: el resumen en español por la salida estándar y, con `BOTS_REPORT_PATH`, el JSON con `bots`, `duration_s` (ventana real de envío), `sent`, `results`, `unresolved`, `drops_per_second` (aceptadas por los bots entre esa ventana), `latency_ms` (`p50`, `p95`, `max`), `latency_within_limit_ratio`, `humidity_contributed` y `accepted_drops` (diferencias), `invariant_ok`, `rnf01_ok` y `rnf02_ok`. Código de salida: 0 si pasan las tres comprobaciones, 1 si falla alguna o la prueba no pudo completarse.

Los bots de carga abren `/play` con `map=false`: solo necesitan el tamaño del valle y los contadores.

## Pruebas

Desde `src/bots/`:

```
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
go test -race ./...
```

No necesitan Docker ni el sistema: el cliente se prueba contra un servidor `httptest` que simula la API y `/play` (acceso, aprovisionamiento con 429, marcos, pendientes y latencia con reloj falso, 4401, reconexión y reenvío); las decisiones de los vecinos, como funciones puras con generador con semilla; la prueba de carga contra un hub simulado (modo demo, restauración también al interrumpir, resultados sin respuesta, invariante roto) y sus umbrales con casos límite.

## Imagen de Docker

El contexto es la raíz del repositorio:

```
docker build -f src/bots/Dockerfile -t hydrovalley-bots .
docker run --rm -e HV_BASE_URL=http://host:8080 -e HV_ADMIN_USERNAME=admin -e HV_ADMIN_PASSWORD=... hydrovalley-bots neighbors
```

Binario estático en `golang:1.26` sobre `gcr.io/distroless/static-debian13:nonroot`. Aún no está en `deploy/compose.yml` ni en el CI (tarea T5).
