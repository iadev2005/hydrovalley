package neighbors

import (
	"math/rand/v2"
	"time"

	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
)

// TileStage 1 es tierra seca y 5 es árbol (hydrovalley.world.v1.TileStage).
const (
	stageDrySoil = 1
	stageTree    = 5
)

// ActionKind es lo que un vecino decide hacer en un turno.
type ActionKind int

const (
	// ActionNone: esperar al siguiente turno (sin mapa o sin agua).
	ActionNone ActionKind = iota
	ActionDrop
	ActionPlace
)

// Action es una decisión ya resuelta a una casilla.
type Action struct {
	Kind         ActionKind
	X, Y         uint32
	Installation string
}

// NextDelay devuelve la espera hasta el siguiente turno: interval con una
// variación uniforme de ±jitter (0 a 1), para que los bots no vayan
// sincronizados.
func NextDelay(r *rand.Rand, interval time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return interval
	}
	f := 1 + jitter*(2*r.Float64()-1)
	return time.Duration(float64(interval) * f)
}

// tileWeight es el peso de una casilla al elegir dónde soltar: prefiere la
// tierra seca y las casillas con humedad que aún pueden subir de etapa; un
// árbol ya no crece, así que pesa lo mínimo.
func tileWeight(stage uint32, humidity client.U64) int {
	switch {
	case stage == stageTree:
		return 1
	case humidity > 0:
		return 6
	case stage == stageDrySoil:
		return 4
	default:
		return 2
	}
}

// PickTile elige una casilla con probabilidad proporcional a su peso. Con
// avoidInstalled excluye las que ya tienen una instalación. ok es falso si
// el estado no trae mapa o no queda ninguna casilla elegible.
func PickTile(r *rand.Rand, st client.State, avoidInstalled bool) (x, y uint32, ok bool) {
	w := int(st.Width)
	n := w * int(st.Height)
	if w == 0 || n == 0 || len(st.Stages) < n || len(st.Humidity) < n {
		return 0, 0, false
	}
	var taken map[int]bool
	if avoidInstalled && len(st.Installations) > 0 {
		taken = make(map[int]bool, len(st.Installations))
		for _, in := range st.Installations {
			taken[int(in.Y)*w+int(in.X)] = true
		}
	}
	weights := make([]int, n)
	total := 0
	for i := range n {
		if taken[i] {
			continue
		}
		weights[i] = tileWeight(st.Stages[i], st.Humidity[i])
		total += weights[i]
	}
	if total == 0 {
		return 0, 0, false
	}
	pick := r.IntN(total)
	for i, wt := range weights {
		if pick < wt {
			return uint32(i % w), uint32(i / w), true
		}
		pick -= wt
	}
	return 0, 0, false
}

// Decide es la lógica pura de un turno. Sin estado con mapa espera. Con
// hojas suficientes para un aspersor, a veces (BuildChance) lo compra y
// coloca; si no, suelta una gota, salvo que sepa que el depósito está vacío
// (un jugador que aún no entró al mundo no trae `player`).
func Decide(r *rand.Rand, st *client.State, cfg Config) Action {
	if st == nil {
		return Action{}
	}
	p := st.Player
	if p != nil && p.NextSprinklerPrice > 0 && p.Leaves >= p.NextSprinklerPrice &&
		cfg.BuildChance > 0 && r.Float64() < cfg.BuildChance {
		if x, y, ok := PickTile(r, *st, true); ok {
			return Action{Kind: ActionPlace, X: x, Y: y, Installation: "sprinkler"}
		}
	}
	if p != nil && p.Water == 0 {
		return Action{}
	}
	if x, y, ok := PickTile(r, *st, false); ok {
		return Action{Kind: ActionDrop, X: x, Y: y}
	}
	return Action{}
}
