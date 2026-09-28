# Despliegue

| Ruta | Qué contiene |
| --- | --- |
| `compose.yml` | Plan B: todo en una máquina, un contenedor por servicio (diagrama `08-despliegue-plan-b`) |
| `.env.example` | Plantilla de `deploy/.env` (credenciales de PostgreSQL, puerto del host y etiqueta de las imágenes). `.env` nunca se versiona |
| `nginx/` | Proxy de entrada: plantilla de configuración y la imagen, que incluye el frontend |
| `nodes/` | Plan A: un archivo por máquina (pendiente) |

## Plan B

```
cp deploy/.env.example deploy/.env      # y cambiar POSTGRES_PASSWORD
docker compose -f deploy/compose.yml up --build
```

Luego, http://localhost:8080. Solo nginx publica un puerto (`HV_HTTP_PORT`, 8080 por defecto); PostgreSQL no se publica. Los servicios se encuentran por su nombre (`world:50051`, `persistence:50051`…) mediante variables de entorno.

- **Arranque**: no se usa `depends_on`. Cada servicio arranca aunque los demás no estén y se reconecta solo (regla 5); `restart: unless-stopped` cubre las caídas. Hasta que la persistencia conecta con PostgreSQL y el simulador carga su instantánea, el hub tiene el valle en pausa.
- **Healthchecks**: van en cada imagen, para que el plan A los herede. Hub: `hub healthcheck` (`/healthz`). Simulador y persistencia: `grpc_health_probe` contra `grpc.health.v1` (SERVING solo con la instantánea cargada o la base conectada). API: `node dist/healthcheck.js`. nginx: `wget` a `/healthz`. PostgreSQL: `pg_isready`, en `compose.yml`.

## Proxy (nginx)

| Ruta | Destino |
| --- | --- |
| `/` | Archivos estáticos del frontend, dentro de la misma imagen |
| `/api/` | `API_ADDR` (plan B: `api:3000`), sin reescribir la ruta; Swagger en `/api/docs` |
| `/play` | `HUB_ADDR` (plan B: `hub:8081`), con las cabeceras de WebSocket y `Host $http_host` (el hub compara `Origin` con `Host`, puerto incluido) |
| `/chat` | 503 hasta que exista el chat (nivel 2) |
| `/healthz` | 200, para el healthcheck |

Los destinos se resuelven en cada petición con el DNS de Docker (`NGINX_RESOLVER`, 127.0.0.11), así nginx arranca aunque la API o el hub todavía no estén.

**TLS (RNF-12), pendiente**: por ahora HTTP en el 8080. En el laboratorio, HTTPS en ese mismo puerto con un certificado autofirmado.

## Prueba de humo

```
tests/e2e/run.sh
```

Levanta el plan B con el proyecto `hydrovalley-e2e` y los valores de `.env.example`, suelta una gota por nginx, espera al tick 50, comprueba que hay una fila en `snapshots` y lo baja todo con `down -v`.
