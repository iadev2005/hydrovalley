# Tareas del equipo y guía para trabajar con IA

Este documento sirve para que cada persona del equipo **elija una tarea y la haga con su asistente de IA** sin romper lo que ya funciona. Tiene dos partes:

1. **Guía para la IA** (secciones 1 a 6): cómo está hecho el repositorio, qué no se puede romper, cómo se prueba cada cosa y cómo se entrega. Pásasela entera a tu asistente antes de pedirle nada.
2. **Tareas** (secciones 7 a 10): el desglose de lo que falta, con objetivo, archivos, criterios de aceptación y cómo verificarlo.

Estado del repositorio al escribir esto: rama `dev` en el PR #44 (viernes 9 de octubre de 2026, fin de la semana 4). La fuente de verdad sigue siendo `docs/documento-inicial.md`: si este documento y aquel no coinciden, manda aquel y hay que corregir este.

---

## Parte 1 · Guía para la IA

### 1. Cómo empezar una sesión con tu IA

Copia esto como primer mensaje, cambiando el ID de la tarea:

```text
Trabajas en el repositorio hydrovalley (proyecto universitario de sistemas distribuidos).
Antes de escribir código lee, en este orden:
1. CLAUDE.md (reglas del proyecto; son obligatorias).
2. docs/tareas-equipo.md, secciones 1 a 6, y la tarea <ID> de la sección 8, 9 o 10.
3. El README del servicio que vas a tocar (src/<servicio>/README.md) y deploy/README.md.
4. Las secciones de docs/documento-inicial.md que cite la tarea.
Después: resume en 5 líneas qué vas a cambiar y qué pruebas vas a escribir, y espera mi visto bueno.
Reglas: pruebas primero (rojo → verde), nombres en inglés y comentarios en español,
nunca inventes cifras ni resultados (usa [DATO: …]), nunca toques archivos .env,
un commit por unidad de trabajo con Conventional Commits, rama feature/… desde dev.
```

Consejos:

- **Una tarea por sesión.** Si tu IA empieza a "aprovechar" para arreglar otra cosa, para y abre otra tarea.
- **Pídele siempre la salida real de las pruebas**, no un "ya pasan". Si no las ejecutó, no están probadas.
- **Si la IA dice que el documento está mal**, no lo cambies por tu cuenta: anótalo en la descripción del PR (regla de `CLAUDE.md`).

### 2. Qué es el sistema, en una página

Un valle isométrico de 32 × 32 casillas compartido por todos los jugadores. Se sueltan gotas, las casillas crecen (tierra seca → pasto → arbusto → flores → árbol), se ganan hojas y con ellas se compran instalaciones de las 3R. Hay evaporación, marchitamiento e incendios. Las reglas y las cifras están en la sección D-01 del documento; las cifras viven en `catalog/catalog.json`.

```text
navegador ──HTTPS/WSS──> nginx :8080 ──/api──> API (TypeScript, Fastify) ──gRPC──> persistencia (Python) ──> PostgreSQL
                                     ──/play─> hub (Go, WebSocket) ──gRPC Step cada 100 ms──> simulador (Rust)
                                     ──/chat─> chat (Go, aún no existe)                         │
                                                                    simulador ──SaveSnapshot cada 5 s──> persistencia
```

| Servicio | Carpeta | Hace | No hace nunca |
| --- | --- | --- | --- |
| nginx | `deploy/nginx/` | Única puerta: `/api`, `/play`, `/chat`, sirve el frontend | Lógica de juego |
| API | `src/api/` | Registro, acceso (token Ed25519), ranking, reportes, `/api/system`, cuentas de bots | Tocar la base de datos |
| Hub | `src/hub/` | Una goroutine por jugador; filtra sesión, `action_id` y 15 gotas/s; llama a `Step` cada 100 ms; difunde el estado cada 1 s; `/stats` | Calcular el mundo |
| Simulador | `src/world/` | **Único escritor** del mundo: aplica los comandos del tick, autómata celular, azar con semilla, instantáneas | Hablar con jugadores |
| Persistencia | `src/persistence/` | **Única puerta a PostgreSQL**: cuentas, catálogo, instantáneas, reportes | Lógica de juego |
| Bots | `src/bots/` | Vecinos, carga (CP-11), tramposo (CP-03), bombero (CP-04) | Llamar por gRPC: solo usan nginx |
| Frontend | `src/frontend/` | Página del juego y panel del sistema (`/sistema.html`) | — |

Frontera Go/Rust: **Go decide si un comando es admisible; Rust decide qué produce.**

### 3. Mapa del repositorio y archivos importantes

| Ruta | Para qué sirve | Cuándo leerlo |
| --- | --- | --- |
| `CLAUDE.md` | Reglas del proyecto, arquitectura, convenciones, ramas, despliegue | **Siempre** |
| `docs/documento-inicial.md` | Requisitos (RF, RNF, RES), decisiones (D-01…), casos de prueba (CP-01…CP-12), plan, guion de la defensa, guía del informe | Siempre que la tarea cite un RF, CP o D |
| `docs/diagrams/*.puml` | Diagramas UML 01–09 (con su `.svg` al lado) | Si cambias el diseño |
| `proto/hydrovalley/<servicio>/v1/*.proto` | Contratos gRPC y los mensajes JSON de `/play` (`play/v1/play.proto`) | Si cambias cómo se hablan dos servicios |
| `catalog/catalog.json` | Cifras del juego (umbrales, precios, fuego…) | Si una regla depende de una cifra |
| `deploy/compose.yml` | Plan B: todo en una máquina; servicio `bots` bajo el perfil `bots` | Para levantar el sistema |
| `deploy/README.md` | Cómo levantar, variables de entorno, claves, casos de fallos | Para levantar el sistema |
| `deploy/nginx/` | Imagen de nginx (incluye el frontend) y su plantilla | Si tocas rutas o el frontend |
| `deploy/nodes/README.md` | Reglas del plan A (cinco máquinas) | Despliegue en el laboratorio |
| `src/<servicio>/README.md` | Cómo está hecho cada servicio, sus variables, sus pendientes | Antes de tocar ese servicio |
| `tests/e2e/` | Pruebas de punta a punta: `run.sh`, `smoke.test.mjs`, `faults*.test.mjs` | Si tu cambio cruza servicios |
| `.github/workflows/ci.yml` | Un job por servicio + `e2e` | Si añades un servicio o una herramienta |
| `odd/tasks/*.md` | Historial de cada funcionalidad: qué se hizo, con qué evidencia y qué quedó pendiente | Para entender por qué algo es como es |
| `scripts/gen-proto.sh` | Genera el código de los `.proto` en `gen/` (no se versiona) | Tras cambiar un `.proto` |

### 4. Reglas que no se rompen (resumen de `CLAUDE.md`)

1. **Escritor único:** solo Rust modifica el mundo. Go llama a `Step` una vez por tick, nunca dos a la vez.
2. **Idempotencia:** toda acción que modifica lleva `action_id`. Resultados finales: `applied | duplicate | rejected | expired`; rechazos inmediatos del hub: `duplicate | rate_limited | paused | forbidden`. El cliente trata `duplicate` como `applied`.
3. **El tick es el reloj lógico.** Nunca se ordenan eventos comparando relojes de máquinas distintas.
4. **Sin streams de gRPC:** todo es pregunta y respuesta, con plazo y reintentos acotados (solo si la llamada es idempotente).
5. **Solo Python toca PostgreSQL.**
6. **El azar solo lo decide Rust**, con semilla guardada en la instantánea.
7. **u64 sin signo** para agua, hojas y humedad; en JSON viajan como **texto**.
8. **Instantánea cada 50 ticks** en segundo plano; `Step` nunca espera a la persistencia.

Y de la portabilidad (plan A ↔ plan B):

- Ninguna dirección en el código: todo por variables de entorno (`WORLD_ADDR`, `PERSISTENCE_ADDR`, `HUB_STATS_ADDR`…). **Nunca `localhost` entre servicios.**
- Cada servicio arranca aunque los demás no estén y se reconecta solo (CP-08 lo comprueba).
- Sin carpetas compartidas entre contenedores.
- Las imágenes del laboratorio las construye el CI (`linux/amd64`). **Nunca se despliega una imagen construida en un Mac con chip Apple.**

### 5. Convenciones

| Tema | Convención |
| --- | --- |
| Idioma | Identificadores, nombres de archivo de código y `.proto` en **inglés**; comentarios, documentación, mensajes de la interfaz y nombres de pruebas en **español neutro** |
| Commits | Conventional Commits: `feat(hub): …`, `fix(api): …`, `test(e2e): …`, `docs: …`, `ci: …`. Sin líneas de atribución a IA |
| Ramas | `feature/<algo>` o `fix/<algo>` desde `dev`; PR contra `dev`. `main` solo recibe `dev` cuando hay versión etiquetada |
| PR | CI en verde (los 9 jobs son obligatorios). Si el código contradice el documento, se dice en la descripción |
| Pruebas | Primero la prueba que falla, luego el código. Cada PR dice qué pruebas añadió y su salida |
| Cifras | Nunca inventadas. Si falta un dato: `[DATO: …]`; si falta una fuente: `[FUENTE: …]` |
| Identificadores | UUID en texto. Fechas ISO 8601 en UTC. Tiempos en segundos |
| Errores | Códigos estándar de gRPC entre servicios |
| Saltos de línea | LF siempre (`.gitattributes`); el laboratorio usa Windows |
| Código generado | En `gen/`, nunca se versiona |
| Diagramas | PlantUML en `docs/diagrams/`; el `.svg` va en **un commit aparte**, después del `.puml` |
| Secretos | Nunca en el repositorio. `deploy/.env` es local; `deploy/.env.example` solo lleva ejemplos |

### 6. Cómo se ejecuta y se prueba cada cosa

| Servicio | Pruebas | Lint y formato | En local |
| --- | --- | --- | --- |
| `src/hub`, `src/bots` (Go 1.26) | `go test -race ./...` | `gofmt -l .`, `go vet ./...`, `go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...` | `go run ./cmd/hub` con `WORLD_ADDR`, `HUB_LISTEN`, `TOKEN_PUBLIC_KEY` |
| `src/world` (Rust 1.97, edición 2024) | `cargo test --locked` | `cargo fmt --check`, `cargo clippy --locked --all-targets -- -D warnings` | `cargo run` con `PERSISTENCE_ADDR`, `WORLD_LISTEN` |
| `src/persistence` (Python 3.12, uv) | `uv run pytest -m "not integration"`; con base de datos: `-m integration` y `TEST_DATABASE_URL` | — | Ver su README |
| `src/api` (Node 22, Fastify 5) | `npm test` (Vitest) | `npm run lint`, `npm run typecheck` | `npm run gen` y `npm run dev` |
| `src/frontend` (TypeScript, Vite, React) | `pnpm test` (Vitest) | `pnpm run lint`, `pnpm run typecheck` | `pnpm run dev` (o lo sirve nginx) |
| `proto` | `buf lint` y `buf breaking` (en el CI) | — | `scripts/gen-proto.sh` |

**Todo el sistema (plan B):** sigue `deploy/README.md`. En resumen: copia `deploy/.env.example` a `deploy/.env`, rellena PostgreSQL y las claves Ed25519 (el comando está en el README), y `docker compose -f deploy/compose.yml up --build`. Abre `http://localhost:8080` (juego) y `http://localhost:8080/sistema.html` (panel).

**Bots:** `docker compose -f deploy/compose.yml --profile bots run --rm bots neighbors` (también `load-test`, `cheater`, `firefighter`), con `HV_ADMIN_USERNAME` y `HV_ADMIN_PASSWORD`.

**Punta a punta:** `tests/e2e/run.sh` levanta el sistema con claves efímeras en el proyecto `hydrovalley-e2e` y corre el smoke, CP-01 (100 gotas simultáneas), los casos de fallos (CP-05, 06, 07, 12), CP-08 y, al final con un stack nuevo y el catálogo de prueba, CP-04 (`E2E_SKIP_CP04=1` lo omite). Tarda unos 7 minutos en el CI. `E2E_SKIP_FAULTS=1` omite los fallos; `HV_HTTP_PORT=18091` si el 8080 está ocupado.

---

## Parte 2 · Tareas

### 7. Cómo se toma y se cierra una tarea

1. Elige una tarea cuyas dependencias estén cerradas. **Avisa en el grupo** (o asígnate el issue de GitHub con su ID, si se crean) para no duplicar trabajo.
2. Rama `feature/<id>-<nombre>` desde `dev` actualizado.
3. Trabaja con tu IA siguiendo la sección 1. Commits pequeños.
4. PR contra `dev` con: qué hace, qué pruebas añadió (con su salida), qué cifras midió y cualquier contradicción con el documento.
5. CI en verde. Que otra persona del equipo le eche un vistazo antes de fusionar (no es obligatorio en GitHub, pero sí recomendable).
6. Si la tarea cambia el diseño, actualiza el diagrama en el mismo PR. Si mide algo que el documento tiene como `[DATO]`, sustitúyelo.

Tamaños: **S** ≈ 1 día · **M** ≈ 2–3 días · **L** ≈ una semana o más (conviene partirla). El "perfil" sugiere quién encaja mejor según el reparto del documento (sección «El reparto»).

### Resumen

Tareas tomadas (actualízalo en tu PR cuando tomes o cierres una): A1, A2, A3 y A4 hechas (Fer2703, PR #46–#50); A8, A9 y A10 hechas (Cristian Baczek, integradas y corregidas por Claude, PR #56); A5, A6 y A7 hechas (Claude, por encargo de Ignacio; PR #51–#53, detalle en `odd/tasks/a5-a7-tls-tracing-diagrams.md`); A11 hecha (Claude, PR #55); A12 hecha (Alfonso, `docs/patrones.md`); B1 hecha (Claude, PR #59) y ensayada en el laboratorio el 23 de octubre (Fer y el equipo; registro en `docs/registro-despliegue-laboratorio.md`); B2 hecha (PR #62); B3 hecha (Cristian Baczek, integrada por Claude); B5 hecha (Claude, PR #60–#65; `world-go` sustituye a Rust por configuración; comparativa medida bajo D-03); B6 hecha (Alfonso José Palma Alvarado, completada e integrada por Claude, PR #73; Rust, `world-go`, persistencia, hub y frontend); B7 hecha (Cristian Baczek, integrada por Claude); B8 completada (Fer2703: Fases 0–5 cerradas; documento de decisión con mediciones en `docs/decisiones/b8-orquestacion.md`, tareas T1–T6 completadas y mediciones de aceptación registradas en `odd/tasks/b8-failover.md`).

| ID | Tarea | Nivel | Tamaño | Depende de | Perfil |
| --- | --- | --- | --- | --- | --- |
| A1 | Publicar imágenes en ghcr.io desde el CI | 1 | S | — | Contenedores / CI |
| A2 | Frontend en TypeScript + Vite + React (migración de los paneles) | 1 | M | — | TypeScript |
| A3 | Valle isométrico con Phaser | 1 | L | A2 | TypeScript |
| A4 | Salida de emergencia a vista cenital | 1 | S | A2 | TypeScript |
| A5 | TLS en nginx (HTTPS y WSS) | 1 | S | — | Contenedores |
| A6 | Identificador de petición de punta a punta (RNF-07) | 1 | M | — | Cualquiera |
| A7 | Diagramas al día | 1 | M | — | Cualquiera |
| A8 | CP-01 de verdad: 100 gotas simultáneas | 1 | S | — | Go / e2e |
| A9 | CP-04 contra un valle real con incendios | 1 | S | — | Go / e2e |
| A10 | Prueba de carga en el CI y RNF-03 | 1 | M | — | Go / CI |
| A11 | `GET /api/catalog` | 1 | S | — | TypeScript |
| A12 | Patrones de la nube identificados en el código | 1 | S | — | Cualquiera |
| A13 | Pendientes pequeños | 1 | S | — | Varios |
| B1 | Despliegue repartido en el laboratorio (plan A) | 2 | M | A1 | Contenedores |
| B2 | Chat (Go) | 2 | L | — | Go |
| B3 | Propagación del fuego | 2 | M | — | Rust |
| B4 | Obras comunitarias (RF-26) | 2 | L | — | Rust + TypeScript |
| B5 | Tick en Go (`world-go`) y comparativa Go vs. Rust | 2 | L | — | Go + Rust |
| B6 | Resumen al volver | 2 | M | A2 | Rust + TypeScript |
| B7 | Prometheus y Grafana | 2 | M | — | Contenedores |
| B8 | Mantener el juego con una máquina apagada (orquestación) | 2 | L | B1, B5 | Contenedores + Go |
| C1 | Informe | — | L | — | Todos |
| C2 | Ensayo de la defensa y video de respaldo | — | M | B1 | Todos |
| D1–D3 | Decisiones del equipo | — | — | — | Todos |

Prioridad para la semana 5 (12–16 oct, se cierra el nivel 1): **A1 primero** (bloquea el laboratorio), luego **A2 → A3/A4** en paralelo con **A5, A6, A7, A8, A9**.

---

### 8. Bloque A · Cerrar el nivel 1 (semana 5)

#### A1 · Publicar imágenes en ghcr.io desde el CI

- **Por qué:** `CLAUDE.md` (sección «Ramas») dice que el pipeline publica las imágenes, pero `ci.yml` no lo hace. Sin eso no se puede desplegar en el laboratorio, porque allí no se construye nada. Cubre RF-21 y RES-06.
- **Archivos:** `.github/workflows/ci.yml`; `deploy/README.md`; `deploy/nodes/README.md`.
- **Qué hacer:**
  1. Un job `publish` que corre solo en `push` a `dev` (tras fusionar) y en etiquetas `v*` sobre `main`. Permisos `packages: write`, `docker/login-action` con `GITHUB_TOKEN`.
  2. Construir con `docker buildx bake -f deploy/compose.yml` (el CI ya usa bake para el e2e) para `linux/amd64` y empujar las seis imágenes: `api`, `hub`, `world`, `persistence`, `nginx`, `bots`. Nombres: los que ya espera `deploy/compose.yml` (`ghcr.io/iadev2005/hydrovalley-<servicio>:${HV_TAG}`). Etiquetas: `dev` en `dev`; `vX.Y` en la etiqueta.
  3. Acciones fijadas por SHA, como el resto del workflow.
  4. Documentar cómo se descarga en el laboratorio (ver D1 sobre la visibilidad de los paquetes).
- **Aceptación:** tras fusionar en `dev`, en una máquina x86 `HV_TAG=dev docker compose -f deploy/compose.yml pull` descarga las seis imágenes y el sistema levanta sin `--build`.
- **Verificación:** el job `publish` en verde en la pestaña Actions; `docker manifest inspect ghcr.io/iadev2005/hydrovalley-hub:dev` muestra `linux/amd64`.
- **Ojo:** el repositorio es privado, así que los paquetes nacen privados (D1).

#### A2 · Frontend en TypeScript + Vite + React

- **Por qué:** el documento y `CLAUDE.md` dicen React + Phaser + TypeScript; hoy es JavaScript plano con un `<canvas>` 2D (`src/frontend/README.md`). Los diagramas 02 y 08 ya lo dibujan como React + Phaser.
- **Archivos:** todo `src/frontend/`; `deploy/nginx/Dockerfile` y `Dockerfile.dockerignore` (etapa de compilación con Node que copia `dist/` a nginx); job `frontend` de `ci.yml`.
- **Qué hacer:**
  1. Vite + React + TypeScript estricto. Mantener **dos páginas**: el juego (`index.html`) y el panel (`sistema.html`).
  2. Portar la lógica pura que ya existe y está probada (`js/auth.js`, `js/play.js`, `js/reports.js`, `js/valley.js`, `js/system.js`) a TypeScript **sin cambiar su comportamiento**, con sus pruebas (Vitest). Esas pruebas son la red de seguridad de la migración.
  3. Los paneles (acceso, sesión, depósito, compras, mejoras, administración, ranking, reporte, panel del sistema) como componentes React (contenedor-presentacional).
  4. Mientras A3 no esté, el valle sigue en el `<canvas>` actual dentro de un componente.
  5. El job `frontend` del CI corre lint, typecheck, pruebas y `build`.
- **Aceptación:** la página hace todo lo que hace hoy (ver la lista en `src/frontend/README.md`); el e2e (`tests/e2e/run.sh`) sigue en verde; `pnpm run build` produce los estáticos que sirve nginx.
- **Verificación:** prueba manual en el navegador: registrarse, entrar, soltar gotas, comprar, ranking, reporte, panel del sistema, reconexión al reiniciar el hub. **Comprobar en un navegador de verdad** (pendiente desde varias funcionalidades).
- **Ojo:** el token va en la URL del WebSocket (riesgo aceptado); los u64 llegan como texto y no se convierten a `Number` si pueden pasar de 2^53.

#### A3 · Valle isométrico con Phaser

- **Por qué:** nivel 1, el corazón visual del juego (RF-03, RF-04).
- **Archivos:** `src/frontend/` (escena de Phaser), `src/frontend/public/assets/`.
- **Qué hacer:**
  1. Escena de Phaser que dibuja las 32 × 32 casillas en isométrico a partir del marco `state` (`stages`, `humidity`, `fires`, `installations`; ver `proto/hydrovalley/play/v1/play.proto`). El servidor no sabe cómo se dibuja: la conversión `(x, y)` → pantalla es solo del cliente.
  2. Clic → casilla: la conversión inversa de pantalla a `(x, y)`, con prueba unitaria (función pura).
  3. Sprite por etapa (cinco), tinte o indicador por humedad, fuego animado, instalaciones con su icono.
  4. Acción pendiente visible (por ejemplo, una gota "en el aire") hasta su resultado final.
  5. Cámara: arrastrar y zoom.
  6. Recursos gráficos: propios o con licencia libre, con la fuente anotada en `src/frontend/assets/README.md` (`[FUENTE: …]` si falta).
- **Aceptación:** con 30 bots (`load-test`) la escena mantiene una tasa de cuadros fluida; las coordenadas de clic coinciden con la casilla que recibe la gota.
- **Verificación:** pruebas unitarias de la conversión de coordenadas; prueba manual con bots vecinos.

#### A4 · Salida de emergencia a vista cenital

- **Por qué:** nivel 1 en `CLAUDE.md` («valle isométrico con salida de emergencia a vista cenital»): si Phaser falla o va lento, el juego sigue jugable.
- **Archivos:** `src/frontend/`.
- **Qué hacer:** un botón para cambiar entre la vista isométrica y la vista cenital (la cuadrícula 2D actual), recordado en `localStorage`. Si Phaser no carga, se abre la cenital automáticamente.
- **Aceptación:** las dos vistas muestran el mismo estado y aceptan los mismos clics.
- **Verificación:** prueba de la función que decide la vista; prueba manual bloqueando la carga de Phaser.

#### A5 · TLS en nginx (HTTPS y WSS)

- **Por qué:** RNF-12 y la sección de contraseñas del documento: «nginx termina TLS en el puerto 8080 del host (con un certificado autofirmado en el laboratorio)». Hoy va en HTTP (`deploy/nginx/templates/default.conf.template`).
- **Archivos:** `deploy/nginx/` (plantilla y Dockerfile), `deploy/compose.yml`, `deploy/README.md`, `tests/e2e/` (aceptar el certificado autofirmado), `src/bots` (opción para aceptar el certificado en pruebas).
- **Qué hacer:** certificado autofirmado generado al arrancar el contenedor si no se monta uno (nunca en el repositorio); HTTPS en 8080 y WSS para `/play`; cabeceras básicas (HSTS no, porque es autofirmado).
- **Aceptación:** `https://localhost:8080` y `wss://…/play` funcionan; el e2e y los bots pasan por HTTPS.
- **Verificación:** e2e en verde; el panel del sistema y el juego cargan en el navegador tras aceptar el aviso del certificado.

#### A6 · Identificador de petición de punta a punta (RNF-07)

- **Por qué:** RNF-07 no está implementado (no hay `request_id` en ningún servicio). Sirve para seguir una petición en los registros de todos los servicios durante la defensa.
- **Archivos:** `deploy/nginx/` (cabecera `X-Request-Id` con `$request_id`), `src/api` (leerla, registrarla, propagarla como metadato gRPC `x-request-id`), `src/persistence` y `src/world` (leer el metadato y registrarlo), `src/hub` (para `/play`, el `action_id` ya hace de identificador: registrarlo de forma consistente).
- **Aceptación:** con un solo `curl` a `/api/auth/login` se encuentra el mismo identificador en los registros de nginx, API y persistencia.
- **Verificación:** prueba unitaria por servicio de la propagación; una prueba e2e que busca el identificador en `docker compose logs`.

#### A7 · Diagramas al día

- **Por qué:** RES-05; los diagramas no se tocan desde el 29 de septiembre y el código cambió mucho.
- **Archivos:** `docs/diagrams/*.puml` (y sus `.svg` en un commit aparte).
- **Deriva conocida:**
  - 02 (componentes): falta `GET /api/system`, el `/stats` del hub, `POST /api/admin/bot-accounts`, el uso de `GetCatalog` y `GetRanking`, el panel `sistema.html`.
  - 03 (secuencia de la gota): el marco `state` ahora lleva `humidity_contributed` y `accepted_drops`.
  - 04 (reinicio de Rust): `restored_tick` ya se expone en `/stats` y `/api/system`.
  - 08 (despliegue): la nota de la línea ~140 dice que los bots no están en `compose.yml`, y sí están (perfil `bots`).
  - 02 y 08 dibujan React + Phaser: correcto como destino, pero coordinar con A2/A3.
- **Aceptación:** los nombres coinciden con los RF, los `.proto` y el código; cada `.svg` regenerado con `docker run --rm -v "$PWD/docs/diagrams:/data" plantuml/plantuml:1.2026.8 -tsvg '/data/*.puml'`.
- **Verificación:** abrir los SVG y revisar contra el código; el `.svg` en commit separado.

#### A8 · CP-01 de verdad: 100 gotas simultáneas

- **Por qué:** RF-08 es «requisito estrella». Hoy CP-01 solo existe en versión ligera (una gota, `tests/e2e/smoke.test.mjs`) y en pruebas de dominio.
- **Archivos:** `tests/e2e/` (nuevo caso) o un escenario en `src/bots`.
- **Qué hacer:** modo demo; un mismo jugador envía 100 gotas a la vez (sin esperar resultados) sobre varias casillas. Hay que respetar el límite de 15/s, así que conviene repartirlas en el tiempo o usar varios jugadores; la regla es que **lo aceptado cuadre exacto**: la humedad aportada sube exactamente las gotas aceptadas y el depósito no cambia en modo demo.
- **Aceptación y verificación:** caso e2e en verde, con una línea `MEDICION CP-01 …` con enviadas, aceptadas y la diferencia de humedad.

#### A9 · CP-04 contra un valle real con incendios

- **Por qué:** el bombero existe, pero en las ejecuciones reales no apareció ningún incendio (`odd/tasks/bots.md`, T4). CP-04 solo está probado con un mundo simulado.
- **Archivos:** `tests/e2e/cp04-firefighter.test.mjs`, `tests/e2e/fixtures/catalog.test-fire.json`, `tests/e2e/compose.cp04.yml`, `tests/e2e/run.sh`.
- **Qué hacer:** levantar el e2e con un catálogo de prueba con `fire.ignition_chance_ppm` alto (sin tocar `catalog/catalog.json`), sembrar plantas, esperar a que se sequen y ardan, y correr `bots firefighter`. Comprobar: el fuego se apaga una vez, hay un solo bono, y nada se pierde.
- **Aceptación y resultado:** CUMPLE (`tests/e2e/cp04-firefighter.test.mjs` en verde; `run.sh` lo corre al final, con un stack nuevo y el catálogo de prueba). Valores de **una ejecución** del autor del caso (14 oct 2026, `[DATO: máquina de esa ejecución]`), no una cifra fija: con `ignition_chance_ppm = 500.000` en `catalog.test-fire.json`, la casilla sembrada en (10, 10) ardió en el tick 933 (el tick de ignición varía de una ejecución a otra: depende del azar y del momento en que arranca el valle). 3 bots en 20 s vieron 1 incendio y lo apagaron en 1 ronda (3 gotas enviadas, 3 aplicadas, 75 de humedad aportada); el bono (5 hojas en esa ejecución) lo recibió un único bot, 0 consumidos y 0 sin apagar. El caso exige un incendio visto y apagado y exactamente un bot con el bono; no exige el tick ni la cantidad de gotas.

#### A10 · Prueba de carga en el CI y RNF-03

- **Por qué:** CP-11 se midió una vez en un Mac (119,3 gotas/s, p95 99,3 ms) y no corre en el CI. RNF-03 (el ranking no debe subir la latencia) no tiene prueba.
- **Archivos:** `.github/workflows/` (workflow manual con `workflow_dispatch`, no en cada PR), `src/bots`.
- **Qué hacer:**
  1. Workflow manual que levanta el sistema y corre `bots load-test` (30 bots, 60 s) y guarda el informe JSON como artefacto.
  2. Variante con alguien pidiendo `GET /api/ranking` en bucle durante la carga, para medir RNF-03.
- **Aceptación:** el informe dice gotas/s, p50, p95 e invariante; con y sin ranking. Las cifras van al documento (nunca inventadas; si el *runner* del CI es más lento que el laboratorio, se dice).

#### A11 · `GET /api/catalog`

- **Por qué:** está en la tabla de APIs externas del documento («Instalaciones, mejoras y cifras del juego») y no existe (`src/api/README.md`).
- **Archivos:** `src/api` (ruta, Swagger, pruebas), usando `CatalogService.GetCatalog`.
- **Aceptación:** devuelve las cifras de `catalog/catalog.json` tal como las sirve la persistencia, u64 como texto, con caché corta. El frontend puede usarlo para mostrar precios y textos.

#### A12 · Patrones de la nube identificados en el código

- **Por qué:** plan de la semana 5 y requisito del informe (al menos 5 patrones; sección «Los cinco patrones de la nube» del documento).
- **Qué hacer:** para cada patrón (Circuit Breaker, Publisher-Subscriber, Rate Limiting, Gatekeeper, Health Endpoint Monitoring, Retry…), anotar dónde está en el código (archivo y función), qué prueba lo demuestra y qué cifra medida lo respalda. Va a un documento nuevo `docs/patrones.md` que luego alimenta el informe (C1).
- **Aceptación:** cada patrón con archivo, prueba y evidencia; nada sin respaldo.

#### A13 · Pendientes pequeños

Cada uno es un PR pequeño:

- **CP-08:** en `tests/e2e/faults-startup.test.mjs` el comentario dice que el acceso se pide una vez, pero se reintenta cada 3 s (puede tocar el límite de 10 por minuto). Corregir comentario o espera; ajustar la ventana de `RestartCount`.
- **RNF-10:** `run.sh` mide el arranque pero no lo exige (< 5 min). Medirlo desde cero (sin caché) y exigirlo.
- **Frontend:** al reconectar, mostrar las acciones reenviadas como «resultado desconocido» hasta su resultado final; el botón **Actualizar** del ranking debe respetar `Retry-After` tras un 429.
- **`/api/catalog` (aviso de la revisión de A11):** con la persistencia caída y la copia vencida, cada refresco espera hasta unos 3 s antes de servir la copia vieja (`src/api/src/catalog-cache.ts`). Mejor servir la copia al instante y refrescar en segundo plano, o recordar el fallo unos segundos.
- **README raíz:** dice React + Phaser; aclarar el estado hasta que A2/A3 estén.
- **Reglas del valle:** `odd/tasks/valley-rules.md` (T5) comprobada en navegador con el frontend React + Phaser (A2/A3/A4) y cerrada con [x].

---

### 9. Bloque B · Nivel 2 (semana 6, en este orden)

#### B1 · Despliegue repartido en el laboratorio (plan A)

- **Por qué:** primero del nivel 2 y objetivo real del proyecto (RES-07). El profesor apagará una máquina en la defensa.
- **Depende de:** A1 (imágenes en ghcr.io) y de que el encargado del laboratorio cree la regla del cortafuegos (entrada TCP 8080, 8081, 8082, 50051 y 5433 desde `10.68.17.0/24`).
- **Archivos:** `deploy/nodes/<nodo>.yml` (`db.yml`, `persistence.yml`, `world.yml`, `hub.yml`, `proxy.yml`), `deploy/nodes/lab-addresses.example` y `secrets.example`, `deploy/nodes/simulate.sh` (simulación del plan A en una máquina), `scripts/lab/` (PowerShell: diagnóstico, despliegue y salud), `deploy/nodes/README.md` (guía del día del laboratorio).
- **Qué hacer:**
  1. Un Compose por máquina con las **mismas imágenes**; solo cambian direcciones y puertos (`10.68.17.x:puerto`). Incluir `HUB_STATS_ADDR` para la API.
  2. Script de diagnóstico: comprueba que cada máquina ve los puertos de las demás antes de levantar nada.
  3. Script de despliegue por máquina (descarga, levanta, comprueba salud). Que funcione en Windows con Docker Desktop sobre WSL y una cuenta no administradora.
  4. Repetir en el laboratorio los casos de fallos «a mano» con el panel del sistema y apagando máquinas de verdad; anotar las cifras en «Resultados medidos» del documento (sustituye su `[DATO: resultados en las máquinas del laboratorio]`).
- **Aceptación:** el juego funciona repartido en las máquinas; el panel muestra los cinco servicios en verde; al apagar la máquina de Python se sigue jugando.

#### B2 · Chat (Go)

- **Por qué:** RF-13, RF-14, RF-15 y CP-09. El contrato ya existe (`proto/hydrovalley/chat/v1/chat.proto`: `SendMessage`, `ListMessages` con número de secuencia). nginx devuelve 503 en `/chat`.
- **Archivos:** `src/chat/` (estructura `cmd/` + `internal/`, como el hub), `deploy/compose.yml`, `deploy/nginx/` (ruta `/chat`), frontend (panel de chat), `ci.yml` (job nuevo), `tests/e2e/` (CP-09).
- **Qué hacer:** servicio gRPC con número de secuencia creciente (el chat es el único que lo asigna: es el reloj lógico del chat); WebSocket `/chat` para la entrega en vivo; el `username` sale del token; `SendMessage` idempotente con `message_id`. Si el chat se cae, el juego sigue (RF-15): añadirlo a los casos de fallos.
- **Aceptación:** CP-09 (dos jugadores envían a la vez y ven el mismo orden) y RF-15 automatizados.

#### B3 · Propagación del fuego

- **Por qué:** nivel 2; hoy el fuego no salta a las casillas vecinas.
- **Archivos:** `src/world/src/domain/` (reglas y pruebas), `catalog/catalog.json` (cifras nuevas), documento (D-01, con las cifras propuestas por el equipo).
- **Qué hacer:** el fuego salta a vecinas con una probabilidad por tick definida en el catálogo; solo con el generador con semilla (regla 6), así la misma semilla da el mismo valle (CP-10).
- **Aceptación:** pruebas de dominio deterministas; CP-10 sigue pasando; las cifras nuevas en D-01.
- **Estado:** hecha (Cristian Baczek; integrada y corregida por Claude). Cifra de catálogo `fire.propagation_chance_ppm = 2000` (0,2 % por tick, unos 2 % por segundo; valor inicial elegido por el equipo). Un sorteo del generador con semilla por casilla elegible con vecina ardiendo, por filas y antes de la ignición, con la 4-vecindad ortogonal y solo desde los fuegos que ardían al empezar el paso (sin cascadas dentro de un tick). Está también en `world-go` (misma regla, conformidad byte a byte con el guion `08-propagacion`). La elegibilidad es la de siempre (planta, humedad 0, sin fuego); no se dejaron ganchos para biomas u obras (RF-26): cuando B4 los necesite, se añaden con su regla y sus pruebas.

#### B4 · Obras comunitarias (RF-26)

- **Por qué:** nivel 2; RF-26 sin implementar.
- **Qué hacer:** leer RF-26 en el documento y proponer el diseño (contrato en `world.proto` y `play.proto`, reglas en Rust, panel en el frontend) en un PR de diseño antes de programar.

#### B5 · Tick en Go (`world-go`) y comparativa Go vs. Rust

- **Por qué:** D-03 y la sección «Riesgos»: la versión en Go del tick sirve para la comparativa y **como plan B** si Rust falla. `src/world-go/` y `tests/bench/` están vacíos.
- **Archivos:** `src/world-go/` (implementa `WorldService.Step` y `GetRanking` con el mismo contrato), `tests/bench/` (comparativa), `ci.yml`.
- **Qué hacer:**
  1. Implementar el paso del autómata en Go con el mismo contrato y, en lo posible, las mismas reglas (el documento admite una versión simplificada).
  2. Banco de pruebas que mide el tiempo de `Step` de las dos versiones con el mismo valle y la misma carga; un gráfico para el informe.
- **Aceptación:** `world-go` puede sustituir a Rust en `compose.yml` (el hub no cambia); la comparativa con cifras medidas.

#### B6 · Resumen al volver

- **Por qué:** nivel 2 (escena del documento «Al día siguiente»).
- **Qué hacer:** al volver, el jugador ve qué pasó desde su última conexión (instalaciones que trabajaron, incendios, hojas ganadas). Proponer el diseño (qué calcula Rust, qué guarda la persistencia, qué muestra el frontend) antes de programar.
- **Estado:** hecha (Alfonso José Palma Alvarado, integrada por Claude). Rust y `world-go` acumulan, por jugador, lo que hicieron sus aspersores desde su última acción aplicada (casillas crecidas, hojas, agua reutilizada, fuegos apagados) y lo borra su siguiente acción; va en `PlayerRecord` (campo 16, solo si no está vacío) y en `PlayerState`. El hub lo manda en el primer `state` de cada conexión (`OfflineSummaryView`) y el frontend lo muestra en un panel descartable. La persistencia guarda además los acumulados de la época (`fires_extinguished`, `tiles_grown`, u64) para los reportes. El goteo y la planta no cuentan (D-01). Pruebas: guion de conformidad `09-resumen-offline` y `tests/e2e/offline-summary.test.mjs`.

#### B7 · Prometheus y Grafana

- **Por qué:** nivel 2; el documento lo plantea como el panel profesional.
- **Archivos:** `deploy/` (servicios bajo un perfil, como `bots`), métricas en el hub (ya existen en `/stats`), API y persistencia.
- **Qué hacer:** exponer `/metrics` (el hub ya calcula las cifras en `internal/metrics`), Prometheus que las recoge y un tablero de Grafana versionado como archivo.
- **Aceptación:** el tablero muestra lo mismo que `/sistema.html` y más historia.
- **Estado:** hecha (Cristian Baczek, integrada por Claude). Hub y API exponen `GET /metrics`; `compose.yml` añade `prometheus` y `grafana` bajo el perfil `monitoring` (puertos solo en `127.0.0.1`, Grafana anónimo de solo lectura); los destinos de scrape se configuran con `HUB_METRICS_ADDR` y `API_METRICS_ADDR` (plan A). El simulador y la persistencia no tienen `/metrics` propio: se vigilan por las sondas de la API (`hydrovalley_service_status`). Ficha: `odd/tasks/b7-prometheus-grafana.md`.

#### B8 · Mantener el juego con una máquina apagada (orquestación)

- **Por qué:** el profesor sugirió la orquestación de contenedores. Hoy, si se apaga la máquina del hub, de nginx o de Rust, el juego se detiene hasta que vuelve (ver «Resultados medidos» en el documento).
- **Depende de:** B1 (plan A funcionando) y, para Rust, B5 (ambas listas).
- **Decisión aprobada (`docs/decisiones/b8-orquestacion.md`):**
  - **Topología (6 máquinas):** las 5 PCs del Plan A (`10.68.17.3` `db`, `10.68.17.9` `persistence`, `10.68.17.10` `world`, `10.68.17.12` `hub`, `10.68.17.11` `proxy`) más una **6.ª máquina física (`standby.yml`)** que aloja `proxy-standby` (`:8080`), `hub-standby` (`:8081`) y `world-standby` con `world-go` (`:50051`).
  - **Entrada (`E2`):** segundo nodo `proxy` con el mismo certificado TLS multi-SAN y la misma `TOKEN_PRIVATE_KEY`, conmutación automática en el frontend y los bots y `upstream` de nginx con `backup` hacia `hub-standby`.
  - **Simulador (`W2`):** `world-standby` corre `world-go` en modo `WORLD_MODE=standby` (carga la instantánea solo al recibir su primer `Step`, nunca al arrancar). Si la máquina de Rust (`10.68.17.10`) se despausa o vuelve, el campo `min_expected_tick` de `StepRequest` obliga a Rust a descartar su memoria vieja y recargar la instantánea escrita por `world-go`, permitiendo regresar de Go a Rust (*failback*) sin que el tick retroceda jamás.
  - **Hub (`H2`):** `hub-standby` vigila `/healthz` del primario y se promueve con `hub_generation = 2` en `StepRequest`. Si el hub primario se despausa (`docker unpause`), recibe `FAILED_PRECONDITION`, detiene su bucle, responde `503` en `/healthz` y cierra sus WebSockets con `1001`.
- **Desglose de implementación (`odd/tasks/b8-failover.md`):** `T1` (entrada doble y cliente con rotación automática), `T2` (simulador `standby` y regreso `world-go → Rust`), `T3` (hub `standby` y *fencing* con retirada al despausar), `T4` (`standby.yml`, `simulate.sh` y scripts del laboratorio), `T5` (casos e2e de failover y `unpause`), `T6` (documentación y diagramas 04 y 08). `T7` (`kind` multinodo) queda descartada por calendario.
- **Aceptación:** apagar o pausar la máquina de `world`, `hub` o `proxy` mantiene el juego en marcha ($\le 50\text{ ticks}$ perdidos), sin que el jugador tenga que cambiar de IP y sin que despausar la máquina original genere dos escritores ni retroceda el tick.

---

### 10. Bloque C · Informe y defensa (semana 7)

#### C1 · Informe

- **Qué hacer:** crear `docs/informe/` y seguir la sección «El informe» del documento (título, objetivos, dónde va cada cosa, cómo se redacta; máximo 20 páginas, al menos 5 patrones). Fuentes: el documento, `docs/patrones.md` (A12), «Resultados medidos», los diagramas y los `odd/tasks/*.md` (evidencia de cada funcionalidad).
- **Regla:** cada cifra del informe sale de una medición registrada. Nada inventado.

#### C2 · Ensayo de la defensa y video de respaldo

- **Qué hacer:** ensayar el guion de seis pasos del documento («El guion de la defensa») en el laboratorio, al menos dos veces, cronometrado; grabar un video de respaldo; una lista de comprobación previa (imágenes descargadas, cortafuegos, claves, bots, panel abierto).
- **Depende de:** B1.

---

### 11. Decisiones pendientes del equipo

No son tareas de programación: hay que decidirlas juntos y anotarlas en el documento.

- **D1 · Visibilidad de las imágenes.** El repositorio es privado, así que los paquetes de ghcr.io también. Opciones: (a) hacer públicos solo los paquetes (el código sigue privado); (b) dejarlos privados y hacer `docker login ghcr.io` en el laboratorio con un token de solo lectura (`read:packages`), cerrando sesión al terminar porque las máquinas son compartidas.
- **D2 · Equivalencia litro ↔ humedad.** Sigue como `[DATO]` (`src/world/README.md`, `src/frontend/README.md`); el reporte muestra los contadores por separado hasta que se decida.
- **D3 · Día exacto de la entrega** (`[DATO]` en `CLAUDE.md` y en el documento), y quién integra cada semana (el rol rotativo de «El reparto»).
