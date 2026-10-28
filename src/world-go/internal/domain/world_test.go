package domain

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// Portadas de las pruebas de world.rs que cubren T2 (gotas, humedad,
// crecimiento, evaporación, marchitamiento y depósito). Mismas cifras
// literales que en Rust.

const (
	ana  = "6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a01"
	beto = "9a0b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c02"
)

func action(n uint32) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

func dropCmd(actionID, playerID string, x, y uint32) Command {
	return Command{ActionID: actionID, PlayerID: playerID, Kind: DropCommand{X: x, Y: y}}
}

func newTestWorld() *World { return NewWorld(d01(), 0) }

// setDemoMode es el atajo de las pruebas que no quieren gastar un tick (en
// Rust, World::set_demo_mode bajo cfg(test)); en producción el modo demo solo
// cambia con SetDemoModeCommand.
func (w *World) setDemoMode(enabled bool) { w.demoMode = enabled }

func outcomes(step StepOutcome) []CommandOutcome {
	var out []CommandOutcome
	for _, r := range step.Results {
		out = append(out, r.Outcome)
	}
	return out
}

func mustPlayer(t *testing.T, w *World, id string) *Player {
	t.Helper()
	p, ok := w.Player(id)
	if !ok {
		t.Fatalf("player %s is not in the world", id)
	}
	return p
}

func mustTile(t *testing.T, w *World, x, y uint32) *Tile {
	t.Helper()
	tile, ok := w.Tile(x, y)
	if !ok {
		t.Fatalf("tile (%d, %d) is out of the valley", x, y)
	}
	return tile
}

func wantOutcomes(t *testing.T, got []CommandOutcome, want ...CommandOutcome) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("outcomes = %v, want %v", got, want)
	}
}

// idle avanza count ticks sin comandos.
func idle(w *World, count uint64) {
	for range count {
		w.Step(nil)
	}
}

// advanceTo avanza hasta que el mundo quede en tick.
func advanceTo(t *testing.T, w *World, tick uint64) {
	t.Helper()
	if w.Tick() > tick {
		t.Fatalf("the world is already at tick %d, past %d", w.Tick(), tick)
	}
	idle(w, tick-w.Tick())
}

// drops son n gotas del jugador sobre (x, y), con action_id distintos a
// partir de first.
func drops(first, n uint32, player string, x, y uint32) []Command {
	var out []Command
	for k := first; k < first+n; k++ {
		out = append(out, dropCmd(action(k), player, x, y))
	}
	return out
}

// assertRF27: agua = recargada - gastada y hojas = ganadas - gastadas.
func assertRF27(t *testing.T, w *World) {
	t.Helper()
	for _, p := range w.Players() {
		if p.Water != p.WaterRefilled-p.WaterSpent {
			t.Errorf("water %s: %d != %d - %d", p.PlayerID, p.Water, p.WaterRefilled, p.WaterSpent)
		}
		if p.Leaves != p.LeavesEarned-p.LeavesSpent {
			t.Errorf("leaves %s: %d != %d - %d", p.PlayerID, p.Leaves, p.LeavesEarned, p.LeavesSpent)
		}
	}
}

func TestNewWorldIsAnEmpty32x32ValleyAtTickZero(t *testing.T) {
	w := newTestWorld()
	if w.Tick() != 0 {
		t.Errorf("tick = %d, want 0", w.Tick())
	}
	if w.Width() != 32 || w.Height() != 32 || len(w.Tiles()) != 1024 {
		t.Errorf("valley is %dx%d with %d tiles", w.Width(), w.Height(), len(w.Tiles()))
	}
	for _, tile := range w.Tiles() {
		if tile != (Tile{}) {
			t.Fatal("a new world has non-empty tiles")
		}
	}
	if mustTile(t, w, 0, 0).Stage != DrySoil {
		t.Error("tile (0, 0) is not dry soil")
	}
	if len(w.Players()) != 0 || w.HumidityContributed() != 0 || w.DemoMode() {
		t.Error("a new world has no players, no humidity and no demo mode")
	}
}

func TestTileLookupIsRowMajorAndBounded(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{dropCmd(action(1), ana, 3, 2)})
	if w.Tiles()[2*32+3].Humidity != 25 {
		t.Error("the tile at index 2*32+3 should hold the drop")
	}
	if mustTile(t, w, 3, 2).Humidity != 25 {
		t.Error("Tile(3, 2) should hold the drop")
	}
	if _, ok := w.Tile(32, 0); ok {
		t.Error("x = 32 is out of the valley")
	}
	if _, ok := w.Tile(0, 32); ok {
		t.Error("y = 32 is out of the valley")
	}
}

func TestEveryStepAdvancesTheTickByOneEvenWithoutCommands(t *testing.T) {
	w := newTestWorld()
	if got := w.Step(nil).Tick; got != 1 {
		t.Errorf("first step tick = %d, want 1", got)
	}
	if got := w.Step(nil).Tick; got != 2 {
		t.Errorf("second step tick = %d, want 2", got)
	}
	if w.Tick() != 2 {
		t.Errorf("tick = %d, want 2", w.Tick())
	}
}

func TestDropCostsOneLitreAndAdds25Humidity(t *testing.T) {
	w := newTestWorld()
	out := w.Step([]Command{dropCmd(action(1), ana, 14, 9)})

	if out.Tick != 1 {
		t.Errorf("tick = %d, want 1", out.Tick)
	}
	want := []CommandResult{{ActionID: action(1), PlayerID: ana, Outcome: AppliedOutcome}}
	if !slices.Equal(out.Results, want) {
		t.Errorf("results = %v, want %v", out.Results, want)
	}
	p := mustPlayer(t, w, ana)
	if p.Water != 19 || p.TankCapacity != 20 || p.WaterSpent != 1 {
		t.Errorf("water/capacity/spent = %d/%d/%d, want 19/20/1", p.Water, p.TankCapacity, p.WaterSpent)
	}
	if p.WaterRefilled != 20 {
		t.Errorf("the full tank counts as refilled: got %d", p.WaterRefilled)
	}
	if p.LastActionTick != 1 {
		t.Errorf("last action tick = %d, want 1", p.LastActionTick)
	}
	if !slices.Equal(p.RecentActions, []AppliedAction{{ActionID: action(1), Tick: 1}}) {
		t.Errorf("recent actions = %v", p.RecentActions)
	}
	if mustTile(t, w, 14, 9).Humidity != 25 || w.HumidityContributed() != 25 {
		t.Error("the tile and the contributed humidity should be 25")
	}
}

func TestDemoDropIsFreeAndAddsOneHumidity(t *testing.T) {
	w := newTestWorld()
	w.setDemoMode(true)
	out := w.Step([]Command{dropCmd(action(1), ana, 0, 0)})

	wantOutcomes(t, outcomes(out), AppliedOutcome)
	p := mustPlayer(t, w, ana)
	if p.Water != 20 || p.WaterSpent != 0 {
		t.Errorf("water/spent = %d/%d, want 20/0", p.Water, p.WaterSpent)
	}
	if mustTile(t, w, 0, 0).Humidity != 1 || w.HumidityContributed() != 1 {
		t.Error("the tile and the contributed humidity should be 1")
	}
}

func TestDropOutOfBoundsIsRejectedWithoutCharging(t *testing.T) {
	w := newTestWorld()
	out := w.Step([]Command{dropCmd(action(1), ana, 32, 0), dropCmd(action(2), ana, 0, 32)})

	oob := RejectedOutcome(OutOfBounds)
	wantOutcomes(t, outcomes(out), oob, oob)
	if _, ok := w.Player(ana); ok {
		t.Error("a rejected command does not bring the player in")
	}
	if w.HumidityContributed() != 0 {
		t.Error("nothing should be contributed")
	}
}

func TestDropWithoutWaterIsRejectedAndDemoModeIgnoresTheTank(t *testing.T) {
	w := newTestWorld()
	out := w.Step(drops(0, 21, ana, 1, 1))

	for i, r := range out.Results[:20] {
		if r.Outcome != AppliedOutcome {
			t.Fatalf("drop %d: %v, want applied", i, r.Outcome)
		}
	}
	if got := out.Results[20].Outcome; got != RejectedOutcome(InsufficientWater) {
		t.Errorf("drop 20: %v, want insufficient water", got)
	}
	if mustPlayer(t, w, ana).Water != 0 {
		t.Error("the tank should be empty")
	}
	if w.HumidityContributed() != 20*25 {
		t.Errorf("contributed = %d, want 500", w.HumidityContributed())
	}

	w.setDemoMode(true)
	out = w.Step([]Command{dropCmd(action(100), ana, 1, 1)})
	wantOutcomes(t, outcomes(out), AppliedOutcome)
	if mustPlayer(t, w, ana).Water != 0 {
		t.Error("demo mode does not touch the tank")
	}
}

func TestResultsFollowRequestOrderAcrossPlayers(t *testing.T) {
	w := newTestWorld()
	out := w.Step([]Command{
		dropCmd(action(1), beto, 2, 2),
		dropCmd(action(2), ana, 40, 2),
		dropCmd(action(3), ana, 2, 2),
	})
	var got [][2]string
	for _, r := range out.Results {
		got = append(got, [2]string{r.ActionID, r.PlayerID})
	}
	want := [][2]string{{action(1), beto}, {action(2), ana}, {action(3), ana}}
	if !slices.Equal(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
	if mustTile(t, w, 2, 2).Humidity != 50 {
		t.Error("the two applied drops should add 50")
	}
}

func TestRepeatedActionIDInTheSameTickIsADuplicate(t *testing.T) {
	w := newTestWorld()
	out := w.Step([]Command{dropCmd(action(1), ana, 5, 5), dropCmd(action(1), ana, 5, 5)})
	wantOutcomes(t, outcomes(out), AppliedOutcome, DuplicateOutcome)
	if mustTile(t, w, 5, 5).Humidity != 25 || mustPlayer(t, w, ana).Water != 19 {
		t.Error("the duplicate must not apply twice")
	}
}

func TestDuplicatesAreTrackedPerPlayer(t *testing.T) {
	w := newTestWorld()
	out := w.Step([]Command{dropCmd(action(1), ana, 5, 5), dropCmd(action(1), beto, 5, 5)})
	wantOutcomes(t, outcomes(out), AppliedOutcome, AppliedOutcome)
}

func TestRejectedActionsAreNotRemembered(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{dropCmd(action(1), ana, 99, 5)})
	out := w.Step([]Command{dropCmd(action(1), ana, 5, 5)})
	wantOutcomes(t, outcomes(out), AppliedOutcome)
}

func TestMalformedCommandsAreInvalidAndDoNotCreatePlayers(t *testing.T) {
	w := newTestWorld()
	out := w.Step([]Command{
		dropCmd("not-a-uuid", ana, 1, 1),
		dropCmd(action(1), "not-a-uuid", 1, 1),
		{ActionID: action(2), PlayerID: ana, Kind: nil},
	})
	invalid := RejectedOutcome(InvalidCommand)
	wantOutcomes(t, outcomes(out), invalid, invalid, invalid)
	if _, ok := w.Player("not-a-uuid"); ok {
		t.Error("a malformed player id must not create a player")
	}
	if _, ok := w.Player(ana); ok {
		t.Error("a command without kind does not bring the player in")
	}
	if w.HumidityContributed() != 0 {
		t.Error("nothing should be contributed")
	}
}

func TestAPlayerEntersTheWorldWithTheFirstAppliedCommand(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{dropCmd(action(1), ana, 40, 0)})
	if _, ok := w.Player(ana); ok {
		t.Fatal("a rejected command does not bring the player in")
	}
	out := w.Step([]Command{dropCmd(action(2), ana, 1, 1)})
	wantOutcomes(t, outcomes(out), AppliedOutcome)
	if mustPlayer(t, w, ana).Water != 19 {
		t.Error("the new player should have spent one litre")
	}
}

func TestNewPlayersBeyondTheCapAreRejected(t *testing.T) {
	w := newTestWorld()
	var fill []Command
	for n := range uint32(MaxPlayers) {
		fill = append(fill, dropCmd(action(n), fmt.Sprintf("00000000-0000-4000-9000-%012d", n), 0, 0))
	}
	for i, r := range w.Step(fill).Results {
		if r.Outcome != AppliedOutcome {
			t.Fatalf("filling command %d: %v", i, r.Outcome)
		}
	}
	if len(w.Players()) != MaxPlayers {
		t.Fatalf("players = %d, want %d", len(w.Players()), MaxPlayers)
	}

	out := w.Step([]Command{
		dropCmd(action(1), ana, 1, 1),
		dropCmd(action(2), "00000000-0000-4000-9000-000000000000", 1, 1),
	})
	wantOutcomes(t, outcomes(out), RejectedOutcome(WorldFull), AppliedOutcome)
	if _, ok := w.Player(ana); ok {
		t.Error("the newcomer must not enter a full world")
	}
	if len(w.Players()) != MaxPlayers {
		t.Errorf("players = %d, want %d", len(w.Players()), MaxPlayers)
	}
}

func TestPlayersAreSortedByID(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{dropCmd(action(1), beto, 0, 0), dropCmd(action(2), ana, 1, 1)})
	ids := []string{}
	for _, p := range w.Players() {
		ids = append(ids, p.PlayerID)
	}
	if !slices.Equal(ids, []string{ana, beto}) {
		t.Errorf("ids = %v, want [%s %s]", ids, ana, beto)
	}
}

func TestSnapshotTicksAreTheMultiplesOf50(t *testing.T) {
	cases := map[uint64]bool{0: false, 1: false, 49: false, 50: true, 51: false, 100: true}
	for tick, want := range cases {
		if got := IsSnapshotTick(tick); got != want {
			t.Errorf("IsSnapshotTick(%d) = %v, want %v", tick, got, want)
		}
	}
}

// ---- Catálogo propio --------------------------------------------------

func customCatalog() Catalog {
	c := d01()
	c.Version = "custom"
	c.Drop.WaterCost = 2
	c.Drop.Humidity = 40
	c.Tank.Capacity = 30
	return c
}

func TestADropUsesTheFiguresOfTheCatalog(t *testing.T) {
	w := NewWorld(customCatalog(), 0)
	out := w.Step([]Command{dropCmd(action(1), ana, 1, 1)})

	wantOutcomes(t, outcomes(out), AppliedOutcome)
	p := w.Players()[0]
	if p.Water != 28 || p.TankCapacity != 30 || p.WaterSpent != 2 {
		t.Errorf("water/capacity/spent = %d/%d/%d, want 28/30/2", p.Water, p.TankCapacity, p.WaterSpent)
	}
	if mustTile(t, w, 1, 1).Humidity != 40 || w.HumidityContributed() != 40 {
		t.Error("the tile and the contributed humidity should be 40")
	}
}

func TestANewPlayerStartsWithTheFullTankOfTheCatalog(t *testing.T) {
	c := d01()
	c.Tank.Capacity = 1
	c.Drop.WaterCost = 2
	w := NewWorld(c, 0)
	out := w.Step([]Command{dropCmd(action(1), ana, 1, 1)})

	wantOutcomes(t, outcomes(out), RejectedOutcome(InsufficientWater))
	if len(w.Players()) != 0 {
		t.Error("the rejected newcomer must not enter")
	}
}

func TestADemoDropUsesTheDemoHumidityOfTheCatalog(t *testing.T) {
	c := customCatalog()
	c.Drop.DemoHumidity = 3
	w := NewWorld(c, 0)
	w.setDemoMode(true)
	w.Step([]Command{dropCmd(action(1), ana, 1, 1)})

	if mustTile(t, w, 1, 1).Humidity != 3 || w.HumidityContributed() != 3 {
		t.Error("the tile and the contributed humidity should be 3")
	}
	if w.Players()[0].Water != 30 {
		t.Error("a demo drop is free")
	}
}

func TestTheWorldReportsTheVersionOfItsCatalog(t *testing.T) {
	if got := NewWorld(customCatalog(), 0).CatalogVersion(); got != "custom" {
		t.Errorf("version = %q, want custom", got)
	}
}

// ---- Reglas por tick --------------------------------------------------

func TestTheTankRefillsOneLitreEvery20Ticks(t *testing.T) {
	w := newTestWorld()
	w.Step(drops(1, 5, ana, 0, 0)) // tick 1: 15 L
	for _, c := range []struct{ tick, water uint64 }{{19, 15}, {20, 16}, {39, 16}, {40, 17}} {
		advanceTo(t, w, c.tick)
		if got := mustPlayer(t, w, ana).Water; got != c.water {
			t.Errorf("tick %d: water = %d, want %d", c.tick, got, c.water)
		}
	}
	assertRF27(t, w)
}

func TestAFullTankDoesNotOverflowAndCountsNothing(t *testing.T) {
	w := newTestWorld()
	w.Step(drops(1, 1, ana, 0, 0))
	advanceTo(t, w, 20) // vuelve a 20 L
	advanceTo(t, w, 100)
	p := mustPlayer(t, w, ana)
	if p.Water != 20 || p.WaterRefilled != 21 {
		t.Errorf("water/refilled = %d/%d, want 20/21", p.Water, p.WaterRefilled)
	}
	assertRF27(t, w)
}

func TestTheRefillIsCappedAtTheCapacityAndCountsOnlyWhatFits(t *testing.T) {
	c := d01()
	c.Tank.RefillAmount = 5
	w := NewWorld(c, 0)
	w.Step(drops(1, 3, ana, 0, 0)) // 17 L
	advanceTo(t, w, 20)
	p := mustPlayer(t, w, ana)
	if p.Water != 20 || p.WaterRefilled != 23 {
		t.Errorf("water/refilled = %d/%d, want 20/23", p.Water, p.WaterRefilled)
	}
	assertRF27(t, w)
}

func TestEveryPlayerRefillsEvenWhenIdle(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{dropCmd(action(1), ana, 0, 0), dropCmd(action(2), beto, 1, 1)})
	advanceTo(t, w, 20)
	if mustPlayer(t, w, ana).Water != 20 || mustPlayer(t, w, beto).Water != 20 {
		t.Error("both players should be back at 20 L")
	}
}

func TestTheRefillIntervalComesFromTheCatalog(t *testing.T) {
	c := d01()
	c.Tank.RefillIntervalSeconds = 1
	w := NewWorld(c, 0)
	w.Step(drops(1, 4, ana, 0, 0))
	for _, s := range []struct{ tick, water uint64 }{{9, 16}, {10, 17}, {20, 18}} {
		advanceTo(t, w, s.tick)
		if got := mustPlayer(t, w, ana).Water; got != s.water {
			t.Errorf("tick %d: water = %d, want %d", s.tick, got, s.water)
		}
	}
}

// Con intervalo 0 Rust no entra en pánico: is_multiple_of(0) solo vale para
// el tick 0, que nunca se calcula, así que no se recarga nunca (world.rs:469).
func TestARefillIntervalOfZeroNeverRefills(t *testing.T) {
	c := d01()
	c.Tank.RefillIntervalSeconds = 0
	w := NewWorld(c, 0)
	w.Step(drops(1, 4, ana, 0, 0))
	idle(w, 100)
	if got := mustPlayer(t, w, ana).Water; got != 16 {
		t.Errorf("water = %d, want 16", got)
	}
}

func TestATileLosesTheCatalogHumidityEverySecond(t *testing.T) {
	w := newTestWorld()
	w.Step(drops(1, 2, ana, 4, 4)) // 50
	for _, s := range []struct{ tick, humidity uint64 }{{9, 50}, {10, 48}, {20, 46}} {
		advanceTo(t, w, s.tick)
		if got := mustTile(t, w, 4, 4).Humidity; got != s.humidity {
			t.Errorf("tick %d: humidity = %d, want %d", s.tick, got, s.humidity)
		}
	}
	if w.HumidityContributed() != 50 {
		t.Error("evaporation never touches the contributed humidity")
	}
}

func TestEvaporationNeverGoesBelowZeroAndUsesTheCatalog(t *testing.T) {
	c := d01()
	c.Evaporation.HumidityPerSecond = 30
	w := NewWorld(c, 0)
	w.Step(drops(1, 2, ana, 4, 4)) // 50
	for _, s := range []struct{ tick, humidity uint64 }{{10, 20}, {20, 0}, {30, 0}} {
		advanceTo(t, w, s.tick)
		if got := mustTile(t, w, 4, 4).Humidity; got != s.humidity {
			t.Errorf("tick %d: humidity = %d, want %d", s.tick, got, s.humidity)
		}
	}
}

func TestTheValleyEvolvesEvenIfNobodyPlays(t *testing.T) {
	w := newTestWorld()
	w.Step(drops(1, 1, ana, 7, 7))
	advanceTo(t, w, 100) // RF-28: solo pasa el tiempo
	if got := mustTile(t, w, 7, 7).Humidity; got != 25-2*10 {
		t.Errorf("humidity = %d, want 5", got)
	}
}

func TestTheDropThatReachesTheThresholdGrowsTheTileAndPaysItsPlayer(t *testing.T) {
	w := newTestWorld()
	w.Step(drops(1, 3, ana, 2, 2))
	if mustTile(t, w, 2, 2).Stage != DrySoil || mustPlayer(t, w, ana).Leaves != 0 {
		t.Fatal("three drops are not enough to grow")
	}

	w.Step(drops(4, 1, ana, 2, 2))
	tile := mustTile(t, w, 2, 2)
	if tile.Stage != Grass || tile.Humidity != 0 {
		t.Errorf("tile = (%d, %d), want (Grass, 0)", tile.Stage, tile.Humidity)
	}
	p := mustPlayer(t, w, ana)
	if p.Leaves != 1 || p.LeavesEarned != 1 || p.LeavesSpent != 0 {
		t.Errorf("leaves/earned/spent = %d/%d/%d, want 1/1/0", p.Leaves, p.LeavesEarned, p.LeavesSpent)
	}
	if w.HumidityContributed() != 100 {
		t.Error("growth does not touch the contributed humidity")
	}
	assertRF27(t, w)
}

func TestTheLeavesGoToThePlayerWhoseDropPushedTheTileOver(t *testing.T) {
	w := newTestWorld()
	// Mismo tick y orden de admisión: Ana pone 75, Beto la empuja al 100.
	commands := drops(1, 3, ana, 2, 2)
	commands = append(commands,
		dropCmd(action(10), beto, 2, 2),
		dropCmd(action(11), ana, 2, 2), // ya es de otra etapa
	)
	w.Step(commands)
	if mustPlayer(t, w, beto).Leaves != 1 || mustPlayer(t, w, ana).Leaves != 0 {
		t.Error("the leaves belong to Beto")
	}
	if mustTile(t, w, 2, 2).Humidity != 25 {
		t.Error("the last drop should leave 25")
	}
	assertRF27(t, w)
}

func TestATileClimbsThroughAllTheStagesWithTheCatalogLeaves(t *testing.T) {
	c := d01()
	c.Tank.Capacity = 100
	w := NewWorld(c, 0)
	stages := []TileStage{DrySoil}
	for round := range uint32(4) {
		// Cuatro gotas por etapa, en ticks seguidos (antes de que evaporar,
		// que toca en el tick 10, reste nada).
		w.Step(drops(round*10, 4, ana, 3, 3))
		stages = append(stages, mustTile(t, w, 3, 3).Stage)
	}
	if !slices.Equal(stages, []TileStage{DrySoil, Grass, Shrub, Flowers, Tree}) {
		t.Errorf("stages = %v", stages)
	}
	if got := mustPlayer(t, w, ana).LeavesEarned; got != 15 { // 1 + 2 + 4 + 8
		t.Errorf("leaves earned = %d, want 15", got)
	}
	assertRF27(t, w)
}

func TestATreeKeepsItsHumidityAtTheCatalogCap(t *testing.T) {
	c := d01()
	c.Tank.Capacity = 100
	w := NewWorld(c, 0)
	w.Step(drops(1, 40, ana, 0, 0)) // 1000 de humedad aportada
	tile := mustTile(t, w, 0, 0)
	if tile.Stage != Tree || tile.Humidity > 100 {
		t.Errorf("tile = (%d, %d), want a tree with at most 100", tile.Stage, tile.Humidity)
	}
	if w.HumidityContributed() != 1000 {
		t.Errorf("contributed = %d, want 1000", w.HumidityContributed())
	}
}

func TestGrowthIsResolvedBeforeEvaporationInsideTheTick(t *testing.T) {
	w := newTestWorld()
	advanceTo(t, w, 9)
	// Tick 10 (toca evaporar): cuatro gotas suman 100 y la casilla crece
	// antes de que la evaporación le quite nada.
	w.Step(drops(1, 4, ana, 5, 5))
	if w.Tick() != 10 {
		t.Fatalf("tick = %d, want 10", w.Tick())
	}
	tile := mustTile(t, w, 5, 5)
	if tile.Stage != Grass || tile.Humidity != 0 {
		t.Errorf("tile = (%d, %d), want (Grass, 0)", tile.Stage, tile.Humidity)
	}
	if mustPlayer(t, w, ana).Leaves != 1 {
		t.Error("the dropper should have 1 leaf")
	}
}

func TestAPlantWithoutHumidityWithersAfterTheCatalogTicks(t *testing.T) {
	w := newTestWorld()
	w.Step(drops(1, 4, ana, 6, 6)) // tick 1: pasto con humedad 0
	if mustTile(t, w, 6, 6).Stage != Grass {
		t.Fatal("the tile should be grass")
	}
	advanceTo(t, w, 99)
	if tile := mustTile(t, w, 6, 6); tile.Stage != Grass || tile.DryTicks != 99 {
		t.Errorf("tick 99: (%d, %d), want (Grass, 99)", tile.Stage, tile.DryTicks)
	}
	advanceTo(t, w, 100) // 100 ticks seguidos con humedad 0
	if tile := mustTile(t, w, 6, 6); tile.Stage != DrySoil || tile.DryTicks != 0 {
		t.Errorf("tick 100: (%d, %d), want (DrySoil, 0)", tile.Stage, tile.DryTicks)
	}
}

func TestADropResetsTheDryCounterOfAPlant(t *testing.T) {
	w := newTestWorld()
	w.Step(drops(1, 4, ana, 6, 6))
	advanceTo(t, w, 50)
	if mustTile(t, w, 6, 6).DryTicks != 50 {
		t.Fatalf("dry ticks = %d, want 50", mustTile(t, w, 6, 6).DryTicks)
	}
	w.Step(drops(10, 1, ana, 6, 6))
	if mustTile(t, w, 6, 6).DryTicks != 0 {
		t.Error("a drop should reset the dry counter")
	}
}

func TestDemoModeHasNoEvaporationAndNoWither(t *testing.T) {
	w := newTestWorld()
	w.Step(drops(1, 4, ana, 6, 6))  // pasto con humedad 0
	w.Step(drops(10, 1, ana, 8, 8)) // 25
	w.setDemoMode(true)
	frozenAt := mustTile(t, w, 6, 6).DryTicks
	advanceTo(t, w, 300)
	if mustTile(t, w, 8, 8).Humidity != 25 {
		t.Error("demo mode freezes the humidity")
	}
	if tile := mustTile(t, w, 6, 6); tile.Stage != Grass || tile.DryTicks != frozenAt {
		t.Errorf("tile = (%d, %d), want (Grass, %d)", tile.Stage, tile.DryTicks, frozenAt)
	}
}

func TestABurningTileFreezesItsStageButKeepsEvaporating(t *testing.T) {
	w := newTestWorld()
	// En Rust se restaura una instantánea con estas casillas (la restauración
	// llega en T6); aquí se ponen directamente.
	w.tiles[0] = Tile{Stage: Grass, Humidity: 10, Fire: &Fire{}}
	w.tiles[1] = Tile{Stage: Grass, Humidity: 0, DryTicks: 90, Fire: &Fire{}}
	advanceTo(t, w, 20)
	if b := mustTile(t, w, 0, 0); b.Humidity != 6 || b.Stage != Grass {
		t.Errorf("burning tile = (%d, %d), want (6, Grass)", b.Humidity, b.Stage)
	}
	if f := mustTile(t, w, 1, 0); f.DryTicks != 90 || f.Stage != Grass {
		t.Errorf("frozen tile = (%d, %d), want (90, Grass)", f.DryTicks, f.Stage)
	}
}

// A una casilla ardiendo la gota le suma humedad pero no la hace crecer; con
// la tercera gota el fuego se apaga, el crecimiento pendiente se resuelve en
// ese momento y quien soltó la gota gana el bono (world.rs:380-388).
func TestTheThirdDropOnABurningTileResolvesTheGrowthAndPaysTheBonus(t *testing.T) {
	w := newTestWorld()
	w.tiles[0] = Tile{Stage: Grass, Humidity: 90, Fire: &Fire{}}
	w.Step(drops(1, 3, ana, 0, 0))
	tile := mustTile(t, w, 0, 0)
	// 90 + 3*25 = 165 -> un ascenso (Grass -> Shrub), sobran 65.
	if tile.Fire != nil || tile.Stage != Shrub || tile.Humidity != 65 {
		t.Errorf("tile = (fire %v, %d, %d), want (out, Shrub, 65)", tile.Fire, tile.Stage, tile.Humidity)
	}
	p := mustPlayer(t, w, ana)
	if p.Leaves != 2+5 || p.LeavesEarned != 7 {
		t.Errorf("leaves/earned = %d/%d, want 7/7", p.Leaves, p.LeavesEarned)
	}
	assertRF27(t, w)
}

func TestNewWorldWithSizeBuildsAValleyOfTheRequestedSize(t *testing.T) {
	w := NewWorldWithSize(d01(), 0, 64, 48)
	if w.Width() != 64 || w.Height() != 48 || len(w.Tiles()) != 64*48 {
		t.Fatalf("got %dx%d with %d tiles", w.Width(), w.Height(), len(w.Tiles()))
	}
	if _, ok := w.Tile(63, 47); !ok {
		t.Error("(63, 47) should be inside")
	}
	if _, ok := w.Tile(64, 0); ok {
		t.Error("(64, 0) should be outside")
	}
	if _, ok := w.Tile(0, 48); ok {
		t.Error("(0, 48) should be outside")
	}
	w.Step([]Command{dropCmd(action(1), ana, 63, 47)})
	if got := w.Tiles()[47*64+63].Humidity; got != 25 {
		t.Errorf("humidity = %d, want 25", got)
	}
	if p := w.Parts(); p.Width != 64 || p.Height != 48 {
		t.Errorf("parts size = %dx%d, want 64x48", p.Width, p.Height)
	}
}

func TestNewWorldWithSizeAtTheGameSizeIsTheSameValleyAsNewWorld(t *testing.T) {
	a := NewWorld(d01(), 9)
	b := NewWorldWithSize(d01(), 9, ValleyWidth, ValleyHeight)
	if !reflect.DeepEqual(a.Parts(), b.Parts()) {
		t.Error("the two valleys differ")
	}
}

func TestASnapshotOfACustomSizeValleyIsNotRestorableInTheGame(t *testing.T) {
	w := NewWorldWithSize(d01(), 0, 64, 64)
	if _, err := Restore(d01(), w.Parts()); err == nil {
		t.Error("Restore accepted a 64x64 valley")
	}
}

func TestNaturalTerrainRejectsDropAndInstallation(t *testing.T) {
	w := NewWorld(terrainCatalog(), 42)
	var waterPos, rockPos [2]uint32
	foundWater, foundRock := false, false
	for i, tile := range w.Tiles() {
		if tile.Stage == Water && !foundWater {
			waterPos = [2]uint32{uint32(i % 32), uint32(i / 32)}
			foundWater = true
		}
		if tile.Stage == Rock && !foundRock {
			rockPos = [2]uint32{uint32(i % 32), uint32(i / 32)}
			foundRock = true
		}
	}
	if !foundWater || !foundRock {
		t.Fatal("expected water and rock tiles in new world")
	}

	// Drop on water/rock rejected
	out := w.Step([]Command{
		dropCmd(action(1), ana, waterPos[0], waterPos[1]),
		dropCmd(action(2), ana, rockPos[0], rockPos[1]),
	})
	wantOutcomes(t, outcomes(out), RejectedOutcome(TileOccupied), RejectedOutcome(TileOccupied))

	// Place installation on water/rock rejected
	w.players[ana] = &playerSlot{record: startingPlayer(ana, 100), recentIDs: make(map[string]struct{})}
	w.players[ana].record.Leaves = 1000
	out2 := w.Step([]Command{
		placeCmd(action(3), ana, Sprinkler, waterPos[0], waterPos[1]),
		placeCmd(action(4), ana, Sprinkler, rockPos[0], rockPos[1]),
	})
	wantOutcomes(t, outcomes(out2), RejectedOutcome(TileOccupied), RejectedOutcome(TileOccupied))
}

func TestShorelineSuppressesFireIgnition(t *testing.T) {
	w := NewWorld(terrainCatalog(), 42)
	var waterPos [2]uint32
	for i, tile := range w.Tiles() {
		if tile.Stage == Water {
			waterPos = [2]uint32{uint32(i % 32), uint32(i / 32)}
			break
		}
	}
	var shoreIdx int
	for _, n := range []int{
		int(waterPos[1]-1)*32 + int(waterPos[0]),
		int(waterPos[1]+1)*32 + int(waterPos[0]),
		int(waterPos[1])*32 + int(waterPos[0]-1),
		int(waterPos[1])*32 + int(waterPos[0]+1),
	} {
		if n >= 0 && n < len(w.Tiles()) && w.Tiles()[n].Stage == DrySoil {
			shoreIdx = n
			break
		}
	}

	w.Tiles()[shoreIdx].Stage = Grass
	w.Tiles()[shoreIdx].Humidity = 0
	w.catalog.Fire.IgnitionChancePPM = 1_000_000
	w.catalog.Fire.PropagationChancePPM = 1_000_000

	w.Step(nil)
	if w.Tiles()[shoreIdx].Fire != nil {
		t.Error("shoreline tile should not ignite")
	}
}

func TestResetValleyKeepsTheNaturalTerrainAndDoesNotDrawRandomNumbers(t *testing.T) {
	w := NewWorld(terrainCatalog(), 42)
	waterBefore, rockBefore := countStage(w, Water), countStage(w, Rock)
	if waterBefore == 0 || rockBefore == 0 {
		t.Fatalf("expected terrain, water %d, rocks %d", waterBefore, rockBefore)
	}
	tilesBefore := slices.Clone(w.Tiles())
	rngBefore := w.RngState()

	w.Step([]Command{resetCmd(action(1))})

	// El reinicio conserva el terreno natural casilla a casilla y no toca el
	// generador: el mapa no cambia y el estado del azar tampoco.
	if waterAfter, rockAfter := countStage(w, Water), countStage(w, Rock); waterBefore != waterAfter || rockBefore != rockAfter {
		t.Errorf("terrain after reset: water %d (was %d), rocks %d (was %d)", waterAfter, waterBefore, rockAfter, rockBefore)
	}
	if !reflect.DeepEqual(tilesBefore, w.Tiles()) {
		t.Error("the reset must keep the natural terrain tile by tile")
	}
	if !bytes.Equal(rngBefore, w.RngState()) {
		t.Error("the reset must not draw random numbers")
	}
}

func TestTheNaturalTerrainHasTheSouthLagoon(t *testing.T) {
	w := NewWorld(terrainCatalog(), 42)
	for y := 26; y <= 28; y++ {
		for x := 17; x <= 19; x++ {
			if got := w.Tiles()[y*32+x].Stage; got != Water {
				t.Errorf("(%d, %d) = %v, want water", x, y, got)
			}
		}
	}
}

// terrainCatalog es el catálogo de D-01 con río de 2 casillas y 12 rocas,
// como el real.
func terrainCatalog() Catalog {
	catalog := d01()
	catalog.Terrain = TerrainRules{RiverWidth: 2, RockCount: 12}
	return catalog
}

func countStage(w *World, stage TileStage) int {
	count := 0
	for _, tile := range w.Tiles() {
		if tile.Stage == stage {
			count++
		}
	}
	return count
}

func TestACatalogWithoutTerrainLeavesTheValleyEmptyAndTheGeneratorUntouched(t *testing.T) {
	w := NewWorld(d01(), 42)
	for i, tile := range w.Tiles() {
		if tile != (Tile{}) {
			t.Fatalf("tile %d = %+v, want empty", i, tile)
		}
	}
	if !bytes.Equal(w.RngState(), NewSplitMix64(42).Bytes()) {
		t.Error("the generator was used with no terrain in the catalog")
	}
}

func TestTheTerrainComesFromTheSeededGeneratorAndSurvivesTheSnapshot(t *testing.T) {
	a, b, c := NewWorld(terrainCatalog(), 42), NewWorld(terrainCatalog(), 42), NewWorld(terrainCatalog(), 43)
	if !reflect.DeepEqual(a.Tiles(), b.Tiles()) {
		t.Error("the same seed must give the same terrain")
	}
	if reflect.DeepEqual(a.Tiles(), c.Tiles()) {
		t.Error("another seed must give another terrain")
	}
	if got := countStage(a, Rock); got != 12 {
		t.Errorf("rocks = %d, want 12", got)
	}
	if got := countStage(a, Water); got < 64 {
		t.Errorf("water = %d, want at least 64", got)
	}
	if bytes.Equal(a.RngState(), NewSplitMix64(42).Bytes()) {
		t.Error("the generator did not advance: its state travels in the snapshot (rule 6)")
	}
	restored, err := Restore(terrainCatalog(), a.Parts())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Tiles(), a.Tiles()) {
		t.Error("the terrain must survive the snapshot as it is")
	}
}

func TestTheRiverNeverLeavesTheRightHandSideOfTheValley(t *testing.T) {
	for seed := uint64(1); seed < 40; seed++ {
		for i, tile := range NewWorld(terrainCatalog(), seed).Tiles() {
			if tile.Stage.IsNatural() && i%32 < 17 {
				t.Fatalf("seed %d: natural tile at column %d", seed, i%32)
			}
		}
	}
}

func cleanCmd(actionID, playerID string, x, y uint32) Command {
	return Command{ActionID: actionID, PlayerID: playerID, Kind: CleanTrashCommand{X: x, Y: y}}
}

func TestCleanTrashReducesLevelAndPaysIncentiveLeaves(t *testing.T) {
	w := rich(holder{ana, 10})
	idx := 2*32 + 2
	w.tiles[idx].TrashLevel = 2
	w.tiles[idx].Stage = DrySoil

	// Primer clean_trash: nivel 2 -> 1 (sin hojas de incentivo aún)
	out1 := w.Step([]Command{cleanCmd(action(1), ana, 2, 2)})
	wantOutcomes(t, outcomes(out1), AppliedOutcome)
	if w.tiles[idx].TrashLevel != 1 {
		t.Errorf("trash level = %d, want 1", w.tiles[idx].TrashLevel)
	}
	if p, _ := w.Player(ana); p.Leaves != 10 {
		t.Errorf("leaves = %d, want 10", p.Leaves)
	}

	// Segundo clean_trash: nivel 1 -> 0 (+2 hojas de incentivo)
	out2 := w.Step([]Command{cleanCmd(action(2), ana, 2, 2)})
	wantOutcomes(t, outcomes(out2), AppliedOutcome)
	if w.tiles[idx].TrashLevel != 0 {
		t.Errorf("trash level = %d, want 0", w.tiles[idx].TrashLevel)
	}
	if p, _ := w.Player(ana); p.Leaves != 12 || p.LeavesEarned != 12 {
		t.Errorf("leaves = %d, leaves_earned = %d, want 12", p.Leaves, p.LeavesEarned)
	}

	// Tercer clean_trash sobre casilla sin basura: rechazado
	out3 := w.Step([]Command{cleanCmd(action(3), ana, 2, 2)})
	wantOutcomes(t, outcomes(out3), RejectedOutcome(InvalidCommand))
}

func TestUrbanBuildingSpawnsAndIsImmuneToFire(t *testing.T) {
	w := rich(holder{ana, 100})
	w.humidityContributed = 600
	advanceTo(t, w, 1)

	var buildingIdx = -1
	for i, tile := range w.Tiles() {
		if tile.HasBuilding {
			buildingIdx = i
			break
		}
	}
	if buildingIdx == -1 {
		t.Fatal("un edificio debe aparecer tras superar el umbral de progreso")
	}

	bx, by := uint32(buildingIdx%32), uint32(buildingIdx/32)

	// Inmune a fuego
	if IsEligible(&w.Tiles()[buildingIdx]) {
		t.Error("building tile should not be eligible for fire")
	}

	// Intento de colocar instalación sobre edificio rechazado
	out := w.Step([]Command{placeCmd(action(1), ana, Sprinkler, bx, by)})
	wantOutcomes(t, outcomes(out), RejectedOutcome(TileOccupied))
}

func TestTrashBinCleansTrashAndConsumesDurability(t *testing.T) {
	w := rich(holder{ana, 100})
	// Colocar cesto de reciclaje en (4, 4)
	w.Step([]Command{placeCmd(action(1), ana, TrashBin, 4, 4)})

	// Sembrar basura nivel 2 en (5, 5) dentro del radio 3x3 del cesto
	targetIdx := 5*32 + 5
	w.tiles[targetIdx].TrashLevel = 2

	binIdx := 4*32 + 4
	if inst := w.tiles[binIdx].Installation; inst == nil || inst.RemainingDurability != 30 {
		t.Fatalf("expected TrashBin with 30 durability, got %+v", inst)
	}

	// Avanzar 1 ciclo de trabajo (20 ticks = 2 s)
	advanceTo(t, w, 20)

	// El cesto debe haber reducido 1 nivel de basura y 1 de durabilidad
	if w.tiles[targetIdx].TrashLevel != 1 {
		t.Errorf("trash level = %d, want 1", w.tiles[targetIdx].TrashLevel)
	}
	if inst := w.tiles[binIdx].Installation; inst == nil || inst.RemainingDurability != 29 {
		t.Errorf("expected 29 durability, got %+v", inst)
	}
}

func TestADropOnATileWithTrashIsRejectedAndCostsNothing(t *testing.T) {
	w := rich(holder{ana, 0})
	w.tiles[3*32+3].TrashLevel = 1
	out := w.Step([]Command{dropCmd(action(1), ana, 3, 3)})
	wantOutcomes(t, outcomes(out), RejectedOutcome(TileOccupied))
	if player, _ := w.Player(ana); player.Water != 20 {
		t.Errorf("water = %d, want 20", player.Water)
	}
}

func TestSprinklersAndPlantPulsesDoNotGrowTilesUnderTrashOrBuildings(t *testing.T) {
	w := rich(holder{ana, 100})
	w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 5, 5)})
	w.Step([]Command{placeCmd(action(2), ana, TreatmentPlant, 9, 9)})
	w.tiles[6*32+5].TrashLevel = 2
	w.tiles[9*32+10].HasBuilding = true
	advanceTo(t, w, 100)
	if tile := w.tiles[6*32+5]; tile.Stage != DrySoil || tile.Humidity != 0 {
		t.Errorf("tile under trash = %+v, want untouched dry soil", tile)
	}
	if w.tiles[9*32+10].Stage != DrySoil {
		t.Error("the tile of a building must not grow")
	}
	if w.tiles[4*32+5].Stage == DrySoil {
		t.Error("a free neighbour must have grown")
	}
}

func TestBuildingsFollowTheUrbanFiguresOfTheCatalog(t *testing.T) {
	catalog := d01()
	catalog.Urban.MaxBuildings = 1
	catalog.Urban.TrashIntervalSeconds = 1
	catalog.Urban.BuildingHumidityThreshold = 10
	w := richWorld(catalog, holder{ana, 0})
	advanceTo(t, w, 5)
	for _, tile := range w.Tiles() {
		if tile.HasBuilding {
			t.Fatal("a building appeared before the threshold")
		}
	}
	w.humidityContributed = 10
	advanceTo(t, w, 30)
	buildings := 0
	for _, tile := range w.Tiles() {
		if tile.HasBuilding {
			buildings++
		}
	}
	if buildings != 1 || !w.tiles[0].HasBuilding {
		t.Errorf("buildings = %d, corner = %v; want a single one in the corner", buildings, w.tiles[0].HasBuilding)
	}
	if got := w.tiles[1].TrashLevel; got != 3 {
		t.Errorf("trash next to the building = %d, want 3", got)
	}
}

func TestLevelThreeTrashSpreadsButNotIntoTheAreaOfAnActiveBin(t *testing.T) {
	w := rich(holder{ana, 100})
	w.Step([]Command{placeCmd(action(1), ana, TrashBin, 10, 10)})
	w.tiles[10*32+8].TrashLevel = 3
	// (9, 10) está en el área 3x3 del cesto; (7, 10), (8, 9) y (8, 11) no.
	advanceTo(t, w, 100)
	if got := w.tiles[10*32+7].TrashLevel; got != 1 {
		t.Errorf("(7, 10) trash = %d, want 1", got)
	}
	if w.tiles[9*32+8].TrashLevel < 1 {
		t.Error("(8, 9) should have received trash")
	}
	if got := w.tiles[10*32+9].TrashLevel; got != 0 {
		t.Errorf("(9, 10) is protected by the bin, trash = %d", got)
	}
}

func TestTrashGrowsFromLevelOneButOnlySpreadsFromLevelTwo(t *testing.T) {
	w := rich(holder{ana, 0})
	w.tiles[10*32+8].TrashLevel = 1
	w.tiles[10*32+20].TrashLevel = 2
	advanceTo(t, w, 100)
	// Nivel 1: sube a 2 y no contagia a nadie en este ciclo.
	if got := w.tiles[10*32+8].TrashLevel; got != 2 {
		t.Errorf("(8, 10) = %d, want 2", got)
	}
	for _, p := range [][2]int{{7, 10}, {9, 10}, {8, 9}, {8, 11}} {
		if got := w.tiles[p[1]*32+p[0]].TrashLevel; got != 0 {
			t.Errorf("(%d, %d) = %d, want 0", p[0], p[1], got)
		}
	}
	// Nivel 2: sube a 3 y suma +1 a sus cuatro vecinas.
	if got := w.tiles[10*32+20].TrashLevel; got != 3 {
		t.Errorf("(20, 10) = %d, want 3", got)
	}
	for _, p := range [][2]int{{19, 10}, {21, 10}, {20, 9}, {20, 11}} {
		if got := w.tiles[p[1]*32+p[0]].TrashLevel; got != 1 {
			t.Errorf("(%d, %d) = %d, want 1", p[0], p[1], got)
		}
	}
}

func polluted(w *World) int {
	count := 0
	for _, tile := range w.Tiles() {
		if tile.TrashLevel > 0 {
			count++
		}
	}
	return count
}

func TestTrashDoesNotSpreadWhenSixtyTilesArePolluted(t *testing.T) {
	w := rich(holder{ana, 0})
	// 58 casillas de nivel 1 en las filas 20 y 21 y una fuente de nivel 2:
	// 59 sucias, queda un solo cupo.
	for i := range 58 {
		w.tiles[(20+i/32)*32+i%32].TrashLevel = 1
	}
	w.tiles[5*32+10].TrashLevel = 2
	if got := polluted(w); got != MaxPollutedTiles-1 {
		t.Fatalf("polluted = %d, want %d", got, MaxPollutedTiles-1)
	}
	advanceTo(t, w, 100)
	// Solo una de las cuatro vecinas limpias entra en el cupo restante.
	fresh := uint32(0)
	for _, p := range [][2]int{{9, 5}, {11, 5}, {10, 4}, {10, 6}} {
		fresh += w.tiles[p[1]*32+p[0]].TrashLevel
	}
	if fresh != 1 || polluted(w) != MaxPollutedTiles {
		t.Fatalf("fresh = %d, polluted = %d; want 1 and %d", fresh, polluted(w), MaxPollutedTiles)
	}
	// Con el tope alcanzado no crece ni se propaga nada hasta limpiar.
	before := slices.Clone(w.Tiles())
	advanceTo(t, w, 200)
	if !reflect.DeepEqual(before, w.Tiles()) {
		t.Error("nothing may change at the cap")
	}
}

func TestABuildingPrefersANeighbourBelowTheMaximumTrashLevel(t *testing.T) {
	catalog := d01()
	catalog.Urban.TrashIntervalSeconds = 1
	w := richWorld(catalog, holder{ana, 0})
	w.tiles[0].HasBuilding = true
	w.tiles[1].TrashLevel = 3
	advanceTo(t, w, 10)
	// (1, 0) está al máximo: la basura va a la siguiente vecina libre.
	if w.tiles[1].TrashLevel != 3 || w.tiles[32].TrashLevel != 1 {
		t.Errorf("(1, 0) = %d, (0, 1) = %d; want 3 and 1", w.tiles[1].TrashLevel, w.tiles[32].TrashLevel)
	}
}

func TestCleaningPaysTheCatalogRewardAndANewPlayerCanClean(t *testing.T) {
	catalog := d01()
	catalog.Urban.CleanRewardLeaves = 7
	w := richWorld(catalog)
	w.tiles[2*32+2].TrashLevel = 1
	out := w.Step([]Command{cleanCmd(action(1), ana, 2, 2)})
	wantOutcomes(t, outcomes(out), AppliedOutcome)
	if player, _ := w.Player(ana); player.Leaves != 7 {
		t.Errorf("leaves = %d, want 7", player.Leaves)
	}
}

func TestAnInstallationFromASnapshotWithoutWearIsRestoredAsNew(t *testing.T) {
	parts := newTestWorld().Parts()
	parts.Tiles[4*32+4].Installation = &Installation{Kind: Sprinkler, OwnerID: ana, RemainingDurability: 0}
	parts.Tiles[4*32+5].Installation = &Installation{Kind: TrashBin, OwnerID: ana, RemainingDurability: 7}
	w, err := Restore(d01(), parts)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.Tiles()[4*32+4].Installation.RemainingDurability; got != 30 {
		t.Errorf("legacy installation durability = %d, want 30", got)
	}
	if got := w.Tiles()[4*32+5].Installation.RemainingDurability; got != 7 {
		t.Errorf("worn installation durability = %d, want 7", got)
	}
}
