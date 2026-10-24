package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	persistencev1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/persistence/v1"
	"google.golang.org/protobuf/proto"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/codec"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
)

// CSVHeader son las columnas de Row.CSV; son las mismas que imprime el
// ejecutor de Rust.
const CSVHeader = "impl,workload,width,height,players,warmup_ticks,measured_ticks," +
	"mean_us,median_us,p95_us,p99_us,min_us,max_us," +
	"applied,duplicate,rejected,installations,burning_tiles,max_burning_tiles,trees,state_fnv1a"

// Row es el resultado de una ejecución.
type Row struct {
	Workload Workload
	Stats    Stats
	// Resultados de los comandos de todos los ticks (calentamiento incluido).
	Applied, Duplicate, Rejected uint64
	// Estado final y casillas que arden (el máximo, muestreado cada 50 ticks
	// fuera del cronómetro).
	Installations, BurningTiles, MaxBurningTiles, Trees int
	// StateFNV1a es la huella de la fila de instantánea final; tiene que
	// coincidir con la del ejecutor de Rust.
	StateFNV1a uint64
	// GCCycles son los ciclos del recolector durante toda la ejecución
	// (informativo; no está en el CSV).
	GCCycles uint32
}

// CSV es la fila de resultados.
func (r Row) CSV() string {
	w, s := r.Workload, r.Stats
	return fmt.Sprintf("go,%s,%d,%d,%d,%d,%d,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%d,%d,%d,%d,%d,%d,%d,%016x",
		w.Name, w.Width, w.Height, w.Players, w.WarmupTicks, w.MeasuredTicks,
		s.MeanUS, s.MedianUS, s.P95US, s.P99US, s.MinUS, s.MaxUS,
		r.Applied, r.Duplicate, r.Rejected, r.Installations, r.BurningTiles,
		r.MaxBurningTiles, r.Trees, r.StateFNV1a)
}

// LoadCatalog lee el catálogo base de la carga (relativo a root, la raíz del
// repositorio) y aplica sus cambios.
func LoadCatalog(root string, w Workload) (domain.Catalog, error) {
	data, err := os.ReadFile(filepath.Join(root, w.CatalogFile))
	if err != nil {
		return domain.Catalog{}, err
	}
	doc := map[string]any{}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return domain.Catalog{}, fmt.Errorf("%s: %w", w.CatalogFile, err)
	}
	for _, change := range w.CatalogSet {
		if err := setNumber(doc, change.Path, change.Value); err != nil {
			return domain.Catalog{}, err
		}
	}
	message, err := catalogMessage(doc)
	if err != nil {
		return domain.Catalog{}, err
	}
	return codec.CatalogFromProto(message)
}

// lookup sigue una ruta con puntos hasta su objeto padre y devuelve la clave
// final.
func lookup(doc map[string]any, path string) (parent map[string]any, key string, err error) {
	keys := strings.Split(path, ".")
	parent = doc
	for _, step := range keys[:len(keys)-1] {
		next, ok := parent[step].(map[string]any)
		if !ok {
			return nil, "", fmt.Errorf("la ruta %s no existe en el catálogo", path)
		}
		parent = next
	}
	key = keys[len(keys)-1]
	if _, ok := parent[key]; !ok {
		return nil, "", fmt.Errorf("la ruta %s no existe en el catálogo", path)
	}
	return parent, key, nil
}

// setNumber cambia el entero de una ruta que ya existe (un nombre mal escrito
// en una carga es un error, no un campo nuevo).
func setNumber(doc map[string]any, path string, value uint64) error {
	parent, key, err := lookup(doc, path)
	if err != nil {
		return err
	}
	if _, ok := parent[key].(json.Number); !ok {
		return fmt.Errorf("%s no es un entero", path)
	}
	parent[key] = json.Number(strconv.FormatUint(value, 10))
	return nil
}

// catalogMessage convierte el catálogo en el mensaje de GetCatalog, como lo
// hace el servicio de persistencia.
func catalogMessage(doc map[string]any) (*persistencev1.Catalog, error) {
	var failure error
	n := func(path string) uint64 {
		parent, key, err := lookup(doc, path)
		if err != nil {
			failure = err
			return 0
		}
		text, ok := parent[key].(json.Number)
		if !ok {
			failure = fmt.Errorf("%s no es un entero", path)
			return 0
		}
		value, err := strconv.ParseUint(text.String(), 10, 64)
		if err != nil {
			failure = fmt.Errorf("%s: %w", path, err)
		}
		return value
	}
	n32 := func(path string) uint32 {
		value := n(path)
		if value > 1<<32-1 {
			failure = fmt.Errorf("%s no cabe en 32 bits", path)
		}
		return uint32(value)
	}
	message := &persistencev1.Catalog{
		CatalogVersion: "bench",
		Drop: &persistencev1.DropRules{
			WaterCost:    n("rules.drop.water_cost_liters"),
			Humidity:     n("rules.drop.humidity_points"),
			DemoHumidity: n("rules.drop.demo_humidity_points"),
		},
		Tank: &persistencev1.TankRules{
			Capacity:              n("rules.tank.capacity_liters"),
			RefillAmount:          n("rules.tank.refill_liters"),
			RefillIntervalSeconds: n32("rules.tank.refill_interval_seconds"),
		},
		Growth: &persistencev1.GrowthRules{
			HumidityPerStage: n("rules.growth.humidity_per_stage_points"),
			LeavesGrass:      n("rules.growth.leaves_grass"),
			LeavesShrub:      n("rules.growth.leaves_shrub"),
			LeavesFlowers:    n("rules.growth.leaves_flowers"),
			LeavesTree:       n("rules.growth.leaves_tree"),
			TreeMaxHumidity:  n("rules.growth.tree_max_humidity_points"),
		},
		Evaporation: &persistencev1.EvaporationRules{
			HumidityPerSecond: n("rules.evaporation.humidity_per_second_points"),
			WitherAfterTicks:  n32("rules.evaporation.wither_after_ticks"),
		},
		Fire: &persistencev1.FireRules{
			DropsToExtinguish:     n32("rules.fire.drops_to_extinguish"),
			ExtinguishBonusLeaves: n("rules.fire.extinguish_bonus_leaves"),
			BurnOutTicks:          n32("rules.fire.burn_out_ticks"),
			IgnitionChancePpm:     n32("rules.fire.ignition_chance_ppm"),
			PropagationChancePpm:  n32("rules.fire.propagation_chance_ppm"),
		},
		Sprinkler: &persistencev1.SprinklerRules{
			Price:           n("installations.sprinkler.price_leaves"),
			AreaSide:        n32("installations.sprinkler.area_side_tiles"),
			Humidity:        n("installations.sprinkler.humidity_points"),
			IntervalSeconds: n32("installations.sprinkler.interval_seconds"),
		},
		Drip: &persistencev1.DripRules{
			Price:              n("installations.drip.price_leaves"),
			AreaSide:           n32("installations.drip.area_side_tiles"),
			EvaporationDivisor: n32("installations.drip.evaporation_divisor"),
		},
		TreatmentPlant: &persistencev1.TreatmentPlantRules{
			Price:           n("installations.treatment_plant.price_leaves"),
			RefillAmount:    n("installations.treatment_plant.refill_liters"),
			IntervalSeconds: n32("installations.treatment_plant.interval_seconds"),
		},
		InstallationPriceIncreasePercent: n32("rules.installation_price_increase_percent"),
		BiggerDrop: &persistencev1.BiggerDropRules{
			Price:    n("upgrades.bigger_drop.price_leaves"),
			AreaSide: n32("upgrades.bigger_drop.area_side_tiles"),
		},
		BiggerTank: &persistencev1.BiggerTankRules{
			Price:         n("upgrades.bigger_tank.price_leaves"),
			ExtraCapacity: n("upgrades.bigger_tank.extra_capacity_liters"),
		},
		OfflineInstallationSeconds: n("rules.offline_installation_seconds"),
	}
	return message, failure
}

func fnv1a(data []byte) uint64 {
	hash := uint64(0xcbf29ce484222325)
	for _, b := range data {
		hash = (hash ^ uint64(b)) * 0x00000100000001b3
	}
	return hash
}

// Run ejecuta la carga: calentamiento y ticks medidos. Solo se cronometra
// World.Step; la generación de comandos queda fuera. root es la raíz del
// repositorio (para el catálogo).
func Run(w Workload, root string) (Row, error) {
	catalog, err := LoadCatalog(root, w)
	if err != nil {
		return Row{}, err
	}
	world := domain.NewWorldWithSize(catalog, w.Seed, w.Width, w.Height)
	generator := NewGenerator(w)
	row := Row{Workload: w}
	samples := make([]int64, 0, w.MeasuredTicks)

	var before, after runtimeStats
	before.read()
	for tick := uint64(1); tick <= w.WarmupTicks+w.MeasuredTicks; tick++ {
		commands := generator.Commands(tick)
		started := time.Now()
		outcome := world.Step(commands)
		elapsed := time.Since(started)
		if tick > w.WarmupTicks {
			samples = append(samples, elapsed.Nanoseconds())
		}
		if tick%50 == 0 {
			row.MaxBurningTiles = max(row.MaxBurningTiles, countBurning(world))
		}
		for _, result := range outcome.Results {
			switch result.Outcome.Kind {
			case domain.Applied:
				row.Applied++
			case domain.Duplicate:
				row.Duplicate++
			default:
				row.Rejected++
			}
		}
	}
	after.read()
	row.GCCycles = after.numGC - before.numGC

	stats, ok := Summarize(samples)
	if !ok {
		return Row{}, fmt.Errorf("no hay muestras")
	}
	row.Stats = stats
	for i := range world.Tiles() {
		tile := &world.Tiles()[i]
		if tile.Installation != nil {
			row.Installations++
		}
		if tile.Stage == domain.Tree {
			row.Trees++
		}
	}
	row.BurningTiles = countBurning(world)
	snapshot, err := codec.EncodeSnapshot(world)
	if err != nil {
		return Row{}, err
	}
	data, err := proto.Marshal(snapshot)
	if err != nil {
		return Row{}, err
	}
	row.StateFNV1a = fnv1a(data)
	return row, nil
}

func countBurning(world *domain.World) int {
	burning := 0
	for i := range world.Tiles() {
		if world.Tiles()[i].Fire != nil {
			burning++
		}
	}
	return burning
}
