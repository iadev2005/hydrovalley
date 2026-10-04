# Pruebas de tolerancia a fallos

## Objetivo

Demostrar con pruebas automáticas, y no con una comprobación a mano, lo que el documento promete cuando un servicio se cae: CP-05 (reinicio de Rust), CP-06 (caída del hub), CP-07 (caída de la persistencia o de PostgreSQL), CP-08 (arranque desordenado) y CP-12 (persistencia caída durante una instantánea). En la defensa el profesor apagará la máquina de un servicio para ver si el sistema lo resiste.

## Problema y por qué

Hasta ahora solo se probó a mano que, al parar world, el panel lo marca en rojo y todo vuelve solo. Ningún caso CP-05 a CP-08 ni CP-12 está automatizado. Además, apagar una máquina no equivale a parar un contenedor: la conexión no se rechaza, sino que nadie responde, y todo depende de los plazos.

## Alcance

- Arnés de fallos en `tests/e2e`: parar y arrancar un servicio (caída del proceso) y pausarlo con `docker pause` (simula la máquina apagada: las conexiones quedan sin respuesta).
- Cada caso mide lo que pide su fila del documento: pérdida máxima, latencia, pausa del valle, *Circuit Breaker*, reconexión sin intervención y que nada se aplique dos veces.
- Se ejecutan con la e2e, en local y en el CI.

Fuera de alcance: orquestación con réplicas (Kubernetes o Swarm; D-05 y `CLAUDE.md`), que se replantea con estos resultados en la mano; CP-09 (chat, nivel 2).

## Restricciones

- Nunca inventar resultados: si un caso falla, se registra el fallo con su salida y se decide aparte si se corrige.
- Los casos usan solo `/api`, `/play` y `/api/system` a través de nginx, más los comandos de Docker para provocar el fallo.
- Tiempo del CI acotado: cada caso dice cuánto tarda.

## Modo de trabajo

- TDD estricto en la lógica pura del arnés (`node --test`); los casos e2e son la verificación.
- Ruta: delegada.

## Tareas

- [ ] T1 · Arnés de fallos y CP-05 (reinicio de Rust, parado y pausado).
- [ ] T2 · CP-06 (hub) y CP-07 / CP-12 (persistencia y PostgreSQL).
- [ ] T3 · CP-08 (arranque en orden inverso, con el tiempo medido) y resumen de resultados en el documento.

## Criterios de aceptación

- Cada caso pasa en local y en el CI, o su fallo queda registrado con evidencia y una decisión.
- Las cifras medidas (tiempos de recuperación, pérdida en ticks) quedan en el documento en lugar de los `[DATO]`.

## Entrega

- Estrategia: `stacked-to-main`; si una tarea queda por debajo del tamaño que pide revisión propia, se junta con la siguiente.

## Progreso

- T1 en `feature/faults-world` (ruta delegada).
