# Catálogo y reglas del valle

## Objetivo

Que el juego sea un juego: las cifras salen de `catalog/` y el simulador en Rust aplica las reglas de D-01 (recarga, evaporación, crecimiento y marchitamiento, instalaciones, mejoras, incendios, modo demo y reinicio).

## Problema y por qué

Hoy Rust solo aplica gotas con cifras fijas en el código. Sin catálogo no se pueden ajustar las cifras sin recompilar, y sin reglas no hay crecimiento, hojas, ranking ni incendios (nivel 1).

## Alcance

- Catálogo: archivo de datos en `catalog/` con las cifras de arranque de D-01; la persistencia lo siembra en `catalog_entries` y sirve `CatalogService.GetCatalog` con `catalog_version`; Rust lo carga al arrancar y al reiniciar el valle.
- Reglas en Rust: recarga del depósito, evaporación, crecimiento (consume 100 de humedad), marchitamiento, hojas para quien hace crecer, modo demo y reinicio del valle (RF-31).
- Instalaciones (aspersor, goteo, planta de tratamiento), su compra con la regla del 15 %, una por casilla, y las dos mejoras.
- Incendios con el generador con semilla (estado en la instantánea): aparición, tres gotas, aspersor y consumo a los 300 ticks.
- Hub y página: los comandos nuevos por `/play` y lo mínimo en pantalla para verlo funcionar; e2e.

Fuera de alcance: propagación del fuego, obras comunitarias (nivel 2), React y Phaser.

## Restricciones

- Las del `CLAUDE.md`: escritor único, azar solo en Rust con semilla en la instantánea, enteros sin signo, el tick como reloj, instantánea cada 50 ticks.
- Las cifras son valores iniciales del catálogo, nunca constantes en el código.

## Modo de trabajo

- TDD estricto (configuración de la sesión). Ejecutores: `cargo test` (world), pytest (persistencia), `go test -race` (hub), `node --test` (página y e2e).
- Ruta: delegada (cada tarea toca 2 o más archivos no triviales).

## Tareas

- [x] T1 · Catálogo: `catalog/` con las cifras de D-01, siembra en la persistencia, `CatalogService.GetCatalog`, y Rust carga el catálogo al arrancar. Evidencia: RED por módulos inexistentes; persistencia 174 unit / 201 con PostgreSQL; world 59+8+11; e2e 6/6. `catalog_version` = SHA-256 del JSON canónico. Huecos: probabilidad de incendio sin cifra en D-01 (`null`, [DATO]); la humedad de la gota en modo demo no tiene campo en el contrato (pasa a T2).
- [ ] T2 · Reglas base en Rust: recarga, evaporación, crecimiento y marchitamiento, hojas, modo demo y reinicio del valle.
- [ ] T3 · Instalaciones y mejoras en Rust, y sus comandos en el hub.
- [ ] T4 · Incendios en Rust con el generador con semilla.
- [ ] T5 · Página y e2e: ver etapas, hojas e incendios; prueba de extremo a extremo del crecimiento.

## Criterios de aceptación

- Cambiar una cifra en `catalog/` cambia el juego sin recompilar.
- Las reglas de D-01 tienen pruebas en Rust, incluidas las de los diagramas 05 y 06.
- Misma semilla + mismos comandos = mismo valle (CP-10).
- CI verde en los ocho jobs.

## Entrega

- Previsión: más de 400 líneas; PR encadenados por tarea.
- Estrategia: `ask-on-risk` → `stacked-to-main` (la misma que eligió el usuario para el acceso): cada tarea en su rama desde `dev` y su PR contra `dev`.

## Progreso

- T1 en `feature/valley-catalog` (ruta delegada).
