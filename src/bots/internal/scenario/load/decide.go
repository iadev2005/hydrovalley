// Paquete load: la prueba de carga de CP-11. Muchos bots sueltan gotas a
// unas 4 por segundo en modo demo; al final se comprueba RNF-01, RNF-02 y
// que la humedad aportada coincida con las gotas aceptadas.
package load

import (
	"math/rand/v2"
	"time"
)

// NextDelay es la espera entre gotas de un bot: interval ± jitter, siempre
// positiva. El documento pide 250 ms ± 50 ms (unas 4 gotas por segundo).
func NextDelay(r *rand.Rand, interval, jitter time.Duration) time.Duration {
	d := interval
	if jitter > 0 {
		d += time.Duration(r.Int64N(int64(2*jitter)+1)) - jitter
	}
	return max(d, time.Millisecond)
}

// PickTile elige una casilla uniformemente dentro del valle de width x
// height. Un valle vacío da (0, 0).
func PickTile(r *rand.Rand, width, height uint32) (x, y uint32) {
	if width == 0 || height == 0 {
		return 0, 0
	}
	return r.Uint32N(width), r.Uint32N(height)
}
