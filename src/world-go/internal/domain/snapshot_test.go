package domain

import (
	"errors"
	"reflect"
	"testing"
)

// Portadas de las pruebas de world.rs y world_fire_tests.rs que pasan por la
// instantánea: restore y parts (world.rs:1146-1190, 1255-1264, 2478-2516,
// 2586-2596; world_fire_tests.rs:485-523). Las que quedaron pendientes en T2 a
// T5 porque Go todavía no restauraba instantáneas.

func mustRestore(t *testing.T, catalog Catalog, parts WorldParts) *World {
	t.Helper()
	w, err := Restore(catalog, parts)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	return w
}

func TestRestoreRoundTripsTheWholeWorld(t *testing.T) {
	w := newTestWorld()
	w.setDemoMode(true)
	w.Step([]Command{
		dropCmd(action(1), ana, 3, 4),
		dropCmd(action(2), beto, 31, 31),
	})
	parts := w.Parts()
	if parts.Tick != 1 || len(parts.Players) != 2 {
		t.Fatalf("tick/players = %d/%d, want 1/2", parts.Tick, len(parts.Players))
	}
	if parts.Players[0].PlayerID >= parts.Players[1].PlayerID {
		t.Error("the players are not sorted by player_id")
	}

	restored := mustRestore(t, d01(), parts)
	if got := restored.Parts(); !reflect.DeepEqual(got, parts) {
		t.Errorf("restored parts differ:\n got %+v\nwant %+v", got, parts)
	}
}

func TestARestoredWorldContinuesTheTickAndKeepsTheDuplicateWindow(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{dropCmd(action(1), ana, 3, 4)})
	restored := mustRestore(t, d01(), w.Parts())

	out := restored.Step([]Command{dropCmd(action(1), ana, 3, 4)})
	if out.Tick != 2 {
		t.Errorf("tick = %d, want 2", out.Tick)
	}
	wantOutcomes(t, outcomes(out), DuplicateOutcome)
	if got := humidityAt(t, restored, 3, 4); got != 25 {
		t.Errorf("humidity = %d, want 25", got)
	}
}

func TestRestoreRejectsAValleyOfAnotherSize(t *testing.T) {
	parts := newTestWorld().Parts()
	parts.Width = 16
	_, err := Restore(d01(), parts)
	if want := (DimensionsError{ExpectedWidth: 32, ExpectedHeight: 32, FoundWidth: 16, FoundHeight: 32}); err != want {
		t.Errorf("err = %v, want %v", err, want)
	}

	parts = newTestWorld().Parts()
	parts.Tiles = parts.Tiles[:len(parts.Tiles)-1]
	_, err = Restore(d01(), parts)
	if want := (TileCountError{Expected: 1024, Found: 1023}); err != want {
		t.Errorf("err = %v, want %v", err, want)
	}
}

func TestRestoreRejectsARepeatedPlayer(t *testing.T) {
	parts := newTestWorld().Parts()
	parts.Players = []Player{startingPlayer(ana, 20), startingPlayer(ana, 20)}
	_, err := Restore(d01(), parts)
	if want := (DuplicatePlayerError{PlayerID: ana}); err != want {
		t.Errorf("err = %v, want %v", err, want)
	}
}

func TestRestoreAppliesTheCurrentCatalogAndNotTheOneOfTheSnapshot(t *testing.T) {
	old := newTestWorld().Parts()
	if old.CatalogVersion != d01Version {
		t.Fatalf("version = %q, want %q", old.CatalogVersion, d01Version)
	}

	restored := mustRestore(t, customCatalog(), old)
	if restored.CatalogVersion() != "custom" || restored.Parts().CatalogVersion != "custom" {
		t.Errorf("version = %q / %q, want custom", restored.CatalogVersion(), restored.Parts().CatalogVersion)
	}
	restored.Step([]Command{dropCmd(action(1), ana, 0, 0)})
	if got := humidityAt(t, restored, 0, 0); got != 40 {
		t.Errorf("humidity = %d, want 40 (the drop of the current catalog)", got)
	}
}

func TestTheSnapshotCarriesInstallationsPurchasesUpgradesAndThe3RCounters(t *testing.T) {
	w := rich(holder{ana, 200})
	w.Step([]Command{
		placeCmd(action(1), ana, Sprinkler, 10, 10),
		placeCmd(action(2), ana, Drip, 12, 10),
		placeCmd(action(3), ana, TreatmentPlant, 2, 2),
		buyCmd(action(4), ana, BiggerTank),
		buyCmd(action(5), ana, BiggerDrop),
	})
	w.Step(drops(10, 8, ana, 11, 10))
	advanceTo(t, w, 60)

	parts := w.Parts()
	var player Player
	for _, p := range parts.Players {
		if p.PlayerID == ana {
			player = p
		}
	}
	if player.WaterReused == 0 || player.WaterReduced == 0 || player.WaterRecycled == 0 {
		t.Errorf("the 3R counters are not all above 0: %+v", player)
	}
	if len(player.Upgrades) != 2 {
		t.Errorf("upgrades = %v, want 2", player.Upgrades)
	}
	restored := mustRestore(t, d01(), parts)
	if got := restored.Parts(); !reflect.DeepEqual(got, parts) {
		t.Error("the snapshot lost installations, purchases, upgrades or counters")
	}
}

func TestTheSnapshotCarriesTheStateOfTheNewRules(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{demoCmd(action(1), false)})
	w.Step(drops(10, 4, ana, 6, 6)) // pasto, humedad 0
	advanceTo(t, w, 30)             // dry_ticks > 0
	w.Step(drops(50, 2, beto, 7, 7))
	w.Step([]Command{resetCmd(action(98))})
	w.Step(drops(60, 4, ana, 6, 6))
	advanceTo(t, w, 45)
	w.Step([]Command{demoCmd(action(97), true)})

	parts := w.Parts()
	if parts.Epoch != 1 || !parts.DemoMode {
		t.Errorf("epoch/demo = %d/%v, want 1/true", parts.Epoch, parts.DemoMode)
	}
	if parts.Tiles[6*32+6].DryTicks == 0 {
		t.Error("dry_ticks should be above 0")
	}
	restored := mustRestore(t, d01(), parts)
	if got := restored.Parts(); !reflect.DeepEqual(got, parts) {
		t.Error("the snapshot lost the state of the new rules")
	}
}

func TestRestoringASnapshotMidRunDoesNotChangeTheOutcome(t *testing.T) {
	straight := scriptWorld()
	runScript(straight, 1, 400)

	interrupted := scriptWorld()
	runScript(interrupted, 1, 150)
	resumed := mustRestore(t, d01(), interrupted.Parts())
	runScript(resumed, 151, 400)
	if !reflect.DeepEqual(resumed.Parts(), straight.Parts()) {
		t.Error("restoring mid-run changed the outcome")
	}
}

// ---- Azar y persistencia (world_fire_tests.rs:485-523) -------------------

func TestARestoredWorldContinuesTheSameRandomSequence(t *testing.T) {
	original := busyWorld(77)
	for tick := range uint32(300) {
		original.Step(fireScript(tick))
	}
	if len(original.Parts().RngState) == 0 {
		t.Fatal("the generator state does not travel in the parts")
	}
	restored := mustRestore(t, catalogWithPPM(5_000), original.Parts())
	if !reflect.DeepEqual(restored.Parts(), original.Parts()) {
		t.Fatal("the restored world differs from the original")
	}
	everBurned := false
	for tick := uint32(300); tick < 700; tick++ {
		original.Step(fireScript(tick))
		restored.Step(fireScript(tick))
		everBurned = everBurned || len(fireMap(restored)) > 0
	}
	if !everBurned {
		t.Error("the test has to see fire after the restore")
	}
	if !reflect.DeepEqual(restored.Parts(), original.Parts()) {
		t.Error("the restored world drew another random sequence")
	}
}

func TestTheInitialStateTravelsInTheParts(t *testing.T) {
	w := NewWorld(d01(), 123)
	if want := NewSplitMix64(123).Bytes(); !reflect.DeepEqual(w.Parts().RngState, want) {
		t.Errorf("rng state = %v, want %v", w.Parts().RngState, want)
	}
}

func TestRestoringASnapshotWithoutRngStateFallsBackToASeedFromItsTick(t *testing.T) {
	parts := NewWorld(d01(), 5).Parts()
	parts.Tick = 40
	parts.RngState = nil
	a := mustRestore(t, d01(), parts)
	b := mustRestore(t, d01(), parts)
	if want := NewSplitMix64(40).Bytes(); !reflect.DeepEqual(a.Parts().RngState, want) {
		t.Errorf("rng state = %v, want %v", a.Parts().RngState, want)
	}
	if !reflect.DeepEqual(a.Parts(), b.Parts()) {
		t.Error("the fallback is not deterministic")
	}

	// Un estado vacío y uno ausente son lo mismo.
	parts.RngState = []byte{}
	if got := mustRestore(t, d01(), parts).Parts().RngState; !reflect.DeepEqual(got, NewSplitMix64(40).Bytes()) {
		t.Errorf("an empty state did not fall back: %v", got)
	}
}

func TestRestoringASnapshotWithAMalformedRngStateIsRefused(t *testing.T) {
	parts := NewWorld(d01(), 5).Parts()
	parts.RngState = []byte{1, 2, 3}
	_, err := Restore(d01(), parts)
	if want := (RngStateError{Len: 3}); err != want {
		t.Errorf("err = %v, want %v", err, want)
	}
	var target RngStateError
	if !errors.As(err, &target) {
		t.Error("the error is not a RngStateError")
	}
}

// ---- Propias de Go: copias y canonización --------------------------------

// Parts y Restore no comparten memoria con el mundo (Rust clona y mueve): un
// cambio en una copia no puede llegar al otro lado.
func TestPartsAndRestoreDoNotShareMemoryWithTheWorld(t *testing.T) {
	w := richWorld(d01(), holder{ana, 100})
	w.Step([]Command{placeCmd(action(1), ana, Drip, 3, 3), buyCmd(action(2), ana, BiggerTank)})
	w.tiles[0] = Tile{Stage: Grass, Fire: &Fire{DropsReceived: 1}}
	before := w.Parts()

	parts := w.Parts()
	parts.Tiles[0].Fire.DropsReceived = 99
	parts.Tiles[tileIdx(3, 3)].Installation.OwnerID = "x"
	parts.Players[0].Upgrades[0] = BiggerDrop
	parts.Players[0].Purchases[0].Count = 99
	parts.Players[0].RecentActions[0].ActionID = "x"
	parts.RngState[0] ^= 0xff
	if !reflect.DeepEqual(w.Parts(), before) {
		t.Fatal("changing the parts changed the world")
	}

	restored := mustRestore(t, d01(), before)
	before.Tiles[0].Fire.BurningTicks = 7
	before.Players[0].Upgrades[0] = BiggerDrop
	before.RngState[0] ^= 0xff
	if reflect.DeepEqual(restored.Parts(), before) {
		t.Fatal("changing the parts after Restore changed the restored world")
	}
}

// Los vacíos se normalizan a nil, para que la igualdad profunda de Parts sea
// la de Rust (Vec vacío == Vec vacío).
func TestPartsNormalizesEmptyCollectionsToNil(t *testing.T) {
	w := newTestWorld()
	w.Step([]Command{dropCmd(action(1), ana, 0, 0)})
	idle(w, 700) // la ventana olvida la acción: RecentActions queda vacío, no nil
	parts := w.Parts()
	player := parts.Players[0]
	if player.RecentActions != nil || player.Purchases != nil || player.Upgrades != nil {
		t.Errorf("empty collections should be nil: %+v", player)
	}
	restored := mustRestore(t, d01(), parts)
	if !reflect.DeepEqual(restored.Parts(), parts) {
		t.Error("the round trip changed the parts")
	}
}

func TestRestoreSortsTheRecentActionsByTick(t *testing.T) {
	parts := newTestWorld().Parts()
	player := startingPlayer(ana, 20)
	player.RecentActions = []AppliedAction{
		{ActionID: action(2), Tick: 9},
		{ActionID: action(1), Tick: 3},
		{ActionID: action(3), Tick: 9},
	}
	parts.Players = []Player{player}
	got := mustRestore(t, d01(), parts).Parts().Players[0].RecentActions
	want := []AppliedAction{
		{ActionID: action(1), Tick: 3},
		{ActionID: action(2), Tick: 9},
		{ActionID: action(3), Tick: 9},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("recent actions = %v, want %v", got, want)
	}
}
