package domain

import (
	"reflect"
	"testing"
)

// Portadas de world_fire_tests.rs (incendios; D-01, RF-28, RF-29, RF-30,
// CP-04, CP-10). Las que dependen de reinicio, de restaurar una instantánea o
// de su estado del generador están en snapshot_test.go.

const caro = "c3a5e2f0-1b4d-4c6e-8f7a-2d9b0c1e3f03"

func catalogWithPPM(ppm uint32) Catalog {
	catalog := d01()
	catalog.Fire.IgnitionChancePPM = ppm
	return catalog
}

// catalogWithPropagationPPM es catalog_with_propagation_ppm de Rust: sin
// ignición espontánea y con la propagación indicada.
func catalogWithPropagationPPM(ppm uint32) Catalog {
	catalog := d01()
	catalog.Fire.IgnitionChancePPM = 0
	catalog.Fire.PropagationChancePPM = ppm
	return catalog
}

// worldWith es world_with de Rust (world_fire_tests.rs:25): un mundo con las
// casillas dadas ya en su estado. Rust restaura una instantánea (T6); aquí se
// ponen directamente.
func worldWith(catalog Catalog, seed uint64, setup func(w *World)) *World {
	w := NewWorld(catalog, seed)
	setup(w)
	return w
}

func tileIdx(x, y uint32) int { return int(y)*int(ValleyWidth) + int(x) }

func fireOf(dropsReceived, burningTicks uint32) *Fire {
	return &Fire{DropsReceived: dropsReceived, BurningTicks: burningTicks}
}

// burningGrass es pasto seco que arde, con la humedad indicada.
func burningGrass(humidity uint64) Tile {
	return Tile{Stage: Grass, Humidity: humidity, Fire: fireOf(0, 0)}
}

func dryGrass() Tile { return Tile{Stage: Grass} }

func wantFire(t *testing.T, w *World, x, y uint32, want *Fire) {
	t.Helper()
	got := mustTile(t, w, x, y).Fire
	if (got == nil) != (want == nil) || (got != nil && *got != *want) {
		t.Errorf("fire at (%d, %d) = %v, want %v", x, y, got, want)
	}
}

func leavesOf(w *World, playerID string) uint64 {
	if p, ok := w.Player(playerID); ok {
		return p.Leaves
	}
	return 0
}

// withUpgrade da a un jugador (de arranque) una mejora ya comprada.
func withUpgrade(w *World, playerID string, kind UpgradeKind) {
	p := &w.playerSlot(playerID).record
	p.Upgrades = append(p.Upgrades, kind)
}

// ---- Aparición ---------------------------------------------------------

func TestADryTileWithAPlantIgnitesWhenTheChanceIsCertain(t *testing.T) {
	w := worldWith(catalogWithPPM(1_000_000), 1, func(w *World) {
		w.tiles[tileIdx(5, 5)] = dryGrass()
	})
	w.Step(nil)
	wantFire(t, w, 5, 5, fireOf(0, 0))
}

func TestOnlyDryTilesWithAPlantAreEligible(t *testing.T) {
	w := worldWith(catalogWithPPM(1_000_000), 1, func(w *World) {
		w.tiles[tileIdx(1, 1)] = Tile{Stage: Grass, Humidity: 5}
		w.tiles[tileIdx(2, 2)] = Tile{} // tierra seca
		w.tiles[tileIdx(3, 3)] = dryGrass()
	})
	w.Step(nil)
	wantFire(t, w, 1, 1, nil) // con humedad no arde
	wantFire(t, w, 2, 2, nil) // sin planta no arde
	wantFire(t, w, 3, 3, fireOf(0, 0))
}

func TestNothingIgnitesWithoutChanceAndBurningTilesAreNotIgnitedAgain(t *testing.T) {
	w := worldWith(catalogWithPPM(0), 1, func(w *World) {
		w.tiles[tileIdx(5, 5)] = dryGrass()
	})
	idle(w, 50)
	wantFire(t, w, 5, 5, nil)

	w = worldWith(catalogWithPPM(1_000_000), 1, func(w *World) {
		w.tiles[tileIdx(5, 5)] = Tile{Stage: Grass, Fire: fireOf(2, 7)}
	})
	w.Step(nil)
	wantFire(t, w, 5, 5, fireOf(2, 8)) // sigue su cuenta
}

func TestDemoModeHasNoNewFires(t *testing.T) {
	w := worldWith(catalogWithPPM(1_000_000), 1, func(w *World) {
		w.demoMode = true
		w.tiles[tileIdx(5, 5)] = dryGrass()
	})
	idle(w, 20)
	wantFire(t, w, 5, 5, nil)
}

// Dos casillas elegibles: la de la fila 0 gasta el primer número y la de la
// fila 3 el segundo, sea cual sea el orden en el que se crearon.
func TestTheIgnitionDrawsOneNumberPerEligibleTileInRowMajorOrder(t *testing.T) {
	w := worldWith(catalogWithPPM(500_000), 11, func(w *World) {
		w.tiles[tileIdx(9, 3)] = dryGrass()
		w.tiles[tileIdx(20, 0)] = dryGrass()
	})
	rng := NewSplitMix64(11)
	first := rng.ChancePPM(500_000)
	second := rng.ChancePPM(500_000)
	w.Step(nil)
	if got := mustTile(t, w, 20, 0).Fire != nil; got != first {
		t.Errorf("(20, 0) burns = %v, want %v", got, first)
	}
	if got := mustTile(t, w, 9, 3).Fire != nil; got != second {
		t.Errorf("(9, 3) burns = %v, want %v", got, second)
	}
	if got, want := w.RngState(), rng.Bytes(); !reflect.DeepEqual(got, want) {
		t.Errorf("rng state = %v, want %v", got, want)
	}
}

func TestATileThatWithersToDrySoilInTheTickIsNotEligible(t *testing.T) {
	w := worldWith(catalogWithPPM(1_000_000), 1, func(w *World) {
		w.tiles[tileIdx(4, 4)] = Tile{Stage: Grass, DryTicks: 99}
	})
	w.Step(nil) // el marchitamiento la deja en tierra seca antes del sorteo
	if mustTile(t, w, 4, 4).Stage != DrySoil {
		t.Error("the tile did not wither")
	}
	wantFire(t, w, 4, 4, nil)
}

// ---- Propagación a vecinas (nivel 2, B3) ---------------------------------

func TestFireSpreadsToAnOrthogonalEligibleNeighborWhenTheChanceIsCertain(t *testing.T) {
	w := worldWith(catalogWithPropagationPPM(1_000_000), 1, func(w *World) {
		w.tiles[tileIdx(5, 5)] = burningGrass(0)
		w.tiles[tileIdx(5, 6)] = dryGrass()
	})
	w.Step(nil)
	wantFire(t, w, 5, 6, fireOf(0, 0))
}

func TestFireDoesNotSpreadWithoutChance(t *testing.T) {
	w := worldWith(catalogWithPropagationPPM(0), 1, func(w *World) {
		w.tiles[tileIdx(5, 5)] = burningGrass(0)
		w.tiles[tileIdx(5, 6)] = dryGrass()
	})
	idle(w, 20)
	wantFire(t, w, 5, 6, nil)
}

func TestFireDoesNotSpreadToIneligibleNeighbors(t *testing.T) {
	w := worldWith(catalogWithPropagationPPM(1_000_000), 1, func(w *World) {
		w.tiles[tileIdx(5, 5)] = burningGrass(0)
		w.tiles[tileIdx(5, 4)] = Tile{Stage: Grass, Humidity: 10} // con humedad
		w.tiles[tileIdx(4, 5)] = Tile{}                           // tierra seca
		w.tiles[tileIdx(6, 5)] = dryGrass()                       // elegible
	})
	w.Step(nil)
	wantFire(t, w, 5, 4, nil)
	wantFire(t, w, 4, 5, nil)
	wantFire(t, w, 6, 5, fireOf(0, 0))
}

func TestFireDoesNotSpreadDiagonally(t *testing.T) {
	w := worldWith(catalogWithPropagationPPM(1_000_000), 1, func(w *World) {
		w.tiles[tileIdx(5, 5)] = burningGrass(0)
		w.tiles[tileIdx(6, 6)] = dryGrass()
	})
	w.Step(nil)
	wantFire(t, w, 6, 6, nil)
}

// Solo propagan los fuegos que ardían al empezar el paso: (7, 5) no prende en
// el mismo tick que (6, 5), sino en el siguiente.
func TestPropagationIsSynchronousOneStepPerTick(t *testing.T) {
	w := worldWith(catalogWithPropagationPPM(1_000_000), 1, func(w *World) {
		w.tiles[tileIdx(5, 5)] = burningGrass(0)
		w.tiles[tileIdx(6, 5)] = dryGrass()
		w.tiles[tileIdx(7, 5)] = dryGrass()
	})
	w.Step(nil)
	if mustTile(t, w, 6, 5).Fire == nil {
		t.Error("tick 1: the fire should reach (6, 5)")
	}
	wantFire(t, w, 7, 5, nil)
	w.Step(nil)
	if mustTile(t, w, 7, 5).Fire == nil {
		t.Error("tick 2: the fire should reach (7, 5)")
	}
}

func TestFireDoesNotPropagateInDemoMode(t *testing.T) {
	w := worldWith(catalogWithPropagationPPM(1_000_000), 1, func(w *World) {
		w.demoMode = true
		w.tiles[tileIdx(5, 5)] = burningGrass(0)
		w.tiles[tileIdx(5, 6)] = dryGrass()
	})
	idle(w, 20)
	wantFire(t, w, 5, 6, nil)
}

// La comparativa usa valles de otros tamaños (NewWorldWithSize): la
// propagación usa las dimensiones del mundo, no las del juego.
func TestFireSpreadsAcrossAValleyOfAnotherSize(t *testing.T) {
	w := NewWorldWithSize(catalogWithPropagationPPM(1_000_000), 1, 64, 8)
	at := func(x, y int) int { return y*64 + x }
	w.tiles[at(40, 2)] = burningGrass(0)
	w.tiles[at(41, 2)] = dryGrass()
	w.tiles[at(40, 3)] = dryGrass()
	w.tiles[at(63, 7)] = burningGrass(0)
	w.tiles[at(0, 0)] = dryGrass() // no es vecina de ningún fuego
	w.Step(nil)
	wantFire(t, w, 41, 2, fireOf(0, 0))
	wantFire(t, w, 40, 3, fireOf(0, 0))
	wantFire(t, w, 0, 0, nil)
}

// Dos casillas elegibles junto a fuegos: la de la fila 0 gasta el primer
// número y la de la fila 4 el segundo. Las que no prenden gastan después un
// número de la ignición (que corre tras la propagación, aunque su
// probabilidad sea 0).
func TestPropagationDrawsInRowMajorOrder(t *testing.T) {
	w := worldWith(catalogWithPropagationPPM(500_000), 42, func(w *World) {
		w.tiles[tileIdx(4, 0)] = burningGrass(0)
		w.tiles[tileIdx(5, 0)] = dryGrass()
		w.tiles[tileIdx(2, 3)] = burningGrass(0)
		w.tiles[tileIdx(2, 4)] = dryGrass()
	})
	rng := NewSplitMix64(42)
	first := rng.ChancePPM(500_000)
	second := rng.ChancePPM(500_000)
	w.Step(nil)
	if got := mustTile(t, w, 5, 0).Fire != nil; got != first {
		t.Errorf("(5, 0) burns = %v, want %v", got, first)
	}
	if got := mustTile(t, w, 2, 4).Fire != nil; got != second {
		t.Errorf("(2, 4) burns = %v, want %v", got, second)
	}
	if !first {
		rng.ChancePPM(0)
	}
	if !second {
		rng.ChancePPM(0)
	}
	if got, want := w.RngState(), rng.Bytes(); !reflect.DeepEqual(got, want) {
		t.Errorf("rng state = %v, want %v", got, want)
	}
}

// ---- Apagar con gotas --------------------------------------------------

func TestThreeDropsPutTheFireOutAndTheThirdPlayerGetsTheBonus(t *testing.T) {
	w := worldWith(catalogWithPPM(0), 1, func(w *World) {
		w.tiles[tileIdx(7, 7)] = burningGrass(0)
	})
	w.Step([]Command{dropCmd(action(1), ana, 7, 7)})
	wantFire(t, w, 7, 7, fireOf(1, 1))
	w.Step([]Command{dropCmd(action(2), beto, 7, 7)})
	wantFire(t, w, 7, 7, fireOf(2, 2))
	if leavesOf(w, ana) != 0 || leavesOf(w, beto) != 0 {
		t.Fatal("leaves were paid before the fire went out")
	}

	w.Step([]Command{dropCmd(action(3), caro, 7, 7)})
	wantFire(t, w, 7, 7, nil)
	if p := mustPlayer(t, w, caro); p.Leaves != 5 || p.LeavesEarned != 5 {
		t.Errorf("caro leaves/earned = %d/%d, want 5/5", p.Leaves, p.LeavesEarned)
	}
	if leavesOf(w, ana) != 0 || leavesOf(w, beto) != 0 {
		t.Error("ana or beto got leaves")
	}
	if tile := mustTile(t, w, 7, 7); tile.Stage != Grass || tile.Humidity != 75 {
		t.Errorf("tile = (%d, %d), want (Grass, 75)", tile.Stage, tile.Humidity)
	}
}

func TestTheSamePlayerCanPutItOutWithThreeDrops(t *testing.T) {
	w := worldWith(catalogWithPPM(0), 1, func(w *World) {
		w.tiles[tileIdx(7, 7)] = burningGrass(0)
	})
	w.Step([]Command{
		dropCmd(action(1), ana, 7, 7),
		dropCmd(action(2), ana, 7, 7),
		dropCmd(action(3), ana, 7, 7),
	})
	wantFire(t, w, 7, 7, nil)
	if got := leavesOf(w, ana); got != 5 {
		t.Errorf("leaves = %d, want 5", got)
	}
}

func TestCP04ThreePlayersInTheSameTickPutItOutOnceWithOneBonus(t *testing.T) {
	w := worldWith(catalogWithPPM(0), 1, func(w *World) {
		w.tiles[tileIdx(7, 7)] = burningGrass(0)
	})
	out := w.Step([]Command{
		dropCmd(action(1), ana, 7, 7),
		dropCmd(action(2), beto, 7, 7),
		dropCmd(action(3), caro, 7, 7),
	})
	wantOutcomes(t, outcomes(out), AppliedOutcome, AppliedOutcome, AppliedOutcome)
	wantFire(t, w, 7, 7, nil)
	var bonus uint64
	for _, id := range []string{ana, beto, caro} {
		bonus += mustPlayer(t, w, id).LeavesEarned
	}
	if bonus != 5 { // un solo bono
		t.Errorf("total bonus = %d, want 5", bonus)
	}
	if got := mustPlayer(t, w, caro).Leaves; got != 5 { // la tercera gota
		t.Errorf("caro leaves = %d, want 5", got)
	}
	if got := mustTile(t, w, 7, 7).Humidity; got != 75 {
		t.Errorf("humidity = %d, want 75", got)
	}
	// La humedad aportada suma lo que las gotas aceptadas.
	if w.HumidityContributed() != 3*25 {
		t.Errorf("humidity contributed = %d, want 75", w.HumidityContributed())
	}

	// Una cuarta gota ya no encuentra fuego: es una gota normal.
	w.Step([]Command{dropCmd(action(4), ana, 7, 7)})
	if mustTile(t, w, 7, 7).Stage != Shrub {
		t.Error("the fourth drop did not grow the tile")
	}
	if got := mustPlayer(t, w, ana).LeavesEarned; got != 2 { // solo el arbusto
		t.Errorf("ana earned = %d, want 2", got)
	}
}

func TestDropsOnABurningTileAddHumidityWithoutGrowingIt(t *testing.T) {
	w := worldWith(catalogWithPPM(0), 1, func(w *World) {
		w.tiles[tileIdx(7, 7)] = burningGrass(80)
	})
	w.Step([]Command{dropCmd(action(1), ana, 7, 7)}) // 105 de humedad, pero arde: no crece
	if tile := mustTile(t, w, 7, 7); tile.Stage != Grass || tile.Humidity != 105 {
		t.Errorf("tile = (%d, %d), want (Grass, 105)", tile.Stage, tile.Humidity)
	}
	wantFire(t, w, 7, 7, fireOf(1, 1))
	if got := leavesOf(w, ana); got != 0 {
		t.Errorf("leaves = %d, want 0", got)
	}
}

func TestGrowthThatWasPendingResolvesWhenTheFireGoesOutForWhoPutItOut(t *testing.T) {
	w := worldWith(catalogWithPPM(0), 1, func(w *World) {
		w.tiles[tileIdx(7, 7)] = Tile{Stage: Grass, Humidity: 80, Fire: fireOf(2, 5)}
	})
	w.Step([]Command{dropCmd(action(1), ana, 7, 7)})
	tile := mustTile(t, w, 7, 7)
	wantFire(t, w, 7, 7, nil)
	if tile.Stage != Shrub || tile.Humidity != 5 {
		t.Errorf("tile = (%d, %d), want (Shrub, 5)", tile.Stage, tile.Humidity)
	}
	if got := leavesOf(w, ana); got != 5+2 { // bono más hojas del arbusto
		t.Errorf("leaves = %d, want 7", got)
	}
}

func TestABiggerDropCountsForEveryBurningTileItReaches(t *testing.T) {
	w := worldWith(catalogWithPPM(0), 1, func(w *World) {
		w.tiles[tileIdx(7, 7)] = Tile{Stage: Grass, Fire: fireOf(2, 5)}
		w.tiles[tileIdx(8, 7)] = burningGrass(0)
		withUpgrade(w, ana, BiggerDrop)
	})
	w.Step([]Command{dropCmd(action(1), ana, 7, 7)})
	wantFire(t, w, 7, 7, nil)
	wantFire(t, w, 8, 7, fireOf(1, 1))
	if got := leavesOf(w, ana); got != 5 {
		t.Errorf("leaves = %d, want 5", got)
	}
}

// Cada fuego apagado es una extinción distinta (decisión del equipo): la gota
// más grande que completa la tercera gota de dos fuegos paga dos bonos.
func TestABiggerDropThatCompletesSeveralFiresPaysTheBonusOncePerFire(t *testing.T) {
	w := worldWith(catalogWithPPM(0), 1, func(w *World) {
		for _, x := range []uint32{7, 8} {
			w.tiles[tileIdx(x, 7)] = Tile{Stage: Grass, Fire: fireOf(2, 5)}
		}
		withUpgrade(w, ana, BiggerDrop)
	})
	w.Step([]Command{dropCmd(action(1), ana, 7, 7)})
	wantFire(t, w, 7, 7, nil)
	wantFire(t, w, 8, 7, nil)
	if got := leavesOf(w, ana); got != 2*5 { // un bono por cada fuego apagado
		t.Errorf("leaves = %d, want 10", got)
	}
}

// ---- Aspersor ----------------------------------------------------------

func sprinklerWorld(owner string) *World {
	return worldWith(catalogWithPPM(0), 1, func(w *World) {
		w.tiles[tileIdx(10, 10)] = Tile{Installation: &Installation{Kind: Sprinkler, OwnerID: owner, RemainingDurability: 30}}
		w.tiles[tileIdx(11, 11)] = burningGrass(0)
		w.tiles[tileIdx(13, 13)] = burningGrass(0) // fuera de la zona 3 x 3
		w.playerSlot(owner).record.LastActionTick = 0
	})
}

func TestASprinklerPutsOutTheFiresOfItsAreaWithoutABonus(t *testing.T) {
	w := sprinklerWorld(ana)
	idle(w, 19)
	wantFire(t, w, 11, 11, fireOf(0, 19)) // todavía no riega
	w.Step(nil)                           // tick 20: riega
	wantFire(t, w, 11, 11, nil)
	// 25 del riego menos 2 de la evaporación, que también cae en el tick 20.
	wantHumidity(t, w, 11, 11, 23)
	if got := leavesOf(w, ana); got != 0 { // sin bono: el aspersor no suelta la tercera gota
		t.Errorf("leaves = %d, want 0", got)
	}
	if mustTile(t, w, 13, 13).Fire == nil { // fuera de su zona sigue ardiendo
		t.Error("the fire outside the area went out")
	}
}

func TestASprinklerOfAnAbsentOwnerDoesNotPutOutFires(t *testing.T) {
	w := sprinklerWorld(ana)
	w.tick = 80_000 // el dueño lleva más de 7.200 s sin actuar
	idle(w, 20)
	if mustTile(t, w, 11, 11).Fire == nil {
		t.Error("the sprinkler of an absent owner put the fire out")
	}
	wantHumidity(t, w, 11, 11, 0)
}

// ---- Consumirse --------------------------------------------------------

func TestAnUnattendedFireBurnsOutAfterTheCatalogTicksIntoDrySoil(t *testing.T) {
	w := worldWith(catalogWithPPM(0), 1, func(w *World) {
		w.tiles[tileIdx(7, 7)] = Tile{
			Stage:        Tree,
			Humidity:     60,
			DryTicks:     3,
			Fire:         fireOf(2, 0),
			Installation: &Installation{Kind: Drip, OwnerID: ana, RemainingDurability: 30},
		}
	})
	idle(w, 299)
	if mustTile(t, w, 7, 7).Fire == nil || mustTile(t, w, 7, 7).Stage != Tree {
		t.Fatal("the fire ended too early")
	}
	w.Step(nil) // tick 300
	tile := mustTile(t, w, 7, 7)
	if tile.Fire != nil || tile.Stage != DrySoil || tile.DryTicks != 0 {
		t.Errorf("tile = (fire %v, %d, %d), want (out, DrySoil, 0)", tile.Fire, tile.Stage, tile.DryTicks)
	}
	if tile.Installation == nil { // la instalación se conserva
		t.Error("the installation was lost")
	}
}

func TestADropOnTheLastTickStillPutsItOutBeforeItBurnsOut(t *testing.T) {
	w := worldWith(catalogWithPPM(0), 1, func(w *World) {
		w.tiles[tileIdx(7, 7)] = Tile{Stage: Grass, Fire: fireOf(2, 299)}
	})
	w.Step([]Command{dropCmd(action(1), ana, 7, 7)}) // los comandos van antes de consumir
	wantFire(t, w, 7, 7, nil)
	if mustTile(t, w, 7, 7).Stage != Grass {
		t.Error("the tile burned out")
	}
}

func TestBurningContinuesInDemoModeSoThatTheFireAlwaysResolves(t *testing.T) {
	w := worldWith(catalogWithPPM(0), 1, func(w *World) {
		w.demoMode = true
		w.tiles[tileIdx(7, 7)] = Tile{Stage: Grass, Fire: fireOf(0, 298)}
	})
	idle(w, 2)
	if mustTile(t, w, 7, 7).Stage != DrySoil {
		t.Error("the fire did not burn out in demo mode")
	}
}

func TestEvaporationContinuesWhileBurningButGrowthAndWitherAreFrozen(t *testing.T) {
	w := worldWith(catalogWithPPM(0), 1, func(w *World) {
		w.tiles[tileIdx(0, 0)] = burningGrass(10)
		w.tiles[tileIdx(1, 0)] = Tile{Stage: Grass, DryTicks: 90, Fire: fireOf(0, 0)}
	})
	idle(w, 30) // tres evaporaciones de 2
	wantHumidity(t, w, 0, 0, 4)
	if frozen := mustTile(t, w, 1, 0); frozen.DryTicks != 90 || frozen.Stage != Grass {
		t.Errorf("frozen tile = (%d, %d), want (90, Grass)", frozen.DryTicks, frozen.Stage)
	}
}

// ---- Azar: determinismo (CP-10) ----------------------------------------

func busyWorld(seed uint64) *World {
	return worldWith(catalogWithPPM(5_000), seed, func(w *World) {
		for x := range ValleyWidth {
			for y := range uint32(4) {
				w.tiles[tileIdx(x, y)] = Tile{Stage: Shrub}
			}
		}
	})
}

// fireScript son gotas de dos jugadores sobre casillas que van cambiando.
func fireScript(tick uint32) []Command {
	n := tick * 2
	return []Command{
		dropCmd(action(n+1), ana, tick%32, tick%4),
		dropCmd(action(n+2), beto, (tick*7)%32, (tick*3)%4),
	}
}

func fireMap(w *World) []int {
	var burnt []int
	for i, tile := range w.Tiles() {
		if tile.Fire != nil {
			burnt = append(burnt, i)
		}
	}
	return burnt
}

func TestCP10SameSeedAndSameCommandsGiveTheSameValley(t *testing.T) {
	a, b := busyWorld(2026), busyWorld(2026)
	everBurned := false
	for tick := range uint32(600) {
		a.Step(fireScript(tick))
		b.Step(fireScript(tick))
		everBurned = everBurned || len(fireMap(a)) > 0
	}
	if !everBurned {
		t.Fatal("the test has to see fire")
	}
	if !reflect.DeepEqual(a, b) { // incluye incendios y el estado del generador
		t.Error("two runs of the same seed and commands gave different valleys")
	}
}

func TestAnotherSeedGivesAnotherFirePattern(t *testing.T) {
	pattern := func(seed uint64) [][]int {
		w := busyWorld(seed)
		var seen [][]int
		for tick := range uint32(300) {
			w.Step(fireScript(tick))
			seen = append(seen, fireMap(w))
		}
		return seen
	}
	if reflect.DeepEqual(pattern(1), pattern(2)) {
		t.Error("seeds 1 and 2 gave the same fire pattern")
	}
}

func TestTheInitialStateIsTheSeed(t *testing.T) {
	w := NewWorld(d01(), 123)
	if want := NewSplitMix64(123).Bytes(); !reflect.DeepEqual(w.RngState(), want) {
		t.Errorf("rng state = %v, want %v", w.RngState(), want)
	}
}

func TestTheRngStateCanBeReadAndSetBack(t *testing.T) {
	w := busyWorld(77)
	idle(w, 40)
	state := w.RngState()
	other := busyWorld(5)
	if !other.SetRngState(state) || !reflect.DeepEqual(other.RngState(), state) {
		t.Fatal("the state did not round trip")
	}
	if other.SetRngState([]byte{1, 2, 3}) {
		t.Error("a malformed state was accepted")
	}
	if !reflect.DeepEqual(other.RngState(), state) {
		t.Error("a refused state changed the generator")
	}
}

// Go-only: el mismo guion con la misma semilla, con compras e instalaciones
// y con fuego de verdad, da el mismo estado completo.
func TestTheSameSeedAndTheSameScriptRunTwiceGiveTheSameWorldState(t *testing.T) {
	everBurned := false
	run := func() *World {
		w := richWorld(catalogWithPPM(5_000), holder{ana, 200}, holder{beto, 200})
		w.rng = NewSplitMix64(99)
		for x := range ValleyWidth {
			for y := uint32(4); y < 8; y++ {
				w.tiles[tileIdx(x, y)] = Tile{Stage: Shrub}
			}
		}
		for tick := range uint32(400) {
			commands := fireScript(tick)
			switch tick {
			case 3:
				commands = append(commands, placeCmd(action(9001), ana, Sprinkler, 1, 5))
			case 5:
				commands = append(commands, placeCmd(action(9002), beto, Drip, 2, 5))
			case 8:
				commands = append(commands, buyCmd(action(9003), ana, BiggerDrop))
			}
			w.Step(commands)
			everBurned = everBurned || len(fireMap(w)) > 0
		}
		return w
	}
	first, second := run(), run()
	if !everBurned {
		t.Fatal("the script has to see fire")
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("two runs of the same seed and script gave different world states")
	}
}
