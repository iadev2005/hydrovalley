# Comparativa Go vs. Rust (D-03)

Mide el mismo paso de simulación del valle en Go y en Rust, en microsegundos por paso, con valles de distinto tamaño. Es la medición de «Implementar el mismo paso de simulación en Go y en Rust» de D-03; el gráfico va al informe.

## Qué se mide

El paso del dominio: `World::step` (Rust, `src/world/`) y `World.Step` (Go, `src/world-go/`), en el mismo proceso, sin red ni gRPC. Los dos parten del mismo catálogo (`catalog/catalog.json`), la misma semilla y los mismos comandos por tick.

- **Tamaño del valle.** El juego es de 32 × 32 casillas (1.024) y no cambia. Para la comparativa existe `World::with_size` / `domain.NewWorldWithSize`, que solo usan los ejecutores: 32 × 32, 64 × 64, 128 × 128, 256 × 256 y 512 × 512. Un valle de otro tamaño no se puede restaurar con `Restore`.
- **Carga** (`workloads/valley-<N>.json`, un archivo por tamaño). Los mismos 64 jugadores en todos los tamaños. Por tick, 20 gotas por cada 1.000 casillas (20 en 32 × 32, 327 en 128 × 128, 5.242 en 512 × 512), el 75 % sobre el cuarto central del valle y el resto repartidas por todo él; así hay zonas que crecen, zonas que se secan y plantas que se marchitan. En la segunda mitad del calentamiento se colocan 2 instalaciones por cada 1.000 casillas (aspersor, goteo y planta por turnos). Los incendios usan la probabilidad del catálogo real. Calentamiento de 400 ticks y luego los ticks medidos (5.000, 4.000, 2.000, 1.000 y 500 según el tamaño).
- **Única cifra del catálogo que cambia:** `rules.tank.capacity_liters` = 10¹², para que ninguna gota se rechace por falta de agua (con 64 jugadores, el depósito real se vaciaría al instante a estas cargas). Todas las demás cifras son las del juego.
- **Cronómetro.** Solo rodea la llamada a `step`: generar los comandos, contar resultados y muestrear el estado quedan fuera. Reloj monótono (`Instant` en Rust, `time.Now` en Go).
- **Estadísticas por paso:** media, mediana, p95, p99, mínimo y máximo, en µs (definiciones en `stats.rs` y `stats.go`, idénticas: p95 y p99 por rango más cercano). El gráfico usa la mediana y el p95 de cada ejecución y, con varias ejecuciones, la mediana de cada una.
- **Comprobación de trabajo igual.** Cada ejecutor imprime la huella FNV-1a de la fila de instantánea final (los bytes son idénticos entre lenguajes, ver `tests/conformance`) y los resultados de los comandos. `run.sh` falla si las huellas difieren entre Rust y Go.

### El generador de comandos

Es el mismo algoritmo en `src/world/examples/bench/workload.rs` y `src/world-go/internal/bench/workload.go`. Usa SplitMix64 con la semilla `seed ^ 0x9E3779B97F4A7C15` (distinta de la del mundo, que usa `seed`). Cada tick, en orden:

1. Si el tick está en la ventana de colocación (de `warmup/2 + 1` a `warmup`), por cada instalación `j` con `j mod duración = tick - inicio`: sortea jugador, `x` e `y` (módulo del número de jugadores, del ancho y del alto); el tipo es `j mod 3` (aspersor, goteo, planta).
2. Gotas, tantas como `área × drops_per_1000_tiles / 1000`: sortea jugador, un número de 0 a 99 (cae en el cuarto central si es menor que `hot_percent`), y dos números para `x` e `y`. En el cuarto central, `x = ancho/4 + n mod (ancho/2)` y `y = alto/4 + n mod (alto/2)`; fuera, `n mod ancho` y `n mod alto`. Siempre son cuatro sorteos por gota.
3. `action_id` correlativos desde 1 (`00000000-0000-4000-8000-<n en 12 cifras>`) y jugadores `00000000-0000-4000-8001-<i en 12 cifras>`.

Una prueba en cada lenguaje compara la huella de los comandos de los 250 primeros ticks de `valley-32.json` con el mismo valor fijo: si los generadores dejan de coincidir, falla una de las dos.

## Cómo ejecutarlo

Desde la raíz del repositorio, con Rust 1.97 (el del CI) y Go 1.26 instalados:

```sh
tests/bench/run.sh
python3 tests/bench/plot.py tests/bench/results/<fecha>-<máquina>.csv docs/informe/comparativa-go-rust.svg
```

`run.sh` compila los dos ejecutores (Rust en `--release`, perfil por defecto de Cargo; Go con `go build`), ejecuta cada configuración `RUNS` veces (3 por defecto; las cifras publicadas usan 4), alternando qué lenguaje corre primero, y escribe `results/<fecha>-<máquina>.csv` y, al lado, un `.json` con la CPU, los núcleos, el sistema operativo y las versiones de Go y Rust. Variables: `RUNS`, `SIZES` (por ejemplo `SIZES="32 256"`), `BENCH_MACHINE` y `BENCH_CPU`. `plot.py` solo usa la biblioteca estándar de Python.

Un ejecutor suelto:

```sh
cd src/world    && cargo run --release --locked --example bench -- ../../tests/bench/workloads/valley-256.json
cd src/world-go && go run ./cmd/world-bench ../../tests/bench/workloads/valley-256.json
```

Las pruebas del generador y de las estadísticas son `src/world/tests/bench_support.rs` (`cargo test --locked`) y `src/world-go/internal/bench` (`go test -race ./...`); el CI las ejecuta con los trabajos de `world` y `world-go`.

## Cautelas

- **Es el paso del dominio en un solo proceso.** No incluye gRPC, la cola del hub, la codificación de mensajes ni la persistencia: no dice cuánto tarda un tick completo del sistema.
- **Una sola máquina, y no es la del laboratorio.** Los resultados versionados son de un Apple Silicon (arm64), un portátil con gestión térmica propia. El laboratorio es x86-64 (i7-13700F, Windows 10 con Docker Desktop y WSL 2): las cifras no se trasladan. **Hay que repetir la medición en el laboratorio** y dejar sus resultados junto a estos (`[DATO: resultados en x86 del laboratorio]`).
- **Un perfil de compilación por lenguaje, sin ajustes.** Rust usa el perfil `release` por defecto de Cargo (sin LTO ni `codegen-units = 1`); Go, el compilador estándar. No se ha perfilado ninguno de los dos ni se ha optimizado a mano. Una comprobación puntual con `lto = "fat"` y `codegen-units = 1` en Rust (una sola ejecución, no incluida en el CSV) mejora los tiempos de Rust, pero no cambia el orden entre los dos lenguajes en los tres tamaños comprobados (32, 256 y 512; ver D-03, «Resultados de la comparativa»).
- **Mismo algoritmo, no necesariamente el mismo código.** Es un port fiel (la conformidad lo comprueba), pero las estructuras de datos de cada lado son las naturales de su lenguaje. La comparativa mide estas dos implementaciones, no los lenguajes en abstracto.
- **Recolector de basura de Go.** No se aísla ni se discute aquí. Los tiempos de Go incluyen lo que el recolector cueste dentro del paso (el ejecutor anota por la salida de error los ciclos que hubo, solo como dato). Siguiendo la advertencia de D-03, el argumento a favor de Rust no son las pausas del recolector: son el reparto de papeles y lo que muestre esta medición.
- **El estado evoluciona.** La carga cambia el estado (casillas que crecen, fuegos); el coste por paso varía a lo largo de la ejecución (por eso se informan mediana, p95 y p99 y no solo la media). Es determinista: los dos lados recorren los mismos estados.
- **Máximos y p99.** Los máximos de un paso aislado dependen del sistema operativo y no se interpretan.
- **Solo los resultados de una ejecución de referencia están en el repositorio** (`results/` y `docs/informe/comparativa-go-rust.svg`); se regeneran con los comandos de arriba.
