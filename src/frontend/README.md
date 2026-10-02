# Frontend

Página mínima del esqueleto, sin React ni Phaser todavía: módulos ES de JavaScript que el navegador carga tal cual, sin paso de compilación. No es un servicio (RES-03): nginx la sirve desde su propia imagen (`deploy/nginx/Dockerfile`).

- Muestra un panel de acceso (usuario, contraseña, **Registrarse** y **Entrar**) que llama a `POST /api/auth/register` y `POST /api/auth/login` en el mismo origen. Los errores de la API se traducen a mensajes (409 nombre repetido, 401 credenciales incorrectas, 429 con los segundos de `Retry-After`, 503 servicio no disponible, 400 el mensaje de la API).
- Con éxito guarda la sesión (`token`, `expiresAt`, `username`) en `localStorage`, oculta el formulario y muestra «Conectado como …» con el botón **Salir**. La contraseña nunca se guarda y el token no se escribe en la consola ni en el registro de la página. Tras un `429` los botones Registrarse y Entrar siguen desactivados hasta que pasa el `Retry-After` (5 s si no lo trae). La decisión al cerrarse el WebSocket (reconectar, volver a entrar con 4401 o con la sesión vencida, ignorar un socket ya soltado) es la función pura `decideOnClose` de `js/play.js`. Al cargar, si hay una sesión vigente se conecta sin pedir datos; si caducó, se descarta.
- Abre `WS /play?token=…&map=true` en el mismo origen, así que funciona igual en `http://localhost:8080` que detrás de nginx en el laboratorio. La identidad (`player_id`) sale solo del token, que la página no decodifica salvo para leer `username` tras el acceso (el login no lo devuelve).
- Si el hub cierra con el código **4401** (token inválido o caducado), borra la sesión, muestra el panel con «la sesión caducó, vuelve a entrar» y **no** reconecta.
- Con cualquier otro cierre, reintenta con espera exponencial y *full jitter* (base 500 ms, techo 10 s); una conexión que dura 5 s o más reinicia la secuencia.
- **Valle** (`<canvas>` de 32 × 32 casillas): color por etapa (tierra seca, pasto, arbusto, flores, árbol), cuadro rojo para el fuego y una letra para cada instalación (A aspersor, G goteo, P planta). Un clic selecciona la casilla. El panel muestra agua y capacidad, hojas y el precio de la siguiente instalación de cada tipo.
- Botones: **Soltar gota**, **Colocar aspersor / riego por goteo / planta de tratamiento** (sobre la casilla elegida) y **Comprar gota grande / depósito mayor**. Cada acción lleva un `action_id` y queda pendiente hasta su resultado final; el mensaje de resultado traduce `applied`, `duplicate` (cuenta como aplicada), `rejected` con su motivo, `expired`, `rate_limited`, `paused` y `forbidden`.
- **Reconexión**: las acciones pendientes se guardan con su mensaje y se reenvían al reabrir el WebSocket (regla 2 de `CLAUDE.md`); un resultado final, de cualquier clase, las quita. El hub ignora las que siguen en cola y responde `duplicate` a las ya aplicadas.
- **Administración (RF-31)**: si el token trae `role = admin` (se lee de su carga útil solo para mostrar los controles; el hub decide), aparecen **Activar/Desactivar modo demo** y **Reiniciar valle**, este último con una confirmación dentro de la página.

| Ruta | Qué contiene |
| --- | --- |
| `index.html`, `styles.css` | La página |
| `js/auth.js` | Registro, entrada y sesión guardada, sin DOM (`fetch`, almacenamiento y reloj inyectados) |
| `js/play.js` | Mensajes de `play.proto` y utilidades sin DOM (UUID de `action_id`, URL con token, cierre 4401, construir y leer mensajes, espera de reconexión, tamaño del valle) |
| `js/valley.js` | Vista del valle sin DOM: estado → casillas y panel, resultado → mensaje, rol del token y acciones pendientes |
| `js/main.js` | DOM, lienzo y WebSocket |
| `test/` | Pruebas de `auth.js` y `play.js` con `node:test` |

## Pruebas

Desde `src/frontend/`, con Node 22 o superior y sin dependencias:

```
npm test
```

## Pendiente

- React para los paneles y Phaser para el valle isométrico, con su etapa de compilación en la imagen de nginx.
- El dibujo del lienzo y los clics no tienen prueba automática (solo la lógica pura de `valley.js` y `play.js`); falta comprobar la página en un navegador.
- El token viaja en la URL del WebSocket y queda en los registros de nginx (riesgo aceptado, ver `src/hub/README.md`).
