# Chat

Servicio de chat en Go (nivel 2). Los mensajes se ordenan con `seq`, un contador
monótono que actúa como reloj lógico (regla 3). Ver `docs/documento-inicial.md`.

## Orden de entrega

`seq` se asigna dentro de `store.Append`, pero la difusión ocurre después. Si dos
emisores difundieran sin coordinación, un suscriptor podría recibir el `seq` 5 y
luego el 6 y otro, el 6 y luego el 5.

Para evitarlo, toda publicación pasa por `Hub.Publish`, que mantiene un único
candado (`publishMu`) mientras asigna el `seq` y encola el mensaje en el canal de
cada suscriptor. Lo usan tanto el WebSocket (`send`) como el gRPC `SendMessage`.
Así cada canal recibe los mensajes en orden estricto de `seq`.

El candado cubre solo el `Append` en memoria y envíos no bloqueantes a canales con
búfer; nunca una escritura de red. Cada conexión tiene su propia goroutine de
escritura que vacía su canal.

## Suscriptor lento

Cada suscriptor tiene un canal con búfer de 128 mensajes. Si se llena, el hub no
descarta el mensaje en silencio ni bloquea la publicación: expulsa al suscriptor
(cierra su canal) y el handler cierra la conexión con el código 1013
(*try again later*).

Motivo: el frontend (`useChatSocket.ts`) solo pide `sync` con `after_seq` al
abrir o reconectar, y no detecta huecos de `seq` en mensajes en vivo. Un mensaje
descartado en silencio quedaría perdido hasta la siguiente reconexión. Al cerrar
la conexión, el cliente reconecta con su backoff y recupera lo perdido con
`sync(after_seq = último seq recibido)`, siempre que el mensaje siga entre los
últimos 200 del búfer del servidor.
