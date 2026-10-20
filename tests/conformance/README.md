# Prueba de conformidad Rust ↔ Go

Demuestra que el simulador en Rust (`src/world/`) y el port en Go (`src/world-go/`) dan el mismo estado con el mismo guion. Es la red de seguridad del plan B de D-03: si Go tiene que retomar el valle desde una instantánea de Rust, las reglas y los bytes tienen que ser los mismos.

## Cómo funciona

```
tests/conformance/
  scenarios/*.json   guiones: semilla, catálogo y comandos por tick (los generan generate_scenarios.py)
  expected/*.json    lo que da Rust con cada guion (lo escribe Rust; Go lo lee)
```

Un guion tiene una semilla, un catálogo (`catalog/catalog.json` con algunas cifras cambiadas por ruta, como `rules.fire.ignition_chance_ppm`; opcionalmente otro para ofrecer al reinicio del valle), los jugadores con nombre y una lista de ticks con sus comandos. Los ticks sin entrada avanzan sin comandos. Los comandos se escriben como el contrato: `drop`, `place_installation`, `buy_upgrade`, `set_demo_mode`, `reset_valley`, o ninguno (un comando sin tipo). El `kind` puede ser un nombre o un número (para mandar valores desconocidos), y el `action` un entero (se convierte en un UUID) o una cadena tal cual (para mandar identificadores mal formados).

Los dos ejecutores hacen lo mismo, tick a tick, pasando por el codec (`catalog_from_proto`, `command_from_proto`, `result_to_proto`, `world_state`, `encode_snapshot` y `decode_snapshot`): crean el valle con la semilla, aplican los comandos y anotan

- el resultado de cada comando (`applied`, `duplicate` o `rejected:<motivo>`);
- `humidity_contributed` y las casillas que arden, tick a tick, y cuántos fuegos se apagaron con la planta en pie y cuántos terminaron en tierra seca;
- cada 50 ticks, una huella (FNV-1a de 64 bits) de la fila de instantánea y del `WorldState`;
- en los ticks `restore_at`, la fila de instantánea completa (hexadecimal) y la restauración del mundo desde ella;
- al final, la fila de instantánea y el `WorldState` completos (hexadecimal).

## Guiones

| Guion | Qué cubre |
| --- | --- |
| `01-catalogo-real` | El `catalog.json` real, seis jugadores: crecimiento hasta árbol, hojas, aspersores, casilla ocupada, agua y hojas insuficientes, evaporación y marchitamiento, modo demo, un reinicio y una restauración |
| `02-incendios` | Ignición al 2 % por casilla seca con planta y tick: aparición con el generador, apagar con tres gotas y su bono, aspersores, fuegos que se consumen, gota más grande |
| `03-demo-y-reinicios` | Modo demo, tres reinicios (uno adopta el catálogo ofrecido, uno es duplicado), épocas, jugadores nuevos tras el reinicio, una restauración |
| `04-invalidos-y-duplicados` | Identificadores en todas las formas de UUID (simple, llaves, urn, mayúsculas) y no válidos, comandos sin tipo y con enums desconocidos, coordenadas fuera del valle, duplicados, un rechazo que se reenvía y la ventana de 600 ticks |
| `05-cifras-propias` | Catálogo con cifras no estándar y economía fuerte: precios escalonados, mejoras, las tres instalaciones, dueños que se ausentan (RF-25), dos restauraciones |
| `06-restauraciones-con-fuego` | Cinco restauraciones, incendios, semilla de 64 bits y un reinicio: debe dar lo mismo que sin restaurar |
| `07-cifras-extremas` | Cifras en el límite de 64 bits sin signo: toda la aritmética satura igual en Rust y en Go |

## Qué comprueba cada lado

- **Rust** (`src/world/tests/conformance.rs`, `cargo test --locked`): vuelve a ejecutar los guiones y exige que `expected/` sea exactamente lo que da ahora. Si el comportamiento de Rust cambia, falla hasta que se regeneren a propósito. Además comprueba que restaurar a mitad del guion no cambia el resultado y que los guiones ejercitan de verdad las reglas.
- **Go** (`src/world-go/internal/conformance`, `go test -race ./...`): ejecuta los guiones y compara con `expected/` (el primer tick que difiere, no solo «distinto»); comprueba que las instantáneas finales son **idénticas byte a byte**; que las instantáneas que escribió Rust se leen en Go, se vuelven a escribir con los mismos bytes y, siguiendo el guion desde ahí, dan el mismo estado final (el plan B); y que restaurar no cambia el resultado.

Los bytes son idénticos: los campos de `WorldSnapshot` se escriben en orden de número, no hay mapas y los ceros se omiten en protobuf, así que prost y Go producen la misma serialización. Si alguna vez dejaran de serlo, la comparación habría que hacerla sobre las partes decodificadas (`Parts()`), no sobre los bytes.

## Cuándo regenerar

Solo si el cambio de comportamiento es a propósito (una regla nueva, una cifra distinta en `catalog/catalog.json`, un guion nuevo). Si Go falla sin que Rust cambie, el error es de Go: se arregla en Go.

```sh
# 1. Si se cambió el generador de guiones, desde la raíz del repositorio:
python3 tests/conformance/generate_scenarios.py

# 2. Resultados esperados (los escribe Rust):
cd src/world
CONFORMANCE_REGENERATE=1 cargo test --locked --test conformance

# 3. Revisar el diff de tests/conformance/expected y comprobar los dos lados:
cargo test --locked
cd ../world-go && go test -count=1 -race ./...
```

`-count=1` evita la caché de `go test`: los archivos de `tests/conformance` quedan fuera del módulo de Go y la caché no los vigila.

## Agregar un guion

Un guion nuevo es una función más en `generate_scenarios.py` (o un JSON a mano en `scenarios/`) y, después, regenerar los esperados. Debe llegar a unos 300 ticks como mínimo; la prueba de Rust y la de Go comprueban que, entre todos, los guiones producen cada tipo de rechazo, duplicados y fuegos.
