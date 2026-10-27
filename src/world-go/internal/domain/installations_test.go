package domain

import (
	"fmt"
	"slices"
	"testing"
)

// Portadas de las pruebas de world.rs de la sección «Instalaciones y mejoras
// (T3)» (world.rs:1783-2455). Mismas cifras literales que en Rust. Las que
// pasan por reinicio o por modo demo con comandos llegan en T5; las que pasan
// por la instantánea, en snapshot_test.go.

// holder es un jugador que ya tiene hojas ganadas.
type holder struct {
	id     string
	leaves uint64
}

// richWorld es rich_world de Rust (world.rs:1783): un mundo con jugadores que
// ya tienen hojas, para probar las compras sin hacer crecer medio valle. Rust
// lo construye restaurando una instantánea (T6); aquí se inyectan.
func richWorld(catalog Catalog, holders ...holder) *World {
	w := NewWorld(catalog, 0)
	for _, h := range holders {
		slot := w.playerSlot(h.id)
		slot.record.Leaves = h.leaves
		slot.record.LeavesEarned = h.leaves
	}
	return w
}

func rich(holders ...holder) *World { return richWorld(d01(), holders...) }

func placeCmd(actionID, playerID string, kind InstallationKind, x, y uint32) Command {
	return Command{ActionID: actionID, PlayerID: playerID, Kind: PlaceInstallationCommand{Kind: kind, X: x, Y: y}}
}

func buyCmd(actionID, playerID string, kind UpgradeKind) Command {
	return Command{ActionID: actionID, PlayerID: playerID, Kind: BuyUpgradeCommand{Kind: kind}}
}

func installationAt(t *testing.T, w *World, x, y uint32) *Installation {
	t.Helper()
	return mustTile(t, w, x, y).Installation
}

func wantInstallation(t *testing.T, w *World, x, y uint32, kind InstallationKind, owner string) {
	t.Helper()
	got := installationAt(t, w, x, y)
	if got == nil || got.Kind != kind || got.OwnerID != owner {
		t.Fatalf("installation at (%d, %d) = %v, want {%d %s}", x, y, got, kind, owner)
	}
}

func humidityAt(t *testing.T, w *World, x, y uint32) uint64 {
	t.Helper()
	return mustTile(t, w, x, y).Humidity
}

func setHumidity(t *testing.T, w *World, x, y uint32, value uint64) {
	t.Helper()
	mustTile(t, w, x, y).Humidity = value
}

func wantHumidity(t *testing.T, w *World, x, y uint32, want uint64) {
	t.Helper()
	if got := humidityAt(t, w, x, y); got != want {
		t.Errorf("humidity at (%d, %d) = %d, want %d", x, y, got, want)
	}
}

func wantPurchases(t *testing.T, p *Player, want ...Purchase) {
	t.Helper()
	if !slices.Equal(p.Purchases, want) {
		t.Fatalf("purchases = %v, want %v", p.Purchases, want)
	}
}

// ---- Colocar -----------------------------------------------------------

func TestPlacingAnInstallationSpendsTheLeavesAndRecordsTheOwner(t *testing.T) {
	w := rich(holder{ana, 50})
	out := w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 5, 6)})

	wantOutcomes(t, outcomes(out), AppliedOutcome)
	wantInstallation(t, w, 5, 6, Sprinkler, ana)
	p := mustPlayer(t, w, ana)
	if p.Leaves != 40 || p.LeavesSpent != 10 || p.LeavesEarned != 50 {
		t.Errorf("leaves/spent/earned = %d/%d/%d, want 40/10/50", p.Leaves, p.LeavesSpent, p.LeavesEarned)
	}
	wantPurchases(t, p, Purchase{Sprinkler, 1})
	if p.LastActionTick != 1 {
		t.Errorf("last action tick = %d, want 1", p.LastActionTick)
	}
	assertRF27(t, w)
	again := w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 5, 6)})
	wantOutcomes(t, outcomes(again), DuplicateOutcome)
}

func TestEachPurchaseOfAKindMakesTheNextOneDearerButNotTheOtherKinds(t *testing.T) {
	w := rich(holder{ana, 200})
	w.Step([]Command{
		placeCmd(action(1), ana, Sprinkler, 0, 0),
		placeCmd(action(2), ana, Sprinkler, 1, 0),
		placeCmd(action(3), ana, Drip, 2, 0),
	})
	p := mustPlayer(t, w, ana)
	if p.LeavesSpent != 10+12+25 {
		t.Errorf("leaves spent = %d, want %d", p.LeavesSpent, 10+12+25)
	}
	for _, tc := range []struct {
		kind InstallationKind
		want uint64
	}{{Sprinkler, 14}, {Drip, 29}, {TreatmentPlant, 40}} {
		if got := w.NextPrice(p, tc.kind); got != tc.want {
			t.Errorf("next price of %d = %d, want %d", tc.kind, got, tc.want)
		}
	}
	assertRF27(t, w)
}

func TestPlacingOutOfBoundsIsRejectedWithoutCharging(t *testing.T) {
	w := rich(holder{ana, 50})
	out := w.Step([]Command{
		placeCmd(action(1), ana, Sprinkler, 32, 0),
		placeCmd(action(2), ana, Sprinkler, 0, 32),
	})
	wantOutcomes(t, outcomes(out), RejectedOutcome(OutOfBounds), RejectedOutcome(OutOfBounds))
	p := mustPlayer(t, w, ana)
	if p.Leaves != 50 || p.LeavesSpent != 0 || len(p.Purchases) != 0 {
		t.Errorf("player = %+v, want nothing charged", p)
	}
}

func TestOneInstallationPerTileEvenFromAnotherPlayer(t *testing.T) {
	w := rich(holder{ana, 50}, holder{beto, 50})
	out := w.Step([]Command{
		placeCmd(action(1), ana, Drip, 3, 3),
		placeCmd(action(2), beto, Sprinkler, 3, 3),
		placeCmd(action(3), ana, Sprinkler, 3, 3),
	})
	wantOutcomes(t, outcomes(out), AppliedOutcome, RejectedOutcome(TileOccupied), RejectedOutcome(TileOccupied))
	wantInstallation(t, w, 3, 3, Drip, ana)
	if got := mustPlayer(t, w, beto).Leaves; got != 50 {
		t.Errorf("beto leaves = %d, want 50", got)
	}
	wantPurchases(t, mustPlayer(t, w, ana), Purchase{Drip, 1})
}

func TestPlacingWithoutEnoughLeavesIsRejectedWithoutCharging(t *testing.T) {
	w := rich(holder{ana, 9})
	out := w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 1, 1)})
	wantOutcomes(t, outcomes(out), RejectedOutcome(InsufficientLeaves))
	if got := mustPlayer(t, w, ana).Leaves; got != 9 {
		t.Errorf("leaves = %d, want 9", got)
	}
	if installationAt(t, w, 1, 1) != nil {
		t.Error("an installation was placed")
	}
	// Con exactamente el precio alcanza.
	w = rich(holder{ana, 10})
	out = w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 1, 1)})
	wantOutcomes(t, outcomes(out), AppliedOutcome)
	if got := mustPlayer(t, w, ana).Leaves; got != 0 {
		t.Errorf("leaves = %d, want 0", got)
	}
}

func TestThePriceOfTheNextOneDecidesTheRejection(t *testing.T) {
	w := rich(holder{ana, 21})
	out := w.Step([]Command{
		placeCmd(action(1), ana, Sprinkler, 0, 0), // 10
		placeCmd(action(2), ana, Sprinkler, 1, 0), // 12 > 11
	})
	wantOutcomes(t, outcomes(out), AppliedOutcome, RejectedOutcome(InsufficientLeaves))
	if got := mustPlayer(t, w, ana).Leaves; got != 11 {
		t.Errorf("leaves = %d, want 11", got)
	}
}

func TestANewPlayerCannotBuyAndARejectionDoesNotBringThemIn(t *testing.T) {
	w := newTestWorld()
	out := w.Step([]Command{
		placeCmd(action(1), ana, Sprinkler, 1, 1),
		buyCmd(action(2), ana, BiggerTank),
	})
	wantOutcomes(t, outcomes(out), RejectedOutcome(InsufficientLeaves), RejectedOutcome(InsufficientLeaves))
	if _, ok := w.Player(ana); ok {
		t.Error("a rejected purchase brought the player in")
	}
}

func TestStructuralRejectionsComeBeforeTheLackOfLeaves(t *testing.T) {
	w := rich(holder{ana, 45}, holder{beto, 0})
	w.Step([]Command{placeCmd(action(1), ana, Drip, 4, 4)}) // 25
	out := w.Step([]Command{
		placeCmd(action(2), beto, Sprinkler, 99, 0),
		placeCmd(action(3), beto, Sprinkler, 4, 4),
		buyCmd(action(4), ana, BiggerTank), // 20
		buyCmd(action(5), ana, BiggerTank), // ya la tiene
	})
	wantOutcomes(t, outcomes(out),
		RejectedOutcome(OutOfBounds),
		RejectedOutcome(TileOccupied),
		AppliedOutcome,
		RejectedOutcome(UpgradeAlreadyOwned),
	)
}

func TestInstallationsCostLeavesInDemoModeToo(t *testing.T) {
	w := rich(holder{ana, 10})
	w.setDemoMode(true)
	out := w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 1, 1)})
	wantOutcomes(t, outcomes(out), AppliedOutcome)
	if got := mustPlayer(t, w, ana).Leaves; got != 0 {
		t.Errorf("leaves = %d, want 0", got)
	}
}

// Un precio 0 del catálogo dejaría entrar a un jugador nuevo con una compra;
// con el mundo lleno se rechaza como en las gotas (world.rs:2018).
func TestANewcomerCannotEnterAFullWorldEvenWithAFreePrice(t *testing.T) {
	catalog := d01()
	catalog.Sprinkler.Price = 0
	w := NewWorld(catalog, 0)
	for n := range MaxPlayers {
		w.playerSlot(fmt.Sprintf("00000000-0000-4000-9000-%012d", n))
	}
	out := w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 1, 1)})
	wantOutcomes(t, outcomes(out), RejectedOutcome(WorldFull))
	if _, ok := w.Player(ana); ok {
		t.Error("the newcomer entered a full world")
	}
}

// Go-only: un valor de enum fuera del contrato no es una instalación ni una
// mejora (Rust no puede representarlo; el codec de T6 también lo rechaza).
func TestUnknownInstallationAndUpgradeKindsAreInvalidCommands(t *testing.T) {
	w := rich(holder{ana, 100})
	out := w.Step([]Command{
		placeCmd(action(1), ana, InstallationKind(9), 1, 1),
		buyCmd(action(2), ana, UpgradeKind(9)),
	})
	wantOutcomes(t, outcomes(out), RejectedOutcome(InvalidCommand), RejectedOutcome(InvalidCommand))
	if p := mustPlayer(t, w, ana); p.Leaves != 100 || len(p.Purchases) != 0 || len(p.Upgrades) != 0 {
		t.Errorf("player = %+v, want nothing charged", p)
	}
}

// ---- Aspersor (Reutilizar) ---------------------------------------------

func TestASprinklerWatersItsArea3x3Every20TicksWithoutTouchingTheRest(t *testing.T) {
	w := rich(holder{ana, 10})
	w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 10, 10)}) // tick 1
	advanceTo(t, w, 19)
	wantHumidity(t, w, 10, 10, 0) // todavía no
	advanceTo(t, w, 20)
	// +25 y, en el mismo tick, la evaporación de 2 (tick múltiplo de 10).
	for y := uint32(9); y <= 11; y++ {
		for x := uint32(9); x <= 11; x++ {
			wantHumidity(t, w, x, y, 23)
		}
	}
	wantHumidity(t, w, 12, 10, 0)
	wantHumidity(t, w, 10, 12, 0)
	// Tick 30: -2. Tick 40: +25 y -2.
	advanceTo(t, w, 40)
	wantHumidity(t, w, 10, 10, 23-2+25-2)
}

func TestASprinklerIsClippedAtTheEdgesOfTheValley(t *testing.T) {
	w := rich(holder{ana, 10})
	w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 0, 0)})
	advanceTo(t, w, 20)
	watered := 0
	for _, tile := range w.Tiles() {
		if tile.Humidity > 0 {
			watered++
		}
	}
	if watered != 4 {
		t.Errorf("watered tiles = %d, want 4", watered)
	}
	if got := mustPlayer(t, w, ana).WaterReused; got != 4*25 {
		t.Errorf("water reused = %d, want 100", got)
	}
}

func TestASprinklerDoesNotUseTheTankNorTheHumidityContributedByDrops(t *testing.T) {
	w := rich(holder{ana, 10})
	w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 10, 10)})
	advanceTo(t, w, 100)
	p := mustPlayer(t, w, ana)
	if p.Water != 20 || p.WaterSpent != 0 {
		t.Errorf("water/spent = %d/%d, want 20/0", p.Water, p.WaterSpent)
	}
	if w.HumidityContributed() != 0 {
		t.Errorf("humidity contributed = %d, want 0", w.HumidityContributed())
	}
	assertRF27(t, w)
}

func TestTheLeavesOfATileGrownByASprinklerGoToItsOwner(t *testing.T) {
	w := rich(holder{ana, 10})
	w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 10, 10)})
	// Beto deja 75 en una casilla del área; la evaporación la baja y el
	// aspersor de Ana (tick 40) la empuja al umbral.
	w.Step(drops(10, 3, beto, 10, 11))
	advanceTo(t, w, 39)
	if mustTile(t, w, 10, 11).Stage != DrySoil {
		t.Fatal("the tile grew too early")
	}
	advanceTo(t, w, 40)
	if mustTile(t, w, 10, 11).Stage != Grass {
		t.Fatal("the tile did not grow")
	}
	p := mustPlayer(t, w, ana)
	if p.LeavesEarned != 11 || p.Leaves != 1 || p.LeavesSpent != 10 {
		t.Errorf("earned/leaves/spent = %d/%d/%d, want 11/1/10", p.LeavesEarned, p.Leaves, p.LeavesSpent)
	}
	if got := mustPlayer(t, w, beto).Leaves; got != 0 {
		t.Errorf("beto leaves = %d, want 0", got)
	}
	assertRF27(t, w)
}

func TestOverlappingSprinklersAddUpAndCountAsReusedWater(t *testing.T) {
	w := rich(holder{ana, 30}, holder{beto, 30})
	w.Step([]Command{
		placeCmd(action(1), ana, Sprinkler, 10, 10),
		placeCmd(action(2), beto, Sprinkler, 11, 10),
	})
	advanceTo(t, w, 20)
	wantHumidity(t, w, 10, 10, 48) // dos aspersores: 50 - 2
	wantHumidity(t, w, 9, 10, 23)
	wantHumidity(t, w, 12, 10, 23)
	if got := mustPlayer(t, w, ana).WaterReused; got != 9*25 {
		t.Errorf("ana water reused = %d, want 225", got)
	}
	if got := mustPlayer(t, w, beto).WaterReused; got != 9*25 {
		t.Errorf("beto water reused = %d, want 225", got)
	}
}

func TestASprinklerUsesTheAreaTheIntervalAndTheHumidityOfTheCatalog(t *testing.T) {
	catalog := d01()
	catalog.Sprinkler.AreaSide = 4 // 1 casilla antes y 2 después
	catalog.Sprinkler.Humidity = 10
	catalog.Sprinkler.IntervalSeconds = 1
	w := richWorld(catalog, holder{ana, 10})
	w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 10, 10)})
	advanceTo(t, w, 10)
	// Cuadrado de lado 4: x e y de 9 a 12.
	type xy struct{ x, y uint32 }
	var watered []xy
	for y := range ValleyHeight {
		for x := range ValleyWidth {
			if humidityAt(t, w, x, y) > 0 {
				watered = append(watered, xy{x, y})
			}
		}
	}
	if len(watered) != 16 || watered[0] != (xy{9, 9}) || watered[15] != (xy{12, 12}) {
		t.Fatalf("watered = %v, want the 16 tiles from (9, 9) to (12, 12)", watered)
	}
	wantHumidity(t, w, 9, 9, 10-2) // 10 y la evaporación del tick 10
}

// ---- Goteo (Reducir) ---------------------------------------------------

func TestDripHalvesTheEvaporationInsideItsArea5x5Only(t *testing.T) {
	w := rich(holder{ana, 25})
	w.Step([]Command{placeCmd(action(1), ana, Drip, 10, 10)})
	for _, p := range [][2]uint32{{10, 10}, {8, 8}, {12, 12}, {7, 10}, {13, 10}} {
		setHumidity(t, w, p[0], p[1], 10)
	}
	advanceTo(t, w, 10)
	wantHumidity(t, w, 10, 10, 9)
	wantHumidity(t, w, 8, 8, 9)
	wantHumidity(t, w, 12, 12, 9)
	wantHumidity(t, w, 7, 10, 8)  // fuera del área
	wantHumidity(t, w, 13, 10, 8) // fuera del área
}

func TestDripCountsTheHumidityItSavedAsReducedWater(t *testing.T) {
	w := rich(holder{ana, 25})
	w.Step([]Command{placeCmd(action(1), ana, Drip, 10, 10)})
	// Con 10 y con 2 se ahorra 1 (pierde 1 en vez de 2). Con 1 pierde 1 igual
	// y con 0 no hay nada que ahorrar.
	setHumidity(t, w, 10, 10, 10)
	setHumidity(t, w, 11, 10, 2)
	setHumidity(t, w, 12, 10, 1)
	advanceTo(t, w, 10)
	if got := mustPlayer(t, w, ana).WaterReduced; got != 2 {
		t.Errorf("water reduced = %d, want 2", got)
	}
	// Quedan 9, 1 y 0: solo la primera ahorra algo.
	advanceTo(t, w, 20)
	if got := mustPlayer(t, w, ana).WaterReduced; got != 2+1 {
		t.Errorf("water reduced = %d, want 3", got)
	}
}

func TestDripDoesNothingInDemoMode(t *testing.T) {
	w := rich(holder{ana, 25})
	w.setDemoMode(true)
	w.Step([]Command{placeCmd(action(1), ana, Drip, 10, 10)})
	setHumidity(t, w, 10, 10, 10)
	advanceTo(t, w, 100)
	wantHumidity(t, w, 10, 10, 10)
	if got := mustPlayer(t, w, ana).WaterReduced; got != 0 {
		t.Errorf("water reduced = %d, want 0", got)
	}
}

func TestOverlappingDripsDoNotStackAndTheFirstOneGetsTheCredit(t *testing.T) {
	w := rich(holder{ana, 25}, holder{beto, 25})
	w.Step([]Command{
		placeCmd(action(1), beto, Drip, 11, 10),
		placeCmd(action(2), ana, Drip, 10, 10),
	})
	setHumidity(t, w, 10, 10, 10)
	advanceTo(t, w, 10)
	wantHumidity(t, w, 10, 10, 9) // a la mitad una vez, no a la cuarta parte
	// El primero en el orden de casillas (fila a fila) es el de Ana.
	if got := mustPlayer(t, w, ana).WaterReduced; got != 1 {
		t.Errorf("ana water reduced = %d, want 1", got)
	}
	if got := mustPlayer(t, w, beto).WaterReduced; got != 0 {
		t.Errorf("beto water reduced = %d, want 0", got)
	}
}

func TestDripDividesTheEvaporationByTheCatalogDivisorRoundingDown(t *testing.T) {
	catalog := d01()
	catalog.Evaporation.HumidityPerSecond = 3
	catalog.Drip.EvaporationDivisor = 2
	w := richWorld(catalog, holder{ana, 25})
	w.Step([]Command{placeCmd(action(1), ana, Drip, 10, 10)})
	setHumidity(t, w, 10, 10, 10)
	advanceTo(t, w, 10)
	wantHumidity(t, w, 10, 10, 9) // 3 / 2 = 1
	if got := mustPlayer(t, w, ana).WaterReduced; got != 2 {
		t.Errorf("water reduced = %d, want 2", got)
	}

	catalog.Drip.EvaporationDivisor = 4
	w = richWorld(catalog, holder{ana, 25})
	w.Step([]Command{placeCmd(action(1), ana, Drip, 10, 10)})
	setHumidity(t, w, 10, 10, 10)
	advanceTo(t, w, 10)
	wantHumidity(t, w, 10, 10, 10) // 3 / 4 = 0
}

// Go-only: un divisor 0 se trata como 1 (world.rs:581, .max(1)) y no entra en
// pánico.
func TestADripDivisorOfZeroActsAsOne(t *testing.T) {
	catalog := d01()
	catalog.Drip.EvaporationDivisor = 0
	w := richWorld(catalog, holder{ana, 25})
	w.Step([]Command{placeCmd(action(1), ana, Drip, 10, 10)})
	setHumidity(t, w, 10, 10, 10)
	advanceTo(t, w, 10)
	wantHumidity(t, w, 10, 10, 8)
	if got := mustPlayer(t, w, ana).WaterReduced; got != 0 {
		t.Errorf("water reduced = %d, want 0", got)
	}
}

func TestDripAlsoSavesOnABurningTileBecauseItStillEvaporates(t *testing.T) {
	w := rich(holder{ana, 25})
	w.Step([]Command{placeCmd(action(1), ana, Drip, 10, 10)})
	*mustTile(t, w, 10, 10) = Tile{
		Humidity:     10,
		Fire:         &Fire{},
		Installation: installationAt(t, w, 10, 10),
	}
	advanceTo(t, w, 10)
	wantHumidity(t, w, 10, 10, 9)
	if got := mustPlayer(t, w, ana).WaterReduced; got != 1 {
		t.Errorf("water reduced = %d, want 1", got)
	}
}

// ---- Planta de tratamiento (Reciclar) ----------------------------------

func TestATreatmentPlantAddsAnExtraLitreEvery20Ticks(t *testing.T) {
	w := rich(holder{ana, 40})
	w.Step([]Command{placeCmd(action(1), ana, TreatmentPlant, 2, 2)})
	w.Step(drops(10, 10, ana, 20, 20)) // 10 L fuera: quedan 10
	advanceTo(t, w, 19)
	if got := mustPlayer(t, w, ana).Water; got != 10 {
		t.Fatalf("water = %d, want 10", got)
	}
	advanceTo(t, w, 20)
	p := mustPlayer(t, w, ana)
	if p.Water != 12 { // la recarga base y la planta, 1 L cada una
		t.Errorf("water = %d, want 12", p.Water)
	}
	if p.WaterRecycled != 1 || p.WaterRefilled != 20+2 {
		t.Errorf("recycled/refilled = %d/%d, want 1/22", p.WaterRecycled, p.WaterRefilled)
	}
	assertRF27(t, w)
}

func TestAPlantOnlyCountsTheLitresThatFitInTheTank(t *testing.T) {
	w := rich(holder{ana, 40})
	w.Step([]Command{placeCmd(action(1), ana, TreatmentPlant, 2, 2)})
	advanceTo(t, w, 200)
	if p := mustPlayer(t, w, ana); p.Water != 20 || p.WaterRecycled != 0 { // depósito lleno
		t.Errorf("water/recycled = %d/%d, want 20/0", p.Water, p.WaterRecycled)
	}

	w.Step(drops(10, 1, ana, 20, 20)) // 19
	advanceTo(t, w, 220)
	// La recarga base lo llenó primero.
	if p := mustPlayer(t, w, ana); p.Water != 20 || p.WaterRecycled != 0 {
		t.Errorf("water/recycled = %d/%d, want 20/0", p.Water, p.WaterRecycled)
	}
	assertRF27(t, w)
}

func TestSeveralPlantsAddUpAndEachCreditsItsOwner(t *testing.T) {
	w := rich(holder{ana, 100}, holder{beto, 100})
	w.Step([]Command{
		placeCmd(action(1), ana, TreatmentPlant, 2, 2),
		placeCmd(action(2), ana, TreatmentPlant, 3, 2),
		placeCmd(action(3), beto, TreatmentPlant, 4, 2),
	})
	w.Step(drops(10, 12, ana, 20, 20))  // 8 L
	w.Step(drops(30, 12, beto, 20, 21)) // 8 L
	advanceTo(t, w, 20)
	if p := mustPlayer(t, w, ana); p.Water != 8+1+2 || p.WaterRecycled != 2 {
		t.Errorf("ana water/recycled = %d/%d, want 11/2", p.Water, p.WaterRecycled)
	}
	if p := mustPlayer(t, w, beto); p.Water != 8+1+1 || p.WaterRecycled != 1 {
		t.Errorf("beto water/recycled = %d/%d, want 10/1", p.Water, p.WaterRecycled)
	}
	assertRF27(t, w)
}

func TestAPlantUsesTheIntervalAndTheLitresOfTheCatalog(t *testing.T) {
	catalog := d01()
	catalog.TreatmentPlant.IntervalSeconds = 1
	catalog.TreatmentPlant.RefillAmount = 3
	w := richWorld(catalog, holder{ana, 40})
	w.Step([]Command{placeCmd(action(1), ana, TreatmentPlant, 2, 2)})
	w.Step(drops(10, 15, ana, 20, 20)) // 5 L
	advanceTo(t, w, 10)
	if p := mustPlayer(t, w, ana); p.Water != 8 || p.WaterRecycled != 3 {
		t.Errorf("water/recycled = %d/%d, want 8/3", p.Water, p.WaterRecycled)
	}
}

// ---- Instalaciones de un jugador ausente (RF-25) -----------------------

func TestInstallationsStopWhenTheirOwnerIsAwayForTooLongAndResumeWithAnAction(t *testing.T) {
	catalog := d01()
	catalog.OfflineInstallationSeconds = 3 // 30 ticks
	w := richWorld(catalog, holder{ana, 100})
	w.Step([]Command{
		placeCmd(action(1), ana, Sprinkler, 10, 10),
		placeCmd(action(2), ana, TreatmentPlant, 20, 20),
	}) // tick 1
	w.Step(drops(10, 10, ana, 5, 5)) // tick 2: última acción
	advanceTo(t, w, 20)
	if humidityAt(t, w, 10, 10) == 0 {
		t.Fatal("the sprinkler stopped within the time limit")
	}
	p := mustPlayer(t, w, ana)
	if p.WaterRecycled != 1 || p.WaterReused != 9*25 {
		t.Fatalf("recycled/reused = %d/%d, want 1/225", p.WaterRecycled, p.WaterReused)
	}
	after20 := humidityAt(t, w, 10, 10)

	// Tick 40: 38 ticks desde la última acción (2) > 30. El aspersor y la
	// planta ya no actúan; la recarga base y la evaporación, sí.
	advanceTo(t, w, 40)
	p = mustPlayer(t, w, ana)
	if p.WaterRecycled != 1 || p.WaterReused != 9*25 {
		t.Errorf("recycled/reused = %d/%d, want 1/225", p.WaterRecycled, p.WaterReused)
	}
	wantHumidity(t, w, 10, 10, after20-2*2) // solo la evaporación

	// Una acción del dueño las reactiva a partir de ese momento.
	w.Step(drops(100, 1, ana, 6, 6)) // tick 41
	advanceTo(t, w, 60)
	if got := mustPlayer(t, w, ana).WaterReused; got != 18*25 {
		t.Errorf("water reused = %d, want 450", got)
	}
}

// Go-only: el límite se incluye (world.rs:481, `<= window`) y un dueño que no
// existe no activa nada.
func TestTheOfflineLimitIsInclusiveAndAnUnknownOwnerIsNeverActive(t *testing.T) {
	catalog := d01()
	catalog.OfflineInstallationSeconds = 3 // 30 ticks
	w := richWorld(catalog, holder{ana, 0})
	mustPlayer(t, w, ana).LastActionTick = 10
	if !w.ownerIsActive(ana, 40) {
		t.Error("an owner away for exactly the window is inactive")
	}
	if w.ownerIsActive(ana, 41) {
		t.Error("an owner away for the window plus one tick is active")
	}
	if w.ownerIsActive(beto, 0) {
		t.Error("an unknown owner is active")
	}
}

// ---- Mejoras -----------------------------------------------------------

func TestTheBiggerTankAddsCapacityForTheCatalogPriceOnce(t *testing.T) {
	w := rich(holder{ana, 30})
	out := w.Step([]Command{buyCmd(action(1), ana, BiggerTank)})
	wantOutcomes(t, outcomes(out), AppliedOutcome)
	p := mustPlayer(t, w, ana)
	if p.TankCapacity != 30 {
		t.Errorf("tank capacity = %d, want 30", p.TankCapacity)
	}
	if p.Water != 20 { // el espacio nuevo no se llena gratis
		t.Errorf("water = %d, want 20", p.Water)
	}
	if p.Leaves != 10 || p.LeavesSpent != 20 {
		t.Errorf("leaves/spent = %d/%d, want 10/20", p.Leaves, p.LeavesSpent)
	}
	if !slices.Equal(p.Upgrades, []UpgradeKind{BiggerTank}) {
		t.Errorf("upgrades = %v, want [BiggerTank]", p.Upgrades)
	}

	out = w.Step([]Command{buyCmd(action(2), ana, BiggerTank)})
	wantOutcomes(t, outcomes(out), RejectedOutcome(UpgradeAlreadyOwned))
	if got := mustPlayer(t, w, ana).Leaves; got != 10 {
		t.Errorf("leaves = %d, want 10", got)
	}
	advanceTo(t, w, 40)
	if got := mustPlayer(t, w, ana).Water; got != 22 { // ahora recarga por encima de 20
		t.Errorf("water = %d, want 22", got)
	}
	assertRF27(t, w)
}

func TestAnUpgradeWithoutEnoughLeavesIsRejectedWithoutCharging(t *testing.T) {
	w := rich(holder{ana, 29})
	out := w.Step([]Command{buyCmd(action(1), ana, BiggerDrop)})
	wantOutcomes(t, outcomes(out), RejectedOutcome(InsufficientLeaves))
	p := mustPlayer(t, w, ana)
	if p.Leaves != 29 || p.LeavesSpent != 0 || len(p.Upgrades) != 0 {
		t.Errorf("player = %+v, want nothing charged", p)
	}
}

func TestTheBiggerDropHumidifies3x3ForOneLitreAndCountsEveryTile(t *testing.T) {
	w := rich(holder{ana, 30})
	w.Step([]Command{buyCmd(action(1), ana, BiggerDrop)})
	out := w.Step(drops(10, 1, ana, 10, 10))
	wantOutcomes(t, outcomes(out), AppliedOutcome)
	for y := uint32(9); y <= 11; y++ {
		for x := uint32(9); x <= 11; x++ {
			wantHumidity(t, w, x, y, 25)
		}
	}
	wantHumidity(t, w, 8, 10, 0)
	if p := mustPlayer(t, w, ana); p.Water != 19 || p.WaterSpent != 1 {
		t.Errorf("water/spent = %d/%d, want 19/1", p.Water, p.WaterSpent)
	}
	if w.HumidityContributed() != 9*25 {
		t.Errorf("humidity contributed = %d, want 225", w.HumidityContributed())
	}
}

func TestTheBiggerDropIsClippedAtTheEdgesAndNeedsAnInBoundsCentre(t *testing.T) {
	w := rich(holder{ana, 30})
	w.Step([]Command{buyCmd(action(1), ana, BiggerDrop)})
	w.Step(drops(10, 1, ana, 0, 0))
	if w.HumidityContributed() != 4*25 {
		t.Fatalf("humidity contributed = %d, want 100", w.HumidityContributed())
	}
	out := w.Step(drops(20, 1, ana, 32, 0))
	wantOutcomes(t, outcomes(out), RejectedOutcome(OutOfBounds))
	if w.HumidityContributed() != 4*25 {
		t.Errorf("humidity contributed = %d, want 100", w.HumidityContributed())
	}
	if got := mustPlayer(t, w, ana).Water; got != 19 {
		t.Errorf("water = %d, want 19", got)
	}
}

func TestTheBiggerDropPaysEveryLeafOfTheTilesItGrowsToTheDropper(t *testing.T) {
	w := rich(holder{ana, 30}, holder{beto, 0})
	w.Step([]Command{buyCmd(action(1), ana, BiggerDrop)})
	w.Step(drops(10, 3, beto, 10, 10)) // 75 en el centro
	w.Step(drops(20, 3, beto, 11, 10)) // 75 a la derecha
	before := mustPlayer(t, w, ana).Leaves
	w.Step(drops(30, 1, ana, 10, 10))
	if mustTile(t, w, 10, 10).Stage != Grass || mustTile(t, w, 11, 10).Stage != Grass {
		t.Error("the tiles did not grow")
	}
	if got := mustPlayer(t, w, ana).Leaves; got != before+2 {
		t.Errorf("leaves = %d, want %d", got, before+2)
	}
	assertRF27(t, w)
}

func TestTheBiggerDropUsesTheAreaOfTheCatalog(t *testing.T) {
	catalog := d01()
	catalog.BiggerDrop.AreaSide = 5
	w := richWorld(catalog, holder{ana, 30})
	w.Step([]Command{buyCmd(action(1), ana, BiggerDrop)})
	w.Step(drops(10, 1, ana, 10, 10))
	if w.HumidityContributed() != 25*25 {
		t.Errorf("humidity contributed = %d, want 625", w.HumidityContributed())
	}
	wantHumidity(t, w, 8, 8, 25)
}

// ---- Pruebas de Fase 1 (Riego por Goteo y Planta de Tratamiento) ----

func TestDripShieldPausesDryTicksAndPreventsWithering(t *testing.T) {
	w := rich(holder{ana, 100})
	w.Step([]Command{placeCmd(action(1), ana, Drip, 5, 5)})
	w.Step([]Command{dropCmd(action(2), ana, 6, 6)})
	w.Step([]Command{dropCmd(action(3), ana, 6, 6)})
	if mustTile(t, w, 6, 6).Stage != Grass {
		t.Fatal("tile (6, 6) did not grow to grass")
	}
	mustTile(t, w, 6, 6).Humidity = 0
	advanceTo(t, w, 200)
	tile := mustTile(t, w, 6, 6)
	if tile.Stage != Grass || tile.DryTicks != 0 {
		t.Errorf("stage = %v, dry_ticks = %d, want Grass and 0", tile.Stage, tile.DryTicks)
	}
}

func TestDripShieldSuppressesFireIgnition(t *testing.T) {
	catalog := d01()
	catalog.Fire.IgnitionChancePPM = 1_000_000 // 100%
	w := richWorld(catalog, holder{ana, 100})
	w.Step([]Command{placeCmd(action(1), ana, Drip, 5, 5)})
	mustTile(t, w, 6, 6).Stage = Grass
	mustTile(t, w, 6, 6).Humidity = 0
	mustTile(t, w, 20, 20).Stage = Grass
	mustTile(t, w, 20, 20).Humidity = 0

	w.Step(nil)
	if mustTile(t, w, 6, 6).Fire != nil {
		t.Error("shielded tile (6, 6) caught fire")
	}
	if mustTile(t, w, 20, 20).Fire == nil {
		t.Error("unshielded tile (20, 20) did not catch fire")
	}
}

func TestDripShieldDoublesDropHumidity(t *testing.T) {
	w := rich(holder{ana, 100})
	w.Step([]Command{placeCmd(action(1), ana, Drip, 10, 10)})
	w.Step([]Command{dropCmd(action(2), ana, 0, 0)})
	if got := humidityAt(t, w, 0, 0); got != 25 {
		t.Errorf("humidity at (0, 0) = %d, want 25", got)
	}
	w.Step([]Command{dropCmd(action(3), ana, 11, 11)})
	if got := humidityAt(t, w, 11, 11); got != 50 {
		t.Errorf("humidity at (11, 11) = %d, want 50 (doubled)", got)
	}
}

func TestTreatmentPlantPulsesGrowthToNeighborsAndCreditsOwner(t *testing.T) {
	w := rich(holder{ana, 100})
	w.Step([]Command{placeCmd(action(1), ana, TreatmentPlant, 5, 5)})
	leavesBefore := mustPlayer(t, w, ana).Leaves
	advanceTo(t, w, 20)

	if mustTile(t, w, 4, 4).Stage != Grass || mustTile(t, w, 5, 4).Stage != Grass || mustTile(t, w, 6, 6).Stage != Grass {
		t.Error("neighbor tiles did not grow to grass")
	}
	if mustTile(t, w, 5, 5).Stage != DrySoil {
		t.Error("center tile (5, 5) should remain DrySoil")
	}
	ana := mustPlayer(t, w, ana)
	if ana.Leaves <= leavesBefore {
		t.Errorf("leaves = %d, want > %d", ana.Leaves, leavesBefore)
	}
	if ana.TilesGrown == 0 {
		t.Error("tiles grown should be > 0")
	}
}

func TestInstallationPlacedInitializesWithCatalogDurability(t *testing.T) {
	w := rich(holder{ana, 100})
	w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 5, 5)})
	inst := mustTile(t, w, 5, 5).Installation
	if inst == nil || inst.RemainingDurability != 30 {
		t.Fatalf("remaining durability = %v, want 30", inst)
	}
}

func TestInstallationDurabilityDecrementsPerCycleAndUninstallsAtZero(t *testing.T) {
	w := rich(holder{ana, 100})
	w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 5, 5)})
	mustTile(t, w, 5, 5).Installation.RemainingDurability = 2

	// Avanzar 1 ciclo (20 ticks)
	advanceTo(t, w, 20)
	inst := mustTile(t, w, 5, 5).Installation
	if inst == nil || inst.RemainingDurability != 1 {
		t.Fatalf("durability after 1 cycle = %v, want 1", inst)
	}

	// Avanzar 2do ciclo (40 ticks): llega a 0 y se desinstala
	advanceTo(t, w, 40)
	if mustTile(t, w, 5, 5).Installation != nil {
		t.Fatalf("installation was not uninstalled upon reaching 0 durability")
	}
}

func TestDripDurabilityDecrementsAndUninstallsAtZero(t *testing.T) {
	w := rich(holder{ana, 100})
	w.Step([]Command{placeCmd(action(1), ana, Drip, 8, 8)})
	mustTile(t, w, 8, 8).Installation.RemainingDurability = 1

	// Avanzar 1 ciclo (20 ticks)
	advanceTo(t, w, 20)
	if mustTile(t, w, 8, 8).Installation != nil {
		t.Fatalf("drip was not uninstalled upon reaching 0 durability")
	}
}
