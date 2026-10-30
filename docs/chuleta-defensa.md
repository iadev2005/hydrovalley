# Chuleta para la defensa

Respuestas cortas a las preguntas del profesor, con el contexto justo para defenderlas. Cada respuesta dice **dónde está la prueba** en el repositorio, por si piden enseñarla. Si una respuesta cambia, se corrige aquí y en `docs/documento-inicial.md`, que es la fuente de verdad.

---

## 1. ¿Qué patrones de diseño usaron? (piden 5)

Hay 9 documentados en `docs/patrones.md`, cada uno con su archivo y su prueba. Los **5 más fáciles de demostrar en vivo**:

| Patrón | Qué hace en hydrovalley | Cómo se ve en la demo |
| --- | --- | --- |
| **Circuit Breaker** | Si el simulador (Rust) no responde 3 veces seguidas, el hub deja de llamarlo, pausa el valle y lo vuelve a probar cada segundo | Apagar la PC de `world`: el panel del sistema lo marca y el juego se pausa sin colgarse |
| **Publisher-Subscriber** | El hub publica el estado del valle una vez por segundo a todos los jugadores conectados; el chat hace lo mismo con los mensajes | Dos navegadores ven el mismo valle y el mismo chat a la vez |
| **Health Endpoint Monitoring** | Cada servicio expone su salud (`/healthz`, `/readyz`, salud de gRPC) y el panel `/sistema.html` las reúne | Abrir `/sistema.html` y ver los servicios en verde o en rojo |
| **Gateway Routing** (puerta de entrada única) | nginx es la única puerta: termina HTTPS y reparte `/api` → API, `/play` → hub y `/chat` → chat | El navegador solo conoce `https://<proxy>:8080` |
| **Rate Limiting** | Máximo 15 gotas por segundo por jugador; más allá, el hub responde `rate_limited` | El bot «tramposo» (CP-03) lo intenta y se le frena |

Otros cuatro, por si preguntan más: **Queue-Based Load Leveling** (las acciones entran a una cola y se aplican al ritmo del tick), **Retry con espera exponencial** (reintentos acotados en llamadas idempotentes), **Gatekeeper** (el hub valida token, rol y formato antes de tocar el mundo) e **Idempotent Consumer** (cada acción lleva un `action_id` y nunca se aplica dos veces).

> **Ojo:** «base de datos por servicio» no es exacto. Tenemos **una sola** base de datos y **un solo servicio dueño** de ella (la persistencia en Python). Si lo nombran, la respuesta es: «Una sola base de datos con un único servicio que la toca; los demás le piden los datos por gRPC».

## 2. ¿Qué bases de datos usan?

**Una sola: PostgreSQL 16 (relacional)**, en su propio nodo (puerto 5433). Solo la persistencia en Python tiene credenciales. Guarda:

- las **cuentas** (usuario y hash Argon2id de la contraseña, nunca la contraseña),
- el **catálogo** de cifras del juego,
- las **instantáneas del valle**, una cada 5 s; los reportes y el ranking salen de ahí.

No hay una segunda base de datos no relacional. Si preguntan por otros sitios donde se guardan cosas:

- el **chat** guarda los últimos 200 mensajes **en memoria**, a propósito: es nivel 2 y el juego tiene que sobrevivir sin él;
- **Prometheus** guarda las métricas de monitoreo (series de tiempo), no datos del juego.

Por qué no se escribe en la base de datos con cada gota: con decenas de jugadores la ahogaría. El mundo vive en la memoria del simulador y se guarda en segundo plano cada 5 s.

## 3. ¿Qué implementan en Rust?

**El simulador del valle** (`src/world/`). Es el **único que puede cambiar el mundo**:

- recibe del hub, cada 100 ms (un *tick*), las acciones de los jugadores y las aplica en orden;
- avanza el valle: humedad, crecimiento, evaporación, marchitamiento, incendios y su propagación, basura, instalaciones;
- decide el azar con un generador con semilla: la misma semilla y las mismas acciones dan el mismo valle;
- guarda una instantánea cada 5 s en segundo plano y calcula el ranking.

**No** está en Rust el login ni el registro (eso es la API en TypeScript con la persistencia en Python), ni hay «perfiles» o «retos».

Por qué Rust: es la parte que hace cálculo de verdad en cada tick (1.024 casillas, 10 veces por segundo) y donde un error de memoria o de concurrencia sería más caro.

## 4. ¿Qué implementan en Go?

- **El hub** (`src/hub/`): una goroutine por jugador conectado por WebSocket. Valida el token, filtra duplicados y el límite de 15 gotas/s, marca el tick cada 100 ms, llama al simulador y difunde el estado. También tiene el Circuit Breaker.
- **El chat** (`src/chat/`): mensajes entre jugadores, ordenados con un contador lógico (`seq`) que asigna el propio chat, así todos ven el mismo orden.
- **Los bots** (`src/bots/`): vecinos, prueba de carga, tramposo y bombero.
- **`world-go`** (`src/world-go/`): una versión del simulador en Go, con las mismas reglas byte a byte, que sirve de **respaldo** de Rust y para la comparativa de rendimiento.

No hay «sala» ni «carrera»: es un único valle compartido.

La frontera, en una frase: **Go decide si una acción es admisible; Rust decide qué produce.**

## 5. ¿Qué modos de gRPC usan?

gRPC tiene 4 modos: unario (pregunta y respuesta), *streaming* del servidor, *streaming* del cliente y bidireccional. **Usamos solo el unario, a propósito.**

- Todas las llamadas internas son de pregunta y respuesta, con tiempo límite y reintentos acotados (`Step`: 200 ms y sin reintento).
- Lo que va «en vivo» a los navegadores (estado del valle, mensajes del chat) va por **WebSocket**, no por streams de gRPC.
- Motivo: los streams eran la parte más difícil (sobre todo en Rust) y a 10 llamadas por segundo en una red local no aportaban nada. Las llamadas unarias son más fáciles de reintentar y de cortar con un tiempo límite, que es lo que necesita el Circuit Breaker.

Contratos en `proto/hydrovalley/` (`world`, `persistence`, `chat`).

## 6. ¿Qué ODS abordan?

> `[DATO: el documento inicial no declara los ODS. Confirmar con el equipo y añadirlo a docs/documento-inicial.md]`. Propuesta según lo que hace el juego:

- **ODS 6 · Agua limpia y saneamiento** (el principal): todo el juego gira en torno a usar bien el agua.
- **ODS 12 · Producción y consumo responsables**: las 3R del agua (reducir con el goteo, reutilizar con el aspersor de agua gris, reciclar con la planta de tratamiento) y la basura que hay que recoger.
- **ODS 15 · Vida de ecosistemas terrestres** (secundario): el valle crece, se seca y se quema si no se cuida.

## 7. ¿Usan contenedores? ¿Y orquestación?

- **Docker**: un contenedor por servicio (nginx, API, hub, chat, simulador, persistencia y PostgreSQL).
- **Docker Compose**: un archivo para todo en una máquina (`deploy/compose.yml`, plan B) y uno por máquina para el despliegue repartido (`deploy/nodes/*.yml`, plan A). Son las mismas imágenes; solo cambian las direcciones.
- **Imágenes**: las construye y publica el CI de GitHub Actions en `ghcr.io`. En el laboratorio no se compila nada.
- **Orquestador dedicado: no.** Se evaluaron y se descartaron:
  - **Docker Swarm**: pide más puertos y falla con Docker Desktop sobre WSL.
  - **Kubernetes (k3s)**: bloqueado por la red de WSL 2 entre las PC de Windows y por puertos cerrados.

  La tolerancia a fallos se resolvió con un nodo de respaldo (pregunta 10).

## 8. ¿En qué está hecho el frontend?

**TypeScript**, con **React** para los paneles y **Phaser** para el valle isométrico, compilado con **Vite** y servido por nginx. Si Phaser no carga, hay una **vista cenital de emergencia** (cuadrícula 2D) con la que se puede seguir jugando.

No es Python ni JavaScript plano: Python solo está en la persistencia.

Mapa de lenguajes completo, por si preguntan:

| Lenguaje | Servicio |
| --- | --- |
| TypeScript | API y frontend |
| Go | hub, chat, bots y `world-go` |
| Rust | simulador |
| Python | persistencia |

## 9. ¿Funciona todo bien?

Respuesta honesta:

- **Nivel 1 completo**: valle, gotas, crecimiento, fuego, instalaciones, mejoras, ranking, valle isométrico con salida de emergencia, instantáneas y recuperación, gRPC, proxy, bots, panel del sistema, Docker Compose, CI y diagramas.
- **Nivel 2 hecho**:
  - despliegue repartido en el laboratorio,
  - chat,
  - propagación del fuego,
  - comparativa Go vs. Rust,
  - resumen al volver,
  - Prometheus y Grafana,
  - nodo de respaldo.
- **No hecho**: obras comunitarias (B4, nivel 2).
- **Cómo lo sabemos**:
  - cada cambio pasa el CI (pruebas de cada servicio más las de punta a punta, casos CP-01 a CP-12);
  - en la predefensa el sistema aguantó que se apagaran nodos y el cortafuegos.
- **Límites conocidos**, dichos antes de que los encuentren:
  - el certificado es autofirmado (aviso del navegador);
  - el chat pierde el historial si se reinicia;
  - la base de datos no tiene réplica.

## 10. ¿Tienen respaldo basado en réplicas?

**Sí, con nodos en espera (*standby*), no con réplicas activas a la vez.** Una sexta máquina tiene un **proxy**, un **hub** y un **simulador** (`world-go`) de reserva (`deploy/nodes/standby.yml`, decisión en `docs/decisiones/b8-orquestacion.md`).

- **Por qué en espera y no activas:** el mundo tiene **un único escritor**. Dos simuladores cambiándolo a la vez lo romperían.
- **Si cae el simulador en Rust:**
  1. el hub lo detecta con el Circuit Breaker y pasa a `world-go`;
  2. `world-go` carga la última instantánea de PostgreSQL y el juego sigue.

  Se pierden como mucho los últimos 5 s (50 ticks). Medido en local: **1,31 s a 1,60 s** de corte.
- **Para que no haya dos hubs mandando a la vez:** cada hub lleva un número de generación (`hub_generation`) y el simulador rechaza al más viejo.
- **Lo que no tiene respaldo:** **PostgreSQL**. Si cae, los jugadores conectados **siguen jugando** (el mundo está en memoria); solo fallan los accesos nuevos y el guardado, que se reintenta solo cuando vuelve.

---

## Preguntas probables extra

- **¿Por qué hay un tick?** Es el reloj lógico del sistema: el orden de las acciones lo fija el tick, nunca la hora de máquinas distintas.
- **¿Cómo evitan que una gota cuente dos veces si se reenvía?** Cada acción lleva un `action_id`. El hub y el simulador recuerdan los ya aplicados y responden `duplicate`.
- **¿Qué pasa si apago la máquina X?**
  - `persistence` o `db`: se sigue jugando.
  - `world`: el valle se pausa y pasa al respaldo.
  - `hub`: los navegadores se reconectan solos (al hub de respaldo si está activo).
  - `proxy`: nadie entra hasta que vuelve o entra el proxy de respaldo.
- **¿Dónde están las contraseñas?** En ningún sitio. La API calcula el hash Argon2id y solo se guarda el hash. HTTPS termina en nginx.
