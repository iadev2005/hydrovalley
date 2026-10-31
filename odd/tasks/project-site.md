# Sitio del proyecto, informe y entrega final

## Objetivo

Publicar el repositorio y una página del proyecto (GitHub Pages) con los diagramas rediseñados y la documentación Swagger, para que el informe enlace a ellos. Después, llevar `dev` a `main` con la etiqueta de la versión de entrega.

## Problema y por qué

El informe tiene un máximo de 12 páginas, así que remite a los diagramas por enlace en lugar de incluirlos como imagen. Esos enlaces solo funcionan si el repositorio es público. Los SVG de PlantUML son difíciles de leer reducidos, y Swagger solo existe mientras el sistema está levantado.

## Alcance

- `docs/site/`: página estática con:
  - los diagramas de componentes, despliegue y secuencia de la gota rediseñados como SVG propio, sin depender de PlantUML;
  - la galería de los nueve diagramas PlantUML;
  - Swagger UI sobre `docs/api/openapi.json`.
- `.github/workflows/pages.yml`: publica `docs/site/` y `docs/diagrams/` en GitHub Pages al fusionar en `main`.
- `docs/api/openapi.json`: copia de `GET /api/docs/json`, sacada de la API en ejecución (las rutas no cambiaron desde ba2c358).
- `docs/informe/`: fuente LaTeX y PDF del informe.
- Los `.puml` siguen siendo la fuente de verdad. Los SVG rediseñados son una vista para el informe y la página.

## Restricciones

- Sin cifras inventadas; todo sale de `docs/documento-inicial.md` y de los registros de pruebas.
- Hacer público el repositorio expone secretos antiguos del historial; el usuario lo pidió tras el aviso del 30 oct 2026.

## Tareas

- [x] T1 · Página del proyecto y diagramas rediseñados (`docs/site/`, `pages.yml`, `docs/api/openapi.json`). Ruta: delegada (escritor), varios archivos no triviales. Commits: f1a299c, bad1913.
- [ ] T2 · Informe en `docs/informe/` con las correcciones del usuario: tablas en su sitio, enlaces explícitos, Swagger, sin «pendiente» ni obras comunitarias. Ruta: inline.
- [ ] T3 · Repositorio público y GitHub Pages activado.
- [ ] T4 · PR `dev` → `main`, fusión y etiqueta `v1.0`.

## TDD

No aplica: son documentación y una página estática. Comprobaciones: el HTML carga sin errores de consola, el workflow pasa `actionlint` si está disponible y el CI queda en verde.

## Progreso

- 2026-10-31: documento creado; T1 en curso.
