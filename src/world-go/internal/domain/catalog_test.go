package domain

import (
	"math"
	"slices"
	"testing"
)

// Portadas de world.rs: the_price_grows_15_percent_per_purchase_rounding_up
// y the_price_uses_the_percentage_of_the_catalog_and_never_overflows.

func TestThePriceGrows15PercentPerPurchaseRoundingUp(t *testing.T) {
	catalog := d01()
	prices := func(kind InstallationKind, n uint32) []uint64 {
		var out []uint64
		for k := range n {
			out = append(out, catalog.InstallationPrice(kind, k))
		}
		return out
	}
	// Cada precio es el anterior más un 15 %, redondeado hacia arriba.
	cases := []struct {
		name string
		kind InstallationKind
		n    uint32
		want []uint64
	}{
		{"sprinkler", Sprinkler, 6, []uint64{10, 12, 14, 17, 20, 23}},
		{"drip", Drip, 4, []uint64{25, 29, 34, 40}},
		{"treatment plant", TreatmentPlant, 3, []uint64{40, 46, 53}},
	}
	for _, c := range cases {
		if got := prices(c.kind, c.n); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestThePriceUsesThePercentageOfTheCatalogAndNeverOverflows(t *testing.T) {
	catalog := d01()
	cases := []struct {
		percent   uint32
		purchased uint32
		want      uint64
	}{
		{0, math.MaxUint32, 10},
		{100, 3, 80},
		{15, math.MaxUint32, math.MaxUint64},
	}
	for _, c := range cases {
		catalog.InstallationPriceIncreasePercent = c.percent
		if got := catalog.InstallationPrice(Sprinkler, c.purchased); got != c.want {
			t.Errorf("%d%% x%d: got %d, want %d", c.percent, c.purchased, got, c.want)
		}
	}
}

func TestBasePricesAndUpgradePricesComeFromTheCatalog(t *testing.T) {
	catalog := d01()
	for kind, want := range map[InstallationKind]uint64{Sprinkler: 10, Drip: 25, TreatmentPlant: 40} {
		if got := catalog.InstallationBasePrice(kind); got != want {
			t.Errorf("base price of %d: got %d, want %d", kind, got, want)
		}
	}
	for kind, want := range map[UpgradeKind]uint64{BiggerDrop: 30, BiggerTank: 20} {
		if got := catalog.UpgradePrice(kind); got != want {
			t.Errorf("upgrade price of %d: got %d, want %d", kind, got, want)
		}
	}
}

func TestLeavesForEachStage(t *testing.T) {
	growth := d01().Growth
	for stage, want := range map[TileStage]uint64{DrySoil: 0, Grass: 1, Shrub: 2, Flowers: 4, Tree: 8} {
		if got := growth.LeavesFor(stage); got != want {
			t.Errorf("leaves for stage %d: got %d, want %d", stage, got, want)
		}
	}
}
