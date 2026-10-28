package conformance

// Prueba de conformidad Rust <-> Go (T7 de B5). Los guiones de
// tests/conformance/scenarios (semilla, catálogo y comandos por tick) se
// ejecutan con el dominio y el codec de Go y se comparan con los archivos de
// tests/conformance/expected, que escribe el ejecutor de Rust
// (src/world/tests/conformance.rs). Si Rust cambia a propósito, se regeneran
// esos archivos (ver tests/conformance/README.md); si Go se aparta, esta
// prueba lo dice con el primer tick que difiere.
//
// El ejecutor es el espejo del de Rust: mismos pasos y mismo formato de
// salida, para comparar estructuras y no texto.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	persistencev1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/persistence/v1"
	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"google.golang.org/protobuf/proto"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/codec"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
)

// root es la raíz del repositorio (este paquete vive en src/world-go/internal).
const root = "../../../.."

func conformanceDir() string { return filepath.Join(root, "tests", "conformance") }

// ---- Guion ---------------------------------------------------------------

type catalogSpec struct {
	File    string            `json:"file"`
	Version string            `json:"version"`
	Set     map[string]uint64 `json:"set"`
}

type commandSpec struct {
	Player            string          `json:"player"`
	Action            json.RawMessage `json:"action"`
	Drop              *xy             `json:"drop"`
	PlaceInstallation *placeSpec      `json:"place_installation"`
	BuyUpgrade        *kindSpec       `json:"buy_upgrade"`
	SetDemoMode       *struct {
		Enabled bool `json:"enabled"`
	} `json:"set_demo_mode"`
	ResetValley *struct{} `json:"reset_valley"`
	CleanTrash  *xy       `json:"clean_trash"`
}

type xy struct {
	X uint32 `json:"x"`
	Y uint32 `json:"y"`
}

type placeSpec struct {
	Kind json.RawMessage `json:"kind"`
	X    uint32          `json:"x"`
	Y    uint32          `json:"y"`
}

type kindSpec struct {
	Kind json.RawMessage `json:"kind"`
}

type scenarioFile struct {
	Seed         uint64            `json:"seed"`
	Catalog      catalogSpec       `json:"catalog"`
	ResetCatalog *catalogSpec      `json:"reset_catalog"`
	Players      map[string]string `json:"players"`
	TotalTicks   uint64            `json:"total_ticks"`
	RestoreAt    []uint64          `json:"restore_at"`
	Ticks        []struct {
		Tick     uint64        `json:"tick"`
		Commands []commandSpec `json:"commands"`
	} `json:"ticks"`
}

type scenario struct {
	name         string
	seed         uint64
	catalog      domain.Catalog
	resetCatalog *domain.Catalog
	totalTicks   uint64
	restoreAt    []uint64
	commands     map[uint64][]*worldv1.Command
}

func scenarioPaths(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(conformanceDir(), "scenarios", "*.json"))
	if err != nil || len(paths) < 4 {
		t.Fatalf("faltan guiones en tests/conformance/scenarios (%d, %v)", len(paths), err)
	}
	sort.Strings(paths)
	return paths
}

func loadScenario(t *testing.T, path string) scenario {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file scenarioFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	s := scenario{
		name:       strings.TrimSuffix(filepath.Base(path), ".json"),
		seed:       file.Seed,
		catalog:    catalogFromSpec(t, file.Catalog),
		totalTicks: file.TotalTicks,
		restoreAt:  file.RestoreAt,
		commands:   make(map[uint64][]*worldv1.Command),
	}
	if file.ResetCatalog != nil {
		reset := catalogFromSpec(t, *file.ResetCatalog)
		s.resetCatalog = &reset
	}
	for _, entry := range file.Ticks {
		if _, repeated := s.commands[entry.Tick]; repeated {
			t.Fatalf("%s: tick repetido: %d", s.name, entry.Tick)
		}
		list := make([]*worldv1.Command, len(entry.Commands))
		for i, spec := range entry.Commands {
			list[i] = commandMessage(t, spec, file.Players)
		}
		s.commands[entry.Tick] = list
	}
	return s
}

// catalogFromSpec es el archivo base con las cifras cambiadas (Set),
// convertido en el mensaje de GetCatalog y validado por el codec.
func catalogFromSpec(t *testing.T, spec catalogSpec) domain.Catalog {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, spec.File))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	for path, value := range spec.Set {
		setNumber(t, doc, path, value)
	}
	catalog, err := codec.CatalogFromProto(catalogMessage(t, doc, spec.Version))
	if err != nil {
		t.Fatalf("el catálogo del guion no es válido: %v", err)
	}
	return catalog
}

// number sigue una ruta con puntos hasta un entero.
func number(t *testing.T, doc map[string]any, path string) uint64 {
	t.Helper()
	var node any = doc
	for _, key := range strings.Split(path, ".") {
		object, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("la ruta %s no existe en el catálogo", path)
		}
		if node, ok = object[key]; !ok {
			t.Fatalf("la ruta %s no existe en el catálogo", path)
		}
	}
	text, ok := node.(json.Number)
	if !ok {
		t.Fatalf("%s no es un entero", path)
	}
	value, err := strconv.ParseUint(text.String(), 10, 64)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return value
}

// setNumber cambia el entero de una ruta que ya existe (un nombre mal escrito
// en un guion es un error, no un campo nuevo).
func setNumber(t *testing.T, doc map[string]any, path string, value uint64) {
	t.Helper()
	number(t, doc, path) // comprueba que existe y es un entero
	keys := strings.Split(path, ".")
	object := doc
	for _, key := range keys[:len(keys)-1] {
		object = object[key].(map[string]any)
	}
	object[keys[len(keys)-1]] = json.Number(strconv.FormatUint(value, 10))
}

func catalogMessage(t *testing.T, doc map[string]any, version string) *persistencev1.Catalog {
	n := func(path string) uint64 { return number(t, doc, path) }
	n32 := func(path string) uint32 {
		value := number(t, doc, path)
		if value > 1<<32-1 {
			t.Fatalf("%s no cabe en 32 bits", path)
		}
		return uint32(value)
	}
	return &persistencev1.Catalog{
		CatalogVersion: version,
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
			Durability:      n32("installations.sprinkler.durability"),
		},
		Drip: &persistencev1.DripRules{
			Price:               n("installations.drip.price_leaves"),
			AreaSide:            n32("installations.drip.area_side_tiles"),
			EvaporationDivisor:  n32("installations.drip.evaporation_divisor"),
			Durability:          n32("installations.drip.durability"),
			WearIntervalSeconds: n32("installations.drip.wear_interval_seconds"),
		},
		TreatmentPlant: &persistencev1.TreatmentPlantRules{
			Price:           n("installations.treatment_plant.price_leaves"),
			RefillAmount:    n("installations.treatment_plant.refill_liters"),
			IntervalSeconds: n32("installations.treatment_plant.interval_seconds"),
			Durability:      n32("installations.treatment_plant.durability"),
		},
		TrashBin: &persistencev1.TrashBinRules{
			Price:           n("installations.trash_bin.price_leaves"),
			AreaSide:        n32("installations.trash_bin.area_side_tiles"),
			IntervalSeconds: n32("installations.trash_bin.interval_seconds"),
			Durability:      n32("installations.trash_bin.durability"),
		},
		Terrain: &persistencev1.TerrainRules{
			RiverWidth: n32("rules.terrain.river_width_tiles"),
			RockCount:  n32("rules.terrain.rock_count"),
		},
		Urban: &persistencev1.UrbanRules{
			BuildingTreeThreshold:      n32("rules.urban.building_tree_threshold"),
			BuildingHumidityThreshold:  n("rules.urban.building_humidity_points_threshold"),
			MaxBuildings:               n32("rules.urban.max_buildings"),
			TrashIntervalSeconds:       n32("rules.urban.trash_interval_seconds"),
			TrashSpreadIntervalSeconds: n32("rules.urban.trash_spread_interval_seconds"),
			CleanRewardLeaves:          n("rules.urban.clean_reward_leaves"),
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
}

// enumValue lee el `kind` de un comando: ausente es 0, una cadena es un
// nombre de la tabla names y un número se pasa tal cual (así los guiones
// pueden mandar valores desconocidos).
func enumValue(t *testing.T, raw json.RawMessage, names map[string]int32) int32 {
	t.Helper()
	if len(raw) == 0 {
		return 0
	}
	if raw[0] == '"' {
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			t.Fatal(err)
		}
		value, ok := names[name]
		if !ok {
			t.Fatalf("tipo desconocido en el guion: %s", name)
		}
		return value
	}
	var value int32
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

var (
	installationNames = map[string]int32{
		"sprinkler":       int32(worldv1.InstallationKind_INSTALLATION_KIND_SPRINKLER),
		"drip":            int32(worldv1.InstallationKind_INSTALLATION_KIND_DRIP),
		"treatment_plant": int32(worldv1.InstallationKind_INSTALLATION_KIND_TREATMENT_PLANT),
		"trash_bin":       int32(worldv1.InstallationKind_INSTALLATION_KIND_TRASH_BIN),
	}
	upgradeNames = map[string]int32{
		"bigger_drop": int32(worldv1.UpgradeKind_UPGRADE_KIND_BIGGER_DROP),
		"bigger_tank": int32(worldv1.UpgradeKind_UPGRADE_KIND_BIGGER_TANK),
	}
)

func commandMessage(t *testing.T, spec commandSpec, players map[string]string) *worldv1.Command {
	t.Helper()
	playerID, named := players[spec.Player]
	if !named {
		playerID = spec.Player
	}
	var action string
	if len(spec.Action) > 0 && spec.Action[0] == '"' {
		if err := json.Unmarshal(spec.Action, &action); err != nil {
			t.Fatal(err)
		}
	} else {
		var n uint64
		if err := json.Unmarshal(spec.Action, &n); err != nil {
			t.Fatalf("action no es un número ni una cadena: %s", spec.Action)
		}
		action = fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
	}
	message := &worldv1.Command{ActionId: action, PlayerId: playerID}
	switch {
	case spec.Drop != nil:
		message.Kind = &worldv1.Command_Drop{Drop: &worldv1.DropCommand{X: spec.Drop.X, Y: spec.Drop.Y}}
	case spec.PlaceInstallation != nil:
		message.Kind = &worldv1.Command_PlaceInstallation{PlaceInstallation: &worldv1.PlaceInstallationCommand{
			Kind: worldv1.InstallationKind(enumValue(t, spec.PlaceInstallation.Kind, installationNames)),
			X:    spec.PlaceInstallation.X,
			Y:    spec.PlaceInstallation.Y,
		}}
	case spec.BuyUpgrade != nil:
		message.Kind = &worldv1.Command_BuyUpgrade{BuyUpgrade: &worldv1.BuyUpgradeCommand{
			Kind: worldv1.UpgradeKind(enumValue(t, spec.BuyUpgrade.Kind, upgradeNames)),
		}}
	case spec.SetDemoMode != nil:
		message.Kind = &worldv1.Command_SetDemoMode{SetDemoMode: &worldv1.SetDemoModeCommand{Enabled: spec.SetDemoMode.Enabled}}
	case spec.ResetValley != nil:
		message.Kind = &worldv1.Command_ResetValley{ResetValley: &worldv1.ResetValleyCommand{}}
	case spec.CleanTrash != nil:
		message.Kind = &worldv1.Command_CleanTrash{CleanTrash: &worldv1.CleanTrashCommand{X: spec.CleanTrash.X, Y: spec.CleanTrash.Y}}
	}
	return message
}

// ---- Resultado -----------------------------------------------------------

// output es lo que se compara entre implementaciones; las etiquetas son las
// del archivo esperado que escribe Rust.
type output struct {
	Scenario            string        `json:"scenario"`
	TotalTicks          uint64        `json:"total_ticks"`
	FinalTick           uint64        `json:"final_tick"`
	HumidityContributed []uint64      `json:"humidity_contributed"`
	BurningTiles        []int         `json:"burning_tiles"`
	FiresPutOut         uint64        `json:"fires_put_out"`
	FiresGoneToDrySoil  uint64        `json:"fires_gone_to_dry_soil"`
	Results             []tickResults `json:"results"`
	Checkpoints         []checkpoint  `json:"checkpoints"`
	Restores            []restore     `json:"restores"`
	FinalSnapshotHex    string        `json:"final_snapshot_hex"`
	FinalWorldStateHex  string        `json:"final_world_state_hex"`
}

type tickResults struct {
	Tick  uint64   `json:"tick"`
	Codes []string `json:"r"`
}

type checkpoint struct {
	Tick          uint64 `json:"tick"`
	SnapshotFNV   string `json:"snapshot_fnv1a"`
	WorldStateFNV string `json:"world_state_fnv1a"`
}

type restore struct {
	Tick        uint64 `json:"tick"`
	SnapshotHex string `json:"snapshot_hex"`
}

// fnv1a es el FNV-1a de 64 bits: huella corta de los bytes de una
// instantánea.
func fnv1a(data []byte) string {
	hash := uint64(0xcbf29ce484222325)
	for _, b := range data {
		hash ^= uint64(b)
		hash *= 0x100000001b3
	}
	return fmt.Sprintf("%016x", hash)
}

func resultCode(result *worldv1.CommandResult) string {
	status := strings.ToLower(strings.TrimPrefix(result.GetStatus().String(), "COMMAND_STATUS_"))
	if result.GetStatus() == worldv1.CommandStatus_COMMAND_STATUS_REJECTED {
		reason := strings.ToLower(strings.TrimPrefix(result.GetReason().String(), "REJECT_REASON_"))
		return status + ":" + reason
	}
	return status
}

func marshal(t *testing.T, message proto.Message) []byte {
	t.Helper()
	data, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func encodeRow(t *testing.T, w *domain.World) []byte {
	t.Helper()
	row, err := codec.EncodeSnapshot(w)
	if err != nil {
		t.Fatal(err)
	}
	return marshal(t, row)
}

func decodeRow(t *testing.T, data []byte, catalog domain.Catalog) *domain.World {
	t.Helper()
	row := &persistencev1.Snapshot{}
	if err := proto.Unmarshal(data, row); err != nil {
		t.Fatalf("la fila de instantánea no se lee: %v", err)
	}
	w, err := codec.DecodeSnapshot(row, catalog)
	if err != nil {
		t.Fatalf("la instantánea no se restaura: %v", err)
	}
	return w
}

// run ejecuta el guion desde el principio.
func run(t *testing.T, s scenario, restoreAt []uint64) output {
	t.Helper()
	world := domain.NewWorld(s.catalog, s.seed)
	return continueRun(t, s, world, 1, s.catalog, restoreAt)
}

// continueRun ejecuta los ticks from..totalTicks del guion sobre world, cuyo
// catálogo vigente (el que se aplica al restaurar) es current.
func continueRun(t *testing.T, s scenario, world *domain.World, from uint64, current domain.Catalog, restoreAt []uint64) output {
	t.Helper()
	out := output{Scenario: s.name, TotalTicks: s.totalTicks}
	wasBurning := make([]bool, len(world.Tiles()))
	for i, tile := range world.Tiles() {
		wasBurning[i] = tile.Fire != nil
	}
	for tick := from; tick <= s.totalTicks; tick++ {
		messages := s.commands[tick]
		commands := make([]domain.Command, len(messages))
		for i, message := range messages {
			commands[i] = codec.CommandFromProto(message)
		}
		step := world.StepWithResetCatalog(commands, s.resetCatalog)
		if step.Tick != tick {
			t.Fatalf("%s: el paso devolvió el tick %d, se esperaba %d", s.name, step.Tick, tick)
		}
		if s.resetCatalog != nil {
			for i, command := range commands {
				if _, isReset := command.Kind.(domain.ResetValleyCommand); isReset && step.Results[i].Outcome == domain.AppliedOutcome {
					current = *s.resetCatalog
				}
			}
		}
		if len(step.Results) > 0 {
			codes := make([]string, len(step.Results))
			for i, result := range step.Results {
				codes[i] = resultCode(codec.ResultToProto(result))
			}
			out.Results = append(out.Results, tickResults{Tick: tick, Codes: codes})
		}
		burning := 0
		for i, tile := range world.Tiles() {
			if wasBurning[i] && tile.Fire == nil {
				if tile.Stage == domain.DrySoil {
					out.FiresGoneToDrySoil++
				} else {
					out.FiresPutOut++
				}
			}
			wasBurning[i] = tile.Fire != nil
			if tile.Fire != nil {
				burning++
			}
		}
		out.HumidityContributed = append(out.HumidityContributed, world.HumidityContributed())
		out.BurningTiles = append(out.BurningTiles, burning)
		if tick%50 == 0 {
			state := marshal(t, codec.WorldState(world))
			out.Checkpoints = append(out.Checkpoints, checkpoint{
				Tick: tick, SnapshotFNV: fnv1a(encodeRow(t, world)), WorldStateFNV: fnv1a(state),
			})
		}
		if slicesContain(restoreAt, tick) {
			row := encodeRow(t, world)
			out.Restores = append(out.Restores, restore{Tick: tick, SnapshotHex: hex.EncodeToString(row)})
			world = decodeRow(t, row, current)
		}
	}
	out.FinalTick = world.Tick()
	out.FinalSnapshotHex = hex.EncodeToString(encodeRow(t, world))
	out.FinalWorldStateHex = hex.EncodeToString(marshal(t, codec.WorldState(world)))
	out.normalize()
	return out
}

// normalize hace que "sin elementos" sea siempre una lista vacía (el archivo
// esperado trae [] y una ejecución sin restauraciones, nil).
func (o *output) normalize() {
	if o.Results == nil {
		o.Results = []tickResults{}
	}
	if o.Checkpoints == nil {
		o.Checkpoints = []checkpoint{}
	}
	if o.Restores == nil {
		o.Restores = []restore{}
	}
}

func slicesContain(items []uint64, value uint64) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func loadExpected(t *testing.T, name string) output {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(conformanceDir(), "expected", name+".json"))
	if err != nil {
		t.Fatalf("falta el resultado esperado de %s (lo escribe Rust, ver tests/conformance/README.md): %v", name, err)
	}
	var expected output
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	expected.normalize()
	return expected
}

// diff dice el primer punto en que dos ejecuciones difieren (vacío si no).
func diff(got, want output) string {
	if got.FinalTick != want.FinalTick {
		return fmt.Sprintf("final_tick = %d, Rust %d", got.FinalTick, want.FinalTick)
	}
	for i := range min(len(got.HumidityContributed), len(want.HumidityContributed)) {
		if got.HumidityContributed[i] != want.HumidityContributed[i] {
			return fmt.Sprintf("humidity_contributed difiere en el tick %d: %d, Rust %d", i+1, got.HumidityContributed[i], want.HumidityContributed[i])
		}
	}
	for i := range min(len(got.Results), len(want.Results)) {
		if !reflect.DeepEqual(got.Results[i], want.Results[i]) {
			return fmt.Sprintf("resultados distintos en el tick %d: %v, Rust %v", want.Results[i].Tick, got.Results[i].Codes, want.Results[i].Codes)
		}
	}
	for i := range min(len(got.BurningTiles), len(want.BurningTiles)) {
		if got.BurningTiles[i] != want.BurningTiles[i] {
			return fmt.Sprintf("casillas que arden distintas en el tick %d: %d, Rust %d", i+1, got.BurningTiles[i], want.BurningTiles[i])
		}
	}
	for i := range min(len(got.Checkpoints), len(want.Checkpoints)) {
		if got.Checkpoints[i] != want.Checkpoints[i] {
			return fmt.Sprintf("la instantánea del tick %d difiere: %+v, Rust %+v", want.Checkpoints[i].Tick, got.Checkpoints[i], want.Checkpoints[i])
		}
	}
	if !reflect.DeepEqual(got, want) {
		return "difiere en un campo que no es ni la humedad ni los resultados ni las huellas (ver restores, fuegos o los bytes finales)"
	}
	return ""
}

// ---- Pruebas -------------------------------------------------------------

// El mismo guion da el mismo estado en Go y en Rust, tick a tick y en los
// bytes de la instantánea y del WorldState finales.
func TestEveryScenarioGivesTheSameStateAsRust(t *testing.T) {
	for _, path := range scenarioPaths(t) {
		s := loadScenario(t, path)
		t.Run(s.name, func(t *testing.T) {
			got := run(t, s, s.restoreAt)
			if message := diff(got, loadExpected(t, s.name)); message != "" {
				t.Error(message)
			}
		})
	}
}

// Restaurar desde una instantánea a mitad del guion no cambia el resultado.
func TestRestoringFromASnapshotNeverChangesTheOutcome(t *testing.T) {
	for _, path := range scenarioPaths(t) {
		s := loadScenario(t, path)
		t.Run(s.name, func(t *testing.T) {
			straight := run(t, s, nil)
			var every25 []uint64
			for tick := uint64(25); tick < s.totalTicks; tick += 25 {
				every25 = append(every25, tick)
			}
			restored := run(t, s, every25)
			if len(restored.Restores) != len(every25) {
				t.Fatalf("restauraciones = %d, want %d", len(restored.Restores), len(every25))
			}
			restored.Restores = []restore{}
			if message := diff(restored, straight); message != "" {
				t.Error(message)
			}
		})
	}
}

// catalogAt es el catálogo vigente tras el tick dado, según los resultados de
// Rust: el del guion, o el ofrecido al reinicio una vez aplicado un reinicio.
func catalogAt(s scenario, expected output, tick uint64) domain.Catalog {
	if s.resetCatalog == nil {
		return s.catalog
	}
	for _, entry := range expected.Results {
		if entry.Tick > tick {
			break
		}
		for i, message := range s.commands[entry.Tick] {
			if _, isReset := message.GetKind().(*worldv1.Command_ResetValley); isReset && entry.Codes[i] == "applied" {
				return *s.resetCatalog
			}
		}
	}
	return s.catalog
}

// Plan B de D-03: Go retoma el valle desde una instantánea que escribió Rust.
// La instantánea de cada restauración (tomada por Rust a mitad del guion) se
// lee en Go, se vuelve a escribir con los mismos bytes y, siguiendo el guion
// desde ahí, da el mismo estado final que la ejecución de Rust.
func TestGoResumesTheValleyFromSnapshotsWrittenByRust(t *testing.T) {
	resumed := 0
	for _, path := range scenarioPaths(t) {
		s := loadScenario(t, path)
		expected := loadExpected(t, s.name)
		for _, saved := range expected.Restores {
			t.Run(fmt.Sprintf("%s/tick-%d", s.name, saved.Tick), func(t *testing.T) {
				data, err := hex.DecodeString(saved.SnapshotHex)
				if err != nil {
					t.Fatal(err)
				}
				current := catalogAt(s, expected, saved.Tick)
				world := decodeRow(t, data, current)
				if world.Tick() != saved.Tick {
					t.Fatalf("la instantánea es del tick %d, se esperaba %d", world.Tick(), saved.Tick)
				}
				if !bytes.Equal(encodeRow(t, world), data) {
					t.Error("leer y volver a escribir la instantánea de Rust no da los mismos bytes")
				}
				got := continueRun(t, s, world, saved.Tick+1, current, nil)
				if got.FinalSnapshotHex != expected.FinalSnapshotHex {
					t.Error("seguir el guion desde la instantánea de Rust no da el mismo estado final")
				}
				if got.FinalWorldStateHex != expected.FinalWorldStateHex {
					t.Error("el WorldState final difiere")
				}
				for i, humidity := range got.HumidityContributed {
					if want := expected.HumidityContributed[int(saved.Tick)+i]; humidity != want {
						t.Fatalf("humidity_contributed difiere en el tick %d: %d, Rust %d", int(saved.Tick)+i+1, humidity, want)
					}
				}
			})
			resumed++
		}
	}
	if resumed < 5 {
		t.Errorf("solo se retomaron %d instantáneas de Rust", resumed)
	}
}

// Las instantáneas finales de Rust se leen en Go y las de Go en Rust: los
// bytes son los mismos, y el estado que dan al leerse también.
func TestSnapshotBytesAreIdenticalAcrossLanguages(t *testing.T) {
	for _, path := range scenarioPaths(t) {
		s := loadScenario(t, path)
		t.Run(s.name, func(t *testing.T) {
			expected := loadExpected(t, s.name)
			rustBytes, err := hex.DecodeString(expected.FinalSnapshotHex)
			if err != nil {
				t.Fatal(err)
			}
			current := catalogAt(s, expected, s.totalTicks)
			fromRust := decodeRow(t, rustBytes, current)

			goWorld := domain.NewWorld(s.catalog, s.seed)
			goOut := continueRun(t, s, goWorld, 1, s.catalog, nil)
			goBytes, err := hex.DecodeString(goOut.FinalSnapshotHex)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(goBytes, rustBytes) {
				t.Fatal("los bytes de la instantánea final de Go y de Rust difieren")
			}
			fromGo := decodeRow(t, goBytes, current)
			if !reflect.DeepEqual(fromGo.Parts(), fromRust.Parts()) {
				t.Error("los mismos bytes dieron estados distintos")
			}

			// El WorldState de Rust se lee y se vuelve a escribir igual.
			stateBytes, err := hex.DecodeString(expected.FinalWorldStateHex)
			if err != nil {
				t.Fatal(err)
			}
			state := &worldv1.WorldState{}
			if err := proto.Unmarshal(stateBytes, state); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(marshal(t, state), stateBytes) {
				t.Error("el WorldState de Rust no se vuelve a escribir con los mismos bytes")
			}
			if !proto.Equal(state, codec.WorldState(fromRust)) {
				t.Error("el WorldState de Go sobre la instantánea de Rust difiere del de Rust")
			}
		})
	}
}

// Los guiones tienen que ejercitar de verdad las reglas: si dejan de producir
// fuegos o rechazos, la conformidad sería vacía.
func TestTheScenariosExerciseTheRulesTheyClaimTo(t *testing.T) {
	codes := map[string]int{}
	maxBurning, putOut, gone := 0, uint64(0), uint64(0)
	for _, path := range scenarioPaths(t) {
		expected := loadExpected(t, strings.TrimSuffix(filepath.Base(path), ".json"))
		if expected.TotalTicks < 300 {
			t.Errorf("%s: solo %d ticks", expected.Scenario, expected.TotalTicks)
		}
		for _, entry := range expected.Results {
			for _, code := range entry.Codes {
				codes[code]++
			}
		}
		for _, burning := range expected.BurningTiles {
			maxBurning = max(maxBurning, burning)
		}
		putOut += expected.FiresPutOut
		gone += expected.FiresGoneToDrySoil
	}
	for _, code := range []string{
		"applied", "duplicate", "rejected:insufficient_water", "rejected:insufficient_leaves",
		"rejected:out_of_bounds", "rejected:tile_occupied", "rejected:upgrade_already_owned", "rejected:invalid_command",
	} {
		if codes[code] == 0 {
			t.Errorf("ningún guion produce %s", code)
		}
	}
	if maxBurning < 3 || putOut < 3 || gone < 3 {
		t.Errorf("fuegos insuficientes: máximo %d, apagados %d, consumidos %d", maxBurning, putOut, gone)
	}
}
