# hydrovalley

Juego incremental cooperativo y distribuido sobre las 3R del agua (reducir, reutilizar y reciclar), inspirado en la mecánica de *Drop and Grow*. Proyecto de Sistemas Distribuidos y Computación en la Nube, UCAB. Equipo de 4 personas.

## Fuente de verdad

- `docs/documento-inicial.md` contiene los requerimientos, las decisiones y el plan completo. Es una copia exportada del documento vivo del equipo y se actualiza al cierre de cada semana.
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
2. **Idempotencia.** Toda acción que modifica algo lleva `action_id`. Go descarta los repetidos y Rust recuerda los últimos aplicados por jugador.
3. **El tick es el reloj lógico.** Nunca se ordenan eventos comparando relojes de máquinas distintas.
4. **Sin streams de gRPC.** Todas las llamadas internas son de pregunta y respuesta.
5. **Solo Python toca la base de datos.** Nadie más tiene credenciales de PostgreSQL.
6. **El azar solo lo decide Rust**, con un generador con semilla cuyo estado se guarda en la instantánea. Misma semilla + mismos comandos = mismo valle.
7. **Enteros de 64 bits sin signo**, nunca decimales, para agua, hojas y humedad. En JSON, los números grandes viajan como texto.
8. **Instantánea del mundo cada 5 s** de Rust a Python; nada de escribir en la base de datos con cada gota.

## Convenciones

- Nombres en inglés en el código y en los `.proto`; comentarios y documentación en español.
- Paquetes gRPC `hydrovalley.<servicio>.v1`, en `proto/hydrovalley/<servicio>/v1/` (guía de estilo de Buf).
- Identificadores: UUID en texto. Fechas: ISO 8601 en UTC. Tiempos: segundos.
- Errores: códigos estándar de gRPC.
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
  tests/                 e2e/ (casos CP-01 a CP-11) y bench/ (comparativa Go vs. Rust)
  scripts/               Diagnóstico del laboratorio y despliegue
  docs/                  documento-inicial.md, diagrams/, informe
```

## Ramas

- `feature/…` y `fix/…` salen de `dev` y vuelven a `dev` por pull request, con el CI en verde.
- `dev` se fusiona en `main` solo cuando hay una versión lista: el sistema completo levanta con Docker Compose, el CI está en verde y pasan los casos de prueba correspondientes. Cada fusión lleva una etiqueta de versión (`v0.1`, `v0.2`…).
- Los ensayos de despliegue en el laboratorio usan la imagen con la etiqueta `dev`; la defensa y la entrega final usan una versión etiquetada de `main`.
- Reglas activas en GitHub sobre `main` y `dev`: pull request obligatorio sin aprobaciones exigidas, comprobaciones del CI obligatorias (en cuanto exista el primer workflow) y force push y borrado bloqueados.

## Entorno de despliegue (laboratorio)

- Máquinas LABBD-01 (`10.68.17.1`) y LABBD-02 (`10.68.17.2`), misma subred. Windows 10 Pro, x86_64, i7-13700F (16 núcleos, 24 hilos) y 31,7 GB de RAM.
- Docker Desktop 29.5.3 con WSL 2. Cuenta genérica del dominio, **no administradora**: ejecuta contenedores y descarga imágenes amd64, pero no instala nada ni abre puertos.
- Las imágenes las construye el pipeline para `linux/amd64`; no se construyen en un Mac con chip Apple para desplegarlas.
- Puertos publicados en el host: proxy **8080** (el 80 lo ocupa Windows), gRPC **50051**, PostgreSQL en contenedor **5433** (el 5432 lo ocupa un PostgreSQL 16 nativo del laboratorio).
- El cortafuegos bloquea las conexiones entrantes entre máquinas hasta que el encargado cree la regla. Nunca se da por hecha la conectividad entre máquinas. Mientras tanto, plan B: todo en una sola máquina con Docker Compose.
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
7. `07-entidad-relacion.puml` (al empezar la persistencia)
8. `08-despliegue.puml`

Los nombres de actores, casos de uso, servicios y métodos deben coincidir con los RF del documento y con los `.proto`. Si el código cambia el diseño, el diagrama se actualiza en el mismo pull request. No se hacen diagramas de clases por servicio: el único de clases es el modelo de dominio del simulador.
