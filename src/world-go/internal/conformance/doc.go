// Package conformance comprueba que world-go da lo mismo que el simulador en
// Rust: ejecuta los guiones de tests/conformance/scenarios y compara el
// resultado con los archivos de tests/conformance/expected, que escribe Rust.
//
// Solo tiene pruebas (conformance_test.go); este archivo existe para que el
// paquete exista también fuera de las pruebas.
package conformance
