// Package bench es el ejecutor Go de la comparativa Go vs. Rust (D-03): mide
// World.Step en un valle del tamaño que pida la carga de trabajo.
//
// La carga se define una vez, en tests/bench/workloads/*.json, y Rust
// (src/world/examples/bench) y Go generan los mismos comandos: lo comprueba
// una huella fija en las pruebas de los dos lados. El algoritmo está descrito
// en tests/bench/README.md. Solo se cronometra World.Step.
package bench
