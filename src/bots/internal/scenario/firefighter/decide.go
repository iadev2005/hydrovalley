// Paquete firefighter: los bomberos (RF-29, RF-30, CP-04). Varios bots
// vigilan el estado y, cuando aparece un incendio, sueltan todos a la vez
// sus gotas sobre la misma casilla hasta que desaparece del estado.
package firefighter

import (
	"math/rand/v2"
	"sort"

	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
)

// Tile es una casilla (x, y) desde la esquina superior izquierda.
type Tile struct{ X, Y uint32 }

// Resultado de un incendio visto.
const (
	// OutcomeExtinguished: desapareció y la casilla conserva su planta.
	OutcomeExtinguished = "extinguished"
	// OutcomeBurnedOut: desapareció y la casilla volvió a tierra seca: se
	// consumió en vez de apagarse.
	OutcomeBurnedOut = "burned_out"
	// OutcomeUnknown: desapareció pero no hay mapa para saber cómo.
	OutcomeUnknown = "unknown"
	// OutcomeUnresolved: seguía ardiendo al terminar, tras haber sido atacado.
	OutcomeUnresolved = "unresolved"
	// OutcomePendingAtEnd: apareció al final y no dio tiempo a atacarlo.
	OutcomePendingAtEnd = "pending_at_end"
)

// stageDrySoil es la etapa «tierra seca» de TileStage (play.proto).
const stageDrySoil = 1

// NextTarget elige la casilla a apagar: la actual mientras siga ardiendo y,
// si no, la primera por fila y luego por columna, para que todos los bots
// coincidan. Sin incendios devuelve false.
func NextTarget(fires []client.Fire, current *Tile) (Tile, bool) {
	if len(fires) == 0 {
		return Tile{}, false
	}
	tiles := make([]Tile, len(fires))
	for i, f := range fires {
		tiles[i] = Tile{X: f.X, Y: f.Y}
	}
	if current != nil {
		for _, t := range tiles {
			if t == *current {
				return t, true
			}
		}
	}
	sort.Slice(tiles, func(i, j int) bool {
		if tiles[i].Y != tiles[j].Y {
			return tiles[i].Y < tiles[j].Y
		}
		return tiles[i].X < tiles[j].X
	})
	return tiles[0], true
}

// Classify dice cómo terminó un incendio por la etapa de su casilla: un fuego
// consumido deja tierra seca (src/world/src/domain/fire.rs); uno apagado
// conserva la planta. Sin mapa (stages vacío) no se sabe.
func Classify(stages []uint32, width uint32, t Tile) string {
	i := int(t.Y)*int(width) + int(t.X)
	if width == 0 || i < 0 || i >= len(stages) {
		return OutcomeUnknown
	}
	if stages[i] == stageDrySoil {
		return OutcomeBurnedOut
	}
	return OutcomeExtinguished
}

// BonusBots devuelve los bots cuyas hojas subieron al menos bonus entre
// before y after. Es una heurística: las hojas también suben por crecimiento.
func BonusBots(before, after []uint64, bonus uint64) []int {
	var out []int
	for i := range min(len(before), len(after)) {
		if after[i] >= before[i] && after[i]-before[i] >= bonus {
			out = append(out, i)
		}
	}
	return out
}

// SeedTiles elige hasta n casillas distintas dentro del valle.
func SeedTiles(r *rand.Rand, width, height uint32, n int) []Tile {
	n = min(n, int(width)*int(height))
	seen := map[Tile]bool{}
	var out []Tile
	for len(out) < n {
		t := Tile{X: r.Uint32N(width), Y: r.Uint32N(height)}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}
