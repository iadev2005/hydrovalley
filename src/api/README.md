# API

Servicio en TypeScript (Node 22 LTS) que atenderá el registro, el acceso con token firmado, el catálogo, el ranking y los reportes por REST, documentado con Swagger (RES-04). Es el esqueleto: por ahora solo expone la salud del proceso y la página de Swagger (ver [Pendiente](#pendiente)).

## Por qué Fastify

- `inject` prueba las rutas sin abrir un puerto, así que las pruebas son rápidas y deterministas.
- Cada ruta declara su esquema JSON: valida la entrada, serializa la salida y alimenta el documento OpenAPI con `@fastify/swagger`, sin mantener la documentación aparte. Express necesitaría varias piezas de terceros para lo mismo.
- Tipos de TypeScript propios y buen rendimiento con poco código.

## Estructura

| Ruta | Qué contiene |
| --- | --- |
| `src/app.ts` | Montaje de la aplicación: rutas y documentación OpenAPI. No escucha: las pruebas la usan con `inject` |
| `src/config.ts` | Variables de entorno |
| `src/main.ts` | Proceso: escucha y se detiene con SIGINT o SIGTERM |
| `src/healthcheck.ts` | Comprobación del `HEALTHCHECK` de Docker (la imagen no trae curl) |
| `src/health-probe.ts` | Lógica probada de esa comprobación: URL de bucle local, corchetes IPv6, plazo y código de salida |
| `test/` | Pruebas con Vitest |

## Variables de entorno

| Variable | Obligatoria | Valor por defecto | Uso |
| --- | --- | --- | --- |
| `API_LISTEN` | No | `0.0.0.0:3000` | Dirección y puerto del HTTP (`host:puerto`, `:puerto` o `[ipv6]:puerto`) |
| `LOG_LEVEL` | No | `info` | Nivel de registro de pino (`fatal`, `error`, `warn`, `info`, `debug`, `trace` o `silent`). Un valor distinto detiene el arranque con un mensaje que nombra la variable y los valores admitidos |

## Rutas

| Ruta | Respuesta |
| --- | --- |
| `GET /api/health` | `200 {"status": "ok"}` mientras el proceso atiende |
| `GET /api/docs` | Página de Swagger UI |
| `GET /api/docs/json` | Documento OpenAPI 3.1 |

Todas cuelgan de `/api` porque nginx reenvía `/api/` sin reescribir la ruta.

## Pruebas

Desde `src/api/`, con Node 22 o superior:

```
npm ci
npm run lint
npm run typecheck
npm test
```

## Ejecutar en local

```
npm run dev        # tsx, sin compilar
# o
npm run build && API_LISTEN=127.0.0.1:3000 npm start
```

## Imagen de Docker

El contexto es la raíz del repositorio, como en los demás servicios:

```
docker build -f src/api/Dockerfile -t hydrovalley-api .
docker run --rm -p 3000:3000 hydrovalley-api
```

Compila con `node:22-trixie-slim`, instala solo las dependencias de producción y se ejecuta con el usuario `node`, sin privilegios. `HEALTHCHECK` ejecuta `node dist/healthcheck.js`. `Dockerfile.dockerignore` limita el contexto a los archivos de este servicio.

## Pendiente

- Registro y acceso: cliente gRPC de `AccountService` (`PERSISTENCE_ADDR`), hash Argon2id y token Ed25519 (diagrama 09).
- Catálogo, ranking (`WorldService.GetRanking` y `AccountService.GetUsernames`) y reportes.
