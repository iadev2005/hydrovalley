package domain

import (
	"fmt"
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
	for i, tile := range w.Tiles() {
		if tile != (Tile{}) {
			t.Fatalf("tile %d is not empty: %+v", i, tile)
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
