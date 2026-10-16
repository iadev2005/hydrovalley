package domain

import (
	"fmt"
	"reflect"
	"testing"
)

// Portadas de las pruebas de world.rs de los comandos de administración
// (RF-31), la ventana de action_id, el reinicio con catálogo y el
// determinismo (world.rs:1008-1043, 1270-1283, 1573-1780, 2009-2455 y 2519-
// 2584). Mismas cifras literales que en Rust.

const admin = "aaaaaaaa-0000-4000-8000-000000000001"

func demoCmd(actionID string, enabled bool) Command {
	return Command{ActionID: actionID, PlayerID: admin, Kind: SetDemoModeCommand{Enabled: enabled}}
}

func resetCmd(actionID string) Command {
	return Command{ActionID: actionID, PlayerID: admin, Kind: ResetValleyCommand{}}
}

func ptr[T any](v T) *T { return &v }

func nextCatalog() Catalog {
	next := customCatalog() // capacidad 30, gota de 40
	next.Tank.RefillIntervalSeconds = 1
	return next
}

// ---- Ventana de action_id (Capa 3) -------------------------------------

func TestAnAppliedActionIsRememberedFor600Ticks(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{dropCmd(action(1), ana, 5, 5)}) // tick 1
	advanceTo(t, w, 600)

	// Tick 601: 600 ticks después, todavía dentro de la ventana.
	out := w.Step([]Command{dropCmd(action(1), ana, 5, 5)})
	wantOutcomes(t, outcomes(out), DuplicateOutcome)

	// Tick 602: fuera de la ventana; se aplica como nuevo.
	out = w.Step([]Command{dropCmd(action(1), ana, 5, 5)})
	wantOutcomes(t, outcomes(out), AppliedOutcome)
	if w.HumidityContributed() != 50 {
		t.Errorf("humidity contributed = %d, want 50", w.HumidityContributed())
	}
}

func TestOldActionsArePrunedFromTheWindow(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{dropCmd(action(1), ana, 5, 5)}) // tick 1
	advanceTo(t, w, 601)
	w.Step([]Command{dropCmd(action(2), ana, 5, 5)}) // tick 602
	want := []AppliedAction{{ActionID: action(2), Tick: 602}}
	if got := mustPlayer(t, w, ana).RecentActions; !reflect.DeepEqual(got, want) {
		t.Errorf("recent actions = %v, want %v", got, want)
	}
}

// Go-only: el índice de duplicados queda podado junto con la lista, y la
// poda también alcanza a un jugador que está inactivo (world.rs:692).
func TestPruningTheWindowAlsoForgetsTheDuplicateIndexOfIdlePlayers(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{dropCmd(action(1), ana, 5, 5), dropCmd(action(2), beto, 5, 5)}) // tick 1
	w.Step([]Command{dropCmd(action(3), beto, 6, 5)})                                // tick 2
	advanceTo(t, w, 601)                                                             // tick 601: lo del tick 1 sigue (600 de distancia)
	if got := len(mustPlayer(t, w, ana).RecentActions); got != 1 {
		t.Fatalf("recent actions at tick 601 = %d, want 1", got)
	}
	idle(w, 1) // tick 602: caen las del tick 1, la del tick 2 sigue
	if got := mustPlayer(t, w, ana).RecentActions; len(got) != 0 {
		t.Errorf("ana recent actions = %v, want none", got)
	}
	if got := mustPlayer(t, w, beto).RecentActions; len(got) != 1 || got[0].Tick != 2 {
		t.Errorf("beto recent actions = %v, want only the one of tick 2", got)
	}
	if _, kept := w.players[ana].recentIDs[action(1)]; kept {
		t.Error("the duplicate index still holds a pruned action")
	}
	if _, kept := w.players[beto].recentIDs[action(3)]; !kept {
		t.Error("the duplicate index lost an action inside the window")
	}
}

// ---- Modo demo y reinicio (RF-31) --------------------------------------

func TestSetDemoModeIsAppliedAndSwitchesTheDropRules(t *testing.T) {
	w := newTestWorld()
	out := w.Step([]Command{demoCmd(action(1), true)})
	wantOutcomes(t, outcomes(out), AppliedOutcome)
	if !w.DemoMode() {
		t.Fatal("demo mode is off")
	}

	w.Step(drops(10, 1, ana, 1, 1))
	wantHumidity(t, w, 1, 1, 1)
	if got := mustPlayer(t, w, ana).Water; got != 20 { // la gota de demo es gratis
		t.Errorf("water = %d, want 20", got)
	}

	out = w.Step([]Command{demoCmd(action(2), false)})
	wantOutcomes(t, outcomes(out), AppliedOutcome)
	if w.DemoMode() {
		t.Fatal("demo mode is on")
	}
	w.Step(drops(20, 1, ana, 1, 1))
	if got := mustPlayer(t, w, ana).Water; got != 19 {
		t.Errorf("water = %d, want 19", got)
	}
}

func TestSetDemoModeTakesEffectInTheSameTickForTheCommandsAfterIt(t *testing.T) {
	w := newTestWorld()
	out := w.Step([]Command{
		dropCmd(action(1), ana, 0, 0),
		demoCmd(action(2), true),
		dropCmd(action(3), ana, 0, 0),
	})
	wantOutcomes(t, outcomes(out), AppliedOutcome, AppliedOutcome, AppliedOutcome)
	wantHumidity(t, w, 0, 0, 26)
	if w.HumidityContributed() != 26 {
		t.Errorf("humidity contributed = %d, want 26", w.HumidityContributed())
	}
}

func TestARepeatedAdminCommandIsADuplicate(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{demoCmd(action(1), true)})
	out := w.Step([]Command{demoCmd(action(1), true)})
	wantOutcomes(t, outcomes(out), DuplicateOutcome)
}

// CP-01 / CP-11 / RF-30 sobre el dominio: varios jugadores, varios ticks,
// muchas gotas sobre la misma casilla (que además crece).
func TestInDemoModeTheHumidityContributedEqualsTheAcceptedDrops(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{demoCmd(action(1), true)})
	players := []string{ana, beto, admin}
	var accepted uint64
	next := uint32(100)
	for tick := range uint32(30) {
		var batch []Command
		for k := range uint32(7) {
			next++
			batch = append(batch, dropCmd(action(next), players[(tick+k)%3], tick%2, 0))
		}
		for _, r := range w.Step(batch).Results {
			if r.Outcome == AppliedOutcome {
				accepted++
			}
		}
	}
	if accepted != 210 {
		t.Fatalf("accepted = %d, want 210", accepted)
	}
	if w.HumidityContributed() != accepted {
		t.Errorf("humidity contributed = %d, want %d", w.HumidityContributed(), accepted)
	}
	for _, p := range w.Players() {
		if p.Water != 20 || p.WaterSpent != 0 { // el depósito no cambia en demo
			t.Errorf("%s water/spent = %d/%d, want 20/0", p.PlayerID, p.Water, p.WaterSpent)
		}
	}
	grew := false
	for _, tile := range w.Tiles() {
		grew = grew || tile.Stage != DrySoil
	}
	if !grew {
		t.Error("no tile grew while the total stayed exact")
	}
	assertRF27(t, w)
}

func TestResetValleyRestoresTheInitialStateAndKeepsTheClock(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{demoCmd(action(1), false)})
	w.Step(drops(10, 6, ana, 2, 2))
	w.Step(drops(20, 4, beto, 9, 9)) // Beto hace crecer una casilla
	advanceTo(t, w, 57)
	touched := false
	for _, tile := range w.Tiles() {
		touched = touched || tile != (Tile{})
	}
	if !touched {
		t.Fatal("the valley is still empty")
	}
	contributed := w.HumidityContributed()

	out := w.Step([]Command{resetCmd(action(99))})
	if out.Tick != 58 {
		t.Errorf("step tick = %d, want 58", out.Tick)
	}
	wantOutcomes(t, outcomes(out), AppliedOutcome)
	if w.Tick() != 58 { // el tick nunca retrocede
		t.Errorf("tick = %d, want 58", w.Tick())
	}
	if w.Epoch() != 1 {
		t.Errorf("epoch = %d, want 1", w.Epoch())
	}
	for i, tile := range w.Tiles() {
		if tile != (Tile{}) {
			t.Fatalf("tile %d was not cleared: %+v", i, tile)
		}
	}
	if w.HumidityContributed() != contributed {
		t.Errorf("humidity contributed = %d, want %d", w.HumidityContributed(), contributed)
	}
	if w.DemoMode() {
		t.Error("demo mode came on")
	}
	idle(w, 1)
	if w.Tick() != 59 {
		t.Errorf("tick = %d, want 59", w.Tick())
	}
}

func TestResetValleyResetsThePlayersCountersButKeepsThePlayers(t *testing.T) {
	w := newTestWorld()
	w.Step(drops(10, 6, ana, 2, 2))
	w.Step(drops(20, 4, beto, 9, 9))
	if got := mustPlayer(t, w, beto).Leaves; got != 1 {
		t.Fatalf("beto leaves = %d, want 1", got)
	}

	w.Step([]Command{resetCmd(action(99))})
	if len(w.Players()) != 3 { // Ana, Beto y el administrador siguen
		t.Errorf("players = %d, want 3", len(w.Players()))
	}
	for _, id := range []string{ana, beto} {
		p := mustPlayer(t, w, id)
		if p.Water != 20 || p.TankCapacity != 20 {
			t.Errorf("%s water/tank = %d/%d, want 20/20", id, p.Water, p.TankCapacity)
		}
		if p.Leaves != 0 || p.LeavesEarned != 0 || p.LeavesSpent != 0 {
			t.Errorf("%s leaves = %d/%d/%d, want 0/0/0", id, p.Leaves, p.LeavesEarned, p.LeavesSpent)
		}
		if p.WaterRefilled != 20 || p.WaterSpent != 0 {
			t.Errorf("%s refilled/spent = %d/%d, want 20/0", id, p.WaterRefilled, p.WaterSpent)
		}
		if p.WaterReduced != 0 || p.WaterReused != 0 || p.WaterRecycled != 0 {
			t.Errorf("%s 3R counters = %d/%d/%d, want 0/0/0", id, p.WaterReduced, p.WaterReused, p.WaterRecycled)
		}
		if len(p.Purchases) != 0 || len(p.Upgrades) != 0 {
			t.Errorf("%s kept purchases or upgrades", id)
		}
	}
	assertRF27(t, w)
}

func TestResetValleyKeepsTheIdempotencyWindow(t *testing.T) {
	w := newTestWorld()
	w.Step(drops(10, 1, ana, 2, 2))
	w.Step([]Command{resetCmd(action(99))})
	out := w.Step([]Command{dropCmd(action(10), ana, 2, 2), resetCmd(action(99))})
	wantOutcomes(t, outcomes(out), DuplicateOutcome, DuplicateOutcome)
	if w.Epoch() != 1 {
		t.Errorf("epoch = %d, want 1", w.Epoch())
	}
	if w.HumidityContributed() != 25 {
		t.Errorf("humidity contributed = %d, want 25", w.HumidityContributed())
	}
}

func TestResetValleySplitsTheTickInBeforeAndAfter(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{
		dropCmd(action(1), ana, 3, 3),
		resetCmd(action(2)),
		dropCmd(action(3), ana, 4, 4),
	})
	wantHumidity(t, w, 3, 3, 0)
	wantHumidity(t, w, 4, 4, 25)
	if got := mustPlayer(t, w, ana).Water; got != 19 {
		t.Errorf("water = %d, want 19", got)
	}
}

func TestEveryResetIncrementsTheEpoch(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{resetCmd(action(1))})
	w.Step([]Command{resetCmd(action(2))})
	if w.Epoch() != 2 {
		t.Errorf("epoch = %d, want 2", w.Epoch())
	}
}

func TestResetValleyRemovesInstallationsPurchasesAndUpgrades(t *testing.T) {
	w := rich(holder{ana, 100})
	w.Step([]Command{
		placeCmd(action(1), ana, Sprinkler, 1, 1),
		buyCmd(action(2), ana, BiggerTank),
		buyCmd(action(3), ana, BiggerDrop),
	})
	advanceTo(t, w, 40)
	w.Step([]Command{resetCmd(action(4))})
	if installationAt(t, w, 1, 1) != nil {
		t.Error("the installation survived the reset")
	}
	p := mustPlayer(t, w, ana)
	if len(p.Purchases) != 0 || len(p.Upgrades) != 0 {
		t.Error("purchases or upgrades survived the reset")
	}
	if p.TankCapacity != 20 || p.LeavesSpent != 0 {
		t.Errorf("tank/spent = %d/%d, want 20/0", p.TankCapacity, p.LeavesSpent)
	}
	if p.WaterReused != 0 || p.WaterReduced != 0 || p.WaterRecycled != 0 {
		t.Errorf("3R counters = %d/%d/%d, want 0/0/0", p.WaterReused, p.WaterReduced, p.WaterRecycled)
	}
	if got := w.NextPrice(p, Sprinkler); got != 10 {
		t.Errorf("next sprinkler price = %d, want 10", got)
	}
}

func TestResettingTheValleyClearsTheFiresButNotTheGenerator(t *testing.T) {
	w := worldWith(catalogWithPPM(0), 3, func(w *World) {
		w.tiles[tileIdx(7, 7)] = burningGrass(0)
	})
	w.Step(nil)
	before := w.RngState()
	w.Step([]Command{resetCmd(action(1))})
	wantFire(t, w, 7, 7, nil)
	if !reflect.DeepEqual(w.RngState(), before) {
		t.Error("the reset changed the generator")
	}
}

// ---- Catálogo ofrecido a un reinicio -----------------------------------

func TestAnAppliedResetAdoptsTheOfferedCatalog(t *testing.T) {
	w := newTestWorld()
	w.Step(drops(1, 1, ana, 0, 0))
	out := w.StepWithResetCatalog([]Command{resetCmd(action(9))}, ptr(nextCatalog()))
	wantOutcomes(t, outcomes(out), AppliedOutcome)
	if w.CatalogVersion() != "custom" {
		t.Errorf("catalog version = %q, want custom", w.CatalogVersion())
	}
	p := mustPlayer(t, w, ana)
	if p.Water != 30 || p.TankCapacity != 30 {
		t.Errorf("water/tank = %d/%d, want 30/30", p.Water, p.TankCapacity)
	}
	advanceTo(t, w, 12) // recarga cada 10 ticks con el catálogo nuevo
	if got := mustPlayer(t, w, ana).WaterRefilled; got != 30 {
		t.Errorf("water refilled = %d, want 30", got)
	}
}

func TestADuplicateResetKeepsTheCurrentCatalog(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{resetCmd(action(9))})
	out := w.StepWithResetCatalog([]Command{resetCmd(action(9))}, ptr(nextCatalog()))
	wantOutcomes(t, outcomes(out), DuplicateOutcome)
	if w.CatalogVersion() != d01Version || w.Epoch() != 1 {
		t.Errorf("catalog/epoch = %q/%d, want %q/1", w.CatalogVersion(), w.Epoch(), d01Version)
	}
}

func TestARejectedResetKeepsTheCurrentCatalog(t *testing.T) {
	w := newTestWorld()
	out := w.StepWithResetCatalog([]Command{resetCmd("not-a-uuid")}, ptr(nextCatalog()))
	wantOutcomes(t, outcomes(out), RejectedOutcome(InvalidCommand))
	if w.CatalogVersion() != d01Version || w.Epoch() != 0 {
		t.Errorf("catalog/epoch = %q/%d, want %q/0", w.CatalogVersion(), w.Epoch(), d01Version)
	}
}

func TestAnOfferedCatalogWithoutAResetIsIgnored(t *testing.T) {
	w := newTestWorld()
	w.StepWithResetCatalog(drops(1, 1, ana, 0, 0), ptr(nextCatalog()))
	if w.CatalogVersion() != d01Version {
		t.Errorf("catalog version = %q, want %q", w.CatalogVersion(), d01Version)
	}
}

// Go-only: el catálogo ofrecido lo adopta el primer reinicio aplicado y se
// consume (Option::take, world.rs:672); un segundo reinicio del mismo tick ya
// usa el catálogo nuevo y no hay nada que adoptar, y el siguiente paso no
// conserva la oferta.
func TestTheOfferedCatalogIsAdoptedOnceAndNeverKeptForTheNextStep(t *testing.T) {
	w := newTestWorld()
	w.StepWithResetCatalog([]Command{resetCmd(action(1)), resetCmd(action(2))}, ptr(nextCatalog()))
	if w.CatalogVersion() != "custom" || w.Epoch() != 2 {
		t.Fatalf("catalog/epoch = %q/%d, want custom/2", w.CatalogVersion(), w.Epoch())
	}
	if w.resetCatalog != nil {
		t.Error("the offer outlived the step")
	}
	w2 := newTestWorld()
	w2.StepWithResetCatalog(nil, ptr(nextCatalog()))
	w2.Step([]Command{resetCmd(action(1))})
	if w2.CatalogVersion() != d01Version {
		t.Errorf("a later reset adopted a stale offer: %q", w2.CatalogVersion())
	}
}

// Go-only: administración por un jugador nuevo con el mundo lleno. Rust no
// comprueba MaxPlayers en estos comandos y player_slot da de alta al
// administrador igual (world.rs:296-303, 316-317).
func TestAdminCommandsFromANewcomerEnterAFullWorldLikeInRust(t *testing.T) {
	w := newTestWorld()
	for n := range MaxPlayers {
		w.playerSlot(fmt.Sprintf("00000000-0000-4000-9000-%012d", n))
	}
	out := w.Step([]Command{demoCmd(action(1), true)})
	wantOutcomes(t, outcomes(out), AppliedOutcome)
	if len(w.Players()) != MaxPlayers+1 {
		t.Errorf("players = %d, want %d", len(w.Players()), MaxPlayers+1)
	}
}

// ---- Instalaciones y gotas en modo demo --------------------------------

func TestSprinklersKeepWorkingInDemoModeWithoutBreakingTheDropInvariant(t *testing.T) {
	w := rich(holder{ana, 10})
	w.Step([]Command{demoCmd(action(1), true)})
	w.Step([]Command{placeCmd(action(2), ana, Sprinkler, 10, 10)})
	w.Step(drops(10, 3, ana, 3, 3))
	advanceTo(t, w, 41)
	wantHumidity(t, w, 10, 10, 50)    // dos riegos y ninguna evaporación
	if w.HumidityContributed() != 3 { // solo cuentan las gotas
		t.Errorf("humidity contributed = %d, want 3", w.HumidityContributed())
	}
}

func TestInDemoModeTheBiggerDropStillHumidifiesOneTileSoTheInvariantHolds(t *testing.T) {
	w := rich(holder{ana, 30})
	w.Step([]Command{buyCmd(action(1), ana, BiggerDrop)})
	w.Step([]Command{demoCmd(action(2), true)})
	before := w.HumidityContributed()
	var accepted uint64
	for _, r := range w.Step(drops(10, 5, ana, 10, 10)).Results {
		if r.Outcome == AppliedOutcome {
			accepted++
		}
	}
	if accepted != 5 {
		t.Fatalf("accepted = %d, want 5", accepted)
	}
	if got := w.HumidityContributed() - before; got != accepted {
		t.Errorf("humidity contributed grew by %d, want %d", got, accepted)
	}
	wantHumidity(t, w, 10, 10, 5)
	wantHumidity(t, w, 9, 10, 0)
	if got := mustPlayer(t, w, ana).WaterSpent; got != 0 {
		t.Errorf("water spent = %d, want 0", got)
	}
}

// ---- Determinismo ------------------------------------------------------

// adminScript es el guion fijo de comandos de las pruebas de determinismo
// (world.rs:2519).
func adminScript(tick uint64) []Command {
	n := uint32(tick * 10)
	var commands []Command
	switch tick % 7 {
	case 0:
		commands = append(commands, drops(n, 5, ana, uint32(tick%5), 1)...)
	case 2:
		commands = append(commands, drops(n, 3, beto, uint32(tick%5), 1)...)
	case 4:
		commands = append(commands, drops(n, 4, admin, 2, 2)...)
	}
	switch tick {
	case 50:
		commands = append(commands, demoCmd(action(9001), true))
	case 120:
		commands = append(commands, demoCmd(action(9002), false))
	case 200:
		commands = append(commands, resetCmd(action(9003)))
	}
	// Compras en el guion: unas se aplican y otras se rechazan según las
	// hojas y las casillas libres, pero igual en cada ejecución.
	switch tick {
	case 3:
		commands = append(commands, placeCmd(action(9101), ana, Sprinkler, 1, 1))
	case 5:
		commands = append(commands, placeCmd(action(9102), beto, Drip, 2, 1))
	case 8:
		commands = append(commands, placeCmd(action(9103), ana, TreatmentPlant, 9, 9))
	case 9:
		commands = append(commands, placeCmd(action(9104), ana, Sprinkler, 1, 1))
	case 12:
		commands = append(commands, buyCmd(action(9105), beto, BiggerDrop))
	case 14:
		commands = append(commands, buyCmd(action(9106), ana, BiggerTank))
	case 210:
		commands = append(commands, placeCmd(action(9107), ana, Sprinkler, 3, 3))
	}
	return commands
}

// scriptWorld: Ana y Beto empiezan con hojas para poder comprar.
func scriptWorld() *World { return rich(holder{ana, 100}, holder{beto, 100}) }

func runScript(w *World, from, to uint64) {
	for tick := from; tick <= to; tick++ {
		w.Step(adminScript(tick))
	}
}

func TestTheSameCommandsAlwaysProduceTheSameValley(t *testing.T) {
	first, second := scriptWorld(), scriptWorld()
	runScript(first, 1, 400)
	runScript(second, 1, 400)
	if !reflect.DeepEqual(first, second) {
		t.Error("two runs of the same script gave different valleys")
	}
	if first.Epoch() != 1 {
		t.Errorf("epoch = %d, want 1", first.Epoch())
	}
	if first.HumidityContributed() == 0 {
		t.Error("no humidity was contributed")
	}

	// Las compras del guion se ejercitaron de verdad antes del reinicio.
	probe := scriptWorld()
	runScript(probe, 1, 199)
	p := mustPlayer(t, probe, ana)
	if p.LeavesSpent == 0 || len(p.Purchases) == 0 || p.WaterReused == 0 {
		t.Errorf("ana did not exercise the purchases: %+v", p)
	}
	if got := mustPlayer(t, probe, beto).Upgrades; !reflect.DeepEqual(got, []UpgradeKind{BiggerDrop}) {
		t.Errorf("beto upgrades = %v, want [BiggerDrop]", got)
	}
}

// Go-only: con azar, compras, demo y reinicio en el mismo guion, la misma
// semilla da el mismo estado completo y otra semilla da otro generador.
func TestTheSameSeedGivesTheSameWorldStateAcrossDemoResetAndPurchases(t *testing.T) {
	run := func(seed uint64) *World {
		catalog := catalogWithPPM(5_000)
		w := richWorld(catalog, holder{ana, 100}, holder{beto, 100})
		w.rng = NewSplitMix64(seed)
		runScript(w, 1, 400)
		return w
	}
	a, b, c := run(7), run(7), run(8)
	if !reflect.DeepEqual(a, b) {
		t.Error("the same seed gave different world states")
	}
	if reflect.DeepEqual(a.RngState(), c.RngState()) {
		t.Error("another seed ended with the same generator state")
	}
}
