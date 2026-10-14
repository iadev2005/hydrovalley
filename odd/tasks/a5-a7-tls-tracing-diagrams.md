# A5, A6 y A7: TLS, identificador de petición y diagramas

## Objetivo

Cerrar tres tareas del nivel 1 de `docs/tareas-equipo.md`: A5 (TLS en nginx, RNF-12), A6 (identificador de petición de punta a punta, RNF-07) y A7 (diagramas al día, RES-05).

## Problema y por qué

- A5: nginx sirve HTTP; el documento dice que termina TLS en el 8080 con un certificado autofirmado en el laboratorio (sección de contraseñas, RNF-12).
- A6: ningún servicio propaga un identificador de petición; RNF-07 no está implementado.
- A7: los diagramas no se tocan desde el 29 de septiembre y no reflejan bots, panel, `/stats`, `restored_tick` ni el frontend nuevo.

## Alcance

- A5: certificado autofirmado generado al arrancar si no se monta uno (nunca en el repositorio ni dentro de la imagen publicada), HTTPS y WSS en el 8080, HTTP en el mismo puerto redirigido a HTTPS, salud interna sin TLS, e2e y bots por HTTPS.
- A6: `X-Request-Id` desde nginx, registrado y propagado por la API como metadato gRPC `x-request-id` a persistencia y simulador; en `/play`, el `action_id` como identificador.
- A7: diagramas 02, 03, 04 y 08 (y los que hagan falta) contra el código; SVG en commit aparte.

## Restricciones

- Ninguna clave privada en el repositorio ni en una imagen publicada en ghcr.io.
- El gRPC interno sigue sin cifrar (riesgo aceptado en `CLAUDE.md`).
- No tocar el frontend salvo lo imprescindible (lo lleva otra persona).

## Modo de trabajo

- TDD estricto donde haya lógica (bots, API, servicios); la configuración de nginx se verifica con la e2e.
- Ruta: delegada.

## Tareas

- [x] T1 · A5 TLS en nginx, e2e y bots por HTTPS. Evidencia: RED de compilación en bots; `go test -race` (bots) ok; frontend lint, typecheck y 195 pruebas tras el cambio del proxy de Vite; e2e por HTTPS y WSS: 15 + 6 (TLS) + 9 + 2 en verde. Certificado ECDSA P-256 autofirmado generado al arrancar (o montado de solo lectura), nunca en la imagen; HTTP en el 8080 redirige a HTTPS conservando ruta y consulta; TLS 1.2 y 1.3; salud por un puerto local sin TLS. La e2e verifica el certificado con `NODE_EXTRA_CA_CERTS` y los bots con `HV_CA_CERT`. Pendiente del usuario: copiar a `deploy/.env.example` el bloque de variables TLS de `deploy/README.md`.
- [x] T2 · A6 identificador de petición de punta a punta. Evidencia: RED en bots, API, persistencia, world y hub; hub y bots `go test -race`, vet, gofmt y staticcheck; API 290, lint y typecheck; persistencia 235; world 215 + 25 de integración, clippy y fmt; e2e 39/39 (smoke 15, TLS 8, identificador 5, fallos 9, arranque 2). Un mismo `X-Request-Id` aparece en nginx, API y persistencia (login) y en nginx, API y world (ranking). `Step` lleva `tick-<n>` y las acciones se siguen por `action_id` en DEBUG. Incluye los ajustes de la revisión de A5: redirección 308, prueba del certificado autogenerado con `TLS_EXTRA_SANS`, rechazo real de TLS 1.1 por el servidor (con `openssl`) y aviso de los bots en modo inseguro.
- [ ] T3 · A7 diagramas al día.

## Criterios de aceptación

- `https://…:8080` y `wss://…/play` funcionan; HTTP redirige; e2e en verde por HTTPS.
- Con una sola petición se encuentra el mismo identificador en los registros de nginx, API y persistencia.
- Los diagramas coinciden con el código y los `.proto`.

## Entrega

- `stacked-to-main`: cada tarea en su rama desde `dev` y su PR.

## Progreso

- T1 en `feature/a5-tls` (ruta delegada); revisión de riesgo alto con 4 lentes aprobada con avisos que se corrigen al empezar la T2: redirección 301 → 308 (la 301 puede convertir un POST en GET), la e2e no ejercita el certificado autogenerado, la comprobación de TLS 1.1 no demuestra nada si Node ya lo rechaza, los bots omiten la verificación por defecto en Compose y una frase de `deploy/README.md`. Fusionada (PR #51). T2 en `feature/a6-request-id` (ruta delegada); revisión de riesgo alto con 4 lentes aprobada. Dos avisos latentes, sin efecto hoy: el registro de persistencia diría `OK` si un servicio usara `set_code` sin `abort` (todos usan `abort`), y un `add_header` en `/api` anularía la herencia si algún día se añaden cabeceras al nivel del servidor.
