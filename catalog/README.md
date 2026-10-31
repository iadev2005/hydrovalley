# Catálogo del juego

`catalog.json` reúne las cifras de arranque de D-01 (`docs/documento-inicial.md`, «Cifras de arranque»). Son **valores iniciales y ajustables**: se cambian aquí, sin tocar código ni recompilar.

## Cómo llega al juego

1. La persistencia lee este archivo al arrancar (variable `CATALOG_PATH`), lo valida y lo guarda en la tabla `catalog_entries`. Si el archivo no es válido, no arranca.
2. `CatalogService.GetCatalog` lo entrega junto con `catalog_version`: el SHA-256 (hexadecimal) del JSON canónico del catálogo (claves ordenadas, sin espacios). Cambiar una cifra cambia la versión; cambiar solo el formato del archivo, no.
3. El simulador (Rust) lo pide al arrancar y al reiniciar el valle, y guarda `catalog_version` en cada instantánea.

Para aplicar un cambio hay que reiniciar la persistencia (siembra el archivo) y luego el simulador (lo vuelve a pedir).

## Forma del archivo

Tres grupos, que son el `kind` de las filas de `catalog_entries`: `rules`, `installations` y `upgrades`. Todos los valores son enteros sin signo (regla 7 de `CLAUDE.md`). Las unidades van en el nombre de la clave.

### `rules`

| Clave | Significado | Valor |
| --- | --- | --- |
| `drop.water_cost_liters` | Agua que cuesta una gota (en modo demo es gratis) | 1 |
| `drop.humidity_points` | Humedad que suma una gota a su casilla | 100 |
| `drop.demo_humidity_points` | Humedad que suma una gota en modo demo (gratis y de una sola casilla) | 1 |
| `tank.capacity_liters` | Capacidad inicial del depósito | 10 |
| `tank.refill_liters` | Litros que recarga el depósito en cada intervalo | 1 |
| `tank.refill_interval_seconds` | Segundos entre recargas | 2 |
| `growth.humidity_per_stage_points` | Humedad que hace falta, y se gasta, para subir una etapa | 100 |
| `growth.leaves_grass`, `leaves_shrub`, `leaves_flowers`, `leaves_tree` | Hojas que gana quien hace llegar la casilla a cada etapa | 1, 2, 3, 5 |
| `growth.tree_max_humidity_points` | Humedad máxima de un árbol | 100 |
| `evaporation.humidity_per_second_points` | Humedad que pierde cada casilla por segundo | 2 |
| `evaporation.wither_after_ticks` | Ticks con humedad 0 antes de bajar una etapa (un tick = 100 ms) | 140 |
| `fire.drops_to_extinguish` | Gotas que apagan un incendio | 3 |
| `fire.extinguish_bonus_leaves` | Hojas para quien suelta la última | 3 |
| `fire.burn_out_ticks` | Ticks hasta que un incendio sin atender se consume | 300 |
| `fire.ignition_chance_ppm` | Probabilidad por tick de que arda cada casilla seca con planta, en partes por millón (0 = no hay incendios) | 400 |
| `fire.propagation_chance_ppm` | Probabilidad por tick de que un fuego salte a una casilla vecina elegible, en partes por millón (0 = no hay propagación) | 3000 |
| `terrain.river_width_tiles` | Ancho del río en casillas; 0 = sin río ni lago (expansión, nivel 2) | 2 |
| `terrain.rock_count` | Rocas sueltas que se intentan colocar al crear o reiniciar el valle; 0 = sin rocas | 12 |
| `urban.building_tree_threshold` | Árboles en el valle a partir de los cuales aparece el primer edificio... | 2 |
| `urban.building_humidity_points_threshold` | ...o humedad total aportada a partir de la cual aparece | 500 |
| `urban.max_buildings` | Tope de edificios en el valle | 2 |
| `urban.trash_interval_seconds` | Segundos entre dos emisiones de basura de un edificio | 15 |
| `urban.trash_spread_interval_seconds` | Segundos entre dos crecimientos y propagaciones de la basura | 20 |
| `urban.clean_reward_leaves` | Hojas de quien deja limpia una casilla de basura | 1 |
| `installation_price_increase_percent` | Aumento del precio por cada compra del mismo tipo de instalación (RF-07) | 5 |
| `offline_installation_seconds` | Tiempo que las instalaciones siguen trabajando tras la última acción de su dueño (RF-25) | 7200 |

### `installations`

| Clave | Significado | Valor |
| --- | --- | --- |
| `sprinkler.price_leaves` | Precio del primer aspersor de agua gris (Reutilizar) | 10 |
| `sprinkler.area_side_tiles` | Lado del área cuadrada que riega | 3 |
| `sprinkler.humidity_points` | Humedad que suma a cada casilla de su área | 25 |
| `sprinkler.interval_seconds` | Segundos entre riegos | 2 |
| `sprinkler.durability` | Durabilidad base (ciclos de riego antes de destruirse) | 30 |
| `drip.price_leaves` | Precio del primer riego por goteo (Reducir) | 25 |
| `drip.area_side_tiles` | Lado del área cuadrada que cubre | 5 |
| `drip.evaporation_divisor` | Divisor de la evaporación en su área (2 = la mitad) | 2 |
| `drip.durability` | Durabilidad base (ciclos de tiempo antes de destruirse) | 30 |
| `drip.wear_interval_seconds` | Segundos de trabajo activo que cuestan un punto de durabilidad del goteo | 2 |
| `treatment_plant.price_leaves` | Precio de la primera planta de tratamiento (Reciclar) | 40 |
| `treatment_plant.refill_liters` | Litros extra de recarga del depósito de su dueño por intervalo | 3 |
| `treatment_plant.interval_seconds` | Segundos entre recargas | 2 |
| `treatment_plant.durability` | Durabilidad base (pulsos antes de destruirse) | 30 |
| `trash_bin.price_leaves` | Precio del primer cesto de reciclaje (expansión, nivel 2) | 15 |
| `trash_bin.area_side_tiles` | Lado del área cuadrada que limpia y protege de la basura | 3 |
| `trash_bin.interval_seconds` | Segundos entre limpiezas (1 nivel de basura por casilla del área) | 2 |
| `trash_bin.durability` | Durabilidad base (limpiezas antes de destruirse) | 30 |

### `upgrades`

| Clave | Significado | Valor |
| --- | --- | --- |
| `bigger_drop.price_leaves` | Precio de la gota más grande | 30 |
| `bigger_drop.area_side_tiles` | Lado del área cuadrada que humedece una gota | 3 |
| `bigger_tank.price_leaves` | Precio del depósito mayor | 20 |
| `bigger_tank.extra_capacity_liters` | Capacidad extra del depósito | 10 |

## Valores elegidos por el equipo para la jugabilidad

La expansión de jugabilidad (`docs/plan-expansion-mecanicas.md`) cambió dos cifras de arranque de D-01 y añadió las demás. Son decisiones de ajuste del equipo, no cifras de D-01; D-01 las recoge en su sección «Valores elegidos por el equipo para la jugabilidad»:

- `drop.humidity_points` pasa de 25 a 100: una gota hace subir una etapa (`growth.humidity_per_stage_points` sigue en 100).
- `treatment_plant.refill_liters` pasa de 1 a 3.
- Las `durability` de las cuatro instalaciones (30), el cesto de reciclaje, el terreno natural y la presión urbana son nuevos.
- Ajuste de ritmo de Cristian Baczek (`12042fb`, `ad737ed`): `tank.capacity_liters` 20 → 10, `growth.leaves_flowers` 4 → 3, `growth.leaves_tree` 8 → 5, `evaporation.wither_after_ticks` 100 → 140, `fire.extinguish_bonus_leaves` 5 → 3, `fire.ignition_chance_ppm` 50 → 1250, `fire.propagation_chance_ppm` 2000 → 10000, `urban.trash_interval_seconds` 5 → 6, `urban.trash_spread_interval_seconds` 10 → 8 y `urban.clean_reward_leaves` 2 → 1. Son valores elegidos por el equipo y se evalúan jugando.
- Ajuste tras la predefensa del 29 de octubre (había demasiada basura y fuego y los precios subían demasiado): `fire.ignition_chance_ppm` 1250 → 400, `fire.propagation_chance_ppm` 10000 → 3000, `urban.trash_interval_seconds` 6 → 15, `urban.trash_spread_interval_seconds` 8 → 20 e `installation_price_increase_percent` 15 → 5.

Los guiones de conformidad 01 a 09 fijan las cifras originales de D-01 (ver `tests/conformance/README.md`); solo el 10 usa este archivo sin cambios.

## Datos pendientes

- `fire.ignition_chance_ppm = 400` (400 por millón, por tick y por casilla elegible; con 1.024 casillas y 10 ticks por segundo equivale a unos 4,1 incendios por segundo si todo el valle fuera elegible) es un valor elegido por el equipo, ajustable: D-01 dice que los incendios «aparecen al azar» pero no da la cifra, y el valor inicial era 50. `[DATO: confirmar con el equipo que este ritmo de incendios es el buscado]`.
- Fuera del catálogo, por ser estructura y no cifras de ajuste: el tamaño del valle (32 × 32), la ventana de 600 ticks de `action_id` recientes y el límite de 15 gotas por segundo (del hub).
