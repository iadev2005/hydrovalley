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
docker run --rm -v "$PWD/proto:/workspace" -w /workspace bufbuild/buf lint
docker run --rm -v "$PWD/proto:/workspace" -w /workspace bufbuild/buf build
```

La validación usa las reglas `STANDARD`, y la detección de cambios incompatibles, `FILE`. Un cambio aquí afecta a todos los servicios, así que pasa por acuerdo del equipo en un pull request.
