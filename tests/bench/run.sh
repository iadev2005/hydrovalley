#!/usr/bin/env bash
# Comparativa Go vs. Rust (D-03): compila los dos ejecutores, mide World::step /
# World.Step en cada tamaño de valle y escribe los resultados en
# tests/bench/results/<fecha>-<máquina>.csv, con un .json al lado con los datos
# de la máquina y de las herramientas.
#
# Uso, desde cualquier carpeta del repositorio:
#
#   tests/bench/run.sh
#
# Variables opcionales:
#   RUNS=3                    ejecuciones de cada configuración
#   SIZES="32 64 128 256 512" tamaños de valle (hay un valley-<N>.json por cada uno)
#   BENCH_MACHINE=nombre      etiqueta de la máquina en el nombre del archivo
#   BENCH_CPU="modelo"        modelo de CPU, si no se detecta
#
# Funciona en macOS, Linux y Git Bash/WSL en Windows. Método y cautelas:
# tests/bench/README.md.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNS="${RUNS:-3}"
SIZES="${SIZES:-32 64 128 256 512}"

# --- Máquina --------------------------------------------------------------
case "$(uname -s)" in
  Darwin)
    cpu="$(sysctl -n machdep.cpu.brand_string 2>/dev/null || echo desconocida)"
    cores="$(sysctl -n hw.ncpu 2>/dev/null || echo 0)"
    memory_bytes="$(sysctl -n hw.memsize 2>/dev/null || echo 0)"
    os="macOS $(sw_vers -productVersion 2>/dev/null || true)"
    ;;
  Linux)
    cpu="$(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2- | sed 's/^ *//' || true)"
    cores="$(nproc 2>/dev/null || echo 0)"
    memory_bytes="$(awk '/MemTotal/ {print $2 * 1024}' /proc/meminfo 2>/dev/null || echo 0)"
    os="$(. /etc/os-release 2>/dev/null && echo "${PRETTY_NAME:-Linux}" || echo Linux)"
    ;;
  *)
    cpu="${PROCESSOR_IDENTIFIER:-desconocida}"
    cores="${NUMBER_OF_PROCESSORS:-0}"
    memory_bytes=0
    os="$(uname -sr)"
    ;;
esac
cpu="${BENCH_CPU:-${cpu:-desconocida}}"
arch="$(uname -m)"
machine="${BENCH_MACHINE:-$(echo "$cpu" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]\{1,\}/-/g; s/^-//; s/-$//')}"
date_utc="$(date -u +%Y-%m-%d)"
go_version="$(go version)"
rust_version="$(rustc --version)"

results="$ROOT/tests/bench/results"
mkdir -p "$results"
csv="$results/$date_utc-$machine.csv"
meta="$results/$date_utc-$machine.json"

# --- Compilación ----------------------------------------------------------
echo "Compilando el ejecutor de Rust (release)..." >&2
(cd "$ROOT/src/world" && cargo build --release --locked --example bench >&2)
rust_bin="$ROOT/src/world/target/release/examples/bench"
[ -x "$rust_bin" ] || rust_bin="$rust_bin.exe"

echo "Compilando el ejecutor de Go..." >&2
go_bin="$(mktemp -d)/world-bench"
(cd "$ROOT/src/world-go" && go build -o "$go_bin" ./cmd/world-bench)

# --- Medición -------------------------------------------------------------
{
  echo "run,$("$rust_bin" --csv-header)"
} >"$csv"

mismatch=0
for run in $(seq 1 "$RUNS"); do
  for size in $SIZES; do
    workload="$ROOT/tests/bench/workloads/valley-$size.json"
    echo "Ejecución $run/$RUNS, valle de ${size}x${size}..." >&2
    # Se alternan los lenguajes en cada configuración para repartir la deriva
    # térmica y de carga de la máquina.
    rust_row="$("$rust_bin" "$workload")"
    go_row="$(BENCH_ROOT="$ROOT" "$go_bin" "$workload" 2>/dev/null)"
    echo "$run,$rust_row" >>"$csv"
    echo "$run,$go_row" >>"$csv"
    # La huella del estado final tiene que ser la misma: señal de que los dos
    # hicieron el mismo trabajo.
    if [ "${rust_row##*,}" != "${go_row##*,}" ]; then
      echo "ERROR: las huellas del estado final difieren en ${size}x${size} (Rust ${rust_row##*,}, Go ${go_row##*,})" >&2
      mismatch=1
    fi
  done
done

# --- Datos de la máquina --------------------------------------------------
cat >"$meta" <<JSON
{
  "date_utc": "$date_utc",
  "machine": "$machine",
  "cpu": "$cpu",
  "logical_cores": $cores,
  "memory_bytes": $memory_bytes,
  "os": "$os",
  "arch": "$arch",
  "go": "$go_version",
  "rust": "$rust_version",
  "rust_profile": "release (el de Cargo por defecto, sin LTO)",
  "runs": $RUNS,
  "sizes": "$SIZES"
}
JSON

echo "Resultados: $csv" >&2
echo "Máquina:    $meta" >&2
if [ "$mismatch" -ne 0 ]; then
  exit 1
fi
