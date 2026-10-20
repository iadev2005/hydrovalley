package bench

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
)

// Workload es una carga: tamaño del valle, jugadores, duración y comandos
// por tick.
type Workload struct {
	Name                      string
	Seed                      uint64
	Width, Height             uint32
	Players                   uint32
	WarmupTicks               uint64
	MeasuredTicks             uint64
	DropsPer1000Tiles         uint64
	HotPercent                uint64
	InstallationsPer1000Tiles uint64
	// CatalogFile es el catálogo base, relativo a la raíz del repositorio.
	CatalogFile string
	// CatalogSet son las cifras del catálogo que cambia la carga (ruta con
	// puntos y valor), en el orden del archivo.
	CatalogSet []CatalogChange
}

// CatalogChange cambia una cifra del catálogo base.
type CatalogChange struct {
	Path  string
	Value uint64
}

// ParseWorkload lee una carga de su JSON. Falta o sobra un campo: error.
func ParseWorkload(data []byte) (Workload, error) {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&raw); err != nil {
		return Workload{}, fmt.Errorf("la carga no es JSON válido: %w", err)
	}
	var err error
	number := func(key string) uint64 {
		value, ok := raw[key]
		if !ok {
			err = errors.Join(err, fmt.Errorf("falta el entero `%s`", key))
			return 0
		}
		n, parseErr := strconv.ParseUint(string(value), 10, 64)
		if parseErr != nil {
			err = errors.Join(err, fmt.Errorf("`%s` no es un entero sin signo", key))
		}
		return n
	}
	text := func(key string) string {
		var s string
		if value, ok := raw[key]; !ok || json.Unmarshal(value, &s) != nil {
			err = errors.Join(err, fmt.Errorf("falta la cadena `%s`", key))
		}
		return s
	}
	small := func(key string) uint32 {
		n := number(key)
		if n > 1<<32-1 {
			err = errors.Join(err, fmt.Errorf("`%s` no cabe en 32 bits", key))
		}
		return uint32(n)
	}

	w := Workload{
		Name:                      text("name"),
		Seed:                      number("seed"),
		Width:                     small("width"),
		Height:                    small("height"),
		Players:                   small("players"),
		WarmupTicks:               number("warmup_ticks"),
		MeasuredTicks:             number("measured_ticks"),
		DropsPer1000Tiles:         number("drops_per_1000_tiles"),
		HotPercent:                number("hot_percent"),
		InstallationsPer1000Tiles: number("installations_per_1000_tiles"),
		CatalogFile:               text("catalog_file"),
	}
	if set, ok := raw["catalog_set"]; !ok {
		err = errors.Join(err, errors.New("falta el objeto `catalog_set`"))
	} else if changes, setErr := parseCatalogSet(set); setErr != nil {
		err = errors.Join(err, setErr)
	} else {
		w.CatalogSet = changes
	}
	if err != nil {
		return Workload{}, err
	}
	switch {
	case w.Width < 2 || w.Height < 2:
		return Workload{}, errors.New("el valle mide al menos 2x2")
	case w.Players == 0:
		return Workload{}, errors.New("hace falta al menos un jugador")
	case w.HotPercent > 100:
		return Workload{}, errors.New("`hot_percent` va de 0 a 100")
	case w.WarmupTicks < 2 || w.MeasuredTicks == 0:
		return Workload{}, errors.New("hace falta calentamiento (2 ticks o más) y ticks medidos")
	}
	return w, nil
}

// parseCatalogSet lee el objeto de cifras cambiadas conservando el orden del
// archivo.
func parseCatalogSet(data json.RawMessage) ([]CatalogChange, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if delim, ok := token.(json.Delim); err != nil || !ok || delim != '{' {
		return nil, errors.New("`catalog_set` no es un objeto")
	}
	var changes []CatalogChange
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		path, _ := key.(string)
		var number json.Number
		decoder.UseNumber()
		if err := decoder.Decode(&number); err != nil {
			return nil, fmt.Errorf("`catalog_set.%s` no es un entero", path)
		}
		value, err := strconv.ParseUint(number.String(), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("`catalog_set.%s` no es un entero sin signo", path)
		}
		changes = append(changes, CatalogChange{Path: path, Value: value})
	}
	return changes, nil
}

func (w Workload) area() uint64 { return uint64(w.Width) * uint64(w.Height) }

// DropsPerTick son las gotas por tick: crecen con el área del valle.
func (w Workload) DropsPerTick() uint64 { return w.area() * w.DropsPer1000Tiles / 1000 }

// InstallationsTotal son las instalaciones que se colocan durante el
// calentamiento.
func (w Workload) InstallationsTotal() uint64 {
	return w.area() * w.InstallationsPer1000Tiles / 1000
}

// placementWindow son el primer tick y la duración de la ventana de
// colocación: la segunda mitad del calentamiento, cuando los jugadores ya
// tienen hojas.
func (w Workload) placementWindow() (first, span uint64) {
	half := w.WarmupTicks / 2
	return half + 1, w.WarmupTicks - half
}

// Generator genera los comandos de cada tick. Hay que pedir los ticks en
// orden (1, 2, 3...): el azar y el contador de acciones avanzan con cada
// llamada.
type Generator struct {
	workload   Workload
	rng        domain.SplitMix64
	nextAction uint64
	players    []string
}

// NewGenerator crea el generador de la carga.
func NewGenerator(w Workload) *Generator {
	players := make([]string, w.Players)
	for n := range players {
		players[n] = fmt.Sprintf("00000000-0000-4000-8001-%012d", n)
	}
	return &Generator{
		workload: w,
		// Distinto del generador del mundo (que usa Seed tal cual).
		rng:        domain.NewSplitMix64(w.Seed ^ 0x9E3779B97F4A7C15),
		nextAction: 1,
		players:    players,
	}
}

func (g *Generator) below(bound uint64) uint64 { return g.rng.NextU64() % bound }

func (g *Generator) command(player uint64, kind domain.CommandKind) domain.Command {
	id := fmt.Sprintf("00000000-0000-4000-8000-%012d", g.nextAction)
	g.nextAction++
	return domain.Command{ActionID: id, PlayerID: g.players[player], Kind: kind}
}

// Commands son los comandos del tick: primero las colocaciones que toquen y
// después las gotas.
func (g *Generator) Commands(tick uint64) []domain.Command {
	w := g.workload
	width, height := uint64(w.Width), uint64(w.Height)
	var commands []domain.Command

	first, span := w.placementWindow()
	if tick >= first && tick < first+span {
		for index := tick - first; index < w.InstallationsTotal(); index += span {
			player := g.below(uint64(w.Players))
			x, y := g.below(width), g.below(height)
			kind := domain.InstallationKind(index % 3) // aspersor, goteo, planta
			commands = append(commands, g.command(player, domain.PlaceInstallationCommand{
				Kind: kind, X: uint32(x), Y: uint32(y),
			}))
		}
	}

	for range w.DropsPerTick() {
		// Siempre cuatro sorteos por gota, caiga donde caiga.
		player := g.below(uint64(w.Players))
		hot := g.below(100) < w.HotPercent
		rawX, rawY := g.rng.NextU64(), g.rng.NextU64()
		var x, y uint64
		if hot {
			x, y = width/4+rawX%(width/2), height/4+rawY%(height/2)
		} else {
			x, y = rawX%width, rawY%height
		}
		commands = append(commands, g.command(player, domain.DropCommand{X: uint32(x), Y: uint32(y)}))
	}
	return commands
}
