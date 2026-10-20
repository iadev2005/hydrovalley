// Package uuid genera identificadores UUID v4 con el generador criptográfico
// del sistema. Lo usan el boot_id del simulador y los ids de las llamadas
// salientes; no hace falta más que eso.
package uuid

import (
	"crypto/rand"
	"encoding/hex"
)

// New devuelve un UUID v4 en texto, con guiones y en minúsculas.
func New() string {
	var b [16]byte
	// crypto/rand.Read no devuelve error en las plataformas soportadas: si
	// el sistema no tiene entropía, el proceso termina (comportamiento de Go).
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40 // versión 4
	b[8] = b[8]&0x3f | 0x80 // variante RFC 4122
	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:])
}
