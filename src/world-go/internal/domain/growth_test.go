package domain

import (
	"slices"
	"testing"
)

// Portadas de las pruebas de growth.rs. Mismas cifras literales.

func tileOf(stage TileStage, humidity uint64) Tile {
	return Tile{Stage: stage, Humidity: humidity}
}

func burning(t Tile) Tile {
	t.Fire = &Fire{}
	return t
}

func TestAddHumidity(t *testing.T) {
	cases := []struct {
		name         string
		tile         Tile
		amount       uint64
		wantStage    TileStage
		wantHumidity uint64
		wantLeaves   uint64
	}{
		{"below the threshold only accumulates", tileOf(DrySoil, 50), 25, DrySoil, 75, 0},
		{"reaching the threshold grows one stage and spends the humidity", tileOf(DrySoil, 75), 25, Grass, 0, 1},
		{"the leftover above the threshold stays in the tile", tileOf(Grass, 90), 25, Shrub, 15, 2},
		{"a huge drop climbs several stages and pays each one", tileOf(DrySoil, 0), 250, Shrub, 50, 3},
		{"a tree does not grow and its humidity is capped", tileOf(Tree, 90), 25, Tree, 100, 0},
		{"growing into a tree caps the leftover too", tileOf(Flowers, 100), 250, Tree, 100, 8},
		{"a burning tile takes the humidity but does not grow", burning(tileOf(Grass, 90)), 25, Grass, 115, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tile := c.tile
			leaves := AddHumidity(&tile, c.amount, d01().Growth)
			if tile.Stage != c.wantStage || tile.Humidity != c.wantHumidity || leaves != c.wantLeaves {
				t.Errorf("got (%d, %d, %d), want (%d, %d, %d)",
					tile.Stage, tile.Humidity, leaves, c.wantStage, c.wantHumidity, c.wantLeaves)
			}
		})
	}
}

func TestEveryStagePaysItsOwnLeaves(t *testing.T) {
	cases := []struct {
		from, to TileStage
		leaves   uint64
	}{
		{DrySoil, Grass, 1},
		{Grass, Shrub, 2},
		{Shrub, Flowers, 4},
		{Flowers, Tree, 8},
	}
	for _, c := range cases {
		tile := tileOf(c.from, 99)
		if got := AddHumidity(&tile, 1, d01().Growth); got != c.leaves {
			t.Errorf("from %d: got %d leaves, want %d", c.from, got, c.leaves)
		}
		if tile.Stage != c.to {
			t.Errorf("from %d: got stage %d, want %d", c.from, tile.Stage, c.to)
		}
	}
}

func TestTheThresholdsComeFromTheCatalog(t *testing.T) {
	rules := d01().Growth
	rules.HumidityPerStage = 40
	rules.LeavesGrass = 5
	tile := tileOf(DrySoil, 30)
	if got := AddHumidity(&tile, 25, rules); got != 5 {
		t.Errorf("got %d leaves, want 5", got)
	}
	if tile.Stage != Grass || tile.Humidity != 15 {
		t.Errorf("got (%d, %d), want (Grass, 15)", tile.Stage, tile.Humidity)
	}
}

func TestGrowingResetsTheDryCounter(t *testing.T) {
	tile := tileOf(Grass, 99)
	tile.DryTicks = 50
	AddHumidity(&tile, 1, d01().Growth)
	if tile.DryTicks != 0 {
		t.Errorf("dry ticks = %d, want 0", tile.DryTicks)
	}
}

func TestAddHumiditySaturatesInsteadOfWrapping(t *testing.T) {
	// satAdd en lugar de desbordar: humedad máxima + 1 sigue siendo máxima
	// antes de gastar las etapas (growth.rs:23).
	tile := tileOf(Tree, ^uint64(0))
	AddHumidity(&tile, 1, d01().Growth)
	if tile.Humidity != 100 {
		t.Errorf("tree humidity = %d, want the cap 100", tile.Humidity)
	}
}

func TestEvaporationSubtractsWithoutGoingBelowZero(t *testing.T) {
	tile := tileOf(Grass, 3)
	Evaporate(&tile, 2)
	if tile.Humidity != 1 {
		t.Fatalf("humidity = %d, want 1", tile.Humidity)
	}
	Evaporate(&tile, 2)
	if tile.Humidity != 0 {
		t.Fatalf("humidity = %d, want 0", tile.Humidity)
	}
}

func TestABurningTileStillEvaporates(t *testing.T) {
	tile := burning(tileOf(Grass, 10))
	Evaporate(&tile, 2)
	if tile.Humidity != 8 {
		t.Errorf("humidity = %d, want 8", tile.Humidity)
	}
}

func TestAPlantWithersAfterTheCatalogTicksAtZeroHumidity(t *testing.T) {
	rules := d01().Evaporation
	tile := tileOf(Shrub, 0)
	for range rules.WitherAfterTicks - 1 {
		if Wither(&tile, rules) {
			t.Fatal("withered too early")
		}
	}
	if tile.Stage != Shrub || tile.DryTicks != 99 {
		t.Fatalf("got (%d, %d), want (Shrub, 99)", tile.Stage, tile.DryTicks)
	}
	if !Wither(&tile, rules) {
		t.Fatal("did not wither at the threshold")
	}
	if tile.Stage != Grass || tile.DryTicks != 0 {
		t.Errorf("got (%d, %d), want (Grass, 0)", tile.Stage, tile.DryTicks)
	}
}

func TestEveryStageDropsOneStageDownToDrySoil(t *testing.T) {
	rules := d01().Evaporation
	tile := tileOf(Tree, 0)
	seen := []TileStage{tile.Stage}
	for range 4 * rules.WitherAfterTicks {
		if Wither(&tile, rules) {
			seen = append(seen, tile.Stage)
		}
	}
	want := []TileStage{Tree, Flowers, Shrub, Grass, DrySoil}
	if !slices.Equal(seen, want) {
		t.Errorf("got %v, want %v", seen, want)
	}
}

func TestAnyHumidityResetsTheDryCounter(t *testing.T) {
	rules := d01().Evaporation
	tile := tileOf(Grass, 0)
	for range 60 {
		Wither(&tile, rules)
	}
	if tile.DryTicks != 60 {
		t.Fatalf("dry ticks = %d, want 60", tile.DryTicks)
	}
	tile.Humidity = 1
	if Wither(&tile, rules) {
		t.Error("withered with humidity")
	}
	if tile.DryTicks != 0 {
		t.Errorf("dry ticks = %d, want 0", tile.DryTicks)
	}
}

func TestDrySoilHasNothingToWither(t *testing.T) {
	rules := d01().Evaporation
	tile := tileOf(DrySoil, 0)
	for range 500 {
		if Wither(&tile, rules) {
			t.Fatal("dry soil withered")
		}
	}
	if tile.Stage != DrySoil || tile.DryTicks != 0 {
		t.Errorf("got (%d, %d), want (DrySoil, 0)", tile.Stage, tile.DryTicks)
	}
}

func TestABurningTileFreezesTheDryCounter(t *testing.T) {
	rules := d01().Evaporation
	tile := burning(tileOf(Grass, 0))
	tile.DryTicks = 98
	for range 10 {
		if Wither(&tile, rules) {
			t.Fatal("a burning tile withered")
		}
	}
	if tile.Stage != Grass || tile.DryTicks != 98 {
		t.Errorf("got (%d, %d), want (Grass, 98)", tile.Stage, tile.DryTicks)
	}
}
