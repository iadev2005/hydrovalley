package codec

import (
	persistencev1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/persistence/v1"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
)

// CatalogFromProto convierte la respuesta de GetCatalog en el catálogo del
// dominio (catalog_from_proto de Rust).
//
// Rechaza un catálogo al que le falte un grupo de cifras o cuyas cifras
// harían el juego imposible (divisiones por cero, intervalos o áreas vacíos,
// versión vacía): mejor no arrancar que simular mal.
func CatalogFromProto(message *persistencev1.Catalog) (domain.Catalog, error) {
	if message.GetCatalogVersion() == "" {
		return domain.Catalog{}, invalidCatalog("catalog_version está vacía")
	}
	drop := message.GetDrop()
	tank := message.GetTank()
	growth := message.GetGrowth()
	evaporation := message.GetEvaporation()
	fire := message.GetFire()
	sprinkler := message.GetSprinkler()
	drip := message.GetDrip()
	plant := message.GetTreatmentPlant()
	biggerDrop := message.GetBiggerDrop()
	biggerTank := message.GetBiggerTank()

	// Mismo orden que Rust: primero los grupos que faltan, luego las cifras.
	for _, section := range []struct {
		name    string
		present bool
	}{
		{"drop", drop != nil}, {"tank", tank != nil}, {"growth", growth != nil},
		{"evaporation", evaporation != nil}, {"fire", fire != nil},
		{"sprinkler", sprinkler != nil}, {"drip", drip != nil},
		{"treatment_plant", plant != nil}, {"bigger_drop", biggerDrop != nil},
		{"bigger_tank", biggerTank != nil},
	} {
		if !section.present {
			return domain.Catalog{}, invalidCatalog("falta " + section.name)
		}
	}
	for _, figure := range []struct {
		name  string
		value uint64
	}{
		{"drop.demo_humidity", drop.GetDemoHumidity()},
		{"tank.refill_interval_seconds", uint64(tank.GetRefillIntervalSeconds())},
		{"growth.humidity_per_stage", growth.GetHumidityPerStage()},
		{"evaporation.wither_after_ticks", uint64(evaporation.GetWitherAfterTicks())},
		{"fire.drops_to_extinguish", uint64(fire.GetDropsToExtinguish())},
		{"fire.burn_out_ticks", uint64(fire.GetBurnOutTicks())},
		{"sprinkler.area_side", uint64(sprinkler.GetAreaSide())},
		{"sprinkler.interval_seconds", uint64(sprinkler.GetIntervalSeconds())},
		{"drip.area_side", uint64(drip.GetAreaSide())},
		{"drip.evaporation_divisor", uint64(drip.GetEvaporationDivisor())},
		{"treatment_plant.interval_seconds", uint64(plant.GetIntervalSeconds())},
		{"bigger_drop.area_side", uint64(biggerDrop.GetAreaSide())},
	} {
		if figure.value == 0 {
			return domain.Catalog{}, invalidCatalog(figure.name + " debe ser mayor que 0")
		}
	}

	return domain.Catalog{
		Version: message.GetCatalogVersion(),
		Drop: domain.DropRules{
			WaterCost:    drop.GetWaterCost(),
			Humidity:     drop.GetHumidity(),
			DemoHumidity: drop.GetDemoHumidity(),
		},
		Tank: domain.TankRules{
			Capacity:              tank.GetCapacity(),
			RefillAmount:          tank.GetRefillAmount(),
			RefillIntervalSeconds: tank.GetRefillIntervalSeconds(),
		},
		Growth: domain.GrowthRules{
			HumidityPerStage: growth.GetHumidityPerStage(),
			LeavesGrass:      growth.GetLeavesGrass(),
			LeavesShrub:      growth.GetLeavesShrub(),
			LeavesFlowers:    growth.GetLeavesFlowers(),
			LeavesTree:       growth.GetLeavesTree(),
			TreeMaxHumidity:  growth.GetTreeMaxHumidity(),
		},
		Evaporation: domain.EvaporationRules{
			HumidityPerSecond: evaporation.GetHumidityPerSecond(),
			WitherAfterTicks:  evaporation.GetWitherAfterTicks(),
		},
		Fire: domain.FireRules{
			DropsToExtinguish:     fire.GetDropsToExtinguish(),
			ExtinguishBonusLeaves: fire.GetExtinguishBonusLeaves(),
			BurnOutTicks:          fire.GetBurnOutTicks(),
			IgnitionChancePPM:     fire.GetIgnitionChancePpm(),
		},
		Sprinkler: domain.SprinklerRules{
			Price:           sprinkler.GetPrice(),
			AreaSide:        sprinkler.GetAreaSide(),
			Humidity:        sprinkler.GetHumidity(),
			IntervalSeconds: sprinkler.GetIntervalSeconds(),
		},
		Drip: domain.DripRules{
			Price:              drip.GetPrice(),
			AreaSide:           drip.GetAreaSide(),
			EvaporationDivisor: drip.GetEvaporationDivisor(),
		},
		TreatmentPlant: domain.TreatmentPlantRules{
			Price:           plant.GetPrice(),
			RefillAmount:    plant.GetRefillAmount(),
			IntervalSeconds: plant.GetIntervalSeconds(),
		},
		InstallationPriceIncreasePercent: message.GetInstallationPriceIncreasePercent(),
		BiggerDrop: domain.BiggerDropRules{
			Price:    biggerDrop.GetPrice(),
			AreaSide: biggerDrop.GetAreaSide(),
		},
		BiggerTank: domain.BiggerTankRules{
			Price:         biggerTank.GetPrice(),
			ExtraCapacity: biggerTank.GetExtraCapacity(),
		},
		OfflineInstallationSeconds: message.GetOfflineInstallationSeconds(),
	}, nil
}

func invalidCatalog(reason string) error { return &InvalidCatalogError{Reason: reason} }
