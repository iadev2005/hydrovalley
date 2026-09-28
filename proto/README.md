# proto

Contratos gRPC de hydrovalley. Se escriben antes que el código de cualquier servicio; cada servicio genera su cliente o su servidor a partir de estos archivos. Fuente de verdad: `docs/documento-inicial.md` («El contrato: cómo se hablan los servicios»).

## Organización

Estilo de Buf: la carpeta refleja el paquete y cada paquete lleva versión. `buf.yaml` está en esta carpeta, que es la raíz del módulo.

| Carpeta | Paquete | Contenido |
| --- | --- | --- |
| `hydrovalley/world/v1/` | `hydrovalley.world.v1` | `WorldService` (`Step`, `GetRanking`), que sirve el simulador en Rust (o `world-go`), y `WorldSnapshot`, el mundo que viaja dentro de cada instantánea |
| `hydrovalley/persistence/v1/` | `hydrovalley.persistence.v1` | Un archivo por servicio, todos servidos por la persistencia (Python): `PersistenceService`, `AccountService`, `CatalogService` y `ReportService` |
| `hydrovalley/chat/v1/` | `hydrovalley.chat.v1` | `ChatService` (`SendMessage`, `ListMessages`) y los mensajes JSON del WebSocket `/chat` |
| `hydrovalley/play/v1/` | `hydrovalley.play.v1` | Sin servicio: los mensajes JSON del WebSocket `/play` entre el navegador y el hub |

Los mensajes de los WebSocket viajan en JSON (protojson con los nombres de campo del `.proto`). Cada mensaje lleva un campo de texto `type`; el detalle está en el comentario inicial de `play.proto`.

Los nombres van en inglés; los comentarios, en español.

## Validar y compilar

Desde la raíz del repositorio, con Docker:

```
docker run --rm -v "$PWD/proto:/workspace" -w /workspace bufbuild/buf:1.73.0 lint
docker run --rm -v "$PWD/proto:/workspace" -w /workspace bufbuild/buf:1.73.0 build
```

La validación usa las reglas `STANDARD`, y la detección de cambios incompatibles, `FILE`. Un cambio aquí afecta a todos los servicios, así que pasa por acuerdo del equipo en un pull request.

El CI (`.github/workflows/ci.yml`, job `proto`) ejecuta `buf lint` y `buf build` en cada pull request a `dev` o `main` y en cada push a `dev`; en los pull requests, además, `buf breaking` contra la rama base.

## Generar código

El código generado nunca se versiona: sale en `gen/`, que está en `.gitignore`. Desde la raíz del repositorio, con Docker y sin instalar buf:

```
scripts/gen-proto.sh
```

El script ejecuta `buf lint` y luego `buf generate` con `proto/buf.gen.yaml` (imagen `bufbuild/buf:1.73.0`; plugins remotos del BSR con versión fijada, así que necesita red). Si el asistente de credenciales de Docker se cuelga, se apunta a una carpeta con un `config.json` que contenga `{}`:

```
DOCKER_CONFIG=/ruta/a/esa/carpeta scripts/gen-proto.sh
```

| Salida | Lenguaje | Plugins | Quién la usa |
| --- | --- | --- | --- |
| `gen/go/` | Go | `protocolbuffers/go`, `grpc/go` | hub, chat, bots, world-go |
| `gen/python/` | Python | `protocolbuffers/python`, `protocolbuffers/pyi`, `grpc/python` | persistencia |
| `gen/ts/` | TypeScript | `bufbuild/es` (protobuf-es v2) | API y frontend |

**Go.** `gen/go/` es un módulo propio, `github.com/iadev2005/hydrovalley/gen/go` (su `go.mod` se copia de la plantilla versionada `proto/go.mod.tmpl`; la usan el script, el Dockerfile del hub y el CI, así que se cambia en un solo sitio). Cada servicio Go lo usa así en su `go.mod`:

```
require github.com/iadev2005/hydrovalley/gen/go v0.0.0
replace github.com/iadev2005/hydrovalley/gen/go => ../../gen/go
```

y lo importa como `github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1`. Se eligió un único módulo compartido en lugar de generar dentro de cada servicio porque hay cuatro servicios Go que usan los mismos contratos: una sola generación y una sola ruta de importación, sin copias que puedan quedar desfasadas. Las versiones de `google.golang.org/protobuf` y `google.golang.org/grpc` las fija el `go.mod` de cada servicio.

**Rust.** No se genera aquí: el simulador (`src/world/`) compila los `.proto` con `tonic-build` en su `build.rs`, que es lo habitual en Rust y no necesita un paso aparte.
