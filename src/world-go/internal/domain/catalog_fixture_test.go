package domain

// d01Version es la versión del catálogo de pruebas.
const d01Version = "d01-fixture"

// d01 son las cifras de arranque de D-01, solo para las pruebas: en
// producción las entrega la persistencia. Es el mismo catálogo que usa
// Catalog::d01() en las pruebas de Rust (con la ignición en 0 ppm).
func d01() Catalog {
	return Catalog{
		Version:     d01Version,
		Drop:        DropRules{WaterCost: 1, Humidity: 25, DemoHumidity: 1},
		Tank:        TankRules{Capacity: 20, RefillAmount: 1, RefillIntervalSeconds: 2},
		Growth:      GrowthRules{HumidityPerStage: 100, LeavesGrass: 1, LeavesShrub: 2, LeavesFlowers: 4, LeavesTree: 8, TreeMaxHumidity: 100},
		Evaporation: EvaporationRules{HumidityPerSecond: 2, WitherAfterTicks: 100},
		Fire: FireRules{
			DropsToExtinguish:     3,
			ExtinguishBonusLeaves: 5,
			BurnOutTicks:          300,
			IgnitionChancePPM:     0,
		},
		Sprinkler:                        SprinklerRules{Price: 10, AreaSide: 3, Humidity: 25, IntervalSeconds: 2},
		Drip:                             DripRules{Price: 25, AreaSide: 5, EvaporationDivisor: 2},
		TreatmentPlant:                   TreatmentPlantRules{Price: 40, RefillAmount: 1, IntervalSeconds: 2},
		InstallationPriceIncreasePercent: 15,
		BiggerDrop:                       BiggerDropRules{Price: 30, AreaSide: 3},
		BiggerTank:                       BiggerTankRules{Price: 20, ExtraCapacity: 10},
		OfflineInstallationSeconds:       7200,
	}
}
