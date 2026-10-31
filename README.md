# hydrovalley

Juego incremental cooperativo y distribuido sobre las 3R del agua (reducir, reutilizar y reciclar), inspirado en la mecánica de *Drop and Grow*. Proyecto de Sistemas Distribuidos y Computación en la Nube, Universidad Católica Andrés Bello (UCAB).

Página del proyecto, con los diagramas y la documentación de la API: <https://iadev2005.github.io/hydrovalley/>.

## Arquitectura

El sistema sigue una arquitectura de microservicios contenerizada con Docker Compose:

| Servicio | Lenguaje / Tecnologías | Carpeta | Responsabilidad |
| --- | --- | --- | --- |
| **Proxy de entrada** | Nginx (TLS terminación) | `deploy/nginx/` | Puerta única: enrutamiento `/api`, `/play`, estáticos y terminación TLS (HTTPS/WSS) |
| **API** | TypeScript (Node.js, Express) | `src/api/` | Autenticación, catálogo, ranking, reportes y Swagger (`/api`) |
| **Frontend** | TypeScript, React 19, Vite, Phaser | `src/frontend/` | Cliente web: paneles React, valle isométrico con Phaser y salida de emergencia a canvas 2D |
| **Hub en tiempo real** | Go (goroutines, WebSockets) | `src/hub/` | Conexiones de jugadores (`/play`), rate limiting (15 gotas/s), deduplicación y broadcast de ticks |
| **Simulador del valle** | Rust (Tonic, Tokio) | `src/world/` | Único escritor autoritativo del estado del valle, autómata celular y generador con semilla |
| **Persistencia** | Python (gRPC, SQLAlchemy) | `src/persistence/` | Único acceso a PostgreSQL: cuentas, catálogo, instantáneas cada 5 s y reportes |
| **Bots** | Go | `src/bots/` | Simulación de jugadores concurrentes, pruebas de carga y escenarios de prueba |

## Estado del Frontend

El cliente web (`src/frontend/`) está completamente migrado y funcional (tareas A2, A3 y A4):
- **Pila tecnológica:** Construido con **Vite**, **React 19** y **TypeScript estricto** (`tsc -b`). Servido como estáticos por Nginx mediante compilación multi-etapa (`deploy/nginx/Dockerfile`).
- **Valle Isométrico (Phaser):** Renderizado interactivo 2.5D de 32 × 32 casillas con proyección de diamante, animaciones de gotas en vuelo, etapas de vegetación e incendios.
- **Salida de Emergencia (Vista Cenital 2D):** Vista interactiva en `<canvas>` 2D como alternativa manual conmutada por el jugador o automática (*Graceful Degradation*) si el navegador carece de soporte WebGL/aceleración gráfica.
- **Paneles React:** Acceso/registro con respeto a `Retry-After`, depósito, mejoras, ranking, reporte 3R y panel del sistema (`/sistema.html`).

## Cómo se levanta

### Despliegue local (Plan B con Docker Compose)

1. Configurar variables de entorno iniciales:
   ```bash
   cp deploy/.env.example deploy/.env
   ```
2. Construir y levantar todos los servicios:
   ```bash
   docker compose -f deploy/compose.yml up --build
   ```
3. Abrir en el navegador: [https://localhost:8080](https://localhost:8080) (o `http://localhost:8080`, Nginx redirige automáticamente a HTTPS).
   - Acceso principal al juego: `/` (`index.html`)
   - Panel de observabilidad del sistema: `/sistema.html`

Detalles completos de configuración, claves y volúmenes en [`deploy/README.md`](deploy/README.md).

## Pruebas

El repositorio cuenta con pruebas unitarias, de integración y extremo a extremo (E2E) por servicio:

- **Punta a punta (E2E y fallos):**
  ```bash
  tests/e2e/run.sh
  ```
  Levanta el stack con claves y certificados TLS efímeros, ejecutando pruebas de humo, concurrencia (CP-01), tolerancia a fallos (CP-05, CP-06, CP-07, CP-08, CP-12) y RNF-10 (< 5 min).
- **Frontend:**
  ```bash
  pnpm --dir src/frontend test       # Vitest (198 pruebas)
  pnpm --dir src/frontend typecheck  # TypeScript estricto
  pnpm --dir src/frontend lint       # ESLint
  ```
- **Mundo (Rust):** `cargo test --manifest-path src/world/Cargo.toml`
- **Hub (Go):** `go test -race ./...` (en `src/hub`)
- **Persistencia (Python):** `pytest` (en `src/persistence`)
- **API (TypeScript):** `pnpm --dir src/api test`

## Documentación de Referencia

- [`docs/documento-inicial.md`](docs/documento-inicial.md): Requerimientos formales, decisiones de arquitectura y especificación de reglas. Fuente de verdad del proyecto.
- [`docs/tareas-equipo.md`](docs/tareas-equipo.md): Guía de tareas del equipo y mapa de trabajo.
- [`CLAUDE.md`](CLAUDE.md): Reglas invariantes del sistema (escritor único, idempotencia, ticks y contratos).
- [`src/frontend/README.md`](src/frontend/README.md): Arquitectura detallada del cliente React + Phaser.
