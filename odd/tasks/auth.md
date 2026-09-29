# Acceso: registro, entrada y token firmado

## Objetivo

Sustituir el `player_id` de prueba del esqueleto por cuentas reales: registro, entrada y un token firmado que el hub verifica al abrir `/play` (RF-01, RF-02, RF-31, RNF-12, RNF-13; diagrama 09).

## Problema y por qué

Hoy cualquiera abre `/play?player_id=<uuid>` y actúa como otro jugador. Sin cuentas no hay RF-01 ni RF-02, y sin rol en el token no se pueden proteger los comandos de administración (RF-31).

## Alcance

- Persistencia: tabla `accounts` (diagrama 07) y `AccountService` (`Register`, `GetAccount`, `GetUsernames`); cuenta de administrador sembrada desde variables de entorno.
- API: `POST /api/register` y `POST /api/login`; Argon2id en la API (hash de relleno si el usuario no existe); token Ed25519 `{player_id, username, role, exp}` de 12 h; Swagger.
- Hub: verificación del token al abrir `/play` (firma, `exp`, rol) con la clave pública; se retira `player_id`; los comandos de administración reciben `forbidden` sin rol de administrador.
- Página: formulario de registro y entrada; guarda el token y abre `/play?token=…`.
- Despliegue y pruebas: claves en `deploy/.env.example`, compose y la prueba de extremo a extremo con registro → entrada → gota.

Fuera de alcance: TLS en nginx, `GetRanking`/`GetReport`, React y Phaser.

## Restricciones

- Las del `CLAUDE.md`: solo Python toca la base de datos; la contraseña nunca viaja por la red interna; claves por variables de entorno; nada de secretos en el repositorio.
- Contratos ya fijados en `proto/hydrovalley/persistence/v1/account.proto` y en el diagrama 09.

## Modo de trabajo

- TDD estricto (configuración de la sesión). Ejecutores: pytest (persistencia), vitest (API), `go test -race` (hub), `node --test` (página y e2e).
- Ruta: delegada (cada tarea toca 2 o más archivos no triviales).

## Tareas

- [x] T1 · Persistencia: migración de `accounts` y `AccountService` con sus pruebas; siembra del administrador. Evidencia: RED en la recolección; `uv run pytest` 113 passed / 21 skipped; con PostgreSQL 16 real 134 passed; `docker build` ok. Usuarios únicos sin distinguir mayúsculas (`lower(username)`).
- [x] T2 · API: registro, entrada, Argon2id y firma Ed25519, cliente gRPC de `AccountService`; pruebas y Swagger. Evidencia: RED (26 y luego 24 fallos), `npm test` 114 passed, lint/typecheck/build ok, `docker build` ok (también `linux/amd64`), e2e 3/3 con clave efímera. Rutas `/api/auth/register` y `/api/auth/login` según el documento; `compose.yml` exige `TOKEN_PRIVATE_KEY`. Commits `077e57e` (revisión 4 enfoques: aprobada), `37bf225` (endurecimiento: límites por IP, un salto de proxy, presupuesto de reintentos, TTL) y `a3520c5` (corrección acotada de un bloqueo global del límite de acceso detectado por la revisión; validador: aprobado). 133 pruebas.
- [x] T3 · Hub: verificación del token en `/play`, retirada de `player_id`, `forbidden` para comandos de administración sin rol; pruebas. Evidencia: RED por compilación; `go test -race` 9 paquetes ok; vet/staticcheck/gofmt limpios; actionlint ok; e2e 4/4 con registro y cierre 4401. `golang-jwt/jwt/v5` solo EdDSA; los comandos de administración no cuentan como gotas. Pendiente T4: la página aún usa `player_id`. Pendiente T5: que nginx no registre el token de la URL y añadir `aud`/`iss` al token. Commits `e0c1fe6` (revisión 4 enfoques: aprobada) y `6420850` (ajustes: una sola clasificación de gota, comando de administración desconocido sin cerrar la sesión; revisión aprobada).
- [x] T4 · Página: registro y entrada, token guardado, conexión con `token`; pruebas. Evidencia: RED sin `js/auth.js`; `npm test` 45 passed; e2e 4/4; prueba con script contra el stack real (registro, 409, 401, entrada, WebSocket y gota `applied`). Sin probar en un navegador real el cableado del DOM. Pendiente: reenviar las acciones pendientes al reconectar.
- [ ] T5 · Despliegue y e2e: variables y claves de ejemplo, compose, prueba de extremo a extremo con cuentas.

## Criterios de aceptación

- Registro y entrada funcionan de punta a punta; la gota llega con el `player_id` del token.
- Sin token, con token caducado o con firma inválida, `/play` se cierra (código 4401 del diagrama 09).
- Un token sin rol de administrador recibe `forbidden` para `set_demo_mode` y `reset_valley`.
- Ninguna contraseña ni hash viaja al navegador; ningún secreto en el repositorio.
- CI verde en los ocho jobs.

## Entrega

- Previsión: más de 400 líneas; se entrega en PR encadenados por tarea.
- Estrategia: `ask-on-risk` → `stacked-to-main` (elegida por el usuario): cada tarea en su rama desde `dev` (`feature/auth-<tarea>`) y su PR contra `dev`, fusionado antes de la siguiente.

## Progreso

- T1 en `feature/auth-persistence` (ruta delegada: migración, servicio, pruebas y README en varios archivos). Commits `9807910` (revisión de 4 enfoques: aprobada) y `884d483` (ajustes de la revisión: detalles de error fijos, prueba de siembra; evaluación: riesgo medio, `under_budget`, pendiente en el tramo). Unitarias 122 passed; con PostgreSQL real 143 passed.
