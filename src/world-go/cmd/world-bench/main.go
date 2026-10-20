// Comando world-bench: ejecutor Go de la comparativa Go vs. Rust (D-03).
// Mide World.Step en el valle de una carga de tests/bench/workloads.
//
// Uso, desde src/world-go:
//
//	go run ./cmd/world-bench ../../tests/bench/workloads/valley-256.json
//	go run ./cmd/world-bench --csv-header
//
// Imprime una fila CSV con las estadísticas por paso (µs) y la huella de la
// instantánea final, que tiene que coincidir con la del ejecutor de Rust.
// Método y cautelas: tests/bench/README.md.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/bench"
)

// repoRoot es la raíz del repositorio, relativa a src/world-go (donde se
// ejecuta el comando); BENCH_ROOT la cambia.
const repoRoot = "../.."

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	if len(args) == 1 && args[0] == "--csv-header" {
		fmt.Fprintln(stdout, bench.CSVHeader)
		return 0
	}
	if len(args) != 1 {
		fmt.Fprintln(stderr, "uso: world-bench <carga.json> | --csv-header")
		return 2
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	workload, err := bench.ParseWorkload(data)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	root := repoRoot
	if env := os.Getenv("BENCH_ROOT"); env != "" {
		root = filepath.Clean(env)
	}
	row, err := bench.Run(workload, root)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintln(stdout, row.CSV())
	fmt.Fprintf(stderr, "ciclos del recolector durante la ejecución: %d\n", row.GCCycles)
	return 0
}
