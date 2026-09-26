# hydrovalley

Juego incremental cooperativo y distribuido sobre las 3R del agua (reducir, reutilizar y reciclar), inspirado en la mecánica de *Drop and Grow*. Proyecto de Sistemas Distribuidos y Computación en la Nube, UCAB. Equipo de 4 personas.

## Fuente de verdad

- `docs/documento-inicial.md` contiene los requerimientos, las decisiones y el plan completo. Es la fuente de verdad: se edita en el repositorio, por pull request, y ningún otro documento la sustituye.
- Si el código o un diagrama contradice ese documento, no decidas por tu cuenta: señálalo en la descripción del pull request.
- Nunca inventes cifras, citas ni resultados de pruebas. Si falta un dato, deja `[DATO: …]`; si falta una fuente, `[FUENTE: …]`.

## Qué se construye

Un valle isométrico de 32 × 32 casillas, compartido por todos los jugadores y simulado en el servidor. Los jugadores sueltan gotas (1 L de su depósito) sobre casillas; con suficiente humedad la casilla sube de etapa (tierra seca → pasto → arbusto → flores → árbol) y quien la hizo crecer gana hojas. Con las hojas se compran instalaciones de las 3R que trabajan solas: aspersor de agua gris (Reutilizar), riego por goteo (Reducir) y planta de tratamiento (Reciclar). Las casillas se evaporan y los incendios aparecen al azar en casillas secas con planta.

Las reglas y las cifras de arranque están en la sección D-01 del documento. Las cifras viven en `catalog/` y se ajustan sin tocar código.

## Arquitectura

| Servicio | Lenguaje | Carpeta | Responsabilidad |
| --- | --- | --- | --- |
| Proxy de entrada | nginx | `deploy/` | Única puerta: `/api` → API, `/play` → hub, `/chat` → chat |
| API | TypeScript | `src/api/` | Registro, acceso (token firmado), catálogo, ranking y reportes. REST documentado con Swagger |
| Frontend | TypeScript (React + Phaser) | `src/frontend/` | React para los paneles; Phaser para el valle isométrico |
| Hub en tiempo real | Go | `src/hub/` | Una goroutine por jugador (WebSocket). Filtra sesión, `action_id` y límite de 15 gotas/s. Marca el tick cada 100 ms y difunde el estado 1 vez por segundo |
| Chat | Go | `src/chat/` | Mensajes entre jugadores, ordenados con reloj lógico. Nivel 2 |
| Simulador del valle | Rust | `src/world/` | Único escritor del estado del mundo. Aplica los comandos del tick en orden y avanza el autómata celular |
| Tick en Go | Go | `src/world-go/` | Versión del tick en Go para la comparativa y como plan B |
| Persistencia | Python | `src/persistence/` | Única puerta a PostgreSQL: cuentas, catálogo, instantáneas y reportes |
| Bots | Go | `src/bots/` | Vecinos, prueba de carga, tramposo y bombero |

Frontera Go/Rust: **Go decide si un comando es admisible; Rust decide qué produce.** Go nunca calcula el mundo; Rust nunca toca una conexión de jugador.

## Reglas que no se rompen

1. **Escritor único.** Solo el simulador en Rust modifica el estado del mundo. Go lo llama con `WorldService.Step` una vez por tick, nunca dos a la vez.
2. **Idempotencia.** Toda acción que modifica algo lleva `action_id`. Go lleva dos conjuntos por sesión: `pending` (en cola o en curso; un reenvío se ignora) y `confirmed` (aplicados, con su tick, durante 60 s; un reenvío recibe `duplicate`). Rust recuerda, por jugador, los aplicados en los últimos 600 ticks; ese conjunto va dentro de la instantánea y es la autoridad final. `Step` devuelve el resultado de cada comando.
   - Un solo reloj para los plazos, el del hub: cada acción admitida recibe exactamente un resultado final, `applied | duplicate | rejected | expired` (`expired`: más de 30 s en la cola desde que el hub la admitió). Los rechazos inmediatos del hub son `duplicate | rate_limited | paused | forbidden`. El cliente trata `duplicate` como `applied`; muestra la acción como pendiente hasta el resultado final y solo reenvía las pendientes al reconectarse.
   - Si Rust cae, el hub pausa el valle (no llama a `Step`), devuelve los comandos del `Step` fallido al frente de la cola (siguen en `pending`) y encola hasta 5.000; los nuevos que pasen de ahí reciben `paused`. Cuando Rust vuelve, su primera respuesta trae `restored_tick` y el hub quita de `confirmed` las entradas con tick mayor; luego drena la cola en lotes de 500 por tick y responde `expired` a los de más de 30 s.
   - El hub cuenta las gotas aceptadas solo a partir de los resultados `applied` de `Step`.
3. **El tick es el reloj lógico.** Nunca se ordenan eventos comparando relojes de máquinas distintas.
4. **Sin streams de gRPC.** Todas las llamadas internas son de pregunta y respuesta. El chat usa `SendMessage`/`ListMessages` con un número de secuencia; la entrega en vivo va por el WebSocket del chat.
5. **Solo Python toca la base de datos.** Nadie más tiene credenciales de PostgreSQL.
6. **El azar solo lo decide Rust**, con un generador con semilla cuyo estado se guarda en la instantánea. Misma semilla + mismos comandos = mismo valle.
7. **Enteros de 64 bits sin signo**, nunca decimales, para agua, hojas y humedad. En JSON, los números grandes viajan como texto.
8. **Instantánea del mundo cada 50 ticks (5 s)** de Rust a Python (`tick % 50 == 0`); nada de escribir en la base de datos con cada gota. Rust copia el mundo y envía `SaveSnapshot` desde una tarea en segundo plano: `Step` nunca espera a la persistencia. Como mucho un guardado en curso (si el anterior sigue, esa instantánea se salta); si falla, se registra y se reintenta en el siguiente tick de instantánea, y el juego sigue. Un fallo de la persistencia nunca abre el *Circuit Breaker* del hub, que solo vigila `Step`. Rust carga el catálogo con `CatalogService.GetCatalog` al arrancar y al reiniciar el valle.

## Convenciones

- Nombres en inglés en el código y en los `.proto`; comentarios y documentación en español.
- Paquetes gRPC `hydrovalley.<servicio>.v1`, en `proto/hydrovalley/<servicio>/v1/` (guía de estilo de Buf).
- Identificadores: UUID en texto. Fechas: ISO 8601 en UTC. Tiempos: segundos.
- Errores: códigos estándar de gRPC.
- Comandos de administración (modo demo, reiniciar valle; RF-31): por el WebSocket `/play`, solo con rol de administrador en el token, y entran en la cola del tick como cualquier comando. Reiniciar el valle no reinicia el tick.
- Token: Ed25519, `{player_id, role, exp}`, 12 h. Firma la API (clave privada); el hub verifica con la pública al abrir `/play`. Claves por variables de entorno.
- Contraseñas: TLS termina en nginx; la API calcula y verifica el hash Argon2id; la persistencia solo guarda el hash. El gRPC interno va sin cifrar dentro de la red del laboratorio.
- Coordenadas: casilla `(x, y)` desde la esquina superior izquierda, empezando en 0. El servidor no sabe cómo se dibuja el valle.
- Dentro de cada servicio, la estructura oficial de su lenguaje: Go con `cmd/` + `internal/`; Rust con la lógica en `src/lib.rs` y el servidor en `src/main.rs`.
- Saltos de línea LF en todo el repositorio (`.gitattributes`). El laboratorio usa Windows.

## Estructura del repositorio

```
hydrovalley/
  CLAUDE.md, README.md, .gitattributes, .gitignore
  .github/workflows/     Un workflow por servicio, con filtro por ruta
  proto/hydrovalley/     Contratos: world/v1, persistence/v1, chat/v1
  src/                   api, frontend, hub, chat, world, world-go, persistence, bots
  catalog/               Cifras del juego (datos, no código)
  deploy/                docker-compose.yml y nginx
  tests/                 e2e/ (casos CP-01 a CP-12) y bench/ (comparativa Go vs. Rust)
  scripts/               Diagnóstico del laboratorio y despliegue
  docs/                  documento-inicial.md, diagrams/, informe
```

## Ramas

- `feature/…` y `fix/…` salen de `dev` y vuelven a `dev` por pull request, con el CI en verde.
- `dev` se fusiona en `main` solo cuando hay una versión lista: el sistema completo levanta con Docker Compose, el CI está en verde y pasan los casos de prueba correspondientes. Cada fusión lleva una etiqueta de versión (`v0.1`, `v0.2`…).
- El pipeline publica las imágenes en `ghcr.io` (`linux/amd64`): `dev` al fusionar en `dev`, `vX.Y` al etiquetar `main`. Los ensayos de despliegue en el laboratorio usan la imagen con la etiqueta `dev`; la defensa y la entrega final usan una versión etiquetada de `main`.
- Reglas activas en GitHub sobre `main` y `dev`: pull request obligatorio sin aprobaciones exigidas, comprobaciones del CI obligatorias (en cuanto exista el primer workflow) y force push y borrado bloqueados.

## Portabilidad del despliegue (plan A y plan B)

Se desarrolla con la topología del plan B (una máquina, un contenedor por servicio) y se despliega también en el plan A (cinco máquinas). Pasar de uno a otro es solo configuración, nunca código:

1. Ninguna dirección en el código: cada servicio lee de variables de entorno a quién llama (`WORLD_ADDR`, `PERSISTENCE_ADDR`, `CHAT_ADDR`…). Plan B: `world:50051`; plan A: `10.68.17.x:50051`.
2. Nunca `localhost` entre servicios.
3. Sin carpetas compartidas entre contenedores: lo compartido viaja por gRPC o por la base de datos.
4. Toda llamada gRPC con tiempo límite y reintentos acotados.
5. Cada servicio arranca aunque los demás no estén y se reconecta solo (RNF-09, CP-08); no depender de `depends_on`.
6. Un archivo base y variaciones: `deploy/compose.yml` para el plan B y `deploy/nodes/<nodo>.yml` para cada máquina del plan A, con las mismas imágenes.

Sin Docker Swarm: pide más puertos en el cortafuegos y es frágil con Docker Desktop sobre WSL.

## Entorno de despliegue (laboratorio)

- Máquinas LABBD-01 (`10.68.17.1`) y LABBD-02 (`10.68.17.2`), misma subred. Windows 10 Pro, x86_64, i7-13700F (16 núcleos, 24 hilos) y 31,7 GB de RAM.
- Docker Desktop 29.5.3 con WSL 2. Cuenta genérica del dominio, **no administradora**: ejecuta contenedores y descarga imágenes amd64, pero no instala nada ni abre puertos.
- Las imágenes las construye el pipeline para `linux/amd64`; no se construyen en un Mac con chip Apple para desplegarlas.
- Puertos publicados en el despliegue repartido: nodo TypeScript **8080** (nginx con HTTPS; el 80 lo ocupa Windows); nodo Go **8081** (WebSocket del hub), **8082** (WebSocket del chat) y **50051** (gRPC del chat); nodos Rust y Python **50051**; nodo de base de datos **5433** (el 5432 lo ocupa un PostgreSQL 16 nativo del laboratorio). En el plan B solo se publica el 8080.
- El cortafuegos bloquea las conexiones entrantes entre máquinas hasta que el encargado cree la regla (entrada TCP 8080, 8081, 8082, 50051 y 5433 desde `10.68.17.0/24`). Nunca se da por hecha la conectividad entre máquinas. Mientras tanto, plan B: todo en una sola máquina con Docker Compose.
- El objetivo sigue siendo el despliegue repartido en las máquinas del laboratorio (RES-07). Una sola máquina es el plan B, no el destino.

## Alcance

- **Nivel 1 (sí o sí):** valle con gotas, humedad, crecimiento, evaporación y marchitamiento; depósito; las tres instalaciones; dos mejoras (gota más grande y depósito mayor); incendios que aparecen y se apagan; límite de gotas; ranking; valle isométrico con salida de emergencia a vista cenital; instantáneas cada 5 s y recuperación tras reiniciar Rust; gRPC; proxy; bots; panel del sistema mínimo; Docker Compose; pipeline; diagramas UML.
- **Nivel 2 (si da tiempo, en orden):** despliegue repartido en el laboratorio, chat, propagación del fuego, obras comunitarias, comparativa Go vs. Rust, resumen al volver, Prometheus y Grafana, más contenido.
- Toda idea nueva dice en qué nivel entra y qué desplaza.

## Calendario

La entrega es en la semana 7 del curso. La semana 3 empieza el lunes 28 de septiembre de 2026.

| Semana | Fechas | Objetivo |
| --- | --- | --- |
| 3 | 28 sep – 2 oct | Cerrar y esqueletar |
| 4 | 5 – 9 oct | Que cada pieza haga lo suyo |
| 5 | 12 – 16 oct | Integrar de verdad y endurecer. Se cierra el nivel 1 |
| 6 | 19 – 23 oct | Nivel 2 y ensayo en el laboratorio |
| 7 | 26 – 30 oct | Informe y defensa. Entrega: `[DATO: día exacto de la entrega]` |

## Diagramas

Se diseña antes de programar. Los diagramas van en PlantUML, en `docs/diagrams/`, numerados:

1. `01-casos-de-uso.puml`
2. `02-componentes.puml` (notación lollipop, obligatoria)
3. `03-secuencia-gota.puml`
4. `04-secuencia-reinicio-rust.puml`
5. `05-estados-casilla.puml`
6. `06-dominio-simulador.puml`
7. `07-entidad-relacion.puml`
8. `08-despliegue.puml`
9. `09-secuencia-acceso.puml`

Los nombres de actores, casos de uso, servicios y métodos deben coincidir con los RF del documento y con los `.proto`. Si el código cambia el diseño, el diagrama se actualiza en el mismo pull request. No se hacen diagramas de clases por servicio: el único de clases es el modelo de dominio del simulador.

Cada `.puml` lleva su `.svg` generado al lado. Los SVG van en un commit aparte, después del commit de los `.puml`, y no pasan por la revisión automática: lo que se revisa es el `.puml`. Se generan con:

```
docker run --rm -v "$PWD/docs/diagrams:/data" plantuml/plantuml:1.2026.8 -tsvg '/data/*.puml'
```
