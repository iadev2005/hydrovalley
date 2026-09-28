# Archivos por nodo (plan A)

Aquí irá un archivo de Compose por máquina del laboratorio (`db.yml`, `persistence.yml`, `world.yml`, `hub.yml`, `proxy.yml`…), según el diagrama `08-despliegue`. Todavía no existen: llegan con el primer ensayo repartido, cuando el encargado cree la regla del cortafuegos.

Reglas que deben cumplir (regla 6 de portabilidad en `CLAUDE.md`):

- Las mismas imágenes que `deploy/compose.yml` (las del pipeline, `linux/amd64`, etiqueta `dev` o una versión de `main`). Nada se construye en el laboratorio.
- Solo cambian direcciones y puertos: `PERSISTENCE_ADDR`, `WORLD_ADDR`, `API_ADDR`, `HUB_ADDR`… con `10.68.17.x:puerto` en vez del nombre del servicio.
- Cada nodo publica solo sus puertos: 8080 (proxy), 8081 y 8082 (hub y chat), 50051 (gRPC) y 5433 (PostgreSQL, porque el 5432 lo ocupa el PostgreSQL nativo del laboratorio).
