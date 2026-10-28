package domain

import "testing"

// Portadas de las pruebas de fire.rs que cubren los ayudantes de casilla que
// usa T2 (receive_drop, extinguish y burn) y los que los acompañan
// (is_eligible, ignite).

func grassTile(humidity uint64) Tile { return tileOf(Grass, humidity) }

func TestEligibilityNeedsAPlantNoHumidityAndNoFire(t *testing.T) {
	g0, g1, dry := grassTile(0), grassTile(1), Tile{}
	if !IsEligible(&g0) {
		t.Error("grass without humidity should be eligible")
	}
	if IsEligible(&g1) {
		t.Error("humidity should make it ineligible")
	}
	if IsEligible(&dry) {
		t.Error("dry soil should be ineligible")
	}
	lit := grassTile(0)
	Ignite(&lit)
	if IsEligible(&lit) {
		t.Error("a burning tile should be ineligible")
	}
}

func TestTheThirdDropPutsTheFireOutAndLaterDropsFindNone(t *testing.T) {
	rules := d01().Fire
	tile := grassTile(0)
	Ignite(&tile)
	for i, want := range []bool{false, false, true, false} {
		if got := ReceiveDrop(&tile, rules); got != want {
			t.Fatalf("drop %d: got %v, want %v", i+1, got, want)
		}
	}
	if tile.Fire != nil {
		t.Error("the fire should be out")
	}
}

func TestBurningOutDropsTheTileToDrySoilKeepingHumidityAndInstallation(t *testing.T) {
	rules := d01().Fire
	tile := grassTile(40)
	tile.DryTicks = 5
	tile.Installation = &Installation{Kind: Drip, OwnerID: "ana", RemainingDurability: 30}
	Ignite(&tile)
	for range 299 {
		if Burn(&tile, rules) {
			t.Fatal("burned out too early")
		}
	}
	if !Burn(&tile, rules) {
		t.Fatal("did not burn out at 300 ticks")
	}
	if tile.Stage != DrySoil || tile.Humidity != 40 || tile.DryTicks != 0 {
		t.Errorf("got (%d, %d, %d), want (DrySoil, 40, 0)", tile.Stage, tile.Humidity, tile.DryTicks)
	}
	if tile.Fire != nil || tile.Installation == nil {
		t.Error("the fire should be gone and the installation kept")
	}
	if Burn(&tile, rules) {
		t.Error("without fire there is nothing to burn out")
	}
}

func TestExtinguishingReportsWhetherItWasBurning(t *testing.T) {
	tile := grassTile(0)
	if Extinguish(&tile) {
		t.Error("it was not burning")
	}
	Ignite(&tile)
	if !Extinguish(&tile) {
		t.Error("it was burning")
	}
}
