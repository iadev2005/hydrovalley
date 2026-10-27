package codec

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	persistencev1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/persistence/v1"
	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"google.golang.org/protobuf/proto"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
)

// Portadas de las pruebas de src/world/src/codec.rs, con las mismas cifras
// literales, más las propias de Go (aliasing, bytes, valores desconocidos).

const (
	ana         = "6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a01"
	d01Version  = "d01-fixture"
	adminPlayer = "aaaaaaaa-0000-4000-8000-000000000001"
)

func action(n uint32) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

// d01 son las cifras de arranque de D-01 (la copia de las pruebas, con la
// ignición en 0 ppm, igual que Catalog::d01 de Rust).
func d01() domain.Catalog {
	return domain.Catalog{
		Version:     d01Version,
		Drop:        domain.DropRules{WaterCost: 1, Humidity: 25, DemoHumidity: 1},
		Tank:        domain.TankRules{Capacity: 20, RefillAmount: 1, RefillIntervalSeconds: 2},
		Growth:      domain.GrowthRules{HumidityPerStage: 100, LeavesGrass: 1, LeavesShrub: 2, LeavesFlowers: 4, LeavesTree: 8, TreeMaxHumidity: 100},
		Evaporation: domain.EvaporationRules{HumidityPerSecond: 2, WitherAfterTicks: 100},
		Fire: domain.FireRules{
			DropsToExtinguish: 3, ExtinguishBonusLeaves: 5, BurnOutTicks: 300, IgnitionChancePPM: 0,
		},
		Sprinkler:                        domain.SprinklerRules{Price: 10, AreaSide: 3, Humidity: 25, IntervalSeconds: 2, Durability: 30},
		Drip:                             domain.DripRules{Price: 25, AreaSide: 5, EvaporationDivisor: 2, Durability: 30},
		TreatmentPlant:                   domain.TreatmentPlantRules{Price: 40, RefillAmount: 1, IntervalSeconds: 2, Durability: 30},
		InstallationPriceIncreasePercent: 15,
		BiggerDrop:                       domain.BiggerDropRules{Price: 30, AreaSide: 3},
		BiggerTank:                       domain.BiggerTankRules{Price: 20, ExtraCapacity: 10},
		OfflineInstallationSeconds:       7200,
	}
}

func newWorld() *domain.World { return domain.NewWorld(d01(), 0) }

func dropCommand(n uint32, x, y uint32) domain.Command {
	return domain.Command{ActionID: action(n), PlayerID: ana, Kind: domain.DropCommand{X: x, Y: y}}
}

func mustRestore(t *testing.T, catalog domain.Catalog, parts domain.WorldParts) *domain.World {
	t.Helper()
	w, err := domain.Restore(catalog, parts)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	return w
}

func mustEncode(t *testing.T, w *domain.World) *persistencev1.Snapshot {
	t.Helper()
	snapshot, err := EncodeSnapshot(w)
	if err != nil {
		t.Fatalf("EncodeSnapshot: %v", err)
	}
	return snapshot
}

func mustDecode(t *testing.T, snapshot *persistencev1.Snapshot, catalog domain.Catalog) *domain.World {
	t.Helper()
	w, err := DecodeSnapshot(snapshot, catalog)
	if err != nil {
		t.Fatalf("DecodeSnapshot: %v", err)
	}
	return w
}

func inner(t *testing.T, snapshot *persistencev1.Snapshot) *worldv1.WorldSnapshot {
	t.Helper()
	message := &worldv1.WorldSnapshot{}
	if err := proto.Unmarshal(snapshot.GetWorldState(), message); err != nil {
		t.Fatalf("unmarshal world_state: %v", err)
	}
	return message
}

func wantParts(t *testing.T, got, want *domain.World) {
	t.Helper()
	if !reflect.DeepEqual(got.Parts(), want.Parts()) {
		t.Errorf("parts differ:\n got %+v\nwant %+v", got.Parts(), want.Parts())
	}
}

// ---- Comandos ------------------------------------------------------------

func TestCommandsMapEveryOneofKind(t *testing.T) {
	// El tipo de la oneof no se exporta en el código generado: cada caso
	// pone su `Kind` con un cierre.
	with := func(set func(c *worldv1.Command)) *worldv1.Command {
		c := &worldv1.Command{ActionId: action(1), PlayerId: ana}
		set(c)
		return c
	}
	place := func(kind worldv1.InstallationKind, x, y uint32) *worldv1.Command {
		return with(func(c *worldv1.Command) {
			c.Kind = &worldv1.Command_PlaceInstallation{PlaceInstallation: &worldv1.PlaceInstallationCommand{Kind: kind, X: x, Y: y}}
		})
	}
	upgrade := func(kind worldv1.UpgradeKind) *worldv1.Command {
		return with(func(c *worldv1.Command) {
			c.Kind = &worldv1.Command_BuyUpgrade{BuyUpgrade: &worldv1.BuyUpgradeCommand{Kind: kind}}
		})
	}
	cases := []struct {
		name    string
		message *worldv1.Command
		want    domain.CommandKind
	}{
		{"drop", with(func(c *worldv1.Command) {
			c.Kind = &worldv1.Command_Drop{Drop: &worldv1.DropCommand{X: 3, Y: 4}}
		}), domain.DropCommand{X: 3, Y: 4}},
		{"place", place(worldv1.InstallationKind_INSTALLATION_KIND_DRIP, 5, 6), domain.PlaceInstallationCommand{Kind: domain.Drip, X: 5, Y: 6}},
		{"upgrade", upgrade(worldv1.UpgradeKind_UPGRADE_KIND_BIGGER_TANK), domain.BuyUpgradeCommand{Kind: domain.BiggerTank}},
		// Un tipo sin especificar o desconocido deja el comando sin tipo
		// (INVALID_COMMAND en el simulador).
		{"unspecified installation", place(worldv1.InstallationKind_INSTALLATION_KIND_UNSPECIFIED, 1, 1), nil},
		{"unknown installation", place(99, 1, 1), nil},
		{"unspecified upgrade", upgrade(0), nil},
		{"demo", with(func(c *worldv1.Command) {
			c.Kind = &worldv1.Command_SetDemoMode{SetDemoMode: &worldv1.SetDemoModeCommand{Enabled: true}}
		}), domain.SetDemoModeCommand{Enabled: true}},
		{"reset", with(func(c *worldv1.Command) {
			c.Kind = &worldv1.Command_ResetValley{ResetValley: &worldv1.ResetValleyCommand{}}
		}), domain.ResetValleyCommand{}},
		{"no kind", with(func(*worldv1.Command) {}), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			command := CommandFromProto(tc.message)
			if command.ActionID != action(1) || command.PlayerID != ana {
				t.Errorf("ids = %q/%q", command.ActionID, command.PlayerID)
			}
			if !reflect.DeepEqual(command.Kind, tc.want) {
				t.Errorf("kind = %#v, want %#v", command.Kind, tc.want)
			}
		})
	}
}

// Una oneof vacía por dentro (el emisor mandó el mensaje sin campos) es el
// mensaje por omisión, como en prost: la gota cae en (0, 0).
func TestAnEmptyOneofMessageIsTheDefaultMessage(t *testing.T) {
	message := &worldv1.Command{ActionId: action(1), PlayerId: ana, Kind: &worldv1.Command_Drop{}}
	if got := CommandFromProto(message).Kind; got != (domain.DropCommand{}) {
		t.Errorf("kind = %#v, want a drop at (0, 0)", got)
	}
	if got := CommandFromProto(nil); got.Kind != nil || got.ActionID != "" {
		t.Errorf("a nil message = %#v, want the empty command", got)
	}
}

// ---- Resultados ----------------------------------------------------------

func TestResultsMapStatusAndReason(t *testing.T) {
	cases := []struct {
		outcome domain.CommandOutcome
		status  worldv1.CommandStatus
		reason  worldv1.RejectReason
	}{
		{domain.AppliedOutcome, worldv1.CommandStatus_COMMAND_STATUS_APPLIED, worldv1.RejectReason_REJECT_REASON_UNSPECIFIED},
		{domain.DuplicateOutcome, worldv1.CommandStatus_COMMAND_STATUS_DUPLICATE, worldv1.RejectReason_REJECT_REASON_UNSPECIFIED},
		{domain.RejectedOutcome(domain.InsufficientWater), worldv1.CommandStatus_COMMAND_STATUS_REJECTED, worldv1.RejectReason_REJECT_REASON_INSUFFICIENT_WATER},
		{domain.RejectedOutcome(domain.OutOfBounds), worldv1.CommandStatus_COMMAND_STATUS_REJECTED, worldv1.RejectReason_REJECT_REASON_OUT_OF_BOUNDS},
		{domain.RejectedOutcome(domain.InvalidCommand), worldv1.CommandStatus_COMMAND_STATUS_REJECTED, worldv1.RejectReason_REJECT_REASON_INVALID_COMMAND},
		{domain.RejectedOutcome(domain.InsufficientLeaves), worldv1.CommandStatus_COMMAND_STATUS_REJECTED, worldv1.RejectReason_REJECT_REASON_INSUFFICIENT_LEAVES},
		{domain.RejectedOutcome(domain.TileOccupied), worldv1.CommandStatus_COMMAND_STATUS_REJECTED, worldv1.RejectReason_REJECT_REASON_TILE_OCCUPIED},
		{domain.RejectedOutcome(domain.UpgradeAlreadyOwned), worldv1.CommandStatus_COMMAND_STATUS_REJECTED, worldv1.RejectReason_REJECT_REASON_UPGRADE_ALREADY_OWNED},
		// El contrato no tiene motivo para «mundo lleno»: INVALID_COMMAND es el
		// más cercano.
		{domain.RejectedOutcome(domain.WorldFull), worldv1.CommandStatus_COMMAND_STATUS_REJECTED, worldv1.RejectReason_REJECT_REASON_INVALID_COMMAND},
	}
	for _, tc := range cases {
		result := ResultToProto(domain.CommandResult{ActionID: action(7), PlayerID: ana, Outcome: tc.outcome})
		if result.GetActionId() != action(7) || result.GetPlayerId() != ana {
			t.Errorf("%+v: ids = %q/%q", tc.outcome, result.GetActionId(), result.GetPlayerId())
		}
		if result.GetStatus() != tc.status || result.GetReason() != tc.reason {
			t.Errorf("%+v: status/reason = %v/%v, want %v/%v", tc.outcome, result.GetStatus(), result.GetReason(), tc.status, tc.reason)
		}
	}
}

// ---- Estado difundido ----------------------------------------------------

func TestWorldStateCarriesTilesPlayersAndCounters(t *testing.T) {
	w := newWorld()
	w.Step([]domain.Command{dropCommand(201, 2, 1)})
	state := WorldState(w)

	if state.GetWidth() != 32 || state.GetHeight() != 32 || len(state.GetTiles()) != 1024 {
		t.Fatalf("size = %dx%d with %d tiles", state.GetWidth(), state.GetHeight(), len(state.GetTiles()))
	}
	tile := state.GetTiles()[32+2]
	if tile.GetStage() != worldv1.TileStage_TILE_STAGE_DRY_SOIL || tile.GetHumidity() != 25 {
		t.Errorf("tile = %v/%d, want dry soil/25", tile.GetStage(), tile.GetHumidity())
	}
	if tile.GetFire() != nil || tile.GetInstallation() != nil {
		t.Error("the tile should not burn or hold an installation")
	}
	if state.GetHumidityContributed() != 25 || state.GetDemoMode() || state.GetEpoch() != 0 {
		t.Errorf("counters = %d/%v/%d", state.GetHumidityContributed(), state.GetDemoMode(), state.GetEpoch())
	}
	if len(state.GetPlayers()) != 1 {
		t.Fatalf("players = %d, want 1", len(state.GetPlayers()))
	}
	player := state.GetPlayers()[0]
	if player.GetPlayerId() != ana || player.GetWater() != 19 || player.GetTankCapacity() != 20 {
		t.Errorf("player = %v/%d/%d, want ana/19/20", player.GetPlayerId(), player.GetWater(), player.GetTankCapacity())
	}
}

func TestWorldStateListsTheNextPriceOfEveryInstallationKind(t *testing.T) {
	parts := newWorld().Parts()
	parts.Players = []domain.Player{{
		PlayerID: ana, Water: 20, TankCapacity: 20, Leaves: 100, WaterRefilled: 20, LeavesEarned: 100,
		Purchases: []domain.Purchase{{Kind: domain.Sprinkler, Count: 2}, {Kind: domain.Drip, Count: 1}},
	}}
	state := WorldState(mustRestore(t, d01(), parts))

	type price struct {
		kind   worldv1.InstallationKind
		leaves uint64
	}
	var got []price
	for _, p := range state.GetPlayers()[0].GetNextPrices() {
		got = append(got, price{p.GetKind(), p.GetLeaves()})
	}
	// Aspersor 10 -> 12 -> 14; goteo 25 -> 29; planta sin compras.
	want := []price{
		{worldv1.InstallationKind_INSTALLATION_KIND_SPRINKLER, 14},
		{worldv1.InstallationKind_INSTALLATION_KIND_DRIP, 29},
		{worldv1.InstallationKind_INSTALLATION_KIND_TREATMENT_PLANT, 40},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("next prices = %v, want %v", got, want)
	}
}

// ---- Instantánea ---------------------------------------------------------

// richWorld es rich_world de Rust: un mundo con incendio, instalación, épocas
// y estado del generador puestos a mano.
func richWorld(t *testing.T) *domain.World {
	t.Helper()
	w := newWorld()
	w.Step([]domain.Command{dropCommand(1, 0, 0), dropCommand(2, 31, 31)})
	parts := w.Parts()
	parts.Epoch = 3
	parts.RngState = []byte{1, 2, 3, 4, 5, 6, 7, 8}
	parts.Tiles[5] = domain.Tile{
		Stage: domain.Flowers, Humidity: 40, DryTicks: 7,
		Fire:         &domain.Fire{DropsReceived: 2, BurningTicks: 90},
		Installation: &domain.Installation{Kind: domain.Drip, OwnerID: ana},
	}
	parts.Players[0].Leaves = 9
	parts.Players[0].LeavesEarned = 12
	parts.Players[0].Purchases = []domain.Purchase{{Kind: domain.Sprinkler, Count: 2}}
	parts.Players[0].Upgrades = []domain.UpgradeKind{domain.BiggerTank}
	return mustRestore(t, d01(), parts)
}

func TestSnapshotRoundTripsThroughTheWireFormat(t *testing.T) {
	w := richWorld(t)
	snapshot := mustEncode(t, w)

	if snapshot.GetTick() != 1 || snapshot.GetEpoch() != 3 || snapshot.GetCatalogVersion() != d01Version {
		t.Errorf("row = %d/%d/%q, want 1/3/%q", snapshot.GetTick(), snapshot.GetEpoch(), snapshot.GetCatalogVersion(), d01Version)
	}
	message := inner(t, snapshot)
	if message.GetCatalogVersion() != d01Version || message.GetTick() != 1 {
		t.Errorf("inner = %q/%d", message.GetCatalogVersion(), message.GetTick())
	}
	if len(snapshot.GetPlayers()) != 1 || snapshot.GetPlayers()[0].GetPlayerId() != ana || snapshot.GetPlayers()[0].GetLeavesEarned() != 12 {
		t.Errorf("player rows = %v", snapshot.GetPlayers())
	}
	if got := len(message.GetPlayers()[0].GetRecentActions()); got != 2 {
		t.Errorf("recent actions = %d, want 2", got)
	}
	wantParts(t, mustDecode(t, snapshot, d01()), w)
}

func TestWorldStateAndSnapshotCarryTheFiresAndTheGeneratorState(t *testing.T) {
	w := richWorld(t)
	state := WorldState(w)
	fire := state.GetTiles()[5].GetFire()
	if fire == nil || fire.GetDropsReceived() != 2 || fire.GetBurningTicks() != 90 {
		t.Fatalf("fire = %v, want drops 2 / ticks 90", fire)
	}
	burning := 0
	for _, tile := range state.GetTiles() {
		if tile.GetFire() != nil {
			burning++
		}
	}
	if burning != 1 {
		t.Errorf("burning tiles = %d, want 1", burning)
	}

	snapshot := mustEncode(t, w)
	if got := inner(t, snapshot).GetRngState(); !reflect.DeepEqual(got, []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Errorf("rng_state = %v", got)
	}
	restored := mustDecode(t, snapshot, d01())
	if got := restored.Parts().RngState; !reflect.DeepEqual(got, []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Errorf("restored rng_state = %v", got)
	}
	tile, _ := restored.Tile(5, 0)
	if tile.Fire == nil || *tile.Fire != (domain.Fire{DropsReceived: 2, BurningTicks: 90}) {
		t.Errorf("restored fire = %v", tile.Fire)
	}
}

func TestInstallationsUpgradesAndThe3RCountersProducedByTheRulesSurviveTheWire(t *testing.T) {
	parts := newWorld().Parts()
	parts.Players = []domain.Player{{
		PlayerID: ana, Water: 20, TankCapacity: 20, Leaves: 200, WaterRefilled: 20, LeavesEarned: 200,
	}}
	w := mustRestore(t, d01(), parts)
	place := func(n uint32, kind domain.InstallationKind, x, y uint32) domain.Command {
		return domain.Command{ActionID: action(n), PlayerID: ana, Kind: domain.PlaceInstallationCommand{Kind: kind, X: x, Y: y}}
	}
	buy := func(n uint32, kind domain.UpgradeKind) domain.Command {
		return domain.Command{ActionID: action(n), PlayerID: ana, Kind: domain.BuyUpgradeCommand{Kind: kind}}
	}
	w.Step([]domain.Command{
		place(1, domain.Sprinkler, 6, 6),
		place(2, domain.Drip, 8, 6),
		place(3, domain.TreatmentPlant, 1, 1),
		buy(4, domain.BiggerDrop),
		buy(5, domain.BiggerTank),
		dropCommand(6, 7, 6),
	})
	for range 60 {
		w.Step(nil)
	}
	player, _ := w.Player(ana)
	if player.WaterReused == 0 || player.WaterReduced == 0 || player.WaterRecycled == 0 {
		t.Fatalf("the 3R counters are not all above 0: %+v", player)
	}

	restored := mustDecode(t, mustEncode(t, w), d01())
	wantParts(t, restored, w)

	state := WorldState(restored)
	installed := 0
	for _, tile := range state.GetTiles() {
		if tile.GetInstallation() != nil {
			installed++
		}
	}
	if installed != 3 {
		t.Errorf("installations = %d, want 3", installed)
	}
	p := state.GetPlayers()[0]
	if p.GetTankCapacity() != 30 || len(p.GetUpgrades()) != 2 {
		t.Errorf("capacity/upgrades = %d/%d, want 30/2", p.GetTankCapacity(), len(p.GetUpgrades()))
	}
}

func TestTheStateProducedByTheTickRulesSurvivesTheWire(t *testing.T) {
	// dry_ticks, época, modo demo y contadores de RF-27 salen de las reglas,
	// no de una instantánea armada a mano.
	command := func(n uint32, player string, kind domain.CommandKind) domain.Command {
		return domain.Command{ActionID: action(n), PlayerID: player, Kind: kind}
	}
	w := newWorld()
	w.Step([]domain.Command{command(1, adminPlayer, domain.ResetValleyCommand{})})
	var growing []domain.Command
	for n := uint32(10); n < 14; n++ {
		growing = append(growing, command(n, ana, domain.DropCommand{X: 4, Y: 4}))
	}
	w.Step(growing) // pasto con humedad 0: empieza a secarse
	for range 30 {
		w.Step(nil)
	}
	w.Step([]domain.Command{command(2, adminPlayer, domain.SetDemoModeCommand{Enabled: true})})

	restored := mustDecode(t, mustEncode(t, w), d01())
	wantParts(t, restored, w)
	parts := restored.Parts()
	if parts.Epoch != 1 || !parts.DemoMode || parts.Tiles[4*32+4].DryTicks != 31 {
		t.Errorf("epoch/demo/dry = %d/%v/%d, want 1/true/31", parts.Epoch, parts.DemoMode, parts.Tiles[4*32+4].DryTicks)
	}
	for _, player := range parts.Players {
		if player.PlayerID != ana {
			continue
		}
		if player.Leaves != 1 || player.LeavesEarned != 1 || player.WaterRefilled != 21 || player.WaterSpent != 4 {
			t.Errorf("ana = %+v", player)
		}
	}
}

func TestDecodeRejectsGarbageAndInconsistentRows(t *testing.T) {
	w := newWorld()

	garbage := mustEncode(t, w)
	garbage.WorldState = []byte{0xff, 0xff, 0xff}
	var malformed *MalformedError
	if _, err := DecodeSnapshot(garbage, d01()); !errors.As(err, &malformed) {
		t.Errorf("garbage: err = %v, want MalformedError", err)
	}

	mismatch := mustEncode(t, w)
	mismatch.Tick = 50
	var tickMismatch *TickMismatchError
	if _, err := DecodeSnapshot(mismatch, d01()); !errors.As(err, &tickMismatch) || *tickMismatch != (TickMismatchError{Row: 50, World: 0}) {
		t.Errorf("mismatch: err = %v", err)
	}

	unknownStage := inner(t, mustEncode(t, w))
	unknownStage.Tiles[0].Stage = 99
	row := mustEncode(t, w)
	row.WorldState = marshal(t, unknownStage)
	var unknown *UnknownEnumError
	if _, err := DecodeSnapshot(row, d01()); !errors.As(err, &unknown) || unknown.Field != "Tile.stage" || unknown.Value != 99 {
		t.Errorf("unknown stage: err = %v", err)
	}

	short := inner(t, mustEncode(t, w))
	short.Tiles = short.Tiles[:len(short.Tiles)-1]
	row = mustEncode(t, w)
	row.WorldState = marshal(t, short)
	var restoreFailure *RestoreFailure
	if _, err := DecodeSnapshot(row, d01()); !errors.As(err, &restoreFailure) {
		t.Errorf("short: err = %v, want RestoreFailure", err)
	} else if want := (domain.TileCountError{Expected: 1024, Found: 1023}); !errors.Is(err, want) {
		t.Errorf("short: err = %v, want to wrap %v", err, want)
	}

	if _, err := DecodeSnapshot(nil, d01()); err == nil {
		t.Error("a nil row was accepted")
	}
}

func marshal(t *testing.T, message *worldv1.WorldSnapshot) []byte {
	t.Helper()
	out, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestUnknownEnumsInsideTheSnapshotAreRefusedWithTheirField(t *testing.T) {
	w := richWorld(t)
	cases := []struct {
		field  string
		break_ func(*worldv1.WorldSnapshot)
	}{
		{"Installation.kind", func(m *worldv1.WorldSnapshot) { m.Tiles[5].Installation.Kind = 7 }},
		{"InstallationCount.kind", func(m *worldv1.WorldSnapshot) { m.Players[0].Purchases[0].Kind = 0 }},
		{"PlayerRecord.upgrades", func(m *worldv1.WorldSnapshot) { m.Players[0].Upgrades[0] = 9 }},
		{"Tile.stage", func(m *worldv1.WorldSnapshot) { m.Tiles[0].Stage = 0 }},
	}
	for _, tc := range cases {
		message := inner(t, mustEncode(t, w))
		tc.break_(message)
		row := mustEncode(t, w)
		row.WorldState = marshal(t, message)
		var unknown *UnknownEnumError
		if _, err := DecodeSnapshot(row, d01()); !errors.As(err, &unknown) || unknown.Field != tc.field {
			t.Errorf("%s: err = %v", tc.field, err)
		}
	}
}

// Un world_state con rng_state vacío (anterior a los incendios) se restaura
// con la semilla del tick; uno con un estado de otro largo se rechaza, igual
// que en Rust.
func TestDecodeFollowsTheRngRulesOfRestore(t *testing.T) {
	w := newWorld()
	w.Step(nil)
	message := inner(t, mustEncode(t, w))

	message.RngState = nil
	row := mustEncode(t, w)
	row.WorldState = marshal(t, message)
	restored := mustDecode(t, row, d01())
	if want := domain.NewSplitMix64(1).Bytes(); !reflect.DeepEqual(restored.Parts().RngState, want) {
		t.Errorf("rng_state = %v, want %v", restored.Parts().RngState, want)
	}

	message.RngState = []byte{1, 2, 3}
	row.WorldState = marshal(t, message)
	if _, err := DecodeSnapshot(row, d01()); !errors.Is(err, domain.RngStateError{Len: 3}) {
		t.Errorf("err = %v, want RngStateError{3}", err)
	}
}

// El mundo restaurado usa el catálogo vigente y no el de la instantánea.
func TestDecodeAppliesTheCurrentCatalog(t *testing.T) {
	snapshot := mustEncode(t, newWorld())
	current := d01()
	current.Version = "vigente"
	current.Drop.Humidity = 40
	restored := mustDecode(t, snapshot, current)
	if restored.CatalogVersion() != "vigente" {
		t.Errorf("version = %q", restored.CatalogVersion())
	}
	restored.Step([]domain.Command{dropCommand(1, 0, 0)})
	if tile, _ := restored.Tile(0, 0); tile.Humidity != 40 {
		t.Errorf("humidity = %d, want 40", tile.Humidity)
	}
}

// Go-only: la salida es determinista (mismo estado, mismos bytes) y
// codificar lo decodificado devuelve los mismos bytes.
func TestEncodingIsDeterministicAndStableAcrossADecode(t *testing.T) {
	w := richWorld(t)
	first, second := mustEncode(t, w), mustEncode(t, w)
	if !proto.Equal(first, second) {
		t.Fatal("two encodings of the same world differ")
	}
	again := mustEncode(t, mustDecode(t, first, d01()))
	if !reflect.DeepEqual(first.GetWorldState(), again.GetWorldState()) {
		t.Error("decode + encode changed the world_state bytes")
	}
}

// Go-only: codificar no deja memoria compartida con el mundo.
func TestEncodingDoesNotShareMemoryWithTheWorld(t *testing.T) {
	w := richWorld(t)
	before := w.Parts()
	state := WorldState(w)
	state.Tiles[5].Fire.BurningTicks = 1
	snapshot := mustEncode(t, w)
	snapshot.WorldState[0] ^= 0xff
	if !reflect.DeepEqual(w.Parts(), before) {
		t.Error("changing the messages changed the world")
	}
}

// ---- Catálogo ------------------------------------------------------------

// d01Message es el catálogo de D-01 tal como lo entrega GetCatalog.
func d01Message() *persistencev1.Catalog {
	return &persistencev1.Catalog{
		CatalogVersion: d01Version,
		Drop:           &persistencev1.DropRules{WaterCost: 1, Humidity: 25, DemoHumidity: 1},
		Tank:           &persistencev1.TankRules{Capacity: 20, RefillAmount: 1, RefillIntervalSeconds: 2},
		Growth: &persistencev1.GrowthRules{
			HumidityPerStage: 100, LeavesGrass: 1, LeavesShrub: 2, LeavesFlowers: 4, LeavesTree: 8, TreeMaxHumidity: 100,
		},
		Evaporation: &persistencev1.EvaporationRules{HumidityPerSecond: 2, WitherAfterTicks: 100},
		Fire: &persistencev1.FireRules{
			DropsToExtinguish: 3, ExtinguishBonusLeaves: 5, BurnOutTicks: 300, IgnitionChancePpm: 0,
		},
		Sprinkler:                        &persistencev1.SprinklerRules{Price: 10, AreaSide: 3, Humidity: 25, IntervalSeconds: 2, Durability: 30},
		Drip:                             &persistencev1.DripRules{Price: 25, AreaSide: 5, EvaporationDivisor: 2, Durability: 30},
		TreatmentPlant:                   &persistencev1.TreatmentPlantRules{Price: 40, RefillAmount: 1, IntervalSeconds: 2, Durability: 30},
		InstallationPriceIncreasePercent: 15,
		BiggerDrop:                       &persistencev1.BiggerDropRules{Price: 30, AreaSide: 3},
		BiggerTank:                       &persistencev1.BiggerTankRules{Price: 20, ExtraCapacity: 10},
		OfflineInstallationSeconds:       7200,
	}
}

func TestACatalogMessageBecomesTheDomainCatalog(t *testing.T) {
	got, err := CatalogFromProto(d01Message())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, d01()) {
		t.Errorf("catalog = %+v, want %+v", got, d01())
	}
}

func TestTheDemoHumidityOfTheDropIsCarriedOver(t *testing.T) {
	message := d01Message()
	message.Drop.DemoHumidity = 7
	got, err := CatalogFromProto(message)
	if err != nil || got.Drop.DemoHumidity != 7 {
		t.Errorf("demo humidity = %d (err %v), want 7", got.Drop.DemoHumidity, err)
	}
}

func TestTheIgnitionChanceAndChangedFiguresAreCarriedOver(t *testing.T) {
	message := d01Message()
	message.Fire.IgnitionChancePpm = 500
	message.Fire.PropagationChancePpm = 1500
	message.Sprinkler.Price = 12
	got, err := CatalogFromProto(message)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fire.IgnitionChancePPM != 500 || got.Sprinkler.Price != 12 {
		t.Errorf("ppm/price = %d/%d, want 500/12", got.Fire.IgnitionChancePPM, got.Sprinkler.Price)
	}
	if got.Fire.PropagationChancePPM != 1500 {
		t.Errorf("propagation ppm = %d, want 1500", got.Fire.PropagationChancePPM)
	}
}

func TestAnIncompleteOrImpossibleCatalogIsRejected(t *testing.T) {
	breaks := []struct {
		name    string
		breakIt func(*persistencev1.Catalog)
	}{
		{"nil message", nil},
		{"version", func(c *persistencev1.Catalog) { c.CatalogVersion = "" }},
		{"drop", func(c *persistencev1.Catalog) { c.Drop = nil }},
		{"tank", func(c *persistencev1.Catalog) { c.Tank = nil }},
		{"growth", func(c *persistencev1.Catalog) { c.Growth = nil }},
		{"evaporation", func(c *persistencev1.Catalog) { c.Evaporation = nil }},
		{"fire", func(c *persistencev1.Catalog) { c.Fire = nil }},
		{"sprinkler", func(c *persistencev1.Catalog) { c.Sprinkler = nil }},
		{"drip", func(c *persistencev1.Catalog) { c.Drip = nil }},
		{"treatment_plant", func(c *persistencev1.Catalog) { c.TreatmentPlant = nil }},
		{"bigger_drop", func(c *persistencev1.Catalog) { c.BiggerDrop = nil }},
		{"bigger_tank", func(c *persistencev1.Catalog) { c.BiggerTank = nil }},
		{"stage", func(c *persistencev1.Catalog) { c.Growth.HumidityPerStage = 0 }},
		{"wither", func(c *persistencev1.Catalog) { c.Evaporation.WitherAfterTicks = 0 }},
		{"sprinkler area", func(c *persistencev1.Catalog) { c.Sprinkler.AreaSide = 0 }},
		{"drip divisor", func(c *persistencev1.Catalog) { c.Drip.EvaporationDivisor = 0 }},
		{"demo humidity", func(c *persistencev1.Catalog) { c.Drop.DemoHumidity = 0 }},
		{"refill interval", func(c *persistencev1.Catalog) { c.Tank.RefillIntervalSeconds = 0 }},
		// Los demás grupos de cifras que dividen o recorren intervalos.
		{"extinguish drops", func(c *persistencev1.Catalog) { c.Fire.DropsToExtinguish = 0 }},
		{"burn out", func(c *persistencev1.Catalog) { c.Fire.BurnOutTicks = 0 }},
		{"sprinkler interval", func(c *persistencev1.Catalog) { c.Sprinkler.IntervalSeconds = 0 }},
		{"plant interval", func(c *persistencev1.Catalog) { c.TreatmentPlant.IntervalSeconds = 0 }},
		{"bigger drop area", func(c *persistencev1.Catalog) { c.BiggerDrop.AreaSide = 0 }},
		{"drip area", func(c *persistencev1.Catalog) { c.Drip.AreaSide = 0 }},
	}
	for _, tc := range breaks {
		t.Run(tc.name, func(t *testing.T) {
			message := d01Message()
			if tc.breakIt == nil {
				message = nil
			} else {
				tc.breakIt(message)
			}
			var invalid *InvalidCatalogError
			if _, err := CatalogFromProto(message); !errors.As(err, &invalid) {
				t.Errorf("err = %v, want InvalidCatalogError", err)
			}
		})
	}
}

// ---- Resumen al volver (RF-25) --------------------------------------------

func worldWithSummary(t *testing.T, summary domain.OfflineSummary) *domain.World {
	t.Helper()
	parts := newWorld().Parts()
	parts.Players = []domain.Player{{
		PlayerID: ana, Water: 20, TankCapacity: 20, WaterRefilled: 20,
		OfflineSummary: summary, FiresExtinguished: 4, TilesGrown: 9,
	}}
	return mustRestore(t, d01(), parts)
}

var nonEmptySummary = domain.OfflineSummary{LeavesEarned: 7, WaterReused: 450, FiresExtinguished: 2, TilesGrown: 3}

func TestAnEmptySummaryIsNotWrittenSoOldSnapshotsKeepTheirBytes(t *testing.T) {
	w := worldWithSummary(t, domain.OfflineSummary{})
	snapshot, err := EncodeSnapshot(w)
	if err != nil {
		t.Fatal(err)
	}
	message := &worldv1.WorldSnapshot{}
	if err := proto.Unmarshal(snapshot.GetWorldState(), message); err != nil {
		t.Fatal(err)
	}
	if message.GetPlayers()[0].OfflineSummary != nil {
		t.Error("the record carries an empty offline summary")
	}
	if WorldState(w).GetPlayers()[0].OfflineSummary != nil {
		t.Error("the state carries an empty offline summary")
	}
}

func TestASummarySurvivesTheSnapshotAndReachesTheWorldState(t *testing.T) {
	w := worldWithSummary(t, nonEmptySummary)
	snapshot, err := EncodeSnapshot(w)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeSnapshot(snapshot, d01())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Parts(), w.Parts()) {
		t.Error("the restored world differs from the original")
	}
	summary := WorldState(restored).GetPlayers()[0].GetOfflineSummary()
	got := [4]uint64{summary.GetLeavesEarned(), summary.GetWaterReused(), summary.GetFiresExtinguished(), summary.GetTilesGrown()}
	if got != [4]uint64{7, 450, 2, 3} {
		t.Errorf("summary in the state = %v, want [7 450 2 3]", got)
	}
}

func TestTheSnapshotRowsForPersistenceCarryTheSprinklerCounters(t *testing.T) {
	snapshot, err := EncodeSnapshot(worldWithSummary(t, nonEmptySummary))
	if err != nil {
		t.Fatal(err)
	}
	row := snapshot.GetPlayers()[0]
	if row.GetFiresExtinguished() != 4 || row.GetTilesGrown() != 9 {
		t.Errorf("row = %d/%d, want 4/9", row.GetFiresExtinguished(), row.GetTilesGrown())
	}
}
