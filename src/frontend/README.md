# Frontend

Página mínima del esqueleto, sin React ni Phaser todavía: módulos ES de JavaScript que el navegador carga tal cual, sin paso de compilación. No es un servicio (RES-03): nginx la sirve desde su propia imagen (`deploy/nginx/Dockerfile`).

- Muestra un panel de acceso (usuario, contraseña, **Registrarse** y **Entrar**) que llama a `POST /api/auth/register` y `POST /api/auth/login` en el mismo origen. Los errores de la API se traducen a mensajes (409 nombre repetido, 401 credenciales incorrectas, 429 con los segundos de `Retry-After`, 503 servicio no disponible, 400 el mensaje de la API).
- Con éxito guarda la sesión (`token`, `expiresAt`, `username`, `playerId`) en `localStorage`, oculta el formulario y muestra «Conectado como …» con el botón **Salir**. La contraseña nunca se guarda y el token no se escribe en la consola ni en el registro de la página. Al cargar, si hay una sesión vigente se conecta sin pedir datos; si caducó, se descarta.
- Abre `WS /play?token=…&map=false` en el mismo origen, así que funciona igual en `http://localhost:8080` que detrás de nginx en el laboratorio. La identidad (`player_id`) sale solo del token.
- Si el hub cierra con el código **4401** (token inválido o caducado), borra la sesión, muestra el panel con «la sesión caducó, vuelve a entrar» y **no** reconecta.
- Con cualquier otro cierre, reintenta con espera exponencial y *full jitter* (base 500 ms, techo 10 s); una conexión que dura 5 s o más reinicia la secuencia.
- **Soltar gota** envía `{"type": "drop", "action_id": …, "x": …, "y": …}` y el registro muestra los `result`, `state` y `error` que llegan.

| Ruta | Qué contiene |
| --- | --- |
| `index.html`, `styles.css` | La página |
| `js/auth.js` | Registro, entrada y sesión guardada, sin DOM (`fetch`, almacenamiento y reloj inyectados) |
| `js/play.js` | Mensajes de `play.proto` y utilidades sin DOM (UUID de `action_id`, URL con token, cierre 4401, construir y leer mensajes, espera de reconexión, tamaño del valle) |
| `js/main.js` | DOM y WebSocket |
| `test/` | Pruebas de `auth.js` y `play.js` con `node:test` |

## Pruebas

Desde `src/frontend/`, con Node 22 o superior y sin dependencias:

```
npm test
```

## Pendiente

- React para los paneles y Phaser para el valle isométrico, con su etapa de compilación en la imagen de nginx.
- Acciones pendientes: reenviarlas al reconectar (regla 2 de `CLAUDE.md`).
- El token viaja en la URL del WebSocket y queda en los registros de nginx (riesgo aceptado, ver `src/hub/README.md`).
