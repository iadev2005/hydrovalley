// Package domain es el núcleo del simulador de world-go: reglas puras, sin
// gRPC ni red. Es un port fiel del dominio en Rust (src/world/src/domain),
// que es la especificación: mismas reglas, mismo orden de operaciones dentro
// del tick y misma aritmética de enteros sin signo.
//
// El dominio no depende de los tipos generados desde proto/; la traducción
// vive en internal/codec.
package domain
