package domain

import "math/bits"

// Catalog son las cifras del juego que aplica el simulador (D-01), tal como
// las entrega CatalogService.GetCatalog.
//
// Las cifras viven en catalog/ y se ajustan sin tocar código: el simulador
// las pide al arrancar y al reiniciar el valle, y las pasa al World. Aquí no
// hay valores por omisión: un mundo solo existe con el catálogo que entregó
// la persistencia.
//
// Las unidades son las del contrato: litros, hojas y puntos de humedad; los
// intervalos *Seconds cuentan segundos y las duraciones *Ticks, ticks de
// 100 ms.
type Catalog struct {
	// Version es el SHA-256 del contenido; va en cada instantánea.
	Version        string
	Drop           DropRules
	Tank           TankRules
	Growth         GrowthRules
	Evaporation    EvaporationRules
	Fire           FireRules
	Sprinkler      SprinklerRules
	Drip           DripRules
	TreatmentPlant TreatmentPlantRules
	TrashBin       TrashBinRules
	Terrain        TerrainRules
	Urban          UrbanRules
	// InstallationPriceIncreasePercent es el aumento de precio por cada
	// compra del mismo tipo de instalación.
	InstallationPriceIncreasePercent uint32
	BiggerDrop                       BiggerDropRules
	BiggerTank                       BiggerTankRules
	// OfflineInstallationSeconds son los segundos que las instalaciones
	// siguen trabajando tras la última acción de su dueño.
	OfflineInstallationSeconds uint64
}

// DropRules son las cifras de una gota.
type DropRules struct {
	// WaterCost son los litros que cuesta una gota fuera del modo demo.
	WaterCost uint64
	// Humidity es la humedad que suma una gota fuera del modo demo.
	Humidity uint64
	// DemoHumidity es la humedad que suma una gota en modo demo (gratis y de
	// una sola casilla).
	DemoHumidity uint64
}

// TankRules son las cifras del depósito.
type TankRules struct {
	// Capacity es la capacidad inicial; un jugador nuevo entra con el
	// depósito lleno.
	Capacity              uint64
	RefillAmount          uint64
	RefillIntervalSeconds uint32
}

// GrowthRules son las cifras del crecimiento.
type GrowthRules struct {
	HumidityPerStage uint64
	LeavesGrass      uint64
	LeavesShrub      uint64
	LeavesFlowers    uint64
	LeavesTree       uint64
	TreeMaxHumidity  uint64
}

// LeavesFor son las hojas que gana quien hace llegar una casilla a stage
// (0 en tierra seca: nunca se llega a ella creciendo).
func (g GrowthRules) LeavesFor(stage TileStage) uint64 {
	switch stage {
	case Grass:
		return g.LeavesGrass
	case Shrub:
		return g.LeavesShrub
	case Flowers:
		return g.LeavesFlowers
	case Tree:
		return g.LeavesTree
	default:
		return 0
	}
}

// EvaporationRules son las cifras de la evaporación y el marchitamiento.
type EvaporationRules struct {
	HumidityPerSecond uint64
	WitherAfterTicks  uint32
}

// FireRules son las cifras de los incendios.
type FireRules struct {
	DropsToExtinguish     uint32
	ExtinguishBonusLeaves uint64
	BurnOutTicks          uint32
	// IgnitionChancePPM son partes por millón por tick.
	IgnitionChancePPM uint32
	// PropagationChancePPM son partes por millón por tick.
	PropagationChancePPM uint32
}

// SprinklerRules son las cifras del aspersor de agua gris (Reutilizar).
type SprinklerRules struct {
	Price           uint64
	AreaSide        uint32
	Humidity        uint64
	IntervalSeconds uint32
	Durability      uint32
}

// DripRules son las cifras del riego por goteo (Reducir).
type DripRules struct {
	Price              uint64
	AreaSide           uint32
	EvaporationDivisor uint32
	Durability         uint32
	// WearIntervalSeconds son los segundos de trabajo activo que cuestan un
	// punto de durabilidad.
	WearIntervalSeconds uint32
}

// TreatmentPlantRules son las cifras de la planta de tratamiento (Reciclar).
type TreatmentPlantRules struct {
	Price           uint64
	RefillAmount    uint64
	IntervalSeconds uint32
	Durability      uint32
}

// TrashBinRules son las cifras del cesto de reciclaje (Fase 4).
type TrashBinRules struct {
	Price           uint64
	AreaSide        uint32
	IntervalSeconds uint32
	Durability      uint32
}

// TerrainRules es el terreno natural que se genera al crear o reiniciar el
// valle (Fase 3). Con los dos valores en cero el valle empieza vacío.
type TerrainRules struct {
	// RiverWidth es el ancho del río en casillas; 0 = sin río ni lago.
	RiverWidth uint32
	// RockCount son las rocas sueltas que se intentan colocar; 0 = sin rocas.
	RockCount uint32
}

// UrbanRules es la presión urbana (Fase 4): edificios que ensucian el valle.
type UrbanRules struct {
	// BuildingTreeThreshold son los árboles en el valle a partir de los que
	// aparece el primer edificio...
	BuildingTreeThreshold uint32
	// BuildingHumidityThreshold es la humedad total aportada a partir de la
	// que aparece.
	BuildingHumidityThreshold uint64
	// MaxBuildings es el tope de edificios en el valle.
	MaxBuildings uint32
	// TrashIntervalSeconds son los segundos entre dos emisiones de basura de
	// un edificio.
	TrashIntervalSeconds uint32
	// TrashSpreadIntervalSeconds son los segundos entre dos propagaciones de
	// la basura de nivel 3.
	TrashSpreadIntervalSeconds uint32
	// CleanRewardLeaves son las hojas de quien deja limpia una casilla.
	CleanRewardLeaves uint64
}

// BiggerDropRules son las cifras de la mejora de gota más grande.
type BiggerDropRules struct {
	Price    uint64
	AreaSide uint32
}

// BiggerTankRules son las cifras de la mejora de depósito mayor.
type BiggerTankRules struct {
	Price         uint64
	ExtraCapacity uint64
}

// InstallationBasePrice es el precio base (la primera compra) de una
// instalación.
func (c *Catalog) InstallationBasePrice(kind InstallationKind) uint64 {
	switch kind {
	case Sprinkler:
		return c.Sprinkler.Price
	case Drip:
		return c.Drip.Price
	case TrashBin:
		return c.TrashBin.Price
	default:
		return c.TreatmentPlant.Price
	}
}

// InstallationBaseDurability es la durabilidad base de una instalación al colocarse.
func (c *Catalog) InstallationBaseDurability(kind InstallationKind) uint32 {
	switch kind {
	case Sprinkler:
		return c.Sprinkler.Durability
	case Drip:
		return c.Drip.Durability
	case TrashBin:
		return c.TrashBin.Durability
	default:
		return c.TreatmentPlant.Durability
	}
}

// InstallationPrice es el precio de la siguiente instalación de kind cuando
// el jugador ya compró purchased del mismo tipo (RF-07).
//
// Cada precio es el anterior más InstallationPriceIncreasePercent por
// ciento, redondeado hacia arriba al entero siguiente: con el 15 % del
// catálogo, un aspersor cuesta 10, 12, 14, 17, 20, 23... (11,5 sube a 12). Se
// encadena sobre el precio entero anterior, no sobre el decimal exacto. La
// cuenta usa 128 bits y se detiene al saturar en u64::MAX (o si el aumento
// no cambia el precio, p. ej. 0 %), así que ni desborda ni itera de más.
func (c *Catalog) InstallationPrice(kind InstallationKind, purchased uint32) uint64 {
	factor := 100 + uint64(c.InstallationPriceIncreasePercent)
	price := c.InstallationBasePrice(kind)
	for range purchased {
		next := mulCeilDiv100(price, factor)
		if next == price {
			break
		}
		price = next
	}
	return price
}

// mulCeilDiv100 es ceil(a*b/100) calculado con 128 bits, saturado a u64::MAX
// (el equivalente de u64::try_from(u128).unwrap_or(u64::MAX) de Rust).
func mulCeilDiv100(a, b uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	lo, carry := bits.Add64(lo, 99, 0)
	hi += carry // a*b < 2^128 - 2^65 + 1, así que sumar 99 no desborda hi
	if hi >= 100 {
		return ^uint64(0) // el cociente no cabe en 64 bits
	}
	quotient, _ := bits.Div64(hi, lo, 100)
	return quotient
}

// UpgradePrice es el precio de una mejora (compra única).
func (c *Catalog) UpgradePrice(kind UpgradeKind) uint64 {
	if kind == BiggerDrop {
		return c.BiggerDrop.Price
	}
	return c.BiggerTank.Price
}
