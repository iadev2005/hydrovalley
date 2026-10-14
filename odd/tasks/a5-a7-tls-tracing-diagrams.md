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

- [ ] T1 · A5 TLS en nginx, e2e y bots por HTTPS.
- [ ] T2 · A6 identificador de petición de punta a punta.
- [ ] T3 · A7 diagramas al día.

## Criterios de aceptación

- `https://…:8080` y `wss://…/play` funcionan; HTTP redirige; e2e en verde por HTTPS.
- Con una sola petición se encuentra el mismo identificador en los registros de nginx, API y persistencia.
- Los diagramas coinciden con el código y los `.proto`.

## Entrega

- `stacked-to-main`: cada tarea en su rama desde `dev` y su PR.

## Progreso

- T1 en `feature/a5-tls` (ruta delegada).
