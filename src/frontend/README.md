# Frontend

Página mínima del esqueleto, sin React ni Phaser todavía: módulos ES de JavaScript que el navegador carga tal cual, sin paso de compilación. No es un servicio (RES-03): nginx la sirve desde su propia imagen (`deploy/nginx/Dockerfile`).

- Genera un `player_id` (UUID) y lo guarda en `localStorage`. Es la identidad provisional hasta que exista el token firmado.
- Abre `WS /play?player_id=…&map=false` en el mismo origen, así que funciona igual en `http://localhost:8080` que detrás de nginx en el laboratorio.
- **Soltar gota** envía `{"type": "drop", "action_id": …, "x": …, "y": …}` y el registro muestra los `result`, `state` y `error` que llegan.

| Ruta | Qué contiene |
| --- | --- |
| `index.html`, `styles.css` | La página |
| `js/play.js` | Mensajes de `play.proto` y utilidades sin DOM (UUID, identidad, URL, construir y leer mensajes) |
| `js/main.js` | DOM y WebSocket |
| `test/` | Pruebas de `play.js` con `node:test` |

## Pruebas

Desde `src/frontend/`, con Node 22 o superior y sin dependencias:

```
npm test
```

## Pendiente

- React para los paneles y Phaser para el valle isométrico, con su etapa de compilación en la imagen de nginx.
- Acciones pendientes: reenviarlas al reconectar (regla 2 de `CLAUDE.md`).
- Token de la API en lugar de `player_id`.
