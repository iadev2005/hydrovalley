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

- [x] T1 · Arnés de fallos y CP-05 (reinicio de Rust, parado y pausado). Evidencia: RED del arnés; 20 pruebas unitarias; e2e 15 + 3 casos de fallos en verde (los fallos tardan unos 67 s). Mediciones locales (Mac, plan B):
  - `kill` de world (caída brusca): detectado en 1 s, *breaker* abierto en 1 s, recuperado en 2,1 s; 10 ticks perdidos frente a la última instantánea (límite: 50); el reenvío de una acción ya aplicada recibe `duplicate`; la gota enviada durante la caída se aplica.
  - `stop` de world (apagado ordenado, con instantánea final): 0 ticks perdidos; recuperado en 2,1 s.
  - `pause` de world (simula la máquina apagada): detectado en 2 s, recuperado en 1 s, 0 ticks perdidos; `/api/system` responde en 1.012 ms y `Step` se corta a los 206 ms (plazo de 200 ms).
  - `restored_tick` no se expone a los clientes: la pérdida se infiere del primer tick tras el reinicio y de la última instantánea (`tick % 50 == 0`).
- [x] T2 · CP-06 (hub) y CP-07 / CP-12 (persistencia y PostgreSQL). Evidencia: RED en hub, API y arnés; API 266, arnés 30; e2e 15 + 9 casos de fallos en verde (los de fallos, 186 s). `restored_tick` se expone en `/stats` y `/api/system`, y el caso `kill` lo comprueba exacto. Mediciones locales:
  - CP-06 hub parado: detectado en 1,1 s, recuperado en 1 s, reconexión en 0,2 s; la acción sin resultado se reenvía y se aplica una sola vez (humedad +1). Pausado: `/api/system` en 886 ms; el reenvío recibe `duplicate`.
  - CP-07 persistence parada / pausada / PostgreSQL parado: quien juega sigue con p95 de 102,6 / 87,9 / 89,1 ms (< 200 ms); un acceso nuevo recibe 503 con aviso claro en 0,8 / 3,0 / 0,8 s; con PostgreSQL parado la persistencia pasa a `not_serving` y se recupera en 6,1 s. Sin fallos de `Step`.
  - CP-12 persistence parada sobre 2 ticks de instantánea: 10 ticks/s, `Step` p95 máximo 2,4 ms, sin pausa y con el *breaker* cerrado; la primera instantánea tras volver es la del tick 1100 (comprobado matando world después: `restored_tick` = 1100).
- [x] T3 · CP-08 (arranque en orden inverso, con el tiempo medido) y resumen de resultados en el documento. Evidencia: RED en API y arnés; API 268, arnés 36; e2e 15 + 9 + 2 en verde (3 min 54 s en total). CP-08 inverso: todo conectado 7,5 s después del último servicio (18,5 s desde el primero), `RestartCount` 0 en los seis; orden normal 2,1 s / 13,4 s. RNF-10: 4 s desde `compose up` hasta los cinco servicios activos, con caché de Docker caliente (no es una medición desde cero). La API acepta un hub sin `restored_tick` (versiones mezcladas). Resultados en «Resultados medidos» del documento.

## Criterios de aceptación

- Cada caso pasa en local y en el CI, o su fallo queda registrado con evidencia y una decisión.
- Las cifras medidas (tiempos de recuperación, pérdida en ticks) quedan en el documento en lugar de los `[DATO]`.

## Entrega

- Estrategia: `stacked-to-main`; si una tarea queda por debajo del tamaño que pide revisión propia, se junta con la siguiente.

## Progreso

- T1 en `feature/faults-world` (ruta delegada); revisión de riesgo alto con 4 lentes aprobada con avisos de robustez de la prueba (sobre todo: la instantánea restaurada en el caso `kill` se infiere de un tick leído tarde y podría fallar en un CI lento). Se corrigen al empezar la T2 exponiendo `restored_tick` en `/stats`. Fusionada (PR #42); en el CI: `kill` 9 ticks perdidos y recuperación en 1 s, `pause` con `/api/system` en 1.004 ms. T2 en `feature/faults-hub-persistence` (ruta delegada); revisión de riesgo alto con 4 lentes aprobada con avisos (aserciones vacías o mal nombradas en la prueba, reinicio del hub antes de su primer `Step`, pausa antes de leer el identificador y, en la API, que un hub sin `restored_tick` se marque caído al mezclar versiones). Se corrigen al empezar la T3. Fusionada (PR #43); en el CI: CP-07 p95 de 68–96 ms, CP-12 a 10,1 ticks/s y la e2e completa tarda 7 min 12 s. T3 en `feature/faults-startup` (ruta delegada).
