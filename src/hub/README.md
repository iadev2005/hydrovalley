# Hub en tiempo real

Servicio en Go que atiende a los jugadores por el WebSocket `/play`, filtra sus comandos (Capa 1), lleva la idempotencia del lado de Go (Capa 3), marca el tick cada 100 ms llamando a `WorldService.Step` y difunde el estado una vez por segundo. Es el esqueleto: por ahora solo admite gotas y todavía no verifica el token (ver [Pendiente](#pendiente)).

Frontera Go/Rust (CLAUDE.md): **el hub decide si un comando es admisible; el simulador decide qué produce.** El hub nunca calcula el mundo.

## Estructura

| Ruta | Qué contiene |
| --- | --- |
| `cmd/hub/` | Proceso: configuración, canal gRPC, bucle del tick, servidor HTTP, parada con SIGINT o SIGTERM y el subcomando `healthcheck` |
| `internal/hub/` | Núcleo sin red: admisión, conjuntos `pending` y `confirmed`, cola, bucle del tick, reparto de resultados, recuperación por `boot_id`, cuenta de gotas aceptadas y difusión. Recibe el mundo como puerto (`WorldClient`) y el reloj como interfaz (`Clock`) |
| `internal/breaker/` | *Circuit Breaker* de `Step` (cerrado, abierto, semiabierto) |
| `internal/ratelimit/` | Límite de gotas por jugador con ventana deslizante |
| `internal/frames/` | Mensajes JSON de `play.proto`: decodificación (`Envelope` y después el mensaje), resultados, errores y `StateMessage` |
| `internal/transport/` | HTTP: WebSocket `/play`, `/healthz` y `/readyz` |
| `internal/worldclient/` | Adaptador gRPC de `WorldService` |
| `internal/config/` | Variables de entorno |

## Requisitos

- Go 1.26.
- El código generado en `gen/go` (módulo `github.com/iadev2005/hydrovalley/gen/go`, que `go.mod` usa con `replace ../../gen/go`). Desde la raíz del repositorio: `scripts/gen-proto.sh` (ver `proto/README.md`).

## Variables de entorno

| Variable | Obligatoria | Valor por defecto | Uso |
| --- | --- | --- | --- |
| `WORLD_ADDR` | Sí | — | `host:puerto` de `WorldService`, p. ej. `world:50051` (plan B) o `10.68.17.x:50051` (plan A) |
| `HUB_LISTEN` | No | `:8081` | Dirección y puerto del HTTP (`/play`, `/healthz`, `/readyz`) |
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
WORLD_ADDR=127.0.0.1:50061 HUB_LISTEN=127.0.0.1:8081 go run ./cmd/hub
```

El hub arranca aunque el simulador no esté (regla 5 de portabilidad): el canal gRPC se conecta con la primera llamada y se reconecta solo, con esperas de como mucho 1 s. Mientras tanto `Step` falla, el valle está en pausa y `/readyz` responde 503.

## Protocolo `/play`

`GET /play?player_id=<uuid>[&map=false]` abre el WebSocket. Los mensajes son los de `proto/hydrovalley/play/v1/play.proto`: JSON plano con un campo `type`.

| Del navegador | Respuesta |
| --- | --- |
| `{"type": "drop", "action_id": "<uuid>", "x": 14, "y": 9}` | Si se admite, nada al instante; tras su `Step`, `{"type": "result", "action_id": "…", "status": "applied"}` (o `duplicate`, o `rejected` con `reason`), o `expired` si pasa más de 30 s en la cola |
| El mismo `action_id` mientras sigue pendiente | Nada: el resultado final llegará |
| JSON inválido, sin `type`, con campos desconocidos, `action_id` que no es UUID, coordenadas negativas o un mensaje binario | `{"type": "error", "code": "invalid_message"}`, con `action_id` si se pudo leer |
| Un `type` desconocido o todavía no soportado | `{"type": "error", "code": "unknown_type"}` |

Cada conexión recibe una vez por segundo un `{"type": "state", …}` con `tick`, `paused`, `demo_mode`, el tamaño y el mapa del valle (sin mapa con `map=false`) y la vista de su jugador (`player`) cuando ya está en el mundo.

## Comportamiento

- **Conexiones**: una goroutine por conexión lee y filtra los mensajes del jugador en fila (Capa 1); otra escribe lo que el núcleo deja en su buzón. El núcleo nunca espera a un cliente lento: si el buzón (256 mensajes) se llena, cierra esa conexión y el cliente se reconecta y reenvía sus pendientes. Un jugador puede tener varias conexiones; los resultados llegan a todas.
- **Sesión lógica**: `pending`, `confirmed` y el límite de gotas son por jugador y sobreviven a las reconexiones mientras el hub siga en marcha. El registro de un jugador solo se borra cuando no tiene conexiones, ni acciones en `pending` o `confirmed`, ni gotas en su ventana del último segundo; así, desconectarse y volver a conectarse no reinicia el límite.
- **Admisión**, en este orden: la conexión sigue registrada; `action_id` en `pending` → se ignora; en `confirmed` → `duplicate` al instante; más de 15 gotas en el último segundo → `rate_limited`; cola con 5.000 comandos → `paused`. Si pasa, entra en `pending` y en la cola. El límite se consulta sin registrar y la gota solo ocupa hueco en la ventana cuando se admite, así que ningún rechazo (tampoco `paused`) consume el cupo de 15/s. Los valores de `status` salen de una traducción explícita (`WireStatus`) y no del nombre del resultado en los registros.
- **Límite de gotas**: ventana deslizante con los instantes de las últimas 15 gotas admitidas. Garantiza que nunca hay más de 15 en cualquier intervalo de 1 s (CP-03); un cubo de fichas con la misma cifra dejaría pasar hasta 30 en el primer segundo. Los rechazos no ocupan hueco.
- **Tick**: cada 100 ms, un solo bucle responde `expired` a lo que lleva más de 30 s en la cola, saca como mucho 500 comandos en orden de admisión y llama a `Step` con un plazo de 200 ms, sin reintento y sin el candado tomado. Como el bucle es uno y `Step` es síncrono, nunca hay dos llamadas a la vez; si una tarda más de 100 ms, el siguiente tick espera. `Step` se llama aunque no haya comandos (RF-28).
- **Resultados**: justo después del `Step`, cada resultado final va a las conexiones de su jugador y quita la acción de `pending`; `applied` y `duplicate` la pasan a `confirmed` con su tick durante 600 ticks (hasta *t* + 600 incluido, igual que Rust). Una respuesta que no trae exactamente un resultado por comando, en orden y con un estado conocido, se trata como un `Step` fallido.
- **`DUPLICATE` tras un `Step` fallido**: si un `Step` falla después de que Rust lo aplicara (p. ej. vence el plazo de 200 ms), el reintento vuelve como `DUPLICATE`. Si el comando viajó en un `Step` fallido y el hub no lo tenía en `confirmed`, es su primera confirmación y se trata como `applied`: el jugador recibe `applied`, entra en `confirmed` con el tick de esa respuesta y se cuenta en las gotas aceptadas de ese tick. Un `DUPLICATE` de un comando que nunca viajó en un `Step` fallido, o que el hub ya tenía en `confirmed`, sigue siendo `duplicate` y no se cuenta.
- **Fallos**: con el primer `Step` fallido el valle queda en pausa y sus comandos vuelven al frente de la cola (siguen en `pending`). Tras 3 fallos seguidos se abre el *Circuit Breaker*: deja de llamar a `Step`, envía al instante un `state` con `paused: true` a todos y prueba una vez por segundo con el siguiente lote; el primer éxito lo cierra.
- **Reinicio de Rust**: el primer `boot_id` visto es la línea base. Cuando cambia, una sola vez y antes de procesar los resultados de esa respuesta, el hub quita de `confirmed` las entradas con tick mayor que `restored_tick` y resta de la cuenta de gotas aceptadas las de esos ticks (CP-05).
- **Gotas aceptadas**: se cuentan solo con los `applied` de `Step` (incluidos los `DUPLICATE` que se tratan como `applied`, ver arriba), por tick durante 600 ticks. `StateMessage` no tiene campo para ellas: el hub registra `accepted_drops` junto a `humidity_contributed` cuando la cuenta cambia (como mucho una vez por segundo), para la comprobación del modo demo (RF-30).
- **`/healthz`**: 200 mientras el proceso atiende (para Docker Compose). **`/readyz`**: 200 solo con el circuito cerrado y el último `Step` correcto; 503 en otro caso. Devuelve en JSON `ready`, `tick`, `paused`, `breaker` y `queue`.
- **Parada**: con SIGINT o SIGTERM cierra las conexiones con `StatusGoingAway`, termina el bucle y apaga el HTTP con un plazo de 5 s.

## Pendiente

- Token firmado (diagrama 09): el hub debe verificar el token Ed25519 al abrir `/play` y tomar de él `player_id` y `role`. Mientras tanto `player_id` va en la consulta como sustituto provisional (se rechaza si no es un UUID) y no hay roles.
- Resto de comandos de `play.proto`: `place_installation`, `buy_upgrade` y, solo con `role = admin`, `set_demo_mode` y `reset_valley` (`forbidden` sin ese rol). Hoy responden `unknown_type`, el código más cercano: el contrato no tiene uno para «no soportado».
- Datos para el panel del sistema (duración de cada `Step`, cola, gotas aceptadas).

## Imagen de Docker

El contexto es la raíz del repositorio, porque el código de Go se genera desde `proto/` durante la construcción:

```
docker build -f src/hub/Dockerfile -t hydrovalley-hub .
docker run --rm -e WORLD_ADDR=world:50051 -p 8081:8081 hydrovalley-hub
```

Genera `gen/go` con `bufbuild/buf:1.73.0`, compila un binario estático en `golang:1.26` y lo copia a `gcr.io/distroless/static-debian13:nonroot`, que se ejecuta con un usuario sin privilegios. La imagen no trae `curl`: su `HEALTHCHECK` ejecuta `hub healthcheck`, que consulta `/healthz` dentro del propio contenedor. `Dockerfile.dockerignore` limita el contexto a `proto/` y a los archivos de este servicio (requiere BuildKit, el constructor por defecto de Docker).
