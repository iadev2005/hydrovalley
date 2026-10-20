// Package codec traduce entre los mensajes del contrato (proto/) y el
// dominio de world-go. Es la frontera: el dominio no conoce ningún tipo
// generado, y aquí se decide cómo se representa cada cosa en el cable.
//
// Es el espejo de src/world/src/codec.rs: la misma conversión, las mismas
// validaciones y la misma tolerancia con los comandos mal formados, de modo
// que una instantánea escrita por Rust la lee Go y al revés (plan B de D-03).
package codec
