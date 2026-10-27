# Hub en tiempo real

Servicio en Go que atiende a los jugadores por el WebSocket `/play`, filtra sus comandos (Capa 1), lleva la idempotencia del lado de Go (Capa 3), marca el tick cada 100 ms llamando a `WorldService.Step` y difunde el estado una vez por segundo. Verifica el token firmado al abrir `/play` (ver [Token y roles](#token-y-roles)); admite gotas, compras (instalaciones y mejoras) y los comandos de administración.

Frontera Go/Rust (CLAUDE.md): **el hub decide si un comando es admisible; el simulador decide qué produce.** El hub nunca calcula el mundo.

## Estructura

| Ruta | Qué contiene |
| --- | --- |
| `cmd/hub/` | Proceso: configuración, canal gRPC, bucle del tick, servidor HTTP, parada con SIGINT o SIGTERM y el subcomando `healthcheck` |
| `internal/auth/` | Verificación del token JWT EdDSA con la clave pública (`TOKEN_PUBLIC_KEY`) e identidad de la sesión (`player_id`, `username`, rol) |
| `internal/hub/` | Núcleo sin red: admisión, conjuntos `pending` y `confirmed`, cola, bucle del tick, reparto de resultados, recuperación por `boot_id`, cuenta de gotas aceptadas y difusión. Recibe el mundo como puerto (`WorldClient`) y el reloj como interfaz (`Clock`) |
| `internal/breaker/` | *Circuit Breaker* de `Step` (cerrado, abierto, semiabierto) |
| `internal/ratelimit/` | Límite de gotas por jugador con ventana deslizante |
| `internal/frames/` | Mensajes JSON de `play.proto`: decodificación (`Envelope` y después el mensaje), resultados, errores y `StateMessage` |
| `internal/metrics/` | Tasa por segundo en ventana móvil y muestra acotada de duraciones con percentiles (base de `/stats`) |
| `internal/transport/` | HTTP: WebSocket `/play`, `/healthz`, `/readyz` y `/stats` |
| `internal/worldclient/` | Adaptador gRPC de `WorldService` |
| `internal/config/` | Variables de entorno |

## Requisitos

- Go 1.26.
- El código generado en `gen/go` (módulo `github.com/iadev2005/hydrovalley/gen/go`, que `go.mod` usa con `replace ../../gen/go`). Desde la raíz del repositorio: `scripts/gen-proto.sh` (ver `proto/README.md`).

## Variables de entorno

| Variable | Obligatoria | Valor por defecto | Uso |
| --- | --- | --- | --- |
| `WORLD_ADDR` | Sí | — | `host:puerto` de `WorldService`, p. ej. `world:50051` (plan B) o `10.68.17.x:50051` (plan A) |
| `TOKEN_PUBLIC_KEY` | Sí | — | Clave pública Ed25519 (SPKI) con la que se verifican los tokens de `/play`: el PEM tal cual, con los saltos de línea escritos como `\n`, o **el base64 del PEM completo** (una sola línea), igual que la privada de la API. Si falta o no es una clave Ed25519, el arranque se detiene con un mensaje que nombra la variable, sin repetir el valor |
| `HUB_LISTEN` | No | `:8081` | Dirección y puerto del HTTP (`/play`, `/healthz`, `/readyz`, `/stats`) |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn` o `error` |
| `HUB_ORIGIN_PATTERNS` | No | — | Orígenes, separados por comas (`host[:puerto]`, con comodines), que pueden abrir `/play` además del mismo origen. Los clientes sin cabecera `Origin` (bots) siempre pueden |

Con el origen por defecto, nginx debe reenviar la cabecera `Host` con su puerto (`proxy_set_header Host $http_host;`): con `$host` se pierde el puerto 8080 y el navegador recibe un 403.

## Pruebas

Desde `src/hub/`, con `gen/go` generado:

```
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
go test -race ./...
```

No necesitan Docker ni el simulador. El núcleo se prueba con un `WorldClient` falso y un reloj manual, llamando a una vuelta del bucle (`Hub.Tick`) cada vez, así que el tiempo es determinista. El transporte se prueba de punta a punta con `httptest` y un cliente WebSocket real; el adaptador gRPC, contra un servidor gRPC en el mismo proceso.

## Ejecutar en local

```
WORLD_ADDR=127.0.0.1:50061 HUB_LISTEN=127.0.0.1:8081 TOKEN_PUBLIC_KEY="$(base64 < token-public.pem | tr -d '\n')" go run ./cmd/hub
```

La pareja de claves se genera como indica `src/api/README.md` (sección «Claves del token»).

El hub arranca aunque el simulador no esté (regla 5 de portabilidad): el canal gRPC se conecta con la primera llamada y se reconecta solo, con esperas de como mucho 1 s. Mientras tanto `Step` falla, el valle está en pausa y `/readyz` responde 503.

## Protocolo `/play`

`GET /play?token=<jwt>[&map=false]` abre el WebSocket. Los mensajes son los de `proto/hydrovalley/play/v1/play.proto`: JSON plano con un campo `type`. El parámetro `player_id` ya no existe: la identidad sale solo del token.

| Del navegador | Respuesta |
| --- | --- |
| `{"type": "drop", "action_id": "<uuid>", "x": 14, "y": 9}` | Si se admite, nada al instante; tras su `Step`, `{"type": "result", "action_id": "…", "status": "applied"}` (o `duplicate`, o `rejected` con `reason`), o `expired` si pasa más de 30 s en la cola |
| `{"type": "set_demo_mode", "action_id": "<uuid>", "enabled": true}` y `{"type": "reset_valley", "action_id": "<uuid>"}` | Con `role = player`: `{"type": "result", …, "status": "forbidden"}` al instante, sin encolar. Con `role = admin`: se admiten y viajan a `Step` como cualquier comando; su resultado final llega como el de una gota |
| `{"type": "place_installation", "action_id": "<uuid>", "installation": "sprinkler", "x": 5, "y": 6}` (`installation`: `sprinkler`, `drip`, `treatment_plant` o `trash_bin`) y `{"type": "buy_upgrade", "action_id": "<uuid>", "upgrade": "bigger_drop"}` (`upgrade`: `bigger_drop` o `bigger_tank`) | Como una gota: nada al instante si se admite y, tras su `Step`, `applied`, `duplicate`, `rejected` con `reason` (`insufficient_leaves`, `tile_occupied`, `out_of_bounds`, `upgrade_already_owned`, `invalid_command`) o `expired`. El hub no sabe de precios ni de hojas: lo decide el simulador |
| `{"type": "clean_trash", "action_id": "<uuid>", "x": 1, "y": 0}` (limpia un nivel de basura de la casilla; expansión de jugabilidad, nivel 2) | Como una gota: nada al instante si se admite y, tras su `Step`, `applied`, `duplicate`, `rejected` con `reason` (`out_of_bounds`, `invalid_command` si la casilla no tiene basura) o `expired`. Usa `action_id` y cola como cualquier acción y **comparte con la gota el límite de 15 por segundo** (una limpieza consume del mismo cupo y, pasado el cupo, recibe `rate_limited`), pero no cuenta como gota aceptada |
| El mismo `action_id` mientras sigue pendiente | Nada: el resultado final llegará |
| JSON inválido, sin `type`, con campos desconocidos, `action_id` que no es UUID, coordenadas negativas, una `installation` o un `upgrade` ausente o que no es exactamente uno de los nombres de arriba, o un mensaje binario | `{"type": "error", "code": "invalid_message"}`, con `action_id` si se pudo leer |
| Un `type` desconocido | `{"type": "error", "code": "unknown_type"}` |

Cada conexión recibe una vez por segundo un `{"type": "state", …}` con `tick`, `paused`, `demo_mode`, el tamaño y el mapa del valle (sin mapa con `map=false`) y la vista de su jugador (`player`) cuando ya está en el mundo. El mapa incluye, además de `stages` (1 a 7: tierra seca, pasto, arbusto, flores, árbol, agua y roca) y `humidity`, `buildings` (edificio por casilla) y `trash` (nivel de basura de 0 a 3 por casilla); cada instalación trae `remaining_durability` y `player` trae `next_trash_bin_price` (expansión de jugabilidad, nivel 2). El **primer** `state` de cada conexión que lleva esa vista añade `player.offline_summary` (resumen al volver, RF-25) si no está vacío: lo que hicieron los aspersores del jugador desde su última acción aplicada (`leaves_earned`, `water_reused`, `fires_extinguished` y `tiles_grown`, `u64` como texto). Los estados siguientes de esa conexión no lo repiten (el cliente lo guarda hasta que el jugador lo cierre), y si el primero ya llega vacío no se agrega después; una reconexión lo vuelve a recibir mientras el jugador no actúe.

## Token y roles

- **Formato** (el que firma la API, `src/api/src/tokens.ts`): JWT con cabecera `alg: EdDSA` y los campos `sub` y `player_id` (el mismo UUID), `username`, `role` (`player` o `admin`), `iss` (`hydrovalley-api`), `aud` (`hydrovalley-play`), `iat` y `exp`. El hub exige `iss` y `aud` (`auth.Issuer`, `auth.Audience`): un token sin ellos, de otro emisor o para otra audiencia se rechaza con 4401.
- **Verificación** (`internal/auth`): solo `EdDSA` con la clave pública; `none` y los HMAC se descartan antes de mirar la firma (con `github.com/golang-jwt/jwt/v5`, que está mantenida y cuya validación de algoritmos es cerrada, en vez de reescribir el análisis del JWT a mano). Además: `exp` obligatorio y en el futuro (con 30 s de margen de reloj), `player_id` un UUID igual a `sub`, `role` en `{player, admin}` y `username` no vacío. No llama a nadie: la persistencia caída no afecta a quien abre `/play` (diagrama 09).
- **Token inválido**: se completa el upgrade y se cierra con el código **4401** (rango privado del WebSocket) y el motivo `invalid or expired token`, como en el diagrama 09; un navegador no puede leer el estado HTTP de un upgrade rechazado, y sin el código no distinguiría «vuelve a entrar» de una caída de red. No se registra ninguna sesión ni se lee un solo mensaje. El motivo detallado va solo al registro, nunca el token.
- **Vencimiento**: el token se comprueba únicamente al abrir. Si vence con la conexión abierta, no se corta; para reconectarse hace falta uno vigente.
- **Rol**: se guarda en la sesión. Solo `admin` puede enviar `set_demo_mode` y `reset_valley` (RF-31); los demás reciben `forbidden` al instante (regla 2 de CLAUDE.md) y el comando no entra en `pending` ni en la cola. Los comandos de administración no cuentan como gotas aceptadas ni consumen el límite de 15 gotas por segundo.
- **Compras** (`place_installation`, `buy_upgrade`): las puede enviar cualquier jugador. Entran en `pending`, `confirmed` y la cola como cualquier comando, pero **no** consumen el límite de 15 gotas por segundo (ni lo frenan) y **no** cuentan como gotas aceptadas: solo las gotas lo hacen. Su `action_id` es único por jugador sea del tipo que sea. El hub las traduce a `PlaceInstallationCommand` y `BuyUpgradeCommand` con el `player_id` del token.
- **Aviso**: el token viaja en la consulta de la URL. nginx no lo escribe en su registro de acceso (usa `$uri`, la ruta sin consulta; ver `deploy/README.md`), pero sigue visible en el historial del navegador y en cualquier otro proxy intermedio. El token dura 12 h y no da acceso a la contraseña.

## Comportamiento

- **Conexiones**: una goroutine por conexión lee y filtra los mensajes del jugador en fila (Capa 1); otra escribe lo que el núcleo deja en su buzón. El núcleo nunca espera a un cliente lento: si el buzón (256 mensajes) se llena, cierra esa conexión y el cliente se reconecta y reenvía sus pendientes. Un jugador puede tener varias conexiones; los resultados llegan a todas.
- **Sesión lógica**: `pending`, `confirmed` y el límite de gotas son por jugador y sobreviven a las reconexiones mientras el hub siga en marcha. El registro de un jugador solo se borra cuando no tiene conexiones, ni acciones en `pending` o `confirmed`, ni gotas en su ventana del último segundo; así, desconectarse y volver a conectarse no reinicia el límite.
- **Admisión** (la misma para gotas, compras y comandos de administración; las compras no pasan por el límite de gotas), en este orden: la conexión sigue registrada; un comando de administración sin rol `admin` → `forbidden` (y termina ahí); `action_id` en `pending` → se ignora; en `confirmed` → `duplicate` al instante; más de 15 gotas en el último segundo → `rate_limited`; cola con 5.000 comandos → `paused`. Si pasa, entra en `pending` y en la cola. El límite se consulta sin registrar y la gota solo ocupa hueco en la ventana cuando se admite, así que ningún rechazo (tampoco `paused`) consume el cupo de 15/s. Los valores de `status` salen de una traducción explícita (`WireStatus`) y no del nombre del resultado en los registros.
- **Límite de gotas**: ventana deslizante con los instantes de las últimas 15 gotas admitidas. Garantiza que nunca hay más de 15 en cualquier intervalo de 1 s (CP-03); un cubo de fichas con la misma cifra dejaría pasar hasta 30 en el primer segundo. Los rechazos no ocupan hueco.
- **Tick**: cada 100 ms, un solo bucle responde `expired` a lo que lleva más de 30 s en la cola, saca como mucho 500 comandos en orden de admisión y llama a `Step` con un plazo de 200 ms, sin reintento y sin el candado tomado. Como el bucle es uno y `Step` es síncrono, nunca hay dos llamadas a la vez; si una tarda más de 100 ms, el siguiente tick espera. `Step` se llama aunque no haya comandos (RF-28).
- **Resultados**: justo después del `Step`, cada resultado final va a las conexiones de su jugador y quita la acción de `pending`; `applied` y `duplicate` la pasan a `confirmed` con su tick durante 600 ticks (hasta *t* + 600 incluido, igual que Rust). Una respuesta que no trae exactamente un resultado por comando, en orden y con un estado conocido, se trata como un `Step` fallido.
- **`DUPLICATE` tras un `Step` fallido**: si un `Step` falla después de que Rust lo aplicara (p. ej. vence el plazo de 200 ms), el reintento vuelve como `DUPLICATE`. Si el comando viajó en un `Step` fallido y el hub no lo tenía en `confirmed`, es su primera confirmación y se trata como `applied`: el jugador recibe `applied`, entra en `confirmed` con el tick de esa respuesta y se cuenta en las gotas aceptadas de ese tick. Un `DUPLICATE` de un comando que nunca viajó en un `Step` fallido, o que el hub ya tenía en `confirmed`, sigue siendo `duplicate` y no se cuenta.
- **Fallos**: con el primer `Step` fallido el valle queda en pausa y sus comandos vuelven al frente de la cola (siguen en `pending`). Tras 3 fallos seguidos se abre el *Circuit Breaker*: deja de llamar a `Step`, envía al instante un `state` con `paused: true` a todos y prueba una vez por segundo con el siguiente lote; el primer éxito lo cierra.
- **Reinicio de Rust**: el primer `boot_id` visto es la línea base. Cuando cambia, una sola vez y antes de procesar los resultados de esa respuesta, el hub quita de `confirmed` las entradas con tick mayor que `restored_tick` y resta de la cuenta de gotas aceptadas las de esos ticks (CP-05).
- **Gotas aceptadas**: se cuentan solo con los `applied` de `Step` (incluidos los `DUPLICATE` que se tratan como `applied`, ver arriba), por tick durante 600 ticks. `StateMessage` las lleva como `accepted_drops` y `humidity_contributed` (texto en JSON, también con `map=false`), una vez por segundo con el estado, para que un cliente compruebe el modo demo (RF-30, CP-11) por diferencias entre dos mensajes. Además, el hub las registra cuando la cuenta cambia (como mucho una vez por segundo).
- **Identificador de petición (RNF-07)**: en el juego el identificador es el `action_id`. Con `LOG_LEVEL=debug` el hub registra la admisión (`action admitted`, `action rejected` con su `outcome`, `action ignored`) y el resultado final (`action result` con su `status`, su motivo y el tick) de cada acción con su `action_id`; con `info` no escribe nada por acción, para que cien gotas por segundo no inunden el registro. Cada `Step` lleva el metadato gRPC `x-request-id: tick-<n>` (`n`: el tick siguiente al último conocido) y el hub lo registra a `debug` (`Step`) y en el aviso `Step failed`; el simulador escribe el mismo id en sus líneas del `Step`. Nunca se registran tokens ni la consulta de `/play`.
- **`/healthz`**: 200 mientras el proceso atiende (para Docker Compose). **`/readyz`**: 200 solo con el circuito cerrado y el último `Step` correcto; 503 en otro caso. Devuelve en JSON `ready`, `tick`, `paused`, `breaker` y `queue`.
- **`/stats`**: métricas en memoria para el panel del sistema (RNF-08), en JSON con `snake_case`. Es interno: **no lleva autenticación**, porque nginx no lo reenvía y el puerto 8081 no se publica; solo lo consulta la API por la red del despliegue (`HUB_STATS_ADDR`). No contiene nombres ni tokens. Medir no frena el tick: cada evento solo incrementa un contador con una sección crítica corta, y los percentiles se calculan al consultar. Los enteros de 64 bits viajan como texto (regla 7); los conteos pequeños, como números. Campos:

  | Campo | Significado |
  | --- | --- |
  | `tick` (texto) | Último tick devuelto por `Step` |
  | `paused` | El valle está en pausa (algún `Step` reciente falló) |
  | `breaker` | Estado del *Circuit Breaker*: `closed`, `open` o `half_open` (los mismos valores que `/readyz`) |
  | `queue` | Comandos en la cola del tick |
  | `sessions` | Conexiones WebSocket abiertas ahora |
  | `goroutines` | `runtime.NumGoroutine()` del proceso |
  | `actions_per_second.admitted` | Acciones admitidas por el hub por segundo, promedio de los últimos 10 s (un decimal). Los rechazos inmediatos y los reenvíos ignorados no cuentan |
  | `actions_per_second.applied` | Resultados finales `applied` por segundo en la misma ventana (incluye el `duplicate` que se trata como `applied` tras un `Step` fallido) |
  | `actions_per_second.window_seconds` | Tamaño de la ventana: 10. Mientras el hub lleva menos de 10 s en marcha, el promedio subestima la tasa real |
  | `latency_ms.p50`, `latency_ms.p95` | Milisegundos desde la admisión hasta el resultado final (`applied`, `duplicate`, `rejected` o `expired`), de las acciones resueltas en los últimos 10 s (percentil por rango más cercano) |
  | `latency_ms.samples` | Muestras usadas; se guardan como mucho las últimas 4096 de la ventana, así que la memoria está acotada |
  | `step_ms.last` | Duración en milisegundos de la última llamada a `Step` (medida con el reloj del hub, fallidas incluidas) |
  | `step_ms.p95` | Percentil 95 de las últimas 600 llamadas a `Step` (sin ventana de tiempo) |
  | `step_ms.failures_total` | `Step` fallidos desde que arrancó el hub |
  | `humidity_total` (texto) | Suma de la humedad de todas las casillas del último estado del valle (0 antes del primer `Step` correcto); se calcula al consultar |
  | `humidity_contributed` (texto) | Humedad aportada por las gotas, tal como la informa el simulador |
  | `accepted_drops` (texto) | Cuenta de gotas aceptadas del hub |
  | `active_fires` | Casillas que arden en el último estado |
  | `boot_id` | `boot_id` de la última respuesta de `Step` (cadena vacía antes del primero) |
  | `restored_tick` (texto) | `restored_tick` de la última respuesta de `Step`: el tick del que Rust se recuperó en el arranque que identifica `boot_id` (0 sin instantánea o antes del primer `Step`) |
  | `generated_at` | Hora de la foto, ISO 8601 en UTC |

- **`/metrics`**: métricas en formato texto estándar de Prometheus (OpenMetrics) para el panel profesional (nivel 2, B7). Comparte las mismas garantías que `/stats`: interno, sin autenticación, cálculo $O(1)$ sin frenar el bucle del tick. Expone contadores acumulados (`hydrovalley_hub_actions_admitted_total`, `applied_total`, `step_failures_total`, `accepted_drops_total`), tasas móviles (`actions_admitted_rate`, `actions_applied_rate`), latencias p50/p95, duración de `Step`, estado del *Circuit Breaker* (`hydrovalley_hub_breaker_code`: 0=closed, 1=open, 2=half_open y `breaker_state`), sesiones activas, goroutines, humedad y fuegos activos.
- **Parada**: con SIGINT o SIGTERM cierra las conexiones con `StatusGoingAway`, termina el bucle y apaga el HTTP con un plazo de 5 s.

## Imagen de Docker

El contexto es la raíz del repositorio, porque el código de Go se genera desde `proto/` durante la construcción:

```
docker build -f src/hub/Dockerfile -t hydrovalley-hub .
docker run --rm -e WORLD_ADDR=world:50051 -p 8081:8081 hydrovalley-hub
```

Genera `gen/go` con `bufbuild/buf:1.73.0`, compila un binario estático en `golang:1.26` y lo copia a `gcr.io/distroless/static-debian13:nonroot`, que se ejecuta con un usuario sin privilegios. La imagen no trae `curl`: su `HEALTHCHECK` ejecuta `hub healthcheck`, que consulta `/healthz` dentro del propio contenedor. `Dockerfile.dockerignore` limita el contexto a `proto/` y a los archivos de este servicio (requiere BuildKit, el constructor por defecto de Docker).
